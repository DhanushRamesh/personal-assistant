package tool

import (
	"context"
	"strings"
	"testing"

	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/base/records/conversation"
)

func lister() Tool {
	return Tool{Name: "thing_list", Domain: "thing", Lists: true,
		Purpose: "x", UseWhen: "always", Channels: []chat.Channel{chat.ChannelDirect},
		Run: func(context.Context, Invocation) Result { return OK("here they are") }}
}

func remover() Tool {
	return Tool{Name: "thing_delete", Domain: "thing", Writes: true,
		Purpose: "x", UseWhen: "always", Channels: []chat.Channel{chat.ChannelDirect},
		Run: func(context.Context, Invocation) Result { return OK("gone") }}
}

// TestAWriteNeedsAReadFirst : A write is refused until its own domain has
// been read in the same turn, and the refusal says how to put it right.
func TestAWriteNeedsAReadFirst(t *testing.T) {
	r, err := NewRegistry(lister(), remover())
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	who := Caller{UserID: "u1", Channel: chat.ChannelDirect}

	cold := r.Call(context.Background(), "thing_delete", Invocation{Caller: who, Args: []byte(`{}`)})
	if cold.Outcome != conversation.OutcomeFailed {
		t.Fatalf("a write ran with nothing read first: %s", cold.Content)
	}
	for _, want := range []string{"Nothing has been changed", "thing_delete", "never made up to fit"} {
		if !strings.Contains(cold.Content, want) {
			t.Errorf("refusal does not mention %q: %s", want, cold.Content)
		}
	}

	warm := r.Call(context.Background(), "thing_delete",
		Invocation{Caller: who, Args: []byte(`{}`), Ran: []string{"thing_list"}})
	if warm.Outcome != conversation.OutcomeOK {
		t.Errorf("a write was refused after its domain was read: %s", warm.Content)
	}

	// A read is never blocked.
	if got := r.Call(context.Background(), "thing_list", Invocation{Caller: who, Args: []byte(`{}`)}); got.Outcome != conversation.OutcomeOK {
		t.Errorf("a read was refused: %s", got.Content)
	}
}

// TestTheRefusalCarriesTheListing : The refusal does the reading rather
// than asking for it.
//
// Asking cost two rounds of a thirty-second turn -- guess, be refused,
// read, write -- and in one conversation about a cinema booking it
// happened four times.
func TestTheRefusalCarriesTheListing(t *testing.T) {
	r, err := NewRegistry(lister(), remover())
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	who := Caller{UserID: "u1", Channel: chat.ChannelDirect}

	cold := r.Call(context.Background(), "thing_delete", Invocation{Caller: who, Args: []byte(`{}`)})

	if !strings.Contains(cold.Content, "here they are") {
		t.Errorf("the refusal did not carry what is there: %s", cold.Content)
	}
	if len(cold.Read) != 1 || cold.Read[0] != "thing_list" {
		t.Errorf("the listing was not recorded as read: %v", cold.Read)
	}
}

// TestADomainThatCannotBeReadWithoutArgumentsIsStillAsked : A listing
// the server cannot call itself is asked for, as it always was.
func TestADomainThatCannotBeReadWithoutArgumentsIsStillAsked(t *testing.T) {
	search := lister()
	search.Name = "thing_search"
	search.Params = Schema{
		Properties: map[string]Property{"q": {Type: "string", Description: "what to look for"}},
		Required:   []string{"q"},
	}

	r, err := NewRegistry(search, remover())
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	who := Caller{UserID: "u1", Channel: chat.ChannelDirect}

	cold := r.Call(context.Background(), "thing_delete", Invocation{Caller: who, Args: []byte(`{}`)})

	if !strings.Contains(cold.Content, "List first") {
		t.Errorf("a listing needing arguments was not asked for: %s", cold.Content)
	}
	if len(cold.Read) != 0 {
		t.Errorf("nothing was read, but %v was recorded as read", cold.Read)
	}
}

// TestAListingThatFailedIsNotRecordedAsRead : The reason for the whole
// guard is an identifier that came from nowhere. A listing that could
// not be fetched must not let the next call write.
func TestAListingThatFailedIsNotRecordedAsRead(t *testing.T) {
	broken := lister()
	broken.Run = func(context.Context, Invocation) Result {
		return Failed("No Google account is connected.")
	}

	r, err := NewRegistry(broken, remover())
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	who := Caller{UserID: "u1", Channel: chat.ChannelDirect}

	cold := r.Call(context.Background(), "thing_delete", Invocation{Caller: who, Args: []byte(`{}`)})

	if len(cold.Read) != 0 {
		t.Errorf("a listing that failed was recorded as read: %v", cold.Read)
	}
	// Passed on, so the model can say what is wrong now rather than
	// meeting the same failure a round later.
	if !strings.Contains(cold.Content, "No Google account is connected.") {
		t.Errorf("the model was not told why: %s", cold.Content)
	}
}

// opener : A read that names one thing by identifier and changes
// nothing, which is the shape mail_read has.
func opener() Tool {
	return Tool{Name: "thing_open", Domain: "thing",
		Purpose: "x", UseWhen: "always", Channels: []chat.Channel{chat.ChannelDirect},
		Params: Schema{
			Properties: map[string]Property{"id": {Type: "string", Description: "which one"}},
			Required:   []string{"id"},
		},
		Run: func(context.Context, Invocation) Result { return OK("the whole of it") }}
}

// TestAReadThatNamesSomethingNeedsAListingFirst : The Netflix case.
//
// He was told a message had arrived about a new film and asked which
// film. The model called mail_read with "netflix_new_film" -- an
// identifier in the shape of the subject line -- and when it failed,
// searched the web and answered about Netflix in general rather than
// about his mail.
//
// mail_read changes nothing, so the guard, which asked only whether a
// tool writes, never looked at it.
func TestAReadThatNamesSomethingNeedsAListingFirst(t *testing.T) {
	r, err := NewRegistry(lister(), opener())
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	who := Caller{UserID: "u1", Channel: chat.ChannelDirect}

	got := r.Call(context.Background(), "thing_open",
		Invocation{Caller: who, Args: []byte(`{"id":"netflix_new_film"}`)})

	if got.Outcome != conversation.OutcomeFailed {
		t.Fatalf("an invented identifier was read without a listing: %s", got.Content)
	}
	if !strings.Contains(got.Content, "here they are") {
		t.Errorf("the listing was not done for it: %s", got.Content)
	}
	// Nothing was changed, but saying so to someone who asked to be told
	// something is noise about a danger that was never there.
	if strings.Contains(got.Content, "Nothing has been changed") {
		t.Errorf("a read was refused as though it were a write: %s", got.Content)
	}

	// With the listing done, the same call goes through.
	again := r.Call(context.Background(), "thing_open",
		Invocation{Caller: who, Args: []byte(`{"id":"18f2c"}`), Ran: []string{"thing_list"}})
	if again.Outcome == conversation.OutcomeFailed {
		t.Errorf("refused after the listing had run: %s", again.Content)
	}
}

// TestAnOptionalIdentifierIsNotNaming : A listing narrowed by an
// identifier is how identifiers are found, not a place to invent one.
func TestAnOptionalIdentifierIsNotNaming(t *testing.T) {
	narrowed := lister()
	narrowed.Params = Schema{
		Properties: map[string]Property{"id": {Type: "string", Description: "only this one"}},
	}
	if NamesSomething(narrowed) {
		t.Error("a tool with an optional identifier was treated as naming one")
	}
	if !NamesSomething(opener()) {
		t.Error("a required identifier was not recognised")
	}
}
