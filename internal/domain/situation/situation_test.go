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
