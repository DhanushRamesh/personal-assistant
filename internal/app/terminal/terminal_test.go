package terminal_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"personal-assistant/internal/app/terminal"
	"personal-assistant/internal/app/tool"
	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/base/records/conversation"
	"personal-assistant/internal/base/thinking/environment"
	"personal-assistant/internal/base/thinking/prompt"
	"personal-assistant/internal/base/world/event"
	"personal-assistant/internal/base/world/notice"
)

// model : A model that answers each request with what answer says, and
// remembers what it was asked and how it was labelled.
type model struct {
	mu       sync.Mutex
	answer   func(n int, req environment.Request) environment.Message
	asked    []environment.Request
	labels   []environment.Label
	released chan struct{}
}

func (m *model) Name() string { return "test" }

func (m *model) Run(ctx context.Context, req environment.Request) (<-chan environment.Message, error) {
	if m.released != nil {
		<-m.released
	}
	m.mu.Lock()
	m.asked = append(m.asked, req)
	l, _ := environment.LabelOf(ctx)
	m.labels = append(m.labels, l)
	n := len(m.asked)
	m.mu.Unlock()
	ch := make(chan environment.Message, 1)
	ch <- m.answer(n, req)
	close(ch)
	return ch, nil
}

func (m *model) calls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.asked)
}

func saying(text string) func(int, environment.Request) environment.Message {
	return func(int, environment.Request) environment.Message { return environment.Final(text) }
}

// published : The announcements put out.
type published struct {
	mu  sync.Mutex
	out []string
}

func (p *published) Publish(_ context.Context, from, userID string, kind conversation.Kind, text string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.out = append(p.out, from+"|"+userID+"|"+string(kind)+"|"+text)
	return nil
}

// notes : What was written down.
type notes struct {
	mu   sync.Mutex
	kept []notice.Noted
}

func (n *notes) Note(_ context.Context, x notice.Noted) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.kept = append(n.kept, x)
	return nil
}

// clock : A time the test moves, read by the reactions as they run.
type clock struct {
	mu sync.Mutex
	at time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.at }

func (c *clock) move(by time.Duration) { c.mu.Lock(); defer c.mu.Unlock(); c.at = c.at.Add(by) }

func reactor(m *model, clock *clock) (*terminal.Reactor, *published, *notes) {
	p, n := &published{}, &notes{}
	return &terminal.Reactor{
		Answerer: m,
		Model:    "fast",
		Voice:    func() string { return "the manner" },
		Publish:  p,
		Notes:    n,
		Now:      clock.now,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, p, n
}

func news(kind, id, extra string) *event.Event {
	return &event.Event{
		Kind:       kind,
		OccurredAt: time.Date(2026, 10, 17, 9, 30, 0, 0, time.UTC),
		Payload:    json.RawMessage(`{"terminal":"` + id + `","name":"Server","space":"Personal assistant"` + extra + `}`),
	}
}

// A sentence goes to the laptop that sent the news, as an immediate
// announcement, labelled for what it cost, and is written down as said.
func TestASentenceIsAnnouncedToTheLaptopThatSentIt(t *testing.T) {
	clock := &clock{at: time.Date(2026, 10, 17, 9, 31, 0, 0, time.UTC)}
	m := &model{answer: saying("Your build in Personal assistant finished -- it passed.")}
	r, p, n := reactor(m, clock)

	r.Terminal(context.Background(), "usr_A", "cli_laptop",
		[]*event.Event{news(event.TerminalFinished, "t1", `,"command":"npm run build","seconds":84,"tail":"built in 80s"`)})
	r.Wait()

	if len(p.out) != 1 || p.out[0] != "cli_laptop|usr_A|immediate|Your build in Personal assistant finished -- it passed." {
		t.Fatalf("published %q", p.out)
	}
	if len(n.kept) != 1 || n.kept[0].Outcome != notice.Said || n.kept[0].Reason != notice.Terminal ||
		n.kept[0].Said == "" || n.kept[0].UserID != "usr_A" {
		t.Fatalf("noted %+v", n.kept)
	}
	if m.labels[0].Feature != environment.FeatureTerminal || m.labels[0].UserID != "usr_A" {
		t.Errorf("labelled %+v", m.labels[0])
	}
	if m.asked[0].Model != "fast" || m.asked[0].SystemPrompt != "the manner" {
		t.Errorf("asked %q with %q", m.asked[0].Model, m.asked[0].SystemPrompt)
	}
	if !strings.Contains(m.asked[0].Prompt, "npm run build") || !strings.Contains(m.asked[0].Prompt, "built in 80s") ||
		!strings.Contains(m.asked[0].Prompt, "1 minute 24 seconds") {
		t.Errorf("the prompt left out what happened:\n%s", m.asked[0].Prompt)
	}
}

// Silence is an answer: nothing is announced, and the decision is still
// written down so the owner can see it was made.
func TestNothingWorthSayingAnnouncesNothing(t *testing.T) {
	clock := &clock{at: time.Date(2026, 10, 17, 9, 31, 0, 0, time.UTC)}
	m := &model{answer: saying(prompt.Silence + ".")}
	r, p, n := reactor(m, clock)

	r.Terminal(context.Background(), "usr_A", "cli_laptop",
		[]*event.Event{news(event.TerminalFinished, "t1", `,"command":"ls -la","seconds":11`)})
	r.Wait()

	if len(p.out) != 0 {
		t.Fatalf("published %q", p.out)
	}
	if len(n.kept) != 1 || n.kept[0].Outcome != notice.Declined {
		t.Fatalf("noted %+v", n.kept)
	}
}

// A shell closed cleanly with nothing run is not even asked about.
func TestAShellClosingCleanlyIsNotAskedAbout(t *testing.T) {
	clock := &clock{at: time.Date(2026, 10, 17, 9, 31, 0, 0, time.UTC)}
	m := &model{answer: saying("Your shell closed.")}
	r, p, _ := reactor(m, clock)

	r.Terminal(context.Background(), "usr_A", "cli_laptop",
		[]*event.Event{news(event.TerminalEnded, "t1", `,"exit":0`)})
	r.Wait()
	if m.calls() != 0 || len(p.out) != 0 {
		t.Fatalf("asked %d times, published %q", m.calls(), p.out)
	}

	// After a command it is asked about: something may have stopped.
	r.Terminal(context.Background(), "usr_A", "cli_laptop",
		[]*event.Event{news(event.TerminalEnded, "t2", `,"exit":0,"command":"npm run dev"`)})
	r.Wait()
	if m.calls() != 1 {
		t.Fatalf("asked %d times after a command, want 1", m.calls())
	}
}

// One reaction a terminal every Gap: news inside it is dropped and noted
// as held. Another terminal is its own, and past the gap it is heard again.
func TestATerminalIsReactedToOnceEveryGap(t *testing.T) {
	clock := &clock{at: time.Date(2026, 10, 17, 9, 31, 0, 0, time.UTC)}
	m := &model{answer: saying(prompt.Silence)}
	r, _, n := reactor(m, clock)

	r.Terminal(context.Background(), "usr_A", "cli_laptop", []*event.Event{
		news(event.TerminalFinished, "t1", `,"command":"make"`),
	})
	r.Wait()
	clock.move(terminal.Gap - time.Second)
	r.Terminal(context.Background(), "usr_A", "cli_laptop", []*event.Event{
		news(event.TerminalAsks, "t1", `,"text":"Claude needs your permission"`),
		news(event.TerminalAsks, "t2", ``),
	})
	r.Wait()
	if m.calls() != 2 {
		t.Fatalf("asked %d times, want 2 (t1 once, t2 once)", m.calls())
	}
	held := 0
	for _, x := range n.kept {
		if x.Outcome == notice.Held {
			held++
		}
	}
	if held != 1 {
		t.Errorf("%d noted as held, want 1: %+v", held, n.kept)
	}

	clock.move(2 * time.Second)
	r.Terminal(context.Background(), "usr_A", "cli_laptop", []*event.Event{
		news(event.TerminalAsks, "t1", ``),
	})
	r.Wait()
	if m.calls() != 3 {
		t.Fatalf("asked %d times past the gap, want 3", m.calls())
	}
}

// Only one reaction runs for a terminal at a time, however long it takes.
func TestOneReactionRunsForATerminalAtATime(t *testing.T) {
	clock := &clock{at: time.Date(2026, 10, 17, 9, 31, 0, 0, time.UTC)}
	m := &model{answer: saying(prompt.Silence), released: make(chan struct{})}
	r, _, _ := reactor(m, clock)

	r.Terminal(context.Background(), "usr_A", "cli_laptop", []*event.Event{news(event.TerminalFinished, "t1", ``)})
	// Past the gap, but the first is still running.
	clock.move(terminal.Gap + time.Minute)
	r.Terminal(context.Background(), "usr_A", "cli_laptop", []*event.Event{news(event.TerminalAsks, "t1", ``)})
	close(m.released)
	r.Wait()
	if m.calls() != 1 {
		t.Fatalf("asked %d times, want 1", m.calls())
	}
}

// Terminal starts the reaction and returns: a model that has not
// answered yet holds nothing up.
func TestStartingAReactionDoesNotWaitForIt(t *testing.T) {
	clock := &clock{at: time.Date(2026, 10, 17, 9, 31, 0, 0, time.UTC)}
	m := &model{answer: saying(prompt.Silence), released: make(chan struct{})}
	r, _, _ := reactor(m, clock)

	done := make(chan struct{})
	go func() {
		r.Terminal(context.Background(), "usr_A", "cli_laptop", []*event.Event{news(event.TerminalFinished, "t1", ``)})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Terminal waited for the model")
	}
	close(m.released)
	r.Wait()
}

// The model may look closer with the terminal tools -- only those --
// three rounds at most, and then has to answer with none offered.
func TestTheModelMayLookThreeTimesWithTheReadingTerminalToolsOnly(t *testing.T) {
	clock := &clock{at: time.Date(2026, 10, 17, 9, 31, 0, 0, time.UTC)}
	var readBy []tool.Caller
	read := tool.Tool{Name: "read_terminal", Purpose: "Reads a terminal.", Lists: true,
		Params: tool.Schema{Properties: map[string]tool.Property{"terminal": {Type: "string", Description: "Its name"}}},
		Run: func(_ context.Context, in tool.Invocation) tool.Result {
			readBy = append(readBy, in.Caller)
			return tool.OK("error TS2322 in places.ts")
		}}
	open := tool.Tool{Name: "open_app", Purpose: "Opens an app.", Writes: true,
		Run: func(context.Context, tool.Invocation) tool.Result { return tool.OK("opened") }}

	m := &model{answer: func(n int, req environment.Request) environment.Message {
		if len(req.Tools) > 0 {
			return environment.ToolCalls([]environment.ToolCall{{ID: "c", Name: "read_terminal", Arguments: `{"terminal":"Server"}`}})
		}
		return environment.Final("Your build in Personal assistant failed -- a type error in places.ts.")
	}}
	r, p, _ := reactor(m, clock)
	// Typing is never offered to a reaction nobody asked for: the app types into Claude Code without asking.
	typing := tool.Tool{Name: "type_into_terminal", Purpose: "Types into a terminal.", Writes: true,
		Run: func(context.Context, tool.Invocation) tool.Result { return tool.OK("typed") }}
	r.Tools = func(string) []tool.Tool { return []tool.Tool{read, open, typing} }

	r.Terminal(context.Background(), "usr_A", "cli_laptop", []*event.Event{news(event.TerminalFinished, "t1", ``)})
	r.Wait()

	if m.calls() != terminal.Rounds+1 {
		t.Fatalf("asked %d times, want %d", m.calls(), terminal.Rounds+1)
	}
	for _, s := range m.asked[0].Tools {
		if s.Name == "open_app" || s.Name == "type_into_terminal" {
			t.Errorf("%s was offered to a reaction nobody asked for", s.Name)
		}
	}
	if len(m.asked[0].Tools) != 1 || len(m.asked[terminal.Rounds].Tools) != 0 {
		t.Errorf("offered %d tools first and %d last", len(m.asked[0].Tools), len(m.asked[terminal.Rounds].Tools))
	}
	if len(readBy) != terminal.Rounds || readBy[0].UserID != "usr_A" || readBy[0].ClientID != "cli_laptop" ||
		readBy[0].Channel != chat.ChannelDirect {
		t.Errorf("read %d times as %+v", len(readBy), readBy)
	}
	if len(p.out) != 1 {
		t.Errorf("published %q", p.out)
	}
}

// The prompt carries what happened and the way to stay silent.
func TestTheAskingSaysWhatHappenedAndHowToStayQuiet(t *testing.T) {
	exit := 1
	got := terminal.Asking(event.Terminal{Kind: event.TerminalEnded, ID: "t", Name: "Server", Space: "PAC",
		Folder: "/home/x", Command: "npm run dev", Seconds: 3700, Exit: &exit, Tail: "Error: boom"},
		time.Date(2026, 10, 17, 9, 30, 0, 0, time.UTC), time.Date(2026, 10, 17, 9, 31, 0, 0, time.UTC),
		time.UTC, "", []string{"read_terminal"})
	for _, want := range []string{"Terminal: Server", "Space: PAC", "Folder: /home/x", "Command: npm run dev",
		"1 hour 1 minute 40 seconds", "Exit code: 1", "Error: boom", "Saturday 17 October 2026, 9:31 am",
		"read_terminal", "You cannot type into a terminal from here", "end with one short question offering it",
		"a yes lets you do exactly that", prompt.Silence} {
		if !strings.Contains(got, want) {
			t.Errorf("the prompt does not say %q:\n%s", want, got)
		}
	}
}
