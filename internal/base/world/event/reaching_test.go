package event

import (
	"encoding/json"
	"testing"
	"time"
)

// reach : An event saying somebody was being got hold of, carrying
// the seconds it ran where it got through.
func reach(t *testing.T, who string, at time.Time, seconds any) Event {
	t.Helper()
	into := map[string]any{"value": who, Reached: "+914412345678"}
	if seconds != nil {
		into[Seconds] = seconds
	}
	raw, err := json.Marshal(into)
	if err != nil {
		t.Fatalf("building an event: %v", err)
	}
	return Event{Kind: "call.missed", OccurredAt: at, Payload: raw}
}

var noon = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

// TestWhatGotThroughAndWhatDidNot : Two comparisons rather than a
// judgement about language, which is the reason this is here at all:
// a model asked to decide it raised a call that had been returned
// eight minutes later, four arrivals out of four.
func TestWhatGotThroughAndWhatDidNot(t *testing.T) {
	open, got := Reaching([]Event{
		reach(t, "Amma", noon, nil),
		reach(t, "Amma", noon.Add(5*time.Minute), nil),
		reach(t, "Amma", noon.Add(8*time.Minute), 142),
		reach(t, "Vishnu", noon.Add(time.Minute), nil),
	})

	if len(got) != 1 || got[0].Who != "Amma" || !got[0].Got {
		t.Fatalf("got = %+v, want the call to Amma settled", got)
	}
	if !got[0].When.Equal(noon.Add(8 * time.Minute)) {
		t.Errorf("settled at %v, want when it got through", got[0].When)
	}
	if len(open) != 1 || open[0].Who != "Vishnu" {
		t.Fatalf("open = %+v, want only Vishnu outstanding", open)
	}
	if open[0].Tries != 1 {
		t.Errorf("Vishnu tried %d times, want 1", open[0].Tries)
	}
}

// TestTriesCountFromTheFirstUnansweredOne : And the time is the last
// of them, because that is what somebody would be told.
func TestTriesCountFromTheFirstUnansweredOne(t *testing.T) {
	open, got := Reaching([]Event{
		reach(t, "Ganesh", noon, nil),
		reach(t, "Ganesh", noon.Add(time.Hour), nil),
		reach(t, "Ganesh", noon.Add(2*time.Hour), nil),
	})
	if len(got) != 0 {
		t.Fatalf("got = %+v, want nothing settled", got)
	}
	if len(open) != 1 || open[0].Tries != 3 {
		t.Fatalf("open = %+v, want three tries", open)
	}
	if !open[0].When.Equal(noon.Add(2 * time.Hour)) {
		t.Errorf("When = %v, want the most recent try", open[0].When)
	}
}

// TestSomethingSettledCanComeBack : Reached once and missed again is
// outstanding, not settled. Otherwise the second call is never
// mentioned because the first was answered.
func TestSomethingSettledCanComeBack(t *testing.T) {
	open, got := Reaching([]Event{
		reach(t, "Amma", noon, 60),
		reach(t, "Amma", noon.Add(time.Hour), nil),
	})
	if len(got) != 0 {
		t.Errorf("got = %+v, want the settled one reopened", got)
	}
	if len(open) != 1 || open[0].Tries != 1 {
		t.Fatalf("open = %+v, want Amma outstanding again", open)
	}
}

// TestAnEventThatIsNotAnAttemptIsIgnored : The number is what says
// somebody was being reached. Without it the event is about
// something else entirely and must not become a missed call.
func TestAnEventThatIsNotAnAttemptIsIgnored(t *testing.T) {
	open, got := Reaching([]Event{
		{Kind: "battery.level", OccurredAt: noon, Payload: json.RawMessage(`{"value":"9"}`)},
		{Kind: "screen.on", OccurredAt: noon, Payload: json.RawMessage(`not json at all`)},
		{Kind: "call.missed", OccurredAt: noon, Payload: nil},
	})
	if len(open) != 0 || len(got) != 0 {
		t.Errorf("open = %+v, got = %+v, want nothing taken as an attempt", open, got)
	}
}

// TestSecondsAreReadHoweverTheSenderWroteThem : Tasker sends a
// number as a string as often as not, and a string that did not
// parse must not read as having got through.
func TestSecondsAreReadHoweverTheSenderWroteThem(t *testing.T) {
	for _, c := range []struct {
		what    string
		seconds any
		through bool
	}{
		{"a number", 142.0, true},
		{"a number as a string", "142", true},
		{"a number with spaces", " 142 ", true},
		{"zero seconds", 0, false},
		{"zero as a string", "0", false},
		{"not a number", "a while", false},
		{"nothing at all", nil, false},
	} {
		t.Run(c.what, func(t *testing.T) {
			_, got := Reaching([]Event{reach(t, "Amma", noon, c.seconds)})
			if (len(got) == 1) != c.through {
				t.Errorf("seconds %v: got = %+v, want through = %v", c.seconds, got, c.through)
			}
		})
	}
}

// TestACallerWithNoNameIsNeverANumber : The number is kept in the
// record and is not for saying aloud.
func TestACallerWithNoNameIsNeverANumber(t *testing.T) {
	const cannot = "somebody the phone could not name"
	for _, who := range []string{"", "   ", Unknown, "+914412345678", "4412345678"} {
		if got := (Reach{Who: who}).Named(); got != cannot {
			t.Errorf("Named(%q) = %q, want %q", who, got, cannot)
		}
	}
	if got := (Reach{Who: "Amma"}).Named(); got != "Amma" {
		t.Errorf("Named(\"Amma\") = %q, want the name", got)
	}
}
