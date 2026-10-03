package notice_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/event"
	"github.com/DhanushRamesh/personal-assistant/internal/notice"
)

var at = time.Date(2026, 10, 4, 21, 0, 0, 0, time.UTC)

// happened : One thing, a given time before now.
func happened(kind, value string, ago time.Duration) event.Event {
	payload, _ := json.Marshal(map[string]string{"value": value})
	return event.Event{Kind: kind, Payload: payload, OccurredAt: at.Add(-ago)}
}

// TestTheThirdOneIsWorthMentioning : The owner's own example. Three
// calls from the same number inside a quarter of an hour is somebody
// trying to reach him; the third is the one worth saying.
func TestTheThirdOneIsWorthMentioning(t *testing.T) {
	history := []event.Event{
		happened("call.missed", "+919000000001", 12*time.Minute),
		happened("call.missed", "+919000000001", 6*time.Minute),
		happened("call.missed", "+919000000001", 0),
	}
	arrived := history[2:]

	got := notice.Find(arrived, history, at)
	if len(got) != 1 {
		t.Fatalf("found %d candidates, want 1: %+v", len(got), got)
	}
	if got[0].Reason != notice.Often {
		t.Errorf("reason = %q, want %q", got[0].Reason, notice.Often)
	}
	if got[0].Times != 3 {
		t.Errorf("counted %d, want 3", got[0].Times)
	}
}

// TestTwiceIsNotWorthMentioning : Two of anything is a coincidence,
// and a remark made on a coincidence is worse than one not made.
func TestTwiceIsNotWorthMentioning(t *testing.T) {
	history := []event.Event{
		happened("call.missed", "+919000000001", 30*24*time.Hour),
		happened("call.missed", "+919000000001", 6*time.Minute),
		happened("call.missed", "+919000000001", 0),
	}
	for _, c := range notice.Find(history[2:], history, at) {
		if c.Reason == notice.Often {
			t.Errorf("two inside the window was called insistent: %+v", c)
		}
	}
}

// TestSomethingNeverSeenBeforeIsWorthMentioning : The other half,
// and the owner's second case: a song he has not played before is
// something it may remark on.
func TestSomethingNeverSeenBeforeIsWorthMentioning(t *testing.T) {
	history := []event.Event{
		happened("music.started", "Veyyon Silli", 3*24*time.Hour),
		happened("music.started", "Nani Kini", 0),
	}
	got := notice.Find(history[1:], history, at)
	if len(got) != 1 || got[0].Reason != notice.Novel {
		t.Fatalf("a first hearing was not noticed: %+v", got)
	}
	if got[0].Label != `music.started Nani Kini` {
		t.Errorf("label = %q", got[0].Label)
	}
}

// TestSomethingOrdinaryIsNotMentioned : Joining the home network is
// not news. He was there.
func TestSomethingOrdinaryIsNotMentioned(t *testing.T) {
	var history []event.Event
	for i := 1; i <= 20; i++ {
		history = append(history, happened("wifi.joined", "Dhanush", time.Duration(i)*8*time.Hour))
	}
	arrived := []event.Event{happened("wifi.joined", "Dhanush", 0)}
	history = append(history, arrived...)

	if got := notice.Find(arrived, history, at); len(got) != 0 {
		t.Errorf("something that happens daily was called news: %+v", got)
	}
}

// TestWorkingMaterialIsNeverMentioned : Position readings arrive
// every few minutes and every value is distinct, so unchecked every
// one of them would look like something never seen before.
func TestWorkingMaterialIsNeverMentioned(t *testing.T) {
	arrived := []event.Event{
		happened(event.Fixed, "12.91100,80.06241", 0),
		happened("music.state", "something", 0),
	}
	if got := notice.Find(arrived, arrived, at); len(got) != 0 {
		t.Errorf("working material was offered as news: %+v", got)
	}
}

// TestNothingIsSaidToAnEmptyRoom : An assistant that cannot tell
// whether anybody is there talks to the wall.
func TestNothingIsSaidToAnEmptyRoom(t *testing.T) {
	c := notice.Candidate{Label: "x", Reason: notice.Novel, At: at}

	out := &notice.Gate{Away: func(string) (bool, bool) { return true, true }}
	if out.Allow("usr_1", c, at) {
		t.Error("spoke to somebody who is out")
	}
	unknown := &notice.Gate{Away: func(string) (bool, bool) { return false, false }}
	if unknown.Allow("usr_1", c, at) {
		t.Error("spoke when it could not tell whether anybody was there")
	}
	nobodyWatching := &notice.Gate{}
	if nobodyWatching.Allow("usr_1", c, at) {
		t.Error("spoke with nothing watching the room at all")
	}
}

// TestNotTwiceInAHurry : The failure that kills this is being a
// nuisance, after which the remarks worth hearing are ignored too.
func TestNotTwiceInAHurry(t *testing.T) {
	g := &notice.Gate{Away: func(string) (bool, bool) { return false, true }}
	first := notice.Candidate{Label: "a", Reason: notice.Novel, At: at}
	second := notice.Candidate{Label: "b", Reason: notice.Novel, At: at}

	if !g.Allow("usr_1", first, at) {
		t.Fatal("the first remark was refused")
	}
	if g.Allow("usr_1", second, at.Add(time.Minute)) {
		t.Error("two remarks a minute apart")
	}
	if !g.Allow("usr_1", second, at.Add(notice.Quiet+time.Second)) {
		t.Error("still silent after the quiet had passed")
	}
}

// TestNotTwiceAboutTheSameThing : Once per thing. But not for ever:
// somebody calling repeatedly today and again next month is two
// pieces of news.
func TestNotTwiceAboutTheSameThing(t *testing.T) {
	g := &notice.Gate{Away: func(string) (bool, bool) { return false, true }}
	c := notice.Candidate{Label: "a", Reason: notice.Often, At: at}

	if !g.Allow("usr_1", c, at) {
		t.Fatal("the first remark was refused")
	}
	later := at.Add(notice.Quiet * 2)
	if g.Allow("usr_1", c, later) {
		t.Error("said the same thing twice in a day")
	}
	if !g.Allow("usr_1", c, at.Add(notice.Over+time.Minute)) {
		t.Error("the same thing a month later is still news, and was refused")
	}
}
