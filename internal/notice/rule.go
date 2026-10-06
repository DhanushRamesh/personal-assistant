package notice

import (
	"strconv"
	"strings"

	"github.com/DhanushRamesh/personal-assistant/internal/event"
)

// Which way a value was going when it arrived.
type way string

const (
	// either : Direction does not matter.
	either way = ""
	// falling : Lower than the reading before it.
	falling way = "falling"
	// rising : Higher than the reading before it.
	rising way = "rising"
)

// Rule : One thing worth saying the moment it happens.
//
// Owner, 6 October 2026: *"the urgent shouldn't be coming from the
// Tasker, the Tasker just sends the events, the server should
// decide at what value it should send event."*
//
// This replaces a payload flag the phone set. The reasoning for
// that flag was sound -- which battery level is worth a warning is
// the person's own preference, and the device knows it -- and it
// lost to a practical fact: changing the number meant an adb
// session editing a profile by hand, where changing it here is a
// line in config.ini. A rule that is painful to change is a rule
// that stays wrong.
//
// What is kept is the shape. A rule names a **label** -- the same
// kind-and-value a sender describes itself with, and the same one
// the counted half of the profile uses -- so nothing here knows
// what a battery is. It knows that "battery.level 20" is worth
// saying, which is a fact about this person and not about
// batteries.
type Rule struct {
	// Label : The thing, as its sender describes it.
	Label string
	// Way : Which direction the value must be moving. Empty means
	// it does not matter.
	//
	// This is what stops a phone charging through fifteen per cent
	// announcing that it is at fifteen per cent. The alternative
	// was asking whether the charger was in, which needs the power
	// events to be in the same window and is a longer way round to
	// the same answer.
	Way way
}

// ParseRules : Reads the rules as they are written in config.ini.
//
// One per entry, "<kind> <value>" with an optional "falling" or
// "rising" after it:
//
//	battery.level 20 falling
//	battery.level 100 rising
//
// Anything unreadable is dropped rather than refused. A typo in
// one rule should cost that rule and not every announcement the
// server could have made.
func ParseRules(lines []string) []Rule {
	out := make([]Rule, 0, len(lines))
	for _, line := range lines {
		// Read from the right. A value can have spaces in it -- an
		// artist is a label like any other -- so the label is
		// everything except a direction on the end, not the first
		// two words.
		var rule Rule
		fields := strings.Fields(line)
		if len(fields) > 2 {
			switch w := way(strings.ToLower(fields[len(fields)-1])); w {
			case falling, rising:
				rule.Way = w
				fields = fields[:len(fields)-1]
			}
		}
		if len(fields) < 2 {
			continue
		}
		rule.Label = strings.Join(fields, " ")
		out = append(out, rule)
	}
	return out
}

// worth : Whether this event is one the rules ask for, given what
// the same kind said before it.
func worth(rules []Rule, e event.Event, before string) bool {
	label := Label(e)
	for _, r := range rules {
		if r.Label != label {
			continue
		}
		if r.Way == either {
			return true
		}
		was, wasErr := strconv.Atoi(before)
		now, nowErr := strconv.Atoi(valueOf(e.Payload))
		if wasErr != nil || nowErr != nil {
			// Nothing to compare against, so no direction is
			// known, so this is not a crossing. Silent rather than
			// assumed: guessing yes made "falling" mean nothing at
			// the edge of the window, and a phone sitting still at
			// twenty per cent announced itself every look.
			//
			// What this costs is a crossing whose previous reading
			// fell outside the window, which is why the window is
			// wider than the one the candidates come from.
			return false
		}
		if r.Way == falling && now < was {
			return true
		}
		if r.Way == rising && now > was {
			return true
		}
	}
	return false
}
