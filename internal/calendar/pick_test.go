package calendar_test

import (
	"errors"
	"testing"

	"github.com/DhanushRamesh/personal-assistant/internal/calendar"
)

// TestPickHeardNames : Names that arrived through speech still find the
// calendar they meant, and an unrecognisable one is a question rather than
// a refusal.
//
// Every spoken form here was said to the assistant and written down wrong
// by speech-to-text on 27 and 28 September 2026.
func TestPickHeardNames(t *testing.T) {
	held := []calendar.Owned{
		{ID: "a", Name: "Jarvis", Mine: true},
		{ID: "b", Name: "rjdhanush22@gmail.com"},
		{ID: "c", Name: "Holidays in India"},
	}

	for _, tc := range []struct {
		said, want string
		err        error
	}{
		{"rjdhanush22@gmail.com", "rjdhanush22@gmail.com", nil},
		{"rjdanesh22rjmail.com", "rjdhanush22@gmail.com", nil},
		{"main", "rjdhanush22@gmail.com", nil},
		{"my calendar", "rjdhanush22@gmail.com", nil},
		{"javas", "Jarvis", nil},
		{"jarvis", "Jarvis", nil},
		{"holidays", "Holidays in India", nil},
		{"holiday in india", "Holidays in India", nil},
		{"birthdays", "", calendar.ErrNoSuchCalendar},
		{"", "", calendar.ErrNoSuchCalendar},
	} {
		got, err := calendar.Pick(held, tc.said)
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
		if got.Name != tc.want {
			t.Errorf("%q -> %q, want %q", tc.said, got.Name, tc.want)
		}
	}
}

// TestPickWillNotGuessBetweenTwo : Two calendars equally near what was
// said is a question, not a coin toss. Reading the wrong calendar answers
// something nobody asked.
func TestPickWillNotGuessBetweenTwo(t *testing.T) {
	held := []calendar.Owned{
		{ID: "a", Name: "Work Trips"},
		{ID: "b", Name: "Work Tripz"},
	}
	if _, err := calendar.Pick(held, "work tripa"); !errors.Is(err, calendar.ErrWhichCalendar) {
		t.Errorf("error = %v, want ErrWhichCalendar", err)
	}
}
