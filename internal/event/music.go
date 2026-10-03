package event

import "strings"

// Playing, Paused : What a music player reports as it goes.
//
// One `Playing` per track and one `Paused` at the end of each, which
// is between fifty and a hundred a day for somebody who listens while
// they work. They are the record -- what was played and when -- and
// they are also, like the position readings, far too many to put in
// front of a model that asked what somebody did today.
//
// `Paused` is deliberately not "stopped". The phone cannot tell a
// pause from the half second between two tracks: both look like the
// player saying it is no longer playing, and only what happens next
// separates them. So the device reports what it saw and the judgement
// is made here, where the next event is already known.
const (
	Playing = "music.playing"
	Paused  = "music.paused"
)

// Started, Stopped : The edges of a sitting, as the phone sees them.
const (
	Started = "music.started"
	Stopped = "music.stopped"
)

// Churn : The kinds that are working material rather than history.
//
// Kept, because what is worked out from them changes -- the stays
// were recomputed twice in a week -- and discarded from anything that
// has to be read, because a day of them is longer than a day of
// everything else put together.
var Churn = []string{Fixed, Playing, Paused}

// Working : Whether a kind is one of those.
//
// A family given as "music." counts: somebody asking for the family
// is asking for what is in it.
func Working(kind string) bool {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return false
	}
	for _, x := range Churn {
		if kind == x || strings.HasPrefix(x, kind) {
			return true
		}
	}
	return false
}
