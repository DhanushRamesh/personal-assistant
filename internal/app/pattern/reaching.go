package pattern

import (
	"strings"
	"time"

	"personal-assistant/internal/base/world/event"
)

// Insisting : How many tries make somebody insisting.
//
// Two, not one. One unanswered call is ordinary and they will see
// it; the same person trying again is a person who needs
// something, and that is the difference between a notification and
// a reason to pick the phone up.
const Insisting = 2

// reaching : Somebody has tried more than once and not got through.
//
//	{"pattern": "reaching", "with": {"tries": 2, "among": "amma"}}
//
// The shape the engine could not say, and the reason patterns
// exist at all. Counting gets close -- `counted call.missed by
// value within 2h >= 2` is two missed calls from one person -- and
// is wrong in the way that matters: it goes on being true after
// they have got through. Somebody ringing twice and then being
// answered is not somebody who needs you, and no window or
// threshold distinguishes them, because the thing that settles it
// is an event that *cancels* the ones before it.
//
// That cancelling is [event.Reaching], which pairs attempts with
// what answered them and hands back only the ones still open. It
// was written for `situation.Trying` and is the whole of what that
// check was; this is that check with its two constants turned into
// blanks.
//
// Blanks:
//
//	tries   how many attempts make it worth saying. Default two.
//	among   only this person. Empty is anybody.
//	within  how far back to look, read by the matcher.
func reaching(evs []event.Event, _ time.Time, with Blanks) (bool, error) {
	tries, said, err := with.Number("tries")
	if err != nil {
		return false, err
	}
	if !said || tries < 1 {
		tries = Insisting
	}
	who, only := with.Word("among")

	open, _ := event.Reaching(evs)
	for _, r := range open {
		if r.Tries < tries {
			continue
		}
		// Matched on the name the event carries, which is the name
		// the sender resolved. The server holds no lookup table,
		// and a rule naming somebody is naming them as their phone
		// does.
		if only && !strings.EqualFold(strings.TrimSpace(r.Who), who) {
			continue
		}
		return true, nil
	}
	return false, nil
}
