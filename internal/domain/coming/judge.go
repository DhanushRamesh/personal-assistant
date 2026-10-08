package coming

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/base/prompt"
	"github.com/DhanushRamesh/personal-assistant/internal/domain/calendar"
)

// Asking : What the model is told before it reads an entry.
//
// The whole of the judgement is in here, and it is written as a
// method rather than a list of kinds on purpose. A list would have
// to name weddings, films, flights and dentists, would be wrong in
// another language, and would be silent about the next sort of thing
// the person puts in the diary. The question asked instead is one
// anybody could answer about anything.
var Asking = prompt.Text(
	"Something is in their diary. Say how much warning they need of it, and why.",
	"The question is how much there is to DO beforehand, not how important it is or how long it lasts.",
	"Something they only have to turn up to needs a few hours: enough to finish what they are doing and travel.",
	"Something that needs a present bought, clothes got ready, a journey worked out, somebody else arranged, money moved, or a thing collected needs days, because that doing has to fit around the rest of their week and cannot be done on the way.",
	"Not everything in a diary is an appointment.",
	"Some entries are a date somebody wrote down so that it would not be missed: nothing is scheduled, nobody has to go anywhere, and that is exactly why the warning is the whole of the point, because on the day itself there is no time left to do the thing the date was written down for.",
	"A day that belongs to another person is nearly always like that: there is something to get and something to say, and both are late on the morning of.",
	"Much of a diary still needs nothing at all.",
	"A public holiday, a festival nobody has to arrange, a block somebody else put in their diary, something that will happen without them and asks nothing of them: say "+Silence+" and no more.",
	"Do not reason that a thing will happen whether or not they prepare -- that is true of almost everything, and the question is whether they will wish they had.",
	"Where warning is owed, give one line for each moment worth a word, longest warning first, as:",
	"3 days -- a present to buy and something to wear",
	"2 hours -- time to leave",
	"Give a second line only where the two moments are about different things.",
	"A film does not need one: by the time they are leaving there is nothing left to arrange.",
	"The reason is said aloud to them, so write it as the thing they have to do and keep it under a dozen words.",
	"Nothing longer than a week, and nothing shorter than ten minutes.",
)

// About : One entry, as the model reads it.
//
// The title, when, how long, and which calendar it came from. The
// calendar matters: a thing on a holidays calendar is not something
// they arranged, and the name says so without this having to know
// what a holiday is.
func About(e calendar.Event, now time.Time, where *time.Location) string {
	if where == nil {
		where = time.UTC
	}

	lines := []string{"Title: " + strings.TrimSpace(e.Title)}
	if e.AllDay {
		lines = append(lines, "When: all day on "+e.Starts.In(where).Format("Monday 2 January"))
	} else {
		lines = append(lines, "When: "+e.Starts.In(where).Format("Monday 2 January, 3:04 pm")+
			", lasting "+Away(e.Ends.Sub(e.Starts))[3:])
	}
	lines = append(lines, "That is "+Away(e.Starts.Sub(now))[3:]+" from now.")
	if w := strings.TrimSpace(e.Where); w != "" {
		lines = append(lines, "Where: "+w)
	}
	if n := strings.TrimSpace(e.Notes); n != "" {
		lines = append(lines, "Notes: "+n)
	}
	if c := strings.TrimSpace(e.Calendar); c != "" {
		lines = append(lines, "On the calendar called: "+c)
	}
	if k := strings.TrimSpace(e.Kind); k != "" {
		lines = append(lines, "Google calls this sort of entry: "+k)
	}
	if !e.Mine {
		lines = append(lines, "This is not on their own calendar; somebody else put it there.")
	}
	return prompt.Block(Asking, prompt.Text(lines...))
}

// said : "3 days -- a present to buy", in the shapes a model writes it.
var said = regexp.MustCompile(`(?i)^\s*[-*\d.)\s]*?(\d+)\s*(minute|min|hour|hr|day|week)s?\b\s*(?:--|-|:|,)?\s*(.*)$`)

// Read : What the model answered, as leads.
//
// Anything unreadable is dropped rather than guessed at. A line that
// does not say a length of time is not a lead, and inventing one
// from a sentence would put a warning somewhere nobody chose.
func Read(answer string) []Lead {
	if Nothing(answer) {
		return nil
	}

	var out []Lead
	seen := map[time.Duration]bool{}
	for _, line := range strings.Split(answer, "\n") {
		m := said.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(m[1])
		if err != nil || n <= 0 {
			continue
		}

		var before time.Duration
		switch strings.ToLower(m[2]) {
		case "minute", "min":
			before = time.Duration(n) * time.Minute
		case "hour", "hr":
			before = time.Duration(n) * time.Hour
		case "day":
			before = time.Duration(n) * 24 * time.Hour
		case "week":
			before = time.Duration(n) * 7 * 24 * time.Hour
		}

		if before > Longest {
			before = Longest
		}
		if before < Shortest || seen[before] {
			continue
		}
		seen[before] = true
		out = append(out, Lead{Before: before, Why: tidy(m[3])})
	}

	// Longest warning first, which is the order they are spoken in and
	// the order Judged.Said counts through.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Before > out[j-1].Before; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// Nothing : Whether the model said this needs no warning.
func Nothing(answer string) bool { return prompt.Nothing(answer) }

// tidy : A reason as it will be said, with the model's punctuation
// taken off the front and a full stop put on the end.
func tidy(why string) string {
	why = strings.TrimSpace(strings.Trim(strings.TrimSpace(why), "-–—:,"))
	if why == "" {
		return ""
	}
	why = strings.ToUpper(why[:1]) + why[1:]
	if !strings.ContainsAny(why[len(why)-1:], ".!?") {
		why += "."
	}
	return why
}
