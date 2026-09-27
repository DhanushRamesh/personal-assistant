package remind

import (
	"fmt"
	"strings"
	"time"
)

// Delivered : What to say when handing over reminders somebody missed
// by being out of the room.
//
// One is said as it would have been said. Several are said as a group,
// because saying each in full repeats the preamble and the address for
// every one: "You should have heard this at 12:38, sir. Time to drink
// water, sir. You should have heard this at 12:51, sir. Time to go down
// and eat, sir." Nobody talks like that.
func Delivered(held []Reminder, at time.Time, loc *time.Location) string {
	if len(held) == 0 {
		return ""
	}
	if loc == nil {
		loc = time.UTC
	}
	if len(held) == 1 {
		return Spoken(held[0], at, loc)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s while you were out.", count(len(held)))
	for i := range held {
		b.WriteString(" At ")
		b.WriteString(held[i].DueAt.In(loc).Format("3:04"))
		b.WriteString(", ")
		b.WriteString(plainly(words(held[i])))
	}
	return b.String()
}

// words : What a reminder says, whichever wording fits being late.
func words(r Reminder) string {
	if late := strings.TrimSpace(r.SaidLate); late != "" {
		return late
	}
	body := strings.TrimSpace(r.Body)
	if body == "" {
		return strings.TrimSpace(r.Title)
	}
	return body
}

// plainly : One item of a list, without the address every sentence
// carries when it is said on its own.
//
// Each reminder is written to be spoken alone, so each ends "sir".
// Read out one after another they pile up, and the group has already
// been addressed once.
func plainly(said string) string {
	said = strings.TrimSpace(said)
	for _, tail := range []string{", sir.", ", sir", " sir.", " sir"} {
		if strings.HasSuffix(said, tail) {
			said = strings.TrimSuffix(said, tail)
			break
		}
	}
	said = strings.TrimRight(said, " .")
	if said == "" {
		return ""
	}
	return strings.ToLower(said[:1]) + said[1:] + "."
}

// count : A small number as the word for it.
func count(n int) string {
	switch n {
	case 2:
		return "Two things"
	case 3:
		return "Three things"
	case 4:
		return "Four things"
	default:
		return fmt.Sprintf("%d things", n)
	}
}
