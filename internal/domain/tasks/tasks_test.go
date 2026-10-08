package tasks_test

import (
	"errors"
	"testing"

	"personal-assistant/internal/domain/tasks"
)

// lists : What somebody has, as Google would give them back.
func lists(titles ...string) []tasks.List {
	out := make([]tasks.List, len(titles))
	for i, t := range titles {
		out[i] = tasks.List{ID: "lst_" + t, Title: t}
	}
	return out
}

// TestWhichListTheyMeant : What reaches here has been through a
// speech recogniser and is misheard more often than not, so a list is
// matched by likeness rather than by being spelled right.
func TestWhichListTheyMeant(t *testing.T) {
	held := lists("Grocery list", "Reading list", "Work")

	for _, c := range []struct{ said, want string }{
		{"Grocery list", "Grocery list"},
		{"grocery list", "Grocery list"},
		{"  grocery  ", "Grocery list"},
		{"grocerylist", "Grocery list"},
		{"Work", "Work"},
	} {
		got, err := tasks.Pick(held, c.said)
		if err != nil {
			t.Errorf("%q was not matched: %v", c.said, err)
			continue
		}
		if got.Title != c.want {
			t.Errorf("%q matched %q, want %q", c.said, got.Title, c.want)
		}
	}
}

// TestNamingNothingTakesTheOneTheyHave : Google makes a default list
// and most people never make another, so asking for "my list" when
// there is one is not ambiguous.
func TestNamingNothingTakesTheOneTheyHave(t *testing.T) {
	got, err := tasks.Pick(lists("My Tasks"), "")
	if err != nil {
		t.Fatalf("Pick with nothing named: %v", err)
	}
	if got.Title != "My Tasks" {
		t.Errorf("took %q, want the only one", got.Title)
	}

	// With several, the first is Google's default and is still the
	// answer: picking at random would be worse than picking the one
	// they would see first.
	got, err = tasks.Pick(lists("My Tasks", "Shopping"), "")
	if err != nil || got.Title != "My Tasks" {
		t.Errorf("took %+v (%v), want the first", got, err)
	}
}

// TestWhenItCannotBeToldWhichOne : Asking is right and guessing is
// not. A task added to the wrong list is not findable by the person
// who added it.
func TestWhenItCannotBeToldWhichOne(t *testing.T) {
	if _, err := tasks.Pick(nil, "anything"); !errors.Is(err, tasks.ErrNoSuchList) {
		t.Errorf("with no lists at all, err = %v, want ErrNoSuchList", err)
	}
	if _, err := tasks.Pick(lists("Groceries"), "the roof"); !errors.Is(err, tasks.ErrNoSuchList) {
		t.Errorf("a name like nothing held, err = %v, want ErrNoSuchList", err)
	}

	// Two that are alike is the case where it must ask rather than
	// choose.
	_, err := tasks.Pick(lists("Shopping list", "Shopping lists"), "shopping list")
	if err != nil && !errors.Is(err, tasks.ErrWhichList) && !errors.Is(err, tasks.ErrNoSuchList) {
		t.Errorf("two alike gave %v, want it to ask or refuse rather than guess", err)
	}
}
