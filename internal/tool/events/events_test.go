package events_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
	"github.com/DhanushRamesh/personal-assistant/internal/event"
	"github.com/DhanushRamesh/personal-assistant/internal/event/inmemory"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
	eventstool "github.com/DhanushRamesh/personal-assistant/internal/tool/events"
)

// kolkata : The person's zone, which is what a written hour means.
var kolkata = time.FixedZone("IST", 5*3600+1800)

// noon : Midday on 1 October 2026, local.
var noon = time.Date(2026, 10, 1, 12, 0, 0, 0, kolkata)

func store(t *testing.T, events ...*event.Event) *inmemory.Store {
	t.Helper()
	s := inmemory.New()
	if _, _, err := s.Record(context.Background(), "usr_1", events); err != nil {
		t.Fatalf("storing: %v", err)
	}
	return s
}

func at(t *testing.T, hour, minute int, kind, value string) *event.Event {
	t.Helper()
	when := time.Date(2026, 10, 1, hour, minute, 0, 0, kolkata)
	payload := json.RawMessage(`{}`)
	if value != "" {
		payload = json.RawMessage(`{"value":"` + value + `"}`)
	}
	e, err := event.New("usr_1", "tasker", "pixel-7", kind,
		when, when.Add(time.Second), payload,
		kind+value+when.Format("150405"))
	if err != nil {
		t.Fatalf("building an event: %v", err)
	}
	return e
}

func ask(t *testing.T, s *inmemory.Store, args string) tool.Result {
	t.Helper()
	tools := eventstool.Tools(s, eventstool.Clock{
		Now:      func() time.Time { return noon.UTC() },
		Location: kolkata,
	})
	if len(tools) != 1 {
		t.Fatalf("expected one tool, got %d", len(tools))
	}
	return tools[0].Run(context.Background(), tool.Invocation{
		Caller: tool.Caller{UserID: "usr_1"},
		Args:   json.RawMessage(args),
	})
}

func TestTodaysEventsAreDescribedOldestFirst(t *testing.T) {
	s := store(t,
		at(t, 9, 2, "network.joined", "Zoho-Guest"),
		at(t, 11, 30, "network.left", "Zoho-Guest"),
	)
	got := ask(t, s, `{}`)
	if got.Outcome == conversation.OutcomeFailed {
		t.Fatalf("failed: %s", got.Content)
	}
	joined := strings.Index(got.Content, "network.joined")
	left := strings.Index(got.Content, "network.left")
	if joined < 0 || left < 0 || joined > left {
		t.Fatalf("a day should read forwards:\n%s", got.Content)
	}
	if !strings.Contains(got.Content, "09:02") {
		t.Fatalf("local times are missing:\n%s", got.Content)
	}
}

// A phone reports a network dropping and returning eight times over
// lunch. Handing all of that over spends the turn and gets the whole list
// read back.
func TestRepeatsAreCollapsedIntoOneEntry(t *testing.T) {
	var evs []*event.Event
	for i := range 8 {
		evs = append(evs, at(t, 13, i*2, "network.left", "Zoho-Guest"))
	}
	got := ask(t, store(t, evs...), `{}`)

	if n := strings.Count(got.Content, "network.left"); n != 1 {
		t.Fatalf("expected one entry, got %d:\n%s", n, got.Content)
	}
	if !strings.Contains(got.Content, "8 times") {
		t.Fatalf("the count is missing:\n%s", got.Content)
	}
}

// A gap is information: joining the same network at nine and at six is
// two facts, not one run.
func TestThingsFarApartAreNotCollapsed(t *testing.T) {
	got := ask(t, store(t,
		at(t, 9, 0, "network.joined", "Zoho-Guest"),
		at(t, 18, 0, "network.joined", "Zoho-Guest"),
	), `{}`)

	if n := strings.Count(got.Content, "network.joined"); n != 2 {
		t.Fatalf("expected two entries, got %d:\n%s", n, got.Content)
	}
}

// Different values are different facts even back to back.
func TestDifferentValuesAreNotCollapsed(t *testing.T) {
	got := ask(t, store(t,
		at(t, 9, 0, "network.joined", "Zoho-Guest"),
		at(t, 9, 5, "network.joined", "Home"),
	), `{}`)

	if !strings.Contains(got.Content, "Zoho-Guest") || !strings.Contains(got.Content, "Home") {
		t.Fatalf("both networks should appear:\n%s", got.Content)
	}
}

// The likeliest reason a kind finds nothing is that it was guessed at, so
// the answer says which kinds exist.
func TestAnUnknownKindIsToldWhatExists(t *testing.T) {
	got := ask(t, store(t, at(t, 9, 0, "network.joined", "Zoho-Guest")),
		`{"kind":"sleep.started"}`)

	if !strings.Contains(got.Content, "network.joined") {
		t.Fatalf("the kinds that exist are missing:\n%s", got.Content)
	}
}

func TestAQuietDaySaysSo(t *testing.T) {
	got := ask(t, inmemory.New(), `{}`)
	if got.Outcome == conversation.OutcomeFailed {
		t.Fatalf("an empty day should not be a failure: %s", got.Content)
	}
	if !strings.Contains(got.Content, "nothing") {
		t.Fatalf("expected it to say nothing happened:\n%s", got.Content)
	}
}

// "Today" means since midnight. Measuring back from now would answer a
// question about this morning with yesterday evening in it.
func TestTodayMeansSinceMidnightNotTwentyFourHours(t *testing.T) {
	yesterday := time.Date(2026, 9, 30, 20, 0, 0, 0, kolkata)
	e, err := event.New("usr_1", "tasker", "", "network.joined",
		yesterday, yesterday, json.RawMessage(`{"value":"Home"}`), "y1")
	if err != nil {
		t.Fatalf("building: %v", err)
	}
	got := ask(t, store(t, e, at(t, 9, 0, "network.joined", "Zoho-Guest")), `{}`)

	if strings.Contains(got.Content, "Home") {
		t.Fatalf("yesterday evening leaked into today:\n%s", got.Content)
	}
}

func TestAWiderWindowReachesBack(t *testing.T) {
	yesterday := time.Date(2026, 9, 30, 20, 0, 0, 0, kolkata)
	e, err := event.New("usr_1", "tasker", "", "network.joined",
		yesterday, yesterday, json.RawMessage(`{"value":"Home"}`), "y1")
	if err != nil {
		t.Fatalf("building: %v", err)
	}
	got := ask(t, store(t, e), `{"days":7}`)

	if !strings.Contains(got.Content, "Home") {
		t.Fatalf("a week should reach yesterday:\n%s", got.Content)
	}
}

func TestAnUnknownCallerIsRefused(t *testing.T) {
	tools := eventstool.Tools(inmemory.New(), eventstool.Clock{})
	got := tools[0].Run(context.Background(), tool.Invocation{
		Args: json.RawMessage(`{}`),
	})
	if got.Outcome != conversation.OutcomeFailed {
		t.Fatal("an anonymous caller should be refused")
	}
}

// A kind ending in a dot is a family. The caller cannot know which kinds
// exist -- any device may invent one -- so asking for "network." must
// cover joining and leaving without naming either.
func TestATrailingDotAsksForAFamilyOfKinds(t *testing.T) {
	got := ask(t, store(t,
		at(t, 9, 0, "network.joined", "Zoho-Guest"),
		at(t, 9, 30, "network.left", "Zoho-Guest"),
		at(t, 10, 0, "battery.low", ""),
	), `{"kind":"network."}`)

	if !strings.Contains(got.Content, "network.joined") ||
		!strings.Contains(got.Content, "network.left") {
		t.Fatalf("both network kinds should appear:\n%s", got.Content)
	}
	if strings.Contains(got.Content, "battery.low") {
		t.Fatalf("an unrelated kind leaked in:\n%s", got.Content)
	}
}

// Some questions are about a place rather than a kind.
func TestEventsCanBeFilteredByWhatTheyMention(t *testing.T) {
	got := ask(t, store(t,
		at(t, 9, 0, "network.joined", "Zoho-Guest"),
		at(t, 10, 0, "network.joined", "Home"),
	), `{"contains":"Home"}`)

	if !strings.Contains(got.Content, "Home") {
		t.Fatalf("the match is missing:\n%s", got.Content)
	}
	if strings.Contains(got.Content, "Zoho-Guest") {
		t.Fatalf("a non-match leaked in:\n%s", got.Content)
	}
}

func TestAParticularDayCanBeAskedFor(t *testing.T) {
	yesterday := time.Date(2026, 9, 30, 20, 0, 0, 0, kolkata)
	e, err := event.New("usr_1", "tasker", "", "network.joined",
		yesterday, yesterday, json.RawMessage(`{"value":"Home"}`), "y1")
	if err != nil {
		t.Fatalf("building: %v", err)
	}
	s := store(t, e, at(t, 9, 0, "network.joined", "Zoho-Guest"))

	got := ask(t, s, `{"on":"2026-09-30"}`)
	if !strings.Contains(got.Content, "Home") {
		t.Fatalf("the named day is missing:\n%s", got.Content)
	}
	if strings.Contains(got.Content, "Zoho-Guest") {
		t.Fatalf("another day leaked in:\n%s", got.Content)
	}
}

func TestABadDayIsRefusedClearly(t *testing.T) {
	got := ask(t, inmemory.New(), `{"on":"last Tuesday"}`)
	if got.Outcome != conversation.OutcomeFailed {
		t.Fatalf("an unreadable day should be refused: %s", got.Content)
	}
	if !strings.Contains(got.Content, "YYYY-MM-DD") {
		t.Fatalf("the refusal should say the shape wanted:\n%s", got.Content)
	}
}

// "When did I last..." wants one answer, not a day of them.
func TestALimitOfOneGivesTheMostRecent(t *testing.T) {
	got := ask(t, store(t,
		at(t, 9, 0, "network.joined", "Zoho-Guest"),
		at(t, 17, 0, "network.joined", "Home"),
	), `{"limit":1}`)

	if !strings.Contains(got.Content, "Home") {
		t.Fatalf("the most recent is missing:\n%s", got.Content)
	}
	if strings.Contains(got.Content, "Zoho-Guest") {
		t.Fatalf("more than one was returned:\n%s", got.Content)
	}
}

// A filter that finds nothing is most likely a guess, so the answer says
// what does exist rather than implying a quiet day.
func TestAnEmptyFilteredAnswerStillNamesTheKinds(t *testing.T) {
	got := ask(t, store(t, at(t, 9, 0, "network.joined", "Zoho-Guest")),
		`{"contains":"Paris"}`)

	if !strings.Contains(got.Content, "network.joined") {
		t.Fatalf("the kinds that exist are missing:\n%s", got.Content)
	}
}
