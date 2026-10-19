package usage_test

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"testing"
	"time"

	"personal-assistant/internal/app/api/apitest"
	usageapi "personal-assistant/internal/app/api/usage"
	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/base/thinking/environment"
	"personal-assistant/internal/domain/usage"
	"personal-assistant/internal/domain/usage/inmemory"
)

// now : The server's clock in these tests, mid-afternoon so that
// "today" has room either side of it.
var now = time.Date(2026, 10, 17, 15, 0, 0, 0, time.UTC)

// setUp : A server whose store holds calls by the signed-in person and
// by somebody else, and the conversation one of them belongs to.
func setUp(t *testing.T) (*apitest.Env, string) {
	t.Helper()
	s := inmemory.New()
	e := apitest.NewWith(t, apitest.Options{Usage: s, Now: func() time.Time { return now }})
	me := e.User.ID

	talk := chat.NewConversation(me, "The cinema booking")
	if err := e.Repo.CreateConversation(context.Background(), talk); err != nil {
		t.Fatal(err)
	}

	keep := func(user string, f environment.Feature, ago time.Duration, in, out int, ended environment.Ended,
		detail, conversation string) {
		if err := s.Add(context.Background(), usage.Call{ID: usage.NewID(), Call: environment.Call{
			Label: environment.Label{UserID: user, Feature: f, Detail: detail,
				ConversationID: conversation, ChatID: map[bool]string{true: "chat_x"}[conversation != ""]},
			Vendor: "anthropic", Model: "claude-sonnet-4-6",
			Input: in, Output: out, At: now.Add(-ago), Took: 2300 * time.Millisecond, Ended: ended,
		}}); err != nil {
			t.Fatal(err)
		}
	}
	// Today.
	keep(me, environment.FeatureChat, time.Hour, 1000, 50, environment.EndedTools, "tools", talk.ID)
	keep(me, environment.FeatureChat, 50*time.Minute, 1200, 80, environment.EndedAnswered, "", talk.ID)
	keep(me, environment.FeatureRuleRemark, 2*time.Hour, 3000, 20, environment.EndedFailed, "anything amiss", "")
	// This week, not today.
	keep(me, environment.FeatureProfile, 3*24*time.Hour, 9000, 400, environment.EndedAnswered, "", "")
	// This month, not this week.
	keep(me, environment.FeatureNoticing, 20*24*time.Hour, 500, 5, environment.EndedAnswered, "", "")
	// Somebody else's, which must never show.
	keep("usr_01OTHER0000000000000000000", environment.FeatureChat, time.Hour, 77777, 7, environment.EndedAnswered, "", "")
	return e, talk.ID
}

// The published field names are the contract the client reads, so the
// whole shape is pinned here, key by key.
func TestTheShapeIsPinned(t *testing.T) {
	e, _ := setUp(t)
	rec := e.Get(t, "/v1/usage")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"totals", "by_feature", "by_model", "calls", "kinds"} {
		if _, ok := raw[k]; !ok {
			t.Errorf("no %q in %s", k, rec.Body.String())
		}
	}
	var shape struct {
		Totals    map[string]map[string]int `json:"totals"`
		ByFeature []map[string]any          `json:"by_feature"`
		ByModel   []map[string]any          `json:"by_model"`
		Calls     []map[string]any          `json:"calls"`
		Kinds     map[string]int            `json:"kinds"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &shape); err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"today", "week", "month"} {
		for _, k := range []string{"calls", "input", "output", "chat_calls", "background_calls"} {
			if _, ok := shape.Totals[w][k]; !ok {
				t.Errorf("totals.%s has no %q", w, k)
			}
		}
	}
	has := func(where string, m map[string]any, keys ...string) {
		for _, k := range keys {
			if _, ok := m[k]; !ok {
				t.Errorf("%s has no %q: %v", where, k, m)
			}
		}
	}
	has("by_feature", shape.ByFeature[0], "feature", "label", "calls", "input", "output")
	has("by_model", shape.ByModel[0], "vendor", "model", "calls", "input", "output")
	for _, c := range shape.Calls {
		has("calls", c, "id", "at", "feature", "label", "vendor", "model", "input", "output", "duration_ms", "ended")
	}
	for _, k := range []string{"all", "chat", "background", "failed"} {
		if _, ok := shape.Kinds[k]; !ok {
			t.Errorf("kinds has no %q", k)
		}
	}
}

// The three windows add up only the person's own calls, today from
// midnight in the server's zone, the week and month back from now.
func TestTotalsAreTheirsForEachWindow(t *testing.T) {
	e, _ := setUp(t)
	var got usageapi.Response
	e.Decode(t, e.Get(t, "/v1/usage"), &got)

	want := usageapi.Windows{
		Today: usageapi.Totals{Calls: 3, Input: 5200, Output: 150, ChatCalls: 2, BackgroundCalls: 1},
		Week:  usageapi.Totals{Calls: 4, Input: 14200, Output: 550, ChatCalls: 2, BackgroundCalls: 2},
		Month: usageapi.Totals{Calls: 5, Input: 14700, Output: 555, ChatCalls: 2, BackgroundCalls: 3},
	}
	if got.Totals != want {
		t.Errorf("totals = %+v, want %+v", got.Totals, want)
	}
	if got.Kinds != (usageapi.Kinds{All: 3, Chat: 2, Background: 1, Failed: 1}) {
		t.Errorf("kinds today = %+v", got.Kinds)
	}
}

// The groups and the list follow days; the groups come most input
// first and carry the words a person reads.
func TestTheWindowAskedForIsGroupedAndListed(t *testing.T) {
	e, talk := setUp(t)
	var got usageapi.Response
	e.Decode(t, e.Get(t, "/v1/usage?days=7"), &got)

	if len(got.ByFeature) != 3 || got.ByFeature[0].Feature != "profile" || got.ByFeature[0].Label != "Profile" ||
		got.ByFeature[1].Feature != "rule_remark" || got.ByFeature[1].Label != "Rule remark" ||
		got.ByFeature[2].Feature != "chat" || got.ByFeature[2].Calls != 2 || got.ByFeature[2].Input != 2200 {
		t.Errorf("by feature = %+v", got.ByFeature)
	}
	if len(got.ByModel) != 1 || got.ByModel[0].Calls != 4 || got.ByModel[0].Model != "claude-sonnet-4-6" {
		t.Errorf("by model = %+v", got.ByModel)
	}
	if got.Kinds != (usageapi.Kinds{All: 4, Chat: 2, Background: 2, Failed: 1}) {
		t.Errorf("kinds this week = %+v", got.Kinds)
	}
	if len(got.Calls) != 4 {
		t.Fatalf("listed %d calls, want the week's 4", len(got.Calls))
	}
	first := got.Calls[0]
	if first.Feature != "chat" || first.Label != "Chat answer" || first.ConversationID != talk ||
		first.ConversationTitle != "The cinema booking" || first.DurationMS != 2300 ||
		first.Ended != "answered" || !first.At.Equal(now.Add(-50*time.Minute)) {
		t.Errorf("newest call = %+v", first)
	}
	if got.Calls[1].Detail != "tools" || got.Calls[2].Detail != "anything amiss" {
		t.Errorf("details = %q, %q", got.Calls[1].Detail, got.Calls[2].Detail)
	}
}

// A call with no conversation says nothing about one, rather than
// sending empty strings for the client to test.
func TestAbsentFieldsAreLeftOut(t *testing.T) {
	e, _ := setUp(t)
	rec := e.Get(t, "/v1/usage?kind=background")
	var raw struct {
		Calls []map[string]any `json:"calls"`
	}
	e.Decode(t, rec, &raw)
	if len(raw.Calls) != 1 {
		t.Fatalf("listed %d background calls today, want 1", len(raw.Calls))
	}
	for _, k := range []string{"chat_id", "conversation_id", "conversation_title"} {
		if _, ok := raw.Calls[0][k]; ok {
			t.Errorf("%q was sent for a call with none", k)
		}
	}
}

func TestTheListIsFilteredAndLimited(t *testing.T) {
	e, _ := setUp(t)
	for _, c := range []struct {
		query string
		want  int
	}{
		{"/v1/usage?days=30", 5},
		{"/v1/usage?days=30&kind=chat", 2},
		{"/v1/usage?days=30&kind=background", 3},
		{"/v1/usage?days=30&kind=failed", 1},
		{"/v1/usage?days=30&limit=2", 2},
		{"/v1/usage?days=30&limit=9999", 5},
	} {
		var got usageapi.Response
		e.Decode(t, e.Get(t, c.query), &got)
		if len(got.Calls) != c.want {
			t.Errorf("%s listed %d, want %d", c.query, len(got.Calls), c.want)
		}
	}
}

func TestNonsenseIsRefused(t *testing.T) {
	e, _ := setUp(t)
	for _, q := range []string{"days=2", "days=week", "kind=costly", "limit=0", "limit=many"} {
		if rec := e.Get(t, "/v1/usage?"+q); rec.Code != http.StatusBadRequest {
			t.Errorf("%s answered %d, want 400", q, rec.Code)
		}
	}
	if rec := e.Anonymous(t, http.MethodGet, "/v1/usage", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token answered %d, want 401", rec.Code)
	}
}

// Every feature has words for a person, so the page never shows an id.
func TestEveryFeatureHasALabel(t *testing.T) {
	for _, f := range environment.Features() {
		if _, ok := usageapi.Labels[f]; !ok {
			t.Errorf("%s has no label", f)
		}
	}
}

// A terminal's reaction is kept under its own id, stored and never
// renamed, and shown as "Terminal".
func TestATerminalsReactionHasItsFeature(t *testing.T) {
	if environment.FeatureTerminal != "terminal" || !slices.Contains(environment.Features(), environment.FeatureTerminal) {
		t.Fatalf("the feature is %q, listed %v", environment.FeatureTerminal, environment.Features())
	}
	if usageapi.Label(environment.FeatureTerminal) != "Terminal" {
		t.Errorf("labelled %q", usageapi.Label(environment.FeatureTerminal))
	}
}

// The page's charts: today by the hour, the window's endings, the chat
// answer time's spread and each feature's mean time, and yesterday to
// compare against -- all of the person's own calls only.
func TestTheInsightsAreTheWindowsByTheHour(t *testing.T) {
	e, _ := setUp(t)
	rec := e.Get(t, "/v1/usage")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Insights usageapi.Insights `json:"insights"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	in := got.Insights
	if in.Bucket != "hour" || len(in.Buckets) != 24 {
		t.Fatalf("bucketed by %q into %d", in.Bucket, len(in.Buckets))
	}
	// 15:00 now: the chats at 14:00 and 14:10, the rule remark at 13:00.
	if b := in.Buckets[14]; b.ChatInput != 2200 || b.BackgroundInput != 0 || b.Calls != 2 {
		t.Errorf("14:00 was %+v", b)
	}
	if b := in.Buckets[13]; b.BackgroundInput != 3000 || b.Calls != 1 {
		t.Errorf("13:00 was %+v", b)
	}
	if in.Ends["answered"] != 1 || in.Ends["tools"] != 1 || in.Ends["failed"] != 1 {
		t.Errorf("ends %v", in.Ends)
	}
	if in.ChatTime.Calls != 2 || in.ChatTime.P50MS != 2300 || in.ChatTime.MaxMS != 2300 {
		t.Errorf("chat time %+v", in.ChatTime)
	}
	if in.Took["chat"] != 2300 {
		t.Errorf("took %v", in.Took)
	}
	if in.Before.Calls != 0 {
		t.Errorf("yesterday had %d calls; none were kept then", in.Before.Calls)
	}
}

// Seven days are bucketed by the day, in the person's zone, the empty
// days too.
func TestAWeekIsBucketedByTheDay(t *testing.T) {
	e, _ := setUp(t)
	rec := e.Get(t, "/v1/usage?days=7")
	var got struct {
		Insights usageapi.Insights `json:"insights"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	in := got.Insights
	if in.Bucket != "day" || len(in.Buckets) != 8 {
		t.Fatalf("bucketed by %q into %d", in.Bucket, len(in.Buckets))
	}
	calls := 0
	for _, b := range in.Buckets {
		calls += b.Calls
	}
	if calls != 4 {
		t.Errorf("%d calls in the week's buckets, want 4", calls)
	}
	if last := in.Buckets[len(in.Buckets)-1]; last.Calls != 3 {
		t.Errorf("today's bucket has %d calls, want 3", last.Calls)
	}
}
