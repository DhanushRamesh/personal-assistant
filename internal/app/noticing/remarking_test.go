package noticing_test

import (
	"encoding/json"
	"testing"
	"time"

	"personal-assistant/internal/app/noticing"
	"personal-assistant/internal/domain/job"
)

// TestTheTwoAllowancesDoNotSpendEachOther : a week of useful rules
// must not silence the remarks, and a talkative week of remarks must
// not crowd out a rule that matters.
func TestTheTwoAllowancesDoNotSpendEachOther(t *testing.T) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

	useful := []job.Job{{Kind: noticing.Kind, Made: now.Add(-time.Minute)}}
	if noticing.Allowed(useful, noticing.Useful, now) {
		t.Error("one written a minute ago should stop another of its own kind")
	}
	if !noticing.Allowed(useful, noticing.Companion, now) {
		t.Error("a useful rule should not spend the companion's allowance")
	}

	remarked := []job.Job{{Kind: noticing.Remarked, Made: now.Add(-time.Minute)}}
	if noticing.Allowed(remarked, noticing.Companion, now) {
		t.Error("a remark written a minute ago should stop another")
	}
	if !noticing.Allowed(remarked, noticing.Useful, now) {
		t.Error("a remark should not spend the useful allowance")
	}
}

// TestARemarkRestsAWeek : the companion is rarer by a wide margin,
// and a remark written yesterday does not free one today.
func TestARemarkRestsAWeek(t *testing.T) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

	yesterday := []job.Job{{Kind: noticing.Remarked, Made: now.Add(-30 * time.Hour)}}
	if noticing.Allowed(yesterday, noticing.Companion, now) {
		t.Error("a remark written yesterday should still stop another")
	}
	// The same age frees a useful one, which is the whole point of
	// the two being separate numbers.
	if !noticing.Allowed([]job.Job{{Kind: noticing.Kind,
		Made: now.Add(-30 * time.Hour)}}, noticing.Useful, now) {
		t.Error("yesterday's useful rule should not count against today")
	}

	fortnight := []job.Job{{Kind: noticing.Remarked, Made: now.Add(-14 * 24 * time.Hour)}}
	if !noticing.Allowed(fortnight, noticing.Companion, now) {
		t.Error("after a fortnight he may remark again")
	}
}

// TestARemarkMustReadSomethingFirst : a rule that speaks without
// looking says the same sentence for months.
func TestARemarkMustReadSomethingFirst(t *testing.T) {
	recited := []job.Act{{Do: "say", Text: "You have been on a Rahman streak, sir."}}
	if noticing.Grounded(recited) {
		t.Error("a fixed sentence reads nothing and is not grounded")
	}

	// A write with no tools is still the model inventing from the
	// occasion alone, which is the same fortune cookie with more
	// words.
	unread := []job.Act{{Do: "write", Ask: "remark on their taste in music"}}
	if noticing.Grounded(unread) {
		t.Error("a write that may call nothing reads nothing")
	}

	looked := []job.Act{
		{Do: "tool", Use: "event_recent", With: json.RawMessage(`{"kind":"music.state"}`)},
		{Do: "write", Ask: "say something about it, or nothing"},
	}
	if !noticing.Grounded(looked) {
		t.Error("a lookup before the write is grounded")
	}

	itself := []job.Act{{Do: "write", Ask: "say something about it",
		Tools: []string{"event_recent"}}}
	if !noticing.Grounded(itself) {
		t.Error("a write that may look things up is grounded")
	}
}

// TestTheUsefulRulesAreNotHeldToTheSameTest : only the companion
// must read before it speaks. A rule that says a fixed sentence when
// the phone did not charge is saying the whole fact.
func TestTheUsefulRulesAreNotHeldToTheSameTest(t *testing.T) {
	if noticing.Useful.Grounded {
		t.Error("a useful rule may say a fixed sentence")
	}
	if !noticing.Companion.Grounded {
		t.Error("a remark may not")
	}
}

// TestBothKindsAreHisToRetire : retirement reads every purpose, so a
// remark that has stopped earning its place can be taken back.
func TestBothKindsAreHisToRetire(t *testing.T) {
	for _, j := range []job.Job{
		{Kind: noticing.Kind},
		{Kind: noticing.Remarked},
		{Kind: noticing.Kind},
		{Kind: noticing.Remarked},
	} {
		if !noticing.Mine(j) {
			t.Errorf("%+v should be one of his", j)
		}
	}
	if noticing.Mine(job.Job{Kind: "rule", Why: "because he asked"}) {
		t.Error("a rule the person asked for is not his to retire")
	}
}
