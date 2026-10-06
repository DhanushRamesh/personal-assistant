package profile

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/calendar"
	"github.com/DhanushRamesh/personal-assistant/internal/prompt"
)

// Entries : The most diary entries given to a description.
//
// Four weeks of somebody's calendar, which is tens rather than
// hundreds. A ceiling against a diary nobody has rather than a
// number anybody is expected to reach.
const Entries = 300

// Diary : What they had planned over the same stretch.
//
// The second return is the calendars that would not be read. A
// description is written from what came back either way -- an
// absent calendar makes it thinner, not wrong -- but it is logged,
// because a diary that has quietly stopped being read looks
// exactly like a person with nothing on.
type Diary interface {
	Everywhere(ctx context.Context, userID string, from, to time.Time) ([]calendar.Event, []string, error)
}

// Planned : Their diary over the window, as the model is shown it.
//
// A separate part of the prompt from what their devices reported,
// and labelled as plans, because the two are not the same kind of
// fact and the description had only ever seen one of them. An
// entry says somebody meant to be somewhere. Whether they were is
// in the readings, if it is anywhere.
//
// The pairing is the point. A dentist at four and a stay near a
// clinic from ten to five says more than either line alone, and
// neither half could say it.
func Planned(entries []calendar.Event, where *time.Location) string {
	if where == nil {
		where = time.UTC
	}
	kept := make([]calendar.Event, 0, len(entries))
	for _, e := range entries {
		if strings.TrimSpace(e.Title) == "" {
			continue
		}
		kept = append(kept, e)
	}
	if len(kept) == 0 {
		return ""
	}
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].Starts.Before(kept[j].Starts) })

	lines := make([]string, 0, len(kept)+1)
	lines = append(lines, "What they had in their diary over the same four weeks:")
	for _, e := range kept {
		lines = append(lines, "- "+entry(e, where))
	}
	return prompt.Lines(lines...)
}

// entry : One diary entry on one line.
func entry(e calendar.Event, where *time.Location) string {
	at := e.Starts.In(where)
	when := at.Format("Mon 2 Jan") + " at " + said(at)
	if e.AllDay {
		when = at.Format("Mon 2 Jan") + ", all day"
	}

	line := fmt.Sprintf("%s: %s", when, strings.TrimSpace(e.Title))
	if w := strings.TrimSpace(e.Where); w != "" {
		line += ", at " + w
	}
	if !e.AllDay && e.Ends.After(e.Starts) {
		line += fmt.Sprintf(" (%s)", lasted(e.Ends.Sub(e.Starts)))
	}
	// Whose it is. Something they were invited to is a weaker
	// statement of intent than something they put there.
	if !e.Mine {
		line += " [invited]"
	}
	return line
}

// said : An hour as somebody would say it.
func said(at time.Time) string {
	return strings.ToLower(at.Format("3:04 pm"))
}

// lasted : How long something was down for.
func lasted(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%d minutes", int(d.Minutes()))
	case d < 2*time.Hour:
		return "an hour"
	default:
		return fmt.Sprintf("%.0f hours", d.Hours())
	}
}
