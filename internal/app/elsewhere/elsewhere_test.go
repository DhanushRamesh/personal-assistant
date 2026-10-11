package elsewhere_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/app/elsewhere"
)

// holding : A source that returns what it is given.
func holding(kind string, named ...string) elsewhere.Source {
	return elsewhere.Source{Kind: kind, Look: func(context.Context, string) ([]elsewhere.Thing, error) {
		out := make([]elsewhere.Thing, 0, len(named))
		for _, n := range named {
			out = append(out, elsewhere.Thing{Kind: kind, Named: n, Tool: kind + "_cancel"})
		}
		return out, nil
	}}
}

// The sentence this whole package exists for. Owner: "stop the water
// alarm ... it should figure out and should come to a conclusion that
// it is an alarm kept to drink water at 11:45."
//
// Nothing he said is in the reminder's text and nothing in the
// reminder's text is what he said. They share one word.
func TestTheWordTheyShareIsEnough(t *testing.T) {
	if !elsewhere.Matches("water alarm", "Time to drink water, sir") {
		t.Error(`"water alarm" did not reach "Time to drink water, sir"`)
	}
	if !elsewhere.Matches("the standup", "Standup") {
		t.Error("a name with a word in front of it did not match")
	}
	if elsewhere.Matches("water alarm", "Call the dentist") {
		t.Error("two unrelated things matched")
	}
	// Short words are in everything and must not match on their own.
	if elsewhere.Matches("the my a", "the dentist") {
		t.Error("a short word matched, which would make everything a candidate")
	}
}

// The domain that just failed is searched again, because it is being
// asked a different question. heard.Best scores "water alarm" against
// "Drink water" at 0.10 and finds nothing; a shared word finds it.
// Skipping that domain would miss the one example this exists for.
func TestTheDomainThatLookedIsSearchedAgain(t *testing.T) {
	s := elsewhere.Search{Sources: []elsewhere.Source{
		holding("reminder", "Drink water", "Call the dentist"),
		holding("rule", "water the plants"),
	}}

	found := s.Found(context.Background(), "usr_1", "water alarm", "reminder")
	if len(found) != 2 {
		t.Fatalf("found %d, want the reminder and the rule: %+v", len(found), found)
	}
	var sawReminder bool
	for _, f := range found {
		if f.Kind == "reminder" && f.Named == "Drink water" {
			sawReminder = true
		}
	}
	if !sawReminder {
		t.Errorf("the reminder the tool could not name was not found either: %+v", found)
	}
}

func TestOneSourceFailingDoesNotLoseTheRest(t *testing.T) {
	broken := elsewhere.Source{Kind: "task", Look: func(context.Context, string) ([]elsewhere.Thing, error) {
		return nil, errors.New("the list could not be read")
	}}
	s := elsewhere.Search{Sources: []elsewhere.Source{broken, holding("rule", "drink water")}}

	found := s.Found(context.Background(), "usr_1", "water", "reminder")
	if len(found) != 1 || found[0].Kind != "rule" {
		t.Errorf("a broken source took the others with it: %+v", found)
	}
}

// A source that never answers must cost the turn the budget and no
// more. This runs on a turn that has already gone wrong; it cannot be
// allowed to be the thing that makes it slow.
func TestASlowSourceIsLeftBehind(t *testing.T) {
	hung := elsewhere.Source{Kind: "diary entry", Look: func(ctx context.Context, _ string) ([]elsewhere.Thing, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	s := elsewhere.Search{Sources: []elsewhere.Source{hung, holding("rule", "drink water")}}

	started := time.Now()
	found := s.Found(context.Background(), "usr_1", "water", "reminder")
	took := time.Since(started)

	if took > elsewhere.Budget+time.Second {
		t.Errorf("waited %s, want no more than the budget of %s", took, elsewhere.Budget)
	}
	if len(found) != 1 {
		t.Errorf("the source that did answer was lost: %+v", found)
	}
}

func TestNothingIsSaidWithoutAName(t *testing.T) {
	s := elsewhere.Search{Sources: []elsewhere.Source{holding("rule", "drink water")}}
	if got := s.Found(context.Background(), "usr_1", "   ", ""); got != nil {
		t.Errorf("searched for nothing and found %+v", got)
	}
	if got := s.Found(context.Background(), "", "water", ""); got != nil {
		t.Errorf("searched for a user that is not there and found %+v", got)
	}
}

// One candidate is a question, several is a choice, and none is a
// caveat rather than an argument with the tool that already spoke.
func TestWhatTheModelIsToldDependsOnHowManyThereAre(t *testing.T) {
	one := elsewhere.Says("water alarm", []elsewhere.Thing{
		{Kind: "reminder", Named: "Time to drink water, sir", When: "at 11:45 am", Tool: "reminder_cancel"}})
	if !strings.Contains(one, "reminder_cancel") || !strings.Contains(one, "yes or no") {
		t.Errorf("one candidate was not put as a question to act on:\n%s", one)
	}

	// Several is still a proposal, not a list read out. He asked for
	// a conclusion; the others are kept back for a no.
	several := elsewhere.Says("water", []elsewhere.Thing{
		{Kind: "reminder", Named: "drink water", Tool: "reminder_cancel"},
		{Kind: "rule", Named: "water the plants", Tool: "rule_cancel"}})
	if !strings.Contains(several, "likeliest") || !strings.Contains(several, "yes or no") {
		t.Errorf("several candidates were not put as one proposal:\n%s", several)
	}
	if !strings.Contains(several, "water the plants") {
		t.Errorf("the others were not kept for a no:\n%s", several)
	}
	if !strings.Contains(several, "unless they say no") {
		t.Errorf("nothing stopped it reading the whole list out:\n%s", several)
	}

	none := elsewhere.Says("qwerty", nil)
	if !strings.Contains(none, "phone") {
		t.Errorf("the one thing the server cannot see went unmentioned:\n%s", none)
	}
	if !strings.Contains(none, "web") {
		t.Errorf("nothing stopped it searching the web for something of his:\n%s", none)
	}
}

// Nearest first, so that cutting the list drops the weakest.
func TestTheLikeliestComesFirst(t *testing.T) {
	s := elsewhere.Search{Sources: []elsewhere.Source{
		holding("rule", "water the plants on Sunday"),
		holding("task", "water"),
	}}
	found := s.Found(context.Background(), "usr_1", "water", "reminder")
	if len(found) != 2 {
		t.Fatalf("found %d, want both: %+v", len(found), found)
	}
	if found[0].Named != "water" {
		t.Errorf("order was %q then %q, want the exact name first",
			found[0].Named, found[1].Named)
	}
}
