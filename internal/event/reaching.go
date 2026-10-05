package event

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Seconds, Reached : The two payload fields that say somebody was
// being reached, and whether it worked.
//
// Conventions rather than kinds, which is what makes them usable
// here. A sender writing a number has said this event was an
// attempt to get hold of a person; one writing seconds has said how
// long it ran. Both mean the same whoever sends them, and no kind
// has to be named for either to be read -- a phone that learns to
// report some new sort of call is covered the day it does.
const (
	Seconds = "seconds"
	Reached = "number"
)

// Reach : One attempt to get hold of somebody.
type Reach struct {
	// Who : The name as the sender gave it, or empty where it gave
	// none.
	Who string
	// When : The last time it happened.
	When time.Time
	// Tries : How many times, counting from the first unanswered one.
	Tries int
	// Got : Whether it worked.
	Got bool
}

// Reaching : Who tried to get hold of them and whether it worked.
//
// Returns what is still outstanding and what got through, in that
// order. An attempt is an event carrying a number and no time; it is
// settled by anything later that carries a number, names the same
// person, and did take some time.
//
// Deciding this is two comparisons and not a judgement about
// language, which is why it is here rather than in a prompt. A model
// asked to make it raised a call that was returned eight minutes
// later, on four arrivals out of four.
func Reaching(evs []Event) (open, got []Reach) {
	waiting := map[string]*Reach{}
	done := map[string]*Reach{}
	order := []string{}
	seen := func(who string) {
		if _, had := waiting[who]; had {
			return
		}
		if _, had := done[who]; had {
			return
		}
		order = append(order, who)
	}

	for _, e := range evs {
		into := payload(e)
		if _, reaching := into[Reached]; !reaching {
			continue
		}
		who := strings.TrimSpace(valued(e))
		seen(who)
		if n, had := size(into[Seconds]); had && n > 0 {
			// It got through, so nothing with this person is
			// outstanding any longer.
			delete(waiting, who)
			done[who] = &Reach{Who: who, When: e.OccurredAt, Got: true}
			continue
		}
		delete(done, who)
		if at, had := waiting[who]; had {
			at.Tries++
			if e.OccurredAt.After(at.When) {
				at.When = e.OccurredAt
			}
			continue
		}
		waiting[who] = &Reach{Who: who, When: e.OccurredAt, Tries: 1}
	}

	for _, who := range order {
		if r, had := waiting[who]; had {
			open = append(open, *r)
		}
		if r, had := done[who]; had {
			got = append(got, *r)
		}
	}
	sort.SliceStable(open, func(i, j int) bool { return open[i].When.Before(open[j].When) })
	return open, got
}

// Named : A caller as they should be spoken of.
//
// A name the sender gave, or that nobody knows who it was. Never the
// number: that is kept in the record and is not for saying aloud.
func (r Reach) Named() string {
	who := strings.TrimSpace(r.Who)
	if who == "" || who == Unknown || Number(who) {
		return "somebody the phone could not name"
	}
	return Readable(who)
}

// payload : An event's payload as a map, or an empty one.
func payload(e Event) map[string]any {
	var into map[string]any
	if json.Unmarshal(e.Payload, &into) != nil {
		return nil
	}
	return into
}

// valued : A sender's own word for the thing.
func valued(e Event) string {
	var into struct {
		Value string `json:"value"`
	}
	if json.Unmarshal(e.Payload, &into) != nil {
		return ""
	}
	return strings.TrimSpace(into.Value)
}

// size : A JSON number however the sender wrote it.
func size(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		return f, err == nil
	}
	return 0, false
}
