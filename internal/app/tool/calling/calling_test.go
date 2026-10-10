package calling_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/app/tool"
	callingtool "personal-assistant/internal/app/tool/calling"
	"personal-assistant/internal/base/records/conversation"
	"personal-assistant/internal/base/world/calling"
	"personal-assistant/internal/base/world/event"
)

var now = time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)

// reader : Call events, newest first.
type reader struct{ events []event.Event }

func (r reader) Recent(_ context.Context, _ string, q event.Query) ([]event.Event, error) {
	var out []event.Event
	for _, e := range r.events {
		if q.Prefix != "" && !strings.HasPrefix(e.Kind, q.Prefix) {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

// rang : What was asked for.
type rang struct {
	calls []calling.Call
	fail  error
}

func (r *rang) Ring(_ context.Context, _ string, c calling.Call) error {
	if r.fail != nil {
		return r.fail
	}
	r.calls = append(r.calls, c)
	return nil
}

func called(kind, who string, ago time.Duration) event.Event {
	body, _ := json.Marshal(map[string]string{"value": who})
	return event.Event{Kind: kind, OccurredAt: now.Add(-ago), Payload: body}
}

// theRoom : The two tools and what they were given.
func theRoom(t *testing.T, events []event.Event) (tool.Tool, tool.Tool, *rang) {
	t.Helper()
	ring := &rang{}
	tools := callingtool.Tools(reader{events: events}, ring, func() time.Time { return now })
	if len(tools) != 2 {
		t.Fatalf("there are %d call tools", len(tools))
	}
	return tools[0], tools[1], ring
}

func ask(t *testing.T, x tool.Tool, args string) tool.Result {
	t.Helper()
	return x.Run(context.Background(), tool.Invocation{
		Caller: tool.Caller{UserID: "u"}, Args: json.RawMessage(args),
	})
}

var theWeek = []event.Event{
	called("call.made", "Alekhya Chintada", 2*time.Hour),
	called("call.made", "Alekhya Chintada", 26*time.Hour),
	called("call.received", "Alekhya Chintada", 30*time.Hour),
	called("call.received", "Amma", time.Hour),
	called("call.received", "Amma", 5*time.Hour),
	called("call.made", "Kliffy", 100*time.Hour),
	called("call.missed", "unknown", 3*time.Hour),
	called("call.received", "+916746580844", 90*time.Hour),
	called("call.received", "", 200*time.Hour),
	called("music.state", "something else", time.Minute),
}

func TestWhoThereIsToRingComesFromCallsEitherWay(t *testing.T) {
	people, _, _ := theRoom(t, theWeek)
	got := ask(t, people, "{}")
	if got.Outcome == conversation.OutcomeFailed {
		t.Fatalf("listing failed: %s", got.Content)
	}
	for _, want := range []string{"Alekhya Chintada", "Amma", "Kliffy"} {
		if !strings.Contains(got.Content, want) {
			t.Errorf("%q is missing from:\n%s", want, got.Content)
		}
	}
	// Somebody with no name cannot be rung back by name, and a bare
	// number is not a name.
	for _, no := range []string{"unknown", "+916746580844"} {
		if strings.Contains(got.Content, no) {
			t.Errorf("%q was offered as somebody to ring:\n%s", no, got.Content)
		}
	}
	// Most called first.
	if strings.Index(got.Content, "Alekhya") > strings.Index(got.Content, "Kliffy") {
		t.Errorf("they are not in order of how often:\n%s", got.Content)
	}
}

// The owner's rule, in so many words: *"confirm always."*
func TestNothingIsDialledUntilItHasBeenPutToThem(t *testing.T) {
	_, place, ring := theRoom(t, theWeek)
	got := ask(t, place, `{"who":"Amma"}`)
	if got.Outcome != conversation.OutcomeFailed {
		t.Errorf("a call went through without being asked about: %s", got.Content)
	}
	if len(ring.calls) != 0 {
		t.Fatalf("it dialled %+v", ring.calls)
	}
	if !strings.Contains(got.Content, "Amma") {
		t.Errorf("it did not say who it would ring:\n%s", got.Content)
	}
}

func TestOnceTheyHaveSaidYesItRings(t *testing.T) {
	_, place, ring := theRoom(t, theWeek)
	got := ask(t, place, `{"who":"Amma","confirmed":true}`)
	if got.Outcome == conversation.OutcomeFailed {
		t.Fatalf("it would not ring: %s", got.Content)
	}
	if len(ring.calls) != 1 || ring.calls[0].Who != "Amma" {
		t.Fatalf("it rang %+v", ring.calls)
	}
}

// How anybody is actually asked for.
func TestAFirstNameIsTheWholeName(t *testing.T) {
	_, place, ring := theRoom(t, theWeek)
	if got := ask(t, place, `{"who":"Alekhya","confirmed":true}`); got.Outcome == conversation.OutcomeFailed {
		t.Fatalf("a first name was refused: %s", got.Content)
	}
	if len(ring.calls) != 1 || ring.calls[0].Who != "Alekhya Chintada" {
		t.Fatalf("it rang %+v", ring.calls)
	}
}

// Heard rather than read. Kliffy has arrived as Cliffy and as
// Clifford, and a confirmed call on a name that was only near is
// still put to them.
func TestANameThatIsOnlyNearIsPutToThemFirst(t *testing.T) {
	_, place, ring := theRoom(t, theWeek)
	got := ask(t, place, `{"who":"Cliffy","confirmed":true}`)
	if got.Outcome != conversation.OutcomeFailed {
		t.Errorf("a misheard name was dialled on its own say-so: %s", got.Content)
	}
	if len(ring.calls) != 0 {
		t.Fatalf("it dialled %+v", ring.calls)
	}
	if !strings.Contains(got.Content, "Kliffy") {
		t.Errorf("it did not offer the name it found:\n%s", got.Content)
	}
}

// Nothing here ever concludes that somebody does not exist: the
// phone's contacts are larger than the list of people called from it.
func TestSomebodyNeverCalledBeforeCanStillBeRung(t *testing.T) {
	_, place, ring := theRoom(t, theWeek)

	first := ask(t, place, `{"who":"Gunalan"}`)
	if first.Outcome != conversation.OutcomeFailed {
		t.Errorf("an unknown name was dialled unasked: %s", first.Content)
	}
	if !strings.Contains(first.Content, "Do not tell them there is no such person") {
		t.Errorf("it did not stop the model saying the person does not exist:\n%s", first.Content)
	}
	if !strings.Contains(first.Content, "Amma") {
		t.Errorf("it did not offer the names it does know:\n%s", first.Content)
	}

	if got := ask(t, place, `{"who":"Gunalan","confirmed":true}`); got.Outcome == conversation.OutcomeFailed {
		t.Fatalf("a confirmed call to a new contact was refused: %s", got.Content)
	}
	if len(ring.calls) != 1 || ring.calls[0].Who != "Gunalan" {
		t.Fatalf("it rang %+v", ring.calls)
	}
}

func TestANumberIsNeverRung(t *testing.T) {
	_, place, ring := theRoom(t, theWeek)
	for _, args := range []string{
		`{"who":"+916746580844","confirmed":true}`,
		`{"who":"9876543210","confirmed":true}`,
	} {
		if got := ask(t, place, args); got.Outcome != conversation.OutcomeFailed {
			t.Errorf("%s was dialled", args)
		}
	}
	if len(ring.calls) != 0 {
		t.Fatalf("it dialled %+v", ring.calls)
	}
}

func TestNobodyNamedIsAskedAboutRatherThanRefusedFlatly(t *testing.T) {
	_, place, ring := theRoom(t, theWeek)
	got := ask(t, place, `{"who":"   ","confirmed":true}`)
	if got.Outcome != conversation.OutcomeFailed || len(ring.calls) != 0 {
		t.Errorf("an empty name got as far as %+v: %s", ring.calls, got.Content)
	}
}

// With no calls on record at all there is still somebody to ring.
func TestWithNoCallsOnRecordANameStillWorks(t *testing.T) {
	_, place, ring := theRoom(t, nil)
	if got := ask(t, place, `{"who":"Amma","confirmed":true}`); got.Outcome == conversation.OutcomeFailed {
		t.Fatalf("with an empty history nobody could be rung: %s", got.Content)
	}
	if len(ring.calls) != 1 {
		t.Fatalf("it rang %+v", ring.calls)
	}
}
