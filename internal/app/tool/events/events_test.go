package events_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/app/tool"
	eventstool "personal-assistant/internal/app/tool/events"
	"personal-assistant/internal/base/conversation"
	"personal-assistant/internal/base/event"
	"personal-assistant/internal/base/event/inmemory"
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
	return run(t, s, "event_recent", args)
}

// askKinds : What the listing says exists.
func askKinds(t *testing.T, s *inmemory.Store) tool.Result {
	t.Helper()
	return run(t, s, "event_kinds", `{}`)
}

// run : One tool of this domain, by name.
func run(t *testing.T, s *inmemory.Store, name, args string) tool.Result {
	t.Helper()
	tools := eventstool.Tools(s, eventstool.Clock{
		Now:      func() time.Time { return noon.UTC() },
		Location: kolkata,
	})
	for _, x := range tools {
		if x.Name == name {
			return x.Run(context.Background(), tool.Invocation{
				Caller: tool.Caller{UserID: "usr_1"},
				Args:   json.RawMessage(args),
			})
		}
	}
	t.Fatalf("%s is not registered", name)
	return tool.Result{}
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

// TestPositionReadingsAreNeverReturned : The readings a phone sends every
// few minutes are working material, not an answer.
//
// They outnumber everything else by two orders of magnitude, so without
// this a day's listing is three hundred coordinates and the two things
// that happened fall off the end of the limit.
func TestPositionReadingsAreNeverReturned(t *testing.T) {
	events := []*event.Event{at(t, 9, 0, "network.joined", "home")}
	for minute := 0; minute < 60; minute += 5 {
		events = append(events, at(t, 10, minute, event.Fixed, "12.911,80.062"))
	}
	events = append(events, at(t, 11, 0, event.Stayed, "the office"))
	s := store(t, events...)

	// Asked for in general, and as a family. Neither may mention one.
	for _, args := range []string{`{"days":1}`, `{"days":1,"kind":"location."}`} {
		got := ask(t, s, args).Content
		if strings.Contains(got, event.Fixed) || strings.Contains(got, "12.911") {
			t.Errorf("%s returned position readings:\n%s", args, got)
		}
	}

	// Asked for by name, and by a coordinate in the payload. Both say
	// nothing was found -- which does quote the question back, so what
	// is checked is that no reading came with it.
	for _, args := range []string{
		`{"days":1,"kind":"location.fix"}`,
		`{"days":1,"contains":"12.911"}`,
	} {
		got := ask(t, s, args).Content
		if !strings.HasPrefix(got, "Nothing ") {
			t.Errorf("%s should have found nothing, got:\n%s", args, got)
		}
		if strings.Contains(got, "10:") {
			t.Errorf("%s returned a reading:\n%s", args, got)
		}
	}

	// And the things worth seeing are still there.
	got := ask(t, s, `{"days":1}`).Content
	for _, want := range []string{"network.joined", event.Stayed} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in:\n%s", want, got)
		}
	}
}

// TestPositionReadingsAreNotOffered : A kind listing that names them is
// an invitation to ask again for something that always comes back empty.
func TestPositionReadingsAreNotOffered(t *testing.T) {
	s := store(t, at(t, 10, 0, event.Fixed, "12.911,80.062"))
	got := ask(t, s, `{"days":1,"kind":"battery.low"}`).Content
	if strings.Contains(got, event.Fixed) {
		t.Errorf("the kind listing offered position readings:\n%s", got)
	}
}

// TestMusicIsLeftOutUntilItIsAskedFor : A track a song is a hundred
// rows an evening against a couple of dozen of everything else, so
// handing them over unasked buries the day. Asking for the kind is a
// real question with a real answer, though, which is where this
// differs from the position readings.
func TestMusicIsLeftOutUntilItIsAskedFor(t *testing.T) {
	for _, c := range []struct {
		what, kind string
		want       bool
	}{
		{"nothing asked for", "", false},
		{"another kind", "call.received", false},
		{"the music kind by name", event.State, true},
		{"the kind the phone used to send", event.Playing, true},
		{"the music family", "music.", true},
		{"the pauses", event.Paused, true},
	} {
		if got := event.Working(c.kind); got != c.want {
			t.Errorf("%s: Working(%q) = %v, want %v", c.what, c.kind, got, c.want)
		}
	}
	// The position readings stay out whatever is asked, which the
	// tool enforces separately; what matters here is that they are
	// never mistaken for something worth handing over.
	if !event.Working(event.Fixed) {
		t.Error("position readings should count as working material")
	}
}

// TestAFamilyIsFoundWithoutTheDot : The bug this exists for.
//
// Asked who had called, the model called event_recent with kind
// "call", which is how a person says it. Only a kind ending in a dot
// counted as a family, so it matched nothing -- and the answer said
// so in a sentence that went on to list call.received among the
// kinds that exist.
//
// The owner had had five calls that morning and was told twice that
// he had had none.
func TestAFamilyIsFoundWithoutTheDot(t *testing.T) {
	s := store(t,
		at(t, 10, 33, "call.received", "Amma"),
		at(t, 10, 45, "call.made", "Kliffy"),
		at(t, 11, 3, "call.received", "Kliffy"),
		at(t, 9, 2, "wifi.joined", "home"),
	)

	for _, asked := range []string{`{"kind":"call"}`, `{"kind":"call."}`} {
		got := ask(t, s, asked)
		if got.Outcome == conversation.OutcomeFailed {
			t.Fatalf("%s failed: %s", asked, got.Content)
		}
		for _, want := range []string{"Amma", "Kliffy"} {
			if !strings.Contains(got.Content, want) {
				t.Errorf("%s did not find %s:\n%s", asked, want, got.Content)
			}
		}
		// And it must not drag in everything else.
		if strings.Contains(got.Content, "wifi.joined") {
			t.Errorf("%s matched a different family:\n%s", asked, got.Content)
		}
	}
}

// TestAFullNameIsStillExact : "call.received" means that one, not
// the family. Widening the dotless case must not widen this.
func TestAFullNameIsStillExact(t *testing.T) {
	s := store(t,
		at(t, 10, 33, "call.received", "Amma"),
		at(t, 10, 45, "call.made", "Kliffy"),
	)
	got := ask(t, s, `{"kind":"call.received"}`)
	if !strings.Contains(got.Content, "Amma") {
		t.Errorf("the exact kind was not found:\n%s", got.Content)
	}
	// The reading stays exact: the sibling's own event is not in
	// it. That it is named afterwards is a different thing, and
	// the test below is about that.
	if strings.Contains(got.Content, "Kliffy") {
		t.Errorf("an exact kind matched its sibling:\n%s", got.Content)
	}
}

// Asked "who called me recently", the model read call.received,
// found yesterday's, and said "you received no calls today,
// sir". There had been a missed call that morning. The
// description already warned against narrowing a family from the
// words of a question and the warning did not hold, so the
// reading now says what else of the family is there.
func TestTheRestOfTheFamilyIsNamed(t *testing.T) {
	s := store(t,
		at(t, 9, 28, "call.received", "Alekhya"),
		at(t, 9, 49, "call.missed", "unknown"),
	)

	got := ask(t, s, `{"kind":"call.received"}`)
	if !strings.Contains(got.Content, "call.missed 1") {
		t.Errorf("the missed call is not mentioned at all:\n%s", got.Content)
	}
	if !strings.Contains(got.Content, "did not ask for it") {
		t.Errorf("it is not said to be outside what was asked:\n%s", got.Content)
	}
}

// A kind with no family has nothing to add, and a prefix has
// already read the whole of it.
func TestNothingIsAddedWhereThereIsNoFamilyToAdd(t *testing.T) {
	s := store(t,
		at(t, 9, 28, "call.received", "Alekhya"),
		at(t, 9, 49, "call.missed", "unknown"),
	)

	if got := ask(t, s, `{"kind":"call."}`); strings.Contains(got.Content, "did not ask for it") {
		t.Errorf("a family read whole was told about itself:\n%s", got.Content)
	}
}

// lasting : One event carrying how long it ran.
func lasting(t *testing.T, hour, minute int, kind, value string, seconds int) *event.Event {
	t.Helper()
	e := at(t, hour, minute, kind, value)
	e.Payload = json.RawMessage(fmt.Sprintf(
		`{"value":%q,"seconds":%d}`, value, seconds))
	return e
}

// TestAShortCallIsJustBrief : Owner: *"don't need to say exact 19
// secs, 23 secs -- those are unwanted."* Handed the number, the
// model reads it out, and four calls recited to the second is the
// call log rather than an answer about the morning.
func TestAShortCallIsJustBrief(t *testing.T) {
	s := store(t,
		lasting(t, 10, 33, "call.received", "Amma", 19),
		lasting(t, 11, 3, "call.received", "Kliffy", 438),
	)
	got := ask(t, s, `{"kind":"call."}`)
	for _, gone := range []string{"19", "438", "seconds="} {
		if strings.Contains(got.Content, gone) {
			t.Errorf("a count of seconds survived (%q):\n%s", gone, got.Content)
		}
	}
	if !strings.Contains(got.Content, "lasted=brief") {
		t.Errorf("a short call was not simply brief:\n%s", got.Content)
	}
	if !strings.Contains(got.Content, "lasted=7 minutes") {
		t.Errorf("a long call lost its length:\n%s", got.Content)
	}
}

// TestTheKindsAreListedWithoutBeingAsked : Owner: *"make the model
// well aware of the phone device events."* Asked whether his phone
// was locked, the assistant answered that its tools covered "calls,
// networks joined, music playing, battery level" and that the lock
// state was not among them. Three of those four were guesses, and
// screen.locked had been arriving all day.
//
// event_recent names no kinds on purpose, so the list has to come
// from the data. Nothing here knows what a screen is.
func TestTheKindsAreListedWithoutBeingAsked(t *testing.T) {
	s := store(t,
		at(t, 9, 0, "screen.unlocked", ""),
		at(t, 9, 5, "call.received", "Amma"),
		at(t, 9, 9, "call.received", "Amma"),
	)
	got := askKinds(t, s)

	for _, want := range []string{"screen.unlocked", "call.received", "and nothing else"} {
		if !strings.Contains(got.Content, want) {
			t.Errorf("the listing is missing %q:\n%s", want, got.Content)
		}
	}
	if got.Outcome == conversation.OutcomeFailed {
		t.Errorf("listing the kinds failed: %s", got.Content)
	}
}

// TestTheKindsGoFirst : It is useless unless the model is holding it
// before it decides, which is what Prefetch means.
func TestTheKindsGoFirst(t *testing.T) {
	for _, x := range eventstool.Tools(store(t), eventstool.Clock{
		Now: func() time.Time { return noon.UTC() }, Location: kolkata}) {
		if x.Name == "event_kinds" {
			if !x.Prefetch {
				t.Error("the kinds are not fetched before the model is asked")
			}
			if len(x.Params.Properties) != 0 {
				t.Error("a prefetched tool cannot take arguments; nobody is there to supply them")
			}
			return
		}
	}
	t.Fatal("event_kinds is not registered")
}

// TestANameFoundNowhereIsLookedForEverywhere : Asked whether Gunalan
// had called, the model narrowed to the kind that means an incoming
// one and the answer said nothing of that kind mentions him -- then
// listed every kind there is with its total, which says nothing
// about him at all. So it reported that he had not called, three
// hours after a call to him.
//
// Telling it not to guess a kind was written twice and ignored
// twice. The widening happens here instead.
func TestANameFoundNowhereIsLookedForEverywhere(t *testing.T) {
	at := time.Date(2026, 10, 5, 19, 12, 0, 0, time.UTC)
	store := &oneEvent{e: event.Event{
		Kind:       "call.made",
		Payload:    []byte(`{"value":"Gunalan","number":"+918610822039","seconds":4}`),
		OccurredAt: at,
	}}
	said := eventstool.Nothing(context.Background(), store, "usr_1",
		event.Query{Kind: "call.received", Contains: "Gunalan"}, "in the past month")

	if !strings.Contains(said, "Gunalan is in the window all the same") {
		t.Errorf("the name was not looked for without the kind:\n%s", said)
	}
	if !strings.Contains(said, "call.made (1)") {
		t.Errorf("the kind it is actually under was not named:\n%s", said)
	}
	// And a name that genuinely is not there still says so plainly.
	absent := eventstool.Nothing(context.Background(), store, "usr_1",
		event.Query{Kind: "call.received", Contains: "Nobody"}, "in the past month")
	if strings.Contains(absent, "in the window all the same") {
		t.Errorf("a name that is not there was claimed to be:\n%s", absent)
	}
}

// oneEvent : A reader holding one event, returned when the query
// does not pin a kind it lacks.
type oneEvent struct{ e event.Event }

func (o *oneEvent) Recent(_ context.Context, _ string, q event.Query) ([]event.Event, error) {
	if q.Kind != "" && q.Kind != o.e.Kind {
		return nil, nil
	}
	if q.Contains != "" && !strings.Contains(string(o.e.Payload), q.Contains) {
		return nil, nil
	}
	return []event.Event{o.e}, nil
}

func (o *oneEvent) Kinds(context.Context, string) ([]event.Kind, error) {
	return []event.Kind{{Kind: o.e.Kind, Count: 1}}, nil
}
