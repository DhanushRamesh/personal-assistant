package pointing_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/app/pointing"
	"personal-assistant/internal/base/thinking/naming"
	"personal-assistant/internal/base/world/heard"
)

// said : A finder that reports exactly the spans it was built with, so a
// test says what GLiNER found without running it.
type said []naming.Span

func (s said) Names(context.Context, string, []string) ([]naming.Span, error) {
	return []naming.Span(s), nil
}
func (s said) Available() bool { return true }

// broken : A finder that fails, for the path where the service is down.
type broken struct{}

func (broken) Names(context.Context, string, []string) ([]naming.Span, error) {
	return nil, errors.New("connection refused")
}
func (broken) Available() bool { return true }

// holding : A Known holding a fixed list, matched the way a Record does.
type holding []string

func (h holding) Spelling(_ context.Context, name string) (string, bool) {
	at, err := heard.Best([]string(h), name)
	if err != nil {
		return "", false
	}
	return h[at], true
}

func person(text string) naming.Span { return naming.Span{Text: text, Label: "person", Score: 0.9} }

// A name his devices have reported is his; a real name they never have is
// the world's; a pronoun or a plain noun is neither.
func TestANameGoesToOneOfThreePlaces(t *testing.T) {
	p := &pointing.Pointer{
		Finder:    said{person("Deepika"), person("Spotify"), person("I"), person("my mom"), person("Nova")},
		Known:     holding{"Deepika", "Alekhya"},
		Assistant: "Nova",
	}

	points, err := p.Points(context.Background(), "did Deepika open Spotify")
	if err != nil {
		t.Fatalf("Points: %v", err)
	}

	want := map[string]pointing.Where{
		"Deepika": pointing.His,
		"Spotify": pointing.Worldly,
		"I":       pointing.Nowhere,
		"my mom":  pointing.Nowhere,
		"Nova":    pointing.Nowhere,
	}
	if len(points) != len(want) {
		t.Fatalf("got %d points, want %d: %v", len(points), len(want), points)
	}
	for _, got := range points {
		if where, ok := want[got.Said]; !ok || got.Where != where {
			t.Errorf("%q went to %s, want %s", got.Said, got.Where, where)
		}
	}
}

// The record holds the spelling because what was said has been through
// speech recognition and the record has not.
func TestTheRecordSpellsItRight(t *testing.T) {
	p := &pointing.Pointer{
		Finder: said{person("alekya"), person("Spotify")},
		Known:  holding{"Alekhya"},
	}
	points, err := p.Points(context.Background(), "call alekya")
	if err != nil {
		t.Fatalf("Points: %v", err)
	}
	if points[0].Term() != "Alekhya" {
		t.Errorf("looked up as %q, want the spelling the record holds", points[0].Term())
	}
	// A worldly name has no stored spelling, so it is looked up as said.
	if points[1].Term() != "Spotify" {
		t.Errorf("looked up as %q, want what was said", points[1].Term())
	}
}

// With nothing to look names up in, every name is worldly. Wrong in the
// safe direction: the web is asked about his sister rather than his own
// record being asked about the weather.
func TestWithNoRecordEveryNameIsWorldly(t *testing.T) {
	p := &pointing.Pointer{Finder: said{person("Deepika")}}
	points, err := p.Points(context.Background(), "ring Deepika")
	if err != nil {
		t.Fatalf("Points: %v", err)
	}
	if points[0].Where != pointing.Worldly {
		t.Errorf("went to %s, want worldly", points[0].Where)
	}
}

// Looking at nothing and not looking are different, so a finder that is
// missing or failing says so rather than answering "no names".
func TestAFinderThatCannotAnswerSaysSo(t *testing.T) {
	none := &pointing.Pointer{Finder: naming.Fake{Off: true}}
	if _, err := none.Points(context.Background(), "ring Deepika"); !errors.Is(err, naming.ErrUnavailable) {
		t.Errorf("an unavailable finder gave %v, want ErrUnavailable", err)
	}
	down := &pointing.Pointer{Finder: broken{}}
	if _, err := down.Points(context.Background(), "ring Deepika"); err == nil {
		t.Error("a failing finder answered as though it had looked")
	}
}

// The same name said twice is one lookup.
func TestTheSameNameIsPointedAtOnce(t *testing.T) {
	p := &pointing.Pointer{Finder: said{person("Deepika"), person("deepika")}}
	points, _ := p.Points(context.Background(), "Deepika, I mean deepika")
	if len(points) != 1 {
		t.Errorf("got %d points for one name: %v", len(points), points)
	}
}

// Named only ever removes, and only words from its own lists. No proper
// noun is in them, so no name can be lost here however badly they are
// chosen -- which is why recall was 100% on all 91 measured spans.
func TestTheListsHoldNoProperNoun(t *testing.T) {
	for _, word := range pointing.Vocabulary() {
		if word != strings.ToLower(word) {
			t.Errorf("%q is capitalised, so it is a name and does not belong in the lists", word)
		}
		if pointing.Named(word, "") {
			t.Errorf("%q is in the lists and still counts as a name", word)
		}
	}
}

// Home and office are what his own places are called, so they are names
// however ordinary the words look.
func TestHisOwnPlacesAreNames(t *testing.T) {
	for _, word := range []string{"home", "office", "rental house"} {
		if !pointing.Named(word, "Nova") {
			t.Errorf("%q is not taken as a name", word)
		}
	}
}

// Anything longer than a name is a clause and is dropped on length.
//
// Not on its words: real names hold filler words too -- "Tears For
// Fears", "Chance the Rapper" -- so nothing shorter can be rejected for
// having one. "capital of Australia" is three words and survives, which
// is one of the four wrong keeps in the 85% measured, and it costs one
// lookup that finds nothing.
func TestAClauseIsNotAName(t *testing.T) {
	for _, span := range []string{"what is the time right now", "a movie I saw last year", ""} {
		if pointing.Named(span, "Nova") {
			t.Errorf("%q is taken as a name", span)
		}
	}
	for _, span := range []string{"Tears For Fears", "Chance the Rapper"} {
		if !pointing.Named(span, "Nova") {
			t.Errorf("%q is not taken as a name", span)
		}
	}
}

// Terms gives the caller one list per place to look.
func TestTermsAreSplitByWhereTheyBelong(t *testing.T) {
	p := &pointing.Pointer{
		Finder: said{person("alekya"), person("Spotify"), person("him")},
		Known:  holding{"Alekhya"},
	}
	points, _ := p.Points(context.Background(), "ask alekya about Spotify")

	if got := pointing.Terms(points, pointing.His); len(got) != 1 || got[0] != "Alekhya" {
		t.Errorf("his terms are %v, want [Alekhya]", got)
	}
	if got := pointing.Terms(points, pointing.Worldly); len(got) != 1 || got[0] != "Spotify" {
		t.Errorf("worldly terms are %v, want [Spotify]", got)
	}
	if got := pointing.Terms(points, pointing.Nowhere); len(got) != 1 {
		t.Errorf("nowhere terms are %v, want one", got)
	}
}

// A name is matched where it begins another, and never where it merely
// appears inside one.
//
// "Tada" is a town and "Alekhya Chintada" is a person his phone reported.
// Matched by containment they are the same name, and a question about
// the town is answered out of his contacts.
func TestANameInTheMiddleOfAnotherIsNotIt(t *testing.T) {
	source := &listing{names: []string{"Alekhya Chintada", "realme Buds Air8", "Holidays in India"}}
	r := &pointing.Record{Sources: []pointing.Names{source}}
	ctx := pointing.WithUser(context.Background(), "usr_1")

	if spelt, ok := r.Spelling(ctx, "Tada"); ok {
		t.Errorf("Tada was matched to %q", spelt)
	}
	// A name that begins another is still it.
	for said, want := range map[string]string{
		"realme":           "realme Buds Air8",
		"Holidays":         "Holidays in India",
		"Alekhya Chintada": "Alekhya Chintada",
	} {
		if spelt, ok := r.Spelling(ctx, said); !ok || spelt != want {
			t.Errorf("%q came back as %q, %v; want %q", said, spelt, ok, want)
		}
	}
	// And a misheard spelling is still matched by likeness.
	if spelt, ok := r.Spelling(ctx, "alekya chintada"); !ok || spelt != "Alekhya Chintada" {
		t.Errorf("a misheard name came back as %q, %v", spelt, ok)
	}
}

// listing : A Names source made from a fixed list, counting its reads.
type listing struct {
	names []string
	reads int
	fail  error
}

func (l *listing) Named(context.Context, string) ([]string, error) {
	l.reads++
	if l.fail != nil {
		return nil, l.fail
	}
	return l.names, nil
}

// The record reads its sources once and reuses the answer, because three
// queries in front of every answer is three queries he waits through.
func TestTheRecordIsReadOnceAndKept(t *testing.T) {
	source := &listing{names: []string{"Deepika", "Alekhya"}}
	now := time.Now()
	r := &pointing.Record{Sources: []pointing.Names{source}, Now: func() time.Time { return now }}
	ctx := pointing.WithUser(context.Background(), "usr_1")

	for i := 0; i < 5; i++ {
		if _, ok := r.Spelling(ctx, "deepika"); !ok {
			t.Fatal("the record does not hold a name its source gave")
		}
	}
	if source.reads != 1 {
		t.Errorf("read the source %d times, want once", source.reads)
	}

	now = now.Add(2 * time.Hour)
	if _, ok := r.Spelling(ctx, "deepika"); !ok {
		t.Fatal("the name went missing when the list went stale")
	}
	if source.reads != 2 {
		t.Errorf("read the source %d times after it went stale, want twice", source.reads)
	}
}

// A source that fails leaves what is held standing. Emptying it would
// send his own people to the web.
func TestAFailedReadKeepsWhatIsHeld(t *testing.T) {
	source := &listing{names: []string{"Deepika"}}
	now := time.Now()
	r := &pointing.Record{Sources: []pointing.Names{source}, Now: func() time.Time { return now }}
	ctx := pointing.WithUser(context.Background(), "usr_1")

	if _, ok := r.Spelling(ctx, "Deepika"); !ok {
		t.Fatal("the name was not held to begin with")
	}
	source.fail = errors.New("the database is away")
	now = now.Add(2 * time.Hour)
	if _, ok := r.Spelling(ctx, "Deepika"); !ok {
		t.Error("a failed read emptied the record")
	}
}

// One value may carry several names, as a song's artists do.
func TestOneValueMayHoldSeveralNames(t *testing.T) {
	source := &listing{names: []string{"Harris Jayaraj, Naresh Iyer"}}
	r := &pointing.Record{Sources: []pointing.Names{source}}
	ctx := pointing.WithUser(context.Background(), "usr_1")

	for _, name := range []string{"Harris Jayaraj", "Naresh Iyer"} {
		if spelt, ok := r.Spelling(ctx, strings.ToLower(name)); !ok || spelt != name {
			t.Errorf("%q came back as %q, %v", name, spelt, ok)
		}
	}
}
