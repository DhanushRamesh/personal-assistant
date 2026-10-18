package catching_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/app/catching"
	"personal-assistant/internal/base/records/conversation"
)

// said : Messages, as a store would hand them back.
type said struct {
	msgs  []conversation.Message
	since time.Time
	limit int
	err   error
}

func (s *said) SaidSince(_ context.Context, _ string, since time.Time, limit int) ([]conversation.Message, error) {
	s.since, s.limit = since, limit
	return s.msgs, s.err
}

func at(day, hour int, role conversation.Role, text string) conversation.Message {
	return conversation.Message{
		Role: role, Content: text,
		At: time.Date(2026, 10, day, hour, 0, 0, 0, time.UTC),
	}
}

func fixed() func() time.Time {
	return func() time.Time { return time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC) }
}

// TestItIsShownWhatTheySaidAndNotWhatItSaid : The assistant's own half
// of the conversation is the half most likely to contain a date -- it
// reads entries out, it offers, it confirms. A pass shown its own words
// would write down the diary it had just read aloud.
func TestItIsShownWhatTheySaidAndNotWhatItSaid(t *testing.T) {
	store := &said{msgs: []conversation.Message{
		at(17, 10, conversation.User, "the dentist is on the 23rd at four"),
		at(17, 10, conversation.Assistant, "You have the audit on the 29th, sir."),
		at(17, 11, conversation.Tool, "calendar_events returned 3 entries"),
	}}
	var shown string
	p := catching.Pass{
		Said: store, Now: fixed(),
		Ask: func(_ context.Context, _, _, ask string) (string, error) {
			shown = ask
			return "Nothing was missing.", nil
		},
	}
	looked, err := p.Run(context.Background(), "usr_1")
	if err != nil {
		t.Fatal(err)
	}
	if looked.Read != 1 {
		t.Errorf("read %d messages, want only the person's own", looked.Read)
	}
	if !strings.Contains(shown, "dentist") {
		t.Errorf("what they said never reached the model:\n%s", shown)
	}
	if strings.Contains(shown, "audit") || strings.Contains(shown, "calendar_events") {
		t.Errorf("it was shown its own words, which is how a read diary gets written back:\n%s", shown)
	}
}

// TestItSaysNothingWhenNobodySpoke : A day with no messages is not a
// day to ask a model about.
func TestItSaysNothingWhenNobodySpoke(t *testing.T) {
	asked := false
	p := catching.Pass{
		Said: &said{msgs: []conversation.Message{
			at(17, 10, conversation.Assistant, "Good morning, sir."),
		}},
		Now: fixed(),
		Ask: func(context.Context, string, string, string) (string, error) {
			asked = true
			return "", nil
		},
	}
	if _, err := p.Run(context.Background(), "usr_1"); err != nil {
		t.Fatal(err)
	}
	if asked {
		t.Error("a model was asked about a day in which they said nothing")
	}
}

// TestItLooksBackFurtherThanADay : The pass runs daily but not to the
// minute, and an arrangement falling in the gap between two runs would
// be lost for good. Seeing a few hours twice costs nothing, because the
// second look reads the diary and finds it already there.
func TestItLooksBackFurtherThanADay(t *testing.T) {
	store := &said{msgs: []conversation.Message{
		at(17, 10, conversation.User, "the dentist is on the 23rd at four"),
	}}
	p := catching.Pass{
		Said: store, Now: fixed(),
		Ask: func(context.Context, string, string, string) (string, error) { return "", nil },
	}
	if _, err := p.Run(context.Background(), "usr_1"); err != nil {
		t.Fatal(err)
	}
	if gap := fixed()().Sub(store.since); gap <= 24*time.Hour {
		t.Errorf("looked back %v, want more than a day so nothing falls between two passes", gap)
	}
	if store.limit != catching.Messages {
		t.Errorf("limit = %d, want %d", store.limit, catching.Messages)
	}
}

// TestADayIsHeadedOnce : What matters is which day a thing was said on,
// since that is what "tomorrow" is counted from. A clock time per line
// and no heading leaves the model to work the date out itself.
func TestADayIsHeadedOnce(t *testing.T) {
	shown := catching.Shown([]conversation.Message{
		at(17, 10, conversation.User, "the dentist is on the 23rd"),
		at(17, 14, conversation.User, "and I must buy a charger"),
		at(18, 8, conversation.User, "amma arrives Friday"),
	}, time.UTC)

	if n := strings.Count(shown, "Saturday 17 October"); n != 1 {
		t.Errorf("the day appears %d times, want once as a heading:\n%s", n, shown)
	}
	if !strings.Contains(shown, "Sunday 18 October") {
		t.Errorf("the second day was never headed:\n%s", shown)
	}
	if !strings.Contains(shown, "10:00 am") || !strings.Contains(shown, "2:00 pm") {
		t.Errorf("the times are missing or not in twelve-hour form:\n%s", shown)
	}
}

// TestAFailureToReadIsNotSilence : A pass that cannot read their
// messages has found nothing, which looks exactly like a quiet day.
func TestAFailureToReadIsNotSilence(t *testing.T) {
	p := catching.Pass{
		Said: &said{err: errors.New("the database is away")},
		Now:  fixed(),
		Ask:  func(context.Context, string, string, string) (string, error) { return "", nil },
	}
	if _, err := p.Run(context.Background(), "usr_1"); err == nil {
		t.Error("a store that could not be read came back as a quiet day")
	}
}

// TestNothingRunsWithoutAModel : The right behaviour for a server with
// no model configured, which is every test server and some real ones.
func TestNothingRunsWithoutAModel(t *testing.T) {
	store := &said{msgs: []conversation.Message{at(17, 10, conversation.User, "anything")}}
	if _, err := (catching.Pass{Said: store, Now: fixed()}).Run(context.Background(), "usr_1"); err != nil {
		t.Fatal(err)
	}
	if store.limit != 0 {
		t.Error("their messages were read for a pass that could never ask anything")
	}
}

// TestTheJudgeForbidsWeighingImportance : The bar is placement, the
// same test the live rule uses. Asked to find what is important a model
// finds something every day, and on a quiet day what it finds is small
// talk written into their diary.
func TestTheJudgeForbidsWeighingImportance(t *testing.T) {
	for _, want := range []string{
		"Placement is the test",
		"Do not weigh how important",
		"read what is already written",
		"cannot hold an hour",
	} {
		if !strings.Contains(strings.ToLower(catching.Judge), strings.ToLower(want)) {
			t.Errorf("the judge never says %q", want)
		}
	}
}
