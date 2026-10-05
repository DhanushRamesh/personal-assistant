package leaving

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Every : How often the diary is looked at for something to set off
// for.
//
// Often enough that the moment is not missed by much, and the moment
// has Door's worth of slack in it anyway. Looking is free: the
// expensive half is the journey, and Worth keeps that to the entries
// that could possibly matter.
const Every = 5 * time.Minute

// Diary : What is in the diary between now and a little way ahead.
type Diary func(ctx context.Context, userID string, from, to time.Time) ([]Appointment, error)

// Say : How it reaches them. Immediate, because a moment that has to
// be caught is not company.
type Say func(ctx context.Context, userID, text string) error

// Caution : One short clause worth hearing on the way out, or empty.
type Caution func(ctx context.Context, userID string) string

// Here : Whether they are in the room to hear it.
//
// The same shape as everywhere else: anything that is not a
// confident yes comes back as not known, and nothing is said.
type Here func(userID string) (here bool, known bool)

// Watcher : Looks at what is coming and says when to set off.
type Watcher struct {
	// Diary : Where what is coming is read from. Required.
	Diary Diary
	// Journey : How long it takes to get to an address. Required.
	// A zero duration or an error means nothing is said.
	Journey Journey
	// Say : Where the words go. Required.
	Say Say
	// Here : Whether anybody is listening. Required: without it
	// nothing is said, which is the safe failure.
	Here Here
	// Caution : One short clause worth hearing on the way out, or
	// empty. Optional: nil adds nothing.
	//
	// Given as words rather than as something to judge, so that what
	// counts as worth a caution lives with whoever knows. This
	// package does not know what a battery is and should not learn.
	Caution Caution
	// Now : The clock, replaceable in tests.
	Now func() time.Time
	// Logger : Where failures go. Nil is silent.
	Logger *slog.Logger

	mu   sync.Mutex
	said map[string]time.Time
}

// Told : How long one announcement stands for.
//
// Keyed on the entry and its start, so the same film on two evenings
// is two announcements and the same film found again on the next
// tick is not. Longer than the window it can be found in, so nothing
// comes back after it is forgotten.
const Told = 3 * time.Hour

// Look : One pass for one person.
//
// Returns what was said, which is usually nothing.
func (w *Watcher) Look(ctx context.Context, userID string) (string, error) {
	if w == nil || w.Diary == nil || w.Journey == nil || w.Say == nil || w.Here == nil {
		return "", nil
	}
	now := w.clock()

	coming, err := w.Diary(ctx, userID, now.Add(-Late), now.Add(Soon))
	if err != nil {
		return "", err
	}

	for _, a := range coming {
		if !Worth(a, now) || w.already(a, now) {
			continue
		}
		// The room before the route. Somebody who is out cannot hear
		// it, and the journey would be from where they are not.
		if here, known := w.Here(userID); !here || !known {
			return "", nil
		}

		takes, err := w.Journey(ctx, userID, a.Where)
		if err != nil {
			if w.Logger != nil {
				w.Logger.WarnContext(ctx, "cannot work out how far it is",
					slog.String("what", a.What), slog.Any("error", err))
			}
			continue
		}
		var caution string
		if w.Caution != nil {
			caution = w.Caution(ctx, userID)
		}
		said, ok := Leaving(a, takes, caution, now)
		if !ok {
			continue
		}
		if err := w.Say(ctx, userID, said); err != nil {
			return "", err
		}
		w.mark(a, now)
		if w.Logger != nil {
			w.Logger.InfoContext(ctx, "said when to set off",
				slog.String("user_id", userID), slog.String("what", a.What),
				slog.Duration("takes", takes), slog.String("said", said))
		}
		return said, nil
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
				w.Logger.WarnContext(ctx, "cannot tell who is going anywhere", slog.Any("error", err))
			}
			continue
		}
		for _, userID := range people {
			if _, err := w.Look(ctx, userID); err != nil && w.Logger != nil {
				w.Logger.WarnContext(ctx, "cannot look at what is coming",
					slog.String("user_id", userID), slog.Any("error", err))
			}
		}
	}
}

func (w *Watcher) key(a Appointment) string {
	return a.What + "@" + a.When.UTC().Format(time.RFC3339)
}

func (w *Watcher) already(a Appointment, now time.Time) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	when, had := w.said[w.key(a)]
	return had && now.Sub(when) < Told
}

func (w *Watcher) mark(a Appointment, now time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.said == nil {
		w.said = map[string]time.Time{}
	}
	w.said[w.key(a)] = now
	for k, when := range w.said {
		if now.Sub(when) >= Told {
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
