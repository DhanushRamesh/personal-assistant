package aside_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/aside"
	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/chat/memory"
	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
)

// at : A fixed afternoon, so the written hour is the same every run.
func at() time.Time { return time.Date(2026, 9, 27, 10, 20, 0, 0, time.UTC) }

// india : The zone the hour is written in.
func india() *time.Location { return time.FixedZone("IST", 5*3600+1800) }

// writer : A writer over a fresh repository, with a user already in it.
func writer(t *testing.T) (*aside.Writer, *memory.Repository, *chat.User) {
	t.Helper()
	repo := memory.New()

	user := &chat.User{ID: "usr_test"}
	if err := repo.CreateUser(context.Background(), user); err != nil {
		t.Fatalf("creating a user: %v", err)
	}

	return &aside.Writer{
		Conversations: repo,
		Clients:       repo,
		Location:      india(),
		Now:           at,
	}, repo, user
}

// client : Registers one on the given channel.
func client(t *testing.T, repo *memory.Repository, userID, name string, ch chat.Channel) *chat.Client {
	t.Helper()
	c := &chat.Client{UserID: userID, Name: name, Channel: ch}
	if err := repo.CreateClient(context.Background(), c); err != nil {
		t.Fatalf("creating a client: %v", err)
	}
	return c
}

// reread : The client as it now stands.
func reread(t *testing.T, repo *memory.Repository, userID, clientID string) chat.Client {
	t.Helper()
	clients, err := repo.ListClients(context.Background(), userID, false)
	if err != nil {
		t.Fatalf("listing clients: %v", err)
	}
	for _, c := range clients {
		if c.ID == clientID {
			return c
		}
	}
	t.Fatalf("client %s is gone", clientID)
	return chat.Client{}
}

// What was said aloud is written into the conversation the voice client is
// talking in, which is where the reply to it will arrive.
func TestAnAsideLandsWhereTheReplyWill(t *testing.T) {
	w, repo, user := writer(t)
	voice := client(t, repo, user.ID, "home assistant", chat.ChannelVoice)

	w.Said(context.Background(), user.ID, "You should have heard this at 3:50 pm, sir. Call the bank.")

	// The client had no conversation yet, so one was taken up for it.
	got := reread(t, repo, user.ID, voice.ID)
	if got.ActiveConversationID == "" {
		t.Fatal("the voice client was left without a conversation")
	}

	said, err := repo.All(context.Background(), got.ActiveConversationID)
	if err != nil {
		t.Fatalf("reading the conversation: %v", err)
	}
	if len(said) != 1 {
		t.Fatalf("%d messages, want the one aside", len(said))
	}
	if said[0].Kind != conversation.Aside {
		t.Errorf("kind = %q, want an aside", said[0].Kind)
	}
	if said[0].Role != conversation.Assistant {
		t.Errorf("role = %q, want the assistant", said[0].Role)
	}
	if !strings.Contains(said[0].Content, "Call the bank") {
		t.Errorf("content = %q", said[0].Content)
	}
}

// The hour is kept, because a model reading the conversation back is given
// the words and not the timestamps. Without it "how late was I" is
// unanswerable.
func TestAnAsideCarriesTheHourItWasSaid(t *testing.T) {
	w, repo, user := writer(t)
	client(t, repo, user.ID, "home assistant", chat.ChannelVoice)

	w.Said(context.Background(), user.ID, "Good afternoon, sir.")

	id, err := chat.EnsureConversation(context.Background(), repo, user.ID)
	if err != nil {
		t.Fatalf("finding the conversation: %v", err)
	}
	said, err := repo.All(context.Background(), id)
	if err != nil || len(said) != 1 {
		t.Fatalf("messages = %v, err = %v", said, err)
	}

	// Ten past four in the afternoon, in the person's own zone.
	if said[0].Detail != "3:50 pm" {
		t.Errorf("hour = %q, want the local time it was said", said[0].Detail)
	}

	// And it reaches the model that way, marked as unprompted.
	forModel := conversation.ForModel(said)
	if len(forModel) != 1 {
		t.Fatalf("%d messages for the model", len(forModel))
	}
	if !strings.Contains(forModel[0].Content, "3:50 pm") ||
		!strings.Contains(forModel[0].Content, "unprompted") {
		t.Errorf("for the model = %q", forModel[0].Content)
	}
}

// A person with nothing that listens is not spoken to, so there is nothing
// to write down and no conversation is disturbed.
func TestNothingIsWrittenWhenNobodyIsListening(t *testing.T) {
	w, repo, user := writer(t)
	typed := client(t, repo, user.ID, "chrome", chat.ChannelDirect)

	w.Said(context.Background(), user.ID, "Good afternoon, sir.")

	if got := reread(t, repo, user.ID, typed.ID); got.ActiveConversationID != "" {
		t.Error("a typed client was given a conversation it never asked for")
	}
}
