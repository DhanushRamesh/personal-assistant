package notice

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/event"
)

const (
	// Every : How often the watcher looks at what has arrived.
	//
	// Not on arrival. Events come in bursts -- six in two seconds when
	// somebody walks through the door -- and reacting to each would
	// make six remarks out of one moment. A tick turns a burst into
	// one look.
	Every = 30 * time.Second

	// Behind : How far back a look reads.
	//
	// Longer than Every, so a thing that arrives late, or while the
	// server was restarting, is still seen. Saying the same thing
	// twice is prevented by the gate rather than by reading a window
	// so tight that things fall out of it.
	Behind = 5 * time.Minute

	// Since : How much history a judgement is made against.
	//
	// The same four weeks the description is written from, because
	// "never seen before" means the same thing in both and a remark
	// that disagreed with the description would be worse than no
	// remark.
	Since = 28 * 24 * time.Hour

	// Within : How long one look may take, including the model.
	//
	// Short. Nothing is waiting on this and nobody asked for it, so a
	// slow answer is simply not worth having: the moment it was about
	// has passed.
	Within = 20 * time.Second
)

// Events : Where what their devices reported is read from.
type Events interface {
	Recent(ctx context.Context, userID string, q event.Query) ([]event.Event, error)
}

// Ask : How the model is asked. Returns what it answered, which may
// be nothing.
type Ask func(ctx context.Context, userID, prompt string) (string, error)

// Say : How a remark reaches the person.
//
// Immediate whether it is urgent, because that decides where it goes
// rather than only what it says: a remark is company and belongs in
// the room, and a warning follows them out of it.
type Say func(ctx context.Context, userID, text string, immediate bool) error

// Watcher : Looks at what has arrived and occasionally says
// something about it.
type Watcher struct {
	// Events : Where events are read from. Required.
	Events Events
	// Gate : What may be said and when. Required; without one
	// nothing is ever said, which is the safe failure.
	Gate *Gate
	// Ask : How the model is asked to write the remark. Required.
	Ask Ask
	// Say : Where the remark goes. Required.
	Say Say
	// Notes : Where what it considered saying is kept, so that a
	// watcher which is working and one which has stopped do not look
	// the same. Optional: nil records nothing and changes nothing
	// else.
	Notes Notes
	// Now : The clock, replaceable in tests.
	Now func() time.Time
	// Logger : Where failures go. Nil is silent.
	Logger *slog.Logger
}

// Look : One pass for one person.
//
// Returns what was said, which is usually nothing. Nothing is not a
// failure here and is not logged as one: most minutes of most days
// contain nothing worth interrupting somebody for, and a watcher
// that says so is working.
func (w *Watcher) Look(ctx context.Context, userID string) (string, error) {
	return w.look(ctx, userID, Behind)
}

// Catching : How far the first look after starting reaches back.
//
// Longer than Behind, and that is the whole of it. The watcher
// sleeps for a while before its first look, so that a restart in
// the middle of an evening does not remark on whatever happened to
// be arriving; then it looks back exactly as far as it normally
// does. The two are the same length, so the window opens precisely
// where the silence ends and nothing arriving during the wait is
// ever seen.
//
// It cost a real one. A battery at 25 per cent, marked by the phone
// to be said at once, arrived at 01:39:14 between two restarts
// thirty-six seconds apart; the first look came at 01:44:42 and
// reached back to 01:39:42. The event was **twenty-six seconds**
// outside it, and nothing anywhere recorded that it had been
// missed -- it simply never became a candidate.
//
// Safe to widen because repeating is prevented elsewhere: the gate
// weighs each thing once per label, and an immediate one once per
// arrival. The worst a wide first look can do is weigh something
// that was already weighed, which it then declines.
func (w *Watcher) Catching() time.Duration { return noticeFirst() + Behind }

// First : One look that reaches back over the silence after
// starting, for the pass that happens before the ticker has ever
// fired.
func (w *Watcher) First(ctx context.Context, userID string) (string, error) {
	return w.look(ctx, userID, w.Catching())
}

func (w *Watcher) look(ctx context.Context, userID string, behind time.Duration) (string, error) {
	if w == nil || w.Events == nil || w.Gate == nil || w.Ask == nil || w.Say == nil {
		return "", nil
	}

	now := w.clock()
	history, err := w.Events.Recent(ctx, userID, event.Query{
		Since: now.Add(-Since),
		Limit: Seen,
		// The position readings only. Every value is a coordinate,
		// there are more of them than of everything else together,
		// and they would fill the cap on their own. The rest of the
		// working material is kept: a player's state is the only
		// place a track is named, and a thing can be new without
		// being countable.
		Omit: []string{event.Fixed},
	})
	if err != nil {
		return "", err
	}

	arrived := make([]event.Event, 0, 8)
	from := now.Add(-behind)
	for _, e := range history {
		if e.OccurredAt.After(from) && !e.OccurredAt.After(now) {
			arrived = append(arrived, e)
		}
	}
	if len(arrived) == 0 {
		return "", nil
	}

	found := Find(arrived, history, now)
	if len(found) == 0 {
		return "", nil
	}
	// Logged whatever happens next, because the ordinary output of
	// this whole feature is silence and silence has four different
	// causes. Without a line here, "nothing was said" cannot be told
	// apart from "nothing was running".
	if w.Logger != nil {
		w.Logger.InfoContext(ctx, "something might be worth saying",
			slog.String("user_id", userID),
			slog.Int("candidates", len(found)),
			slog.String("first", found[0].Label),
			slog.String("because", string(found[0].Reason)))
	}

	for _, c := range found {
		// Weighed once. A candidate stays inside the look-back window
		// for five minutes and this ticks every thirty seconds, so
		// without this one song is recorded ten times over as held.
		if w.Gate.Considered(c, now) {
			continue
		}
		allowed, why := w.Gate.Allow(userID, c, now)
		if !allowed {
			if w.Logger != nil {
				w.Logger.InfoContext(ctx, "not the moment to say it",
					slog.String("about", c.Label), slog.String("because", why))
			}
			w.note(ctx, userID, c, Held, "", why)
			continue
		}
		// One at a time. Two things worth saying in the same moment
		// are two remarks, and the second will still be worth saying
		// in twenty minutes if it was worth saying now.
		said, err := w.Ask(ctx, userID, Remark(c, now))
		if err != nil {
			w.note(ctx, userID, c, Failed, "", "could not ask: "+err.Error())
			return "", err
		}
		said = strings.TrimSpace(said)
		if said == "" || Nothing(said) {
			// The model's own refusal, and the point of asking it.
			if w.Logger != nil {
				w.Logger.InfoContext(ctx, "decided it was not worth saying",
					slog.String("about", c.Label))
			}
			w.note(ctx, userID, c, Declined, "", "")
			return "", nil
		}
		if err := w.Say(ctx, userID, said, c.Immediate); err != nil {
			w.note(ctx, userID, c, Failed, said, "could not say it: "+err.Error())
			return "", err
		}
		// What it was, so the next refusal can name it rather than
		// saying something was said.
		w.Gate.Spoke(said, now)
		w.note(ctx, userID, c, Said, said, "")
		if w.Logger != nil {
			w.Logger.InfoContext(ctx, "said something unasked",
				slog.String("user_id", userID),
				slog.String("about", c.Label),
				slog.String("because", string(c.Reason)),
				slog.String("said", said))
		}
		return said, nil
	}
	return "", nil
}

// Seen : The most events one look reads.
//
// A cap rather than a promise. Four weeks fits comfortably today and
// will not for ever, and the oldest of them only establishes whether
// a thing is familiar, which the newest answer just as well.
const Seen = 4000

// Run : Looks, for ever, on a tick.
func (w *Watcher) Run(ctx context.Context, who func(context.Context) ([]string, error)) {
	ticker := time.NewTicker(Every)
	defer ticker.Stop()

	// The first pass reaches back over the wait before it, so that
	// nothing arriving while the watcher was holding off is lost.
	first := true

	for {
		if !first {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}

		pass, cancel := context.WithTimeout(ctx, Within)
		people, err := who(pass)
		if err != nil {
			if w.Logger != nil {
				w.Logger.WarnContext(pass, "cannot tell who to watch", slog.Any("error", err))
			}
			cancel()
			continue
		}
		for _, userID := range people {
			look := w.Look
			if first {
				look = w.First
			}
			if _, err := look(pass, userID); err != nil && w.Logger != nil {
				w.Logger.WarnContext(pass, "cannot look at what arrived",
					slog.String("user_id", userID), slog.Any("error", err))
			}
		}
		first = false
		cancel()
	}
}

// clock : Now.
func (w *Watcher) clock() time.Time {
	if w.Now == nil {
		return time.Now()
	}
	return w.Now()
}

// note : Records what came of one candidate.
//
// Never an error the caller has to handle. The remark has already
// been said or already not been, and failing to write the row costs
// one line of a page nobody is waiting on.
func (w *Watcher) note(ctx context.Context, userID string, c Candidate,
	outcome Outcome, said, because string) {
	if w.Notes == nil {
		return
	}
	if err := w.Notes.Note(ctx, Noted{
		UserID: userID, At: c.At, Label: c.Label, Reason: c.Reason,
		Outcome: outcome, Said: said, Because: because,
	}); err != nil && w.Logger != nil {
		w.Logger.WarnContext(ctx, "cannot write down what it considered saying",
			slog.Any("error", err))
	}
}

// Waited : How long whoever runs this holds off before the first
// look, so that Catching can reach back over it.
//
// Set by the caller rather than read from it, because the wait is
// the server's decision and how far to catch up is this package's.
// Zero leaves the default, which is what the server uses.
var Waited = 5 * time.Minute

func noticeFirst() time.Duration {
	if Waited > 0 {
		return Waited
	}
	return 5 * time.Minute
}
