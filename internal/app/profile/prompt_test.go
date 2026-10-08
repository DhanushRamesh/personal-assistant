package profile_test

import (
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/app/profile"
	"personal-assistant/internal/base/conversation"
	"personal-assistant/internal/domain/memory"
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

	got := profile.Prompt(profile.SoFar, profile.Evidence{Said: said, Replies: replies})
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
	got := profile.Prompt(profile.SoFar, profile.Evidence{Said: []conversation.Message{{Content: "hello", At: when}}})
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
	asked := profile.Prompt(profile.Day, profile.Evidence{})
	for _, want := range []string{
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

// A description is taken back apart so that a rebuild where one
// part failed can keep what that part said last time. The
// headings are written by this package, so this is reading back
// what it wrote.
func TestADescriptionComesApartIntoItsParts(t *testing.T) {
	whole := "WHAT IS SO\n- goes to the office, 11 times over 14 days\n\n" +
		"AN ORDINARY DAY\nUp around seven.\n\n" +
		"WHAT THEY ARE LIKE\nHe asks for things plainly."

	got := profile.Split(whole)
	if len(got) != 3 {
		t.Fatalf("%d parts out of three: %v", len(got), got)
	}
	if !strings.Contains(got[profile.SoFar], "11 times over 14 days") {
		t.Errorf("what is so came out as %q", got[profile.SoFar])
	}
	if got[profile.Day] != "Up around seven." {
		t.Errorf("the day came out as %q", got[profile.Day])
	}
	if got[profile.Like] != "He asks for things plainly." {
		t.Errorf("what they are like came out as %q", got[profile.Like])
	}
}

// Nothing to take apart, and a description missing a part, both
// come back as what is there rather than as an error.
func TestSplittingWhatIsNotThere(t *testing.T) {
	if got := profile.Split(""); len(got) != 0 {
		t.Errorf("an empty description gave %v", got)
	}
	got := profile.Split("WHAT IS SO\n- goes to the office\n\nWHAT THEY ARE LIKE\nQuiet.")
	if len(got) != 2 {
		t.Errorf("a description of two parts gave %d: %v", len(got), got)
	}
	if _, had := got[profile.Day]; had {
		t.Error("a part that was not there came back anyway")
	}
}

// The last part is what the first adds up to, so it is given what
// the first just said. The other two are told nothing about each
// other.
func TestOnlyTheLastPartIsToldWhatTheFirstFound(t *testing.T) {
	e := profile.Evidence{SoFar: "- goes to the office, 11 times over 14 days"}
	for _, part := range profile.Parts {
		asked := profile.Prompt(part, e)
		has := strings.Contains(asked, "11 times over 14 days")
		if part == profile.Like && !has {
			t.Error("the prose part was not told what the counted one found")
		}
		if part != profile.Like && has {
			t.Errorf("%q was told what the counted part found", part)
		}
	}
}

// TestEachPartIsGivenOnlyItsOwnEvidence : Three calls handed the same
// week of talk and the same month of counts wrote the same findings
// three times over, each in its own register. The commute appeared as
// a count, then as an hour of the day, then as a remark about his
// weeks.
//
// So the day is hours and places and is given no conversation, and the
// prose is words and is given no raw counts -- it reaches them through
// what the counted part made of them, which it is handed separately.
func TestEachPartIsGivenOnlyItsOwnEvidence(t *testing.T) {
	at := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	e := profile.Evidence{
		Said:    []conversation.Message{{Content: "remind me about the barber", At: at}},
		Rhythm:  "- office: arriving or leaving recorded on 4 days",
		Planned: "Thu 1 Oct 9:00 am: barber",
		Asking:  "their diary, most of all",
	}

	for _, c := range []struct {
		part  profile.Part
		wants []string
		not   []string
	}{
		{profile.SoFar,
			[]string{"barber", "recorded on 4 days", "most of all"},
			nil},
		{profile.Day,
			[]string{"recorded on 4 days", "9:00 am"},
			[]string{"remind me about the barber", "most of all"}},
		{profile.Like,
			[]string{"remind me about the barber"},
			[]string{"recorded on 4 days", "most of all"}},
	} {
		asked := profile.Prompt(c.part, e)
		for _, want := range c.wants {
			if !strings.Contains(asked, want) {
				t.Errorf("%q was not given %q", c.part, want)
			}
		}
		for _, avoid := range c.not {
			if strings.Contains(asked, avoid) {
				t.Errorf("%q was given %q, which is another part's evidence", c.part, avoid)
			}
		}
	}
}

// TestAPartIsCorrectingOnlyItsOwnSection : A call is asked to correct
// what it wrote last time. The other two sections are the other two
// calls' business, and handing all three to each of them is how one
// part comes to rewrite another's.
func TestAPartIsCorrectingOnlyItsOwnSection(t *testing.T) {
	// As write assembles it: the heading, then the body.
	was := strings.Join([]string{
		string(profile.SoFar) + "\n- plays badminton, 4 times over 7 days",
		string(profile.Day) + "\nLeaves around ten.",
		string(profile.Like) + "\nHe keeps a close eye on birthdays.",
	}, "\n\n")

	for _, c := range []struct {
		part profile.Part
		mine string
	}{
		{profile.SoFar, "plays badminton"},
		{profile.Day, "Leaves around ten"},
		{profile.Like, "close eye on birthdays"},
	} {
		asked := profile.Prompt(c.part, profile.Evidence{Was: was})
		if !strings.Contains(asked, c.mine) {
			t.Errorf("%q was not given its own section back", c.part)
		}
		for _, other := range []string{"plays badminton", "Leaves around ten", "close eye on birthdays"} {
			if other == c.mine {
				continue
			}
			if strings.Contains(asked, other) {
				t.Errorf("%q was given another part's section: %q", c.part, other)
			}
		}
	}
}
