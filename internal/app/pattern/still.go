package pattern

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"personal-assistant/internal/base/world/event"
	"personal-assistant/internal/domain/job"
)

// still : A thing that started and has not been ended.
//
//	{"pattern": "still", "with":
//	  {"on": "bluetooth.connected", "off": "bluetooth.disconnected",
//	   "is": "bike helmet"}}
//
// Owner, wanting to be told when he gets home with the helmet
// still paired: *"if more than 15 minutes the helmet bluetooth is
// connected, it can say to turn it off."*
//
// Which the grammar cannot ask. "Connected" is easy and wrong --
// the most recent `bluetooth.connected` is the helmet whether or
// not it has since been unpaired. Counting is easy and wrong the
// other way: no disconnect in the last fifteen minutes is also
// true of a helmet unpaired an hour ago. What settles it is which
// of the two happened *last*, and the engine has no way to compare
// the times of two sources.
//
// This is the same shape as [reaching] with the specifics taken
// out. There it is calls answered cancelling calls missed; here it
// is any kind cancelling any other. The second customer, and the
// reason the general form is worth having: the first was about
// people, this one is about a helmet, and neither needed a word
// added to the grammar.
//
// Blanks:
//
//	on      the kind that starts it. Required.
//	off     the kind that ends it. Required.
//	is      only readings with this value. Empty is any.
//	within  how far back to look, read by the matcher.
func still(evs []event.Event, _ time.Time, with Blanks) (bool, error) {
	on, said := with.Word("on")
	if !said {
		return false, fmt.Errorf("%w: still needs the kind that starts it, as \"on\"",
			job.ErrUnreadableGate)
	}
	off, said := with.Word("off")
	if !said {
		return false, fmt.Errorf("%w: still needs the kind that ends it, as \"off\"",
			job.ErrUnreadableGate)
	}
	if strings.EqualFold(on, off) {
		return false, fmt.Errorf("%w: a thing cannot be ended by itself", job.ErrUnreadableGate)
	}
	only, named := with.Word("is")

	// The latest of each, by when it happened. Events arrive in no
	// promised order, so neither end is taken on position.
	var started, ended time.Time
	for _, e := range evs {
		if named && !strings.EqualFold(strings.TrimSpace(valued(e)), only) {
			continue
		}
		at := e.OccurredAt
		switch {
		case strings.EqualFold(e.Kind, on):
			if at.After(started) {
				started = at
			}
		case strings.EqualFold(e.Kind, off):
			if at.After(ended) {
				ended = at
			}
		}
	}

	// Never started is not still going. An absence proves nothing,
	// and a rule that fired because it had never heard of the
	// thing would be worse than one that stayed quiet.
	if started.IsZero() {
		return false, nil
	}
	return started.After(ended), nil
}

// valued : The value a sender gave, or empty.
func valued(e event.Event) string {
	var into map[string]any
	if len(e.Payload) == 0 {
		return ""
	}
	if err := jsonInto(e.Payload, &into); err != nil {
		return ""
	}
	v, found := into["value"]
	if !found {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

// jsonInto : A payload, read. Its own function so the import stays
// out of the shapes themselves.
func jsonInto(raw []byte, into any) error { return json.Unmarshal(raw, into) }
