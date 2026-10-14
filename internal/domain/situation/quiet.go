package situation

import (
	"sort"
	"strings"
	"time"

	"personal-assistant/internal/base/world/event"
)

const (
	// Silence : How long without a word from a device is wrong.
	//
	// Measured over five days of this phone: the gap between one
	// report and the next is two minutes at the median and twelve
	// and a half at the ninety-ninth percentile. Forty-five minutes
	// is three times the worst ordinary gap, so it is not reached
	// by a quiet afternoon.
	//
	// The number only means that with position readings counted. A
	// phone with the screen off and nobody calling says nothing for
	// hours; what it never stops doing is reporting where it is.
	// That is the heartbeat, and the watcher stopped omitting it
	// for this check.
	//
	// Used twice, and the second use is what makes it safe on a
	// device nobody measured: a silence this long only means
	// something where the device had not already gone this quiet
	// while it was working.
	Silence = 45 * time.Minute

	// Chatty : How many reports in the window before a device's
	// silence is evidence of anything.
	//
	// Something that reported once and stopped may simply have had
	// one thing to say. A floor and nothing more -- the test that
	// does the work is [Quiet]'s, that the device was reporting
	// steadily rather than merely often.
	Chatty = 5
)

// Quiet : That a device which reports constantly has stopped.
//
// Owner, after three hours of events never arriving and nobody
// noticing until he asked: the server watched a phone that reports
// every few minutes say nothing at all, and had no opinion about
// it.
//
// Worth saying because the failure is invisible from both ends. The
// phone is fine and believes it is sending; the server is fine and
// has nothing to report on. Only the gap between them is wrong, and
// nothing was looking at the gap.
//
// Measured on when the server was *told*, not when things happened.
// A phone off the network spools what it sees and delivers the lot
// on reconnection, so by the time anybody reads the events they
// look continuous -- the three hours of silence are only visible in
// when each one arrived.
//
// "Reports constantly" is the device's own rhythm and not a count.
// Counting was wrong on the laptop, which says one thing in its
// life -- that it joined or left a network -- in pairs five seconds
// apart. Six of those inside a minute cleared a threshold of five
// and made its ordinary overnight quiet a fault: measured over
// three days, twelve of its twenty-eight gaps were longer than
// [Silence] and the ninetieth percentile was eight hours. So a
// device qualifies by never having gone this quiet while it was
// working, which is a question about that device and needs nothing
// measured in advance.
func Quiet(evs []event.Event, now time.Time) (Found, bool) {
	heard := map[string][]time.Time{}
	// The start of the evidence rather than the start of the window
	// the caller meant, so this stays a function of what it was
	// given. They are the same thing in practice: something reports
	// within the first minutes of any window three hours wide.
	from := time.Time{}
	for _, e := range evs {
		if e.Source == "" || e.ReceivedAt.IsZero() {
			continue
		}
		who := e.Source
		if e.Device != "" {
			who = e.Device
		}
		heard[who] = append(heard[who], e.ReceivedAt)
		if from.IsZero() || e.ReceivedAt.Before(from) {
			from = e.ReceivedAt
		}
	}

	// Sorted, because a map is in no order and a check that named a
	// different device on each pass would be announced twice.
	devices := make([]string, 0, len(heard))
	for who := range heard {
		devices = append(devices, who)
	}
	sort.Strings(devices)

	for _, who := range devices {
		at := heard[who]
		if len(at) < Chatty {
			continue
		}
		sort.Slice(at, func(i, j int) bool { return at[i].Before(at[j]) })

		last := at[len(at)-1]
		if now.Sub(last) < Silence {
			continue
		}
		if longest(from, at) >= Silence {
			// It goes this quiet anyway. Nothing has gone wrong
			// that this device's own week does not do.
			continue
		}
		return Found{
			// When it fell silent and not how long ago, so one
			// outage is one sentence for as long as it lasts.
			// Saying the minutes made a new sentence every run,
			// and the engine's guard against repeating itself is
			// the sentence.
			Says: "I have not heard anything from " + named(who) + " since " +
				last.In(now.Location()).Format("3:04 pm") +
				", sir. It usually reports every few minutes.",
		}, true
	}
	return Found{}, false
}

// QuietExcept : [Quiet], leaving out what the machine the assistant runs
// on reports about itself, under the source name host.
//
// That machine cannot have gone quiet while this is running on it, and
// what it reports is not a heartbeat: the laptop says only that it
// joined or left a network. A day of office wifi dropping every few
// minutes made that look like a rhythm, and at home, on a network that
// held, the server announced at a quarter to one that the laptop it was
// running on had not been heard from since 11:57 pm. Empty host leaves
// everything in.
func QuietExcept(host string) Check {
	host = strings.TrimSpace(host)
	return func(evs []event.Event, now time.Time) (Found, bool) {
		if host == "" {
			return Quiet(evs, now)
		}
		kept := make([]event.Event, 0, len(evs))
		for _, e := range evs {
			if e.Source != host {
				kept = append(kept, e)
			}
		}
		return Quiet(kept, now)
	}
}

// longest : The longest a device went without a word while it was
// still reporting.
//
// Counted from the start of the evidence, so a device that said
// nothing for the first two hours of the window and then spoke in a
// burst is a device that goes quiet for two hours.
func longest(from time.Time, at []time.Time) time.Duration {
	worst, prev := time.Duration(0), from
	for _, t := range at {
		if gap := t.Sub(prev); gap > worst {
			worst = gap
		}
		prev = t
	}
	return worst
}

// named : A device as somebody would refer to it.
//
// The identifier a device gives itself is for matching, not for
// saying out loud. "pixel-7" read aloud is a product number.
func named(who string) string {
	switch {
	case strings.HasPrefix(who, "pixel"), who == "tasker":
		return "your phone"
	case strings.Contains(who, "laptop"), strings.HasPrefix(who, "dhanush"):
		return "your laptop"
	}
	return who
}
