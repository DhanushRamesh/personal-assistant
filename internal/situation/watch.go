package situation

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/event"
)

const (
	// Every : How often the checks are run.
	//
	// Slower than the watcher that announces what a sender marked,
	// because nothing here is a warning from a device: these are
	// situations that have been true for a while by the time they
	// are worth saying, and will still be true in two minutes.
	Every = 2 * time.Minute

	// Behind : How far back a run reads.
	//
	// Wide enough that a charger plugged in half an hour ago is
	// still in the window, since that is the whole of what the
	// charger check is about.
	Behind = 3 * time.Hour

	// Again : How long one situation stands said.
	//
	// Long, because these are not events that recur: a cable that is
	// not in stays not in, and saying so every two minutes is how an
	// assistant gets switched off.
	Again = 6 * time.Hour
)

// Events : Where what their devices reported is read from.
type Events interface {
	Recent(ctx context.Context, userID string, q event.Query) ([]event.Event, error)
}

// Say : How it reaches them.
type Say func(ctx context.Context, userID, text string) error

// Here : Whether they are in the room. Anything that is not a
// confident yes is a no.
type Here func(userID string) (here bool, known bool)

// Watcher : Runs the checks and says the first thing that holds.
type Watcher struct {
	// Events : Where the window comes from. Required.
	Events Events
	// Checks : What to look for. Empty finds nothing, which is the
	// safe failure.
	Checks []Check
	// Say : Where the words go. Required.
	Say Say
	// Here : Whether anybody is listening. Required.
	Here Here
	// Now : The clock, replaceable in tests.
	Now func() time.Time
	// Logger : Where failures go. Nil is silent.
	Logger *slog.Logger

	mu   sync.Mutex
	said map[string]time.Time
}

// Look : One pass for one person.
//
// The first check that holds is the only one said. Two situations at
// once is two things to say, and the second is still true in two
// minutes -- a person told two things at a desk remembers neither.
func (w *Watcher) Look(ctx context.Context, userID string) (string, error) {
	if w == nil || w.Events == nil || w.Say == nil || w.Here == nil || len(w.Checks) == 0 {
		return "", nil
	}
	now := w.clock()

	// The room first. Nothing here is urgent enough to be worth
	// reading a window for somebody who cannot hear the answer.
	if here, known := w.Here(userID); !here || !known {
		return "", nil
	}

	evs, err := w.Events.Recent(ctx, userID, event.Query{
		Since: now.Add(-Behind),
		Omit:  []string{event.Fixed, event.Stayed},
	})
	if err != nil {
		return "", err
	}

	for _, check := range w.Checks {
		found, ok := check(evs, now)
		if !ok || w.already(found, now) {
			continue
		}
		if err := w.Say(ctx, userID, found.Says); err != nil {
			return "", err
		}
		w.mark(found, now)
		if w.Logger != nil {
			w.Logger.InfoContext(ctx, "said what several things together mean",
				slog.String("user_id", userID), slog.String("about", found.Key),
				slog.String("said", found.Says))
		}
		return found.Says, nil
	}
	return "", nil
}

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
		people, err := who(ctx)
		if err != nil {
			if w.Logger != nil {
				w.Logger.WarnContext(ctx, "cannot tell who to watch", slog.Any("error", err))
			}
			continue
		}
		for _, userID := range people {
			if _, err := w.Look(ctx, userID); err != nil && w.Logger != nil {
				w.Logger.WarnContext(ctx, "cannot look at what is going on",
					slog.String("user_id", userID), slog.Any("error", err))
			}
		}
	}
}

func (w *Watcher) already(f Found, now time.Time) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	when, had := w.said[f.Key]
	return had && now.Sub(when) < Again
}

func (w *Watcher) mark(f Found, now time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.said == nil {
		w.said = map[string]time.Time{}
	}
	w.said[f.Key] = now
	for k, when := range w.said {
		if now.Sub(when) >= Again {
			delete(w.said, k)
		}
	}
}

func (w *Watcher) clock() time.Time {
	if w.Now == nil {
		return time.Now()
	}
	return w.Now()
}
