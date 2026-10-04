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
type Say func(ctx context.Context, userID, text string) error

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
	from := now.Add(-Behind)
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
		if err := w.Say(ctx, userID, said); err != nil {
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

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
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
			if _, err := w.Look(pass, userID); err != nil && w.Logger != nil {
				w.Logger.WarnContext(pass, "cannot look at what arrived",
					slog.String("user_id", userID), slog.Any("error", err))
			}
		}
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
