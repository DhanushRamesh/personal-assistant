package notice_test

import (
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/event"
	"github.com/DhanushRamesh/personal-assistant/internal/notice"
)

// The rules as they are actually written in config.ini.
var live = notice.ParseRules([]string{
	"battery.level 25 falling", "battery.level 20 falling",
	"battery.level 15 falling", "battery.level 100 rising",
})

// draining : Readings in the order they arrived.
func draining(values ...string) []event.Event {
	out := make([]event.Event, 0, len(values))
	for i, v := range values {
		out = append(out, happened("battery.level", v, time.Duration(len(values)-i)*time.Minute))
	}
	return out
}

// Owner: *"the urgent shouldn't be coming from the Tasker, the
// Tasker just sends the events, the server should decide at what
// value it should send event."* The phone reports every per cent
// and says nothing about which of them matter.
func TestTheServerPicksTheValueWorthSaying(t *testing.T) {
	got := notice.Find(live, draining("22", "21", "20", "19"), at)

	if len(got) != 1 {
		t.Fatalf("found %d, want only the twenty: %+v", len(got), got)
	}
	if got[0].Label != "battery.level 20" {
		t.Errorf("said %q, want the twenty", got[0].Label)
	}
}

// What the direction is for. A phone charging up through fifteen
// per cent is not a phone running out, and announcing it is the
// behaviour the Tasker profile had to suppress by hand.
func TestChargingThroughAThresholdSaysNothing(t *testing.T) {
	if got := notice.Find(live, draining("13", "14", "15", "16"), at); len(got) != 0 {
		t.Errorf("a phone filling up announced that it was emptying: %+v", got)
	}
}

// And the other way: reaching full is worth knowing, and only on
// the way up.
func TestReachingFullIsSaidOnce(t *testing.T) {
	if got := notice.Find(live, draining("98", "99", "100"), at); len(got) != 1 {
		t.Errorf("reaching full was not mentioned: %+v", got)
	}
	// A phone sitting at a hundred and then dropping has not just
	// reached anything.
	if got := notice.Find(live, draining("100", "99"), at); len(got) != 0 {
		t.Errorf("coming off full was announced as reaching it: %+v", got)
	}
}

// Nothing it was not asked about. Twenty-six kinds of event arrive
// and the rules name four labels; the rest are for the profile and
// the events tool, not for interrupting somebody.
func TestNothingElseIsEverSaid(t *testing.T) {
	arrived := []event.Event{
		happened("screen.unlocked", "", time.Minute),
		happened("call.missed", "Gunalan", 2*time.Minute),
		happened("wifi.joined", "home", 3*time.Minute),
		happened("battery.level", "64", 4*time.Minute),
	}
	if got := notice.Find(live, arrived, at); len(got) != 0 {
		t.Errorf("something nobody asked about was offered: %+v", got)
	}
}

// A value can have spaces in it -- an artist is a label like any
// other -- so the direction is read off the end, not the label off
// the front.
func TestALabelMayHaveSpacesInIt(t *testing.T) {
	rules := notice.ParseRules([]string{"music.state Yuvan Shankar Raja"})
	if len(rules) != 1 || rules[0].Label != "music.state Yuvan Shankar Raja" {
		t.Fatalf("parsed as %+v", rules)
	}
	with := notice.ParseRules([]string{"music.state Yuvan Shankar Raja rising"})
	if len(with) != 1 || with[0].Label != "music.state Yuvan Shankar Raja" {
		t.Errorf("the direction ate part of the label: %+v", with)
	}
}

// A typo costs that rule and not every announcement the server
// could have made.
func TestAnUnreadableRuleIsDroppedAlone(t *testing.T) {
	rules := notice.ParseRules([]string{"battery.level 20 falling", "nonsense", ""})
	if len(rules) != 1 {
		t.Errorf("parsed %d rules from one good one and two bad: %+v", len(rules), rules)
	}
}

// No rules means no announcements, which is the honest behaviour
// for a server nobody has told what matters.
func TestNoRulesSaysNothing(t *testing.T) {
	if got := notice.Find(nil, draining("22", "21", "20"), at); len(got) != 0 {
		t.Errorf("a server with no rules announced something: %+v", got)
	}
}
