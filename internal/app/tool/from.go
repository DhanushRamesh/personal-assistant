package tool

// From : How a person would name where a tool's answer came from.
//
// Not the tool and not the domain's code name: "your diary", never
// "calendar_events" and never "the calendar domain". The point is a
// clause somebody can check, and a name out of the machine is the
// machinery leak [persona.NamesMachinery] exists to stop.
//
// Why this exists. Of 353 answers that called a tool, 48 said where
// anything came from. So an answer that was looked up and one that was
// not are the same sentence, and nothing in it can be checked without
// opening a panel -- which by voice does not exist. Owner, 19 October
// 2026: *"i'm not getting trust on our jarvis ... there is something
// missing"*.
//
// Empty for a domain whose answer is about nothing the person keeps.
func From(domain string) string { return from[domain] }

// from : The words for each domain, in the person's terms.
//
// A domain missing from here says nothing rather than something
// awkward, and TestEveryDomainThatReadsSaysWhereFrom holds the ones
// that must be in it.
var from = map[string]string{
	"calendar":     "your diary",
	"coming":       "your diary",
	"task":         "your lists",
	"reminder":     "your reminders",
	"mail":         "your mail",
	"memory":       "what you have told me",
	"event":        "your own devices",
	"calling":      "your phone",
	"map":          "the map",
	"web":          "the web",
	"leaving":      "your diary and the map",
	"situation":    "your own devices",
	"conversation": "what was said before",
}

// Carries : What a tool adds to its answer so the reply can say where
// it came from.
//
// Given as the first line of the result rather than as a rule to weigh,
// because a rule the model must weigh gets broken and a line it is
// reading does not -- the same reason [events.Moving] states the rate
// in words instead of handing over two numbers to subtract.
//
// Empty where there are no words for the domain, or where the tool
// changes something: a write is reported by what it did, and "from your
// lists" in front of "added" reads as though the adding was a lookup.
func (t Tool) Carries() string {
	if t.Writes {
		return ""
	}
	words := From(t.Domain)
	if words == "" {
		return ""
	}
	return "From " + words + ". Say where this came from when you answer, in a few words, so they can tell it was looked up and not remembered."
}
