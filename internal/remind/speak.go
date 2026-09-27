package remind

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/announce"
)

// LateBy : How far behind its time a reminder must be before it is said to
// be late.
//
// A couple of minutes covers the loop's own delay and the wait for the
// satellite to fall quiet, neither of which is worth remarking on.
const LateBy = 2 * time.Minute

// Speaker : Somewhere a reminder can be said.
type Speaker interface {
	// Say : Delivers the reminder, or reports why it could not.
	//
	// Reporting failure matters. A reminder nobody heard must stay pending
	// so it can be tried again, and be counted as missed rather than said.
	Say(ctx context.Context, r Reminder) error
}

// ErrNowhereToSay : Returned when nothing is configured to deliver a
// reminder. Not a fault, and not a delivery either.
var ErrNowhereToSay = errors.New("remind: nowhere to say it")

// Aloud : A Speaker that talks through a voice satellite.
type Aloud struct {
	// Announcer : Where it is said. Nil says nowhere.
	Announcer announce.Announcer
	// Location : The person's zone, for saying what time a late one was
	// due. Nil is UTC.
	Location *time.Location
	// Now : The clock, replaceable in tests. Nil uses the real one.
	Now func() time.Time
}

// Say : Speaks the reminder aloud.
func (a Aloud) Say(ctx context.Context, r Reminder) error {
	if a.Announcer == nil || !a.Announcer.Available() {
		return ErrNowhereToSay
	}

	at := time.Now().UTC()
	if a.Now != nil {
		at = a.Now().UTC()
	}
	loc := a.Location
	if loc == nil {
		loc = time.UTC
	}
	return a.Announcer.Say(ctx, Spoken(r, at, loc))
}

// Nowhere : A Speaker with nothing behind it, for a server that cannot
// speak. It refuses rather than reporting success, so a reminder nobody
// could hear is not recorded as delivered.
type Nowhere struct{}

// Say : Always fails with ErrNowhereToSay.
func (Nowhere) Say(context.Context, Reminder) error { return ErrNowhereToSay }

// Everywhere : A Speaker that tries several in turn.
//
// One delivery is enough. It fails only when every one of them did, so a
// browser that is closed does not stop the satellite saying it.
type Everywhere struct {
	// To : Where to try, in order.
	To []Speaker
	// Logger : Where a failure that did not matter goes. Optional.
	Logger *slog.Logger
}

// Say : Delivers to the first place that will take it.
func (e Everywhere) Say(ctx context.Context, r Reminder) error {
	if len(e.To) == 0 {
		return ErrNowhereToSay
	}

	var failed []error
	for _, to := range e.To {
		err := to.Say(ctx, r)
		if err == nil {
			return nil
		}
		failed = append(failed, err)
		if e.Logger != nil && !errors.Is(err, ErrNowhereToSay) {
			e.Logger.WarnContext(ctx, "a reminder could not be delivered there",
				slog.String("reminder_id", r.ID), slog.Any("error", err))
		}
	}
	return errors.Join(failed...)
}

// Spoken : What a reminder sounds like.
//
// The body alone, when it already reads as something said. A title is for
// a listing and saying it as well would have the assistant announce
// "Wake: time to get up".
//
// A late one says so first. The server having been unreachable is the
// usual reason, and a reminder said at a quarter to eleven that sounded
// exactly like one said at ten is acted on as though it were ten.
func Spoken(r Reminder, at time.Time, loc *time.Location) string {
	body := strings.TrimSpace(r.Body)
	if body == "" {
		body = strings.TrimSpace(r.Title)
	}
	if at.Sub(r.DueAt) <= LateBy {
		return body
	}

	// The past-tense wording, with the hour put in here. The grammar is
	// the model's, written when the reminder was set; the time is the
	// server's, because it is arithmetic and arithmetic is not the
	// model's to get wrong.
	if late := strings.TrimSpace(r.SaidLate); late != "" {
		return join(late, "at "+due(r.DueAt, at, loc)+", sir.")
	}

	// Nothing was written, so the body goes as it is with the hour in
	// front. Said first, because "take your tablets" heard at noon
	// sounds like now unless something says otherwise before the words.
	return "You should have heard this at " + due(r.DueAt, at, loc) + ", sir. " + body
}

// join : A sentence and the phrase that finishes it, without doubling
// the full stop the model almost certainly left on the end.
func join(said, tail string) string {
	said = strings.TrimSpace(said)
	said = strings.TrimRight(said, ".!? ")
	return said + " " + tail
}

// due : When a late reminder was due, as it would be said.
//
// The day is named only when it is not today, which within the grace
// window means it crossed midnight.
func due(dueAt, at time.Time, loc *time.Location) string {
	was, now := dueAt.In(loc), at.In(loc)
	clock := was.Format("3:04 pm")

	if was.YearDay() == now.YearDay() && was.Year() == now.Year() {
		return clock
	}
	return clock + " " + was.Format("on Monday")
}
