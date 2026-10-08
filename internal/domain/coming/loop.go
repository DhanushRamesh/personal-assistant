package coming

import (
	"context"
	"log/slog"
	"time"

	"personal-assistant/internal/domain/calendar"
)

// Diary : What this needs of the calendar.
type Diary interface {
	// Everywhere : Everything across all their calendars between two
	// moments, and the names of any that could not be read.
	Everywhere(ctx context.Context, userID string, from, to time.Time) ([]calendar.Event, []string, error)
}

// Store : Where judgements are kept.
//
// Kept rather than recomputed because the judging costs a model call
// and the answer does not change: what a wedding needs doing before
// it is the same at nine in the morning as at ten.
type Store interface {
	// Held : Every judgement for entries that have not started.
	Held(ctx context.Context, userID string, from time.Time) ([]Judged, error)
	// Keep : Writes a judgement, replacing any for the same entry.
	Keep(ctx context.Context, j Judged) error
	// Spoke : Records that one more of an entry's leads was said.
	Spoke(ctx context.Context, userID, eventID string, said int, at time.Time) error
	// Settle : Records that they said they are on top of something,
	// so nothing more is said about it.
	Settle(ctx context.Context, userID, eventID string, at time.Time) error
	// Trim : Records what is left to do, once part of it is done, so
	// the warnings not yet given say that instead.
	Trim(ctx context.Context, userID, eventID, still string, at time.Time) error
	// Drop : Forgets judgements for entries no longer in the diary.
	Drop(ctx context.Context, userID string, keep []string) error
}

// Ask : How the model is asked. Returns what it answered.
type Ask func(ctx context.Context, userID, prompt string) (string, error)

// Say : How a warning reaches the person.
type Say func(ctx context.Context, userID, text string) error

// Loop : Looks ahead in the diary and speaks before things arrive.
type Loop struct {
	// Diary, Store : Required.
	Diary Diary
	Store Store
	// Ask : How an entry is judged. Required.
	Ask Ask
	// Say : Where a warning goes. Required.
	Say Say
	// Location : Their zone, which is what a written hour means.
	Location *time.Location
	// Now : The clock, replaceable in tests.
	Now func() time.Time
	// Every : How often to look. Zero selects Every.
	Every time.Duration
	// Logger : Where failures go. Nil is silent.
	Logger *slog.Logger
}

// Pass : What one look did.
type Pass struct {
	// Read : Entries seen in the diary.
	Read int
	// Judged : Entries sent to the model, because they were new or
	// had changed.
	Judged int
	// Quiet : Entries the model said needed no warning.
	Quiet int
	// Said : Warnings spoken.
	Said int
	// Failed : Judgements or warnings that went wrong, to try again.
	Failed int
}

// Who : Whose diaries to look at. The same shape the unasked
// watcher uses, and for the same reason: there is one person today
// and nothing here should assume it.
type Who func(ctx context.Context) ([]string, error)

// Run : Looks until the context ends.
func (l *Loop) Run(ctx context.Context, who Who) {
	if !l.usable() || who == nil {
		return
	}

	every := l.Every
	if every <= 0 {
		every = Every
	}

	ticker := time.NewTicker(every)
	defer ticker.Stop()

	// Once before waiting, so a restart does not cost a whole tick of
	// silence on something due in the next five minutes.
	l.all(ctx, who)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			l.all(ctx, who)
		}
	}
}

// all : One look for everybody.
func (l *Loop) all(ctx context.Context, who Who) {
	people, err := who(ctx)
	if err != nil {
		l.log(ctx, "cannot tell whose diary to look at", err)
		return
	}
	for _, userID := range people {
		l.Once(ctx, userID)
	}
}

// Once : One look at the diary.
func (l *Loop) Once(ctx context.Context, userID string) Pass {
	var pass Pass
	if !l.usable() || userID == "" {
		return pass
	}

	now := l.clock()
	ahead, missing, err := l.Diary.Everywhere(ctx, userID, now, now.Add(Ahead))
	if err != nil {
		l.log(ctx, "cannot read the diary", err)
		return pass
	}
	if len(missing) > 0 {
		l.note(ctx, "some calendars could not be read", slog.Any("calendars", missing))
	}
	pass.Read = len(ahead)

	held, err := l.Store.Held(ctx, userID, now)
	if err != nil {
		l.log(ctx, "cannot read what was judged", err)
		return pass
	}
	known := make(map[string]Judged, len(held))
	for _, j := range held {
		known[j.EventID] = j
	}

	// Anything still in the diary keeps its judgement; anything gone
	// from it loses one, so a cancelled entry cannot speak.
	living := make([]string, 0, len(ahead))

	for _, e := range ahead {
		if e.Starts.Before(now) {
			continue
		}
		living = append(living, e.ID)

		j, had := known[e.ID]
		if !had || j.Shape != Shape(e.Title, e.Starts) {
			judged, ok := l.judge(ctx, userID, e, now, &pass)
			if !ok {
				continue
			}
			j = judged
		}
		if j.Over(now) {
			continue
		}
		if i, due := j.Due(now); due {
			l.speak(ctx, j, i, now, &pass)
		}
	}

	if err := l.Store.Drop(ctx, userID, living); err != nil {
		l.log(ctx, "cannot forget what left the diary", err)
	}
	return pass
}

// judge : Asks the model what warning one entry needs, and keeps the
// answer.
//
// A judgement is kept even when it is "nothing", so the same entry is
// not sent to the model again on the next tick. Most of a diary needs
// no warning and that is the answer this would otherwise pay for
// three hundred times a day.
func (l *Loop) judge(ctx context.Context, userID string, e calendar.Event, now time.Time, pass *Pass) (Judged, bool) {
	pass.Judged++

	asking, cancel := context.WithTimeout(ctx, Within)
	defer cancel()

	answer, err := l.Ask(asking, userID, About(e, now, l.where()))
	if err != nil {
		pass.Failed++
		l.log(ctx, "cannot judge what is coming", err, slog.String("title", e.Title))
		return Judged{}, false
	}

	j := Judged{
		UserID: userID, EventID: e.ID,
		Shape: Shape(e.Title, e.Starts), Title: e.Title, Starts: e.Starts,
		Leads: Read(answer), At: now,
	}
	if len(j.Leads) == 0 {
		pass.Quiet++
	}
	if err := l.Store.Keep(ctx, j); err != nil {
		pass.Failed++
		l.log(ctx, "cannot keep a judgement", err, slog.String("title", e.Title))
		return Judged{}, false
	}
	return j, true
}

// speak : Says one lead and records that it was said.
//
// Recorded whatever the speaking did, because the two mistakes are
// not equal: a warning lost is one sentence missed, and a warning
// repeated every five minutes until the thing starts is the feature
// being turned off.
func (l *Loop) speak(ctx context.Context, j Judged, i int, now time.Time, pass *Pass) {
	said := j.Sentence(i, now, l.where())

	if err := l.Store.Spoke(ctx, j.UserID, j.EventID, i+1, now); err != nil {
		pass.Failed++
		l.log(ctx, "cannot record a warning", err, slog.String("title", j.Title))
		return
	}
	if err := l.Say(ctx, j.UserID, said); err != nil {
		pass.Failed++
		l.log(ctx, "cannot say what is coming", err, slog.String("title", j.Title))
		return
	}

	pass.Said++
	l.note(ctx, "said what is coming",
		slog.String("title", j.Title),
		slog.Duration("before", j.Leads[i].Before),
		slog.Time("starts", j.Starts))
}

// usable : Whether this has everything it needs.
func (l *Loop) usable() bool {
	return l != nil && l.Diary != nil && l.Store != nil &&
		l.Ask != nil && l.Say != nil
}

func (l *Loop) clock() time.Time {
	if l.Now != nil {
		return l.Now().UTC()
	}
	return time.Now().UTC()
}

func (l *Loop) where() *time.Location {
	if l.Location != nil {
		return l.Location
	}
	return time.UTC
}

func (l *Loop) log(ctx context.Context, msg string, err error, with ...any) {
	if l.Logger == nil {
		return
	}
	l.Logger.WarnContext(ctx, msg, append(with, slog.Any("error", err))...)
}

func (l *Loop) note(ctx context.Context, msg string, with ...any) {
	if l.Logger == nil {
		return
	}
	l.Logger.InfoContext(ctx, msg, with...)
}
