package logging_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/DhanushRamesh/personal-assistant/internal/base/logging"
)

// TestAPanicIsLoggedAndSurvived : The whole point. A loop that panics
// must cost that loop's turn and nothing else.
func TestAPanicIsLoggedAndSurvived(t *testing.T) {
	var out bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&out, nil))

	logging.Surviving(context.Background(), logger, "a rule pass", func() {
		var m map[string]int
		m["boom"] = 1 // assignment to entry in nil map
	})

	said := out.String()
	if !strings.Contains(said, "panic recovered") {
		t.Errorf("the panic was not logged: %s", said)
	}
	if !strings.Contains(said, "a rule pass") {
		t.Errorf("the log does not say which loop panicked: %s", said)
	}
	if !strings.Contains(said, "stack") {
		t.Errorf("the log has no stack, so nobody can find it: %s", said)
	}
}

// TestWhatComesAfterStillRuns : Surviving is only worth anything if
// the caller carries on, which for a ticking loop is the next tick.
func TestWhatComesAfterStillRuns(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))

	ran := 0
	for i := 0; i < 3; i++ {
		logging.Surviving(context.Background(), logger, "a pass", func() {
			ran++
			if ran == 2 {
				panic("the second one is bad")
			}
		})
	}
	if ran != 3 {
		t.Errorf("ran %d passes, want all 3: one panicking pass stopped the rest", ran)
	}
}

// TestNoPanicIsLeftUnsaid : A nil logger is not a reason to swallow
// one. The default handler is the one configured at startup, and some
// of the places this is needed have no logger of their own.
func TestNoPanicIsLeftUnsaid(t *testing.T) {
	var out bytes.Buffer
	was := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&out, nil)))
	defer slog.SetDefault(was)

	logging.Surviving(context.Background(), nil, "reading a message", func() {
		panic("no logger here")
	})

	if !strings.Contains(out.String(), "panic recovered") {
		t.Errorf("a panic with no logger went unsaid: %q", out.String())
	}
}
