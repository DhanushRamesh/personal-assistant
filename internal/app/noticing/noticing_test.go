package noticing_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/app/noticing"
	"personal-assistant/internal/domain/job"
)

func watcher(answer string, err error) noticing.Watcher {
	return noticing.Watcher{
		Ask: func(_ context.Context, _, _ string) (string, error) { return answer, err },
		Describe: func(_ context.Context, _ string) string {
			return "He charged the phone on 8 of 9 nights."
		},
		Holding: func(_ context.Context, _ string) (string, int, error) { return "none", 0, nil },
	}
}

// TestNothingIsTheOrdinaryAnswer : most hours contain nothing worth
// being woken for, and a pass that felt obliged to produce something
// would be the whole problem.
func TestNothingIsTheOrdinaryAnswer(t *testing.T) {
	got, err := watcher(`{"nothing": "nothing has moved"}`, nil).
		Wondered(context.Background(), "u")
	if err != nil {
		t.Fatalf("wondering: %v", err)
	}
	if got.Asked != "" {
		t.Errorf("it should have asked for nothing, got %q", got.Asked)
	}
}

// TestSomethingWorthHaving : and when there is, it comes back as a
// sentence somebody else can turn into a rule.
func TestSomethingWorthHaving(t *testing.T) {
	got, err := watcher(`{"rule":"tell me if the phone has not charged overnight","name":"the phone did not charge","because":"8 of 9 nights"}`, nil).
		Wondered(context.Background(), "u")
	if err != nil {
		t.Fatalf("wondering: %v", err)
	}
	if !strings.Contains(got.Asked, "charged overnight") {
		t.Errorf("asked: %q", got.Asked)
	}
	if got.Name == "" || got.Why == "" {
		t.Errorf("a rule he wrote should carry its name and its reason: %+v", got)
	}
}

// TestAnswerThatCannotBeReadWritesNothing : the safe way for this to
// fail. A missed rule costs one unremarked pattern; a misread one
// runs for ever.
func TestAnswerThatCannotBeReadWritesNothing(t *testing.T) {
	for _, answer := range []string{"I think you should...", "", "{oh dear"} {
		got, err := watcher(answer, nil).Wondered(context.Background(), "u")
		if err != nil || got.Asked != "" {
			t.Errorf("%q should have written nothing, got %+v (%v)", answer, got, err)
		}
	}
}

// TestFencedJSONIsRead : what models actually return when asked for
// JSON, and refusing it would be refusing the commonest right answer.
func TestFencedJSONIsRead(t *testing.T) {
	got, _ := watcher("```json\n{\"rule\":\"say when the laptop is unplugged all day\",\"name\":\"n\",\"because\":\"b\"}\n```", nil).
		Wondered(context.Background(), "u")
	if !strings.Contains(got.Asked, "unplugged") {
		t.Errorf("fenced JSON should be read: %q", got.Asked)
	}
}

// TestNothingKnownMeansNoRule : nothing is known about them yet, so
// nothing about them is worth a standing rule.
func TestNothingKnownMeansNoRule(t *testing.T) {
	w := watcher(`{"rule":"anything","name":"n","because":"b"}`, nil)
	w.Describe = func(context.Context, string) string { return "   " }
	got, _ := w.Wondered(context.Background(), "u")
	if got.Asked != "" {
		t.Errorf("with nothing known it should write nothing, got %q", got.Asked)
	}
}

// TestAModelThatFailsIsReported : unlike an unreadable answer, a
// broken model is a fault and is said so.
func TestAModelThatFailsIsReported(t *testing.T) {
	if _, err := watcher("", errors.New("no model")).Wondered(context.Background(), "u"); err == nil {
		t.Error("a failing model should be reported")
	}
}

// TestTheAllowance : one a day, and six hours between, taken from the
// rules themselves so a restart does not hand him a fresh allowance.
func TestTheAllowance(t *testing.T) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	mine := []job.Job{{Kind: noticing.Kind, Made: now.Add(-2 * time.Hour)}}
	if noticing.Allowed(mine, now) {
		t.Error("one written two hours ago should stop another")
	}
	old := []job.Job{{Kind: noticing.Kind, Made: now.Add(-30 * time.Hour)}}
	if !noticing.Allowed(old, now) {
		t.Error("yesterday's should not count against today")
	}
	if !noticing.Allowed(nil, now) {
		t.Error("having written none, he may write one")
	}
	// Rules the person asked for do not spend his allowance.
	theirs := []job.Job{{Kind: "rule", Made: now.Add(-time.Minute)}}
	if !noticing.Allowed(theirs, now) {
		t.Error("their rules are not his")
	}
}
