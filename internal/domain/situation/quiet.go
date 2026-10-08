package situation

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/base/event"
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
	Silence = 45 * time.Minute

	// Chatty : How many reports in the window make a device one
	// whose silence means something.
	//
	// Something that reported once and stopped may simply have had
	// one thing to say. This is for the devices that talk
	// constantly, because only their silence is evidence.
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
func Quiet(evs []event.Event, now time.Time) (Found, bool) {
	heard := map[string]time.Time{}
	said := map[string]int{}
	for _, e := range evs {
		if e.Source == "" || e.ReceivedAt.IsZero() {
			continue
		}
		who := e.Source
		if e.Device != "" {
			who = e.Device
		}
		said[who]++
		if e.ReceivedAt.After(heard[who]) {
			heard[who] = e.ReceivedAt
		}
	}

	// Sorted, because a map is in no order and a check that names a
	// different device on each pass would be announced twice.
	devices := make([]string, 0, len(heard))
	for who := range heard {
		devices = append(devices, who)
	}
	sort.Strings(devices)

	for _, who := range devices {
		if said[who] < Chatty {
			continue
		}
		gone := now.Sub(heard[who])
		if gone < Silence {
			continue
		}
		return Found{
			// Keyed by the device and the hour it fell silent, so
			// one outage is one thing said rather than one every
			// time the window is looked at.
			Key: "quiet:" + who + ":" + heard[who].UTC().Format("2006-01-02T15"),
			Says: "I have not heard anything from " + named(who) + " for " +
				lasted(gone) + ", sir. It usually reports every few minutes.",
		}, true
	}
	return Found{}, false
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

// lasted : A duration as somebody would say it, rounded to
// something a person would actually use.
func lasted(d time.Duration) string {
	switch {
	case d < 90*time.Minute:
		return strconv.Itoa(int(d.Round(time.Minute).Minutes())) + " minutes"
	case d < 24*time.Hour:
		hours := int(d.Round(30 * time.Minute).Hours())
		if hours <= 1 {
			return "over an hour"
		}
		return strconv.Itoa(hours) + " hours"
	default:
		return "more than a day"
	}
}
