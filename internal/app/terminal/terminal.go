// Package terminal : The assistant reacting to a terminal on the owner's
// laptop that wants attention.
//
// Owner, 17 October 2026: the assistant is to *"hear when one finishes,
// to speak and act on its own"*. The desktop app reports three things --
// a long command finished, a program asked for the owner, a shell ended
// -- and this decides, with a short turn of the fast model, whether any
// of it is worth a sentence. The model may look closer with the terminal
// tools the app offers; it ends with one sentence for the laptop that
// sent the news -- and, when a command finished with a step plainly
// next, one question offering it, which the owner answers aloud -- or
// with nothing.
//
// Started as the news lands and never waited on: the laptop is answered
// at once, and the reaction runs on its own.
package terminal

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"personal-assistant/internal/app/noticing"
	"personal-assistant/internal/app/tool"
	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/base/records/conversation"
	"personal-assistant/internal/base/thinking/environment"
	"personal-assistant/internal/base/thinking/prompt"
	"personal-assistant/internal/base/world/event"
	"personal-assistant/internal/base/world/notice"
)

// Gap : The least time between two reactions to one terminal.
//
// A terminal that finishes, rings and closes within a few seconds is one
// thing happening, and three sentences about it would be two too many.
// News from the same terminal inside the gap is dropped, and noted as
// held so the owner can see it was.
const Gap = 30 * time.Second

// Rounds : How many times the model may ask for tools before it must
// answer from what it has.
const Rounds = 3

// Timeout : The longest a reaction may take, tools and all. Past this a
// build that finished is old news.
const Timeout = 2 * time.Minute

// MaxConversation : The most of Claude Code's record put in front of the
// model. The app sends about eight thousand characters.
const MaxConversation = 12 << 10

// MaxTail : The most of a terminal's last lines put in front of the
// model, kept from the end. The app sends about forty lines; this only
// stops one very wide line from filling the prompt.
const MaxTail = 8 << 10

// Publisher : Puts an announcement out to the client that caused it.
type Publisher interface {
	Publish(ctx context.Context, from, userID string, kind conversation.Kind, text string) error
}

// Reactor : Decides, per terminal's news, whether to say anything.
//
// Safe for concurrent use. The zero value is not usable; fill the
// fields and use it from one place.
type Reactor struct {
	// Answerer : The model asked. Required.
	Answerer environment.Environment
	// Vendor, Model : Which model -- the fast one, as remarks use.
	Vendor string
	Model  string
	// Voice : The system prompt: their manner, with the assistant's
	// name as configured. Nil sends none.
	Voice func() string
	// Address : How the person is addressed, for describing the tools.
	// Nil leaves the descriptions as written.
	Address func() string
	// Usually : The shape of their ordinary day, from the standing
	// description. Nil or empty leaves it out.
	Usually func(ctx context.Context, userID string) string
	// Tools : The tools their devices offer now. Only those for
	// terminals are offered to the model. Nil offers none.
	Tools func(userID string) []tool.Tool
	// Publish : Where a sentence goes. Required.
	Publish Publisher
	// Notes : Where each decision is written, said or not. Optional.
	Notes notice.Notes
	// Location : Their zone, for the time. Nil is UTC.
	Location *time.Location
	// Now : The clock. Nil is time.Now.
	Now func() time.Time
	// Logger : Required.
	Logger *slog.Logger

	mu      sync.Mutex
	running map[string]bool
	last    map[string]time.Time
	wg      sync.WaitGroup
}

// Wait : Blocks until every reaction started so far has ended. For
// tests, and for shutting down cleanly.
func (r *Reactor) Wait() { r.wg.Wait() }

// Terminal : Starts a reaction to each piece of news, at most one at a
// time and one every Gap for each terminal. Never blocks on the model,
// the store or the network; everything slow runs on its own.
func (r *Reactor) Terminal(ctx context.Context, userID, clientID string, news []*event.Event) {
	// Detached from the request, which ends as soon as the laptop is
	// answered, but keeping what it carries for the log.
	base := context.WithoutCancel(ctx)
	for _, e := range news {
		t, ok := event.TerminalOf(e)
		if !ok {
			continue
		}
		if t.Ordinary() {
			r.Logger.DebugContext(ctx, "a terminal closed in the ordinary way",
				slog.String("terminal", t.Name))
			continue
		}
		at := e.OccurredAt
		key := userID + "\x00" + clientID + "\x00" + t.ID
		if why := r.claim(key); why != "" {
			r.Logger.InfoContext(ctx, "terminal news dropped",
				slog.String("kind", t.Kind), slog.String("terminal", t.Name), slog.String("why", why))
			r.wg.Add(1)
			go func() {
				defer r.wg.Done()
				r.note(base, userID, at, t, notice.Held, "", why)
			}()
			continue
		}
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			defer r.release(key)
			run, cancel := context.WithTimeout(base, Timeout)
			defer cancel()
			r.react(run, userID, clientID, at, t)
		}()
	}
}

// claim : Takes the terminal for one reaction, or says why it cannot.
func (r *Reactor) claim(key string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running == nil {
		r.running, r.last = map[string]bool{}, map[string]time.Time{}
	}
	now := r.now()
	// Forgotten once past the gap, so the map holds only terminals
	// heard from in the last half minute.
	for k, at := range r.last {
		if now.Sub(at) >= Gap && !r.running[k] {
			delete(r.last, k)
		}
	}
	if r.running[key] {
		return "already reacting to this terminal"
	}
	if at, ok := r.last[key]; ok && now.Sub(at) < Gap {
		return "reacted to this terminal " + strconv.Itoa(int(now.Sub(at).Seconds())) + "s ago"
	}
	r.running[key], r.last[key] = true, now
	return ""
}

// release : Lets the terminal have its next reaction, once the gap allows.
func (r *Reactor) release(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.running, key)
}

func (r *Reactor) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// react : One reaction: ask, look if it wants to, then say or stay quiet.
func (r *Reactor) react(ctx context.Context, userID, clientID string, at time.Time, t event.Terminal) {
	ctx = environment.Labelled(ctx, environment.Label{
		UserID: userID, Feature: environment.FeatureTerminal, Detail: detail(t),
	})
	tools := r.terminalTools(userID)
	var usually string
	if r.Usually != nil {
		usually = r.Usually(ctx, userID)
	}
	ask := Asking(t, at, r.now(), r.Location, usually, names(tools))

	answer, err := r.converse(ctx, userID, clientID, ask, tools)
	if err != nil {
		r.Logger.WarnContext(ctx, "cannot react to a terminal",
			slog.String("terminal", t.Name), slog.Any("error", err))
		r.note(ctx, userID, at, t, notice.Failed, "", err.Error())
		return
	}
	if prompt.Nothing(answer) {
		r.Logger.InfoContext(ctx, "a terminal's news was not worth saying",
			slog.String("kind", t.Kind), slog.String("terminal", t.Name))
		r.note(ctx, userID, at, t, notice.Declined, "", "not worth saying")
		return
	}
	said := strings.TrimSpace(answer)
	if err := r.Publish.Publish(ctx, clientID, userID, conversation.Immediate, said); err != nil {
		r.Logger.ErrorContext(ctx, "cannot announce a terminal's news",
			slog.String("terminal", t.Name), slog.Any("error", err))
		r.note(ctx, userID, at, t, notice.Failed, said, err.Error())
		return
	}
	r.Logger.InfoContext(ctx, "a terminal's news was announced",
		slog.String("kind", t.Kind), slog.String("terminal", t.Name))
	r.note(ctx, userID, at, t, notice.Said, said, "")
}

// note : Writes down what was decided. A record that cannot be written is
// logged; the reaction stands either way.
func (r *Reactor) note(ctx context.Context, userID string, at time.Time, t event.Terminal,
	outcome notice.Outcome, said, because string) {
	if r.Notes == nil {
		return
	}
	err := r.Notes.Note(ctx, notice.Noted{
		UserID:  userID,
		At:      at,
		Label:   cut(t.Kind+" "+cmp.Or(t.Name, t.ID), 160),
		Reason:  notice.Terminal,
		Outcome: outcome,
		Said:    said,
		Because: cut(because, 255),
	})
	if err != nil {
		r.Logger.WarnContext(ctx, "cannot write down a terminal's reaction", slog.Any("error", err))
	}
}

// terminalTools : The tools for terminals the person's devices offer now,
// to look with and never to type with.
//
// Only those. The app offers more -- opening apps and spaces -- and a
// reaction nobody asked for has no business rearranging the laptop. Nor
// typing: the app types into Claude Code without asking the owner, as it
// should when they asked, and a reaction nobody asked for must not answer
// Claude Code's questions for them -- made certain here, not left to the
// prompt.
func (r *Reactor) terminalTools(userID string) []tool.Tool {
	if r.Tools == nil {
		return nil
	}
	var out []tool.Tool
	for _, t := range r.Tools(userID) {
		if strings.Contains(t.Name, "terminal") && !strings.HasPrefix(t.Name, "type_") && t.Run != nil {
			out = append(out, t)
		}
	}
	return out
}

// converse : Asks, runs the tools it asks for, and asks again; Rounds
// times at most, then once more with no tools so it has to answer.
func (r *Reactor) converse(ctx context.Context, userID, clientID, ask string, tools []tool.Tool) (string, error) {
	specs, byName, err := r.specs(tools)
	if err != nil {
		return "", err
	}
	var system string
	if r.Voice != nil {
		system = r.Voice()
	}
	var (
		history []environment.Turn
		ran     []string
		next    = ask
	)
	for round := 0; ; round++ {
		offer := specs
		if round >= Rounds {
			offer = nil
		}
		stream, err := r.Answerer.Run(ctx, environment.Request{
			Prompt:       next,
			History:      history,
			Purpose:      environment.PurposeRemark,
			Vendor:       r.Vendor,
			Model:        r.Model,
			SystemPrompt: system,
			Tools:        offer,
		})
		if err != nil {
			return "", err
		}
		// Drained to the end, so the provider is never left blocked.
		var last *environment.Message
		for msg := range stream {
			m := msg
			last = &m
		}
		switch {
		case last == nil:
			return "", fmt.Errorf("the provider said nothing")
		case last.Kind == environment.KindError:
			return "", fmt.Errorf("%s", last.Text)
		case last.Kind != environment.KindToolCalls:
			return last.Text, nil
		case round >= Rounds:
			return "", fmt.Errorf("asked for tools %d times without answering", round+1)
		}

		if next != "" {
			history = append(history, environment.Turn{Role: environment.RoleUser, Text: next})
			next = ""
		}
		history = append(history, environment.Turn{Role: environment.RoleAssistant, ToolCalls: last.ToolCalls})
		results := make([]environment.ToolResult, 0, len(last.ToolCalls))
		for _, c := range last.ToolCalls {
			out := tool.Failed("There is no tool called " + c.Name + " here.")
			if t, ok := byName[c.Name]; ok {
				out = t.Run(ctx, tool.Invocation{
					Caller: tool.Caller{UserID: userID, ClientID: clientID, Channel: chat.ChannelDirect},
					Args:   json.RawMessage(c.Arguments),
					Ran:    ran,
				})
			}
			ran = append(ran, c.Name)
			r.Logger.InfoContext(ctx, "a terminal's reaction called a tool",
				slog.String("tool", c.Name), slog.String("outcome", string(out.Outcome)))
			results = append(results, environment.ToolResult{ID: c.ID, Content: out.Content})
		}
		history = append(history, environment.Turn{Role: environment.RoleTool, ToolResults: results})
	}
}

// specs : The tools as the model is shown them, and by name for running.
func (r *Reactor) specs(tools []tool.Tool) ([]environment.ToolSpec, map[string]tool.Tool, error) {
	if len(tools) == 0 {
		return nil, nil, nil
	}
	var address string
	if r.Address != nil {
		address = r.Address()
	}
	specs := make([]environment.ToolSpec, 0, len(tools))
	byName := make(map[string]tool.Tool, len(tools))
	for _, t := range tools {
		schema, err := tool.Narrated(t.Params, address).MarshalJSON()
		if err != nil {
			return nil, nil, fmt.Errorf("the schema for %s will not render: %w", t.Name, err)
		}
		specs = append(specs, environment.ToolSpec{
			Name:        t.Name,
			Description: tool.NarratedDescription(t.Description(), address),
			Parameters:  schema,
		})
		byName[t.Name] = t
	}
	return specs, byName, nil
}

// Asking : What the model is asked about one terminal's news.
//
// Exported so a test, and anybody reading, can see the whole of it.
func Asking(t event.Terminal, at, now time.Time, where *time.Location, usually string, tools []string) string {
	if where == nil {
		where = time.UTC
	}

	facts := []string{"What happened: " + happened(t)}
	add := func(label, value string) {
		if strings.TrimSpace(value) != "" {
			facts = append(facts, label+": "+value)
		}
	}
	add("Terminal", t.Name)
	add("Space", t.Space)
	add("Folder", t.Folder)
	add("Command", t.Command)
	if t.Seconds > 0 {
		add("It ran for", lasted(t.Seconds))
	}
	if t.Exit != nil {
		add("Exit code", strconv.Itoa(*t.Exit))
	}
	add("The notification said", strings.Join(strings.Fields(t.Text), " "))
	if !at.IsZero() {
		add("When", at.In(where).Format("3:04 pm"))
	}
	add("Now", now.In(where).Format("Monday 2 January 2006, 3:04 pm"))

	output := "It showed no output."
	if tail := tailOf(t.Tail); tail != "" {
		output = prompt.Lines(
			"Its last lines of output, with anything like a key, a token or a password already hidden as ••••:",
			tail)
	}

	var record string
	if t.Conversation != "" {
		record = prompt.Block(
			prompt.Text(
				"It is Claude Code, and this is its own record of the conversation, newest last, secrets hidden: what they asked of it, what it did and what it said.",
				"If it is asking them something -- a question at the end of what it said, or a choice on its screen -- say plainly what it asks and which answer you would choose, with the reason in a few words, judged from this record.",
				"If the record does not settle it, say what it hinges on. The choice stays theirs: you are advising, not answering it.",
			),
			cut(t.Conversation, MaxConversation),
		)
	}

	var ordinary string
	if strings.TrimSpace(usually) != "" {
		ordinary = prompt.Block(
			prompt.Text(
				"This is what an ordinary day looks like for them, worked out from weeks of their own devices and their own words.",
				"Use it only to judge whether this is a moment to be interrupted; never mention it.",
			),
			strings.TrimSpace(usually),
		)
	}

	var looking string
	if len(tools) > 0 {
		looking = prompt.Text(
			"You may look closer before deciding, with these tools: "+strings.Join(tools, ", ")+".",
			"Read more of this terminal, or see what else is open, when what is in front of you does not settle it; you have a few looks at most.",
			"You cannot type into a terminal from here. Where a program is asking -- Claude Code's choices, a yes or no, a password -- that answer is theirs; tell them it is waiting.",
		)
	}

	// Owner, 17 October 2026: *"when the terminal process is done it
	// should announce and ask a follow-up proactive question"*. The
	// question is answered aloud with no wake word. Only what the
	// assistant can do is offered: every terminal may be read, and only
	// Claude Code typed into (owner, same day: *"it can read all
	// terminals but can write only to Claude terminal"*) -- so never a
	// shell command to run.
	offering := prompt.Text(
		"When a command has finished and there is an obvious next step you could take for them, end with one short question offering it.",
		"Offer only what you can do: explain an error or what the output means, look closer at a terminal, or -- where Claude Code is open in one -- ask Claude Code to do it. You read every terminal but type only into Claude Code: never offer to run a command in a shell; a command they should run, say plainly for them to run.",
		"They answer aloud, without your name, so offer one thing only, said so it cannot be read two ways.",
		"No question when no step follows naturally, and never one offering to answer a program's question for them.",
	)

	return prompt.Block(
		prompt.Text(
			"Something has just happened in one of the terminals on their laptop, in the desktop app.",
			"You are deciding whether to tell them, and then, only if you are, writing the one sentence they will hear.",
			"The deciding is the part that matters. Saying nothing is always available, costs them nothing, and is right whenever hearing it would not change what they do next.",
		),
		prompt.Lines(facts...),
		output,
		record,
		prompt.Text(
			"Usually worth saying: a command that ran a while and finished -- they asked to hear whenever one is done, even in the terminal they are looking at -- with whether it worked and, when it did not, the gist of why in a few words;",
			"a program waiting for them, such as Claude Code asking something or done with its work;",
			"a server or other long-running program that stopped, or a shell that ended with an error.",
		),
		prompt.Text(
			"Usually not worth saying: a quick or routine command, output that only scrolled past,",
			"and anything whose meaning you cannot tell from what is in front of you.",
		),
		ordinary,
		looking,
		offering,
		noticing.Interrupting,
		prompt.Text(
			"Say it as one short spoken sentence in your own manner: what happened and where, by the terminal's or the space's name, then the one thing worth knowing; then the question, if you are offering a step.",
			"Never read out a path, a command line, a hidden value or an error word for word; say what it means. A short command you offer to run is the one exception.",
			"Say only what is in front of you or what you looked up, and nothing you have worked out around it.",
		),
		prompt.Text("Answer with the sentence, and the question if there is one, and nothing else.",
			"If it is not worth saying, answer with "+prompt.Silence+"."),
	)
}

// happened : The news in words.
func happened(t event.Terminal) string {
	switch t.Kind {
	case event.TerminalFinished:
		return "a command that had been running for a while has finished."
	case event.TerminalAsks:
		return "a program in it is asking for them -- it rang the terminal's bell or sent a notification."
	case event.TerminalEnded:
		return "its shell has exited."
	}
	return t.Kind
}

// lasted : A number of seconds as a person would say it.
func lasted(seconds float64) string {
	s := int(seconds + 0.5)
	h, m, sec := s/3600, s%3600/60, s%60
	part := func(n int, unit string) string {
		if n == 0 {
			return ""
		}
		if n == 1 {
			return "1 " + unit
		}
		return strconv.Itoa(n) + " " + unit + "s"
	}
	out := strings.Join(strings.Fields(part(h, "hour")+" "+part(m, "minute")+" "+part(sec, "second")), " ")
	if out == "" {
		return "under a second"
	}
	return out
}

// tailOf : The last lines, at most MaxTail bytes of them, from the end.
func tailOf(tail string) string {
	tail = strings.TrimRight(tail, " \n\t")
	if len(tail) <= MaxTail {
		return tail
	}
	tail = tail[len(tail)-MaxTail:]
	for len(tail) > 0 && !utf8.RuneStart(tail[0]) {
		tail = tail[1:]
	}
	if i := strings.IndexByte(tail, '\n'); i >= 0 {
		tail = tail[i+1:]
	}
	return tail
}

// detail : What the usage page shows beside the feature: the terminal and
// what it did.
func detail(t event.Terminal) string {
	return cut(cmp.Or(t.Name, t.ID)+", "+strings.TrimPrefix(t.Kind, "terminal."), 80)
}

// names : The tools' names, for telling the model what it has.
func names(tools []tool.Tool) []string {
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		out = append(out, t.Name)
	}
	return out
}

// cut : s, at most n characters.
func cut(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
