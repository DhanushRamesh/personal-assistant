package greet_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/base/event"
	"personal-assistant/internal/domain/greet"
)

// india : The zone the hour is written in.
var india = time.FixedZone("IST", 5*3600+1800)

// evening : Half past six on a Friday.
var evening = time.Date(2026, 10, 2, 18, 30, 0, 0, india)

func happened(kind, value string, at time.Time) event.Event {
	p, _ := json.Marshal(map[string]string{"value": value})
	return event.Event{Kind: kind, Payload: p, OccurredAt: at}
}

// TestTheGapIsGivenInWords : It is what decides the greeting -- ten
// minutes is hello again and nine hours is somebody who has had a day.
func TestTheGapIsGivenInWords(t *testing.T) {
	got := greet.Prompt(greet.Told{Now: evening, Since: evening.Add(-9 * time.Hour)})
	if !strings.Contains(got, "9 hours ago") {
		t.Errorf("the gap is missing:\n%s", got)
	}
	if !strings.Contains(got, "Anything from before that") {
		t.Error("nothing stops it raising what was already discussed")
	}
}

// TestNeverSpokenToThemBefore : A first greeting has no gap to describe.
func TestNeverSpokenToThemBefore(t *testing.T) {
	got := greet.Prompt(greet.Told{Now: evening})
	if !strings.Contains(got, "not spoken to them before") {
		t.Errorf("the first greeting is not marked as one:\n%s", got)
	}
}

// TestRemindersMustAllBeSaid : The model writes them now instead of Go
// appending fixed text, so losing one is the risk that replaced it.
func TestRemindersMustAllBeSaid(t *testing.T) {
	got := greet.Prompt(greet.Told{
		Now:       evening,
		Reminders: []string{"you were going to call the plumber at four", "the milk"},
	})
	for _, want := range []string{
		"you were going to call the plumber at four", "the milk",
		"Say all of them", "leaving one out",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q from:\n%s", want, got)
		}
	}
}

// TestNoRemindersSaysNothingAboutThem : An empty heading invites the
// model to mention reminders it was not given.
func TestNoRemindersSaysNothingAboutThem(t *testing.T) {
	if got := greet.Prompt(greet.Told{Now: evening}); strings.Contains(got, "Say all of them") {
		t.Errorf("reminders are discussed with none to say:\n%s", got)
	}
}

// TestEventsAreGivenPlainlyAndNotInterpreted : What a kind means is the
// model's to work out, so a kind nobody wrote code for still arrives.
func TestEventsAreGivenPlainlyAndNotInterpreted(t *testing.T) {
	got := greet.Prompt(greet.Told{
		Now:   evening,
		Since: evening.Add(-3 * time.Hour),
		Events: []event.Event{
			happened("place.exited", "office", evening.Add(-40*time.Minute)),
			happened("call.missed", "Priya", evening.Add(-30*time.Minute)),
		},
	})
	for _, want := range []string{"place exited: office", "call missed: Priya", "5:50 pm", "6:00 pm"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q from:\n%s", want, got)
		}
	}
	// And it is told not to read them back.
	for _, want := range []string{"Never list what the devices reported", "One thing at most"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing the instruction %q", want)
		}
	}
}

// TestNothingHappenedIsSaidSo : Silence is an answer, and leaving the
// section out invites the model to imagine one.
func TestNothingHappenedIsSaidSo(t *testing.T) {
	if got := greet.Prompt(greet.Told{Now: evening}); !strings.Contains(got, "reported nothing in between") {
		t.Errorf("an empty window is not stated:\n%s", got)
	}
}

// TestOnlyTheNewestEventsAreGiven : This runs at the door, and the
// prompt must not grow with however much the phone learns to report.
func TestOnlyTheNewestEventsAreGiven(t *testing.T) {
	var evs []event.Event
	for i := range greet.Most + 50 {
		evs = append(evs, happened("network.joined", "net", evening.Add(-time.Duration(greet.Most+50-i)*time.Minute)))
	}
	got := greet.Prompt(greet.Told{Now: evening, Events: evs})
	if n := strings.Count(got, "network.joined"); n > greet.Most {
		t.Errorf("%d events given, at most %d expected", n, greet.Most)
	}
}

// TestAFailedModelStillGreets : Somebody is standing in the doorway.
func TestAFailedModelStillGreets(t *testing.T) {
	w := &greet.Writer{Ask: func(context.Context, string) (string, error) {
		return "", errors.New("the provider is down")
	}}
	said, written := w.Write(context.Background(), greet.Told{
		Now: evening, Opening: "Good evening, sir. Welcome back.",
		Reminders: []string{"you were going to call the plumber."},
	})
	if written {
		t.Error("a failure was reported as written")
	}
	if !strings.Contains(said, "Good evening, sir. Welcome back.") {
		t.Errorf("the fixed greeting was not said: %q", said)
	}
	if !strings.Contains(said, "plumber") {
		t.Errorf("the reminder was dropped on the fallback path: %q", said)
	}
}

// TestASlowModelStillGreets : The budget is a couple of seconds.
func TestASlowModelStillGreets(t *testing.T) {
	w := &greet.Writer{Within: 50 * time.Millisecond,
		Ask: func(ctx context.Context, _ string) (string, error) {
			<-ctx.Done()
			return "", ctx.Err()
		}}
	start := time.Now()
	said, written := w.Write(context.Background(), greet.Told{Now: evening, Opening: "Evening."})
	if written || said != "Evening." {
		t.Errorf("a slow model should fall back, got %q written=%v", said, written)
	}
	if time.Since(start) > time.Second {
		t.Errorf("waited %v for a greeting", time.Since(start))
	}
}

// TestAnEmptyAnswerIsNotSaid : A model that returns nothing has not
// greeted anybody, and silence at the door is the failure being avoided.
func TestAnEmptyAnswerIsNotSaid(t *testing.T) {
	w := &greet.Writer{Ask: func(context.Context, string) (string, error) { return "   ", nil }}
	said, written := w.Write(context.Background(), greet.Told{Now: evening, Opening: "Evening."})
	if written || said != "Evening." {
		t.Errorf("an empty answer was used: %q written=%v", said, written)
	}
}

// TestNoModelAtAllIsHowItWorkedBefore : A server with nothing configured
// says one of the fixed sentences, exactly as it used to.
func TestNoModelAtAllIsHowItWorkedBefore(t *testing.T) {
	var w *greet.Writer
	said, written := w.Write(context.Background(), greet.Told{Now: evening, Opening: "Evening."})
	if written || said != "Evening." {
		t.Errorf("got %q written=%v", said, written)
	}
}

// TestRepeatsAreFoldedWithTheirCount : The count is the signal, so a
// run is one line saying how many rather than ten lines or none.
func TestRepeatsAreFoldedWithTheirCount(t *testing.T) {
	var evs []event.Event
	for i := range 3 {
		evs = append(evs, happened("call.missed", "Priya", evening.Add(-time.Duration(30-i*5)*time.Minute)))
	}
	for i := range 6 {
		evs = append(evs, happened("network.joined", "Dhanush_EXT", evening.Add(-time.Duration(120-i*10)*time.Minute)))
	}
	got := greet.Prompt(greet.Told{Now: evening, Since: evening.Add(-3 * time.Hour), Events: evs})

	if n := strings.Count(got, "call missed: Priya"); n != 1 {
		t.Errorf("three calls should be one line, got %d", n)
	}
	if !strings.Contains(got, "(3 times, the last at") {
		t.Errorf("the count is missing:\n%s", got)
	}
	if n := strings.Count(got, "network joined: Dhanush_EXT"); n != 1 {
		t.Errorf("six reconnections should be one line, got %d", n)
	}
	if !strings.Contains(got, "(6 times, the last at") {
		t.Error("the reconnection count is missing")
	}
}

// TestSomethingOnceHasNoCount : "(1 times)" reads as a bug and invites
// the model to mention a number that means nothing.
func TestSomethingOnceHasNoCount(t *testing.T) {
	got := greet.Prompt(greet.Told{
		Now: evening, Events: []event.Event{happened("place.entered", "home", evening.Add(-time.Minute))},
	})
	if strings.Contains(got, "1 times") {
		t.Errorf("a single event was given a count:\n%s", got)
	}
}

// TestAFoldedRunKeepsItsPlaceInTheSequence : Ordered by when each thing
// first happened, so leaving then arriving still reads that way.
func TestAFoldedRunKeepsItsPlaceInTheSequence(t *testing.T) {
	got := greet.Prompt(greet.Told{
		Now: evening, Since: evening.Add(-3 * time.Hour),
		Events: []event.Event{
			happened("place.exited", "office", evening.Add(-60*time.Minute)),
			happened("network.left", "Zoho-Guest", evening.Add(-58*time.Minute)),
			happened("network.left", "Zoho-Guest", evening.Add(-50*time.Minute)),
			happened("place.entered", "home", evening.Add(-5*time.Minute)),
		},
	})
	left := strings.Index(got, "place exited: office")
	home := strings.Index(got, "place entered: home")
	if left < 0 || home < 0 || left > home {
		t.Errorf("leaving should come before arriving:\n%s", got)
	}
}

// TestWhatIsOrdinaryIsSaidSo : Without it the window reads as though
// everything in it were news -- a phone moving between a router and
// its extender inside one house produced "you have been moving about
// quite a bit" to somebody who had not left the building.
func TestWhatIsOrdinaryIsSaidSo(t *testing.T) {
	roam := happened("network.joined", "Dhanush_EXT", evening.Add(-2*time.Hour))
	call := happened("call.missed", "Priya", evening.Add(-30*time.Minute))

	got := greet.Prompt(greet.Told{
		Now: evening, Since: evening.Add(-3 * time.Hour),
		Events: []event.Event{roam, call},
		Usual:  map[string]int{greet.Label(roam): 84, greet.Label(call): 1},
	})

	if !strings.Contains(got, "[84 in the past four weeks]") {
		t.Errorf("the ordinary thing is not marked ordinary:\n%s", got)
	}
	if !strings.Contains(got, "[1 in the past four weeks]") {
		t.Errorf("the rare thing has no count:\n%s", got)
	}
	for _, want := range []string{"to tell you what is ordinary for them", "is the texture of their life and not news"} {
		if !strings.Contains(got, want) {
			t.Errorf("the prompt does not explain the number: %q", want)
		}
	}
}

// TestSomethingNeverSeenBeforeHasNoNumber : And is said to be worth a
// look for exactly that reason.
func TestSomethingNeverSeenBeforeHasNoNumber(t *testing.T) {
	got := greet.Prompt(greet.Told{
		Now:    evening,
		Events: []event.Event{happened("door.forced", "back", evening.Add(-time.Minute))},
		Usual:  map[string]int{},
	})
	if strings.Contains(got, "in the past four weeks]") {
		t.Errorf("a thing never seen was given a count:\n%s", got)
	}
	if !strings.Contains(got, "has not happened before") {
		t.Error("the prompt does not say an uncounted thing is worth a look")
	}
}

// TestLabelMatchesTheLine : Two spellings of one event is a count that
// never matches a line, which fails silently and reads as everything
// being unprecedented.
func TestLabelMatchesTheLine(t *testing.T) {
	e := happened("call.missed", "Priya", evening.Add(-time.Hour))
	got := greet.Prompt(greet.Told{Now: evening, Events: []event.Event{e},
		Usual: map[string]int{greet.Label(e): 7}})
	// Keyed on the label, printed in the readable form. The two
	// spellings have to stay in step: a count keyed on one and a
	// line printed in the other never meet, which fails silently and
	// reads as everything being unprecedented.
	if !strings.Contains(got, "call missed: Priya  [7 in the past four weeks]") {
		t.Errorf("the count did not land on its line:\n%s", got)
	}
	if greet.Label(e) != "call.missed Priya" {
		t.Errorf("the counting key changed with the wording: %q", greet.Label(e))
	}
}

// TestThePayloadCarriesMoreThanItsValue : A stay is a place and a
// number of minutes, and a call is a person and a length. Keying the
// count on value alone was right; throwing the rest away with it was
// not.
func TestThePayloadCarriesMoreThanItsValue(t *testing.T) {
	stay := event.Event{Kind: "place.stayed", OccurredAt: evening.Add(-9 * time.Hour),
		Payload: []byte(`{"value":"the office","minutes":501,"still":true}`)}
	call := event.Event{Kind: "call.ended", OccurredAt: evening.Add(-time.Hour),
		Payload: []byte(`{"value":"Priya","minutes":42}`)}

	got := greet.Prompt(greet.Told{Now: evening, Since: evening.Add(-10 * time.Hour),
		Events: []event.Event{stay, call}})

	for _, want := range []string{
		// A stay is a span now rather than a line of fields, because
		// it re-reports itself every five minutes while it is open.
		"stayed at the office, reported as 8 hours",
		"call ended: Priya  (minutes 42)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q from:\n%s", want, got)
		}
	}
	// And the count is still keyed on the value alone, or a call of a
	// different length would be a different thing every time.
	if greet.Label(call) != "call.ended Priya" {
		t.Errorf("the label picked up more than the value: %q", greet.Label(call))
	}
}

// TestAStrangerIsCountedByNumberAndNeverReadOut : The label keeps the
// number so one stranger can be told from another; the line says
// "an unknown number" because nobody asked to hear eleven digits.
func TestAStrangerIsCountedByNumberAndNeverReadOut(t *testing.T) {
	var evs []event.Event
	for i := range 4 {
		evs = append(evs, happened("call.missed", "+919876543210",
			evening.Add(-time.Duration(40-i*5)*time.Minute)))
	}
	evs = append(evs, happened("call.missed", "Priya", evening.Add(-10*time.Minute)))

	got := greet.Prompt(greet.Told{Now: evening, Since: evening.Add(-2 * time.Hour), Events: evs})

	if strings.Contains(got, "9876543210") {
		t.Errorf("the number was read out:\n%s", got)
	}
	if !strings.Contains(got, "call missed: an unknown number  (4 times") {
		t.Errorf("four calls from one stranger should be one line with a count:\n%s", got)
	}
	if !strings.Contains(got, "call missed: Priya") {
		t.Error("a known caller should still be named")
	}
	// And the count is still keyed on the number, so two strangers stay
	// two things.
	if greet.Label(evs[0]) == greet.Label(evs[4]) {
		t.Error("a stranger and a known caller share a label")
	}
}

// TestANumberDoesNotLeakThroughAnotherField : Readable masks the
// value; a call carries the number beside the name, and without this
// the digits reach the prompt through the field the masking does not
// look at.
func TestANumberDoesNotLeakThroughAnotherField(t *testing.T) {
	call := event.Event{Kind: "call.made", OccurredAt: evening.Add(-time.Hour),
		Payload: []byte(`{"value":"Alekhya Chintada","number":"+919182053794","seconds":21}`)}

	got := greet.Prompt(greet.Told{Now: evening, Since: evening.Add(-3 * time.Hour),
		Events: []event.Event{call}})

	if strings.Contains(got, "9182053794") {
		t.Errorf("the number reached the prompt:\n%s", got)
	}
	if !strings.Contains(got, "call made: Alekhya Chintada") {
		t.Error("the name should still be there")
	}
	// As words. A sender writing seconds has said it is a span, and
	// twenty-one of them is brief and nothing more.
	if strings.Contains(got, "21") {
		t.Errorf("the exact count of seconds reached the prompt:\n%s", got)
	}
	if !strings.Contains(got, "brief") {
		t.Error("the duration should still be there, in words")
	}
}

// TestWhatIsComingIsOfferedButNotRecited : Sixteen open tasks is a
// normal number to have and a terrible thing to be told at a door.
func TestWhatIsComingIsOfferedButNotRecited(t *testing.T) {
	tomorrow := evening.Add(13 * time.Hour)
	got := greet.Prompt(greet.Told{
		Now: evening,
		Diary: []greet.Appointment{
			{What: "Standup", When: tomorrow, Where: "the office"},
		},
		Chores: greet.Chores{
			Due: []greet.Chore{
				{What: "renew the insurance", By: evening.Add(-48 * time.Hour), Overdue: true},
			},
			Others: 15,
		},
	})

	if !strings.Contains(got, "tomorrow 7:30 am: Standup, at the office") {
		t.Errorf("the appointment is missing or misread:\n%s", got)
	}
	if !strings.Contains(got, "renew the insurance") || !strings.Contains(got, "not done") {
		t.Errorf("the overdue task is missing:\n%s", got)
	}
	// The count is given to judge by and told not to be said.
	if !strings.Contains(got, "15 other things") {
		t.Error("the count of the rest is missing")
	}
	for _, want := range []string{"Say at most one of these", "never say how many there are"} {
		if !strings.Contains(got, want) {
			t.Errorf("the prompt does not say %q", want)
		}
	}
}

// TestNothingComingSaysNothingAtAll : An empty heading invites the
// model to mention a diary it was not given.
func TestNothingComingSaysNothingAtAll(t *testing.T) {
	got := greet.Prompt(greet.Told{Now: evening})
	if strings.Contains(got, "What is coming") {
		t.Errorf("an empty diary was discussed:\n%s", got)
	}
}

// TestAnAllDayThingIsNotGivenAnHour : "Meganadham's birthday today at
// midnight" is a sentence about a bug.
func TestAnAllDayThingIsNotGivenAnHour(t *testing.T) {
	got := greet.Prompt(greet.Told{
		Now: evening,
		Diary: []greet.Appointment{
			{What: "Deepavali", When: evening.Add(20 * time.Hour), AllDay: true},
		},
	})
	if !strings.Contains(got, "tomorrow all day: Deepavali") {
		t.Errorf("an all-day event should not carry an hour:\n%s", got)
	}
	if strings.Contains(got, "12:00 am") {
		t.Error("midnight was read out")
	}
}

// TestItIsToldWhatTheyHadPlanned : Owner: *"it knows I go to a
// movie in the calendar, then it knows my location. When I come
// back it should cross-check whether I went there, and ask me how
// was the movie."*
//
// It could not, and not for want of thinking: the diary it was
// given ran from now forwards, so it saw where they had been and
// never what they had meant to do. Nothing here does the checking.
func TestItIsToldWhatTheyHadPlanned(t *testing.T) {
	ist := time.FixedZone("IST", 5*3600+1800)
	now := time.Date(2026, 10, 4, 21, 10, 0, 0, ist)

	got := greet.Prompt(greet.Told{
		Now: now, Since: now.Add(-5 * time.Hour),
		Were: []greet.Appointment{{
			What: "Meesaya Murukku 2 Movie", When: now.Add(-3 * time.Hour),
			Where: "Seven Screen's Cinemas, Kilambakkam"}},
	})

	for _, want := range []string{
		"Meesaya Murukku 2 Movie", "Seven Screen's Cinemas",
		"Compare it with where they actually were",
		"do not require the names to match",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q", want)
		}
	}
	// A plan is not proof it happened. Measured live: with this line
	// the film is asked about by name two times out of two, and
	// without the surrounding caution a dentist appointment in
	// Velachery was asked about while they sat in Urapakkam.
	if !strings.Contains(got, "not evidence that it happened") {
		t.Error("a plan may be read as a thing that happened")
	}
}

// TestNothingPlannedAddsNothing : Most evenings there is nothing in
// the diary, and an empty heading is a question the model will try
// to answer.
func TestNothingPlannedAddsNothing(t *testing.T) {
	got := greet.Prompt(greet.Told{Now: time.Now(), Were: nil})
	if strings.Contains(got, "was in their diary for the time they were away") {
		t.Error("an empty section was written anyway")
	}
}

// TestTheOpeningIsAlwaysSaid : Owner, after a greeting that began
// "Kliffy rang while you were out, sir": "it should say the good
// morning / afternoon / evening and welcome backs at the beginning."
//
// The model is told the opening has been said and asked for what
// follows; the opening itself is put in front of the answer here, so
// that a model which ignores the instruction cannot drop it.
func TestTheOpeningIsAlwaysSaid(t *testing.T) {
	w := &greet.Writer{Ask: func(context.Context, string) (string, error) {
		return "Kliffy rang while you were out, sir.", nil
	}}
	said, written := w.Write(context.Background(), greet.Told{
		Now: evening, Opening: "Good evening, sir. Welcome back.",
		Events: []event.Event{happened("call.missed", "unknown", evening.Add(-time.Minute))},
	})
	if !written {
		t.Fatal("the model wrote one and it was not used")
	}
	want := "Good evening, sir. Welcome back. Kliffy rang while you were out, sir."
	if said != want {
		t.Errorf("got %q, want %q", said, want)
	}
}

// TestNobodyIsGreetedTwice : And the other way. A model that writes
// its own salutation anyway must not produce two of them out loud.
func TestNobodyIsGreetedTwice(t *testing.T) {
	for _, answer := range []string{
		"Good evening, sir. Kliffy rang while you were out.",
		"Evening, sir -- Kliffy rang while you were out.",
		"Welcome back. Kliffy rang while you were out.",
		"Good evening, sir. Welcome back. Kliffy rang while you were out.",
	} {
		w := &greet.Writer{Ask: func(context.Context, string) (string, error) {
			return answer, nil
		}}
		said, _ := w.Write(context.Background(), greet.Told{
			Now: evening, Opening: "Good evening, sir. Welcome back.",
			Events: []event.Event{happened("call.missed", "unknown", evening.Add(-time.Minute))},
		})
		want := "Good evening, sir. Welcome back. Kliffy rang while you were out."
		if said != want {
			t.Errorf("from %q\n got %q\nwant %q", answer, said, want)
		}
	}
}

// TestTheModelIsToldNotToGreet : The instruction, so that the
// stripping above is a guard rather than the mechanism.
func TestTheModelIsToldNotToGreet(t *testing.T) {
	asked := greet.Prompt(greet.Told{Now: evening, Opening: "Good evening, sir. Welcome back."})
	if !strings.Contains(asked, "Good evening, sir. Welcome back.") {
		t.Error("the model was not told what had already been said")
	}
	if !strings.Contains(asked, "Do not greet them again") {
		t.Error("the model was not told to write only what follows")
	}
}

// TestTheEvidenceHasThreeDistances : The scale the judgement is made
// on. Recorded outright may be said; several things pointing the same
// way may be asked about; one ambiguous thing is silence.
//
// Wired as well as written: a block that is not in Prompt is a rule
// that can never run, and this file has been fooled by one before.
func TestTheEvidenceHasThreeDistances(t *testing.T) {
	asked := greet.Prompt(greet.Told{Now: evening, Opening: "Evening, sir. Welcome back."})
	for _, want := range []string{
		"three distances",
		"you may simply say it",
		"You may not state it",
		"do not say that you are unsure either",
	} {
		if !strings.Contains(asked, want) {
			t.Errorf("the confidence ladder is not in the prompt: %q missing", want)
		}
	}
}

// TestEvidenceIsNotASubject : Most of what a phone reports is how you
// work out the one thing worth saying, not the thing itself. Stated
// as a test anybody can apply, because a list of kinds is wrong the
// day a new sender appears.
func TestEvidenceIsNotASubject(t *testing.T) {
	asked := greet.Prompt(greet.Told{Now: evening, Opening: "Evening, sir. Welcome back."})
	if !strings.Contains(asked, "evidence rather than a subject") {
		t.Error("the model is not told that most of what it is given is not a topic")
	}
	// And it must stay general. Naming kinds here is the mistake the
	// file records three times over.
	for _, kind := range []string{"unlocked", "wifi", "Wi-Fi", "bluetooth", "Bluetooth", "battery"} {
		if strings.Contains(asked, kind) {
			t.Errorf("the prompt names a kind of event (%q); it must not", kind)
		}
	}
}

// TestNothingProvesAnAbsence : A device says where it was and nothing
// at all about where it was not. Owner's barber case: the diary said
// nine, the phone says home all morning, and that is not proof.
func TestNothingProvesAnAbsence(t *testing.T) {
	told := greet.Told{
		Now: evening, Opening: "Evening, sir. Welcome back.",
		Were: []greet.Appointment{{What: "Barber", When: evening.Add(-10 * time.Hour)}},
	}
	asked := greet.Prompt(told)
	for _, want := range []string{
		"says nothing whatever about where it was not",
		"never conclude that they did not go",
		"you may ask whether they got to it at all",
	} {
		if !strings.Contains(asked, want) {
			t.Errorf("the absence rule is not in the prompt: %q missing", want)
		}
	}
}

// TestAnUnvouchedNameIsSomebody : "Kliffy" came off a phone's contact
// list and may be a nickname, a mishearing or the wrong person. A
// wrong name is not a hedge; it is a different person.
func TestAnUnvouchedNameIsSomebody(t *testing.T) {
	asked := greet.Prompt(greet.Told{Now: evening, Opening: "Evening, sir. Welcome back."})
	if !strings.Contains(asked, "Say somebody rather than a name you cannot vouch for") {
		t.Error("the model is not told to fall back to somebody")
	}
}

// TestNothingToWriteFromIsNotAsked : A prompt with nothing under it
// is a page about finding something worth saying, handed to
// something obliging. The one sampled arrival with an empty window
// answered it with an invented missed call.
func TestNothingToWriteFromIsNotAsked(t *testing.T) {
	var asked int
	w := &greet.Writer{Ask: func(context.Context, string) (string, error) {
		asked++
		return "Somebody tried to reach you, sir.", nil
	}}
	said, written := w.Write(context.Background(), greet.Told{
		Now: evening, Opening: "Good evening, sir. Welcome back.",
	})
	if asked != 0 {
		t.Errorf("asked a model about an empty window %d times", asked)
	}
	if written || said != "Good evening, sir. Welcome back." {
		t.Errorf("got %q written=%v", said, written)
	}
}

// TestOnlyWhatWasLeftHangingIsRaised : The block saying somebody
// tried to reach them used to appear whenever the window mentioned
// that sort of thing at all, so an answered call got raised as
// though it were owed. Owner's rulings: a call returned eight
// minutes later is settled and is nothing.
func TestOnlyWhatWasLeftHangingIsRaised(t *testing.T) {
	reaching := func(kind, who string, secs int, at time.Time) event.Event {
		p := fmt.Sprintf(`{"value":%q,"number":"+919000000001"}`, who)
		if secs >= 0 {
			p = fmt.Sprintf(`{"value":%q,"number":"+919000000001","seconds":%d}`, who, secs)
		}
		return event.Event{Kind: kind, Payload: []byte(p), OccurredAt: at}
	}
	base := greet.Told{Now: evening, Opening: "Evening, sir. Welcome back."}

	// Nothing of the sort in the window at all.
	quiet := base
	quiet.Events = []event.Event{happened("wifi.joined", "Dhanush", evening.Add(-time.Hour))}
	if strings.Contains(greet.Prompt(quiet), "did not get through") {
		t.Error("a window with nothing of the sort was told about it")
	}

	// Tried and never got through.
	open := base
	open.Events = []event.Event{reaching("call.missed", "Priya", -1, evening.Add(-time.Hour))}
	if got := greet.Outstanding(open); len(got) != 1 {
		t.Errorf("a call nobody answered was not left hanging: %v", got)
	}
	if !strings.Contains(greet.Prompt(open), "did not get through") {
		t.Error("what was left hanging was not put to the model")
	}

	// Tried, then spoke. Settled.
	back := base
	back.Events = []event.Event{
		reaching("call.missed", "Priya", -1, evening.Add(-time.Hour)),
		reaching("call.made", "Priya", 21, evening.Add(-52*time.Minute)),
	}
	if got := greet.Outstanding(back); len(got) != 0 {
		t.Errorf("a call that was returned is still hanging: %v", got)
	}
	asked := greet.Prompt(back)
	if strings.Contains(asked, "did not get through") {
		t.Error("a settled call was raised as though it were owed")
	}
	if !strings.Contains(asked, "These got through") {
		t.Error("the model was not told what got through")
	}
	// By name and hour, not as a sentence about the window. Told
	// only that nothing was owed, it stopped asking whether somebody
	// had reached them and started asking how the call went.
	if got := greet.Settled(back); len(got) != 1 || !strings.Contains(got[0], "Priya") {
		t.Errorf("the call that got through was not named: %v", got)
	}

	// Answered when it came, and never a missed one at all.
	spoke := base
	spoke.Events = []event.Event{reaching("call.received", "Amma", 19, evening.Add(-time.Hour))}
	if got := greet.Outstanding(spoke); len(got) != 0 {
		t.Errorf("an answered call was treated as an attempt: %v", got)
	}

	// A zero is not a time.
	none := base
	none.Events = []event.Event{reaching("call.unanswered", "Muthu", 0, evening.Add(-time.Hour))}
	if got := greet.Outstanding(none); len(got) != 1 {
		t.Errorf("a call of no length was treated as having happened: %v", got)
	}

	// A caller with no name is not named.
	nameless := base
	nameless.Events = []event.Event{reaching("call.missed", "unknown", -1, evening.Add(-time.Hour))}
	if got := greet.Outstanding(nameless); len(got) != 1 ||
		!strings.Contains(got[0], "could not name") {
		t.Errorf("a nameless caller was given a name: %v", got)
	}
}

// TestTheStayAndTheMusicReachTheModel : Owner, looking at a window
// that had neither: "stay should reach and also the music, only
// then it can know whether I went to movie or not."
//
// They are the two that answer it. Where somebody sat for three
// hours says whether they went; music through the same hours says
// they did not. Both used to be filtered out of the greeting
// entirely because of how many rows they are, which is true of the
// rows and not of the facts.
func TestTheStayAndTheMusicReachTheModel(t *testing.T) {
	at := func(h, m int) time.Time {
		return time.Date(evening.Year(), evening.Month(), evening.Day(), h, m, 0, 0, evening.Location())
	}
	evs := []event.Event{
		{Kind: "place.exited", OccurredAt: at(17, 57), Payload: []byte(`{"value":"home"}`)},
	}
	// A stay that said so every five minutes for three hours, the way
	// an open one does.
	for m := 0; m <= 170; m += 5 {
		evs = append(evs, event.Event{Kind: "place.stayed", OccurredAt: at(18, 5).Add(time.Duration(m) * time.Minute),
			Payload: []byte(fmt.Sprintf(`{"value":"Flavors non veg restaurant","minutes":%d}`, m))})
	}
	for m := 0; m < 40; m += 3 {
		evs = append(evs, event.Event{Kind: "music.state", OccurredAt: at(17, 47).Add(time.Duration(m) * time.Minute),
			Payload: []byte(`{"value":"Daft Punk","track":"Face to Face","playing":"true"}`)})
	}

	got := greet.Prompt(greet.Told{Now: at(21, 25), Since: at(16, 25), Events: evs})

	if !strings.Contains(got, "stayed at Flavors non veg restaurant, reported as 2 hours 50 minutes") {
		t.Errorf("the stay did not reach the model as one span:\n%s", got)
	}
	if !strings.Contains(got, "listening until") || !strings.Contains(got, "Daft Punk") {
		t.Error("the music did not reach the model")
	}
	// One line each, not thirty-five and fourteen.
	if n := strings.Count(got, "stayed at Flavors"); n != 1 {
		t.Errorf("the stay was written out %d times", n)
	}
	if n := strings.Count(got, "listening until"); n != 1 {
		t.Errorf("the listening was written out %d times", n)
	}
}
