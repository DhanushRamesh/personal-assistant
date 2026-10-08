package platformai

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

// TestBeingToldToSlowDownIsNotBeingToldNo : The gateway reports
// rate limiting as a plain 400 with the reason in words, so nothing
// caught it and the person heard "I am having some difficulty
// reaching the server" for a service that was working and busy.
func TestBeingToldToSlowDownIsNotBeingToldNo(t *testing.T) {
	limit := &APIError{
		Message: "You have made too many requests continuously. Please try again after some time.",
		Status:  http.StatusBadRequest,
	}
	if !limited(limit.Status, limit) {
		t.Error("the gateway's own words were not read as rate limiting")
	}
	if !limited(http.StatusTooManyRequests, errors.New("whatever")) {
		t.Error("a 429 was not read as rate limiting")
	}
}

// TestAMalformedRequestIsNotRetried : A 400 that means the asking
// was wrong will be wrong again, and retrying it for ever is exactly
// what must not happen.
func TestAMalformedRequestIsNotRetried(t *testing.T) {
	bad := &APIError{Message: "model is required", Status: http.StatusBadRequest}
	if limited(bad.Status, bad) {
		t.Error("a malformed request was taken for rate limiting")
	}
	if limited(http.StatusInternalServerError, errors.New("boom")) {
		t.Error("an ordinary failure was taken for rate limiting")
	}
}

// TestTheWaitEndsWithTheTurn : A turn has thirty seconds before the
// caller cuts it. Sleeping past that spends the budget to arrive
// after nobody is listening.
func TestTheWaitEndsWithTheTurn(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if sleep(ctx, time.Hour) {
		t.Error("it waited on a turn that was already over")
	}
	if !sleep(context.Background(), time.Millisecond) {
		t.Error("an ordinary wait did not finish")
	}
}

// TestItGivesUpRatherThanHammering : Two attempts, because a third
// wait would outlast the turn.
func TestItGivesUpRatherThanHammering(t *testing.T) {
	if len(Slowed) != 2 {
		t.Errorf("waits are %v, want two of them", Slowed)
	}
	var total time.Duration
	for i, d := range Slowed {
		if i > 0 && d <= Slowed[i-1] {
			t.Errorf("the waits do not grow: %v", Slowed)
		}
		total += d
	}
	if total > 10*time.Second {
		t.Errorf("the waits total %s, which is most of a turn", total)
	}
}
