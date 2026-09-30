package vocabulary_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
	"github.com/DhanushRamesh/personal-assistant/internal/vocabulary"
)

// said : Messages to build from.
type said struct {
	msgs []conversation.Message
	err  error
}

func (s said) SaidSince(_ context.Context, _ string, _ time.Time, _ int) ([]conversation.Message, error) {
	return s.msgs, s.err
}

// kept : Where a list ends up, and whether it got there.
type kept struct {
	terms  []string
	at     time.Time
	writes int
	err    error
}

func (k *kept) Put(_ context.Context, _ string, terms []string, at time.Time) error {
	if k.err != nil {
		return k.err
	}
	k.terms, k.at = terms, at
	k.writes++
	return nil
}

func (k *kept) Get(context.Context, string) ([]string, time.Time, error) {
	return k.terms, k.at, nil
}

func (k *kept) Everyone(context.Context) ([]string, time.Time, error) {
	return k.terms, k.at, nil
}

// week : Enough messages that a rebuild is worth doing.
func week(n int) []conversation.Message {
	out := make([]conversation.Message, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, conversation.Message{
			Content: "remind me about Alekhya",
			At:      time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC),
		})
	}
	return out
}

// answering : A builder whose model says whatever is given.
func answering(answer string, store *kept, msgs []conversation.Message) *vocabulary.Builder {
	return &vocabulary.Builder{
		Said:  said{msgs: msgs},
		Store: store,
		Ask: func(context.Context, string) (string, error) {
			return answer, nil
		},
		Now: func() time.Time { return time.Date(2026, 9, 30, 4, 0, 0, 0, time.UTC) },
	}
}

// The names come back one per line, and that is what is stored.
func TestTheNamesAreStored(t *testing.T) {
	store := &kept{}
	b := answering("Alekhya\nChintada\nToofy\n", store, week(30))

	if err := b.Build(context.Background(), "usr_1"); err != nil {
		t.Fatalf("building: %v", err)
	}

	want := []string{"Alekhya", "Chintada", "Toofy"}
	if strings.Join(store.terms, ",") != strings.Join(want, ",") {
		t.Errorf("stored %v, want %v", store.terms, want)
	}
	if store.at.IsZero() {
		t.Error("stored without a time")
	}
}

// A thin week is left alone. Whatever is already stored was built from
// more than this, so replacing it makes the list worse.
func TestAThinWeekChangesNothing(t *testing.T) {
	store := &kept{}
	b := answering("Alekhya\n", store, week(vocabulary.Least-1))

	if err := b.Build(context.Background(), "usr_1"); err != nil {
		t.Fatalf("building: %v", err)
	}
	if store.writes != 0 {
		t.Errorf("wrote %d times, want none", store.writes)
	}
}

// A week with no names in it stores the empty list rather than failing.
// "What is the time" all week genuinely has no names, and refusing to
// write would leave last week's names primed forever.
func TestAWeekWithNoNamesStoresNothingRatherThanFailing(t *testing.T) {
	store := &kept{}
	b := answering("\n\n", store, week(30))

	if err := b.Build(context.Background(), "usr_1"); err != nil {
		t.Fatalf("building: %v", err)
	}
	if store.writes != 1 {
		t.Fatalf("wrote %d times, want one", store.writes)
	}
	if len(store.terms) != 0 {
		t.Errorf("stored %v, want nothing", store.terms)
	}
}

// TestOnlyNamesSurviveTheAnswer : Everything the model adds around the
// names is dropped.
//
// This is the whole reason Clean exists. What it returns is handed to a
// speech engine as words to expect, so a heading or an aside primes the
// decoder for a sentence nobody will ever say -- and takes the place of
// a name that somebody says daily.
func TestOnlyNamesSurviveTheAnswer(t *testing.T) {
	answer := strings.Join([]string{
		"Here are the proper nouns I found:",
		"- Alekhya",
		"2. Chintada",
		`"Toofy"`,
		"Hyderabad (a city)",
		"the air conditioner in the bedroom that keeps coming up",
		"",
		"* Zomato",
		"Let me know if you need more.",
	}, "\n")

	got := vocabulary.Clean(answer)
	want := []string{"Alekhya", "Chintada", "Toofy", "Zomato"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("kept %v, want %v", got, want)
	}
}

// One name, once. The same name twice in an answer would be sent to
// Deepgram as two keyterms out of a hundred.
func TestANameIsKeptOnce(t *testing.T) {
	got := vocabulary.Clean("Alekhya\nalekhya\nALEKHYA\nChintada")
	if len(got) != 2 {
		t.Errorf("kept %v, want two", got)
	}
}

// More names than the budget allows are cut, not sent.
func TestTheListIsCapped(t *testing.T) {
	var lines []string
	for i := 0; i < vocabulary.Cap*2; i++ {
		lines = append(lines, "Name"+string(rune('A'+i%26))+string(rune('a'+i/26)))
	}
	if got := vocabulary.Clean(strings.Join(lines, "\n")); len(got) != vocabulary.Cap {
		t.Errorf("kept %d, want %d", len(got), vocabulary.Cap)
	}
}

// A failure to read or to ask is reported, and nothing is stored: a
// half-built list would replace a good one.
func TestAFailureStoresNothing(t *testing.T) {
	for _, c := range []struct {
		name string
		b    *vocabulary.Builder
	}{
		{"cannot read what they said", &vocabulary.Builder{
			Said:  said{err: errors.New("the database is away")},
			Store: &kept{},
			Ask:   func(context.Context, string) (string, error) { return "Alekhya", nil },
		}},
		{"cannot ask the model", &vocabulary.Builder{
			Said:  said{msgs: week(30)},
			Store: &kept{},
			Ask: func(context.Context, string) (string, error) {
				return "", errors.New("the provider is away")
			},
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			if err := c.b.Build(context.Background(), "usr_1"); err == nil {
				t.Fatal("reported success")
			}
			if store := c.b.Store.(*kept); store.writes != 0 {
				t.Errorf("wrote %d times, want none", store.writes)
			}
		})
	}
}

// Nothing runs unconfigured, and nobody is described without being
// named.
func TestItRefusesWhatItCannotDo(t *testing.T) {
	if err := (&vocabulary.Builder{}).Build(context.Background(), "usr_1"); err == nil {
		t.Error("an unconfigured builder reported success")
	}
	if err := answering("x", &kept{}, week(30)).Build(context.Background(), ""); err == nil {
		t.Error("built a list for nobody")
	}
}
