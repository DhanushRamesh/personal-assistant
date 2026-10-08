package situation_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/base/world/event"
	"personal-assistant/internal/domain/situation"
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

// told : What Home Assistant sends when the phone's own charging
// state changes -- the state in the kind, the socket and the level
// in the payload.
func told(state string, lvl int, at time.Time) event.Event {
	p := `{"value":"ac"}`
	if lvl >= 0 {
		p = fmt.Sprintf(`{"value":"ac","level":"%d"}`, lvl)
	}
	return event.Event{Kind: "power." + state, Payload: []byte(p), OccurredAt: at}
}

// TestBeingToldBeatsWorkingItOut : The phone knows whether current
// is going in and says so. Where it has said, the twenty minutes of
// arithmetic underneath are not consulted at all -- which is the
// whole reason the sender was added, since a level that has not
// moved cannot tell a dead charger from a phone asleep in a pocket.
func TestBeingToldBeatsWorkingItOut(t *testing.T) {
	// Said outright five minutes ago. No waiting for a level to sit
	// still, and the level it came with is the one quoted.
	flat := []event.Event{
		power(true, ago(6*time.Minute)),
		told("not_charging", 41, ago(5*time.Minute)),
	}
	found, ok := situation.NotCharging(flat, now)
	if !ok {
		t.Fatal("said nothing when the phone said nothing was going in")
	}
	if !strings.Contains(found.Says, "41 per cent") {
		t.Errorf("did not say where it had got to: %q", found.Says)
	}

	// A cable being seated, and a hot phone pausing to cool, both
	// read as not_charging for a few seconds. Neither is a fault.
	seating := []event.Event{
		power(true, ago(1*time.Minute)),
		told("not_charging", 41, ago(30*time.Second)),
	}
	if said, ok := situation.NotCharging(seating, now); ok {
		t.Errorf("complained before the cable had settled: %q", said.Says)
	}

	// Saying charging is not the same as getting anywhere. Forty
	// minutes of current that never shows is a charger or a cable
	// too weak to matter, and the cable being seated is exactly why
	// it must not be blamed.
	weak := []event.Event{
		power(true, ago(41*time.Minute)),
		told("charging", 31, ago(40*time.Minute)),
		level(31, ago(38*time.Minute)),
		level(31, ago(3*time.Minute)),
	}
	found, ok = situation.NotCharging(weak, now)
	if !ok {
		t.Fatal("said nothing about forty minutes of charging that gained nothing")
	}
	if !strings.Contains(found.Says, "31 per cent") {
		t.Errorf("did not say where it is stuck: %q", found.Says)
	}
	if strings.Contains(found.Says, "may not be in properly") {
		t.Errorf("blamed a cable the phone says current is going through: %q", found.Says)
	}

	// Rising is a charger working, whatever it is called.
	rising := []event.Event{
		power(true, ago(41*time.Minute)),
		told("charging", 31, ago(40*time.Minute)),
		level(31, ago(38*time.Minute)),
		level(44, ago(3*time.Minute)),
	}
	if said, ok := situation.NotCharging(rising, now); ok {
		t.Errorf("complained about a charger that is charging: %q", said.Says)
	}

	// Near the top it stands still because there is nowhere left to
	// go, and that is the one time charging and flat is innocent.
	topped := []event.Event{
		power(true, ago(41*time.Minute)),
		told("charging", 99, ago(40*time.Minute)),
		level(99, ago(38*time.Minute)),
		level(99, ago(3*time.Minute)),
	}
	if said, ok := situation.NotCharging(topped, now); ok {
		t.Errorf("called a nearly full battery a weak charger: %q", said.Says)
	}

	// Off the cable entirely, or done with it.
	for _, state := range []string{"discharging", "full"} {
		off := []event.Event{
			power(true, ago(41*time.Minute)),
			told(state, 31, ago(40*time.Minute)),
			level(31, ago(38*time.Minute)),
			level(31, ago(3*time.Minute)),
		}
		if said, ok := situation.NotCharging(off, now); ok {
			t.Errorf("complained about a phone that is %s: %q", state, said.Says)
		}
	}

	// A sender that left the level out still gets the sentence,
	// just without a number in it.
	bare := []event.Event{told("not_charging", -1, ago(5*time.Minute))}
	found, ok = situation.NotCharging(bare, now)
	if !ok {
		t.Fatal("said nothing because the level was missing")
	}
	if strings.Contains(found.Says, "per cent") {
		t.Errorf("invented a level it was never sent: %q", found.Says)
	}

	// A phone that said charging and is getting on with it says
	// nothing at all, which is the ordinary case and has to stay
	// quiet or the other half is noise.
	fine := []event.Event{
		power(true, ago(41*time.Minute)),
		told("charging", 31, ago(40*time.Minute)),
		level(31, ago(38*time.Minute)),
		level(52, ago(3*time.Minute)),
	}
	if said, ok := situation.NotCharging(fine, now); ok {
		t.Errorf("interrupted a charge that is going fine: %q", said.Says)
	}

	// The newest word wins. Told it was charging, then told it had
	// stopped.
	stopped := []event.Event{
		power(true, ago(2*time.Hour)),
		told("charging", 31, ago(2*time.Hour)),
		told("not_charging", 52, ago(10*time.Minute)),
	}
	if _, ok := situation.NotCharging(stopped, now); !ok {
		t.Error("kept quiet after the phone said charging had stopped")
	}

	// Said once, said under one name, however often it is asked.
	again, _ := situation.NotCharging(flat, now.Add(time.Minute))
	if again.Key != found.Key && again.Key == "" {
		t.Error("gave the same cable two different keys")
	}
}
