package events_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DhanushRamesh/personal-assistant/internal/api/apitest"
	"github.com/DhanushRamesh/personal-assistant/internal/api/events"
	"github.com/DhanushRamesh/personal-assistant/internal/event/inmemory"
)

func post(t *testing.T, e *apitest.Env, body string) (int, events.BatchResponse) {
	t.Helper()
	rec := e.Do(t, http.MethodPost, "/v1/events", body)
	var out events.BatchResponse
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("the reply could not be read: %v (%s)", err, rec.Body.String())
		}
	}
	return rec.Code, out
}

func TestABatchIsStoredAndReportedBack(t *testing.T) {
	store := inmemory.New()
	e := apitest.NewWith(t, apitest.Options{DeviceEvents: store})

	code, out := post(t, e, `{"source":"tasker","device":"pixel-7","events":[
		{"kind":"location.arrived","occurred_at":"2026-09-30T09:00:00Z",
		 "payload":{"place":"home"},"dedupe_key":"a"},
		{"kind":"battery.low","occurred_at":"2026-09-30T09:05:00Z","dedupe_key":"b"}]}`)

	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if len(out.Stored) != 2 || len(out.Seen) != 0 || len(out.Rejected) != 0 {
		t.Fatalf("stored %v, seen %v, rejected %v", out.Stored, out.Seen, out.Rejected)
	}
	if got := len(store.All(e.User.ID)); got != 2 {
		t.Fatalf("%d events stored, want 2", got)
	}
}

// The phone resends when it is not sure the first attempt arrived. A
// duplicate must be harmless and must be reported as deletable, not as an
// error the phone keeps retrying forever.
func TestAResentBatchStoresNothingTwice(t *testing.T) {
	store := inmemory.New()
	e := apitest.NewWith(t, apitest.Options{DeviceEvents: store})
	body := `{"source":"tasker","events":[
		{"kind":"location.arrived","occurred_at":"2026-09-30T09:00:00Z","dedupe_key":"a"}]}`

	post(t, e, body)
	_, out := post(t, e, body)

	if len(out.Seen) != 1 || len(out.Stored) != 0 {
		t.Fatalf("stored %v, seen %v", out.Stored, out.Seen)
	}
	if got := len(store.All(e.User.ID)); got != 1 {
		t.Fatalf("%d events stored, want 1", got)
	}
}

// A spool flushed twice without being truncated carries the same key twice
// in one request.
func TestADuplicateInsideOneBatchIsStoredOnce(t *testing.T) {
	store := inmemory.New()
	e := apitest.NewWith(t, apitest.Options{DeviceEvents: store})

	_, out := post(t, e, `{"source":"tasker","events":[
		{"kind":"a.b","occurred_at":"2026-09-30T09:00:00Z","dedupe_key":"same"},
		{"kind":"a.b","occurred_at":"2026-09-30T09:00:00Z","dedupe_key":"same"}]}`)

	if len(out.Stored) != 1 || len(out.Seen) != 1 {
		t.Fatalf("stored %v, seen %v", out.Stored, out.Seen)
	}
}

// One broken profile on the phone must not stop every other event it has
// buffered. The failure would be total, permanent, and nowhere near its
// cause.
func TestABadEventDoesNotFailTheBatch(t *testing.T) {
	store := inmemory.New()
	e := apitest.NewWith(t, apitest.Options{DeviceEvents: store})

	_, out := post(t, e, `{"source":"tasker","events":[
		{"kind":"Not A Kind","occurred_at":"2026-09-30T09:00:00Z","dedupe_key":"bad"},
		{"kind":"location.arrived","occurred_at":"2026-09-30T09:00:00Z","dedupe_key":"good"}]}`)

	if len(out.Stored) != 1 || out.Stored[0] != "good" {
		t.Fatalf("stored %v, want only the good one", out.Stored)
	}
	if len(out.Rejected) != 1 || out.Rejected[0].DedupeKey != "bad" {
		t.Fatalf("rejected %v, want the bad one named", out.Rejected)
	}
	if out.Rejected[0].Why == "" {
		t.Fatal("a rejection did not say why")
	}
}

func TestAnEmptyBatchIsAccepted(t *testing.T) {
	e := apitest.NewWith(t, apitest.Options{DeviceEvents: inmemory.New()})
	if code, _ := post(t, e, `{"source":"tasker","events":[]}`); code != http.StatusOK {
		t.Fatalf("status %d, want 200", code)
	}
}

func TestEventsNeedAToken(t *testing.T) {
	e := apitest.NewWith(t, apitest.Options{DeviceEvents: inmemory.New()})
	req := httptest.NewRequest(http.MethodPost, "/v1/events",
		strings.NewReader(`{"source":"tasker","events":[]}`))
	req.Header.Set("Content-Type", "application/json")
	if rec := e.Serve(req); rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", rec.Code)
	}
}

func listing(t *testing.T, e *apitest.Env, query string) events.ListResponse {
	t.Helper()
	rec := e.Do(t, http.MethodGet, "/v1/events"+query, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var out events.ListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("reply unreadable: %v", err)
	}
	return out
}

func three(t *testing.T, e *apitest.Env) {
	t.Helper()
	post(t, e, `{"source":"tasker","device":"pixel-7","events":[
		{"kind":"network.joined","occurred_at":"2026-09-30T08:00:00Z","dedupe_key":"a"},
		{"kind":"network.left","occurred_at":"2026-09-30T09:00:00Z","dedupe_key":"b"},
		{"kind":"network.joined","occurred_at":"2026-09-30T10:00:00Z","dedupe_key":"c"}]}`)
}

// Newest first by when it HAPPENED, not when it arrived. A phone that was
// offline all morning delivers the morning at teatime, and ordering by
// arrival would scatter a day through the list.
func TestEventsAreListedNewestFirstByWhenTheyHappened(t *testing.T) {
	e := apitest.NewWith(t, apitest.Options{DeviceEvents: inmemory.New()})
	three(t, e)

	got := listing(t, e, "")
	if len(got.Events) != 3 {
		t.Fatalf("got %d events, want 3", len(got.Events))
	}
	for i := 1; i < len(got.Events); i++ {
		if got.Events[i-1].OccurredAt.Before(got.Events[i].OccurredAt) {
			t.Fatalf("out of order at %d", i)
		}
	}
}

func TestAListingCanBeFilteredByKind(t *testing.T) {
	e := apitest.NewWith(t, apitest.Options{DeviceEvents: inmemory.New()})
	three(t, e)

	got := listing(t, e, "?kind=network.left")
	if len(got.Events) != 1 || got.Events[0].Kind != "network.left" {
		t.Fatalf("filtering gave %d events: %+v", len(got.Events), got.Events)
	}
}

// The kinds are counted across everything, not across the page: a filtered
// listing still has to say what else there is, or the filter has no way
// back.
func TestKindsAreCountedAcrossEverythingNotThePage(t *testing.T) {
	e := apitest.NewWith(t, apitest.Options{DeviceEvents: inmemory.New()})
	three(t, e)

	got := listing(t, e, "?kind=network.left")
	counts := map[string]int64{}
	for _, k := range got.Kinds {
		counts[k.Kind] = k.Count
	}
	if counts["network.joined"] != 2 || counts["network.left"] != 1 {
		t.Fatalf("counts = %v, want joined 2 and left 1", counts)
	}
}

// The one number that says the phone had been offline, worked out here so
// a screen does not have to.
func TestHowLateAnEventWasIsReported(t *testing.T) {
	e := apitest.NewWith(t, apitest.Options{DeviceEvents: inmemory.New()})
	post(t, e, `{"source":"tasker","events":[
		{"kind":"a.b","occurred_at":"2026-09-30T08:00:00Z","dedupe_key":"late"}]}`)

	got := listing(t, e, "")
	if len(got.Events) != 1 {
		t.Fatalf("got %d events", len(got.Events))
	}
	if got.Events[0].LateBy <= 0 {
		t.Fatalf("late_by = %d, want the gap to arrival", got.Events[0].LateBy)
	}
}

func TestABadLimitIsRefused(t *testing.T) {
	e := apitest.NewWith(t, apitest.Options{DeviceEvents: inmemory.New()})
	if rec := e.Do(t, http.MethodGet, "/v1/events?limit=nonsense", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", rec.Code)
	}
}
