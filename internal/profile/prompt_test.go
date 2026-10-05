package profile_test

import (
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
	"github.com/DhanushRamesh/personal-assistant/internal/memory"
	"github.com/DhanushRamesh/personal-assistant/internal/profile"
)

// when : A fixed evening to measure the replies from.
var when = time.Date(2026, 10, 3, 18, 0, 0, 0, time.UTC)

// TestAReplyToSomethingVolunteeredIsWeighed : Owner: *"whenever it
// announces or greets, the acknowledgement or the respective
// response, if relevant, has to be taken more weight in profiling,
// greeting and when talking to me."*
//
// What the person says has always been read without what it
// answered -- on purpose, so the assistant does not describe
// itself. The cost was that "ok" and "stop telling me that"
// arrived as two unremarkable messages, when one of them is the
// only direct evidence there is about whether an unprompted
// sentence was wanted.
func TestAReplyToSomethingVolunteeredIsWeighed(t *testing.T) {
	said := []conversation.Message{{Content: "what is on tomorrow", At: when}}
	replies := []conversation.Reply{{
		Kind:  conversation.PresenceAnnouncement,
		Spoke: "Good evening, sir. You were out a good while.",
		Said:  "The movie was quite good.",
		At:    when, After: 14 * time.Second,
	}}

	got := profile.Prompt(said, "", "", replies, "")
	for _, want := range []string{
		"The movie was quite good", "Good evening, sir",
		"14 seconds", "Nobody asked for any of those sentences",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q", want)
		}
	}
	// And it must stay a description of them rather than a report
	// card on the assistant.
	if !strings.Contains(got, "not how well the assistant is doing") {
		t.Error("nothing stops it grading the assistant")
	}
}

// TestNoRepliesAddsNothing : Most weeks there are none, and an
// empty heading is a question the model will try to answer.
func TestNoRepliesAddsNothing(t *testing.T) {
	got := profile.Prompt([]conversation.Message{{Content: "hello", At: when}}, "", "", nil, "")
	if strings.Contains(got, "said back when the assistant spoke first") {
		t.Error("an empty section was written anyway")
	}
}

// TestRenamingIsNotAnswered : "I have called this conversation
// Today's alibi" is said in the middle of a turn, so the next thing
// the person says is the next thing they were going to say anyway.
// Measured against the real transcript it was seven of the first
// ten pairs, every one a question with nothing to do with the name.
func TestRenamingIsNotAnswered(t *testing.T) {
	for _, k := range conversation.Volunteered() {
		if k == conversation.Renaming {
			t.Fatal("housekeeping is being read as something they answered")
		}
	}
	// But everything else the assistant says unasked is.
	var found bool
	for _, k := range conversation.Volunteered() {
		if k == conversation.Immediate {
			found = true
		}
	}
	if !found {
		t.Error("a warning spoken unasked is not weighed")
	}
}

// TestTheDayHasASectionOfItsOwn : Owner: "the profiling should have
// the daily routine section -- it should be able to profile my daily
// activities."
//
// It is also what the greeting needs most. Telling an unusual day
// from an ordinary one is the whole of that job, and the ordinary
// one was scattered across two parts written for other purposes.
func TestTheDayHasASectionOfItsOwn(t *testing.T) {
	asked := profile.Prompt(nil, "", "", nil, "")
	for _, want := range []string{
		"Write three parts",
		"AN ORDINARY DAY",
		"in the order it happens, from waking to sleeping",
		"If weekdays and the weekend are different days, say so",
		"An hour nothing is known about is said to be unknown",
		"the thing an unusual day is measured against",
	} {
		if !strings.Contains(asked, want) {
			t.Errorf("the ordinary day is not asked for: %q missing", want)
		}
	}
	// And the old home for it has given it up, so the two do not
	// compete for the same hundred and fifty words.
	if strings.Contains(asked, "And their weeks: what they do most days") {
		t.Error("the day is still being asked for twice")
	}
}

// TestTheDescriptionMayBeLongerThanAMemory : It stood at 3,988 runes
// of the 4,000 a memory is allowed, which is not a description that
// had finished -- it is one that stopped. A section could not be
// added to it without the save failing, silently.
func TestTheDescriptionMayBeLongerThanAMemory(t *testing.T) {
	long := strings.Repeat("a", memory.MaxBody+500)
	if _, err := memory.New("usr_1", memory.TierAlways, profile.Subject, long); err == nil {
		t.Error("a memory somebody asked to keep is no longer held to its length")
	}
	if _, err := memory.Written("usr_1", memory.TierAlways, profile.Subject, long); err != nil {
		t.Errorf("the description the server writes for itself was refused: %v", err)
	}
	tooLong := strings.Repeat("a", memory.MaxWritten+1)
	if _, err := memory.Written("usr_1", memory.TierAlways, profile.Subject, tooLong); err == nil {
		t.Error("there is no limit at all on what the server writes")
	}
}
