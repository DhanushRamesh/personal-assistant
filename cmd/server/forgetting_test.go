package main

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"personal-assistant/internal/base/thinking/environment"
	"personal-assistant/internal/domain/usage"
	usagememory "personal-assistant/internal/domain/usage/inmemory"
)

// The daily pass keeps ninety days of model calls and drops what is
// older -- the owner wants a month against the month before, and the
// table is not to grow for ever.
func TestTheDailyPassForgetsOldModelCalls(t *testing.T) {
	if usage.Keep != 90*24*time.Hour {
		t.Fatalf("model calls are kept %v, want ninety days", usage.Keep)
	}
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	s := usagememory.New()
	for _, age := range []time.Duration{91 * 24 * time.Hour, 89 * 24 * time.Hour} {
		if err := s.Add(context.Background(), usage.Call{ID: usage.NewID(), Call: environment.Call{
			Label: environment.Label{UserID: "usr_A", Feature: environment.FeatureChat},
			At:    now.Add(-age), Ended: environment.EndedAnswered,
		}}); err != nil {
			t.Fatal(err)
		}
	}

	forgetOlder(context.Background(), s, usage.Keep, "model calls",
		func() time.Time { return now }, slog.New(slog.NewTextHandler(io.Discard, nil)))

	left, _ := s.List(context.Background(), usage.Query{UserID: "usr_A", Since: now.Add(-365 * 24 * time.Hour)})
	if len(left) != 1 || !left[0].At.Equal(now.Add(-89*24*time.Hour)) {
		t.Errorf("left %+v, want only the call from 89 days ago", left)
	}
}
