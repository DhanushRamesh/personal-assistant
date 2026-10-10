package calling

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"personal-assistant/internal/base/records/conversation"
)

// Asked : How long a question about a call stays answerable.
//
// Three minutes. Long enough for somebody to think about it or be
// interrupted mid-sentence, short enough that a "yes" to something
// else does not ring a person they were asked about before lunch.
const Asked = 3 * time.Minute

// Pending : A call that has been put to the person and not yet rung.
type Pending struct {
	// Token : What the model has to hand back. Server-made, so it
	// cannot be invented.
	Token string
	// Who : The name that was asked about, as it will be rung. The
	// call places this and never what the second call carries, so a
	// confirmed question about one person cannot ring another.
	Who string
	// Said : What they actually said, when the name was matched by
	// likeness rather than quoted.
	Said string
	// At : When the question was handed over. What the person has
	// said since is measured from here.
	At time.Time

	// Speaker, Mute : What they asked for along with the call --
	// "call Alekhya and put it on speakers" is one wish, not two,
	// and it has to survive the question in between.
	Speaker bool
	Mute    bool
}

// Heard : What the person has said, so that an answer can be told from
// a model answering itself.
//
// The whole of the gate. A boolean the model fills in is not a
// confirmation -- measured on the owner's own phone, the first
// call_place of a fresh conversation arrived with confirmed true and
// nobody had been asked anything.
type Heard interface {
	// SaidSince : What this person said after a moment, oldest first.
	SaidSince(ctx context.Context, userID string, since time.Time, limit int) ([]conversation.Message, error)
}

// Asking : The questions put about calls, by person.
//
// One each. Being asked about a second call replaces the first,
// because the person only heard the second one.
//
// In memory, and lost on a restart, which is the right way round: a
// question nobody can remember being asked should be asked again.
type Asking struct {
	mu   sync.Mutex
	held map[string]Pending
	// Now : The clock, replaceable in tests.
	Now func() time.Time
}

// NewAsking : A place to keep what has been asked.
func NewAsking(now func() time.Time) *Asking {
	return &Asking{held: map[string]Pending{}, Now: now}
}

// Put : Records that a call has been put to somebody, and gives the
// token that answers it.
func (a *Asking) Put(userID, who, said string, speaker, mute bool) Pending {
	p := Pending{
		Token: token(), Who: who, Said: said, At: a.clock(),
		Speaker: speaker, Mute: mute,
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.held[userID] = p
	return p
}

// Take : The pending call for this person, removed.
//
// Removed whether or not it turns out to be usable: a token is good
// once. A refused one has to be asked again rather than retried,
// since the thing that failed is the asking.
func (a *Asking) Take(userID string) (Pending, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	p, ok := a.held[userID]
	if ok {
		delete(a.held, userID)
	}
	return p, ok
}

// Peek : The pending call for this person, left where it is.
func (a *Asking) Peek(userID string) (Pending, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	p, ok := a.held[userID]
	return p, ok
}

// Keep : Puts one back, for a refusal that should not cost the
// question -- a token that arrived before the person had answered is
// about to be answered, and throwing it away would mean asking twice.
func (a *Asking) Keep(userID string, p Pending) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.held[userID] = p
}

// Stale : Whether this was asked too long ago to still be answered.
func (a *Asking) Stale(p Pending) bool {
	return a.clock().Sub(p.At) > Asked
}

// Answered : Whether the person has said anything since the question
// was put to them.
//
// This is what the token alone cannot tell. The model can hold a real
// token and still be answering itself: asked and confirmed inside one
// turn, nobody ever heard the question. A message from the person
// after the moment it was asked is the only evidence that they did.
//
// A fault reads as not answered. Failing closed on a call is a
// question repeated; failing open is a stranger's phone ringing.
func Answered(ctx context.Context, heard Heard, userID string, p Pending) bool {
	if heard == nil {
		return false
	}
	said, err := heard.SaidSince(ctx, userID, p.At.Add(time.Millisecond), 1)
	if err != nil {
		return false
	}
	return len(said) > 0
}

// token : Something the model cannot have made up.
func token() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Never seen, and a predictable token is worse than none:
		// an empty one matches nothing, so the call is refused.
		return ""
	}
	return hex.EncodeToString(b[:])
}

// clock : Now, in UTC.
func (a *Asking) clock() time.Time {
	if a == nil || a.Now == nil {
		return time.Now().UTC()
	}
	return a.Now().UTC()
}
