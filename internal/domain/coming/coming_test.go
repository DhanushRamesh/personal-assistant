package coming_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/domain/calendar"
	"github.com/DhanushRamesh/personal-assistant/internal/domain/coming"
)

var kolkata = time.FixedZone("IST", 5*3600+1800)

// at : A fixed Sunday afternoon to measure everything from.
var at = time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)

// TestTheLeadIsWhateverTheDoingNeeds : Owner: *"a movie at 6 is fine
// a few hours before. Attending a wedding has to be said a day or
// two before, because I need to plan -- buy a gift, iron the shirts.
// So the reminder start depends on what action it is."*
//
// Nothing here knows what either of those is. What is tested is that
// whatever the model says is carried through intact.
func TestTheLeadIsWhateverTheDoingNeeds(t *testing.T) {
	got := coming.Read("3 days -- a present to buy and a shirt to iron\n2 hours -- time to leave")
	if len(got) != 2 {
		t.Fatalf("two moments were asked for, got %d: %+v", len(got), got)
	}
	if got[0].Before != 72*time.Hour {
		t.Errorf("the first is %s, want 3 days", got[0].Before)
	}
	if got[1].Before != 2*time.Hour {
		t.Errorf("the second is %s, want 2 hours", got[1].Before)
	}
	if !strings.Contains(got[0].Why, "present to buy") {
		t.Errorf("the reason was lost: %q", got[0].Why)
	}
}

// TestTheLongestWarningIsSaidFirst : They are spoken in order and
// counted through, so a model answering the other way round must not
// put the day-of word before the days-before one.
func TestTheLongestWarningIsSaidFirst(t *testing.T) {
	got := coming.Read("2 hours -- time to leave\n3 days -- buy something")
	if len(got) != 2 || got[0].Before < got[1].Before {
		t.Fatalf("not longest first: %+v", got)
	}
}

// TestMostOfADiaryIsSilent : A holiday, a birthday, somebody else's
// block. Saying nothing is the ordinary answer and must not be read
// as a failure to answer.
func TestMostOfADiaryIsSilent(t *testing.T) {
	for _, answer := range []string{"NOTHING", "nothing", " Nothing. ", ""} {
		if got := coming.Read(answer); len(got) != 0 {
			t.Errorf("%q gave %+v, want silence", answer, got)
		}
		if !coming.Nothing(answer) {
			t.Errorf("%q was not read as silence", answer)
		}
	}
}

// TestNothingAbsurdGetsThrough : A ceiling and a floor, because the
// model is being asked a judgement and a judgement can be wrong. A
// fortnight's notice is forgotten by the time it matters; four
// minutes is not notice at all.
func TestNothingAbsurdGetsThrough(t *testing.T) {
	got := coming.Read("3 weeks -- far too early\n4 minutes -- far too late")
	if len(got) != 1 {
		t.Fatalf("want one survivor, got %+v", got)
	}
	if got[0].Before != coming.Longest {
		t.Errorf("the long one was not capped: %s", got[0].Before)
	}
}

// TestALineThatSaysNoTimeIsNotALead : Guessing a moment out of a
// sentence would put a warning somewhere nobody chose.
func TestALineThatSaysNoTimeIsNotALead(t *testing.T) {
	if got := coming.Read("You should probably get a present at some point"); len(got) != 0 {
		t.Errorf("a sentence became a lead: %+v", got)
	}
}

// TestEachMomentIsSaidOnce : The failure that kills this is the same
// warning every five minutes until the thing starts.
func TestEachMomentIsSaidOnce(t *testing.T) {
	j := coming.Judged{
		Starts: at.Add(48 * time.Hour),
		Leads: []coming.Lead{
			{Before: 72 * time.Hour}, {Before: 2 * time.Hour},
		},
	}

	i, due := j.Due(at)
	if !due || i != 0 {
		t.Fatalf("the three-day warning is not due: %d %v", i, due)
	}
	j.Said = 1
	if _, due := j.Due(at); due {
		t.Error("the same warning came due twice")
	}

	later := at.Add(47 * time.Hour)
	i, due = j.Due(later)
	if !due || i != 1 {
		t.Fatalf("the two-hour warning is not due: %d %v", i, due)
	}
}

// TestNothingIsSaidOnceItHasStarted : A warning about a thing already
// under way is not a warning.
func TestNothingIsSaidOnceItHasStarted(t *testing.T) {
	j := coming.Judged{Starts: at, Leads: []coming.Lead{{Before: time.Hour}}}
	if !j.Over(at) {
		t.Error("a thing starting now still had something to say")
	}
	if !j.Over(at.Add(time.Minute)) {
		t.Error("a thing already begun still had something to say")
	}
}

// TestRenamingThrowsTheJudgementAway : "dinner" becoming "dinner with
// the Raos at their place" is a different amount of doing.
func TestRenamingThrowsTheJudgementAway(t *testing.T) {
	was := coming.Shape("dinner", at)
	if coming.Shape("dinner with the Raos", at) == was {
		t.Error("a rename kept its old judgement")
	}
	if coming.Shape("dinner", at.Add(time.Hour)) == was {
		t.Error("moving it kept its old judgement")
	}
	if coming.Shape("  Dinner ", at) != was {
		t.Error("tidying the capitals counted as a change")
	}
}

// diary : A diary holding whatever a test puts in it.
type diary struct {
	events []calendar.Event
	err    error
}

func (d *diary) Everywhere(context.Context, string, time.Time, time.Time) ([]calendar.Event, []string, error) {
	return d.events, nil, d.err
}

// store : Judgements, in memory.
type store struct {
	held []coming.Judged
	kept []coming.Judged
}

func (s *store) Held(context.Context, string, time.Time) ([]coming.Judged, error) {
	return s.held, nil
}
func (s *store) Keep(_ context.Context, j coming.Judged) error {
	s.kept = append(s.kept, j)
	s.held = append(s.held, j)
	return nil
}
func (s *store) Spoke(_ context.Context, _, eventID string, said int, at time.Time) error {
	for i := range s.held {
		if s.held[i].EventID == eventID {
			s.held[i].Said, s.held[i].Spoke = said, at
		}
	}
	return nil
}
func (s *store) Settle(_ context.Context, _, eventID string, at time.Time) error {
	for i := range s.held {
		if s.held[i].EventID == eventID {
			s.held[i].Settled = at
		}
	}
	return nil
}
func (s *store) Trim(_ context.Context, _, eventID, still string, _ time.Time) error {
	for i := range s.held {
		if s.held[i].EventID == eventID {
			s.held[i].Still = still
		}
	}
	return nil
}
func (s *store) Drop(context.Context, string, []string) error { return nil }

// TestItSpeaksBeforeTheThingArrives : The whole point. A wedding in
// two days, judged as needing three, is said now.
func TestItSpeaksBeforeTheThingArrives(t *testing.T) {
	var spoken []string
	l := &coming.Loop{
		Diary: &diary{events: []calendar.Event{{
			ID: "evt_1", Title: "Attending a wedding",
			Starts: at.Add(48 * time.Hour), Ends: at.Add(54 * time.Hour), Mine: true,
		}}},
		Store:    &store{},
		Location: kolkata,
		Now:      func() time.Time { return at },
		Ask: func(context.Context, string, string) (string, error) {
			return "3 days -- a present to buy and a shirt to iron", nil
		},
		Say: func(_ context.Context, _, text string) error {
			spoken = append(spoken, text)
			return nil
		},
	}

	pass := l.Once(context.Background(), "usr_1")
	if pass.Said != 1 {
		t.Fatalf("said %d, want 1: %+v", pass.Said, pass)
	}
	if !strings.Contains(spoken[0], "Attending a wedding") ||
		!strings.Contains(spoken[0], "present to buy") {
		t.Errorf("the sentence lost something: %q", spoken[0])
	}
}

// TestASilentEntryIsJudgedOnce : Most of a diary needs nothing, and
// paying a model call for that answer every five minutes is three
// hundred calls a day to be told the same thing.
func TestASilentEntryIsJudgedOnce(t *testing.T) {
	asked := 0
	s := &store{}
	l := &coming.Loop{
		Diary: &diary{events: []calendar.Event{{
			ID: "evt_1", Title: "Independence Day", Calendar: "Holidays in India",
			Starts: at.Add(100 * time.Hour), Ends: at.Add(124 * time.Hour), AllDay: true,
		}}},
		Store: s,
		Now:   func() time.Time { return at },
		Ask: func(context.Context, string, string) (string, error) {
			asked++
			return coming.Silence, nil
		},
		Say: func(context.Context, string, string) error {
			t.Error("a holiday was announced")
			return nil
		},
	}

	l.Once(context.Background(), "usr_1")
	l.Once(context.Background(), "usr_1")
	if asked != 1 {
		t.Errorf("the model was asked %d times, want 1", asked)
	}
}

// TestAFailedJudgementIsTriedAgain : A model that could not be
// reached told us nothing about the entry, so nothing may be kept
// about it.
func TestAFailedJudgementIsTriedAgain(t *testing.T) {
	s := &store{}
	l := &coming.Loop{
		Diary: &diary{events: []calendar.Event{{
			ID: "evt_1", Title: "Something", Starts: at.Add(time.Hour), Mine: true,
		}}},
		Store: s,
		Now:   func() time.Time { return at },
		Ask: func(context.Context, string, string) (string, error) {
			return "", errors.New("the model is away")
		},
		Say: func(context.Context, string, string) error { return nil },
	}

	if pass := l.Once(context.Background(), "usr_1"); pass.Failed != 1 {
		t.Errorf("failed %d, want 1", pass.Failed)
	}
	if len(s.kept) != 0 {
		t.Errorf("a judgement was kept from a failure: %+v", s.kept)
	}
}

// TestACancelledEntryCannotSpeak : Judgements outlive the diary
// unless something forgets them.
func TestACancelledEntryCannotSpeak(t *testing.T) {
	var kept []string
	l := &coming.Loop{
		Diary: &diary{events: []calendar.Event{{
			ID: "evt_live", Title: "Still there", Starts: at.Add(time.Hour), Mine: true,
		}}},
		Store: &dropping{keep: &kept},
		Now:   func() time.Time { return at },
		Ask:   func(context.Context, string, string) (string, error) { return coming.Silence, nil },
		Say:   func(context.Context, string, string) error { return nil },
	}

	l.Once(context.Background(), "usr_1")
	if len(kept) != 1 || kept[0] != "evt_live" {
		t.Errorf("the living entries were %v, want just evt_live", kept)
	}
}

type dropping struct {
	store
	keep *[]string
}

func (d *dropping) Drop(_ context.Context, _ string, keep []string) error {
	*d.keep = keep
	return nil
}

// TestChangingTheQuestionAsksAgain : The first wording put "a
// birthday nobody has to attend" among the things needing no
// warning, so every birthday was judged silent. Owner: *"birthdays
// should be told a few days before to buy a gift."*
//
// Fixing the words fixed nothing on its own: a hundred entries were
// already decided under the old ones and nothing would ask again. So
// the wording is part of the shape, and a kept answer to a question
// nobody asks any more stops matching.
func TestChangingTheQuestionAsksAgain(t *testing.T) {
	under := coming.Shape("Alekhya's birthday", at)
	if !strings.Contains(under, "#") {
		t.Fatalf("the shape does not say which question was asked: %q", under)
	}
	// Two different questions must not share a shape. Reaching the
	// real one is enough to know the fingerprint is of the wording
	// and not of a constant.
	if strings.HasSuffix(under, "#") {
		t.Error("the fingerprint of the question is empty")
	}
}

// TestADayAboutSomebodyElseEarnsWarning : The method, not the kind.
// Nothing in the question names a birthday; it asks whether the day
// belongs to another person, which is as true of an anniversary or
// a leaving-do.
func TestADayAboutSomebodyElseEarnsWarning(t *testing.T) {
	if strings.Contains(strings.ToLower(coming.Asking), "birthday") {
		t.Error("the question names a kind of event instead of asking a method")
	}
	if !strings.Contains(strings.ToLower(coming.Asking), "another person") {
		t.Error("the question no longer covers a day that belongs to somebody else")
	}
	// The distinction the owner actually cares about: a date written
	// down so it is not missed is not the same as a thing that will
	// happen without him. The first wordings collapsed the two and
	// every birthday came out silent.
	if !strings.Contains(strings.ToLower(coming.Asking), "not be missed") {
		t.Error("the question no longer separates a date written down from an appointment")
	}
}

// TestSettledStopsTheSecondWarning : Owner: *"'ok got it' means no
// need of that reminder any more, because I'm already on it."* A
// wedding warned about three days out, answered, must not warn
// again on the day.
func TestSettledStopsTheSecondWarning(t *testing.T) {
	j := coming.Judged{
		Starts: at.Add(48 * time.Hour),
		Leads:  []coming.Lead{{Before: 72 * time.Hour}, {Before: 2 * time.Hour}},
		Said:   1,
	}
	if j.Over(at) {
		t.Fatal("the second warning was already gone before anybody said anything")
	}
	j.Settled = at
	if !j.Over(at) {
		t.Error("they said they were on it and it still had something to say")
	}
	if _, due := j.Due(at.Add(47 * time.Hour)); !due {
		t.Error("Due is meant to say only what the clock says; Over is what settles it")
	}
}

// TestWhatIsLeftReplacesTheReason : Owner picked this over dropping
// the whole warning. A warning says "a present to buy and something
// to wear"; told the present is bought, the one nearer the time has
// to ask only for the shirt, and must not ask again for the
// present.
func TestWhatIsLeftReplacesTheReason(t *testing.T) {
	j := coming.Judged{
		Title: "Attending Vignesh's wedding", Starts: at.Add(2 * time.Hour),
		Leads: []coming.Lead{{Before: 2 * time.Hour, Why: "A present to buy and something to wear."}},
	}

	was := j.Sentence(0, at, kolkata)
	if !strings.Contains(was, "present to buy") {
		t.Fatalf("the first warning lost its reason: %q", was)
	}

	j.Still = "Something to wear."
	now := j.Sentence(0, at, kolkata)
	if strings.Contains(now, "present") {
		t.Errorf("it asked again for the present: %q", now)
	}
	if !strings.Contains(now, "Something to wear") {
		t.Errorf("it lost what is left: %q", now)
	}
	// Still part of the same thing: the title and the hour do not
	// change because half of it is done.
	if !strings.Contains(now, "Attending Vignesh's wedding") {
		t.Errorf("the sentence stopped saying what it is about: %q", now)
	}
}

// TestPartOfItDoneIsNotAllOfItDone : Trimming must not silence the
// warning; that is what settling is for.
func TestPartOfItDoneIsNotAllOfItDone(t *testing.T) {
	j := coming.Judged{
		Starts: at.Add(2 * time.Hour),
		Leads:  []coming.Lead{{Before: 2 * time.Hour}},
		Still:  "Something to wear.",
	}
	if j.Over(at) {
		t.Error("saying half of it was done dropped the whole warning")
	}
}
