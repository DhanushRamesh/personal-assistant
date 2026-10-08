package situations_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/app/tool"
	"personal-assistant/internal/app/tool/situations"
	"personal-assistant/internal/base/chat"
	"personal-assistant/internal/base/conversation"
	"personal-assistant/internal/base/event"
	"personal-assistant/internal/base/prompt"
	"personal-assistant/internal/domain/situation"
)

var noon = time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

// held : Readings, as the tool reads them.
type held struct{ evs []event.Event }

func (h held) Recent(context.Context, string, event.Query) ([]event.Event, error) {
	return h.evs, nil
}

// found : A check that always finds the same thing.
func found(says string) situation.Check {
	return func([]event.Event, time.Time) (situation.Found, bool) {
		return situation.Found{Key: says, Says: says}, true
	}
}

// nothing : A check that never finds anything.
func nothing() situation.Check {
	return func([]event.Event, time.Time) (situation.Found, bool) {
		return situation.Found{}, false
	}
}

// ask : Runs the tool as the person.
func ask(t *testing.T, checks ...situation.Check) tool.Result {
	t.Helper()
	all := situations.Tools(held{}, checks, func() time.Time { return noon })
	if len(all) != 1 {
		t.Fatalf("%d tools, want one", len(all))
	}
	return all[0].Run(context.Background(), tool.Invocation{
		Caller: tool.Caller{UserID: "usr_1", Channel: chat.ChannelDirect},
	})
}

// These are interruptions, and two at once is one too many. The
// first check that finds something wins, in the order they were
// given.
func TestOneThingAtATime(t *testing.T) {
	out := ask(t, nothing(), found("The cable may not be in, sir."), found("Something else, sir."))
	if out.Outcome == conversation.OutcomeFailed {
		t.Fatalf("refused: %s", out.Content)
	}
	if out.Content != "The cable may not be in, sir." {
		t.Errorf("it said %q", out.Content)
	}
	if strings.Contains(out.Content, "Something else") {
		t.Error("it said both")
	}
}

// Finding nothing answers with the word the engine reads as the
// end of a sequence, so a rule that looks every two minutes costs
// a model call only when there is something to say.
func TestNothingFoundEndsTheSequence(t *testing.T) {
	for _, c := range []struct {
		why    string
		checks []situation.Check
	}{
		{"no check finds anything", []situation.Check{nothing(), nothing()}},
		{"there are no checks", nil},
	} {
		out := ask(t, c.checks...)
		if out.Outcome == conversation.OutcomeFailed {
			t.Fatalf("%s: refused: %s", c.why, out.Content)
		}
		if !prompt.Nothing(out.Content) {
			t.Errorf("%s: it answered %q", c.why, out.Content)
		}
	}
}

// Two things the watcher did that this does not, so that nobody
// looks for them here.
//
// It held a situation said for six hours, keyed per check. A rule
// is now told what it said last time and asked whether what it
// has is the same news, which covers that and covers the part the
// key could not: the quiet check's sentence says how long the
// silence has been, so it differed every run.
//
// And it asked whether anybody was in the room before saying
// anything. Presence belongs to the client now -- the server
// publishes a copy per client and each decides for itself whether
// anybody heard.
func TestWhatThisNoLongerDoes(t *testing.T) {
	first := ask(t, found("The cable may not be in, sir."))
	again := ask(t, found("The cable may not be in, sir."))
	if first.Content != again.Content {
		t.Errorf("the tool kept state between calls: %q then %q", first.Content, again.Content)
	}
}
