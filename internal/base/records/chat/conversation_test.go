package chat_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"personal-assistant/internal/base/records/chat"
)

// talking : A Conversations that remembers what it was told, which is
// all these two functions need to be judged on.
type talking struct {
	kept   []chat.Conversation
	active map[string]string
	failOn string
	made   int
}

func (c *talking) ListConversations(_ context.Context, _ string, limit int) ([]chat.Conversation, error) {
	if c.failOn == "list" {
		return nil, errors.New("the database is away")
	}
	if limit > 0 && len(c.kept) > limit {
		return c.kept[:limit], nil
	}
	return c.kept, nil
}

func (c *talking) CreateConversation(_ context.Context, conv *chat.Conversation) error {
	if c.failOn == "create" {
		return errors.New("the database is away")
	}
	c.made++
	c.kept = append([]chat.Conversation{*conv}, c.kept...)
	return nil
}

func (c *talking) SetActiveConversation(_ context.Context, _, clientID, id string) error {
	if c.failOn == "set" {
		return errors.New("the database is away")
	}
	if c.active == nil {
		c.active = map[string]string{}
	}
	c.active[clientID] = id
	return nil
}

// TestSomebodyAlwaysHasSomewhereToTalk : Reuse is the default and a
// new conversation is something asked for.
//
// Logging in from a second client, or finding the active one gone,
// must not start a fresh thread: the history is the point of having
// a conversation at all.
func TestSomebodyAlwaysHasSomewhereToTalk(t *testing.T) {
	// With none, one is made.
	repo := &talking{}
	id, err := chat.EnsureConversation(context.Background(), repo, "usr_1")
	if err != nil {
		t.Fatalf("EnsureConversation: %v", err)
	}
	if id == "" || repo.made != 1 {
		t.Fatalf("made %d conversations and returned %q, want exactly one", repo.made, id)
	}

	// With one, it is taken up rather than replaced.
	again, err := chat.EnsureConversation(context.Background(), repo, "usr_1")
	if err != nil {
		t.Fatalf("EnsureConversation: %v", err)
	}
	if again != id {
		t.Errorf("returned %q the second time, want the existing %q", again, id)
	}
	if repo.made != 1 {
		t.Errorf("made %d conversations, want the first one reused", repo.made)
	}
}

// TestAClientWithNowhereToTalkIsGivenSomewhereAndTold : The telling
// is the half that matters. Resolving a conversation and not
// remembering it means doing it again on the next turn, and a client
// that never settles never has a history.
func TestAClientWithNowhereToTalkIsGivenSomewhereAndTold(t *testing.T) {
	repo := &talking{}

	id, err := chat.ActiveConversation(context.Background(), repo, "usr_1", "cli_1", "")
	if err != nil {
		t.Fatalf("ActiveConversation: %v", err)
	}
	if repo.active["cli_1"] != id {
		t.Errorf("the client was given %q and told %q", id, repo.active["cli_1"])
	}
}

// TestAClientThatKnowsWhereItIsTalkingIsLeftAlone : And nothing is
// read or written, because there is nothing to decide.
func TestAClientThatKnowsWhereItIsTalkingIsLeftAlone(t *testing.T) {
	repo := &talking{failOn: "list"} // any read at all would fail

	id, err := chat.ActiveConversation(context.Background(), repo, "usr_1", "cli_1", "cnv_known")
	if err != nil {
		t.Fatalf("ActiveConversation went looking when it was already told: %v", err)
	}
	if id != "cnv_known" {
		t.Errorf("returned %q, want the conversation it was given", id)
	}
}

// TestAFailureIsReportedRatherThanGuessedAround : Each of the three
// things that can fail, because returning an empty identifier as
// though it worked would put the next turn somewhere nobody can find.
func TestAFailureIsReportedRatherThanGuessedAround(t *testing.T) {
	for _, when := range []string{"list", "create", "set"} {
		t.Run(when, func(t *testing.T) {
			repo := &talking{failOn: when}
			id, err := chat.ActiveConversation(context.Background(), repo, "usr_1", "cli_1", "")
			if err == nil {
				t.Fatalf("a failure at %q was swallowed, returning %q", when, id)
			}
			if id != "" {
				t.Errorf("returned %q alongside an error", id)
			}
		})
	}
}

// TestArchivingIsAMomentAndNotAFlag : Archived() reads a timestamp,
// so a conversation archived and written back without one would
// quietly reappear.
func TestArchivingIsAMomentAndNotAFlag(t *testing.T) {
	c := chat.NewConversation("usr_1", "about the roof")
	if c.Archived() {
		t.Error("a conversation was archived the moment it was made")
	}

	c.Archive()
	if !c.Archived() {
		t.Error("Archive() did not archive it")
	}
	was := c.ArchivedAt

	// Archiving twice must not move the moment: when it was put away
	// is a fact about the first time.
	c.Archive()
	if c.ArchivedAt != was {
		t.Error("archiving again moved when it was archived")
	}

	c.Unarchive()
	if c.Archived() {
		t.Error("Unarchive() left it archived")
	}
}

// TestAConversationIdentifierSaysWhatItIs : And is not mistaken for a
// chat, which is the other thing with a ULID in this package.
func TestAConversationIdentifierSaysWhatItIs(t *testing.T) {
	id := chat.NewConversationID()
	if !strings.HasPrefix(id, chat.ConversationIDPrefix) {
		t.Errorf("%q does not begin with %q", id, chat.ConversationIDPrefix)
	}
	if !chat.ValidConversationID(id) {
		t.Errorf("%q was minted and then refused", id)
	}
	if chat.ValidConversationID(chat.NewID()) {
		t.Error("a chat identifier was accepted as a conversation")
	}
	for _, bad := range []string{"", "cnv_", "cnv_short", id + "x"} {
		if chat.ValidConversationID(bad) {
			t.Errorf("%q was accepted", bad)
		}
	}
}
