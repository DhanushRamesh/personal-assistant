package rules_test

import (
	"context"
	"strings"
	"testing"

	"personal-assistant/internal/app/tool/rules"
	"personal-assistant/internal/base/world/event"
)

// answering : A writer that gives these answers in turn, and
// remembers what it was asked each time.
func answering(said ...string) (*[]string, rules.Writer) {
	asked := &[]string{}
	n := 0
	return asked, rules.Writer{
		Ask: func(_ context.Context, _, brief string) (string, error) {
			*asked = append(*asked, brief)
			if n >= len(said) {
				return said[len(said)-1], nil
			}
			out := said[n]
			n++
			return out, nil
		},
		Kinds: func(context.Context, string) ([]event.Kind, error) {
			return []event.Kind{{Kind: "battery.level"}, {Kind: "call.missed"}}, nil
		},
		Reading: func() []string { return []string{"event_recent", "calendar_events"} },
	}
}

const good = `{"when":{"wake":[{"every":"10m"}],` +
	`"all":[{"source":"battery.level","op":"<","value":20}]},` +
	`"perform":[{"do":"say","text":"the battery is low"}]}`

// TestAMistakeIsHandedBackWithTheReason : The whole of why a rule
// is written by something that does nothing else. A refusal here
// is a sentence somebody could read, so it is given straight back
// to the writer -- and a mistake is described in the same words
// whether a person is being told or a model is being corrected.
func TestAMistakeIsHandedBackWithTheReason(t *testing.T) {
	asked, w := answering(
		// Nothing would ever wake it.
		`{"when":{"all":[{"source":"battery.level","op":"<","value":20}]},`+
			`"perform":[{"do":"say","text":"low"}]}`,
		good,
	)
	when, acts, bad := w.Written(context.Background(), "usr_1", "tell me when the battery is low", "", look)
	if bad != "" {
		t.Fatalf("a mendable mistake was not mended: %s", bad)
	}
	if len(*asked) != 2 {
		t.Fatalf("it was asked %d times, want a second go", len(*asked))
	}
	// The second time it is told what was wrong, in those words.
	if !strings.Contains((*asked)[1], "Your last answer was refused") {
		t.Error("the second go did not say what was wrong")
	}
	if !strings.Contains((*asked)[1], "wake") {
		t.Errorf("the reason was not handed back: %s", (*asked)[1])
	}
	if len(when.Wake) != 1 || len(acts) != 1 {
		t.Errorf("the mended rule is %+v / %+v", when, acts)
	}
}

// TestItGivesUpAndSaysWhy : Three goes, and then the reason -- not
// a shrug. Somebody asked for something, and why it could not be
// done is the only useful thing left to say.
func TestItGivesUpAndSaysWhy(t *testing.T) {
	asked, w := answering(`{"when":{"wake":[{"every":"10m"}]},"perform":[]}`)
	_, _, bad := w.Written(context.Background(), "usr_1", "do a thing", "", look)
	if bad == "" {
		t.Fatal("a rule with nothing to do was accepted")
	}
	if len(*asked) != rules.Tries {
		t.Errorf("it had %d goes, want %d", len(*asked), rules.Tries)
	}
	if !strings.Contains(bad, "do something") {
		t.Errorf("it gave up with %q", bad)
	}
}

// TestTheWriterIsToldWhatExists : A rule naming a kind nobody
// reports never fires, and a model with no list in front of it
// writes the kind that sounds right. There is no list of kinds
// anywhere in the engine on purpose, so it comes from what has
// actually arrived.
func TestTheWriterIsToldWhatExists(t *testing.T) {
	asked, w := answering(good)
	w.Written(context.Background(), "usr_1", "tell me when the battery is low",
		"The rules they already have:\n  {\"name\":\"an old one\"}", look)

	brief := (*asked)[0]
	for _, want := range []string{
		"battery.level", "call.missed", // what the devices report
		"event_recent", "calendar_events", // what a rule may use
		"an old one",                               // their own rules, as examples
		"tell me when the battery is low",          // what they asked for
		"when", "perform", "arrives", "older_than", // the grammar
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("the brief never mentions %q", want)
		}
	}
}

// TestARefusalFromTheWriterIsPassedOn : Asked for something that
// is not a rule, it says so, and that sentence is what the person
// hears rather than three goes at writing it anyway.
func TestARefusalFromTheWriterIsPassedOn(t *testing.T) {
	asked, w := answering(`{"cannot":"Nothing a device reports says whether the bins are out."}`)
	_, _, bad := w.Written(context.Background(), "usr_1", "tell me when the bins are out", "", look)
	if !strings.Contains(bad, "bins") {
		t.Errorf("it said %q", bad)
	}
	if len(*asked) != 1 {
		t.Errorf("it tried %d times at something it had already refused", len(*asked))
	}
}

// TestAFenceIsNotARefusal : Told to answer with JSON and nothing
// else, and it mostly does. A perfectly good rule thrown away
// because a model put three backticks round it is a refusal
// nobody can act on.
func TestAFenceIsNotARefusal(t *testing.T) {
	_, w := answering("```json\n" + good + "\n```")
	when, acts, bad := w.Written(context.Background(), "usr_1", "x", "", look)
	if bad != "" {
		t.Fatalf("a fenced answer was refused: %s", bad)
	}
	if len(when.Wake) != 1 || len(acts) != 1 {
		t.Errorf("the fenced rule came out as %+v / %+v", when, acts)
	}
}

// TestNoModelMeansNoGuessing : A server with nothing to write with
// says so, rather than writing something.
func TestNoModelMeansNoGuessing(t *testing.T) {
	if _, _, bad := (rules.Writer{}).Written(context.Background(), "usr_1", "x", "", look); bad == "" {
		t.Error("a server with no model wrote a rule anyway")
	}
	_, w := answering(good)
	if _, _, bad := w.Written(context.Background(), "usr_1", "  ", "", look); bad == "" {
		t.Error("it wrote a rule from nothing at all")
	}
}
