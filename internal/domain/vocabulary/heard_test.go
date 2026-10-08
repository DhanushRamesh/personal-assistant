package vocabulary_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/base/world/event"
	"personal-assistant/internal/domain/vocabulary"
)

// reported : One thing a device said happened.
func reported(kind, value string) event.Event {
	payload, _ := json.Marshal(map[string]string{"value": value})
	return event.Event{Kind: kind, Payload: payload, OccurredAt: time.Now()}
}

// TestTheDeviceSpellingIsTaken : The reason this exists. A name in
// the transcript has been through speech recognition and may already
// be wrong; the phone read the same name off a contact and spelt it
// the way they spell it.
func TestTheDeviceSpellingIsTaken(t *testing.T) {
	got := vocabulary.Heard([]event.Event{
		reported("call.made", "Alekhya Chintada"),
		reported("place.stayed", "Ammaniammal School"),
		reported("wifi.joined", "Dhanush_EXT"),
	})
	for _, want := range []string{"Alekhya Chintada", "Ammaniammal School", "Dhanush_EXT"} {
		if !has(got, want) {
			t.Errorf("%q was not taken from what the devices reported: %v", want, got)
		}
	}
}

// TestWhatIsNeverAName : Three shapes, each of which would otherwise
// be handed to a speech engine as a word to expect.
func TestWhatIsNeverAName(t *testing.T) {
	got := vocabulary.Heard([]event.Event{
		reported("location.fix", "12.91100,80.06241"),
		reported("call.missed", "+919000000001"),
		reported("call.received", event.Unknown),
		reported("call.received", event.UnknownNumber),
		reported("music.started", ""),
	})
	if len(got) != 0 {
		t.Errorf("something that is never a name was offered as one: %v", got)
	}
}

// TestMostReportedFirst : There are a hundred places on the list. A
// name reported twenty times is one they live with; a name reported
// once may be a stranger who rang.
func TestMostReportedFirst(t *testing.T) {
	var all []event.Event
	all = append(all, reported("call.received", "A stranger"))
	for i := 0; i < 5; i++ {
		all = append(all, reported("call.made", "Alekhya Chintada"))
	}
	got := vocabulary.Heard(all)
	if len(got) < 2 || got[0] != "Alekhya Chintada" {
		t.Errorf("the name they live with is not first: %v", got)
	}
}

// TestThePromptSaysWhichSpellingWins : Without that the model has two
// spellings of one name and no reason to prefer either.
func TestThePromptSaysWhichSpellingWins(t *testing.T) {
	p := vocabulary.Prompt(nil, []string{"Alekhya Chintada"})
	if !strings.Contains(p, "Alekhya Chintada") {
		t.Error("the reported names are not in the prompt")
	}
	for _, want := range []string{"not been through speech recognition", "that is the mishearing"} {
		if !strings.Contains(p, want) {
			t.Errorf("the prompt does not say %q", want)
		}
	}
	// And nothing at all when there are none, rather than a heading
	// with an empty list under it.
	if strings.Contains(vocabulary.Prompt(nil, nil), "most-reported first") {
		t.Error("an empty list still produced a heading")
	}
}

func has(all []string, want string) bool {
	return slices.Contains(all, want)
}
