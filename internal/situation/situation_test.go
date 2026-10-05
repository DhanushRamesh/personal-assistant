package situation_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/event"
	"github.com/DhanushRamesh/personal-assistant/internal/situation"
)

var now = time.Date(2026, 10, 6, 15, 0, 0, 0, time.UTC)

func ago(d time.Duration) time.Time { return now.Add(-d) }

// reaching : One attempt to get hold of somebody. A negative length
// is a sender that gave none at all, which is what a call nobody
// answered looks like.
func reaching(who string, secs int, at time.Time) event.Event {
	p := fmt.Sprintf(`{"value":%q,"number":"+919000000001"}`, who)
	if secs >= 0 {
		p = fmt.Sprintf(`{"value":%q,"number":"+919000000001","seconds":%d}`, who, secs)
	}
	return event.Event{Kind: "call.missed", Payload: []byte(p), OccurredAt: at}
}

func level(n int, at time.Time) event.Event {
	return event.Event{Kind: "battery.level",
		Payload: []byte(fmt.Sprintf(`{"value":"%d"}`, n)), OccurredAt: at}
}

func power(on bool, at time.Time) event.Event {
	kind := "power.unplugged"
	if on {
		kind = "power.plugged"
	}
	return event.Event{Kind: kind, Payload: []byte(`{"value":""}`), OccurredAt: at}
}

// TestOnceIsOrdinaryAndTwiceIsSomebodyWhoNeedsYou : One unanswered
// call they will see for themselves. The same person trying again
// is a person who needs something, and that is the difference
// between a notification and a reason to pick the phone up.
func TestOnceIsOrdinaryAndTwiceIsSomebodyWhoNeedsYou(t *testing.T) {
	once := []event.Event{reaching("Amma", -1, ago(20*time.Minute))}
	if _, ok := situation.Trying(once, now); ok {
		t.Error("spoke about a single unanswered call")
	}

	twice := append(once, reaching("Amma", -1, ago(8*time.Minute)))
	found, ok := situation.Trying(twice, now)
	if !ok {
		t.Fatal("said nothing about somebody trying twice")
	}
	if !strings.Contains(found.Says, "Amma") {
		t.Errorf("did not name her: %q", found.Says)
	}

	// And not once they have spoken.
	spoke := append(twice, reaching("Amma", 42, ago(2*time.Minute)))
	if said, ok := situation.Trying(spoke, now); ok {
		t.Errorf("raised a call she got through on: %q", said.Says)
	}
}

// TestANamelessCallerIsNotNamed : The number is kept in the record
// and is never said aloud.
func TestANamelessCallerIsNotNamed(t *testing.T) {
	evs := []event.Event{
		reaching("unknown", -1, ago(20*time.Minute)),
		reaching("unknown", -1, ago(5*time.Minute)),
	}
	found, ok := situation.Trying(evs, now)
	if !ok {
		t.Fatal("said nothing")
	}
	if strings.Contains(found.Says, "9000000001") {
		t.Errorf("read the number out: %q", found.Says)
	}
	if !strings.Contains(found.Says, "could not name") {
		t.Errorf("gave a nameless caller a name: %q", found.Says)
	}
}

// TestACableThatIsNotIn : The failure nobody notices until morning.
// Worth saying while they are in the room and the cable is an arm's
// length away, and worth saying nowhere else.
func TestACableThatIsNotIn(t *testing.T) {
	stuck := []event.Event{
		power(true, ago(40*time.Minute)),
		level(31, ago(38*time.Minute)),
		level(31, ago(3*time.Minute)),
	}
	found, ok := situation.NotCharging(stuck, now)
	if !ok {
		t.Fatal("said nothing about a charger doing nothing for forty minutes")
	}
	if !strings.Contains(found.Says, "31 per cent") {
		t.Errorf("did not say where it is stuck: %q", found.Says)
	}

	// Rising is a charger working.
	rising := []event.Event{
		power(true, ago(40*time.Minute)),
		level(31, ago(38*time.Minute)),
		level(44, ago(3*time.Minute)),
	}
	if said, ok := situation.NotCharging(rising, now); ok {
		t.Errorf("complained about a charger that is charging: %q", said.Says)
	}

	// Not plugged in at all is not a charger problem.
	off := []event.Event{
		power(false, ago(40*time.Minute)),
		level(31, ago(38*time.Minute)),
		level(31, ago(3*time.Minute)),
	}
	if _, ok := situation.NotCharging(off, now); ok {
		t.Error("complained about a charger that is not there")
	}

	// Too soon to tell.
	soon := []event.Event{
		power(true, ago(5*time.Minute)),
		level(31, ago(4*time.Minute)),
		level(31, ago(1*time.Minute)),
	}
	if _, ok := situation.NotCharging(soon, now); ok {
		t.Error("gave the charger no time at all")
	}

	// Full is not stuck.
	full := []event.Event{
		power(true, ago(40*time.Minute)),
		level(100, ago(38*time.Minute)),
		level(100, ago(3*time.Minute)),
	}
	if said, ok := situation.NotCharging(full, now); ok {
		t.Errorf("called a full battery a broken cable: %q", said.Says)
	}

	// Nothing reported lately is nothing known lately.
	stale := []event.Event{
		power(true, ago(3*time.Hour)),
		level(31, ago(3*time.Hour)),
		level(31, ago(2*time.Hour)),
	}
	if said, ok := situation.NotCharging(stale, now); ok {
		t.Errorf("spoke from readings two hours old: %q", said.Says)
	}
}
