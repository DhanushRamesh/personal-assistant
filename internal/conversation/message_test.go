package conversation_test

import (
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
)

// said : A message of the given role and length, so a test can say how much
// budget it consumes without writing out the text.
func said(role conversation.Role, text string) conversation.Message {
	return conversation.Message{Role: role, Content: text, At: time.Now()}
}

// Joining two questions keeps the time of the earlier one: that is when the
// speaker started saying all of it.
func TestForModelKeepsTheEarlierTime(t *testing.T) {
	first := time.Date(2026, 9, 22, 9, 0, 0, 0, time.UTC)
	second := first.Add(time.Minute)
	messages := []conversation.Message{
		{Kind: conversation.Chat, Role: conversation.User, Content: "list three languages", At: first},
		{Kind: conversation.Chat, Role: conversation.User, Content: "no, make it four", At: second},
	}

	joined := conversation.ForModel(messages)

	if len(joined) != 1 {
		t.Fatalf("joined into %d messages, want one", len(joined))
	}
	if !joined[0].At.Equal(first) {
		t.Errorf("time = %v, want the earlier %v", joined[0].At, first)
	}
	if !strings.Contains(joined[0].Content, "make it four") {
		t.Errorf("the correction was lost: %q", joined[0].Content)
	}
}

// A failure is shown to the person but never sent back to a model: read as
// conversation it becomes the model explaining an outage it had no part in.
func TestForModelIsGivenFailuresWithTheirDetail(t *testing.T) {
	messages := []conversation.Message{
		{Kind: conversation.Chat, Role: conversation.User, Content: "what is the time"},
		{
			Kind:    conversation.Failure,
			Role:    conversation.Assistant,
			Content: "The service could not complete the request.",
			Detail:  "INVALID_OAUTHTOKEN (HTTP 401)",
		},
		{Kind: conversation.Chat, Role: conversation.User, Content: "what exactly went wrong"},
	}

	forModel := conversation.ForModel(messages)

	// Three, not two joined into one: the failure separates the questions, so
	// the model can see that the first was answered with an outage and that
	// the second is asking about it.
	if len(forModel) != 3 {
		t.Fatalf("model saw %d messages, want all three", len(forModel))
	}
	if !strings.Contains(forModel[1].Content, "INVALID_OAUTHTOKEN") {
		t.Errorf("the exact error was withheld from the model: %q", forModel[1].Content)
	}

	// The person is shown the sentence and not the detail; the detail is
	// carried beside it for a client to reveal when asked.
	shown := conversation.ForPerson(messages)
	if len(shown) != 3 {
		t.Fatalf("the person saw %d messages, want all three", len(shown))
	}
	if strings.Contains(shown[1].Content, "INVALID_OAUTHTOKEN") {
		t.Errorf("the raw error leaked into what is read aloud: %q", shown[1].Content)
	}
	if shown[1].Detail != "INVALID_OAUTHTOKEN (HTTP 401)" {
		t.Errorf("detail = %q, want it kept beside the sentence", shown[1].Detail)
	}
}

// An answer that came back blank is not part of the conversation.
func TestForModelDropsEmptyMessages(t *testing.T) {
	messages := []conversation.Message{
		{Kind: conversation.Chat, Role: conversation.User, Content: "hello"},
		{Kind: conversation.Chat, Role: conversation.Assistant, Content: "   "},
		{Kind: conversation.Chat, Role: conversation.User, Content: "still there?"},
	}

	if got := len(conversation.ForModel(messages)); got != 1 {
		t.Errorf("kept %d messages, want the two questions joined into one", got)
	}
}

func TestForModelKeepsAnInterruption(t *testing.T) {
	messages := []conversation.Message{
		{Kind: conversation.Chat, Role: conversation.User, Content: "add rice to the list"},
		{Kind: conversation.Interruption, Role: conversation.Assistant, Content: "[stopped]"},
		{Kind: conversation.Chat, Role: conversation.User, Content: "what did you get done"},
	}

	forModel := conversation.ForModel(messages)

	// Three messages, not two joined into one: the interruption sits between
	// the questions and keeps them apart, which is the whole point. A model
	// that saw them joined would read one question and never learn that the
	// first was cut off.
	if len(forModel) != 3 {
		t.Fatalf("model saw %d messages, want all three", len(forModel))
	}
	if forModel[1].Kind != conversation.Interruption {
		t.Errorf("the interruption did not reach the model: %+v", forModel[1])
	}
}

func TestInterruptedIsTheAssistantsTurn(t *testing.T) {
	at := time.Date(2026, 9, 25, 13, 0, 0, 0, time.UTC)

	m := conversation.Interrupted("ses_1", "", at)

	// Written as the assistant so the roles still alternate. As the user it
	// would join onto the question before it and read as part of what was
	// asked.
	if m.Role != conversation.Assistant {
		t.Errorf("role = %q, want the assistant", m.Role)
	}
	if m.Kind != conversation.Interruption {
		t.Errorf("kind = %q, want an interruption", m.Kind)
	}
	if strings.TrimSpace(m.Content) == "" {
		t.Error("an interruption with no content would be dropped by ForModel")
	}
}

// Every kind this package can build has to be one the store accepts. They
// were listed in two places, and an interruption was rejected for a morning
// because only one of them learned about it.
func TestEveryConstructedMessageIsValid(t *testing.T) {
	at := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

	for name, m := range map[string]conversation.Message{
		"said":        conversation.Said("ses_1", "what is the time", at),
		"answered":    conversation.Answered("ses_1", "half past two", at),
		"failed":      conversation.Failed("ses_1", "it did not work", "HTTP 500", at),
		"interrupted": conversation.Interrupted("ses_1", "", at),
	} {
		if err := m.Valid(); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// A position is not an identity. seq orders the conversation and would move
// if anything were ever removed from the middle of one; the identifier names
// the same message afterwards.
func TestEveryMessageGetsItsOwnIdentifier(t *testing.T) {
	at := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	seen := map[string]bool{}

	for _, m := range []conversation.Message{
		conversation.Said("ses_1", "one", at),
		conversation.Said("ses_1", "one", at),
		conversation.Answered("ses_1", "two", at),
		conversation.Failed("ses_1", "three", "detail", at),
		conversation.Interrupted("ses_1", "", at),
	} {
		if !strings.HasPrefix(m.ID, conversation.MessageIDPrefix) {
			t.Errorf("id = %q, want the %s prefix", m.ID, conversation.MessageIDPrefix)
		}
		if seen[m.ID] {
			t.Errorf("identifier handed out twice: %s", m.ID)
		}
		seen[m.ID] = true
	}
}

func TestAMessageWithoutAnIdentifierIsRefused(t *testing.T) {
	m := conversation.Said("ses_1", "hello", time.Now())
	m.ID = ""

	if err := m.Valid(); err == nil {
		t.Error("a message with no identifier was accepted")
	}
}

// What a tool returned in an earlier turn is taken out of the window.
//
// It was true when it ran and is the most authoritative-looking thing
// in the history, so the model reuses it rather than looking again.
// Four rounds of forbidding that changed nothing; removing it is the
// only thing that has.
func TestOldToolResultsAreWithdrawn(t *testing.T) {
	at := time.Now().UTC()
	said := []conversation.Message{
		conversation.Said("c", "what reminders do I have", at),
		conversation.ToolsReturned("c", []conversation.ToolResult{{
			ID: "t1", Name: "reminder_list",
			Content: "Tablets at 9:00 am [id rem_01ABC]",
		}}, at),
	}

	got := conversation.ForModel(said)

	var results []conversation.ToolResult
	for _, m := range got {
		results = append(results, m.ToolResults...)
	}
	if len(results) != 1 {
		t.Fatalf("%d results, want the one", len(results))
	}
	if strings.Contains(results[0].Content, "Tablets") {
		t.Errorf("the old answer is still there: %q", results[0].Content)
	}
	if strings.Contains(results[0].Content, "rem_01ABC") {
		t.Errorf("a stale identifier is still there: %q", results[0].Content)
	}
	// Which tool ran is a fact about the conversation and is kept.
	if !strings.Contains(results[0].Content, "reminder_list") {
		t.Errorf("content = %q, want the tool named", results[0].Content)
	}
	if !strings.Contains(results[0].Content, "Call it again") {
		t.Errorf("content = %q, want it to say to look again", results[0].Content)
	}
}

// What is stored is untouched: only the copy sent to the model is.
func TestWithdrawingDoesNotTouchWhatIsStored(t *testing.T) {
	at := time.Now().UTC()
	stored := conversation.ToolsReturned("c", []conversation.ToolResult{{
		ID: "t1", Name: "reminder_list", Content: "Tablets at 9:00 am",
	}}, at)

	_ = conversation.ForModel([]conversation.Message{stored})

	if !strings.Contains(stored.ToolResults[0].Content, "Tablets") {
		t.Errorf("the stored message was altered: %q", stored.ToolResults[0].Content)
	}
}

// A person reading the transcript still sees what the tool said.
func TestAPersonStillSeesToolResults(t *testing.T) {
	at := time.Now().UTC()
	said := []conversation.Message{conversation.ToolsReturned("c", []conversation.ToolResult{{
		ID: "t1", Name: "reminder_list", Content: "Tablets at 9:00 am",
	}}, at)}

	for _, m := range conversation.ForPerson(said) {
		for _, r := range m.ToolResults {
			if !strings.Contains(r.Content, "Tablets") {
				t.Errorf("a person can no longer see what ran: %q", r.Content)
			}
		}
	}
}

// TestAnInterruptionSaysWhatStoppedIt : A turn cut short by something
// other than the person says so.
//
// Home Assistant allows a turn thirty seconds and then closes the
// connection. Four turns in a row were cut that way on 29 September
// 2026 -- measured at 29.98 seconds each -- and the transcript recorded
// every one as "The person stopped this before it finished." They had
// not touched it. The transcript is read back by the assistant and by
// the owner, and a false account of why something ended is worse than
// none.
func TestAnInterruptionSaysWhatStoppedIt(t *testing.T) {
	at := time.Date(2026, 9, 29, 12, 37, 59, 0, time.UTC)

	const why = "This was cut short after thirty seconds: whatever asked the question stopped waiting."
	m := conversation.Interrupted("ses_1", why, at)

	if !strings.Contains(m.Content, "stopped waiting") {
		t.Errorf("content = %q, want it to say what happened", m.Content)
	}
	if strings.Contains(m.Content, "The person stopped") {
		t.Errorf("content = %q, blamed the person for something they did not do", m.Content)
	}

	// The ordinary case is unchanged: no reason means they stopped it.
	plain := conversation.Interrupted("ses_1", "", at)
	if !strings.Contains(plain.Content, "The person stopped this before it finished") {
		t.Errorf("content = %q, want the usual wording", plain.Content)
	}

	// Either way it is the same kind of thing, and the model is given it.
	for _, got := range []conversation.Message{m, plain} {
		if got.Kind != conversation.Interruption {
			t.Errorf("kind = %q, want an interruption", got.Kind)
		}
		if len(conversation.ForModel([]conversation.Message{got})) != 1 {
			t.Error("the model was not told the turn was cut short")
		}
	}
}
