package job_test

import (
	"context"
	"testing"
	"time"

	"personal-assistant/internal/domain/job"
	"personal-assistant/internal/domain/job/inmemory"
)

// waiting : A rule of acts, due now.
func pausing(t *testing.T, s *inmemory.Store, now time.Time, acts ...job.Act) job.Job {
	t.Helper()
	j, err := job.Make("w1", "u", "a rule that waits", "rule",
		job.Occasion{Wake: []job.Hook{{Every: "1h"}}}, acts, now)
	if err != nil {
		t.Fatalf("making it: %v", err)
	}
	j.Next = now
	s.Add(j)
	return j
}

// TestAWaitStopsTheSequenceAndComesBack : the whole of it. The acts
// before the wait run, the ones after do not, and the rule is set
// to carry on at the one after it.
func TestAWaitStopsTheSequenceAndComesBack(t *testing.T) {
	now := time.Date(2026, 10, 9, 18, 0, 0, 0, time.UTC)
	s := inmemory.New()
	h := &hands{}
	pausing(t, s, now,
		job.Act{Do: job.DoSay, Text: "first"},
		job.Act{Do: job.DoWait, For: "5m"},
		job.Act{Do: job.DoSay, Text: "second"})

	runner(s, h, now).Once(context.Background())

	if len(h.did) != 1 || h.did[0] != "say:first" {
		t.Fatalf("only the acts before the wait should have run: %v", h.did)
	}
	j, _ := s.Get("w1")
	if j.Resume != 2 {
		t.Errorf("it should carry on at the act after the wait, not %d", j.Resume)
	}
	if want := now.Add(5 * time.Minute); !j.Next.Equal(want) {
		t.Errorf("it should come back at %s, not %s", want, j.Next)
	}

	// And when it comes back, only the rest runs.
	runner(s, h, want(now)).Once(context.Background())
	if len(h.did) != 2 || h.did[1] != "say:second" {
		t.Fatalf("carrying on should run only what is left: %v", h.did)
	}
	j, _ = s.Get("w1")
	if j.Resume != 0 {
		t.Errorf("a finished run is not part-way through anything: %d", j.Resume)
	}
}

func want(now time.Time) time.Time { return now.Add(5 * time.Minute) }

// TestTheConditionsAreNotAskedAgain : a rule that waits must not
// quietly become "wait, and check nothing has changed".
func TestTheConditionsAreNotAskedAgain(t *testing.T) {
	now := time.Date(2026, 10, 9, 18, 0, 0, 0, time.UTC)
	s := inmemory.New()
	h := &hands{}
	j, err := job.Make("w2", "u", "waits with a condition", "rule",
		job.Occasion{
			Wake: []job.Hook{{Every: "1h"}},
			All:  []job.Where{{Source: "battery.level", Op: "<", Value: 20}},
		},
		[]job.Act{
			{Do: job.DoWait, For: "5m"},
			{Do: job.DoSay, Text: "late"},
		}, now)
	if err != nil {
		t.Fatalf("making it: %v", err)
	}
	j.Next = now
	s.Add(j)

	runner(s, h, now).Once(context.Background())

	// It held, so it waited. Now make the condition false and let
	// it carry on: it should speak anyway.
	h.withhold = "the battery came back up"
	runner(s, h, now.Add(5*time.Minute)).Once(context.Background())

	for _, did := range h.did {
		if did == "when:the battery came back up" {
			t.Fatal("the conditions were asked again on the way back")
		}
	}
	if len(h.did) == 0 || h.did[len(h.did)-1] != "say:late" {
		t.Errorf("it should have carried on and spoken: %v", h.did)
	}
}

// TestAWaitWithNothingAfterIt : refused where it can be seen. It
// would run, move the rule's time, come back and find nothing left
// -- which looks from outside like a rule that ran twice and did
// nothing either time.
func TestAWaitWithNothingAfterIt(t *testing.T) {
	now := time.Date(2026, 10, 9, 18, 0, 0, 0, time.UTC)
	_, err := job.Make("w3", "u", "waits for nothing", "rule",
		job.Occasion{Wake: []job.Hook{{Every: "1h"}}},
		[]job.Act{{Do: job.DoSay, Text: "hello"}, {Do: job.DoWait, For: "5m"}}, now)
	if err == nil {
		t.Fatal("a wait at the end should be refused")
	}
}

// TestAWaitThatIsNotALengthOfTime : said plainly rather than
// discovered at the moment it would have run.
func TestAWaitActThatIsNotALengthOfTime(t *testing.T) {
	now := time.Date(2026, 10, 9, 18, 0, 0, 0, time.UTC)
	_, err := job.Make("w4", "u", "waits a while", "rule",
		job.Occasion{Wake: []job.Hook{{Every: "1h"}}},
		[]job.Act{{Do: job.DoWait, For: "a bit"}, {Do: job.DoSay, Text: "hello"}}, now)
	if err == nil {
		t.Fatal(`"a bit" is not a length of time`)
	}
}
