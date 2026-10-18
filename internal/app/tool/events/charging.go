package events

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"personal-assistant/internal/base/world/event"
)

// Battery, Plugged, Unplugged : The kinds this reads.
const (
	Battery   = "battery.level"
	Plugged   = "power.plugged"
	Unplugged = "power.unplugged"
)

// Readings : How many battery readings are looked at, whatever the
// caller asked for.
//
// Not the caller's limit, and that is the whole point. Asked how long
// to a full charge on 18 October 2026, the model asked for two
// readings and got 23% at 22:35 and 24% at 22:47. The phone had been
// plugged in at 22:46, so eleven of those twelve minutes were it
// draining: the rate it worked out was the discharge rate, and the
// answer was twelve hours against a true forty-five minutes.
//
// Twelve covers a quarter of an hour of charging at the rate a phone
// actually charges, and hours of standing still.
const Readings = 12

// Settle : How long after plugging in before a rate is worth taking.
//
// A phone jumps a percent or two the moment it is connected, so the
// first reading after the plug is not a rate, it is the jump. A minute
// of it is thrown away.
const Settle = time.Minute

// Moving : Whether the battery is going up or down, and how fast, said
// rather than left to be worked out.
//
// Empty when it cannot be said honestly: fewer than two readings since
// the last plug or unplug, no movement between them, or no battery
// readings at all. Saying nothing is right -- the model still has the
// readings themselves and can describe what it sees.
//
// A sentence and not a number, for the reason [A span is said, not
// counted] gives: handed two figures and a subtraction, a model does
// the subtraction in its head and gets it wrong. On that same evening
// it had 76% to go at its own figure of twelve minutes a percent --
// 912 minutes -- and said twelve hours.
func Moving(ctx context.Context, reader Reader, userID string, now time.Time) string {
	if reader == nil || userID == "" {
		return ""
	}

	levels, err := reader.Recent(ctx, userID, event.Query{
		Kind: Battery, Limit: Readings, Since: now.Add(-24 * time.Hour),
	})
	if err != nil || len(levels) < 2 {
		return ""
	}
	power, _ := reader.Recent(ctx, userID, event.Query{
		Kinds: []string{Plugged, Unplugged}, Limit: 1, Since: now.Add(-24 * time.Hour),
	})

	// Only the readings since the last plug or unplug, and only those
	// a minute past it: before that the question is about a different
	// state and the first reading after it is the jump, not a rate.
	from := time.Time{}
	charging := false
	if len(power) > 0 {
		from = power[0].OccurredAt.Add(Settle)
		charging = power[0].Kind == Plugged
	}

	kept := make([]reading, 0, len(levels))
	for _, e := range levels {
		if !from.IsZero() && e.OccurredAt.Before(from) {
			continue
		}
		at, err := strconv.Atoi(strings.TrimSpace(event.Value(e.Payload)))
		if err != nil || at < 0 || at > 100 {
			continue
		}
		kept = append(kept, reading{At: e.OccurredAt, Level: at})
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].At.Before(kept[j].At) })
	if len(kept) < 2 {
		return ""
	}

	first, last := kept[0], kept[len(kept)-1]
	moved := last.Level - first.Level
	over := last.At.Sub(first.At)
	if moved == 0 || over <= 0 {
		return ""
	}
	if (moved > 0) != charging && len(power) > 0 {
		// It is going the other way from what the plug says, which
		// happens while a phone is being used harder than it charges.
		// Said as what is happening rather than as what should be.
		charging = moved > 0
	}

	per := over / time.Duration(abs(moved))
	target := 100
	if moved < 0 {
		target = 0
	}
	left := time.Duration(abs(target-last.Level)) * per

	way := "falling"
	if moved > 0 {
		way = "charging"
	}
	return fmt.Sprintf("\n\nIt is %s at about %s per percent, from %d%% %s. At that rate it reaches %d%% in about %s. Say it that way rather than working it out again.",
		way, Span(per), last.Level, Ago(now.Sub(last.At)), target, Span(left))
}

// reading : One battery level at one moment.
type reading struct {
	At    time.Time
	Level int
}

// Span : A length of time as somebody would say it.
//
// Whole units wherever one is close enough, because "about an hour" is
// what a person says and "1.0 hours" is what a spreadsheet says. A
// decimal only when the remainder would be lost: fifteen hours and
// twelve minutes is 15.2 and not 15.
func Span(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	switch {
	case d < time.Minute:
		return count(int(d.Round(5*time.Second).Seconds()), "second")
	case d < 90*time.Minute:
		return count(int(d.Round(time.Minute).Minutes()), "minute")
	case d < 36*time.Hour:
		if off := d - d.Round(time.Hour); off < 6*time.Minute && off > -6*time.Minute {
			return count(int(d.Round(time.Hour).Hours()), "hour")
		}
		return fmt.Sprintf("%.1f hours", d.Round(6*time.Minute).Hours())
	default:
		days := d.Hours() / 24
		if off := days - float64(int(days+0.5)); off < 0.05 && off > -0.05 {
			return count(int(days+0.5), "day")
		}
		return fmt.Sprintf("%.1f days", days)
	}
}

// count : A number and its unit, one of which is not plural.
func count(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

// Ago : How long ago, for saying how fresh a reading is.
func Ago(d time.Duration) string {
	if d < 2*time.Minute {
		return "just now"
	}
	return Span(d) + " ago"
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
