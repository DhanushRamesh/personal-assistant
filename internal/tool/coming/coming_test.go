package coming_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/coming"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
	comingtool "github.com/DhanushRamesh/personal-assistant/internal/tool/coming"
)

var (
	kolkata = time.FixedZone("IST", 5*3600+1800)
	now     = time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)
)

// warnings : Judgements in memory.
type warnings struct {
	held    []coming.Judged
	settled []string
}

func (w *warnings) Held(context.Context, string, time.Time) ([]coming.Judged, error) {
	return w.held, nil
}
func (w *warnings) Settle(_ context.Context, _, eventID string, _ time.Time) error {
	w.settled = append(w.settled, eventID)
	return nil
}

// warned : One entry already spoken about, with more to say.
func warned(id, title string, spoke time.Duration) coming.Judged {
	return coming.Judged{
		EventID: id, Title: title, Starts: now.Add(48 * time.Hour),
		Leads: []coming.Lead{{Before: 72 * time.Hour}, {Before: 2 * time.Hour}},
		Said:  1, Spoke: now.Add(-spoke),
	}
}

func ask(t *testing.T, w *warnings, args string) tool.Result {
	t.Helper()
	tools := comingtool.Tools(w, comingtool.Clock{
		Now: func() time.Time { return now }, Location: kolkata,
	})
	return tools[0].Run(context.Background(), tool.Invocation{
		Caller: tool.Caller{UserID: "usr_1"},
		Args:   json.RawMessage(args),
	})
}

// TestGotItDropsTheOneJustSaid : Answering a warning names nothing,
// because the thing being answered is the last thing said.
func TestGotItDropsTheOneJustSaid(t *testing.T) {
	w := &warnings{held: []coming.Judged{
		warned("evt_old", "Barber", 3*time.Hour),
		warned("evt_new", "Attending Vignesh's wedding", time.Minute),
	}}

	got := ask(t, w, `{}`)
	if len(w.settled) != 1 || w.settled[0] != "evt_new" {
		t.Fatalf("settled %v, want the one just said", w.settled)
	}
	if !strings.Contains(got.Content, "wedding") {
		t.Errorf("the answer does not say what was dropped: %s", got.Content)
	}
	if !strings.Contains(got.Content, "stays in their diary") {
		t.Errorf("dropping a warning must not read as cancelling the thing: %s", got.Content)
	}
}

// TestItFindsTheOneTheyNamed : What reaches here came through
// speech, so "the wedding" has to find "Attending Vignesh's
// wedding".
func TestItFindsTheOneTheyNamed(t *testing.T) {
	w := &warnings{held: []coming.Judged{
		warned("evt_1", "Barber", time.Hour),
		warned("evt_2", "Attending Vignesh's wedding", 2*time.Hour),
	}}

	ask(t, w, `{"about":"the wedding"}`)
	if len(w.settled) != 1 || w.settled[0] != "evt_2" {
		t.Errorf("settled %v, want evt_2", w.settled)
	}
}

// TestItWillNotGuessBetweenTwo : Dropping the wrong warning loses a
// sentence nobody will ever know was owed, so an ambiguous name
// settles nothing and says so.
func TestItWillNotGuessBetweenTwo(t *testing.T) {
	w := &warnings{held: []coming.Judged{
		warned("evt_1", "Wedding of Vignesh", time.Hour),
		warned("evt_2", "Wedding reception", 2*time.Hour),
	}}

	got := ask(t, w, `{"about":"wedding"}`)
	if len(w.settled) != 0 {
		t.Errorf("it guessed: %v", w.settled)
	}
	if !strings.Contains(got.Content, "More than one") {
		t.Errorf("it did not say the name was ambiguous: %s", got.Content)
	}
}

// TestNothingUnspokenIsSettled : Settling a thing never mentioned
// would be acting on a warning nobody heard.
func TestNothingUnspokenIsSettled(t *testing.T) {
	quiet := warned("evt_1", "Something", 0)
	quiet.Spoke = time.Time{}
	w := &warnings{held: []coming.Judged{quiet}}

	got := ask(t, w, `{}`)
	if len(w.settled) != 0 {
		t.Errorf("a warning nobody heard was dropped: %v", w.settled)
	}
	if !strings.Contains(got.Content, "no warning outstanding") {
		t.Errorf("the answer is wrong: %s", got.Content)
	}
}

// TestShortWordsDoNotMatch : "wed" must not find "Wednesday
// standup".
func TestShortWordsDoNotMatch(t *testing.T) {
	w := &warnings{held: []coming.Judged{warned("evt_1", "Wednesday standup", time.Hour)}}
	if got := ask(t, w, `{"about":"wed"}`); len(w.settled) != 0 {
		t.Errorf("a fragment matched a different thing: %s", got.Content)
	}
}
