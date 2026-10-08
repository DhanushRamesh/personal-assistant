package job_test

import (
	"testing"
	"time"

	"personal-assistant/internal/domain/job"
)

// TestALengthOfTimeAsSomebodyWritesIt : time.ParseDuration stops at
// hours, because beyond that a day is not always twenty-four of them.
// Here it is: these are rules, and "quiet for a week" is the thing
// somebody actually says. The alternative was asking them for 168h.
func TestALengthOfTimeAsSomebodyWritesIt(t *testing.T) {
	for _, c := range []struct {
		written string
		want    time.Duration
	}{
		{"30m", 30 * time.Minute},
		{"2h", 2 * time.Hour},
		{"1d", 24 * time.Hour},
		{"7d", 7 * 24 * time.Hour},
		{"1w", 7 * 24 * time.Hour},
		{"1.5h", 90 * time.Minute},
	} {
		got, err := job.Span(c.written)
		if err != nil {
			t.Errorf("%q: %v", c.written, err)
			continue
		}
		if got != c.want {
			t.Errorf("%q is %s, want %s", c.written, got, c.want)
		}
	}

	for _, bad := range []string{"", "a while", "0s", "-7d", "7 days", "soon", "d"} {
		if _, err := job.Span(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}
