package presence_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/event"
	"github.com/DhanushRamesh/personal-assistant/internal/presence"
)

func moment(h, m int) time.Time { return time.Date(2026, 10, 2, h, m, 0, 0, time.UTC) }

func said(kind, value string, at time.Time) event.Event {
	payload, _ := json.Marshal(map[string]string{"value": value})
	return event.Event{Kind: kind, Source: presence.Phone, Payload: payload, OccurredAt: at}
}

func fix(at time.Time) event.Event {
	payload, _ := json.Marshal(map[string]string{"value": "12.91100,80.06242"})
	return event.Event{Kind: event.Fixed, Source: presence.Phone, Payload: payload, OccurredAt: at}
}

// Inside the geofence they drew, with the phone still reporting.
func TestInsideTheirOwnGeofenceIsPresent(t *testing.T) {
	got := presence.Here([]event.Event{
		said(event.Entered, "home", moment(9, 0)),
		fix(moment(11, 58)),
	}, "home", moment(12, 0))

	if got.Where != presence.Present {
		t.Errorf("where = %q (%s), want present", got.Where, got.Why)
	}
}

// Having left is the one thing that holds a reminder back.
func TestHavingLeftIsAway(t *testing.T) {
	got := presence.Here([]event.Event{
		said(event.Entered, "home", moment(9, 0)),
		said(event.Exited, "home", moment(10, 22)),
		fix(moment(11, 58)),
	}, "home", moment(12, 0))

	if got.Where != presence.Away {
		t.Errorf("where = %q (%s), want away", got.Where, got.Why)
	}
}

// A phone that has stopped reporting cannot say where anybody is. The
// geofences only fire on change, so the five-minute readings are the
// only heartbeat there is.
func TestAQuietPhoneCannotSayWhereAnybodyIs(t *testing.T) {
	got := presence.Here([]event.Event{
		said(event.Entered, "home", moment(9, 0)),
		fix(moment(9, 5)),
	}, "home", moment(12, 0))

	if got.Where != presence.Unknown {
		t.Errorf("where = %q, want unknown: the phone went quiet three hours ago", got.Where)
	}
	if !got.Stale {
		t.Error("the silence was not reported")
	}
}

// Android drops geofence departures. One missed must not keep somebody
// at home for days.
func TestAnArrivalNobodyEverLeftGoesUnknown(t *testing.T) {
	got := presence.Here([]event.Event{
		said(event.Entered, "home", moment(9, 0).AddDate(0, 0, -1)),
		fix(moment(11, 58)),
	}, "home", moment(12, 0))

	if got.Where == presence.Present {
		t.Errorf("a day-old arrival with no departure still reads as present: %s", got.Why)
	}
}

// Somewhere nobody has ever crossed cannot be answered about, and that
// is not an absence.
func TestAnUnknownPlaceIsNotAnAbsence(t *testing.T) {
	got := presence.Here([]event.Event{fix(moment(11, 58))}, "office", moment(12, 0))

	if got.Where != presence.Unknown {
		t.Errorf("where = %q, want unknown", got.Where)
	}
}

// Nothing configured answers nothing, rather than guessing.
func TestWithNowhereConfiguredItSaysSo(t *testing.T) {
	if got := presence.Here([]event.Event{fix(moment(11, 58))}, "", moment(12, 0)); got.Where != presence.Unknown {
		t.Errorf("where = %q, want unknown", got.Where)
	}
}
