package stored_test

import (
	"testing"
	"time"

	"personal-assistant/internal/base/records/stored"
)

// TestNowMatchesWhatTheDatabaseWillKeep : The whole reason this
// package exists.
//
// MySQL's DATETIME(3) rounds what it is given. A value kept at
// nanoseconds in memory stops matching the row it was just written
// to, and every later comparison between the two is quietly wrong --
// a reminder that will not match itself, a chat that looks changed
// when nothing changed it.
//
// Truncating rather than rounding is the point: rounding can move a
// moment forward, and a moment that has not happened yet is worse
// than one slightly past.
func TestNowMatchesWhatTheDatabaseWillKeep(t *testing.T) {
	at := stored.Now()

	if at.Truncate(stored.Precision) != at {
		t.Errorf("Now() = %v, want nothing finer than %v", at, stored.Precision)
	}
	if at.Nanosecond()%int(time.Millisecond) != 0 {
		t.Errorf("Now() kept %d nanoseconds, which no column will", at.Nanosecond())
	}
	if at.Location() != time.UTC {
		t.Errorf("Now() is in %v, want UTC: the zone is the server's and the row's is not",
			at.Location())
	}
	if !at.After(time.Now().UTC().Add(-time.Second)) {
		t.Errorf("Now() = %v, which is not now", at)
	}
}

// TestTruncatingNeverMovesAMomentForward : Rounding would, and a
// timestamp in the future is a thing nothing else here expects.
func TestTruncatingNeverMovesAMomentForward(t *testing.T) {
	for _, ns := range []int{0, 1, 499_999, 500_000, 999_999} {
		at := time.Date(2026, 10, 8, 12, 0, 0, ns, time.UTC)
		got := at.Truncate(stored.Precision)
		if got.After(at) {
			t.Errorf("%d nanoseconds became %v, which is later than %v", ns, got, at)
		}
	}
}
