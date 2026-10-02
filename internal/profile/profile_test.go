package profile_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
	"github.com/DhanushRamesh/personal-assistant/internal/memory"
	"github.com/DhanushRamesh/personal-assistant/internal/memory/inmemory"
	"github.com/DhanushRamesh/personal-assistant/internal/profile"
)

// at : A fixed week.
func at() time.Time { return time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC) }

// said : n messages from one person, spread over the week.
func said(n int) []conversation.Message {
	out := make([]conversation.Message, 0, n)
	for i := range n {
		out = append(out, conversation.Message{
			Kind: conversation.Chat, Role: conversation.User,
			Content: "remind me about the roof",
			At:      at().Add(-time.Duration(n-i) * time.Hour),
		})
	}
	return out
}

// heard : A stand-in for the conversation store.
type heard struct {
	msgs []conversation.Message
	err  error
	// since : What the builder asked for, so the window can be asserted.
	since time.Time
}

func (h *heard) SaidSince(_ context.Context, _ string, since time.Time, _ int) ([]conversation.Message, error) {
	h.since = since
	return h.msgs, h.err
}

func build(t *testing.T, h *heard, answer string, askErr error) (*profile.Builder, memory.Store) {
	t.Helper()
	store := inmemory.New()
	return &profile.Builder{
		Said:     h,
		Memories: store,
		Ask: func(context.Context, string) (string, error) {
			return answer, askErr
		},
		Now: at,
	}, store
}

// held : The profile memory, or nil.
func held(t *testing.T, store memory.Store) *memory.Memory {
	t.Helper()
	all, err := store.All(context.Background(), "usr_1", memory.TierAlways)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	for i := range all {
		if all[i].Subject == profile.Subject {
			return &all[i]
		}
	}
	return nil
}

// TestTheProfileIsWrittenAndThenRewritten : One profile, replaced each
// time, never a pile of contradicting descriptions in every prompt.
func TestTheProfileIsWrittenAndThenRewritten(t *testing.T) {
	h := &heard{msgs: said(40)}
	b, store := build(t, h, "He asks about the roof more than anything else.", nil)

	if err := b.Build(context.Background(), "usr_1"); err != nil {
		t.Fatalf("Build: %v", err)
	}
	got := held(t, store)
	if got == nil {
		t.Fatal("no profile was written")
	}
	if !strings.Contains(got.Body, "roof") {
		t.Errorf("body = %q", got.Body)
	}
	first := got.ID

	b2 := &profile.Builder{Said: h, Memories: store, Now: at,
		Ask: func(context.Context, string) (string, error) {
			return "He has moved on to the garden.", nil
		}}
	if err := b2.Build(context.Background(), "usr_1"); err != nil {
		t.Fatalf("second Build: %v", err)
	}

	all, _ := store.All(context.Background(), "usr_1", memory.TierAlways)
	profiles := 0
	for _, m := range all {
		if m.Subject == profile.Subject {
			profiles++
		}
	}
	if profiles != 1 {
		t.Errorf("%d profiles held, want the one rewritten", profiles)
	}
	if got := held(t, store); got.ID != first || !strings.Contains(got.Body, "garden") {
		t.Errorf("the profile was not rewritten in place: %+v", got)
	}
}

// TestTooLittleToGoOnWritesNothing : A confident description drawn from
// a handful of sentences is worse than none, because it goes into every
// prompt afterwards and is acted on.
func TestTooLittleToGoOnWritesNothing(t *testing.T) {
	b, store := build(t, &heard{msgs: said(3)}, "He is a deeply private man.", nil)

	if err := b.Build(context.Background(), "usr_1"); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := held(t, store); got != nil {
		t.Errorf("a profile was invented from three sentences: %q", got.Body)
	}
}

// TestAnExistingProfileSurvivesAQuietWeek : Too little to go on leaves
// what is there alone rather than replacing it with something worse.
func TestAnExistingProfileSurvivesAQuietWeek(t *testing.T) {
	b, store := build(t, &heard{msgs: said(40)}, "He asks about the roof.", nil)
	if err := b.Build(context.Background(), "usr_1"); err != nil {
		t.Fatalf("Build: %v", err)
	}

	quiet := &profile.Builder{Said: &heard{msgs: said(2)}, Memories: store, Now: at,
		Ask: func(context.Context, string) (string, error) {
			return "He barely speaks.", nil
		}}
	if err := quiet.Build(context.Background(), "usr_1"); err != nil {
		t.Fatalf("quiet Build: %v", err)
	}

	if got := held(t, store); got == nil || !strings.Contains(got.Body, "roof") {
		t.Errorf("the profile was lost to a quiet week: %+v", got)
	}
}

// TestNothingIsKeptWhenTheModelFails : A failure leaves the old profile
// standing rather than blanking it.
func TestNothingIsKeptWhenTheModelFails(t *testing.T) {
	b, store := build(t, &heard{msgs: said(40)}, "", errors.New("no"))

	if err := b.Build(context.Background(), "usr_1"); err == nil {
		t.Error("a failed description was not reported")
	}
	if got := held(t, store); got != nil {
		t.Errorf("something was written anyway: %q", got.Body)
	}
}

// TestItReadsAWeek : The window is a week, not a day: a day is one mood
// and a handful of subjects, and what recurs cannot be told from what
// happened once.
func TestItReadsAWeek(t *testing.T) {
	h := &heard{msgs: said(40)}
	b, _ := build(t, h, "Something.", nil)
	_ = b.Build(context.Background(), "usr_1")

	if want := at().Add(-7 * 24 * time.Hour); !h.since.Equal(want) {
		t.Errorf("read from %v, want %v", h.since, want)
	}
}

// TestThePromptAsksForWhatWasWanted : the things they talk about, the
// people they know, and how they behave -- the owner's three.
func TestThePromptAsksForWhatWasWanted(t *testing.T) {
	p := profile.Prompt(said(3), "")

	for _, want := range []string{"talk about", "people in their life", "behave"} {
		if !strings.Contains(p, want) {
			t.Errorf("the prompt does not ask about %q", want)
		}
	}
	// And guards against the invention this cannot otherwise catch.
	for _, want := range []string{"Only what is in front of you", "leave it out"} {
		if !strings.Contains(p, want) {
			t.Errorf("the prompt does not say %q", want)
		}
	}
	if !strings.Contains(p, "remind me about the roof") {
		t.Error("what they said is not in the prompt")
	}
}

// TestItIsToldNotToGuessOriginOrRecordHealth : The two things the first
// rebuild got wrong.
//
// It wrote "almost certainly Indian, likely based in or around
// Hyderabad" from a girlfriend's address and the films that had come
// up, and listed four symptoms mentioned in passing. Neither had been
// said. A hedge is not a defence: a guess in a description read before
// every answer is acted on exactly as a fact is.
func TestItIsToldNotToGuessOriginOrRecordHealth(t *testing.T) {
	p := profile.Prompt(said(3), "")

	for _, want := range []string{
		"Where they live, where they are from, their nationality",
		"do not reason towards it from somebody else's address",
		"leave out symptoms, conditions and medicines",
		"Hedging is not a way round",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("the prompt does not say %q", want)
		}
	}
}
