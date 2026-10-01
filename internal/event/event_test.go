package event

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func good(t *testing.T) *Event {
	t.Helper()
	e, err := New("usr_1", "tasker", "pixel-7", "location.arrived",
		at("2026-10-01T09:00:00Z"), at("2026-10-01T09:00:01Z"),
		json.RawMessage(`{"place":"home"}`), "tasker-4821")
	if err != nil {
		t.Fatalf("a plain event was refused: %v", err)
	}
	return e
}

func TestAPlainEventIsAccepted(t *testing.T) {
	e := good(t)
	if !ValidID(e.ID) {
		t.Fatalf("identifier %q is not an event identifier", e.ID)
	}
	if e.OccurredAt.Location() != time.UTC || e.ReceivedAt.Location() != time.UTC {
		t.Fatal("times were not stored as UTC")
	}
}

// The phone spends hours unable to reach anything and then sends the lot.
// An event is not refused for being old; that is the normal case.
func TestAnEventFromYesterdayIsAccepted(t *testing.T) {
	_, err := New("usr_1", "tasker", "", "location.left",
		at("2026-09-30T06:00:00Z"), at("2026-10-01T09:00:00Z"),
		json.RawMessage(`{}`), "k1")
	if err != nil {
		t.Fatalf("a buffered event was refused: %v", err)
	}
}

// A mis-set clock would otherwise write rows that sort above everything
// real and stay at the top of "what happened today" forever.
func TestAnEventFromTheFutureIsRefused(t *testing.T) {
	_, err := New("usr_1", "tasker", "", "battery.low",
		at("2026-10-01T10:00:00Z"), at("2026-10-01T09:00:00Z"),
		json.RawMessage(`{}`), "k1")
	if !errors.Is(err, ErrFromTheFuture) {
		t.Fatalf("got %v, want ErrFromTheFuture", err)
	}
}

// A few seconds of skew between two clocks must not refuse a real event.
func TestASmallClockSkewIsTolerated(t *testing.T) {
	_, err := New("usr_1", "tasker", "", "battery.low",
		at("2026-10-01T09:00:30Z"), at("2026-10-01T09:00:00Z"),
		json.RawMessage(`{}`), "k1")
	if err != nil {
		t.Fatalf("thirty seconds of skew was refused: %v", err)
	}
}

func TestKindsAreNormalisedAndChecked(t *testing.T) {
	ok := []string{"location.arrived", "call.missed", "app.opened",
		"a", "train.ticket_scanned", "x.y.z", "battery.level_5"}
	for _, k := range ok {
		if !dotted(k) {
			t.Errorf("%q should be a valid kind", k)
		}
	}
	bad := []string{"", ".leading", "trailing.", "two..dots",
		"Has.Capitals", "has spaces", "has-hyphen", "has/slash"}
	for _, k := range bad {
		if dotted(k) {
			t.Errorf("%q should not be a valid kind", k)
		}
	}
}

// "Location Arrived" and "location.arrived" must not become two things
// nobody notices for a year.
func TestCaseAndSpaceAreFoldedBeforeChecking(t *testing.T) {
	e, err := New("usr_1", "tasker", "", "  Location.Arrived  ",
		at("2026-10-01T09:00:00Z"), at("2026-10-01T09:00:00Z"), nil, "k1")
	if err != nil {
		t.Fatalf("a kind needing tidying was refused: %v", err)
	}
	if e.Kind != "location.arrived" {
		t.Fatalf("kind = %q, want location.arrived", e.Kind)
	}
}

// Plenty of events are the whole story by their name alone.
func TestAnAbsentPayloadBecomesAnEmptyObject(t *testing.T) {
	e, err := New("usr_1", "tasker", "", "screen.on",
		at("2026-10-01T09:00:00Z"), at("2026-10-01T09:00:00Z"), nil, "k1")
	if err != nil {
		t.Fatalf("an event with no payload was refused: %v", err)
	}
	if string(e.Payload) != "{}" {
		t.Fatalf("payload = %q, want {}", e.Payload)
	}
}

// An object, so a kind can grow a field later without what is already
// stored changing shape.
func TestAPayloadThatIsNotAnObjectIsRefused(t *testing.T) {
	for _, p := range []string{`[1,2]`, `"text"`, `42`, `{broken`} {
		_, err := New("usr_1", "tasker", "", "a.b",
			at("2026-10-01T09:00:00Z"), at("2026-10-01T09:00:00Z"),
			json.RawMessage(p), "k1")
		if !errors.Is(err, ErrPayloadNotObject) {
			t.Errorf("payload %s: got %v, want ErrPayloadNotObject", p, err)
		}
	}
}

func TestTheEnvelopeIsRequired(t *testing.T) {
	now := at("2026-10-01T09:00:00Z")
	cases := map[string]struct {
		user, source, kind, dedupe string
		want                       error
	}{
		"no user":   {"", "tasker", "a.b", "k", ErrNoUser},
		"no source": {"usr_1", "", "a.b", "k", ErrNoSource},
		"no kind":   {"usr_1", "tasker", "", "k", ErrNoKind},
		"no dedupe": {"usr_1", "tasker", "a.b", "", ErrNoDedupeKey},
	}
	for name, c := range cases {
		_, err := New(c.user, c.source, "", c.kind, now, now, nil, c.dedupe)
		if !errors.Is(err, c.want) {
			t.Errorf("%s: got %v, want %v", name, err, c.want)
		}
	}
}

// One bad profile must not be able to post a photograph four hundred
// times.
func TestAnOversizedPayloadIsRefused(t *testing.T) {
	big := `{"a":"` + strings.Repeat("x", MaxPayload) + `"}`
	_, err := New("usr_1", "tasker", "", "a.b",
		at("2026-10-01T09:00:00Z"), at("2026-10-01T09:00:00Z"),
		json.RawMessage(big), "k1")
	if !errors.Is(err, ErrTooLong) {
		t.Fatalf("got %v, want ErrTooLong", err)
	}
}
