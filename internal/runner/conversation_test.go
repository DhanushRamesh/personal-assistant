package runner_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
	"github.com/DhanushRamesh/personal-assistant/internal/environment"
	"github.com/DhanushRamesh/personal-assistant/internal/runner"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
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

	seen := recorder.history()
	if len(seen) != 2 {
		t.Fatalf("model saw %d turns, want the question and the answer", len(seen))
	}
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

// A cancelled turn leaves its question and a note that it was stopped. The
// question alone would be read by the next turn as something never answered,
// and joined onto whatever is asked next.
//
// This test used to assert the question alone, and passed for the wrong
// reason: the note was being written and silently refused by the store,
// because Valid did not know the kind. The log said so and nothing else did.
func TestACancelledChatLeavesItsQuestionAndAMark(t *testing.T) {
	h := newHarness(t, &environment.Stub{
		Updates: []string{"one", "two", "three"},
		Delay:   30 * time.Millisecond,
	}, runner.Options{})

	tk := h.submit(t, "List three programming languages.")
	h.await(t, tk.ID, chat.StatusRunning)
	h.runner.Cancel(tk.ID)
	h.await(t, tk.ID, chat.StatusCancelled, chat.StatusCompleted, chat.StatusFailed)

	said := h.repo.Said(h.conversation(t))
	if len(said) != 2 {
		t.Fatalf("recorded %v, want the question and the mark", contents(said))
	}
	if said[0].Content != "List three programming languages." {
		t.Errorf("recorded %q", said[0].Content)
	}
	if said[1].Kind != conversation.Interruption {
		t.Errorf("second message is %q, want an interruption", said[1].Kind)
	}
	if said[1].Role != conversation.Assistant {
		t.Errorf("the mark is %q, want the assistant so the roles alternate", said[1].Role)
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
	if p.failWith != "" {
		ch <- environment.Failure(p.failWith, p.failCode, p.failDetail)
	} else {
		ch <- environment.Final("an answer")
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

	tk := h.submit(t, "hello")
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
