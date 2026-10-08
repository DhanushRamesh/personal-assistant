package speech

import (
	"testing"
	"time"
)

func TestNotedUtteranceIsFoundAgain(t *testing.T) {
	c := New()
	c.Note("Radish is 35. Recharge is 548.", 15.0)

	secs, ok := c.Was("Radish is 35. Recharge is 548.")
	if !ok {
		t.Fatal("an utterance that was noted was not found")
	}
	if secs != 15.0 {
		t.Fatalf("seconds = %v, want 15", secs)
	}
}

func TestSpacingAndCaseDoNotStopAMatch(t *testing.T) {
	c := New()
	c.Note("  Radish is 35.  ", 15.0)

	if _, ok := c.Was("radish is 35."); !ok {
		t.Fatal("the same words with different spacing did not match")
	}
}

func TestAnotherUtteranceIsNotMarked(t *testing.T) {
	c := New()
	c.Note("Radish is 35.", 15.0)

	if _, ok := c.Was("What is the time?"); ok {
		t.Fatal("an unrelated question was reported as cut off")
	}
}

// An empty transcript must not match every future empty question: a
// recording can time out having caught no words at all.
func TestEmptyIsNeverRecorded(t *testing.T) {
	c := New()
	c.Note("", 15.0)
	c.Note("   ", 15.0)

	if _, ok := c.Was(""); ok {
		t.Fatal("an empty transcript was recorded and matched")
	}
}

// Reading must not consume: Home Assistant can ask the same question twice,
// and the second attempt was cut off exactly as much as the first.
func TestReadingTwiceStillMatches(t *testing.T) {
	c := New()
	c.Note("Radish is 35.", 15.0)

	if _, ok := c.Was("Radish is 35."); !ok {
		t.Fatal("first read did not match")
	}
	if _, ok := c.Was("Radish is 35."); !ok {
		t.Fatal("the report was consumed by being read")
	}
}

func TestAnOldReportStopsMatching(t *testing.T) {
	now := time.Now()
	c := New()
	c.now = func() time.Time { return now }
	c.Note("Radish is 35.", 15.0)

	c.now = func() time.Time { return now.Add(Remember + time.Second) }
	if _, ok := c.Was("Radish is 35."); ok {
		t.Fatal("a report older than Remember still matched")
	}
}

// A bridge gone wrong must not be able to grow the process.
func TestTheStoreIsBounded(t *testing.T) {
	c := New()
	for i := range Most * 4 {
		c.Note(string(rune('a'+i%26))+string(rune('a'+i/26)), 15.0)
	}
	if len(c.seen) > Most {
		t.Fatalf("kept %d reports, more than the ceiling of %d", len(c.seen), Most)
	}
}
