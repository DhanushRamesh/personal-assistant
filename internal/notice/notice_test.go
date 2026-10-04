package notice_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/event"
	"github.com/DhanushRamesh/personal-assistant/internal/notice"
)

var at = time.Date(2026, 10, 4, 21, 0, 0, 0, time.UTC)

// toggling : One thing a sender reported as a state, the way a
// player and a screen do: a value, and a field saying which way it
// is.
func toggling(kind, value, field, is string, ago time.Duration) event.Event {
	payload, _ := json.Marshal(map[string]string{"value": value, field: is})
	return event.Event{Kind: kind, Payload: payload, OccurredAt: at.Add(-ago)}
}

// happened : One thing, a given time before now.
func happened(kind, value string, ago time.Duration) event.Event {
	payload, _ := json.Marshal(map[string]string{"value": value})
	return event.Event{Kind: kind, Payload: payload, OccurredAt: at.Add(-ago)}
}

// TestTheThirdOneIsWorthMentioning : The owner's own example. Three
// calls from the same number inside a quarter of an hour is somebody
// trying to reach him; the third is the one worth saying.
func TestTheThirdOneIsWorthMentioning(t *testing.T) {
	history := []event.Event{
		happened("call.missed", "+919000000001", 12*time.Minute),
		happened("call.missed", "+919000000001", 6*time.Minute),
		happened("call.missed", "+919000000001", 0),
	}
	arrived := history[2:]

	got := notice.Find(arrived, history, at)
	if len(got) != 1 {
		t.Fatalf("found %d candidates, want 1: %+v", len(got), got)
	}
	if got[0].Reason != notice.Often {
		t.Errorf("reason = %q, want %q", got[0].Reason, notice.Often)
	}
	if got[0].Times != 3 {
		t.Errorf("counted %d, want 3", got[0].Times)
	}
}

// TestTwiceIsNotWorthMentioning : Two of anything is a coincidence,
// and a remark made on a coincidence is worse than one not made.
func TestTwiceIsNotWorthMentioning(t *testing.T) {
	history := []event.Event{
		happened("call.missed", "+919000000001", 30*24*time.Hour),
		happened("call.missed", "+919000000001", 6*time.Minute),
		happened("call.missed", "+919000000001", 0),
	}
	for _, c := range notice.Find(history[2:], history, at) {
		if c.Reason == notice.Often {
			t.Errorf("two inside the window was called insistent: %+v", c)
		}
	}
}

// TestSomethingNeverSeenBeforeIsWorthMentioning : The other half,
// and the owner's second case: a song he has not played before is
// something it may remark on.
func TestSomethingNeverSeenBeforeIsWorthMentioning(t *testing.T) {
	history := []event.Event{
		happened("music.started", "Veyyon Silli", 3*24*time.Hour),
		happened("music.started", "Nani Kini", 0),
	}
	got := notice.Find(history[1:], history, at)
	if len(got) != 1 || got[0].Reason != notice.Novel {
		t.Fatalf("a first hearing was not noticed: %+v", got)
	}
	if got[0].Label != `music.started Nani Kini` {
		t.Errorf("label = %q", got[0].Label)
	}
}

// TestSomethingOrdinaryIsNotMentioned : Joining the home network is
// not news. He was there.
func TestSomethingOrdinaryIsNotMentioned(t *testing.T) {
	var history []event.Event
	for i := 1; i <= 20; i++ {
		history = append(history, happened("wifi.joined", "Dhanush", time.Duration(i)*8*time.Hour))
	}
	arrived := []event.Event{happened("wifi.joined", "Dhanush", 0)}
	history = append(history, arrived...)

	if got := notice.Find(arrived, history, at); len(got) != 0 {
		t.Errorf("something that happens daily was called news: %+v", got)
	}
}

// TestAPositionReadingIsNeverMentioned : They arrive every few
// minutes and every value is distinct, so unchecked every one of
// them would look like something never seen before.
func TestAPositionReadingIsNeverMentioned(t *testing.T) {
	arrived := []event.Event{happened(event.Fixed, "12.91100,80.06241", 0)}
	if got := notice.Find(arrived, arrived, at); len(got) != 0 {
		t.Errorf("a coordinate was offered as news: %+v", got)
	}
}

// TestWorkingMaterialCanBeNewButNotFrequent : A player reporting
// playing, paused, playing is one song being listened to, not three
// of anything. Counting the toggles would have the assistant remark
// on somebody using a pause button.
//
// It can still be new, which is the half worth having: the owner's
// own example is hearing a song it has not heard before.
func TestWorkingMaterialCanBeNewButNotFrequent(t *testing.T) {
	var all []event.Event
	for d := 1; d <= 20; d++ {
		all = append(all, toggling(event.State, "A familiar artist", "playing", "true", time.Duration(d)*8*time.Hour))
	}
	var toggles []event.Event
	for i := 0; i < 5; i++ {
		is := []string{"true", "false"}[i%2]
		toggles = append(toggles, toggling(event.State, "A familiar artist", "playing", is, time.Duration(i)*2*time.Minute))
	}
	all = append(all, toggles...)

	for _, c := range notice.Find(toggles, all, at) {
		if c.Reason == notice.Often {
			t.Errorf("a pause button was counted as something happening a lot: %+v", c)
		}
	}

	// But an artist never reported before is news.
	fresh := []event.Event{toggling(event.State, "Somebody new", "playing", "true", 0)}
	got := notice.Find(fresh, append(all, fresh...), at)
	if len(got) != 1 || got[0].Reason != notice.Novel {
		t.Fatalf("a first hearing was not noticed: %+v", got)
	}
}

// TestANewSenderNeedsNoCodeHere : The owner, asking whether screen
// lock and unlock could be added: it must cost nothing in this
// server.
//
// A screen is unlocked fifty times a day, so under the repeat rule
// it would be a nuisance from the first hour. What stops that used
// to be a list of kinds in this package, which meant every toggle
// the person invented needed a line here before it behaved. It is
// read from the payload now: a field that is true or false says
// what the event is, and the sender has already written it.
func TestANewSenderNeedsNoCodeHere(t *testing.T) {
	var all []event.Event
	for d := 1; d <= 10; d++ {
		all = append(all, toggling("screen.changed", "pixel 7", "locked", "false", time.Duration(d)*6*time.Hour))
	}
	var unlocks []event.Event
	for i := 0; i < 6; i++ {
		is := []string{"false", "true"}[i%2]
		unlocks = append(unlocks, toggling("screen.changed", "pixel 7", "locked", is, time.Duration(i)*90*time.Second))
	}
	all = append(all, unlocks...)

	for _, c := range notice.Find(unlocks, all, at) {
		if c.Reason == notice.Often {
			t.Errorf("checking a phone six times was called news: %+v", c)
		}
	}
}

// TestSomethingThatHappenedIsStillCounted : The rule must not
// swallow the thing it was built for. A missed call carries fields
// too, and none of them says true or false.
func TestSomethingThatHappenedIsStillCounted(t *testing.T) {
	history := []event.Event{
		happened("call.missed", "+919000000001", 12*time.Minute),
		happened("call.missed", "+919000000001", 6*time.Minute),
		happened("call.missed", "+919000000001", 0),
	}
	got := notice.Find(history[2:], history, at)
	if len(got) != 1 || got[0].Reason != notice.Often {
		t.Fatalf("the third call stopped being counted: %+v", got)
	}
}

// TestTheValueItselfIsNotAState : A sender whose whole value is
// "true" is saying something happened, not describing a state of
// something.
func TestTheValueItselfIsNotAState(t *testing.T) {
	payload, _ := json.Marshal(map[string]string{"value": "true"})
	if notice.Toggling(payload) {
		t.Error("a value of true was read as a state field")
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

	got := notice.Find([]event.Event{fresh}, []event.Event{fresh}, at)
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
	c := notice.Candidate{Label: "x", Reason: notice.Novel, At: at}

	out := &notice.Gate{Away: func(string) (bool, bool) { return true, true }}
	if ok, _ := out.Allow("usr_1", c, at); ok {
		t.Error("spoke to somebody who is out")
	}
	unknown := &notice.Gate{Away: func(string) (bool, bool) { return false, false }}
	if ok, _ := unknown.Allow("usr_1", c, at); ok {
		t.Error("spoke when it could not tell whether anybody was there")
	}
	nobodyWatching := &notice.Gate{}
	if ok, _ := nobodyWatching.Allow("usr_1", c, at); ok {
		t.Error("spoke with nothing watching the room at all")
	}
}

// TestNotTwiceInAHurry : The failure that kills this is being a
// nuisance, after which the remarks worth hearing are ignored too.
func TestNotTwiceInAHurry(t *testing.T) {
	g := &notice.Gate{Away: func(string) (bool, bool) { return false, true }}
	first := notice.Candidate{Label: "a", Reason: notice.Novel, At: at}
	second := notice.Candidate{Label: "b", Reason: notice.Novel, At: at}

	if ok, _ := g.Allow("usr_1", first, at); !ok {
		t.Fatal("the first remark was refused")
	}
	if ok, _ := g.Allow("usr_1", second, at.Add(time.Minute)); ok {
		t.Error("two remarks a minute apart")
	}
	if ok, _ := g.Allow("usr_1", second, at.Add(notice.Quiet+time.Second)); !ok {
		t.Error("still silent after the quiet had passed")
	}
}

// TestNotTwiceAboutTheSameThing : Once per thing. But not for ever:
// somebody calling repeatedly today and again next month is two
// pieces of news.
func TestNotTwiceAboutTheSameThing(t *testing.T) {
	g := &notice.Gate{Away: func(string) (bool, bool) { return false, true }}
	c := notice.Candidate{Label: "a", Reason: notice.Often, At: at}

	if ok, _ := g.Allow("usr_1", c, at); !ok {
		t.Fatal("the first remark was refused")
	}
	later := at.Add(notice.Quiet * 2)
	if ok, _ := g.Allow("usr_1", c, later); ok {
		t.Error("said the same thing twice in a day")
	}
	if ok, _ := g.Allow("usr_1", c, at.Add(notice.Over+time.Minute)); !ok {
		t.Error("the same thing a month later is still news, and was refused")
	}
}

// recorded : An event store holding a fixed set.
type recorded struct{ held []event.Event }

func (r recorded) Recent(_ context.Context, _ string, _ event.Query) ([]event.Event, error) {
	return r.held, nil
}

// TestTheModelMayAnswerWithNothing : The pressure valve. Code finds
// candidates generously, so most looks must end in silence, and
// silence is a success rather than a wasted call.
func TestTheModelMayAnswerWithNothing(t *testing.T) {
	var spoke []string
	w := &notice.Watcher{
		Events: recorded{held: []event.Event{happened("music.started", "a new song", 0)}},
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

// TestARemarkIsSpoken : And the other way: when the model has
// something, it reaches the person.
func TestARemarkIsSpoken(t *testing.T) {
	var spoke []string
	w := &notice.Watcher{
		Events: recorded{held: []event.Event{happened("music.started", "a new song", 0)}},
		Gate:   &notice.Gate{Away: func(string) (bool, bool) { return false, true }},
		Ask: func(context.Context, string, string) (string, error) {
			return "That is a new one, sir.", nil
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
	if len(spoke) != 1 || spoke[0] != "That is a new one, sir." {
		t.Errorf("the remark did not reach them: %v", spoke)
	}
}

// TestOneRemarkPerLook : Two things worth saying in the same moment
// are two remarks, and the second is still worth saying in twenty
// minutes if it was worth saying now.
func TestOneRemarkPerLook(t *testing.T) {
	var asked int
	w := &notice.Watcher{
		Events: recorded{held: []event.Event{
			happened("music.started", "a new song", 0),
			happened("wifi.joined", "a new network", 0),
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
		Events: recorded{held: []event.Event{happened("wifi.joined", "home", 3*time.Hour)}},
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
	c := notice.Candidate{Label: "music.state Stevie Wonder", Reason: notice.Novel, At: at}

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

// TestARefusalNamesWhatBeatIt : Owner, reading the page: "I don't
// want 'something was said', I want exactly what was said."
//
// "Something was said four minutes ago" tells the person nothing
// they can act on. The sentence itself says whether the thing held
// back was worth more than the thing that beat it to it.
func TestARefusalNamesWhatBeatIt(t *testing.T) {
	g := &notice.Gate{Away: func(string) (bool, bool) { return false, true }}
	g.Spoke("That is the third call from that number, sir.", at)

	_, why := g.Allow("usr_1", notice.Candidate{Label: "b", At: at}, at.Add(time.Minute))
	if !strings.Contains(why, "third call from that number") {
		t.Errorf("the refusal does not say what was said: %q", why)
	}
	// And one minute is one minute.
	if strings.Contains(why, "1 minutes") {
		t.Errorf("a single minute was pluralised: %q", why)
	}
}

// TestAFreshGateStillExplainsItself : After a restart the clock is
// fresh but nothing has been said through this gate, so there is no
// sentence to name.
func TestAFreshGateStillExplainsItself(t *testing.T) {
	g := &notice.Gate{Away: func(string) (bool, bool) { return false, false }}
	_, why := g.Allow("usr_1", notice.Candidate{Label: "a", At: at}, at)
	if why == "" {
		t.Error("a refusal gave no reason at all")
	}
}

// urgently : One thing a device marked as needing to be said.
func urgently(kind, value string, more map[string]string) event.Event {
	fields := map[string]string{"value": value, notice.Urgent: "true"}
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
	// Familiar, frequent, and in no way unusual: without the mark it
	// would never be a candidate at all.
	var history []event.Event
	for i := 0; i < 30; i++ {
		history = append(history, happened("battery.level", "25", time.Duration(i)*6*time.Hour))
	}
	arrived := []event.Event{urgently("battery.level", "25", nil)}
	history = append(history, arrived...)

	got := notice.Find(arrived, history, at)
	if len(got) != 1 {
		t.Fatalf("a device insisting was not heard: %+v", got)
	}
	if got[0].Reason != notice.Told || !got[0].Urgent {
		t.Errorf("reason = %q urgent = %v, want told and urgent", got[0].Reason, got[0].Urgent)
	}
}

// TestAWarningIsNotHeldForPoliteness : The quiet between remarks
// exists so that company does not become a nuisance. A battery about
// to die is not company, and somebody who is not told because
// something else was said four minutes ago is worse off than
// somebody interrupted twice.
func TestAWarningIsNotHeldForPoliteness(t *testing.T) {
	g := &notice.Gate{Away: func(string) (bool, bool) { return false, true }}
	ordinary := notice.Candidate{Label: "a", Reason: notice.Novel, At: at}
	warning := notice.Candidate{Label: "battery.level 5", Reason: notice.Told, Urgent: true, At: at}

	if ok, _ := g.Allow("usr_1", ordinary, at); !ok {
		t.Fatal("the first remark was refused")
	}
	if ok, why := g.Allow("usr_1", warning, at.Add(time.Minute)); !ok {
		t.Errorf("a warning was held back for politeness: %s", why)
	}
	// And twice in a day is two warnings, not one and a silence.
	if ok, why := g.Allow("usr_1", warning, at.Add(2*time.Minute)); !ok {
		t.Errorf("the same warning an hour later was refused: %s", why)
	}
}

// TestAWarningFollowsThemOutOfTheRoom : Where they are decides how
// it is said, not whether. A warning held back because they stepped
// next door is a warning that did not happen.
func TestAWarningFollowsThemOutOfTheRoom(t *testing.T) {
	out := &notice.Gate{Away: func(string) (bool, bool) { return true, true }}
	warning := notice.Candidate{Label: "battery.level 5", Reason: notice.Told, Urgent: true, At: at}

	if ok, why := out.Allow("usr_1", warning, at); !ok {
		t.Errorf("a warning was refused because the room was empty: %s", why)
	}
	// But company still belongs in the room.
	ordinary := notice.Candidate{Label: "a", Reason: notice.Novel, At: at}
	if ok, _ := out.Allow("usr_1", ordinary, at); ok {
		t.Error("an ordinary remark was made to an empty room")
	}
}

// TestTheModelMayNotDeclineAWarning : The refusal is the whole point
// of asking a model, and it is exactly wrong here.
func TestTheModelMayNotDeclineAWarning(t *testing.T) {
	warning := notice.Candidate{Label: "battery.level 5", Reason: notice.Told, Urgent: true, At: at}
	asked := notice.Remark(warning, at)

	if !strings.Contains(asked, "has to be said") {
		t.Error("the prompt does not say a warning must be said")
	}
	if !strings.Contains(asked, "Do not answer with "+notice.Silence) {
		t.Error("the prompt still offers the refusal")
	}
	// And an ordinary remark is still free to decline.
	ordinary := notice.Remark(notice.Candidate{Label: "a", Reason: notice.Novel, At: at}, at)
	if strings.Contains(ordinary, "has to be said") {
		t.Error("an ordinary remark was told it must be said")
	}
}

// TestAReadingIsNotNews : A song, a place and a caller are
// identities, and the first one is worth a word. A battery at
// seventy-three per cent is a measurement, and every measurement is
// new the first time it is taken.
//
// A phone reporting every one per cent would otherwise offer a
// hundred novelties a day, each of them nothing, and each costing a
// model call to decline.
func TestAReadingIsNotNews(t *testing.T) {
	fresh := []event.Event{toggling("battery.level", "73", "charging", "false", 0)}
	if got := notice.Find(fresh, fresh, at); len(got) != 0 {
		t.Errorf("a battery reading was offered as news: %+v", got)
	}

	// And a name still is.
	song := []event.Event{toggling("music.state", "Stevie Wonder", "playing", "true", 0)}
	if got := notice.Find(song, song, at); len(got) != 1 || got[0].Reason != notice.Novel {
		t.Errorf("a first hearing stopped being news: %+v", got)
	}
}

// TestAReadingCanStillInsist : Which is the whole point of the mark:
// a battery at five per cent is not unusual and is still worth
// saying.
func TestAReadingCanStillInsist(t *testing.T) {
	fresh := []event.Event{urgently("battery.level", "5", nil)}
	got := notice.Find(fresh, fresh, at)
	if len(got) != 1 || got[0].Reason != notice.Told {
		t.Fatalf("a device insisting about a reading was not heard: %+v", got)
	}
}
