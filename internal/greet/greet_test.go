package greet_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/event"
	"github.com/DhanushRamesh/personal-assistant/internal/greet"
)

// india : The zone the hour is written in.
var india = time.FixedZone("IST", 5*3600+1800)

// evening : Half past six on a Friday.
var evening = time.Date(2026, 10, 2, 18, 30, 0, 0, india)

func happened(kind, value string, at time.Time) event.Event {
	p, _ := json.Marshal(map[string]string{"value": value})
	return event.Event{Kind: kind, Payload: p, OccurredAt: at}
}

// TestTheGapIsGivenInWords : It is what decides the greeting -- ten
// minutes is hello again and nine hours is somebody who has had a day.
func TestTheGapIsGivenInWords(t *testing.T) {
	got := greet.Prompt(greet.Told{Now: evening, Since: evening.Add(-9 * time.Hour)})
	if !strings.Contains(got, "9 hours ago") {
		t.Errorf("the gap is missing:\n%s", got)
	}
	if !strings.Contains(got, "Anything from before that") {
		t.Error("nothing stops it raising what was already discussed")
	}
}

// TestNeverSpokenToThemBefore : A first greeting has no gap to describe.
func TestNeverSpokenToThemBefore(t *testing.T) {
	got := greet.Prompt(greet.Told{Now: evening})
	if !strings.Contains(got, "not spoken to them before") {
		t.Errorf("the first greeting is not marked as one:\n%s", got)
	}
}

// TestRemindersMustAllBeSaid : The model writes them now instead of Go
// appending fixed text, so losing one is the risk that replaced it.
func TestRemindersMustAllBeSaid(t *testing.T) {
	got := greet.Prompt(greet.Told{
		Now:       evening,
		Reminders: []string{"you were going to call the plumber at four", "the milk"},
	})
	for _, want := range []string{
		"you were going to call the plumber at four", "the milk",
		"Say all of them", "leaving one out",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q from:\n%s", want, got)
		}
	}
}

// TestNoRemindersSaysNothingAboutThem : An empty heading invites the
// model to mention reminders it was not given.
func TestNoRemindersSaysNothingAboutThem(t *testing.T) {
	if got := greet.Prompt(greet.Told{Now: evening}); strings.Contains(got, "Say all of them") {
		t.Errorf("reminders are discussed with none to say:\n%s", got)
	}
}

// TestEventsAreGivenPlainlyAndNotInterpreted : What a kind means is the
// model's to work out, so a kind nobody wrote code for still arrives.
func TestEventsAreGivenPlainlyAndNotInterpreted(t *testing.T) {
	got := greet.Prompt(greet.Told{
		Now:   evening,
		Since: evening.Add(-3 * time.Hour),
		Events: []event.Event{
			happened("place.exited", "office", evening.Add(-40*time.Minute)),
			happened("call.missed", "Priya", evening.Add(-30*time.Minute)),
		},
	})
	for _, want := range []string{"place.exited office", "call.missed Priya", "5:50 pm", "6:00 pm"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q from:\n%s", want, got)
		}
	}
	// And it is told not to read them back.
	for _, want := range []string{"Never list what the devices reported", "One thing at most"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing the instruction %q", want)
		}
	}
}

// TestNothingHappenedIsSaidSo : Silence is an answer, and leaving the
// section out invites the model to imagine one.
func TestNothingHappenedIsSaidSo(t *testing.T) {
	if got := greet.Prompt(greet.Told{Now: evening}); !strings.Contains(got, "reported nothing in between") {
		t.Errorf("an empty window is not stated:\n%s", got)
	}
}

// TestOnlyTheNewestEventsAreGiven : This runs at the door, and the
// prompt must not grow with however much the phone learns to report.
func TestOnlyTheNewestEventsAreGiven(t *testing.T) {
	var evs []event.Event
	for i := range greet.Most + 50 {
		evs = append(evs, happened("network.joined", "net", evening.Add(-time.Duration(greet.Most+50-i)*time.Minute)))
	}
	got := greet.Prompt(greet.Told{Now: evening, Events: evs})
	if n := strings.Count(got, "network.joined"); n > greet.Most {
		t.Errorf("%d events given, at most %d expected", n, greet.Most)
	}
}

// TestAFailedModelStillGreets : Somebody is standing in the doorway.
func TestAFailedModelStillGreets(t *testing.T) {
	w := &greet.Writer{Ask: func(context.Context, string) (string, error) {
		return "", errors.New("the provider is down")
	}}
	said, written := w.Write(context.Background(), greet.Told{
		Now: evening, Fallback: "Good evening, sir.",
		Reminders: []string{"you were going to call the plumber."},
	})
	if written {
		t.Error("a failure was reported as written")
	}
	if !strings.Contains(said, "Good evening, sir.") {
		t.Errorf("the fixed greeting was not said: %q", said)
	}
	if !strings.Contains(said, "plumber") {
		t.Errorf("the reminder was dropped on the fallback path: %q", said)
	}
}

// TestASlowModelStillGreets : The budget is a couple of seconds.
func TestASlowModelStillGreets(t *testing.T) {
	w := &greet.Writer{Within: 50 * time.Millisecond,
		Ask: func(ctx context.Context, _ string) (string, error) {
			<-ctx.Done()
			return "", ctx.Err()
		}}
	start := time.Now()
	said, written := w.Write(context.Background(), greet.Told{Now: evening, Fallback: "Evening."})
	if written || said != "Evening." {
		t.Errorf("a slow model should fall back, got %q written=%v", said, written)
	}
	if time.Since(start) > time.Second {
		t.Errorf("waited %v for a greeting", time.Since(start))
	}
}

// TestAnEmptyAnswerIsNotSaid : A model that returns nothing has not
// greeted anybody, and silence at the door is the failure being avoided.
func TestAnEmptyAnswerIsNotSaid(t *testing.T) {
	w := &greet.Writer{Ask: func(context.Context, string) (string, error) { return "   ", nil }}
	said, written := w.Write(context.Background(), greet.Told{Now: evening, Fallback: "Evening."})
	if written || said != "Evening." {
		t.Errorf("an empty answer was used: %q written=%v", said, written)
	}
}

// TestNoModelAtAllIsHowItWorkedBefore : A server with nothing configured
// says one of the fixed sentences, exactly as it used to.
func TestNoModelAtAllIsHowItWorkedBefore(t *testing.T) {
	var w *greet.Writer
	said, written := w.Write(context.Background(), greet.Told{Now: evening, Fallback: "Evening."})
	if written || said != "Evening." {
		t.Errorf("got %q written=%v", said, written)
	}
}
