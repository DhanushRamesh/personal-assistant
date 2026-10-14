package rules_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"personal-assistant/internal/app/api/apitest"
	"personal-assistant/internal/app/api/rules"
	"personal-assistant/internal/domain/job"
)

// held : A rules store where whoever asks has job_mine, and job_theirs
// is somebody else's.
type held struct{}

func (held) Mine(_ context.Context, userID, _ string, _ int, _ ...job.State) ([]job.Job, error) {
	return []job.Job{{ID: "job_mine", UserID: userID}}, nil
}

// journal : A run journal holding the runs given, by rule.
type journal struct {
	runs  map[string][]job.Record
	since time.Time
}

func (j *journal) Of(_ context.Context, jobID string, since time.Time, _ int) ([]job.Record, error) {
	j.since = since
	return j.runs[jobID], nil
}

// A rule's history is every run the server keeps, newest first, each
// with what it came to and what each step did -- and only for its owner.
func TestARuleTellsItsHistory(t *testing.T) {
	ran := &journal{runs: map[string][]job.Record{}}
	e := apitest.NewWith(t, apitest.Options{Ran: ran, Rules: held{}})
	at := time.Date(2026, 10, 13, 7, 42, 0, 0, time.UTC)
	ran.runs["job_mine"] = []job.Record{
		{At: at, Took: 2400 * time.Millisecond, Said: "Good morning, sir.", Steps: []job.Tried{{Do: job.Call, Use: "event_recent", Came: job.Went, Took: 120, Why: "locked at 12:11 am"}}},
		{At: at.Add(-time.Hour), Stopped: true, Why: "hour >= 8 did not hold"},
		{At: at.Add(-2 * time.Hour), Failed: true, Why: "the database did not answer"},
		{At: at.Add(-3 * time.Hour)},
	}
	ran.runs["job_theirs"] = []job.Record{{At: at, Said: "not yours"}}

	rec := e.Do(t, http.MethodGet, "/v1/rules/job_mine/runs", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var got rules.RunsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	outcomes := []string{}
	for _, r := range got.Runs {
		outcomes = append(outcomes, r.Outcome)
	}
	if want := []string{"said", "declined", "failed", "held"}; len(outcomes) != 4 || outcomes[0] != want[0] || outcomes[1] != want[1] || outcomes[2] != want[2] || outcomes[3] != want[3] {
		t.Errorf("outcomes = %v, want %v", outcomes, want)
	}
	first := got.Runs[0]
	if first.Said != "Good morning, sir." || first.TookMS != 2400 || len(first.Steps) != 1 || first.Steps[0].Use != "event_recent" || first.Steps[0].Why != "locked at 12:11 am" {
		t.Errorf("first run = %+v", first)
	}
	if time.Since(ran.since) < 29*24*time.Hour {
		t.Errorf("reached back only to %v, not the month the server keeps", ran.since)
	}

	if rec := e.Do(t, http.MethodGet, "/v1/rules/job_theirs/runs", ""); rec.Code != http.StatusNotFound {
		t.Errorf("somebody else's rule answered %d, want 404", rec.Code)
	}
	if rec := e.Do(t, http.MethodGet, "/v1/rules/job_mine/runs?limit=x", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("an unreadable limit answered %d, want 400", rec.Code)
	}
}
