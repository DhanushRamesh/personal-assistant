package remind_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/remind"
)

// mouth : An announcer that keeps what it was given, or refuses.
type mouth struct {
	said []string
	fail error
	off  bool
}

func (m *mouth) Say(_ context.Context, message string) error {
	if m.fail != nil {
		return m.fail
	}
	m.said = append(m.said, message)
	return nil
}

func (m *mouth) Available() bool { return !m.off }

// notebook : Somewhere asides are written down.
type notebook struct {
	users []string
	texts []string
}

func (n *notebook) Reminded(_ context.Context, userID, text string) {
	n.users = append(n.users, userID)
	n.texts = append(n.texts, text)
}

// due : A reminder whose time has come.
func due() remind.Reminder {
	return remind.Reminder{
		ID: "rem_1", UserID: "usr_1", Scope: remind.ScopeUser,
		Title: "Call the bank", Body: "Call the bank",
		DueAt: time.Date(2026, 9, 27, 10, 20, 0, 0, time.UTC),
	}
}

// A reminder said aloud is written into the conversation, word for word as
// it was spoken, so the person can answer it.
func TestSpeakingAReminderWritesItDown(t *testing.T) {
	say, note := &mouth{}, &notebook{}
	aloud := remind.Aloud{Announcer: say, Announcements: note, Now: due().DueAt.UTC}

	if err := aloud.Say(context.Background(), due()); err != nil {
		t.Fatalf("saying it: %v", err)
	}

	if len(say.said) != 1 {
		t.Fatalf("spoke %v", say.said)
	}
	if len(note.texts) != 1 {
		t.Fatalf("wrote down %v, want the one reminder", note.texts)
	}
	if note.texts[0] != say.said[0] {
		t.Errorf("wrote %q but said %q", note.texts[0], say.said[0])
	}
	if note.users[0] != "usr_1" {
		t.Errorf("written against %q", note.users[0])
	}
	if !strings.Contains(note.texts[0], "Call the bank") {
		t.Errorf("written = %q", note.texts[0])
	}
}

// Nothing is written down when nothing was heard. A note of a sentence
// nobody was told is read back as context by the next turn, which then
// answers something that was never said.
func TestAReminderThatWasNotHeardIsNotWrittenDown(t *testing.T) {
	note := &notebook{}
	refused := errors.New("the satellite is unreachable")

	for _, c := range []struct {
		name  string
		aloud remind.Aloud
	}{
		{"speaking failed", remind.Aloud{Announcer: &mouth{fail: refused}, Announcements: note}},
		{"nowhere to speak", remind.Aloud{Announcer: &mouth{off: true}, Announcements: note}},
		{"no announcer at all", remind.Aloud{Announcements: note}},
	} {
		t.Run(c.name, func(t *testing.T) {
			if err := c.aloud.Say(context.Background(), due()); err == nil {
				t.Fatal("reported success with nothing spoken")
			}
			if len(note.texts) != 0 {
				t.Errorf("wrote down %v", note.texts)
			}
		})
	}
}
