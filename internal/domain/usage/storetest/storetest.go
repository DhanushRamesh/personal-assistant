// Package storetest runs one set of cases against any usage.Store.
//
// The in-memory store stands in for MySQL in every API test, so the
// two must add up alike: a window's edge counted on one side and not
// the other is exactly the drift nobody sees until the numbers on the
// settings page disagree with the log.
package storetest

import (
	"context"
	"testing"
	"time"

	"personal-assistant/internal/base/thinking/environment"
	"personal-assistant/internal/domain/usage"
)

// New : Opens a store and returns it with two users to own calls.
type New func(t *testing.T) (s usage.Store, user, other string)

// Run : Every case both stores must pass.
func Run(t *testing.T, open New) {
	t.Helper()
	for _, c := range []struct {
		name string
		run  func(t *testing.T, open New)
	}{
		{"a call kept comes back whole", aCallComesBackWhole},
		{"totals count only the window and only theirs", totalsKeepToTheWindow},
		{"nothing kept adds up to zero", nothingIsZero},
		{"calls are grouped by feature, the most input first", groupedByFeature},
		{"calls are grouped by model", groupedByModel},
		{"a listing is newest first and filtered by kind", listedByKind},
		{"a listing is limited", listingIsLimited},
		{"old calls are forgotten and newer ones kept", oldOnesAreForgotten},
	} {
		t.Run(c.name, func(t *testing.T) { c.run(t, open) })
	}
}

var at = time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)

// call : One call by user from feature, minutes after at.
func call(user string, f environment.Feature, minutes, in, out int, ended environment.Ended) usage.Call {
	return usage.Call{ID: usage.NewID(), Call: environment.Call{
		Label:  environment.Label{UserID: user, Feature: f},
		Vendor: "anthropic", Model: "claude-sonnet-4-6",
		Input: in, Output: out,
		At:    at.Add(time.Duration(minutes) * time.Minute),
		Took:  1500 * time.Millisecond,
		Ended: ended,
	}}
}

func add(t *testing.T, s usage.Store, calls ...usage.Call) {
	t.Helper()
	for _, c := range calls {
		if err := s.Add(context.Background(), c); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}
}

func aCallComesBackWhole(t *testing.T, open New) {
	s, user, _ := open(t)
	c := call(user, environment.FeatureRuleRemark, 0, 1200, 34, environment.EndedAnswered)
	c.Detail = "anything amiss"
	c.ChatID = "chat_01JABCDEFGHJKMNPQRSTVWXYZ0"
	c.ConversationID = "conv_01JABCDEFGHJKMNPQRSTVWXYZ0"
	add(t, s, c)

	got, err := s.List(context.Background(), usage.Query{UserID: user, Since: at})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d calls, want 1", len(got))
	}
	g := got[0]
	if g.ID != c.ID || g.Feature != c.Feature || g.Detail != c.Detail ||
		g.ChatID != c.ChatID || g.ConversationID != c.ConversationID ||
		g.Vendor != c.Vendor || g.Model != c.Model || g.Input != 1200 || g.Output != 34 ||
		g.Took != c.Took || g.Ended != c.Ended || !g.At.Equal(c.At) || g.UserID != user {
		t.Errorf("came back as %+v, want %+v", g, c)
	}
}

func totalsKeepToTheWindow(t *testing.T, open New) {
	ctx := context.Background()
	s, user, other := open(t)
	add(t, s,
		// Before the window: not counted.
		call(user, environment.FeatureChat, -1, 9999, 9999, environment.EndedAnswered),
		// On its edge: counted, the window being inclusive.
		call(user, environment.FeatureChat, 0, 100, 10, environment.EndedTools),
		call(user, environment.FeatureTitle, 1, 20, 2, environment.EndedAnswered),
		call(user, environment.FeatureProfile, 2, 300, 30, environment.EndedFailed),
		call(user, environment.FeatureRuleRemark, 3, 40, 4, environment.EndedAnswered),
		// Somebody else's: never counted.
		call(other, environment.FeatureChat, 1, 5000, 500, environment.EndedAnswered),
	)
	got, err := s.Totals(ctx, user, at)
	if err != nil {
		t.Fatalf("Totals: %v", err)
	}
	want := usage.Totals{Calls: 4, Input: 460, Output: 46, Chat: 2, Background: 2, Failed: 1}
	if got != want {
		t.Errorf("totals = %+v, want %+v", got, want)
	}
}

func nothingIsZero(t *testing.T, open New) {
	s, user, _ := open(t)
	got, err := s.Totals(context.Background(), user, at)
	if err != nil {
		t.Fatalf("Totals: %v", err)
	}
	if got != (usage.Totals{}) {
		t.Errorf("totals = %+v, want zero", got)
	}
	features, err := s.ByFeature(context.Background(), user, at)
	if err != nil || len(features) != 0 {
		t.Errorf("by feature = %+v, %v; want none", features, err)
	}
}

func groupedByFeature(t *testing.T, open New) {
	s, user, _ := open(t)
	add(t, s,
		call(user, environment.FeatureChat, 0, 100, 1, environment.EndedAnswered),
		call(user, environment.FeatureChat, 1, 150, 2, environment.EndedTools),
		call(user, environment.FeatureProfile, 2, 900, 3, environment.EndedAnswered),
		call(user, environment.FeatureGreeting, 3, 10, 4, environment.EndedAnswered),
	)
	got, err := s.ByFeature(context.Background(), user, at)
	if err != nil {
		t.Fatalf("ByFeature: %v", err)
	}
	want := []usage.ByFeature{
		{Feature: environment.FeatureProfile, Calls: 1, Input: 900, Output: 3},
		{Feature: environment.FeatureChat, Calls: 2, Input: 250, Output: 3},
		{Feature: environment.FeatureGreeting, Calls: 1, Input: 10, Output: 4},
	}
	if len(got) != len(want) {
		t.Fatalf("by feature = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("by feature [%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func groupedByModel(t *testing.T, open New) {
	s, user, _ := open(t)
	fast := call(user, environment.FeatureGreeting, 0, 50, 5, environment.EndedAnswered)
	fast.Model = "claude-haiku-4-5-20251001"
	add(t, s,
		call(user, environment.FeatureChat, 1, 100, 1, environment.EndedAnswered),
		call(user, environment.FeatureChat, 2, 100, 1, environment.EndedAnswered),
		fast,
	)
	got, err := s.ByModel(context.Background(), user, at)
	if err != nil {
		t.Fatalf("ByModel: %v", err)
	}
	want := []usage.ByModel{
		{Vendor: "anthropic", Model: "claude-sonnet-4-6", Calls: 2, Input: 200, Output: 2},
		{Vendor: "anthropic", Model: "claude-haiku-4-5-20251001", Calls: 1, Input: 50, Output: 5},
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("by model = %+v, want %+v", got, want)
	}
}

func listedByKind(t *testing.T, open New) {
	ctx := context.Background()
	s, user, _ := open(t)
	chatCall := call(user, environment.FeatureChat, 0, 1, 1, environment.EndedAnswered)
	condensing := call(user, environment.FeatureCondense, 1, 1, 1, environment.EndedAnswered)
	noticing := call(user, environment.FeatureNoticing, 2, 1, 1, environment.EndedFailed)
	other := call(user, environment.FeatureOther, 3, 1, 1, environment.EndedStopped)
	add(t, s, chatCall, condensing, noticing, other)

	for _, c := range []struct {
		kind usage.Kind
		want []string
	}{
		{usage.KindAll, []string{other.ID, noticing.ID, condensing.ID, chatCall.ID}},
		{"", []string{other.ID, noticing.ID, condensing.ID, chatCall.ID}},
		{usage.KindChat, []string{condensing.ID, chatCall.ID}},
		{usage.KindBackground, []string{other.ID, noticing.ID}},
		{usage.KindFailed, []string{noticing.ID}},
	} {
		got, err := s.List(ctx, usage.Query{UserID: user, Since: at, Kind: c.kind})
		if err != nil {
			t.Fatalf("List %q: %v", c.kind, err)
		}
		var ids []string
		for _, g := range got {
			ids = append(ids, g.ID)
		}
		if len(ids) != len(c.want) {
			t.Errorf("kind %q listed %v, want %v", c.kind, ids, c.want)
			continue
		}
		for i := range ids {
			if ids[i] != c.want[i] {
				t.Errorf("kind %q listed %v, want %v", c.kind, ids, c.want)
				break
			}
		}
	}
}

func listingIsLimited(t *testing.T, open New) {
	s, user, _ := open(t)
	for i := 0; i < 5; i++ {
		add(t, s, call(user, environment.FeatureChat, i, 1, 1, environment.EndedAnswered))
	}
	got, err := s.List(context.Background(), usage.Query{UserID: user, Since: at, Limit: 2})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 || !got[0].At.Equal(at.Add(4*time.Minute)) {
		t.Errorf("listed %d, first at %v; want the newest two", len(got), got)
	}
}

func oldOnesAreForgotten(t *testing.T, open New) {
	ctx := context.Background()
	s, user, _ := open(t)
	// Long before anything another test keeps, since a shared database
	// is forgotten by time alone and must not take their rows.
	long := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
	old := call(user, environment.FeatureChat, 0, 1, 1, environment.EndedAnswered)
	old.At = long
	kept := call(user, environment.FeatureChat, 0, 1, 1, environment.EndedAnswered)
	kept.At = long.Add(48 * time.Hour)
	add(t, s, old, kept)

	gone, err := s.Forget(ctx, long.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if gone < 1 {
		t.Errorf("forgot %d, want the old one", gone)
	}
	got, err := s.List(ctx, usage.Query{UserID: user, Since: long.Add(-time.Hour)})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].ID != kept.ID {
		t.Errorf("left %+v, want only the newer one", got)
	}
}
