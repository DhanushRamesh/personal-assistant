package vocabulary

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/base/event"
)

// Reported : How far back the names their devices gave are read.
//
// Four weeks against the week of conversation, for the same reason
// the description reads two windows: what somebody is talking about
// is this week's business, but a name is worth a place on the list
// only if it keeps coming round, and a month is what shows that.
const Reported = 28 * 24 * time.Hour

// Named : The most names taken from events in one pass.
//
// There are a hundred places on the list in all, so this is a cap on
// the candidates rather than on what is kept. Most-seen first, since
// a name reported twenty times is one they live with and a name
// reported once may be a stranger who rang.
const Named = 60

// Did : Where what their devices reported is read from.
type Did interface {
	Recent(ctx context.Context, userID string, q event.Query) ([]event.Event, error)
}

// Heard : The names their own devices gave, most-reported first.
//
// Worth more than the same name pulled out of a transcript, which is
// the point of this. A transcript of somebody saying "Alekhya" has
// already been through speech recognition and may say "Alekya"; the
// phone that reported the call read the name off the contact and
// spelt it the way they spelt it. These are the correct spellings of
// exactly the words that keep being got wrong.
//
// Nothing here knows what kind of event anything is. A name is
// whatever value a sender gave, minus the three shapes that are
// never names.
func Heard(events []event.Event) []string {
	times := map[string]int{}
	order := []string{}
	for _, e := range events {
		value := valueOf(e.Payload)
		if !nameable(value) {
			continue
		}
		if _, seen := times[value]; !seen {
			order = append(order, value)
		}
		times[value]++
	}

	sort.SliceStable(order, func(i, j int) bool { return times[order[i]] > times[order[j]] })
	if len(order) > Named {
		order = order[:Named]
	}
	return order
}

// nameable : Whether a value could be a name at all.
//
// Three shapes are never names, and each is here because it would
// otherwise be handed to a speech engine as a word to expect: a
// coordinate, a telephone number, and the word stored when a caller
// had no name at all.
func nameable(value string) bool {
	value = strings.TrimSpace(value)
	switch {
	case value == "", value == event.Unknown, value == event.UnknownNumber:
		return false
	case event.Position(value), event.Number(value):
		return false
	}
	// A single character, or something far too long to be said aloud
	// as a name.
	return len([]rune(value)) > 1 && len([]rune(value)) <= 60
}

// valueOf : The value a sender gave, if it gave one.
func valueOf(payload json.RawMessage) string {
	if len(payload) == 0 {
		return ""
	}
	var into struct {
		Value any `json:"value"`
	}
	if err := json.Unmarshal(payload, &into); err != nil || into.Value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(into.Value))
}
