package runner_test

import (
	"cmp"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/app/runner"
	"personal-assistant/internal/app/tool"
	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/base/records/conversation"
	"personal-assistant/internal/base/thinking/environment"
)

// contents : What was said in a conversation, as plain strings.
func contents(messages []conversation.Message) []string {
	out := make([]string, len(messages))
	for i, m := range messages {
		out[i] = string(m.Role) + "/" + string(m.Kind) + ": " + m.Content
	}
	return out
}

// Both halves of an exchange are written down, in the order they were said.
func TestAnExchangeIsRecorded(t *testing.T) {
	h := newHarness(t, &environment.Stub{}, runner.Options{})

	tk := h.submit(t, "check my merge requests")
	h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

	said := h.repo.Said(h.conversation(t))
	if len(said) != 2 {
		t.Fatalf("recorded %v, want the question and the answer", contents(said))
	}

	if said[0].Role != conversation.User || said[0].Content != "check my merge requests" {
		t.Errorf("first message = %q by %q, want the question", said[0].Content, said[0].Role)
	}
	if said[1].Role != conversation.Assistant || said[1].Kind != conversation.Chat {
		t.Errorf("second message = %q by %q", said[1].Content, said[1].Role)
	}
	if said[0].Seq != 1 || said[1].Seq != 2 {
		t.Errorf("positions = %d, %d, want 1, 2", said[0].Seq, said[1].Seq)
	}
}

// The question being answered must not also reach the model as the last thing
// said. Sent twice, a model reads a question it has not been asked yet as
// something already discussed.
func TestTheQuestionIsNotAlsoInItsOwnHistory(t *testing.T) {
	recorder := &recordingProvider{}
	h := newHarness(t, recorder, runner.Options{})

	first := h.submit(t, "List three programming languages.")
	h.await(t, first.ID, chat.StatusCompleted, chat.StatusFailed)

	second := h.submit(t, "No, make it four.")
	h.await(t, second.ID, chat.StatusCompleted, chat.StatusFailed)

	for _, turn := range recorder.history() {
		if strings.Contains(turn.Text, "No, make it four.") {
			t.Errorf("the question is repeated in its own history as %q: %s", turn.Role, turn.Text)
		}
	}
}

// The earlier exchange does reach the model, both halves of it, which is the
// whole reason the log exists.
func TestTheEarlierExchangeReachesTheModel(t *testing.T) {
	recorder := &recordingProvider{}
	h := newHarness(t, recorder, runner.Options{})

	first := h.submit(t, "List three programming languages.")
	h.await(t, first.ID, chat.StatusCompleted, chat.StatusFailed)

	second := h.submit(t, "No, make it four.")
	h.await(t, second.ID, chat.StatusCompleted, chat.StatusFailed)

	// Five: the question, the answer, the naming of the conversation,
	// and the clock pair every turn carries after the history. The
	// naming is a turn of its own since 20 October 2026 -- anything the
	// server marks is kept apart from the reply above it, so that what
	// it last said is the last thing and not a paragraph inside one.
	// See conversation.Message.joinable and Runner.clock.
	seen := recorder.history()
	if len(seen) != 5 {
		for i, turn := range seen {
			t.Logf("  turn %d: %-9s %q", i, turn.Role, turn.Text)
		}
		t.Fatalf("model saw %d turns, want the exchange, the naming and the clock", len(seen))
	}
	if !strings.Contains(seen[2].Text, "You named this conversation") {
		t.Errorf("the naming was joined to the answer again: %q", seen[2].Text)
	}
	if !strings.Contains(seen[3].Text, "The time where the person is") {
		t.Errorf("the clock is not the last thing before the question: %q", seen[3].Text)
	}
	seen = seen[:2]
	if seen[0].Role != environment.RoleUser || !strings.Contains(seen[0].Text, "List three") {
		t.Errorf("first turn = %q by %q", seen[0].Text, seen[0].Role)
	}
	if seen[1].Role != environment.RoleAssistant {
		t.Errorf("second turn = %q by %q, want the answer", seen[1].Text, seen[1].Role)
	}
	// The log knows when each was said; the model is not told. Timestamps
	// on the wire made the model echo them back into its answers.
	said := h.repo.Said(h.conversation(t))
	for _, m := range said {
		if m.At.IsZero() {
			t.Errorf("the log did not record when %q was said", m.Role)
		}
	}
}

// A failure is written down, because the person saw it and a correction
// refers to it — but it is never handed back to a model.
func TestAFailureIsRecordedAndSentOnWithItsDetail(t *testing.T) {
	const reason = "The service could not complete the request."
	const detail = "INVALID_OAUTHTOKEN (HTTP 401)"
	recorder := &recordingProvider{
		failWith:   reason,
		failCode:   "unauthorised",
		failDetail: detail,
	}
	h := newHarness(t, recorder, runner.Options{})

	first := h.submit(t, "what is the time")
	h.await(t, first.ID, chat.StatusFailed, chat.StatusCompleted)

	said := h.repo.Said(h.conversation(t))
	if len(said) != 2 || said[1].Kind != conversation.Failure {
		t.Fatalf("recorded %v, want the question then the failure", contents(said))
	}
	if len(conversation.ForPerson(said)) != 2 {
		t.Error("the person is not shown the failure they watched happen")
	}

	recorder.failWith = ""
	second := h.submit(t, "try again")
	h.await(t, second.ID, chat.StatusCompleted, chat.StatusFailed)

	// The failure is given to the model, and with the exact error, so that
	// "what went wrong?" is answerable. Withholding it is what used to make
	// the model invent an explanation for an outage it had no part in.
	var sawDetail bool
	for _, turn := range recorder.history() {
		if strings.Contains(turn.Text, detail) {
			sawDetail = true
		}
	}
	if !sawDetail {
		t.Errorf("the exact error never reached the model: %v", recorder.history())
	}
}

// A turn stopped before anything was said or done is forgotten, question
// and all: nothing ran that the next turn has to reason about, and the
// owner wants it out of the history (17 October 2026).
func TestACancelledChatThatDidNothingIsForgotten(t *testing.T) {
	h := newHarness(t, &environment.Stub{
		Updates: []string{"one", "two", "three"},
		Delay:   30 * time.Millisecond,
	}, runner.Options{})

	tk := h.submit(t, "List three programming languages.")
	h.await(t, tk.ID, chat.StatusRunning)
	h.runner.Cancel(tk.ID)
	h.awaitForgotten(t, tk.ID)

	if said := h.repo.Said(h.conversation(t)); len(said) != 0 {
		t.Fatalf("recorded %v, want nothing", contents(said))
	}
}

// recordingProvider : Answers, and keeps the history it was given so a test
// can check what a model would actually have seen.
type recordingProvider struct {
	failWith   string
	failCode   string
	failDetail string
	seen       []environment.Turn
	system     string
	prompts    []string
	// answer : What it replies with. Empty answers "an answer".
	answer string
	// calls : Tools it asks for on its first reply, then answers.
	calls []string
	asked bool
}

// systemPrompt : What the assistant was last told about itself.
func (p *recordingProvider) systemPrompt() string { return p.system }

func (p *recordingProvider) Name() string { return "recording" }

func (p *recordingProvider) history() []environment.Turn {
	return append([]environment.Turn(nil), p.seen...)
}

func (p *recordingProvider) Run(ctx context.Context, req environment.Request) (<-chan environment.Message, error) {
	// Only what the person asked. Naming and condensing run after the answer
	// and carry no history, so recording them would wipe what is asserted on.
	if req.Purpose == environment.PurposeChat {
		p.seen = append([]environment.Turn(nil), req.History...)
		p.system = req.SystemPrompt
		p.prompts = append(p.prompts, req.Prompt)
	}

	ch := make(chan environment.Message, 1)
	if len(p.calls) > 0 && !p.asked {
		p.asked = true
		want := make([]environment.ToolCall, 0, len(p.calls))
		for i, name := range p.calls {
			want = append(want, environment.ToolCall{
				ID: fmt.Sprintf("c%d", i), Name: name, Arguments: "{}",
			})
		}
		ch <- environment.Message{Kind: environment.KindToolCalls, ToolCalls: want}
		close(ch)
		return ch, nil
	}
	if p.failWith != "" {
		ch <- environment.Failure(p.failWith, p.failCode, p.failDetail)
	} else {
		ch <- environment.Final(cmp.Or(p.answer, "an answer"))
	}
	close(ch)
	return ch, nil
}

// Spoken, a failure says the sentence and not the error. There is no
// "more info" on a speaker, and reading a DNS failure aloud is not an
// answer to that: the detail is kept on the message and given to the
// model, so asking what went wrong reaches it.
func TestAVoiceFailureKeepsTheExactErrorToItself(t *testing.T) {
	const detail = "platformai: Output blocked by content filtering policy (HTTP 400)"

	recorder := &recordingProvider{
		failWith:   "The server would not let me answer that.",
		failCode:   "filtered",
		failDetail: detail,
	}
	h := newHarness(t, recorder, runner.Options{})

	tk := h.submitOn(t, chat.ChannelVoice, "do you know the lyrics of Fireflies")
	done := h.await(t, tk.ID, chat.StatusFailed, chat.StatusCompleted)

	if strings.Contains(done.Error, "content filtering") {
		t.Errorf("spoken failure = %q, want the jargon kept out of the room", done.Error)
	}
	if done.ErrorDetail != detail {
		t.Errorf("detail = %q, want it kept exactly so it can be asked for", done.ErrorDetail)
	}
}

// Typed, the sentence alone. The exact error is under "more info", and a wall
// of service jargon in the transcript buries the part anybody reads.
func TestATypedFailureKeepsTheJargonOutOfTheWay(t *testing.T) {
	const detail = "platformai: Output blocked by content filtering policy (HTTP 400)"

	recorder := &recordingProvider{
		failWith:   "The service would not let me answer that.",
		failCode:   "filtered",
		failDetail: detail,
	}
	h := newHarness(t, recorder, runner.Options{})

	tk := h.submitOn(t, chat.ChannelDirect, "do you know the lyrics of Fireflies")
	done := h.await(t, tk.ID, chat.StatusFailed, chat.StatusCompleted)

	if strings.Contains(done.Error, "content filtering") {
		t.Errorf("shown failure = %q, want the jargon left to more-info", done.Error)
	}
	if done.ErrorDetail != detail {
		t.Errorf("detail = %q, want it kept for when it is asked for", done.ErrorDetail)
	}
}

// A spoken turn is told its words may be misheard. A typed one is not: typing
// means what it says, and reinterpreting a word somebody chose deliberately
// is worse than taking it literally.
func TestOnlyASpokenTurnIsWarnedAboutMishearing(t *testing.T) {
	recorder := &recordingProvider{}
	h := newHarness(t, recorder, runner.Options{})

	spoken := h.submitOn(t, chat.ChannelVoice, "can you unlock any of the two conversations")
	h.await(t, spoken.ID, chatDone...)
	if !strings.Contains(recorder.systemPrompt(), "sounds like") {
		t.Error("a spoken turn was not warned that its words may be misheard")
	}

	typed := h.submitOn(t, chat.ChannelDirect, "can you unlock any of the two conversations")
	h.await(t, typed.ID, chatDone...)
	if strings.Contains(recorder.systemPrompt(), "sounds like") {
		t.Error("a typed turn was warned, and typing means what it says")
	}
}

// TestNamingTheConversationIsInTheConversation : The assistant naming a
// conversation is written into it, so being asked about it can be
// answered.
//
// Asked "why did you rename like that?" the assistant answered that it
// had no record of performing a rename and asked what was meant. It was
// telling the truth: the new name went to the client as an event and
// into the listing, and nowhere a model could read. The owner's rule is
// that whatever the assistant does unasked belongs in the conversation.
func TestNamingTheConversationIsInTheConversation(t *testing.T) {
	recorder := &recordingProvider{}
	h := newHarness(t, recorder, runner.Options{})

	first := h.submit(t, "what is the time now")
	h.await(t, first.ID, chat.StatusCompleted, chat.StatusFailed)

	var note *conversation.Message
	for i, m := range h.repo.Said(h.conversation(t)) {
		if m.Kind == conversation.Renaming {
			note = &h.repo.Said(h.conversation(t))[i]
		}
	}
	if note == nil {
		t.Fatalf("the naming was not written down: %v", contents(h.repo.Said(h.conversation(t))))
	}

	// Shown, and given to the model with what it was: a rename asked
	// about afterwards is a question about why, and "you named this
	// conversation" is the part that answers it.
	forModel := conversation.ForModel([]conversation.Message{*note})
	if len(forModel) != 1 {
		t.Fatalf("the naming was withheld from the model")
	}
	if !strings.Contains(forModel[0].Content, "named this conversation") {
		t.Errorf("the model is not told what happened: %q", forModel[0].Content)
	}
	if len(conversation.ForPerson([]conversation.Message{*note})) != 1 {
		t.Error("the person is not shown the rename they watched happen")
	}
}

// offering : A registry with one tool, so a turn has something to look
// at.
func offering() *tool.Registry {
	r, err := tool.NewRegistry(tool.Tool{
		Name:     "task_list_add",
		Domain:   "task",
		Writes:   true,
		Purpose:  "Start a new to-do list.",
		UseWhen:  "They ask for a new list.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params:   tool.Schema{Properties: map[string]tool.Property{}},
		Run: func(context.Context, tool.Invocation) tool.Result {
			return tool.OK("Started it.")
		},
	})
	if err != nil {
		panic(err)
	}
	return r
}

// TestAnAnswerWithoutToolsIsSentBackToLook : The answer that keeps
// being wrong is "I cannot".
//
// Asked to create a task list, the assistant replied "I am not able to
// create task lists from here, sir -- that needs to be done on your
// device", with task_list_add in the same prompt. It had called
// nothing. The persona already said to check the tools before saying
// no; it did not hold, five times over.
func TestAnAnswerWithoutToolsIsSentBackToLook(t *testing.T) {
	recorder := &recordingProvider{}
	h := newHarness(t, recorder, runner.Options{Tools: offering()})

	tk := h.submit(t, "create a new task list called premium web complaints")
	h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

	if len(recorder.prompts) < 2 {
		t.Fatalf("the answer went out unchecked: %v", recorder.prompts)
	}
	if !strings.Contains(recorder.prompts[1], "did not use any tool") {
		t.Errorf("it was not told what it skipped: %q", recorder.prompts[1])
	}
	if !strings.Contains(recorder.prompts[1], "cannot do something") {
		t.Errorf("it was not told which claim to check: %q", recorder.prompts[1])
	}
}

// TestItIsSentBackOnlyOnce : A model that answered without tools twice
// has made its case, and the rounds are better spent on the answer.
func TestItIsSentBackOnlyOnce(t *testing.T) {
	recorder := &recordingProvider{}
	h := newHarness(t, recorder, runner.Options{Tools: offering()})

	tk := h.submit(t, "what is on my list")
	h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

	if len(recorder.prompts) != 2 {
		t.Errorf("asked %d times, want the question and one look: %v",
			len(recorder.prompts), recorder.prompts)
	}
}

// TestNothingIsSentBackWhenThereAreNoTools : With nothing to look at,
// looking is not a thing that can be asked for.
func TestNothingIsSentBackWhenThereAreNoTools(t *testing.T) {
	recorder := &recordingProvider{}
	h := newHarness(t, recorder, runner.Options{})

	tk := h.submit(t, "hello")
	h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

	if len(recorder.prompts) != 1 {
		t.Errorf("a turn with no tools was sent back: %v", recorder.prompts)
	}
}

// TestAYesToAnOfferIsSentBack : An offer the assistant made itself is
// answered by doing the thing offered, so "okay" to it goes back to look
// at the tools, and then back again for having changed nothing.
//
// The offer arrives as conversation.Immediate, which is how a terminal
// asking for the owner is published, so the question is not in the reply
// history at all.
//
// Three rounds, not two. The second is doWhatWasOffered, which fires
// because this recorder writes nothing. An offer of words alone -- and
// reading the numbers out is one -- is answered the same way a second
// time and goes out as it is, so what it costs is a round and never
// the answer. Correctness over latency, which is the standing trade.
func TestAYesToAnOfferIsSentBack(t *testing.T) {
	recorder := &recordingProvider{}
	h := newHarness(t, recorder, runner.Options{Tools: offering()})

	if _, err := h.repo.Append(context.Background(), conversation.Message{
		ID:             conversation.NewMessageID(),
		ConversationID: h.conversation(t),
		Kind:           conversation.Immediate,
		Role:           conversation.Assistant,
		Content:        "The run has finished, sir -- shall I read the numbers out?",
		At:             time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	tk := h.submit(t, "okay")
	h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

	if len(recorder.prompts) != 3 {
		t.Errorf("asked %d times, want three times: %v", len(recorder.prompts), recorder.prompts)
	}
}

// TestAYesIsToldWhatItAnswers : A bare yes carries no subject, so the
// question it answers goes in front of it, and the send-back to look
// says it again after its own instruction -- which it does not answer.
//
// Owner, 19 October 2026: "yes" typed under "Shall I tell it to move on
// to step 4?" was taken for another terminal, then answered "Understood,
// sir. I will check the tools before claiming..."; the step was never told.
func TestAYesIsToldWhatItAnswers(t *testing.T) {
	recorder := &recordingProvider{}
	h := newHarness(t, recorder, runner.Options{Tools: offering()})
	const offer = "The Server terminal is done, sir. Shall I tell it to move on to step 4?"
	if _, err := h.repo.Append(context.Background(), conversation.Message{
		ID:             conversation.NewMessageID(),
		ConversationID: h.conversation(t),
		Kind:           conversation.Immediate,
		Role:           conversation.Assistant,
		Content:        offer,
		At:             time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	tk := h.submit(t, "yes")
	h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

	// Three: the question in front of the yes, the send-back to look at
	// the tools, and doWhatWasOffered for having changed nothing. This
	// offer is to tell a terminal to move on, so the third is the one
	// that matters -- it is the round where the typing happens.
	if len(recorder.prompts) != 3 {
		t.Fatalf("asked %d times, want three times: %v", len(recorder.prompts), recorder.prompts)
	}
	if got := recorder.prompts[2]; !strings.Contains(got, "they said yes to this") || !strings.Contains(got, offer) {
		t.Errorf("the send-back did not restate the offer: %q", got)
	}
	if got := recorder.prompts[0]; !strings.Contains(got, "saying yes to this") || !strings.Contains(got, offer) || !strings.HasSuffix(got, "yes") {
		t.Errorf("the yes went without its question: %q", got)
	}
	if got := recorder.prompts[1]; !strings.Contains(got, "not from them: do not answer it") || !strings.Contains(got, offer) {
		t.Errorf("the send-back does not say what they asked: %q", got)
	}
}

// TestAReplyChosenIsToldWhatItAnswers : A message chosen in the app is
// what the prompt answers, whatever came last, and whatever was said.
func TestAReplyChosenIsToldWhatItAnswers(t *testing.T) {
	recorder := &recordingProvider{}
	h := newHarness(t, recorder, runner.Options{})
	tk, err := chat.New(h.conversation(t), chat.ChannelDirect, "what did it find?")
	if err != nil {
		t.Fatalf("chat.New: %v", err)
	}
	tk.ReplyTo = "Claude Code in Laptop client has finished, sir."
	if err := h.repo.Create(context.Background(), tk); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := h.runner.Submit(tk); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)
	if got := recorder.prompts[0]; !strings.Contains(got, "replying to this message of yours") || !strings.Contains(got, tk.ReplyTo) {
		t.Errorf("the reply went without its message: %q", got)
	}
}

// TestWhatIsSaidInPassingIsNotSentBack : Thanks or a hello has nothing to
// look up, so its answer goes out as it is, in one round.
func TestWhatIsSaidInPassingIsNotSentBack(t *testing.T) {
	recorder := &recordingProvider{}
	h := newHarness(t, recorder, runner.Options{Tools: offering()})

	tk := h.submit(t, "thanks")
	h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

	if len(recorder.prompts) != 1 {
		t.Errorf("asked %d times, want once: %v", len(recorder.prompts), recorder.prompts)
	}
}

// TestASendBackCarriesTheAnswerItIsAbout : A send-back is a note about
// an answer, so the answer has to be there to note.
//
// It was not. The history went user, user -- the question, then "say
// it shorter" with nothing between them -- so the instruction arrived
// as the person speaking and there was nothing to revise. On 19
// October 2026, after answering only "Sir.", the model was handed
// lookFirst and replied "Understood, sir. I will search before
// claiming that anything has not happened... I will read both lists of
// tools before saying I cannot do something": the instruction read
// back to him, nearly word for word, as though he had asked for a
// promise.
//
// Driven here through the length send-back, with no tools offered so
// that lookFirst cannot fire. lookFirst is the one send-back this
// cannot reach: it is always begun speculatively, 109 times of 109 in
// the log, and a request sent before the answer existed cannot carry
// it. See TestTheLookFirstRoundCannotCarryTheAnswer.
func TestASendBackCarriesTheAnswerItIsAbout(t *testing.T) {
	recorder := &recordingProvider{answer: "One. Two. Three. Four."}
	h := newHarness(t, recorder, runner.Options{})

	tk := h.submitOn(t, chat.ChannelVoice, "how was the traffic")
	h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

	if len(recorder.prompts) < 2 {
		t.Fatalf("a long spoken answer was not sent back: %v", recorder.prompts)
	}

	seen := recorder.history()
	var roles []environment.Role
	for _, turn := range seen {
		roles = append(roles, turn.Role)
	}
	if len(seen) < 2 {
		t.Fatalf("the send-back carried %d turns: %v", len(seen), roles)
	}

	last := seen[len(seen)-1]
	if last.Role != environment.RoleAssistant || last.Text != recorder.answer {
		t.Errorf("the send-back's history ends with %s %q, want the answer it is about: %v",
			last.Role, last.Text, roles)
	}
	if seen[len(seen)-2].Role != environment.RoleUser {
		t.Errorf("the turn before the answer is %s, want the question: %v", seen[len(seen)-2].Role, roles)
	}

	// And two of the person's turns never sit side by side, which is
	// what made an instruction read as something he said.
	for i := 1; i < len(roles); i++ {
		if roles[i] == environment.RoleUser && roles[i-1] == environment.RoleUser {
			t.Errorf("two of the person's turns in a row at %d: %v", i, roles)
			break
		}
	}
}

// reading : A registry whose tools only look, so a result carries where
// it came from.
func reading() *tool.Registry {
	look := func(name, domain, content string) tool.Tool {
		return tool.Tool{
			Name: name, Domain: domain, Lists: true,
			Purpose: "Looks something up.", UseWhen: "They ask about it.",
			Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
			Params:   tool.Schema{Properties: map[string]tool.Property{}},
			Run: func(context.Context, tool.Invocation) tool.Result {
				return tool.OK(content)
			},
		}
	}
	r, err := tool.NewRegistry(
		look("calendar_events", "calendar", "Tuesday 20 October, 9am, the dentist."),
		look("mail_unread", "mail", "Two unread."),
	)
	if err != nil {
		panic(err)
	}
	return r
}

// toolResults : Every tool result the model was shown.
func toolResults(p *recordingProvider) []string {
	var out []string
	for _, turn := range p.history() {
		for _, got := range turn.ToolResults {
			out = append(out, got.Content)
		}
	}
	return out
}

// TestAToolSaysWhereItsAnswerCameFrom : A looked-up answer has to be
// able to say so, and the words reach the model with the result.
//
// Of 353 answers that called a tool, 48 said where anything came from,
// so a looked-up answer and an invented one read as the same sentence.
// Owner, 19 October 2026: "i'm not getting trust on our jarvis ...
// there is something missing". What is missing is being able to check.
func TestAToolSaysWhereItsAnswerCameFrom(t *testing.T) {
	recorder := &recordingProvider{calls: []string{"calendar_events"}}
	h := newHarness(t, recorder, runner.Options{Tools: reading()})

	tk := h.submit(t, "what is on tomorrow")
	h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

	results := toolResults(recorder)
	if len(results) == 0 {
		t.Fatal("no tool result reached the model")
	}
	if !strings.Contains(results[0], "your diary") {
		t.Errorf("the result did not say where it came from:\n%s", results[0])
	}
	// In the person's words, never the machine's: this is the leak
	// persona.NamesMachinery exists to stop, arriving by another route.
	if strings.Contains(results[0], "calendar_events") {
		t.Errorf("the result named the tool to the model as its source:\n%s", results[0])
	}
}

// Said once a turn. Every result is carried back to the model on each
// round after it, so a second telling is twenty words re-read for the
// rest of the turn -- see "One thing said eighteen times".
func TestWhereItCameFromIsSaidOnceATurn(t *testing.T) {
	recorder := &recordingProvider{calls: []string{"calendar_events", "mail_unread"}}
	h := newHarness(t, recorder, runner.Options{Tools: reading()})

	tk := h.submit(t, "what is on, and any mail")
	h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

	said := 0
	for _, content := range toolResults(recorder) {
		if strings.Contains(content, "Say where this came from") {
			said++
		}
	}
	if said != 1 {
		t.Errorf("where it came from was said %d times in one turn, want once", said)
	}
}

// TestAYesThatOnlyReadsIsSentBack : Reading is not doing.
//
// On 19 October 2026 the assistant offered "Shall I tell it to keep
// going?", he said yes, and the turn called read_terminal twice and
// reported the status. The first draft was "Yes, sir."
//
// Everything that should have caught it was already there and did not:
// conversation.SayingYesTo put the offer in front of the model with
// "Do what it offered, now, with the terminal or the thing it names",
// the look-first send-back fired, and the model went and looked. What
// was missing was anything that could tell looking from doing.
func TestAYesThatOnlyReadsIsSentBack(t *testing.T) {
	recorder := &recordingProvider{calls: []string{"thing_list"}}
	h := newHarness(t, recorder, runner.Options{Tools: offering()})
	const offer = "The Server terminal is done, sir. Shall I tell it to keep going?"
	if _, err := h.repo.Append(context.Background(), conversation.Message{
		ID:             conversation.NewMessageID(),
		ConversationID: h.conversation(t),
		Kind:           conversation.Immediate,
		Role:           conversation.Assistant,
		Content:        offer,
		At:             time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	tk := h.submit(t, "yes")
	h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

	// A tool ran, so the look-first guard is satisfied and would let
	// this out. It read, and nothing was changed.
	var sent string
	for _, p := range recorder.prompts {
		if strings.Contains(p, "Reading something is not doing it") {
			sent = p
		}
	}
	if sent == "" {
		t.Fatalf("a yes answered by reading went out unchallenged: %v", recorder.prompts)
	}
	if !strings.Contains(sent, offer) {
		t.Errorf("the send-back did not say what had been offered: %q", sent)
	}
	// The way out is said, so an offer of words alone is not trapped.
	if !strings.Contains(sent, "it will be sent as it is") {
		t.Errorf("the send-back left no way out: %q", sent)
	}
}

// TestAnOfferToDoSomethingIsSentBack : Asking leave is not an answer.
//
// The persona has forbidden it from the start -- "Never end a reply
// with a question or an offer of further help. Where you would ask
// permission, say what you are about to do instead" -- and on 19
// October 2026 it ended a report with "Shall I tell it to keep going?".
// He said yes, and the yes then had to be recognised, carried and
// enforced by three separate pieces of machinery.
//
// Caught here instead, one round earlier, where it costs nothing.
func TestAnOfferToDoSomethingIsSentBack(t *testing.T) {
	recorder := &recordingProvider{
		answer: "The Server terminal is done, sir. Shall I tell it to keep going?",
	}
	h := newHarness(t, recorder, runner.Options{Tools: offering()})

	tk := h.submit(t, "how is the server")
	h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

	var sent string
	for _, p := range recorder.prompts {
		if strings.Contains(p, "it ends by offering to do something") {
			sent = p
		}
	}
	if sent == "" {
		t.Fatalf("an answer that offered instead of doing went out: %v", recorder.prompts)
	}
	// The question the persona does want is left open, since a sentence
	// cannot say whether what it offers can be undone.
	if !strings.Contains(sent, "cannot be taken back") {
		t.Errorf("the send-back left no room for a question that is owed: %q", sent)
	}
	// Once. Saying the same thing again sends it.
	var times int
	for _, p := range recorder.prompts {
		if strings.Contains(p, "it ends by offering to do something") {
			times++
		}
	}
	if times != 1 {
		t.Errorf("sent back %d times, want once: %v", times, recorder.prompts)
	}
}

// TestAnHonestQuestionStillGoesOut : A question about what they meant
// is not an offer, and is wanted.
func TestAnHonestQuestionStillGoesOut(t *testing.T) {
	recorder := &recordingProvider{
		answer: "The diary has two of these, sir. Did you mean the Tuesday one?",
	}
	h := newHarness(t, recorder, runner.Options{Tools: offering()})

	tk := h.submit(t, "move the dentist")
	h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

	for _, p := range recorder.prompts {
		if strings.Contains(p, "it ends by offering to do something") {
			t.Fatalf("an honest question was sent back as an offer: %v", recorder.prompts)
		}
	}
}

// TestAServerNoteIsOffBeforeTheShapeIsJudged : The "hi" turn.
//
// He typed "hi" on 20 October 2026 and was answered with six bullets
// of how every answer would be shaped for the screen.
//
// The model had replied "Good morning, sir." and copied the server's
// own marker under it -- "[Said unasked at 12:52 am, news from their
// devices ...]", which decorates an announcement in the history so the
// model can tell one from a reply. Two sentences and a long bracket
// read as prose, so the shape guard sent it back to be put in points,
// and the round that came back answered the note instead of him.
//
// Every piece worked. conversation.Unmarked takes that marker off, and
// would have; it ran at the end, after the shape had been judged on
// text the server was about to remove.
func TestAServerNoteIsOffBeforeTheShapeIsJudged(t *testing.T) {
	recorder := &recordingProvider{
		answer: "Good morning, sir.\n\n[Said unasked at 12:52 am, news from their devices -- " +
			"a record of what you said then, never a way to begin an answer] The laptop " +
			"charged to full overnight and the office calendar has nothing before eleven.",
	}
	h := newHarness(t, recorder, runner.Options{Tools: offering()})

	tk := h.submit(t, "hi")
	h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

	// Asked once. The marker is gone before the shape is judged, so
	// there is nothing to send back for.
	if len(recorder.prompts) != 1 {
		t.Errorf("a greeting was sent back to be reshaped: %v", recorder.prompts)
	}
	kept := h.repo.Said(h.conversation(t))
	if len(kept) < 2 {
		t.Fatalf("recorded %v, want the question and the answer", contents(kept))
	}
	said := kept[len(kept)-1].Content
	if strings.Contains(said, "Said unasked") || strings.Contains(said, "[") {
		t.Errorf("the server's own note reached him: %q", said)
	}
	if !strings.Contains(said, "Good morning") {
		t.Errorf("the answer itself was lost with the note: %q", said)
	}
}

// TestTheSubjectIsWhateverItSaidLast : An announcement is something it
// said, and what he says next answers it.
//
// On 20 October 2026 it announced that Claude Code had drawn a layout
// on the canvas and wanted approval. He answered "you didn't even look
// at" and was told how long he had slept the night before -- which was
// the subject of the last reply, two announcements earlier.
//
// lastSaid takes replies only, and the subject handed over was an
// answer he had stopped talking about. lastAloud takes whatever it
// last said, which is the distinction the runner already drew for a
// yes to an offer and had not drawn here.
func TestTheSubjectIsWhateverItSaidLast(t *testing.T) {
	recorder := &recordingProvider{answer: "Which did you mean, sir?"}
	h := newHarness(t, recorder, runner.Options{Tools: offering()})
	conv := h.conversation(t)

	for _, m := range []conversation.Message{
		{Kind: conversation.Chat, Content: "You typically sleep around 2:30 am, sir."},
		{Kind: conversation.Immediate, Content: "Your battery is running low, sir -- nineteen percent."},
		{Kind: conversation.Immediate, Content: "Claude Code has drawn the new usage layout on the canvas and wants approval."},
	} {
		m.ID, m.ConversationID, m.Role, m.At = conversation.NewMessageID(), conv, conversation.Assistant, time.Now().UTC()
		if _, err := h.repo.Append(context.Background(), m); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	tk := h.submit(t, "you didn't even look at")
	h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

	var sent string
	for _, p := range recorder.prompts {
		if strings.Contains(p, "The subject is what you yourself said last") {
			sent = p
		}
	}
	if sent == "" {
		t.Fatalf("the subject was never handed over: %v", recorder.prompts)
	}
	if !strings.Contains(sent, "drawn the new usage layout") {
		t.Errorf("the subject handed over was not the last thing it said: %q", sent)
	}
	if strings.Contains(sent, "sleep around 2:30") {
		t.Errorf("the subject was a reply he had stopped talking about: %q", sent)
	}
}

// TestAskingWhatToolsExistIsNotLookingAnythingUp : The call duration
// turn.
//
// He asked how long a call had lasted and was told "your devices do
// not record call duration, sir". They do: the event carried
// seconds 465, and the two turns before it had read those same events.
//
// What happened in between is that the model called tool_describe,
// found nothing named for durations, and answered from that. Asking
// what tools exist counted as having looked, so the guard that insists
// on looking had nothing to say.
func TestAskingWhatToolsExistIsNotLookingAnythingUp(t *testing.T) {
	recorder := &recordingProvider{
		calls:  []string{tool.DescribeName},
		answer: "Your devices do not record call duration, sir.",
	}
	h := newHarness(t, recorder, runner.Options{Tools: offering()})

	tk := h.submit(t, "how long did I talk to her")
	h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

	var sent bool
	for _, p := range recorder.prompts {
		if strings.Contains(p, "you did not use any tool this turn") {
			sent = true
		}
	}
	if !sent {
		t.Errorf("a turn that only asked what tools exist counted as having looked: %v", recorder.prompts)
	}
}
