package job_test

import (
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/domain/job"
)

// thursday : Thursday 8 October 2026, half past six in the evening,
// in the owner's own zone.
var thursday = time.Date(2026, 10, 8, 18, 30, 0, 0, india)

func next(t *testing.T, pattern string, after time.Time) time.Time {
	t.Helper()
	c, err := job.ReadCron(pattern)
	if err != nil {
		t.Fatalf("%q: %v", pattern, err)
	}
	at, err := c.Next(after, india)
	if err != nil {
		t.Fatalf("%q: %v", pattern, err)
	}
	return at
}

// TestACronPatternNamesItsNextMoment : What "at 08:00" could never
// say is when *not* to run. A weekday rule had to wake every
// morning and decline on the two days it was never meant for.
func TestACronPatternNamesItsNextMoment(t *testing.T) {
	for _, c := range []struct {
		pattern string
		want    string
	}{
		// Thursday evening, so the next weekday nine is Friday.
		{"cron 0 9 * * 1-5", "2026-10-09 09:00"},
		// Every quarter of an hour, nine till five: tomorrow, since
		// half past six is past it.
		{"cron */15 9-17 * * *", "2026-10-09 09:00"},
		// Every quarter of an hour, all day.
		{"cron */15 * * * *", "2026-10-08 18:45"},
		// The first of the month.
		{"cron 0 7 1 * *", "2026-11-01 07:00"},
		// Named days and months.
		{"cron 30 20 * * sun", "2026-10-11 20:30"},
		{"cron 0 0 25 dec *", "2026-12-25 00:00"},
		// A list.
		{"cron 0 9 1,15 * *", "2026-10-15 09:00"},
		// A range with a step.
		{"cron 0 9-17/4 * * *", "2026-10-09 09:00"},
		// From a number onwards, which cron has always read as a
		// step to the end of the field.
		{"cron 50/5 * * * *", "2026-10-08 18:50"},
		// Later the same hour.
		{"cron 45 18 * * *", "2026-10-08 18:45"},
		// Every minute: the very next one, never the one we are on.
		{"cron * * * * *", "2026-10-08 18:31"},
	} {
		got := next(t, c.pattern, thursday)
		if got.Format("2006-01-02 15:04") != c.want {
			t.Errorf("%q -> %s, want %s", c.pattern, got.Format("2006-01-02 15:04"), c.want)
		}
	}
}

// TestBothDayFieldsMeanEither : The one rule in cron that surprises
// everybody, kept because every other cron behaves this way and a
// pattern copied from elsewhere has to mean here what it meant
// there.
func TestBothDayFieldsMeanEither(t *testing.T) {
	// The first of the month OR a Monday -- not the Mondays that
	// fall on the first. From Thursday the 8th, the next Monday is
	// the 12th, which comes before the 1st of November.
	if got := next(t, "cron 0 9 1 * mon", thursday); got.Day() != 12 {
		t.Errorf("both day fields given: got the %dth, want the 12th (the next Monday)", got.Day())
	}
	// Only the weekday given, so only it decides.
	if got := next(t, "cron 0 9 * * mon", thursday); got.Day() != 12 {
		t.Errorf("weekday only: got the %dth, want the 12th", got.Day())
	}
	// Only the day of the month given, so only it decides.
	if got := next(t, "cron 0 9 1 * *", thursday); got.Day() != 1 || got.Month() != time.November {
		t.Errorf("day of month only: got %s, want 1 November", got.Format("2 Jan"))
	}
}

// TestSundayIsBothNoughtAndSeven : Every other cron accepts either,
// so a pattern written either way has to mean the same day.
func TestSundayIsBothNoughtAndSeven(t *testing.T) {
	nought := next(t, "cron 0 9 * * 0", thursday)
	seven := next(t, "cron 0 9 * * 7", thursday)
	if !nought.Equal(seven) {
		t.Errorf("0 gave %v and 7 gave %v", nought, seven)
	}
	if nought.Weekday() != time.Sunday {
		t.Errorf("landed on a %v", nought.Weekday())
	}
}

// TestAPatternThatCannotWorkIsRefusedWhenItIsWritten : The failure
// this codebase has least patience with is a rule that never fires
// and never says why.
func TestAPatternThatCannotWorkIsRefusedWhenItIsWritten(t *testing.T) {
	for _, bad := range []string{
		"cron 0 9 * *",         // four fields
		"cron 0 9 * * * *",     // six
		"cron 60 9 * * *",      // no such minute
		"cron 0 24 * * *",      // no such hour
		"cron 0 9 32 * *",      // no such day
		"cron 0 9 * 13 *",      // no such month
		"cron 0 9 * * 8",       // no such weekday
		"cron 0 17-9 * * *",    // runs backwards
		"cron */0 * * * *",     // a step of nothing
		"cron 0 9 * * fishday", // not a day
		"cron 0 9 ,, * *",      // empty parts
	} {
		if c, err := job.ReadCron(bad); err == nil {
			t.Errorf("%q was accepted as %+v", bad, c)
		}
	}

	// Parses perfectly and can never happen. It must not search for
	// ever.
	c, err := job.ReadCron("cron 0 0 30 2 *")
	if err != nil {
		t.Fatalf("the thirtieth of February would not parse: %v", err)
	}
	if at, err := c.Next(thursday, india); err == nil {
		t.Errorf("found a thirtieth of February at %v", at)
	} else if !strings.Contains(err.Error(), "never comes") {
		t.Errorf("gave an unhelpful reason: %v", err)
	}
}

// TestACronRuleIsAnOrdinarySchedule : It has to be a clock like the
// others -- repeating, picked up by the tick, and able to carry a
// trigger beside it.
func TestACronRuleIsAnOrdinarySchedule(t *testing.T) {
	const s = "cron 0 9 * * 1-5"
	if !job.Ticks(s) || !job.Repeats(s) || job.Watches(s) {
		t.Errorf("%q: ticks=%v repeats=%v watches=%v",
			s, job.Ticks(s), job.Repeats(s), job.Watches(s))
	}
	at, err := job.Following(s, thursday, india)
	if err != nil {
		t.Fatalf("Following: %v", err)
	}
	if got := at.Format("2006-01-02 15:04"); got != "2026-10-09 09:00" {
		t.Errorf("Following gave %s", got)
	}

	// Both halves, as any other clock may have.
	const both = "cron 0 9 * * 1-5 also when battery.level < 20"
	if !job.Ticks(both) || !job.Watches(both) {
		t.Errorf("%q lost a half: ticks=%v watches=%v", both, job.Ticks(both), job.Watches(both))
	}
	if _, err := job.Watched(both); err != nil {
		t.Errorf("the trigger half would not read: %v", err)
	}
	if _, err := job.Following(both, thursday, india); err != nil {
		t.Errorf("the clock half would not read: %v", err)
	}

	// A rule written with a pattern nobody could satisfy is refused
	// where it is written.
	if _, err := job.New("job_x", "usr_1", "a job", "rule",
		"cron 0 9 * 13 *", []job.Step{{Do: job.Announce, Say: "hello"}}, thursday); err == nil {
		t.Error("a rule with an impossible pattern was created")
	}
}
