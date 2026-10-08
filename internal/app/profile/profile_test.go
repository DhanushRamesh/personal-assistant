package profile_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/app/profile"
	"personal-assistant/internal/base/records/conversation"
	"personal-assistant/internal/base/world/event"
	"personal-assistant/internal/base/world/routine"
	"personal-assistant/internal/domain/memory"
	"personal-assistant/internal/domain/memory/inmemory"
)

// reported : A stand-in for the event store.
type reported struct{ events []event.Event }

func (r *reported) Recent(context.Context, string, event.Query) ([]event.Event, error) {
	return r.events, nil
}

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

	// Something said since, or there would be nothing to rebuild from
	// and the second build would rightly decline to make the same
	// description twice.
	h.msgs = append(h.msgs, conversation.Message{
		Kind: conversation.Chat, Role: conversation.User,
		Content: "what about the garden", At: at().Add(time.Minute),
	})

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
	// The owner's three are what the prose part is for.
	p := profile.Prompt(profile.Like, profile.Evidence{Said: said(3)})

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
	p := profile.Prompt(profile.SoFar, profile.Evidence{Said: said(3)})

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

// TestNothingSaidSinceIsNotRebuilt : It runs hourly, and most hours
// nothing has happened. Asking a model to write the same paragraph from
// the same week, every hour, for ever, is the whole cost of the
// cadence and none of the benefit.
func TestNothingSaidSinceIsNotRebuilt(t *testing.T) {
	h := &heard{msgs: said(40)}
	asked := 0
	b, store := build(t, h, "He asks about the roof.", nil)
	if err := b.Build(context.Background(), "usr_1"); err != nil {
		t.Fatalf("Build: %v", err)
	}
	first := held(t, store).Body

	// The same builder, as the server has: one, ticking hourly.
	b.Ask = func(context.Context, string) (string, error) {
		asked++
		return "something else entirely", nil
	}
	for range 5 {
		if err := b.Build(context.Background(), "usr_1"); err != nil {
			t.Fatalf("rebuild: %v", err)
		}
	}
	if asked != 0 {
		t.Errorf("the model was asked %d times with nothing new to say", asked)
	}
	if got := held(t, store).Body; got != first {
		t.Errorf("the description changed without anything happening: %q", got)
	}
}

// TestTheDescriptionSoFarIsGivenBackToBeCorrected : It is a draft, not a
// source, and the prompt has to say so or a rebuild inherits whatever
// the last one invented.
func TestTheDescriptionSoFarIsGivenBackToBeCorrected(t *testing.T) {
	h := &heard{msgs: said(40)}
	var sent string
	b, store := build(t, h, "He asks about the roof.", nil)
	if err := b.Build(context.Background(), "usr_1"); err != nil {
		t.Fatalf("Build: %v", err)
	}

	h.msgs = append(h.msgs, conversation.Message{
		Kind: conversation.Chat, Role: conversation.User,
		Content: "and the garden", At: at().Add(time.Minute),
	})
	b2 := &profile.Builder{Said: h, Memories: store, Now: at,
		Ask: func(_ context.Context, p string) (string, error) {
			sent = p
			return "He asks about the garden.", nil
		}}
	if err := b2.Build(context.Background(), "usr_1"); err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	if !strings.Contains(sent, "He asks about the roof.") {
		t.Error("the description so far was not given back")
	}
	for _, want := range []string{"draft to correct", "Delete anything"} {
		if !strings.Contains(sent, want) {
			t.Errorf("the prompt does not say %q", want)
		}
	}
}

// TestAnAfternoonOfTestingIsNotACharacter : The bug this exists for.
//
// The description written on 4 October 2026 said he "repeats himself
// heavily, sometimes cycling through the same request a dozen times
// in a row" and "loses patience quickly". Neither was about him. It
// was an afternoon of saying the same questions into the microphone
// to time them.
func TestAnAfternoonOfTestingIsNotACharacter(t *testing.T) {
	at := time.Date(2026, 10, 3, 18, 0, 0, 0, time.UTC)
	var said []conversation.Message
	for i := 0; i < 11; i++ {
		said = append(said, conversation.Message{
			Content: "What is the time?",
			At:      at.Add(time.Duration(i) * 20 * time.Second),
		})
	}
	said = append(said, conversation.Message{Content: "What is the time?", At: at.Add(90 * time.Minute)})

	got := profile.Folded(said)
	if len(got) != 2 {
		t.Fatalf("eleven askings in four minutes became %d, want 2 "+
			"(the run, and the one an hour and a half later)", len(got))
	}
	if !got[1].At.Equal(at.Add(90 * time.Minute)) {
		t.Errorf("the later asking was folded away: %v", got[1].At)
	}
}

// TestPunctuationAndCaseAreNotADifferentSentence : "Hello?" and
// "hello." are one thing said twice. Speech punctuates differently
// every time, so matching on the characters would fold nothing.
func TestPunctuationAndCaseAreNotADifferentSentence(t *testing.T) {
	at := time.Date(2026, 10, 3, 18, 0, 0, 0, time.UTC)
	got := profile.Folded([]conversation.Message{
		{Content: "Hello?", At: at},
		{Content: "hello.", At: at.Add(time.Second)},
		{Content: "  HELLO  ", At: at.Add(2 * time.Second)},
	})
	if len(got) != 1 {
		t.Errorf("the same greeting three ways stayed %d messages", len(got))
	}
}

// TestDifferentThingsAreKept : The fold must not quietly eat a week.
func TestDifferentThingsAreKept(t *testing.T) {
	at := time.Date(2026, 10, 3, 18, 0, 0, 0, time.UTC)
	got := profile.Folded([]conversation.Message{
		{Content: "what reminders do I have", At: at},
		{Content: "what is on my calendar tomorrow", At: at.Add(time.Minute)},
		{Content: "call Alekhya", At: at.Add(2 * time.Minute)},
	})
	if len(got) != 3 {
		t.Errorf("three different things became %d", len(got))
	}
}

// TestThePromptAsksForTwoLayers : The description is read before
// every answer and acted on, so the part of it that can be checked
// has to be separable from the part that cannot.
//
// Prose alone was the whole of it until 4 October 2026, and a wrong
// line in prose lives until somebody notices and says so. A line
// carrying a count can be argued with.
func TestThePromptAsksForTwoLayers(t *testing.T) {
	// One call per part since 7 October, so each asks for its own
	// and is told not to write the others.
	for _, part := range profile.Parts {
		asked := profile.Prompt(part, profile.Evidence{Said: said(3)})
		if !strings.Contains(asked, string(part)) {
			t.Errorf("the prompt for %q does not name it", part)
		}
		if !strings.Contains(asked, "do not write any of the others") {
			t.Errorf("the prompt for %q does not keep to its own part", part)
		}
		for _, other := range profile.Parts {
			if other != part && strings.Contains(asked, "\n"+string(other)) {
				t.Errorf("the prompt for %q also asks for %q", part, other)
			}
		}
	}

	counted := profile.Prompt(profile.SoFar, profile.Evidence{Said: said(3)})
	if !strings.Contains(counted, "cannot put a count on does not belong") {
		t.Error("a fact without a count is not refused")
	}
	// The brief is the job, and "summary" was the old one.
	if !strings.Contains(counted, "Not a summary") {
		t.Error("the prompt still reads as a request for a summary")
	}
}

// TestTheMethodNamesNoKindOfThing : The owner's rule. The incoming
// events describe themselves, so a prompt that names kinds rots the
// day a new sender is added.
func TestTheMethodNamesNoKindOfThing(t *testing.T) {
	p := profile.Prompt(profile.SoFar, profile.Evidence{Said: said(3)})

	for _, named := range []string{
		"wifi", "bluetooth", "calendar", "music", "call.", "location",
		"reminder", "geofence", "battery", "step",
	} {
		if strings.Contains(strings.ToLower(p), named) {
			t.Errorf("the prompt names a kind of thing: %q", named)
		}
	}
}

// TestUsingTheAssistantIsNotTheSubject : The description said he
// repeats himself and loses patience, which was an afternoon of
// timing the same questions. The fold removes the evidence; this
// says it in words as well, because the fold only catches repetition
// inside half an hour.
func TestUsingTheAssistantIsNotTheSubject(t *testing.T) {
	p := profile.Prompt(profile.SoFar, profile.Evidence{Said: said(3)})
	if !strings.Contains(p, "not their use of the assistant") {
		t.Error("the prompt does not say to describe the person rather than the usage")
	}
}

// usualStore : Somewhere for a test to catch what was worked out.
type usualStore struct {
	kept  []routine.Fact
	times int
}

func (u *usualStore) Keep(_ context.Context, _ string, f []routine.Fact) error {
	u.kept, u.times = f, u.times+1
	return nil
}
func (u *usualStore) All(context.Context, string) ([]routine.Fact, error) {
	return u.kept, nil
}

// TestTheRoutineIsWorkedOutOnAQuietWeek : The description rebuilds
// only when somebody has said something, which is right -- events
// arrive all day and no paragraph turns on one more. The routine is
// the opposite: it is made of events, and something acts on it
// unattended, so a week of silence must not leave it as it was a
// fortnight ago.
func TestTheRoutineIsWorkedOutOnAQuietWeek(t *testing.T) {
	at := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	usually := &usualStore{}

	// Four mornings leaving home at ten, and not a word said.
	var did []event.Event
	for d := 0; d < 4; d++ {
		when := at.AddDate(0, 0, -d-1)
		did = append(did, event.Event{
			Kind:       "place.exited",
			Payload:    []byte(`{"value":"home"}`),
			OccurredAt: time.Date(when.Year(), when.Month(), when.Day(), 10, 5, 0, 0, time.UTC),
			ReceivedAt: at,
		})
	}

	b := &profile.Builder{
		Said:     &heard{},
		Did:      &reported{events: did},
		Memories: inmemory.New(),
		Usually:  usually,
		Where:    time.UTC,
		Now:      func() time.Time { return at },
		Ask: func(context.Context, string) (string, error) {
			t.Error("a model was asked to describe somebody who said nothing")
			return "", nil
		},
	}
	if err := b.Build(context.Background(), "usr_1"); err != nil {
		t.Fatal(err)
	}

	if usually.times != 1 {
		t.Fatalf("the routine was worked out %d times, want once", usually.times)
	}
	if len(usually.kept) != 1 {
		t.Fatalf("kept %d facts, want the one hour the evidence supports", len(usually.kept))
	}
	if got := usually.kept[0]; got.Name() != "usually.home.leaves" || got.Hour != 10 {
		t.Errorf("worked out %q at %d, want usually.home.leaves at 10", got.Name(), got.Hour)
	}
}
