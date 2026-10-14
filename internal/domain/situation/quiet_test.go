package situation_test

import (
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/base/world/event"
	"personal-assistant/internal/domain/situation"
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
	if !strings.Contains(got.Says, "10:30 am") {
		t.Errorf("it did not say when it fell silent: %q", got.Says)
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

// One outage is one thing said.
//
// The engine stops a rule repeating itself by comparing what it
// says now with what it said last time, so a sentence that counts
// the minutes defeats the only guard there is. Live, that was five
// announcements of one silence on the evening of 8 October, ten
// minutes apart, each a longer number than the last.
func TestOneOutageIsOneSentence(t *testing.T) {
	evs := chatter("pixel-7", 3*time.Hour, 90*time.Minute)

	first, _ := situation.Quiet(evs, midday)
	later, _ := situation.Quiet(evs, midday.Add(10*time.Minute))
	if first.Says != later.Says {
		t.Errorf("the same outage was said twice in different words:\n%q\n%q",
			first.Says, later.Says)
	}
}

// The laptop says one thing in its life -- that it joined or left a
// network -- and says it in pairs five seconds apart. Counting
// reports, six of those inside a minute made it a device that
// "reports constantly", and its ordinary quiet became a fault:
// measured over three days, twelve of its twenty-eight gaps were
// longer than the threshold.
//
// Live, 10 October: "Your laptop has gone quiet, sir -- nothing
// reported in the last forty-eight minutes."
func TestADeviceThatIsQuietForHoursAnywayIsNotAFault(t *testing.T) {
	var evs []event.Event
	// The phone talking throughout, which is what sets the window.
	evs = append(evs, chatter("pixel-7", 3*time.Hour, 0)...)
	// And the laptop changing network once, in a burst, two hours
	// into it and nothing since.
	for i := range 6 {
		evs = append(evs, reporting("dhanush-12514",
			time.Hour-time.Duration(i)*5*time.Second))
	}

	if got, ok := situation.Quiet(evs, midday); ok {
		t.Errorf("a laptop that is quiet for hours most days was called a fault: %q",
			got.Says)
	}
}

// And the same device is still worth watching once it does report
// steadily. Disqualifying it for ever would be the opposite
// mistake: the laptop going silent mid-afternoon while it was
// sending every few minutes is exactly the failure this is for.
func TestTheSameDeviceCountsOnceItIsSteady(t *testing.T) {
	evs := chatter("dhanush-12514", 3*time.Hour, 90*time.Minute)

	got, ok := situation.Quiet(evs, midday)
	if !ok {
		t.Fatal("a laptop reporting every five minutes and then stopping went unmentioned")
	}
	if !strings.Contains(got.Says, "your laptop") {
		t.Errorf("it read out the device identifier: %q", got.Says)
	}
}

// The machine the assistant runs on is never reported quiet: it is the
// thing doing the reporting. Its own chatter is left out, and nothing
// else is.
func TestTheAssistantsOwnMachineIsNeverQuiet(t *testing.T) {
	var evs []event.Event
	for _, e := range chatter("dhanush-12514", 3*time.Hour, 90*time.Minute) {
		e.Source, e.Kind = "laptop", "wifi.joined"
		evs = append(evs, e)
	}
	if _, ok := situation.QuietExcept("laptop")(evs, midday); ok {
		t.Error("the assistant's own machine was reported quiet")
	}
	if _, ok := situation.QuietExcept("laptop")(chatter("pixel-7", 3*time.Hour, 90*time.Minute), midday); !ok {
		t.Error("leaving out the laptop also silenced the phone")
	}
}
