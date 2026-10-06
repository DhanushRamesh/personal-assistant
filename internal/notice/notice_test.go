package notice_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/event"
	"github.com/DhanushRamesh/personal-assistant/internal/notice"
)

var at = time.Date(2026, 10, 4, 21, 0, 0, 0, time.UTC)

// happened : One thing a sender reported and did not mark.
func happened(kind, value string, ago time.Duration) event.Event {
	payload, _ := json.Marshal(map[string]string{"value": value})
	return event.Event{Kind: kind, Payload: payload, OccurredAt: at.Add(-ago)}
}

// marked : One thing the rules below ask to be announced.
func marked(kind, value string, ago time.Duration) event.Event {
	return happened(kind, value, ago)
}

// asked : Rules naming exactly the labels a test uses.
func wants(labels ...string) []notice.Rule { return notice.ParseRules(labels) }

// TestAPositionReadingIsNeverMentioned : They arrive every few
// minutes, none of them is a thing to say a sentence about, and a
// sender that marked one anyway is a sender with a bug.
func TestAPositionReadingIsNeverMentioned(t *testing.T) {
	arrived := []event.Event{marked(event.Fixed, "12.91100,80.06241", 0)}
	rules := wants(event.Fixed + " 12.91100,80.06241")
	if got := notice.Find(rules, arrived, at); len(got) != 0 {
		t.Errorf("a coordinate was offered as news: %+v", got)
	}
}

// TestNothingUnmarkedIsEverSaid : The whole of the change of 5
// October 2026. Owner, looking at "Sir, your phone has been
// unlocked three times in the last few minutes": *"no need for
// these kind of unasked announcement."*
//
// Six of a thing in ten minutes, and a thing never reported before:
// the two things this used to speak about unprompted. Neither is
// marked, so neither is said.
func TestNothingUnmarkedIsEverSaid(t *testing.T) {
	var arrived []event.Event
	for i := 0; i < 6; i++ {
		arrived = append(arrived, happened("screen.unlocked", "", time.Duration(i)*90*time.Second))
	}
	arrived = append(arrived, happened("something.new", "never seen", 0))

	if got := notice.Find(nil, arrived, at); len(got) != 0 {
		t.Errorf("it judged something worth saying on its own: %+v", got)
	}
}

// TestTheTrackReachesTheRemark : A label is the kind and the value
// and nothing else, so that two of a thing are counted as two of the
// same thing. A player reports the artist as the value and the track
// beside it, and a remark about music that cannot name the song is
// not worth making.
func TestTheTrackReachesTheRemark(t *testing.T) {
	payload, _ := json.Marshal(map[string]string{
		"value": "Yuvan Shankar Raja", "track": "Poongatre Poongatre", "via": "spotify",
	})
	fresh := event.Event{Kind: event.State, Payload: payload, OccurredAt: at}

	got := notice.Find(wants("music.state Yuvan Shankar Raja"), []event.Event{fresh}, at)
	if len(got) != 1 {
		t.Fatalf("found %d, want 1", len(got))
	}
	if got[0].Label != "music.state Yuvan Shankar Raja" {
		t.Errorf("the track got into the label, which is counted: %q", got[0].Label)
	}
	if !strings.Contains(got[0].Detail, "Poongatre Poongatre") {
		t.Errorf("the track is not available to say: %q", got[0].Detail)
	}
	if !strings.Contains(notice.Remark(got[0], at), "Poongatre Poongatre") {
		t.Error("the track did not reach the prompt")
	}
}

// TestNothingIsSaidToAnEmptyRoom : An assistant that cannot tell
// whether anybody is there talks to the wall.
func TestNothingIsSaidToAnEmptyRoom(t *testing.T) {
	out := &notice.Gate{Away: func(string) (bool, bool) { return true, true }}
	if ok, _ := out.Allow("usr_1"); ok {
		t.Error("spoke to somebody who is out")
	}
	unknown := &notice.Gate{Away: func(string) (bool, bool) { return false, false }}
	if ok, _ := unknown.Allow("usr_1"); ok {
		t.Error("spoke when it could not tell whether anybody was there")
	}
	nobodyWatching := &notice.Gate{}
	if ok, _ := nobodyWatching.Allow("usr_1"); ok {
		t.Error("spoke with nothing watching the room at all")
	}
	// The room is the only thing left that can hold an announcement
	// back. Owner: "event arrives -> check if present -> announce."
	// The phone shows its own battery; announcing it to nobody is
	// not announcing.
	if ok, _ := out.Allow("usr_1"); ok {
		t.Error("an announcement was made to an empty room")
	}
}

// recorded : An event store holding a fixed set.
type recorded struct{ held []event.Event }

func (r recorded) Recent(_ context.Context, _ string, _ event.Query) ([]event.Event, error) {
	return r.held, nil
}

// TestTheModelMayAnswerWithNothing : The prompt forbids it, so this
// should never happen. Kept because the cost of reading a refusal
// as an announcement is that the refusal is spoken aloud.
func TestTheModelMayAnswerWithNothing(t *testing.T) {
	var spoke []string
	w := &notice.Watcher{
		Rules:  wants("battery.level 5"),
		Events: recorded{held: []event.Event{marked("battery.level", "5", 0)}},
		Gate:   &notice.Gate{Away: func(string) (bool, bool) { return false, true }},
		Ask:    func(context.Context, string, string) (string, error) { return notice.Silence, nil },
		Say: func(_ context.Context, _, text string, _ bool) error {
			spoke = append(spoke, text)
			return nil
		},
		Now: func() time.Time { return at },
	}

	said, err := w.Look(context.Background(), "usr_1")
	if err != nil {
		t.Fatalf("look: %v", err)
	}
	if said != "" || len(spoke) != 0 {
		t.Errorf("a refusal was read out loud: %q %v", said, spoke)
	}
}

// TestAnAnnouncementIsSpoken : And the other way: when the model has
// written it, it reaches the person.
func TestAnAnnouncementIsSpoken(t *testing.T) {
	var spoke []string
	w := &notice.Watcher{
		Rules:  wants("battery.level 5"),
		Events: recorded{held: []event.Event{marked("battery.level", "5", 0)}},
		Gate:   &notice.Gate{Away: func(string) (bool, bool) { return false, true }},
		Ask: func(context.Context, string, string) (string, error) {
			return "Five per cent, sir.", nil
		},
		Say: func(_ context.Context, _, text string, _ bool) error {
			spoke = append(spoke, text)
			return nil
		},
		Now: func() time.Time { return at },
	}

	if _, err := w.Look(context.Background(), "usr_1"); err != nil {
		t.Fatalf("look: %v", err)
	}
	if len(spoke) != 1 || spoke[0] != "Five per cent, sir." {
		t.Errorf("the announcement did not reach them: %v", spoke)
	}
}

// TestOneAnnouncementPerLook : Two things to announce in the same
// moment are two announcements, and the second is found again on
// the next tick thirty seconds later.
func TestOneAnnouncementPerLook(t *testing.T) {
	var asked int
	w := &notice.Watcher{
		Rules: wants("battery.level 5"),
		Events: recorded{held: []event.Event{
			marked("battery.level", "5", 0),
			marked("device.hot", "pixel 7", 0),
		}},
		Gate: &notice.Gate{Away: func(string) (bool, bool) { return false, true }},
		Ask: func(context.Context, string, string) (string, error) {
			asked++
			return "Something, sir.", nil
		},
		Say: func(context.Context, string, string, bool) error { return nil },
		Now: func() time.Time { return at },
	}

	if _, err := w.Look(context.Background(), "usr_1"); err != nil {
		t.Fatalf("look: %v", err)
	}
	if asked != 1 {
		t.Errorf("asked the model %d times for one moment, want 1", asked)
	}
}

// TestNothingArrivedCostsNothing : Most ticks. No model is asked and
// no event store is troubled beyond the one read.
func TestNothingArrivedCostsNothing(t *testing.T) {
	var asked int
	w := &notice.Watcher{
		Rules:  wants("battery.level 5"),
		Events: recorded{held: []event.Event{marked("battery.level", "5", 3*time.Hour)}},
		Gate:   &notice.Gate{Away: func(string) (bool, bool) { return false, true }},
		Ask: func(context.Context, string, string) (string, error) {
			asked++
			return "Something, sir.", nil
		},
		Say: func(context.Context, string, string, bool) error { return nil },
		Now: func() time.Time { return at },
	}

	if _, err := w.Look(context.Background(), "usr_1"); err != nil {
		t.Fatalf("look: %v", err)
	}
	if asked != 0 {
		t.Errorf("asked a model about a quiet five minutes %d times", asked)
	}
}

// TestARefusalIsRecognisedHoweverItIsWritten : A model asked for
// nothing produces something anyway unless it is given something to
// return, and whatever it returns must not be spoken.
func TestARefusalIsRecognisedHoweverItIsWritten(t *testing.T) {
	for _, said := range []string{"NOTHING", "nothing", " Nothing. ", "\"NOTHING\""} {
		if !notice.Nothing(said) {
			t.Errorf("a refusal written as %q would have been spoken", said)
		}
	}
	for _, said := range []string{"Nothing much has happened, sir.", "That is a new one, sir."} {
		if notice.Nothing(said) {
			t.Errorf("a real remark was thrown away: %q", said)
		}
	}
}

// TestEachThingIsWeighedOnce : A candidate stays inside the
// look-back window for five minutes and the watcher ticks every
// thirty seconds, so one song was being recorded ten times over as
// held.
func TestEachThingIsWeighedOnce(t *testing.T) {
	g := &notice.Gate{Away: func(string) (bool, bool) { return false, true }}
	c := notice.Candidate{Label: "music.state Stevie Wonder", Reason: notice.Immediate, Immediate: true, At: at}

	if g.Considered(c, at) {
		t.Fatal("the first look was treated as a repeat")
	}
	for _, later := range []time.Duration{30 * time.Second, 2 * time.Minute, 5 * time.Minute} {
		if !g.Considered(c, at.Add(later)) {
			t.Errorf("weighed again after %s", later)
		}
	}
	// But not for ever: the same thing tomorrow is news again.
	if g.Considered(c, at.Add(notice.Weighing+time.Minute)) {
		t.Error("still refusing to weigh it after the window had passed")
	}
}

// urgently : One thing the rules ask to be said at once.
func urgently(kind, value string, more map[string]string) event.Event {
	fields := map[string]string{"value": value}
	for k, v := range more {
		fields[k] = v
	}
	payload, _ := json.Marshal(fields)
	return event.Event{Kind: kind, Payload: payload, OccurredAt: at}
}

// TestADeviceCanInsist : Owner: "there are some definitive unasked
// announcements -- if my battery goes below 25, 10, 5 per cent it
// should say no matter what, and when the phone is getting too hot
// it should say."
//
// Which level is worth a warning is the person's own preference and
// belongs on the device that knows it. The sender marks the event;
// nothing here knows what a battery is.
func TestADeviceCanInsist(t *testing.T) {
	// Familiar, frequent, and in no way unusual. The mark is the
	// only thing that makes it a candidate, and the only thing that
	// ever does.
	arrived := []event.Event{
		happened("battery.level", "26", time.Minute),
		urgently("battery.level", "25", nil),
	}

	got := notice.Find(wants("battery.level 25 falling"), arrived, at)
	if len(got) != 1 {
		t.Fatalf("a reading the rules ask for was not heard: %+v", got)
	}
	if got[0].Reason != notice.Immediate || !got[0].Immediate {
		t.Errorf("reason = %q urgent = %v, want told and urgent", got[0].Reason, got[0].Immediate)
	}
}

// TestAnImmediateAnnouncementRepeatsItself : Owner: "if 100 is
// reached, it announces, I plug out, it becomes 99 in 5 secs and I
// plug back in it becomes 100 in 10 secs -- it has to announce
// again." The same label at a different moment is a different
// announcement.
func TestAnImmediateAnnouncementRepeatsItself(t *testing.T) {
	g := &notice.Gate{Away: func(string) (bool, bool) { return false, true }}
	first := notice.Candidate{Label: "battery.level 100", Reason: notice.Immediate, Immediate: true, At: at}
	again := first
	again.At = at.Add(10 * time.Second)

	if g.Considered(first, at) {
		t.Fatal("the first announcement was weighed before it arrived")
	}
	if ok, why := g.Allow("usr_1"); !ok {
		t.Fatalf("the first announcement was refused: %s", why)
	}
	// The very same arrival, found again on the next tick, is one
	// moment and is not said twice.
	if !g.Considered(first, at.Add(30*time.Second)) {
		t.Error("one arrival was weighed twice")
	}
	// Reaching it again ten seconds later is a second arrival.
	if g.Considered(again, again.At) {
		t.Error("the same level reached again was taken for the first")
	}
	if ok, why := g.Allow("usr_1"); !ok {
		t.Errorf("the same level reached again was refused: %s", why)
	}
}

// TestTheModelIsNotAskedToDecline : The refusal is the whole point
// of asking a model about a remark, and it is exactly wrong about a
// thing a device said has to be known. With the remarks gone, it is
// wrong every time.
func TestTheModelIsNotAskedToDecline(t *testing.T) {
	warning := notice.Candidate{Label: "battery.level 5", Reason: notice.Immediate, Immediate: true, At: at}
	asked := notice.Remark(warning, at)

	if !strings.Contains(asked, "has been made") {
		t.Error("the prompt still invites the model to weigh it")
	}
	if !strings.Contains(asked, "do not answer with "+notice.Silence) {
		t.Error("the prompt does not forbid the refusal")
	}
}

// TestAReadingCanStillInsist : Which is the whole point of the
// rules: a battery at five per cent is not unusual and is still
// worth saying.
func TestAReadingCanStillInsist(t *testing.T) {
	fresh := []event.Event{urgently("battery.level", "5", nil)}
	got := notice.Find(wants("battery.level 5"), fresh, at)
	if len(got) != 1 || got[0].Reason != notice.Immediate {
		t.Fatalf("a device insisting about a reading was not heard: %+v", got)
	}
}

// TestTwoWarningsAreBothSaid : Owner: "if it's urgent it should say
// every time, regardless of twenty minutes since the last and twice
// within ten minutes -- if an urgent event is captured, say it."
//
// Two warnings a minute apart are two events. Nothing about the
// first makes the second less worth hearing.
func TestTwoWarningsAreBothSaid(t *testing.T) {
	g := &notice.Gate{Away: func(string) (bool, bool) { return false, true }}
	first := notice.Candidate{Label: "battery.level 10", Reason: notice.Immediate, Immediate: true, At: at}
	second := notice.Candidate{Label: "battery.level 5", Reason: notice.Immediate, Immediate: true, At: at.Add(time.Minute)}

	if g.Considered(first, at) {
		t.Fatal("the first warning was treated as a repeat")
	}
	if g.Considered(second, at.Add(time.Minute)) {
		t.Error("a second warning a minute later was held back")
	}
	// Even the same warning again, if the device reports it afresh.
	again := notice.Candidate{Label: "battery.level 5", Reason: notice.Immediate, Immediate: true, At: at.Add(2 * time.Minute)}
	if g.Considered(again, at.Add(2*time.Minute)) {
		t.Error("the same warning reported again was held back")
	}
}

// TestOneWarningIsNotSaidTenTimes : The watcher re-reads the last
// five minutes every thirty seconds. Without this, one warning is
// found ten times and said ten times.
func TestOneWarningIsNotSaidTenTimes(t *testing.T) {
	g := &notice.Gate{Away: func(string) (bool, bool) { return false, true }}
	warning := notice.Candidate{Label: "battery.level 5", Reason: notice.Immediate, Immediate: true, At: at}

	if g.Considered(warning, at) {
		t.Fatal("the first look was treated as a repeat")
	}
	for _, later := range []time.Duration{30 * time.Second, 2 * time.Minute, 5 * time.Minute} {
		if !g.Considered(warning, at.Add(later)) {
			t.Errorf("the same moment was weighed again after %s", later)
		}
	}
}

// TestAProviderHiccupDoesNotSwallowAWarning : At five past midnight
// on 6 October a battery at twenty per cent, marked by the phone to
// be said at once, was found, allowed, and lost: the ask failed
// because the provider was busy, and the gate had already marked the
// arrival weighed. It was never tried again, and the window it could
// be found in is shorter than the mark lasts.
//
// The one class of announcement that exists because waiting costs
// something must not be lost to a hiccup lasting seconds.
func TestAProviderHiccupDoesNotSwallowAWarning(t *testing.T) {
	var asked int
	var spoke []string
	w := &notice.Watcher{
		Rules:  wants("battery.level 20"),
		Events: recorded{held: []event.Event{marked("battery.level", "20", 0)}},
		Gate:   &notice.Gate{Away: func(string) (bool, bool) { return false, true }},
		Ask: func(context.Context, string, string) (string, error) {
			asked++
			if asked == 1 {
				return "", errors.New("the provider is busy")
			}
			return "Twenty per cent, sir.", nil
		},
		Say: func(_ context.Context, _, text string, _ bool) error {
			spoke = append(spoke, text)
			return nil
		},
		Now: func() time.Time { return at },
	}

	if _, err := w.Look(context.Background(), "usr_1"); err == nil {
		t.Fatal("a failed ask was reported as a success")
	}
	if len(spoke) != 0 {
		t.Fatalf("said something despite the failure: %v", spoke)
	}

	// The next look, thirty seconds later, inside the same window.
	if _, err := w.Look(context.Background(), "usr_1"); err != nil {
		t.Fatalf("second look: %v", err)
	}
	if len(spoke) != 1 {
		t.Errorf("the warning was lost: asked %d times, said %v", asked, spoke)
	}

	// And it is still said only once.
	if _, err := w.Look(context.Background(), "usr_1"); err != nil {
		t.Fatalf("third look: %v", err)
	}
	if len(spoke) != 1 {
		t.Errorf("said it %d times: %v", len(spoke), spoke)
	}
}

// TestAProviderDownAllEveningStillWarnsThem : Retrying covers a
// provider busy for a moment. A provider down for the evening would
// otherwise be the same as never having been told, and the whole
// point of this class of announcement is that waiting costs
// something.
func TestAProviderDownAllEveningStillWarnsThem(t *testing.T) {
	var asked int
	var spoke []string
	w := &notice.Watcher{
		Rules:  wants("battery.level 20"),
		Events: recorded{held: []event.Event{marked("battery.level", "20", 0)}},
		Gate:   &notice.Gate{Away: func(string) (bool, bool) { return false, true }},
		Ask: func(context.Context, string, string) (string, error) {
			asked++
			return "", errors.New("the provider is down")
		},
		Say: func(_ context.Context, _, text string, immediate bool) error {
			if !immediate {
				t.Error("a warning went out as company rather than at once")
			}
			spoke = append(spoke, text)
			return nil
		},
		Now: func() time.Time { return at },
	}

	// The first two looks try and say nothing.
	for i := 1; i <= notice.Tries-1; i++ {
		if _, err := w.Look(context.Background(), "usr_1"); err == nil {
			t.Fatalf("look %d: a failure was reported as a success", i)
		}
		if len(spoke) != 0 {
			t.Fatalf("look %d: gave up on wording it too soon: %v", i, spoke)
		}
	}

	// The third says it in the sender's own words.
	if _, err := w.Look(context.Background(), "usr_1"); err != nil {
		t.Fatalf("the last look: %v", err)
	}
	if len(spoke) != 1 {
		t.Fatalf("the warning was lost after %d tries: %v", asked, spoke)
	}
	if !strings.Contains(spoke[0], "battery level 20") {
		t.Errorf("the plain words do not say what it was: %q", spoke[0])
	}
	if !strings.Contains(spoke[0], "cannot word it") {
		t.Errorf("nothing explains why it is terse: %q", spoke[0])
	}

	// And it is still said only once.
	if _, err := w.Look(context.Background(), "usr_1"); err != nil {
		t.Fatalf("after: %v", err)
	}
	if len(spoke) != 1 {
		t.Errorf("said it %d times: %v", len(spoke), spoke)
	}
}

// TestThePlainWordsNameNoKind : Built from the label, which is the
// sender's own words, so a kind nobody has thought of yet comes out
// as well as a battery does.
func TestThePlainWordsNameNoKind(t *testing.T) {
	for label, want := range map[string]string{
		"battery.level 20":            "battery level 20",
		"device.overheating pixel 7":  "device overheating pixel 7",
		"something.nobody.imagined x": "something nobody imagined x",
	} {
		said := notice.Plain(notice.Candidate{Label: label, Immediate: true})
		if !strings.Contains(said, want) {
			t.Errorf("%q came out as %q", label, said)
		}
	}
	if notice.Plain(notice.Candidate{}) != "" {
		t.Error("a candidate with no label produced words anyway")
	}
}
