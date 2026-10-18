package glance_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/app/glance"
	"personal-assistant/internal/base/records/client"
	"personal-assistant/internal/base/thinking/environment"
)

type notes struct{ asked, said, client string }

func (n *notes) Glanced(_ context.Context, _ string, c *client.Client, asked, said string, _ time.Time) {
	n.asked, n.said, n.client = asked, said, c.ID
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// One call, with the manner, what is shown and the question; the
// exchange written down after.
func TestALookIsOneCallAndIsWrittenDown(t *testing.T) {
	var calls int
	var system, asked string
	var feature environment.Feature
	n := &notes{}
	g := &glance.Glancer{
		Ask: func(ctx context.Context, s, a string) (string, error) {
			calls++
			system, asked = s, a
			if l, ok := environment.LabelOf(ctx); ok {
				feature = l.Feature
			}
			return "  The build failed in Shell: a missing import.  ", nil
		},
		Voice:  func() string { return "MANNER" },
		Notes:  n,
		Logger: quiet(),
	}
	said, err := g.Look(context.Background(), "u1", &client.Client{ID: "c1", Name: "Laptop"},
		"What am I seeing?", "In front of them: the terminal Shell.\nIts last lines:\nerror TS2304", true)
	if err != nil {
		t.Fatal(err)
	}
	if said != "The build failed in Shell: a missing import." || calls != 1 || system != "MANNER" {
		t.Errorf("said %q in %d calls with %q", said, calls, system)
	}
	if feature != environment.FeatureGlance {
		t.Errorf("labelled %q", feature)
	}
	for _, want := range []string{"What am I seeing?", "in Laptop right now", "error TS2304", "said aloud", "Lead with whatever needs them",
		"which answer you would choose"} {
		if !strings.Contains(asked, want) {
			t.Errorf("the prompt does not say %q:\n%s", want, asked)
		}
	}
	if n.asked != "What am I seeing?" || n.said != said || n.client != "c1" {
		t.Errorf("wrote %+v", n)
	}
}

// Nothing shown is nothing to say; a failed call writes nothing down.
func TestNothingShownOrAFailedCallSaysNothing(t *testing.T) {
	n := &notes{}
	g := &glance.Glancer{
		Ask:    func(context.Context, string, string) (string, error) { return "", errors.New("down") },
		Notes:  n,
		Logger: quiet(),
	}
	if _, err := g.Look(context.Background(), "u1", nil, "What is this?", "  ", false); !errors.Is(err, glance.ErrNothingShown) {
		t.Errorf("nothing shown: %v", err)
	}
	if _, err := g.Look(context.Background(), "u1", nil, "What is this?", "the page", false); err == nil {
		t.Error("a failed call answered")
	}
	if n.asked != "" {
		t.Errorf("wrote %+v", n)
	}
}

// What is shown is cut to MaxWhere; a written answer is asked for as written.
func TestTheAskingIsBoundedAndWritten(t *testing.T) {
	got := glance.Asking("what's this", "", strings.Repeat("x", glance.MaxWhere+500), false)
	if strings.Count(got, "x") > glance.MaxWhere+10 || !strings.Contains(got, "in the app right now") || !strings.Contains(got, "It is written") {
		t.Errorf("asking:\n%.300s", got)
	}
}
