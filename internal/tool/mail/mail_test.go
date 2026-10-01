package mail_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
	"github.com/DhanushRamesh/personal-assistant/internal/mail"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
	mailtool "github.com/DhanushRamesh/personal-assistant/internal/tool/mail"
)

// box : A mailbox that records the query it was asked for.
type box struct {
	asked   string
	give    []mail.Message
	threads []mail.Thread
}

func (b *box) Search(_ context.Context, _, query string, _ int) ([]mail.Message, error) {
	b.asked = query
	return b.give, nil
}
func (b *box) Recent(ctx context.Context, u string, n int) ([]mail.Message, error) {
	return b.Search(ctx, u, "in:inbox", n)
}
func (b *box) Unread(ctx context.Context, u string, n int) ([]mail.Message, error) {
	return b.Search(ctx, u, "in:inbox is:unread", n)
}
func (b *box) One(context.Context, string, string) (*mail.Message, error) { return nil, nil }
func (b *box) Threads(_ context.Context, _, query string, _ int) ([]mail.Thread, error) {
	b.asked = query
	return b.threads, nil
}
func (b *box) Count(context.Context, string) (int, error) { return 0, nil }

// threading : The mail_thread tool over a recording mailbox.
func threading(t *testing.T, b *box) tool.Tool {
	t.Helper()
	for _, x := range mailtool.All(b, mailtool.Clock{}) {
		if x.Name == "mail_thread" {
			return x
		}
	}
	t.Fatal("mail_thread is not among the tools")
	return tool.Tool{}
}

// searching : The mail_search tool over a recording mailbox.
func searching(t *testing.T, b *box) tool.Tool {
	t.Helper()
	for _, x := range mailtool.All(b, mailtool.Clock{}) {
		if x.Name == "mail_search" {
			return x
		}
	}
	t.Fatal("mail_search is not among the tools")
	return tool.Tool{}
}

// run : Calls the tool with these arguments.
func run(t *testing.T, x tool.Tool, args string) tool.Result {
	t.Helper()
	return x.Run(context.Background(), tool.Invocation{
		Caller: tool.Caller{Channel: chat.ChannelVoice, UserID: "usr_1"},
		Args:   json.RawMessage(args),
	})
}

// TestASenderIsTheWholeCall : "Did I get any mail from amazon.in" has
// the search in it, and the model should not have to write Gmail
// syntax or ask for it.
//
// It did ask, measured on 30 September 2026: it answered "I need a
// search parameter for the mail_search tool, sir -- for instance
// from:amazon.in", naming the query it would not write.
func TestASenderIsTheWholeCall(t *testing.T) {
	b := &box{}
	got := run(t, searching(t, b), `{"from":"amazon"}`)

	if got.Outcome == conversation.OutcomeFailed {
		t.Fatalf("a sender alone was refused: %s", got.Content)
	}
	if b.asked != "from:amazon" {
		t.Errorf("searched %q, want from:amazon", b.asked)
	}
}

// No in: qualifier, because Gmail's default already excludes spam and
// trash and includes the archive -- which is what the question means.
func TestTheSearchIsNotScopedToTheInbox(t *testing.T) {
	b := &box{}
	run(t, searching(t, b), `{"from":"amazon"}`)
	if strings.Contains(b.asked, "in:") {
		t.Errorf("searched %q: in:inbox misses filed mail, in:anywhere returns spam", b.asked)
	}
}

// The fields combine into one query rather than needing the model to
// write it.
func TestTheFieldsBecomeOneQuery(t *testing.T) {
	b := &box{}
	run(t, searching(t, b), `{"from":"alekhya","about":"flat","unread":true,"within_days":7}`)
	for _, want := range []string{"from:alekhya", "flat", "is:unread", "newer_than:7d"} {
		if !strings.Contains(b.asked, want) {
			t.Errorf("searched %q, missing %q", b.asked, want)
		}
	}
}

// TestATwoWordNameIsQuoted : Gmail reads a space as "and", so an
// unquoted name of two words becomes two conditions and matches much
// less than it should.
func TestATwoWordNameIsQuoted(t *testing.T) {
	b := &box{}
	run(t, searching(t, b), `{"from":"Red Giant Movies"}`)
	if !strings.Contains(b.asked, `from:"Red Giant Movies"`) {
		t.Errorf("searched %q, want the name quoted as one term", b.asked)
	}
}

// Raw Gmail syntax still works, for what the fields cannot say.
func TestRawSyntaxIsKept(t *testing.T) {
	b := &box{}
	run(t, searching(t, b), `{"from":"amazon","query":"has:attachment"}`)
	if !strings.Contains(b.asked, "has:attachment") || !strings.Contains(b.asked, "from:amazon") {
		t.Errorf("searched %q, want both the sender and the raw part", b.asked)
	}
}

// With nothing to search on it refuses, and says what to fill in
// rather than telling the model to ask the person.
func TestNothingToSearchOnIsRefused(t *testing.T) {
	b := &box{}
	got := run(t, searching(t, b), `{}`)
	if got.Outcome != conversation.OutcomeFailed {
		t.Errorf("outcome %q, want a refusal", got.Outcome)
	}
	if b.asked != "" {
		t.Errorf("searched %q with nothing given", b.asked)
	}
	if !strings.Contains(got.Content, "from") {
		t.Errorf("did not say which field to fill: %q", got.Content)
	}
}

// TestSentMailIsSearchable : Asked when they last sent a message, the
// assistant answered that it had no tool for it and that the sent
// folder was not covered.
//
// Both untrue: gmail.readonly reads sent mail and one qualifier finds
// it. It said so even after the look-first rule sent the answer back,
// which is the lesson -- being told to check the tools does not help
// when no description mentions the thing being asked about.
func TestSentMailIsSearchable(t *testing.T) {
	b := &box{}
	got := run(t, searching(t, b), `{"sent":true,"most":1}`)

	if got.Outcome == conversation.OutcomeFailed {
		t.Fatalf("asking for sent mail was refused: %s", got.Content)
	}
	if !strings.Contains(b.asked, "in:sent") {
		t.Errorf("searched %q, want in:sent", b.asked)
	}
}

// "When did I last email Alekhya" is a recipient and the sent folder.
func TestARecipientBecomesTo(t *testing.T) {
	b := &box{}
	run(t, searching(t, b), `{"to":"alekhya","sent":true}`)
	for _, want := range []string{"to:alekhya", "in:sent"} {
		if !strings.Contains(b.asked, want) {
			t.Errorf("searched %q, missing %q", b.asked, want)
		}
	}
}

// TestUnansweredIsDecidedFromTheThread : Whether they replied cannot
// be asked of Gmail directly.
//
// "in:inbox -in:sent" excludes an exchange they replied to rather
// than returning one they did not, so the question has to be settled
// from the thread's own labels after it is read.
func TestUnansweredIsDecidedFromTheThread(t *testing.T) {
	b := &box{threads: []mail.Thread{
		{ID: "t1", Subject: "the flat", Mine: true,
			Messages: []mail.Message{{ID: "m1", From: "alekhya"}}},
		{ID: "t2", Subject: "invoice", Mine: false,
			Messages: []mail.Message{{ID: "m2", From: "accounts"}}},
	}}
	got := run(t, threading(t, b), `{"unanswered":true}`)

	if strings.Contains(got.Content, "the flat") {
		t.Errorf("an exchange they replied to was reported as waiting: %q", got.Content)
	}
	if !strings.Contains(got.Content, "invoice") {
		t.Errorf("the one they never answered is missing: %q", got.Content)
	}
}

// Nothing waiting is an answer, not an empty list.
func TestNothingWaitingSaysSo(t *testing.T) {
	b := &box{threads: []mail.Thread{
		{ID: "t1", Subject: "the flat", Mine: true,
			Messages: []mail.Message{{ID: "m1", From: "alekhya"}}},
	}}
	got := run(t, threading(t, b), `{"unanswered":true}`)
	if got.Outcome == conversation.OutcomeFailed {
		t.Fatalf("refused: %s", got.Content)
	}
	if !strings.Contains(got.Content, "Nothing is waiting") {
		t.Errorf("did not say plainly that nothing is waiting: %q", got.Content)
	}
}

// A sender and a subject become one query, the same as the message search.
func TestAThreadSearchTakesWhatTheySaid(t *testing.T) {
	b := &box{threads: []mail.Thread{{ID: "t1", Subject: "the flat"}}}
	run(t, threading(t, b), `{"from":"alekhya","about":"flat"}`)
	for _, want := range []string{"from:alekhya", "flat"} {
		if !strings.Contains(b.asked, want) {
			t.Errorf("searched %q, missing %q", b.asked, want)
		}
	}
}

// Asked for nothing in particular it reads the inbox rather than
// refusing, because "any conversations" is a real question.
func TestAThreadSearchWithNothingGivenReadsTheInbox(t *testing.T) {
	b := &box{threads: []mail.Thread{{ID: "t1", Subject: "the flat"}}}
	got := run(t, threading(t, b), `{}`)
	if got.Outcome == conversation.OutcomeFailed {
		t.Fatalf("refused with nothing given: %s", got.Content)
	}
	if b.asked != "in:inbox" {
		t.Errorf("searched %q, want in:inbox", b.asked)
	}
}
