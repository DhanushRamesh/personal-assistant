package memory_test

import (
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/domain/memory"
)

// Nothing stored adds nothing to the prompt, rather than an empty heading.
func TestNothingProducesNoBlock(t *testing.T) {
	if got := memory.Standing(nil); got != "" {
		t.Errorf("Standing = %q, want empty", got)
	}
	if got := memory.Offered(nil, time.UTC); got != "" {
		t.Errorf("Offered = %q, want empty", got)
	}
}

// What is always known is stated plainly, one per line.
func TestStandingListsWhatIsKnown(t *testing.T) {
	got := memory.Standing([]memory.Memory{
		{Subject: "Birthday", Body: "22 October 1999"},
		{Subject: "Home", Body: "Chennai"},
	})

	for _, want := range []string{"22 October 1999", "Chennai"} {
		if !strings.Contains(got, want) {
			t.Errorf("Standing is missing %q:\n%s", want, got)
		}
	}
	if lines := strings.Count(got, "\n- "); lines != 2 {
		t.Errorf("got %d listed facts, want 2", lines)
	}
}

// The offered notes say that none of them fitting is the usual case.
//
// This is the whole of what makes them safe. A question with nothing stored
// about it still has a nearest memory, and it scores in the same range as a
// real match, so without being told the model treats a near miss as an
// answer.
func TestOfferedSaysTheyMayNotFit(t *testing.T) {
	got := memory.Offered([]memory.Match{
		{Memory: memory.Memory{Subject: "Roof quote", Body: "forty thousand"}},
	}, time.UTC)

	for _, want := range []string{
		"most of the time none of them will",
		"ignoring all of them",
		"judge whether it is the same thing",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Offered is missing %q:\n%s", want, got)
		}
	}
}

// A note that was not used is not mentioned, or the answer becomes a report
// about the assistant's filing.
func TestOfferedForbidsTalkingAboutUnusedNotes(t *testing.T) {
	got := memory.Offered([]memory.Match{{Memory: memory.Memory{Subject: "A", Body: "b"}}}, time.UTC)

	if !strings.Contains(got, "Do not mention a note you did not use") {
		t.Errorf("Offered lets the assistant talk about notes it ignored:\n%s", got)
	}
}

// Every offered memory is in the block.
func TestOfferedCarriesEveryNote(t *testing.T) {
	got := memory.Offered([]memory.Match{
		{Memory: memory.Memory{Subject: "Roof", Body: "forty thousand"}},
		{Memory: memory.Memory{Subject: "Cricket", Body: "Kapil Dev"}},
		{Memory: memory.Memory{Subject: "Home", Body: "Chennai"}},
	}, time.UTC)

	for _, want := range []string{"forty thousand", "Kapil Dev", "Chennai"} {
		if !strings.Contains(got, want) {
			t.Errorf("Offered is missing %q", want)
		}
	}
}

// IDs are taken in the order they were matched, so what was offered can be
// recorded as used.
func TestIDsFollowTheMatches(t *testing.T) {
	got := memory.IDs([]memory.Match{
		{Memory: memory.Memory{ID: "mem_1"}},
		{Memory: memory.Memory{ID: "mem_2"}},
	})

	if len(got) != 2 || got[0] != "mem_1" || got[1] != "mem_2" {
		t.Errorf("IDs = %v", got)
	}
}

// The licence to warn is narrow on purpose. Unbounded, it produces an
// assistant that second-guesses every sentence; without it, one that
// watches somebody walk into something it knew about.
func TestOfferedPermitsAWarningAndBoundsIt(t *testing.T) {
	got := memory.Offered([]memory.Match{
		{Memory: memory.Memory{Subject: "Roof quote", Body: "Agreed forty thousand."}},
	}, time.UTC)

	for _, want := range []string{
		"says what they are about to do",
		"a note disagrees with it",
		"one short line",
		"say what it is you are going on",
		"not where it merely shares a subject",
		"not where it agrees with what they intend",
		"invent no concern it does not support",
		"Most turns need no such line",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the licence to warn is missing %q:\n%s", want, got)
		}
	}
}

// Warning and answering are told apart, because the rule for one forbids
// the other.
func TestAnsweringAndWarningAreSeparateInstructions(t *testing.T) {
	got := memory.Offered([]memory.Match{{Memory: memory.Memory{Subject: "A", Body: "b"}}}, time.UTC)

	answer := strings.Index(got, "To answer with:")
	warn := strings.Index(got, "To warn with:")
	if answer < 0 || warn < 0 {
		t.Fatalf("the two jobs are not named separately:\n%s", got)
	}
	if warn < answer {
		t.Error("warning is stated before answering, which is not the common case")
	}
}

// The transcript carries no licence to warn. It contains whatever
// speech-to-text got wrong, and a warning founded on a misheard sentence is
// worse than no warning at all.
func TestTheTranscriptMayNotWarn(t *testing.T) {
	got := memory.Quoted([]memory.Heard{{
		Exchange: memory.Exchange{Text: "They said: something", At: time.Now()},
	}}, time.UTC)

	if strings.Contains(got, "To warn with:") {
		t.Errorf("the transcript was given the licence to warn:\n%s", got)
	}
	if !strings.Contains(got, "never state one as a fact of your own") {
		t.Errorf("the transcript lost its quoting rule:\n%s", got)
	}
}

// Refusing a note because the question named the same thing differently is
// a miss and a false statement at once, so both are forbidden by name.
func TestOfferedJudgesTheThingNotTheWording(t *testing.T) {
	got := memory.Offered([]memory.Match{{Memory: memory.Memory{Subject: "Roof quote", Body: "forty thousand"}}}, time.UTC)

	for _, want := range []string{
		"named one way in the question and another in the note",
		"judge whether it is the same thing",
		"Do not stretch a note to cover a different thing",
		"never say there is nothing on record",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Offered is missing %q:\n%s", want, got)
		}
	}
}

// A note says when it was written, because precedence already tells the
// assistant to say what was said "and when" -- and before this it had no
// way to know. The day, in the person's own time, and never the minute.
func TestANoteSaysWhenItWasWritten(t *testing.T) {
	chennai, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Skipf("no zone database here: %v", err)
	}
	// Late enough in the day that the zone changes the date, which is
	// what makes this a test of the zone and not of the format.
	at := time.Date(2026, time.October, 16, 21, 30, 0, 0, time.UTC)

	got := memory.Offered([]memory.Match{
		{Memory: memory.Memory{Subject: "Girlfriend", Body: "Alekhya is in Hyderabad.", CreatedAt: at}},
	}, chennai)

	if !strings.Contains(got, "[written 17 October 2026]") {
		t.Errorf("a note does not say when it was written, in their own time:\n%s", got)
	}
	if strings.Contains(got, "pm]") || strings.Contains(got, ":") && strings.Contains(got, "[written 17 October 2026 at") {
		t.Errorf("a note carries the clock as well as the day:\n%s", got)
	}
	if !strings.Contains(got, "stamped with the day it was written") {
		t.Errorf("nothing tells the assistant what the stamp is:\n%s", got)
	}
}

// Amending a memory is writing it again, so the later day is the one that
// counts: a note changed last week is evidence about last week.
func TestAnAmendedNoteSaysWhenItWasChanged(t *testing.T) {
	made := time.Date(2026, time.January, 2, 9, 0, 0, 0, time.UTC)
	changed := time.Date(2026, time.September, 30, 9, 0, 0, 0, time.UTC)

	got := memory.Offered([]memory.Match{
		{Memory: memory.Memory{Subject: "A", Body: "b", CreatedAt: made, UpdatedAt: changed}},
	}, time.UTC)

	if !strings.Contains(got, "[written 30 September 2026]") {
		t.Errorf("an amended note is dated from when it was first stored:\n%s", got)
	}
}

// A memory with no times on it says nothing rather than 1 January year one,
// which is every memory built in a test and none built by the server.
func TestANoteWithNoTimeSaysNothing(t *testing.T) {
	got := memory.Offered([]memory.Match{
		{Memory: memory.Memory{Subject: "A", Body: "b"}},
	}, time.UTC)

	if strings.Contains(got, "[written") {
		t.Errorf("a note with no stored time was dated anyway:\n%s", got)
	}
}

// TestWhatTheStampCosts : Printed rather than asserted. This block is in
// front of the model on every turn that recalls anything, and what it
// costs is worth knowing by the character rather than by the feeling.
func TestWhatTheStampCosts(t *testing.T) {
	at := time.Date(2026, time.October, 17, 9, 0, 0, 0, time.UTC)
	notes := func(dated bool) []memory.Match {
		out := []memory.Match{
			{Memory: memory.Memory{Subject: "Girlfriend location", Body: "Girlfriend Alekhya Chintada is in Hyderabad."}},
			{Memory: memory.Memory{Subject: "Roof quote", Body: "The roofer quoted forty thousand rupees."}},
			{Memory: memory.Memory{Subject: "Location", Body: "Lives in Chennai."}},
		}
		if dated {
			for i := range out {
				out[i].Memory.CreatedAt = at
			}
		}
		return out
	}

	dated := memory.Offered(notes(true), time.UTC)
	plain := memory.Offered(notes(false), time.UTC)

	t.Logf("three notes: %d characters, of which the stamps are %d", len(dated), len(dated)-len(plain))
	t.Logf("the instruction that explains them is in both, and costs %d once",
		len("\n\nEach note is stamped with the day it was written, and is evidence of what was "+
			"true then. Where somebody was, what a thing cost, what was decided: any of it may "+
			"have moved on since. Say when a note is from rather than stating it as true now."))
	for _, line := range strings.Split(dated, "\n") {
		if strings.HasPrefix(line, "- ") {
			t.Logf("  %s", line)
		}
	}
}
