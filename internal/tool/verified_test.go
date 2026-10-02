package tool_test

import (
	"strings"
	"testing"

	"github.com/DhanushRamesh/personal-assistant/internal/tool"
)

// A change carries both ends, because the person is listening and has
// nothing to compare against.
func TestAChangeCarriesBothEnds(t *testing.T) {
	got := tool.Changed("Changed the reminder",
		tool.Change{What: "the time", From: "9:00 am", To: "10:30 am"})

	for _, want := range []string{"9:00 am", "10:30 am", "was", "is now"} {
		if !strings.Contains(got.Content, want) {
			t.Errorf("content = %q, want %q in it", got.Content, want)
		}
	}
}

// Only what actually moved is reported. A field given the same value it
// already held is not a change and must not be announced as one.
func TestOnlyWhatMovedIsReported(t *testing.T) {
	got := tool.Changed("Changed the reminder",
		tool.Change{What: "the time", From: "9:00 am", To: "10:30 am"},
		tool.Change{What: "the name", From: "Tablets", To: "Tablets"})

	if strings.Contains(got.Content, "the name") {
		t.Errorf("content = %q, want the unchanged field left out", got.Content)
	}
}

// A write where nothing moved says so, rather than claiming a change.
func TestNothingMovedIsSaidPlainly(t *testing.T) {
	got := tool.Changed("Changed the reminder",
		tool.Change{What: "the time", From: "9:00 am", To: "9:00 am"})

	if !strings.Contains(got.Content, "nothing had actually moved") {
		t.Errorf("content = %q", got.Content)
	}
}

// A deletion reports both counts, which is the listener's only check
// that one thing went and not all of them.
func TestADeletionReportsBothCounts(t *testing.T) {
	got := tool.Removed("Cancelled the reminder", 4, 3, "reminder")

	if got.Outcome != "ok" {
		t.Fatalf("outcome = %q", got.Outcome)
	}
	for _, want := range []string{"were 4 reminders before", "there are 3 reminders now"} {
		if !strings.Contains(got.Content, want) {
			t.Errorf("content = %q, want %q in it", got.Content, want)
		}
	}
}

// Losing more than one is not a success. It is the failure this exists
// to catch, and it is called out rather than buried.
func TestLosingMoreThanOneIsNotQuietlyFine(t *testing.T) {
	got := tool.Removed("Cancelled the reminder", 4, 1, "reminder")

	if got.Outcome != "partial" {
		t.Errorf("outcome = %q, want partial", got.Outcome)
	}
	if !strings.Contains(got.Content, "3 fewer") {
		t.Errorf("content = %q, want it to say how many went", got.Content)
	}
}

// Singular and plural read correctly, since this is spoken aloud.
func TestCountsReadCorrectly(t *testing.T) {
	got := tool.Removed("Cancelled it", 1, 0, "reminder")
	if !strings.Contains(got.Content, "was 1 reminder before") {
		t.Errorf("content = %q, want the singular", got.Content)
	}
	if !strings.Contains(got.Content, "there are none now") {
		t.Errorf("content = %q, want none rather than zero", got.Content)
	}

	many := tool.Removed("Cancelled it", 3, 2, "memory")
	if !strings.Contains(many.Content, "were 3 memories before") {
		t.Errorf("content = %q, want the irregular plural", many.Content)
	}
}

// A write that did not survive being read back is a failure, whatever
// the write itself reported.
func TestAWriteThatDidNotTakeIsAFailure(t *testing.T) {
	got := tool.Unverified("Changing the reminder", "it still says 9:00 am")

	if got.Outcome != "failed" {
		t.Errorf("outcome = %q, want failed", got.Outcome)
	}
	if !strings.Contains(got.Content, "Do not say it is done") {
		t.Errorf("content = %q", got.Content)
	}
}

// The answer is left alone when it already said what it owed.
func TestAnAnswerThatSaidItIsUntouched(t *testing.T) {
	owed := tool.Owing([]tool.Result{
		tool.Changed("Changed it", tool.Change{What: "the time", From: "9:00 pm", To: "10:00 pm"}),
	})
	said := "Moved it from 9:00 pm to 10:00 pm, sir."

	if got := tool.Ensure(said, "", owed); got != said {
		t.Errorf("answer was changed to %q", got)
	}
}

// And gets the missing fact added when it did not.
//
// Measured: handed "the time was 9:00 pm, is now 10:00 pm", the model
// said "Moved to 10:00 pm, sir." The before is the half the person
// cannot reconstruct, so it is the half enforced.
func TestADroppedFactIsAddedBack(t *testing.T) {
	owed := tool.Owing([]tool.Result{
		tool.Changed("Changed it", tool.Change{What: "the time", From: "9:00 pm", To: "10:00 pm"}),
	})

	got := tool.Ensure("Moved to 10:00 pm, sir.", "", owed)
	if !strings.Contains(got, "9:00 pm") {
		t.Errorf("answer = %q, want the before added back", got)
	}
	if !strings.HasPrefix(got, "Moved to 10:00 pm, sir.") {
		t.Errorf("answer = %q, want what the model said kept", got)
	}
}

// Counts are enforced the same way, since they are the listener's only
// check that one thing went and not several.
func TestADroppedCountIsAddedBack(t *testing.T) {
	owed := tool.Owing([]tool.Result{tool.Removed("Cancelled it", 4, 3, "reminder")})

	got := tool.Ensure("Cancelled, sir.", "", owed)
	if !strings.Contains(got, "4") || !strings.Contains(got, "3") {
		t.Errorf("answer = %q, want both counts", got)
	}
}

// A tool that promised nothing adds nothing.
func TestNothingOwedAddsNothing(t *testing.T) {
	owed := tool.Owing([]tool.Result{tool.OK("Listed them.")})
	if got := tool.Ensure("Here they are.", "", owed); got != "Here they are." {
		t.Errorf("answer = %q", got)
	}
}

// TestEveryChangeLabelTakesWas : The from-to clause is built as
// "<label> was <old> before", so every label has to be a noun phrase that
// takes "was".
//
// It has not always been. "when it is" gave "When it is was 4:00 pm on
// Tuesday before", which is what the owner heard, and "the notes" and
// "what it says" were the same shape of mistake.
func TestEveryChangeLabelTakesWas(t *testing.T) {
	for _, label := range []string{
		"it", "the kind", "the name", "the note",
		"the place", "the subject", "the time", "the wording",
	} {
		got := tool.Changed("Changed it",
			tool.Change{What: label, From: "one", To: "two"})
		clause := label + " was one before."
		if !strings.Contains(strings.ToLower(got.Else), strings.ToLower(clause)) {
			t.Errorf("label %q does not read: %s", label, got.Else)
		}
		for _, wrong := range []string{"when ", "what ", "how "} {
			if strings.HasPrefix(label, wrong) {
				t.Errorf("label %q begins with %q, so the clause reads as a question, not a statement",
					label, strings.TrimSpace(wrong))
			}
		}
	}
}

// TestAddedOwesWhatWasMade : Creating owes the thing itself, the way
// removing owes the count.
func TestAddedOwesWhatWasMade(t *testing.T) {
	r := tool.Added("Put in the diary", "Dentist", "Dentist at 4 pm on Friday", 3, 4, "event")
	owed := tool.Merged(tool.Owing([]tool.Result{r}))
	if len(owed) != 1 {
		t.Fatalf("a creation owes one thing, got %d", len(owed))
	}

	if got := tool.Ensure("Done, sir.", "", owed); !strings.Contains(got, "Dentist at 4 pm on Friday") {
		t.Errorf("an answer that said nothing was not told what was made: %s", got)
	}

	// Naming the thing is enough. The rendering is the server's wording
	// and no model writes it, so requiring it verbatim meant every
	// creation was followed by the server repeating itself: "marked on
	// the 5th of February, sir. It is Gunalan's Birthday, all day on
	// Friday 5 February."
	for _, said := range []string{
		"I have put Dentist at 4 pm on Friday in the diary, sir.",
		"The dentist is in the diary for four o'clock on Friday, sir.",
		"Noted, sir: Dentist, Friday afternoon.",
	} {
		if got := tool.Ensure(said, "", owed); got != said {
			t.Errorf("an answer that named it was added to:\n  said: %s\n  got : %s", said, got)
		}
	}
}

// TestAddedSentencesSpeakInThePersona : A sentence the server adds is
// addressed the way the persona would address them, and only if the reply
// has not already done it.
func TestAddedSentencesSpeakInThePersona(t *testing.T) {
	// Four removals one after another, as they actually arrive. A single
	// Removed of 8 to 4 is four fewer at once, which is a different thing
	// and owes nothing.
	var raw []tool.Owed
	for _, n := range [][2]int{{8, 7}, {7, 6}, {6, 5}, {5, 4}} {
		raw = append(raw, tool.Owing([]tool.Result{tool.Removed("Taken out", n[0], n[1], "event")})...)
	}
	owed := tool.Merged(raw)

	got := tool.Ensure("They are gone.", "sir", owed)
	if !strings.Contains(got, "4 events now, sir.") && !strings.Contains(got, "now, sir.") {
		t.Errorf("the added sentence was not addressed: %s", got)
	}

	// Once per reply and never twice: the persona's own rule.
	twice := tool.Ensure("They are gone, sir.", "sir", owed)
	if strings.Count(strings.ToLower(twice), "sir") != 1 {
		t.Errorf("said sir twice in one reply: %s", twice)
	}

	// A persona that addresses nobody gets nothing added.
	plain := tool.Ensure("They are gone.", "", owed)
	if strings.Contains(plain, ",  ") || strings.HasSuffix(plain, ", .") {
		t.Errorf("an empty address left a scar: %s", plain)
	}
	if strings.Contains(plain, "sir") {
		t.Errorf("the plain persona was made to say sir: %s", plain)
	}

	// And a different persona says its own word.
	boss := tool.Ensure("They are gone.", "boss", owed)
	if !strings.Contains(boss, "boss") {
		t.Errorf("friday was not addressed as boss: %s", boss)
	}
}

// The denial follows what the model said rather than replacing it. What
// it wrote is usually the better sentence; it is only the claim at the
// end of it that is false.
func TestADenialFollowsTheAnswer(t *testing.T) {
	got := tool.Unchanged("Moved it to six ten on Sunday.", "sir")

	if !strings.HasPrefix(got, "Moved it to six ten on Sunday.") {
		t.Errorf("the answer was not kept: %q", got)
	}
	if !strings.Contains(got, "not actually made that change") {
		t.Errorf("the change was not denied: %q", got)
	}
	if !strings.Contains(got, "sir") {
		t.Errorf("the denial is not in the persona's manner: %q", got)
	}
}

// Said once. An answer already addressing the person is not addressed
// twice in two sentences.
func TestADenialDoesNotRepeatTheAddress(t *testing.T) {
	got := tool.Unchanged("I could not reach the diary, sir.", "sir")

	if n := strings.Count(got, "sir"); n != 1 {
		t.Errorf("the person is addressed %d times: %q", n, got)
	}
}

// An empty answer is a denial on its own, not a leading space. The
// model returning nothing is a failed turn, and the person still has to
// be told that nothing moved.
func TestADenialStandsAloneWhenThereIsNoAnswer(t *testing.T) {
	got := tool.Unchanged("", "sir")

	if got != "I have not actually made that change, sir." {
		t.Errorf("the denial alone reads as %q", got)
	}
}

// An obligation with no fact to check is appended anyway. That nothing
// changed is not a value that can be looked for in a sentence, and
// looking for the English words a model would use to admit it is a
// check that stops working in another language.
func TestWhatIsOwedRegardlessIsAlwaysSaid(t *testing.T) {
	owed := tool.Owing([]tool.Result{{
		Regardless: "Nothing needed changing: the length was already three hours.",
	}})
	if len(owed) != 1 || !owed[0].Always {
		t.Fatalf("not owed unconditionally: %+v", owed)
	}

	// Said even by an answer that already covered it, since there is
	// no way to tell that it did.
	got := tool.Ensure("Nothing needed changing there.", "sir", owed)
	if !strings.Contains(got, "already three hours") {
		t.Errorf("the obligation was dropped: %q", got)
	}
}

// A write that moved nothing says what it found, not only that it
// found nothing. "Nothing changed" invites the person to assume what
// they asked for was already true.
func TestAChangeThatMovedNothingNamesTheValues(t *testing.T) {
	got := tool.Changed("Changed the event",
		tool.Change{What: "the name", From: "Meesaya Murukku 2", To: "Meesaya Murukku 2"},
		tool.Change{What: "the end", From: "9:10 pm", To: "9:10 pm"},
		tool.Change{What: "the place", From: "", To: ""},
	)

	if got.Regardless == "" {
		t.Fatal("a write that moved nothing owes nothing")
	}
	for _, want := range []string{"was already", "Meesaya Murukku 2", "9:10 pm"} {
		if !strings.Contains(got.Regardless, want) {
			t.Errorf("owed = %q, want %q in it", got.Regardless, want)
		}
	}
	// One sentence, because it is heard rather than read.
	if n := strings.Count(got.Regardless, "was already"); n != 1 {
		t.Errorf("said %d times over, want one sentence: %q", n, got.Regardless)
	}
	// An empty value is not a value anybody can check.
	if strings.Contains(got.Regardless, "the place") {
		t.Errorf("an empty value was read back: %q", got.Regardless)
	}
}

// A write that did move something owes its before, as it always has,
// and owes no standing-still sentence.
func TestAChangeThatMovedOwesNothingExtra(t *testing.T) {
	got := tool.Changed("Changed the event",
		tool.Change{What: "the end", From: "9:10 pm", To: "10:10 pm"})

	if got.Regardless != "" {
		t.Errorf("a real change was reported as standing still: %q", got.Regardless)
	}
	if len(got.MustSay) != 1 || got.MustSay[0] != "9:10 pm" {
		t.Errorf("the before was not owed: %v", got.MustSay)
	}
}
