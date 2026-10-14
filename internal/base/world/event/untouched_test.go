package event

import (
	"testing"
	"time"
)

// at : A clock time turned into an instant, rolling past midnight into
// the following day the way the evening did.
func moment(day time.Time, clock string) time.Time {
	t, err := time.Parse("15:04:05", clock)
	if err != nil {
		panic(err)
	}
	when := time.Date(day.Year(), day.Month(), day.Day(),
		t.Hour(), t.Minute(), t.Second(), 0, time.UTC)
	if t.Hour() < 12 {
		when = when.AddDate(0, 0, 1)
	}
	return when
}

func touches(day time.Time, clocks []string) []time.Time {
	out := make([]time.Time, 0, len(clocks))
	for _, c := range clocks {
		out = append(out, moment(day, c))
	}
	return out
}

// TestQuietFindsTheNight : the thing the assistant could not do.
//
// Asked how long he had slept, it answered "roughly two to three
// hours" -- reasoned from a remembered habit instead of the evidence in
// front of it -- and called the rest "fragmentary activity through the
// night". The fragments were a five-and-three-quarter-hour hole it
// never noticed.
func TestQuietFindsTheNight(t *testing.T) {
	for _, c := range []struct {
		what    string
		day     time.Time
		clocks  []string
		from    string
		to      string
		longest time.Duration
		next    time.Duration
	}{
		{
			what:    "13 October into the 14th",
			day:     time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC),
			clocks:  lastNight,
			from:    "02:55:24",
			to:      "08:40:12",
			longest: 5*time.Hour + 44*time.Minute + 48*time.Second,
			next:    52*time.Minute + 5*time.Second,
		},
		{
			what:    "12 October into the 13th",
			day:     time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC),
			clocks:  theNightBefore,
			from:    "01:32:17",
			to:      "08:18:43",
			longest: 6*time.Hour + 46*time.Minute + 26*time.Second,
			next:    52*time.Minute + 53*time.Second,
		},
	} {
		t.Run(c.what, func(t *testing.T) {
			all := touches(c.day, c.clocks)
			got, found := Quiet(all, all[0].Add(-time.Hour), all[len(all)-1].Add(time.Hour))
			if !found {
				t.Fatal("found no quiet stretch in a night that plainly has one")
			}
			if !got.From.Equal(moment(c.day, c.from)) || !got.To.Equal(moment(c.day, c.to)) {
				t.Errorf("quiet %s to %s, want %s to %s",
					got.From.Format("15:04:05"), got.To.Format("15:04:05"), c.from, c.to)
			}
			if got.Long() != c.longest {
				t.Errorf("long = %s, want %s", got.Long(), c.longest)
			}
			if got.Next != c.next {
				t.Errorf("next longest = %s, want %s", got.Next, c.next)
			}
			if !got.Clear() {
				t.Errorf("%s against a next-longest of %s did not read as clear-cut",
					got.Long(), got.Next)
			}
			t.Logf("untouched %s to %s (%s); next longest %s, %.1f times over",
				got.From.Format("15:04"), got.To.Format("15:04"), got.Long(),
				got.Next, float64(got.Long())/float64(got.Next))
		})
	}
}

// A night he was up and down through is not clear-cut, and naming one
// stretch of it as the night would be calling a coin toss.
func TestTwoAlikeStretchesAreNotClearCut(t *testing.T) {
	day := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	all := touches(day, []string{"23:00:00", "00:00:00", "03:00:00", "03:05:00", "06:05:00"})
	got, found := Quiet(all, all[0].Add(-time.Hour), all[len(all)-1].Add(time.Hour))
	if !found {
		t.Fatal("found nothing")
	}
	if got.Long() != 3*time.Hour || got.Next != 3*time.Hour {
		t.Fatalf("long %s, next %s; want three hours each", got.Long(), got.Next)
	}
	if got.Clear() {
		t.Error("three hours against another three hours read as clear-cut")
	}
}

// Nothing to go on is said as nothing, not as a stretch of no length.
// A phone nobody touched all night says nothing about whether anybody
// slept, and one touch has no gap either side of it.
func TestNothingToGoOnFindsNothing(t *testing.T) {
	day := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	from := time.Date(2026, 10, 13, 18, 0, 0, 0, time.UTC)
	to := from.Add(18 * time.Hour)

	if _, found := Quiet(nil, from, to); found {
		t.Error("no touches at all produced a quiet stretch")
	}
	if _, found := Quiet(touches(day, []string{"23:00:00"}), from, to); found {
		t.Error("a single touch produced a quiet stretch")
	}
	// Touches outside the window are not counted, so a window with
	// nothing of its own in it finds nothing.
	outside := touches(day, []string{"19:00:00", "20:00:00"})
	if _, found := Quiet(outside, to, to.Add(time.Hour)); found {
		t.Error("touches outside the window were counted")
	}
}

// An incoming call at four in the morning is something that happened to
// him, not something he did. Counting one would cut a night in half and
// report two short sleeps where there was one long one.
func TestWhatHappensToHimIsNotATouch(t *testing.T) {
	for _, kind := range []string{"call.received", "call.missed", "call.unanswered", "location.fix", "battery.level"} {
		if Touches[kind] {
			t.Errorf("%s counts as the person touching the phone", kind)
		}
	}
	for _, kind := range []string{"screen.unlocked", "screen.locked", "call.made", "music.state"} {
		if !Touches[kind] {
			t.Errorf("%s does not count as the person touching the phone", kind)
		}
	}
}
