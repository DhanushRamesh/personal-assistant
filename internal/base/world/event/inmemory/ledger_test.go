package inmemory_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/base/world/event"
	"personal-assistant/internal/base/world/event/inmemory"
)

// ledgered : A store holding what one day of the phone looked like when
// its books were wrong: places left that were never entered, a headset
// connected twice in the same second, screen locks that carry nothing.
func ledgered(t *testing.T) *inmemory.Store {
	t.Helper()
	s := inmemory.New()
	n := 0
	add := func(source, kind, payload string, ago time.Duration) {
		n++
		at := back(ago)
		e := &event.Event{
			Kind: kind, Source: source, Device: "pixel-7",
			OccurredAt: at, ReceivedAt: at,
			DedupeKey: fmt.Sprintf("k%d", n), Payload: []byte(payload),
		}
		if _, _, err := s.Record(context.Background(), "u", []*event.Event{e}); err != nil {
			t.Fatal(err)
		}
	}
	add("tasker", "place.entered", `{"value":"office"}`, 9*time.Hour)
	add("tasker", "place.exited", `{"value":"office"}`, 8*time.Hour)
	add("tasker", "place.exited", `{"value":"office"}`, 6*time.Hour)
	add("tasker", "place.exited", `{"value":"Office"}`, 4*time.Hour)
	add("tasker", "bluetooth.connected", `{"value":"bike helmet"}`, 3*time.Hour)
	add("tasker", "bluetooth.connected", `{"value": "bike helmet"}`, 3*time.Hour-time.Second)
	add("tasker", "bluetooth.disconnected", `{"value":"bike helmet"}`, 2*time.Hour)
	add("tasker", "screen.locked", `{"value":"unknown","via":"phone"}`, 90*time.Minute)
	add("tasker", "screen.locked", `{"value":"unknown","via":"phone"}`, 60*time.Minute)
	add("tasker", "battery.level", `{"value":41}`, 30*time.Minute)
	add("tasker", "power.plugged", `{"level":"40"}`, 20*time.Minute)
	// The server's own report shares the place family, and is not the
	// phone's: the phone's entered and exited are still a pair.
	add("server", "place.stayed", `{"value":"office"}`, 7*time.Hour)
	// Outside the window.
	add("tasker", "place.entered", `{"value":"home"}`, 30*time.Hour)
	return s
}

// Each reporter's each action is one line, counted as kept: the
// repeated report, the filler value and the missing one each show.
func TestTheLedgerCountsEachActionAsItWasKept(t *testing.T) {
	s := ledgered(t)
	lines, err := s.Ledger(context.Background(), "u", back(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]event.Line{}
	for _, l := range lines {
		by[l.Source+" "+l.Kind] = l
	}
	if l := by["tasker place.exited"]; l.Count != 3 {
		t.Errorf("place.exited counted %d, want 3", l.Count)
	}
	if l := by["tasker place.entered"]; l.Count != 1 {
		t.Errorf("place.entered counted %d, want 1 inside the window", l.Count)
	}
	if l := by["tasker bluetooth.connected"]; l.Repeats != 1 {
		t.Errorf("a report sent twice a second apart counted %d repeats, want 1", l.Repeats)
	}
	if l := by["tasker screen.locked"]; l.Unknown != 2 || l.Valueless != 0 {
		t.Errorf("screen.locked unknown=%d valueless=%d, want 2 and 0", l.Unknown, l.Valueless)
	}
	if l := by["tasker power.plugged"]; l.Valueless != 1 {
		t.Errorf("a payload with no value counted %d valueless, want 1", l.Valueless)
	}
	if l := by["tasker battery.level"]; l.Valueless != 0 {
		t.Error("a number is a value")
	}
	if l := by["tasker screen.locked"]; l.Every() != 30*time.Minute {
		t.Errorf("every %s, want 30m", l.Every())
	}
}

// Entered and exited are a pair, counted per place and per reporter;
// three exits from one entry is the fault the ledger exists to show.
func TestPairsThatDoNotBalanceAreFound(t *testing.T) {
	s := ledgered(t)
	since := back(24 * time.Hour)
	lines, _ := s.Ledger(context.Background(), "u", since)
	paired := event.Paired(lines)

	var kinds []string
	for _, two := range paired {
		kinds = append(kinds, two[0], two[1])
	}
	values, err := s.Values(context.Background(), "u", since, kinds)
	if err != nil {
		t.Fatal(err)
	}
	var office, helmet *event.Pair
	for _, p := range event.Pairs(paired, values) {
		switch {
		case p.Family == "place" && p.Value == "office":
			office = &p
		case p.Family == "bluetooth" && p.Value == "bike helmet":
			helmet = &p
		}
	}
	if office == nil || office.NA != 1 || office.NB != 3 || !office.Off() {
		t.Errorf("office = %+v, want 1 entered against 3 exited, off", office)
	}
	if helmet == nil || helmet.Off() {
		t.Errorf("helmet = %+v, want two against one, which is within the window's edges", helmet)
	}
	if _, ok := paired[event.Reporter{Source: "server", Device: "pixel-7", Family: "place"}]; ok {
		t.Error("the server's one place action was taken for a pair")
	}
}

// Two actions that share a family and nothing else are not a pair; a
// value that never changes is flagged whatever the word is; and an
// action is silent only when it is quieter than it ever was.
func TestTheLedgerKnowsAPairAFillerAndASilence(t *testing.T) {
	s := inmemory.New()
	n := 0
	add := func(kind, payload string, at time.Time) {
		n++
		e := &event.Event{Kind: kind, Source: "tasker", Device: "pixel-7", OccurredAt: at, ReceivedAt: at,
			DedupeKey: fmt.Sprintf("k%d", n), Payload: []byte(payload)}
		if _, _, err := s.Record(context.Background(), "u", []*event.Event{e}); err != nil {
			t.Fatal(err)
		}
	}
	// Battery readings every five minutes for two days, then nothing for
	// three hours; and battery.full once with a value of its own.
	start := back(51 * time.Hour)
	for at := start; at.Before(back(3 * time.Hour)); at = at.Add(5 * time.Minute) {
		add("battery.level", fmt.Sprintf(`{"value":%d}`, at.Minute()), at)
	}
	add("battery.full", `{"value":"unknown"}`, back(20*time.Hour))
	// The laptop's way of leaving: always "none".
	for i := 0; i < 25; i++ {
		add("wifi.left", `{"value":"none"}`, start.Add(time.Duration(i)*time.Hour))
	}
	// Music, now and then, last heard a day ago after gaps of two days.
	for _, ago := range []time.Duration{50, 48, 26, 24} {
		for j := 0; j < 6; j++ {
			add("music.state", fmt.Sprintf(`{"value":"track %d"}`, j), back(ago*time.Hour+time.Duration(j)*time.Minute))
		}
	}

	since := back(52 * time.Hour)
	lines, _ := s.Ledger(context.Background(), "u", since)
	paired := event.Paired(lines)
	var kinds []string
	for _, two := range paired {
		kinds = append(kinds, two[0], two[1])
	}
	values, _ := s.Values(context.Background(), "u", since, kinds)
	pairs := event.Pairs(paired, values)
	faults := event.Faults(lines, pairs, when, time.UTC)

	for _, p := range pairs {
		if p.Family == "battery" {
			t.Fatalf("a battery level was paired with the battery being full: %+v", p)
		}
	}
	said := ""
	for _, f := range faults {
		said += f.Kind + ": " + f.Says + "\n"
	}
	for _, want := range []string{`wifi.left: all 25 say "none"`, "battery.level: nothing since"} {
		if !strings.Contains(said, want) {
			t.Errorf("missing %q in:\n%s", want, said)
		}
	}
	if strings.Contains(said, "music.state: nothing since") {
		t.Errorf("music that went quiet as it always does was called silent:\n%s", said)
	}
}
