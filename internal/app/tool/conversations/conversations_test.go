package conversations_test

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"personal-assistant/internal/base/records/chat/memory"
	"personal-assistant/internal/base/records/conversation"
	"personal-assistant/internal/base/records/user"

	"personal-assistant/internal/app/tool"
	"personal-assistant/internal/app/tool/conversations"
	"personal-assistant/internal/base/records/chat"
)

// Every conversation tool has to survive registration, which is where a
// missing description or an untyped argument is caught.
func TestEveryConversationToolRegisters(t *testing.T) {
	if _, err := tool.NewRegistry(conversations.All(nil)...); err != nil {
		t.Fatalf("a conversation tool is not usable: %v", err)
	}
}

// Deleting is the one thing that cannot be undone, and on voice a misheard
// sentence is the whole authorisation.
func TestVoiceCannotDelete(t *testing.T) {
	r, err := tool.NewRegistry(conversations.All(nil)...)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	for _, x := range r.For(chat.ChannelVoice) {
		if x.Name == "conversation_delete" {
			t.Error("voice is offered conversation_delete")
		}
	}

	var typedHasIt bool
	for _, x := range r.For(chat.ChannelDirect) {
		if x.Name == "conversation_delete" {
			typedHasIt = true
		}
	}
	if !typedHasIt {
		t.Error("typed cannot delete either, so nothing can")
	}
}

// Every tool shows the model at least one worked call, and the examples have
// to be valid against the schema the model is actually shown: an example
// that lies about its arguments teaches the model to get them wrong.
//
// Checked against the narrated schema rather than the tool's own, because
// that is the one the model reads. The two differ by the saying argument,
// which is added on the way out and taken off on the way back, and an
// example without it teaches the model to leave it out -- which is exactly
// what happened when these examples were written before it existed.
func TestEveryExampleMatchesItsSchema(t *testing.T) {
	for _, x := range conversations.All(nil) {
		if len(x.Examples) == 0 {
			t.Errorf("%s shows no example call", x.Name)
			continue
		}
		offered := tool.Narrated(x.Params, "sir")
		for _, e := range x.Examples {
			if err := tool.Validate(offered, []byte(e.Args)); err != nil {
				t.Errorf("%s has an example the model would be refused for copying: %s — %v",
					x.Name, e.Args, err)
			}
			if !strings.Contains(e.Args, `"saying"`) {
				t.Errorf("%s has an example with no saying, which teaches the model to omit it: %s",
					x.Name, e.Args)
			}
		}
	}
}

// held : A repository with one conversation in it, and who owns it.
func held(t *testing.T, title string) (*memory.Repository, string, string) {
	t.Helper()
	repo := memory.New()
	ctx := context.Background()

	person, err := user.NewUser("dhanush", "a-hash")
	if err != nil {
		t.Fatalf("NewUser: %v", err)
	}
	if err := repo.CreateUser(ctx, person); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	c := chat.NewConversation(person.ID, title)
	if err := repo.CreateConversation(ctx, c); err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	return repo, person.ID, c.ID
}

// deleting : Calls conversation_delete as the given person.
func deleting(t *testing.T, repo *memory.Repository, userID, args string) tool.Result {
	t.Helper()
	r, err := tool.NewRegistry(conversations.All(repo)...)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	// Listed first, because a write refuses until its domain has
	// been read in the same turn -- which is itself a guard, and the
	// first thing these tests ran into.
	return r.Call(context.Background(), "conversation_delete", tool.Invocation{
		Caller: tool.Caller{UserID: userID, Channel: chat.ChannelDirect},
		Args:   []byte(args),
		Ran:    []string{"conversation_list"},
	})
}

// TestNothingIsDeletedUnlessTheNameMatches : The title is the cheapest
// possible proof that the model looked before it acted.
//
// An identifier it invented or misremembered would otherwise destroy a
// real conversation and everything said in it, and there is nothing to
// undo it with.
func TestNothingIsDeletedUnlessTheNameMatches(t *testing.T) {
	repo, userID, id := held(t, "Roof Quotes")

	for _, wrong := range []string{"", "roof quotes", "Roof Quote", "Something Else"} {
		got := deleting(t, repo, userID, `{"conversation_id":"`+id+`","confirm_title":`+strconv.Quote(wrong)+`}`)
		if got.Outcome != conversation.OutcomeFailed {
			t.Errorf("a title of %q deleted it anyway", wrong)
		}
		if still, err := repo.GetConversation(context.Background(), id); err != nil || still == nil {
			t.Fatalf("it was deleted with the title %q", wrong)
		}
	}

	// And the exact name does.
	got := deleting(t, repo, userID, `{"conversation_id":"`+id+`","confirm_title":"Roof Quotes"}`)
	if got.Outcome == conversation.OutcomeFailed {
		t.Fatalf("the right title did not delete it: %s", got.Content)
	}
	if still, _ := repo.GetConversation(context.Background(), id); still != nil {
		t.Error("it said it deleted it and it is still there")
	}
}

// TestSomebodyElsesConversationIsNotFound : Not refused, which would
// tell them it exists. Somebody guessing identifiers learns nothing.
func TestSomebodyElsesConversationIsNotFound(t *testing.T) {
	repo, _, id := held(t, "Roof Quotes")

	got := deleting(t, repo, "usr_somebody_else",
		`{"conversation_id":"`+id+`","confirm_title":"Roof Quotes"}`)
	if got.Outcome != conversation.OutcomeFailed {
		t.Fatal("somebody deleted a conversation that was not theirs")
	}
	if still, _ := repo.GetConversation(context.Background(), id); still == nil {
		t.Fatal("it is gone")
	}
	if strings.Contains(got.Content, "Roof Quotes") {
		t.Errorf("the refusal named the conversation: %q", got.Content)
	}
}

// TestDeletingRefusesUntilTheListingHasRun : Found by writing the
// tests above, which failed until they listed first.
//
// It is the guard that makes the title check meaningful: without it a
// model could pair an invented identifier with an invented title and
// both would be its own invention. With it, the identifier has to
// have come from a listing in this turn.
func TestDeletingRefusesUntilTheListingHasRun(t *testing.T) {
	repo, userID, id := held(t, "Roof Quotes")

	r, err := tool.NewRegistry(conversations.All(repo)...)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	got := r.Call(context.Background(), "conversation_delete", tool.Invocation{
		Caller: tool.Caller{UserID: userID, Channel: chat.ChannelDirect},
		Args:   []byte(`{"conversation_id":"` + id + `","confirm_title":"Roof Quotes"}`),
		// Nothing read this turn.
	})
	if got.Outcome != conversation.OutcomeFailed {
		t.Fatal("it deleted a conversation without having read the listing")
	}
	if still, _ := repo.GetConversation(context.Background(), id); still == nil {
		t.Fatal("it is gone")
	}
}
