package assist

import (
	"strings"
	"testing"
	"time"
)

// TestAHangUpSaysWhenItReallyHappened : The transcript is what the
// assistant reads back as its own past, so a time in it has to be the
// time that happened.
//
// "Thirty seconds" was written when every measured hang-up was at
// 29.98. Of 144 turns labelled that way since, half were cut at 1.8
// seconds -- so a sentence written to correct a false account of why a
// turn ended had itself become one.
func TestAHangUpSaysWhenItReallyHappened(t *testing.T) {
	quick := callerLeft(1800 * time.Millisecond)
	if strings.Contains(quick, "thirty seconds") {
		t.Errorf("a turn cut at 1.8s says thirty seconds: %q", quick)
	}
	if !strings.Contains(quick, "cut short") || !strings.Contains(quick, "Nobody stopped it") {
		t.Errorf("the sentence lost what it is for: %q", quick)
	}

	long := callerLeft(30 * time.Second)
	if quick == long {
		t.Errorf("a turn cut at 1.8s and one cut at thirty read the same: %q", long)
	}
	t.Logf("at 1.8s: %s", quick)
	t.Logf("at 30s:  %s", long)
}
