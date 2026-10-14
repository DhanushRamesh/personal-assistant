package events

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"personal-assistant/internal/app/tool"
	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/base/world/event"
)

// ledger : How well each reporter is keeping its books.
func ledger(books event.Ledgers, clock Clock) tool.Tool {
	return tool.Tool{
		Name:   "event_ledger",
		Domain: "event",
		Lists:  true,
		Purpose: "How each of the person's devices is keeping its reports: for every source and every " +
			"action it sends, how many arrived, how often they usually come, when last, and what is " +
			"wrong with them.",
		UseWhen: "To know whether a device's reports can be trusted before relying on them, to explain " +
			"why something was missed or wrong, or when asked how the devices are doing.",
		Avoid: "Not for what happened, which is event_recent; this is about the reporting itself.",
		WhenUnasked: "A fault here is about the reporting, not about the person. Do not raise it " +
			"unless what you were asked depends on it.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{Properties: map[string]tool.Property{
			"days": {Type: "integer", Description: fmt.Sprintf(
				"How many days back to look, %d by default and %d at most.", event.LedgerDays, event.LedgerMostDays)},
		}},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			if in.Caller.UserID == "" {
				return tool.Failed("This request did not come from a known person.")
			}
			var args struct {
				Days int `json:"days"`
			}
			_ = json.Unmarshal(in.Args, &args)
			days := args.Days
			if days <= 0 {
				days = event.LedgerDays
			}
			days = min(days, event.LedgerMostDays)

			now := clock.now()
			where := clock.where()
			lines, _, faults, err := event.Ledgered(ctx, books, in.Caller.UserID, now.AddDate(0, 0, -days), now, where)
			if err != nil {
				return tool.Broke("The devices' reports could not be counted.", err)
			}
			if len(lines) == 0 {
				return tool.OK(fmt.Sprintf("Their devices have reported nothing in the past %d days.", days))
			}

			var b strings.Builder
			fmt.Fprintf(&b, "What each of their devices sent in the past %d days, by source and device, "+
				"each action with how many, how often they usually come, and when last.\n", days)
			reporter := ""
			for _, l := range lines {
				if who := l.Source + " " + l.Device; who != reporter {
					reporter = who
					fmt.Fprintf(&b, "\n%s\n", strings.TrimSpace(who))
				}
				often := ""
				if every := l.Every(); every > 0 {
					often = ", every " + event.Roughly(every)
				}
				fmt.Fprintf(&b, "  %-24s %6d%s, last %s\n", l.Kind, l.Count, often,
					l.Last.In(where).Format("2 Jan 15:04"))
			}
			if len(faults) == 0 {
				b.WriteString("\nNothing wrong in how they were kept.")
			} else {
				b.WriteString("\nWhat is wrong:\n")
				for _, f := range faults {
					fmt.Fprintf(&b, "  %s %s %s: %s\n", f.Source, strings.TrimSpace(f.Device), f.Kind, f.Says)
				}
			}
			return tool.OK(strings.TrimRight(b.String(), "\n"))
		},
	}
}
