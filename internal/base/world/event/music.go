package event

import "strings"

// State : What a player is doing, as the device sees it and without
// any judgement about what it meant.
//
// This replaced Playing and Paused on 3 October 2026. The phone was
// deciding whether a notification meant paused, resumed, skipped or
// stopped, and could not: the notifications arrive in bursts, several
// in the same second, and only what comes next separates a pause from
// the half second between two tracks. Every rule added to the phone
// fixed one case and invented another, ending in three events in two
// seconds for one skip.
//
// So the device reports the track and whether it is playing, when
// either has changed, and nothing else. The judgement is made here,
// where the next event is already in hand.
//
// The kinds it replaced -- music.playing, music.paused,
// music.started and music.stopped -- are gone. They stopped being
// written on 3 October 2026 and were kept named while their rows
// were still in the table; the rows went on 14 October and the names
// went with them. Twenty-eight rows between the four, none of them
// newer than the day they were replaced.
const State = "music.state"

// Churn : The kinds that are working material rather than history.
//
// Kept, because what is worked out from them changes -- the stays
// were recomputed twice in a week -- and discarded from anything that
// has to be read, because a day of them is longer than a day of
// everything else put together.
var Churn = []string{Fixed, State}

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
