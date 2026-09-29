package heard_test

import (
	"errors"
	"testing"

	"github.com/DhanushRamesh/personal-assistant/internal/heard"
)

// TestBestOnWhatWasActuallyHeard : Every spoken form here was said to the
// assistant and written down wrong by speech-to-text on 27 and 28
// September 2026.
func TestBestOnWhatWasActuallyHeard(t *testing.T) {
	names := []string{"Jarvis", "rjdhanush22@gmail.com", "Holidays in India", "Diary for the week"}

	for _, tc := range []struct {
		said, want string
		err        error
	}{
		{"rjdhanush22@gmail.com", "rjdhanush22@gmail.com", nil},
		{"rjdanesh22rjmail.com", "rjdhanush22@gmail.com", nil},
		{"javas", "Jarvis", nil},
		{"jarvis", "Jarvis", nil},
		{"holidays", "Holidays in India", nil},
		{"holiday in india", "Holidays in India", nil},
		{"diary for the weak", "Diary for the week", nil},
		{"birthdays", "", heard.ErrNone},
		{"", "", heard.ErrNone},
	} {
		at, err := heard.Best(names, tc.said)
		if tc.err != nil {
			if !errors.Is(err, tc.err) {
				t.Errorf("%q: error = %v, want %v", tc.said, err, tc.err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: unexpected error %v", tc.said, err)
			continue
		}
		if names[at] != tc.want {
			t.Errorf("%q -> %q, want %q", tc.said, names[at], tc.want)
		}
	}
}

// TestBestWillNotGuessBetweenTwo : Two names equally near is a question,
// not a coin toss.
func TestBestWillNotGuessBetweenTwo(t *testing.T) {
	if _, err := heard.Best([]string{"Work Trips", "Work Tripz"}, "work tripa"); !errors.Is(err, heard.ErrSeveral) {
		t.Errorf("error = %v, want ErrSeveral", err)
	}
}

// TestExactlyKnowsTheDifference : Whether the name was said or merely
// approached, which decides if a change is confirmed first.
func TestExactlyKnowsTheDifference(t *testing.T) {
	if !heard.Exactly("rjdhanush22@gmail.com", "RJDhanush22@Gmail.com") {
		t.Error("punctuation and case should not make it a different name")
	}
	if heard.Exactly("Jarvis", "javas") {
		t.Error("javas is near Jarvis, not the same as it")
	}
}
