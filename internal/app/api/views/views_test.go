package views_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/app/api/views"
	"github.com/DhanushRamesh/personal-assistant/internal/base/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/base/conversation"
)

// The JSON names here are the API's published contract: a client reads them,
// and renaming one silently breaks it. These tests pin the names rather than
// the rendering, which the handler tests already cover.

// keys : The field names a value marshals to.
func keys(t *testing.T, v any) map[string]bool {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("unmarshal %s: %v", raw, err)
	}
	out := map[string]bool{}
	for k := range fields {
		out[k] = true
	}
	return out
}

// assertKeys : Reports any published name that is missing or unexpected.
func assertKeys(t *testing.T, v any, want ...string) {
	t.Helper()
	got := keys(t, v)
	for _, name := range want {
		if !got[name] {
			t.Errorf("%T does not publish %q", v, name)
		}
		delete(got, name)
	}
	for name := range got {
		t.Errorf("%T publishes an unexpected %q", v, name)
	}
}

func TestChatPublishesItsFields(t *testing.T) {
	now := time.Now().UTC().Truncate(chat.StoredPrecision)
	tk := &chat.Chat{
		ID: chat.NewID(), ConversationID: chat.NewConversationID(), Prompt: "hello",
		Status: chat.StatusCompleted, Response: "hi", CreatedAt: now, UpdatedAt: now,
		StartedAt: &now, FinishedAt: &now,
	}

	assertKeys(t, views.OfChat(tk),
		"id", "conversation_id", "prompt", "status", "response",
		"created_at", "updated_at", "started_at", "finished_at")
}

// A listing must not carry response bodies: they can be large, and a listing
// is read to choose one, not to read them all.
func TestSummaryOmitsTheResponse(t *testing.T) {
	now := time.Now().UTC().Truncate(chat.StoredPrecision)
	got := keys(t, views.OfSummary(chat.Summary{
		ID: chat.NewID(), Prompt: "hello", Status: chat.StatusCompleted,
		CreatedAt: now, UpdatedAt: now,
	}))

	if got["response"] {
		t.Error("a chat summary publishes a response body")
	}
	if !got["prompt"] || !got["status"] {
		t.Errorf("a summary is missing its basics: %v", got)
	}
}

// The password hash must never be published, whatever else a user carries.
func TestUserNeverPublishesTheHash(t *testing.T) {
	user, err := chat.NewUser("tester", "$2a$04$averyrealisticlookinghashvalue")
	if err != nil {
		t.Fatalf("chat.NewUser: %v", err)
	}

	assertKeys(t, views.OfUser(user), "id", "username", "created_at")

	raw, _ := json.Marshal(views.OfUser(user))
	for _, prefix := range []string{"$2a$", "$2b$", "$2y$"} {
		if strings.Contains(string(raw), prefix) {
			t.Errorf("the password hash was published: %s", raw)
		}
	}
}

// The token is never published with a client: only its hash is stored, and
// the one time the token itself appears is the login response.
func TestClientNeverPublishesTheToken(t *testing.T) {
	client, err := chat.NewClient(chat.NewUserID(), "my phone", "a-token-hash", chat.ChannelDirect)
	if err != nil {
		t.Fatalf("chat.NewClient: %v", err)
	}
	client.ActiveConversationID = chat.NewConversationID()

	assertKeys(t, views.OfClient(*client, true),
		"id", "name", "channel", "current", "revoked", "active_conversation_id",
		"created_at")

	raw, _ := json.Marshal(views.OfClient(*client, true))
	if strings.Contains(string(raw), "a-token-hash") {
		t.Errorf("the token hash was published: %s", raw)
	}
}

// Revoked is published even when false, so a client that is fine is visibly
// fine rather than merely silent about it.
func TestClientAlwaysStatesWhetherRevoked(t *testing.T) {
	client, _ := chat.NewClient(chat.NewUserID(), "", "hash", chat.ChannelDirect)

	got := keys(t, views.OfClient(*client, false))
	if !got["revoked"] || !got["current"] {
		t.Errorf("revoked and current must always appear: %v", got)
	}
	// An unrevoked client has no revocation time to publish.
	if got["revoked_at"] {
		t.Error("revoked_at appears on a client that was never revoked")
	}
}

func TestConversationPublishesItsFields(t *testing.T) {
	conversation := chat.NewConversation(chat.NewUserID(), "groceries")

	assertKeys(t, views.OfConversation(*conversation, true),
		"id", "title", "active", "created_at", "updated_at")
}

// TestTheTimelineSaysWhenTheAnswerBegan : The owner, looking at a turn
// marked "Answered 7.2s": "log the first token received time too
// there."
//
// 7.2s is when the last word was written. The answer is spoken as it
// is written, so by then they had been listening for over a second,
// and the only honest number for how long they waited is missing from
// the timeline entirely.
func TestTheTimelineSaysWhenTheAnswerBegan(t *testing.T) {
	started := time.Date(2026, 10, 3, 18, 0, 0, 0, time.UTC)
	began := started.Add(5900 * time.Millisecond)
	done := started.Add(7200 * time.Millisecond)

	tk := &chat.Chat{
		ID: "chat_x", Status: chat.StatusCompleted,
		StartedAt: &started, AnswerBeganAt: &began, FinishedAt: &done,
	}
	out := views.OfTimeline(tk, []conversation.Message{
		{Role: conversation.Assistant, Kind: conversation.Chat,
			Content: "You were at home most of the day, sir.", At: done},
	})

	var beganAt, answeredAt *int64
	for _, s := range out.Steps {
		switch s.Kind {
		case views.StepBegan:
			beganAt = s.OffsetMS
		case views.StepAnswered:
			answeredAt = s.OffsetMS
		}
	}
	if beganAt == nil {
		t.Fatal("the timeline does not say when the answer began")
	}
	if *beganAt != 5900 {
		t.Errorf("began at %d ms, want 5900", *beganAt)
	}
	if answeredAt == nil || *answeredAt != 7200 {
		t.Fatalf("the answer's own time moved: %v", answeredAt)
	}
	// In front of the answer, or a timeline read top to bottom has
	// the turn finishing before it started speaking.
	for _, s := range out.Steps {
		if s.Kind == views.StepAnswered {
			t.Fatal("the answer comes before the moment it began")
		}
		if s.Kind == views.StepBegan {
			break
		}
	}
}

// TestATurnThatStreamedNothingSaysNothing : Nil is not measured, and a
// turn that never streamed -- a failure, or a screen with nobody
// listening -- must not grow a step claiming it began at zero.
func TestATurnThatStreamedNothingSaysNothing(t *testing.T) {
	started := time.Date(2026, 10, 3, 18, 0, 0, 0, time.UTC)
	done := started.Add(time.Second)

	out := views.OfTimeline(
		&chat.Chat{ID: "chat_y", Status: chat.StatusCompleted,
			StartedAt: &started, FinishedAt: &done},
		[]conversation.Message{{Role: conversation.Assistant,
			Kind: conversation.Chat, Content: "Good evening, sir.", At: done}})

	for _, s := range out.Steps {
		if s.Kind == views.StepBegan {
			t.Error("a turn that streamed nothing reports when it began")
		}
	}
}
