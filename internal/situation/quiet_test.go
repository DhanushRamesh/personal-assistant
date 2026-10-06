package situation_test

import (
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/event"
	"github.com/DhanushRamesh/personal-assistant/internal/situation"
)

var midday = time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

// reporting : A device saying something, heard when it happened.
func reporting(device string, ago time.Duration) event.Event {
	at := midday.Add(-ago)
	return event.Event{
		Source: "tasker", Device: device, Kind: "location.fix",
		OccurredAt: at, ReceivedAt: at,
	}
}

// chatter : A device reporting every five minutes, up to ago.
func chatter(device string, from, until time.Duration) []event.Event {
	var out []event.Event
	for d := from; d >= until; d -= 5 * time.Minute {
		out = append(out, reporting(device, d))
	}
	return out
}

// Owner, after three hours of events never arriving and nobody
// noticing until he asked. The phone believes it is sending and
// the server has nothing to report on; only the gap is wrong.
func TestADeviceThatStopsReportingIsMentioned(t *testing.T) {
	// Talking all morning, nothing for the last hour and a half.
	evs := chatter("pixel-7", 3*time.Hour, 90*time.Minute)

	got, ok := situation.Quiet(evs, midday)
	if !ok {
		t.Fatal("a phone silent for ninety minutes went unmentioned")
	}
	if !strings.Contains(got.Says, "your phone") {
		t.Errorf("it read out the device identifier: %q", got.Says)
	}
	if !strings.Contains(got.Says, "hour") {
		t.Errorf("it did not say how long: %q", got.Says)
	}
}

// A phone reporting normally is not a story. Measured: the gap
// between reports is two minutes at the median and twelve and a
// half at the ninety-ninth percentile.
func TestAPhoneTalkingNormallySaysNothing(t *testing.T) {
	evs := chatter("pixel-7", 3*time.Hour, 0)
	if _, ok := situation.Quiet(evs, midday); ok {
		t.Error("a phone reporting every five minutes was called silent")
	}
}

// Something that reported once and stopped may simply have had one
// thing to say. Only a device that talks constantly has a silence
// worth remarking on.
func TestSomethingThatBarelyReportsIsNotMissed(t *testing.T) {
	evs := []event.Event{
		reporting("doorbell", 3*time.Hour),
		reporting("doorbell", 2*time.Hour),
	}
	if _, ok := situation.Quiet(evs, midday); ok {
		t.Error("a device with two reports in three hours was called silent")
	}
}

// Measured on when the server was told, not when things happened.
// A phone off the network spools what it sees and delivers the lot
// at once, so by arrival everything looks continuous -- which is
// exactly the case that went unnoticed for three hours.
func TestItIsAboutBeingHeardFromNotAboutWhenThingsHappened(t *testing.T) {
	var evs []event.Event
	for d := 3 * time.Hour; d >= 0; d -= 5 * time.Minute {
		at := midday.Add(-d)
		evs = append(evs, event.Event{
			Source: "tasker", Device: "pixel-7", Kind: "location.fix",
			// Happened all morning, and every one of them only
			// reached the server two hours ago.
			OccurredAt: at, ReceivedAt: midday.Add(-2 * time.Hour),
		})
	}
	if _, ok := situation.Quiet(evs, midday); !ok {
		t.Error("a backlog delivered two hours ago read as a phone still talking")
	}
}

// One outage is one thing said. The key holds the hour it fell
// silent, so looking again five minutes later is the same
// situation rather than a new one.
func TestOneOutageIsOneThing(t *testing.T) {
	evs := chatter("pixel-7", 3*time.Hour, 90*time.Minute)

	first, _ := situation.Quiet(evs, midday)
	later, _ := situation.Quiet(evs, midday.Add(5*time.Minute))
	if first.Key != later.Key {
		t.Errorf("the same outage got two keys: %q then %q", first.Key, later.Key)
	}
}
