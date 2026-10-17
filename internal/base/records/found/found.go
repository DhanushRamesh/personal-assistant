// Package found keeps what a tool found out of the conversation that
// asked for it.
//
// Measured on the real store: tool results are 51% of everything a
// conversation holds -- 581,023 characters against 366,876 of speech --
// and a conversation carries the last 60,000 bytes of itself on every
// turn. One `event_recent` came back with three hundred events, a
// battery climbing 77 to 100 a line at a time, and that wall then rode
// along in the history for the rest of the conversation, re-read by
// the model on every turn and worth nothing after the first.
//
// So a result too big to be worth repeating is written here, and the
// conversation keeps one line saying what it was and how to read it
// again. Nothing is destroyed, which is what separates this from
// summarising: the assistant can always come back for the whole thing,
// and whether it ever does is the measure of whether the one line was
// good enough.
package found

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Big : How long a result may be before it is put aside.
//
// Measured rather than chosen. The average tool result is 623
// characters and the heaviest tools average a thousand: event_recent
// 1,019, read_terminal 1,678, conversation_list 1,711. Two thousand
// leaves every ordinary answer in the conversation where it belongs --
// a reminder set, a place named, a calendar day with two things on it
// -- and catches the walls.
//
// Set too low this costs a turn for nothing: the assistant reads the
// line, does not have what it needs, and comes back for the body.
// Rereads counts exactly that, so the number can be moved on evidence.
const Big = 2000

// Found : One result, kept.
type Found struct {
	// ID : What the conversation refers to it by.
	ID string
	// UserID, ConversationID : Whose it is, and where it was found.
	ConversationID string
	UserID         string
	// Tool, Arguments : What was asked, so that asking the same thing
	// twice can be recognised.
	Tool      string
	Arguments string
	// Head : The one line the conversation keeps instead of the body.
	Head string
	// Body : The result as the tool wrote it.
	Body string
	// Rereads : How many times the assistant came back for the body.
	Rereads int
	// At : When it was found.
	At time.Time
}

// Bytes : How long the body is.
func (f Found) Bytes() int { return len(f.Body) }

// ErrGone : The result is not there to read.
//
// Returned rather than an empty body, because the two mean different
// things to whoever asked: nothing found is an answer, and a result
// that has been forgotten is a reason to ask the tool again.
var ErrGone = errors.New("found: that result is no longer kept")

// Store : Where results are kept.
type Store interface {
	// Keep : Writes one, and returns it with its identifier.
	Keep(ctx context.Context, f Found) (Found, error)
	// Read : The whole result, and a note that it was read again.
	//
	// Scoped to the person, so an identifier guessed or carried over
	// from somebody else's conversation finds nothing.
	Read(ctx context.Context, userID, id string) (Found, error)
	// Forget : Everything kept for a conversation.
	Forget(ctx context.Context, conversationID string) error
}

// Worth : Whether a result is worth putting aside.
//
// Only length decides it. What a tool returns is its own business and
// nothing here reads it, so the rule has to be one that holds for a
// tool written next year as well as for the ones written already.
func Worth(body string) bool { return len(body) > Big }

// Line : What the conversation keeps in place of the body.
//
// Three things, because the model has to decide from this line alone
// whether it needs the rest: what it was, how much there is, and how
// to get it. The head is the tool's own first line wherever it has
// one, which for every tool here is already a sentence saying what
// was found -- "300 events today, as 298 entries once repeats are
// grouped".
func Line(f Found) string {
	head := strings.TrimSpace(f.Head)
	if head == "" {
		head = f.Tool + " answered"
	}
	return head + " [the rest is kept: found_read(\"" + f.ID + "\"), " +
		plural(f.Bytes()) + " in all]"
}

// Head : A result's own first line, for the line that replaces it.
func Head(body string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(body), "\n")
	return strings.TrimSpace(first)
}

// plural : A size, in words nobody has to decode.
func plural(n int) string {
	switch {
	case n < 1000:
		return itoa(n) + " characters"
	default:
		return itoa((n+500)/1000) + " thousand characters"
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
