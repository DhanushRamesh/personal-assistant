package calling_test

import (
	"context"
	"encoding/json"
	"regexp"
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

// heard : What the person has said, oldest first.
type heard struct{ said []conversation.Message }

func (h *heard) SaidSince(_ context.Context, _ string, since time.Time, _ int) ([]conversation.Message, error) {
	var out []conversation.Message
	for _, m := range h.said {
		if m.At.After(since) || m.At.Equal(since) {
			out = append(out, m)
		}
	}
	return out, nil
}

// answers : The person saying something, now.
func (h *heard) answers(what string) {
	h.said = append(h.said, conversation.Message{
		Role: conversation.User, Content: what, At: now.Add(time.Second),
	})
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
func theRoom(t *testing.T, events []event.Event) (tool.Tool, tool.Tool, *rang, *heard) {
	t.Helper()
	ring := &rang{}
	listening := &heard{}
	tools := callingtool.Tools(reader{events: events}, ring, listening,
		func() time.Time { return now })
	if len(tools) != 2 {
		t.Fatalf("there are %d call tools", len(tools))
	}
	return tools[0], tools[1], ring, listening
}

// token : The word a refusal hands back, so a test can answer with it.
//
// Taken from what the model is actually told rather than from inside
// the store, because what is unreachable from the refusal is
// unreachable to the model too.
func token(t *testing.T, got tool.Result) string {
	t.Helper()
	found := regexp.MustCompile(`confirm "([0-9a-f]+)"`).FindStringSubmatch(got.Content)
	if found == nil {
		t.Fatalf("no word to confirm with in:\n%s", got.Content)
	}
	return found[1]
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
	people, _, _, _ := theRoom(t, theWeek)
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
	_, place, ring, _ := theRoom(t, theWeek)
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
	_, place, ring, listening := theRoom(t, theWeek)

	asked := ask(t, place, `{"who":"Amma"}`)
	word := token(t, asked)
	listening.answers("yes, go on")

	got := ask(t, place, `{"who":"Amma","confirm":"`+word+`"}`)
	if got.Outcome == conversation.OutcomeFailed {
		t.Fatalf("it would not ring: %s", got.Content)
	}
	if len(ring.calls) != 1 || ring.calls[0].Who != "Amma" {
		t.Fatalf("it rang %+v", ring.calls)
	}
}

// The hole this was built to close. Measured on the owner's own
// phone: the first call_place of a fresh conversation arrived with
// confirmed true, and nobody had been asked anything.
func TestItCannotAgreeWithItselfInOneBreath(t *testing.T) {
	_, place, ring, _ := theRoom(t, theWeek)

	asked := ask(t, place, `{"who":"Amma"}`)
	word := token(t, asked)

	// A real word, handed straight back, with nothing said between.
	got := ask(t, place, `{"who":"Amma","confirm":"`+word+`"}`)
	if got.Outcome != conversation.OutcomeFailed {
		t.Errorf("it confirmed its own question: %s", got.Content)
	}
	if len(ring.calls) != 0 {
		t.Fatalf("it dialled %+v", ring.calls)
	}
}

func TestAMadeUpWordDialsNothing(t *testing.T) {
	_, place, ring, listening := theRoom(t, theWeek)
	ask(t, place, `{"who":"Amma"}`)
	listening.answers("yes")

	if got := ask(t, place, `{"who":"Amma","confirm":"deadbeef"}`); got.Outcome != conversation.OutcomeFailed {
		t.Errorf("an invented word was taken for agreement: %s", got.Content)
	}
	if len(ring.calls) != 0 {
		t.Fatalf("it dialled %+v", ring.calls)
	}
}

// Nothing to answer, so there is nothing to agree to.
func TestAWordWithNoQuestionBehindItDialsNothing(t *testing.T) {
	_, place, ring, listening := theRoom(t, theWeek)
	listening.answers("yes")

	if got := ask(t, place, `{"who":"Amma","confirm":"3f9a1c20b4d8"}`); got.Outcome != conversation.OutcomeFailed {
		t.Errorf("it rang on a word nobody issued: %s", got.Content)
	}
	if len(ring.calls) != 0 {
		t.Fatalf("it dialled %+v", ring.calls)
	}
}

// Agreeing to one person never rings another.
func TestAnAnsweredQuestionRingsOnlyWhoItWasAbout(t *testing.T) {
	_, place, ring, listening := theRoom(t, theWeek)
	asked := ask(t, place, `{"who":"Amma"}`)
	word := token(t, asked)
	listening.answers("yes")

	got := ask(t, place, `{"who":"Kliffy","confirm":"`+word+`"}`)
	if got.Outcome != conversation.OutcomeFailed {
		t.Errorf("it swapped the name after they agreed: %s", got.Content)
	}
	if len(ring.calls) != 0 {
		t.Fatalf("it dialled %+v", ring.calls)
	}
}

// A word is good once.
func TestAgreementIsNotReusable(t *testing.T) {
	_, place, ring, listening := theRoom(t, theWeek)
	asked := ask(t, place, `{"who":"Amma"}`)
	word := token(t, asked)
	listening.answers("yes")

	if got := ask(t, place, `{"who":"Amma","confirm":"`+word+`"}`); got.Outcome == conversation.OutcomeFailed {
		t.Fatalf("the first call was refused: %s", got.Content)
	}
	if got := ask(t, place, `{"who":"Amma","confirm":"`+word+`"}`); got.Outcome != conversation.OutcomeFailed {
		t.Errorf("the same agreement rang twice: %s", got.Content)
	}
	if len(ring.calls) != 1 {
		t.Fatalf("it dialled %+v", ring.calls)
	}
}

// How anybody is actually asked for.
func TestAFirstNameIsTheWholeName(t *testing.T) {
	_, place, ring, listening := theRoom(t, theWeek)
	asked := ask(t, place, `{"who":"Alekhya"}`)
	if !strings.Contains(asked.Content, "Alekhya Chintada") {
		t.Fatalf("a first name did not reach the whole name:\n%s", asked.Content)
	}
	word := token(t, asked)
	listening.answers("yes")

	// Answered with the words they used, not the name it resolved.
	if got := ask(t, place, `{"who":"Alekhya","confirm":"`+word+`"}`); got.Outcome == conversation.OutcomeFailed {
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
	_, place, ring, _ := theRoom(t, theWeek)
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
	_, place, ring, listening := theRoom(t, theWeek)

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

	word := token(t, first)
	listening.answers("yes, that one")
	if got := ask(t, place, `{"who":"Gunalan","confirm":"`+word+`"}`); got.Outcome == conversation.OutcomeFailed {
		t.Fatalf("an agreed call to a new contact was refused: %s", got.Content)
	}
	if len(ring.calls) != 1 || ring.calls[0].Who != "Gunalan" {
		t.Fatalf("it rang %+v", ring.calls)
	}
}

func TestANumberIsNeverRung(t *testing.T) {
	_, place, ring, _ := theRoom(t, theWeek)
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
	_, place, ring, _ := theRoom(t, theWeek)
	got := ask(t, place, `{"who":"   ","confirmed":true}`)
	if got.Outcome != conversation.OutcomeFailed || len(ring.calls) != 0 {
		t.Errorf("an empty name got as far as %+v: %s", ring.calls, got.Content)
	}
}

// With no calls on record at all there is still somebody to ring.
func TestWithNoCallsOnRecordANameStillWorks(t *testing.T) {
	_, place, ring, listening := theRoom(t, nil)
	word := token(t, ask(t, place, `{"who":"Amma"}`))
	listening.answers("yes")
	if got := ask(t, place, `{"who":"Amma","confirm":"`+word+`"}`); got.Outcome == conversation.OutcomeFailed {
		t.Fatalf("with an empty history nobody could be rung: %s", got.Content)
	}
	if len(ring.calls) != 1 {
		t.Fatalf("it rang %+v", ring.calls)
	}
}
