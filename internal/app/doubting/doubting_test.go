package doubting_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"personal-assistant/internal/app/doubting"
	"personal-assistant/internal/base/records/chat"
)

// set : Four cases, one of each outcome, so a score can be read by eye.
func set() []doubting.Case {
	return []doubting.Case{
		{ChatID: "a", Prompt: "what is in my diary tomorrow", Looked: true},
		{ChatID: "b", Prompt: "how much battery has the laptop", Looked: true},
		{ChatID: "c", Prompt: "what is the time", Looked: false},
		{ChatID: "d", Prompt: "thank you", Looked: false},
	}
}

// mine : A judge that doubts whichever chats it is told to.
type mine struct{ doubt map[string]bool }

func (mine) Name() string { return "mine" }
func (m mine) Doubt(_ context.Context, c doubting.Case) (bool, error) {
	return m.doubt[c.ChatID], nil
}

// broken : A judge that cannot answer.
type broken struct{}

func (broken) Name() string { return "broken" }
func (broken) Doubt(context.Context, doubting.Case) (bool, error) {
	return false, errors.New("the model is not there")
}

// TestTheTwoEndsAreTheBaselines : What the runner does today, and the
// other end. A judge is only worth having between them.
func TestTheTwoEndsAreTheBaselines(t *testing.T) {
	always, err := doubting.Score(context.Background(), doubting.Always{}, set())
	if err != nil {
		t.Fatal(err)
	}
	if always.Caught != 2 || always.Missed != 0 || always.Wasted != 2 || always.Spared != 0 {
		t.Errorf("always = %+v; want every doubt caught and every needless round paid for", always)
	}
	if always.Kept() != 1 || always.Cut() != 0 {
		t.Errorf("always kept %.2f cut %.2f; want 1 and 0", always.Kept(), always.Cut())
	}

	never, err := doubting.Score(context.Background(), doubting.Never{}, set())
	if err != nil {
		t.Fatal(err)
	}
	if never.Missed != 2 || never.Spared != 2 || never.Caught != 0 || never.Wasted != 0 {
		t.Errorf("never = %+v; want nothing caught and nothing wasted", never)
	}
	if never.Kept() != 0 || never.Cut() != 1 {
		t.Errorf("never kept %.2f cut %.2f; want 0 and 1", never.Kept(), never.Cut())
	}
}

// TestAJudgeThatCannotAnswerLooksAnyway : Failing closed is the only
// safe direction. A needless round costs seconds; a missed one costs an
// answer that was made up.
func TestAJudgeThatCannotAnswerLooksAnyway(t *testing.T) {
	s, err := doubting.Score(context.Background(), broken{}, set())
	if err != nil {
		t.Fatal(err)
	}
	if s.Failed != 4 {
		t.Errorf("failed on %d; want all four", s.Failed)
	}
	if s.Missed != 0 || s.Caught != 2 {
		t.Errorf("%+v; a judge that broke let a doubt through", s)
	}
}

// TestASavingIsStatedInSeconds : Rounds are what the server counts and
// seconds are what somebody waits, so the score carries both.
func TestASavingIsStatedInSeconds(t *testing.T) {
	s, err := doubting.Score(context.Background(), mine{doubt: map[string]bool{"a": true, "b": true}}, set())
	if err != nil {
		t.Fatal(err)
	}
	if s.Caught != 2 || s.Missed != 0 || s.Spared != 2 || s.Wasted != 0 {
		t.Fatalf("%+v; want the perfect judge on this set", s)
	}
	if want := 2 * doubting.Round; s.Saved(doubting.Round) != want {
		t.Errorf("saved %v; want %v", s.Saved(doubting.Round), want)
	}
}

// at : A call that ended a given way.
func at(min int, how chat.CallEnd, early bool) chat.Call {
	when := time.Date(2026, 10, 18, 9, min, 0, 0, time.UTC)
	return chat.Call{AskedAt: when, DoneAt: &when, Ended: how, Early: early}
}

// TestASendBackIsReadOutOfTheCallsAChatKeeps : Nothing had to be
// written down for this: a request that ended in prose, followed by
// another, is a send-back, and what the next one ended in is the label.
func TestASendBackIsReadOutOfTheCallsAChatKeeps(t *testing.T) {
	for _, tc := range []struct {
		name   string
		calls  []chat.Call
		want   bool
		looked bool
	}{
		{"answered then asked for tools", []chat.Call{at(1, chat.CallAnswered, false), at(2, chat.CallTools, false)}, true, true},
		{"answered then answered again", []chat.Call{at(1, chat.CallAnswered, false), at(2, chat.CallAnswered, false)}, true, false},
		{"answered once and left", []chat.Call{at(1, chat.CallAnswered, false)}, false, false},
		{"tools first is not a send-back", []chat.Call{at(1, chat.CallTools, false), at(2, chat.CallAnswered, false)}, false, false},
	} {
		got, ok := doubting.Sent(chat.Chat{ID: "x", Prompt: "p", Calls: tc.calls})
		if ok != tc.want {
			t.Errorf("%s: found=%v, want %v", tc.name, ok, tc.want)
			continue
		}
		if ok && got.Looked != tc.looked {
			t.Errorf("%s: looked=%v, want %v", tc.name, got.Looked, tc.looked)
		}
	}
}

// TestTheRoundAlreadyRunningIsNotTheAnswer : A request begun before the
// one in front of it finished is the round that was already in flight
// when the send-back was decided. Read as the answer, it labels the
// case by the wrong call.
func TestTheRoundAlreadyRunningIsNotTheAnswer(t *testing.T) {
	got, ok := doubting.Sent(chat.Chat{ID: "x", Prompt: "p", Calls: []chat.Call{
		at(1, chat.CallAnswered, false),
		at(1, chat.CallAnswered, true), // begun early, not the reply to the send-back
		at(2, chat.CallTools, false),
	}})
	if !ok {
		t.Fatal("no case found")
	}
	if !got.Looked {
		t.Error("labelled from the early round, so a real catch reads as waste")
	}
}

// TestOnlyKeepsWhatAJudgeCanBeReadOn : One that needs the thrown-away
// answer cannot be scored on a case recorded before those were kept.
func TestOnlyKeepsWhatAJudgeCanBeReadOn(t *testing.T) {
	cases := []doubting.Case{{ChatID: "a"}, {ChatID: "b", Draft: "Four-oh-four, sir."}}
	if got := doubting.Only(cases, true); len(got) != 1 || got[0].ChatID != "b" {
		t.Errorf("with a draft = %v; want only b", got)
	}
	if got := doubting.Only(cases, false); len(got) != 2 {
		t.Errorf("without = %v; want both", got)
	}
}
