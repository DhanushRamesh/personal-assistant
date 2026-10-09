package job_test

import (
	"context"
	"testing"
	"time"

	"personal-assistant/internal/domain/job"
	"personal-assistant/internal/domain/job/inmemory"
)

// TestTheMemoryDoesNotOutliveTheOccasion : the battery bug.
//
// A rule woken by a line being crossed is told what it said last
// time so it does not say it twice. That memory has to end when
// the reading comes back over the line, or the same discharge
// tomorrow is declined as the same news as the one today -- which
// is what happened: three battery rules fired at exactly the right
// readings and all three were silenced by sentences from a
// previous discharge.
func TestTheMemoryDoesNotOutliveTheOccasion(t *testing.T) {
	now := noon
	s := inmemory.New()

	j, err := job.Make("b15", "u", "battery at 15", "rule",
		job.Occasion{Wake: []job.Hook{{On: &job.Where{
			Source: "battery.level", Op: "<", Value: 15}}}},
		[]job.Act{{Do: job.DoWrite, Ask: "say the battery is low"}}, now)
	if err != nil {
		t.Fatalf("making it: %v", err)
	}
	// It spoke yesterday, and remembers saying so.
	j.Said = "Your battery has passed the level you set, sir, and is now at fourteen"
	s.Add(j)

	h := &hands{}
	r := &job.Runner{Store: s, Doer: h, Where: india,
		Now: func() time.Time { return now }}

	// The phone is charging: 40 now, 35 before. Nothing crosses
	// the line, and the rule is no longer about anything.
	r.Readings = seen{"battery.level": {at("40", 0), at("35", time.Minute)}}
	r.Landed(context.Background(), "u", []string{"battery.level"})

	got, _ := s.Get("b15")
	if got.Said != "" {
		t.Fatalf("the reading came back over the line, so the memory should have gone: %q", got.Said)
	}

	// And a rule still under the line keeps its memory, because
	// the occasion has not ended.
	j.Said = "already said"
	s.Add(j)
	r.Readings = seen{"battery.level": {at("12", 0), at("11", time.Minute)}}
	r.Landed(context.Background(), "u", []string{"battery.level"})

	got, _ = s.Get("b15")
	if got.Said != "already said" {
		t.Errorf("still under the line, so it is still the same occasion: %q", got.Said)
	}
}

// TestCrossingStillFires : the fix must not cost the thing it is
// protecting -- a real crossing still runs the rule.
func TestCrossingStillFires(t *testing.T) {
	now := noon
	s := inmemory.New()
	j, err := job.Make("b20", "u", "battery at 20", "rule",
		job.Occasion{Wake: []job.Hook{{On: &job.Where{
			Source: "battery.level", Op: "<", Value: 20}}}},
		[]job.Act{{Do: job.DoSay, Text: "twenty percent, sir"}}, now)
	if err != nil {
		t.Fatalf("making it: %v", err)
	}
	s.Add(j)

	h := &hands{}
	r := &job.Runner{Store: s, Doer: h, Where: india,
		Now: func() time.Time { return now },
		// 19 now, 21 before: that is the crossing.
		Readings: seen{"battery.level": {at("19", 0), at("21", time.Minute)}}}
	r.Landed(context.Background(), "u", []string{"battery.level"})

	if len(h.did) != 1 || h.did[0] != "say:twenty percent, sir" {
		t.Errorf("the crossing should still have run it: %v", h.did)
	}
}
