// Package elsewhere answers the question a tool cannot answer for
// itself: if this is not a reminder, what is it?
//
// Owner: *"when something is ambiguous it should derive with the
// existing data of mine -- my calendar, tasks, reminders, phone alarms,
// contacts, events, rules and also its memory -- if nothing is related
// it should do a web search."*
//
// The shape of the problem is that a name does not say which domain it
// belongs to. *"Stop the water alarm"* is a reminder here, but alarm is
// the word for a thing on the phone, drink water is a rule that could
// have written it, and either could have been on the diary. The model
// has to pick a tool before it can look, and picking wrongly produces
// "there is no such reminder" -- which is true of reminders and false
// of the question.
//
// So this runs after a tool has looked by name and come up empty, never
// before. On a turn that worked it costs nothing at all. Measured on
// his own chats: one further round trip to the model costs 3,452ms
// against an average of 2.25 rounds a turn, and the local sources here
// answer in single-digit milliseconds in parallel. Asking the person
// which they meant costs a whole turn, about six seconds, plus their
// attention.
//
// What this does not do is decide. It hands back what is called
// something like that and where each one lives; one candidate or
// several is the difference between acting and asking, and that belongs
// to the tool's own words and the model, not here.
package elsewhere

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"personal-assistant/internal/base/world/heard"
)

// Thing : One candidate, in whatever holds it.
type Thing struct {
	// Kind : What sort of thing it is, in the singular and in the
	// words the person would use: reminder, rule, task, diary entry.
	Kind string
	// Named : What it is called, as it is stored.
	Named string
	// When : When it is for, already rendered. Empty for things that
	// are not at a time.
	//
	// Rendered rather than a time, because a number never reaches the
	// model from this server: 11:45 am is a thing to say and a
	// timestamp is a thing to get wrong.
	When string
	// Tool : What acts on it, so the model's next call is a name it
	// has rather than a guess at one.
	Tool string
	// Live : Whether it is still going to happen.
	//
	// "Stop the water alarm" is about a thing with a future. His own
	// database holds three reminders called "Drink water" -- two
	// long fired and one due at 11:45 tomorrow -- and offering all
	// three as a choice is a worse answer than offering none.
	Live bool
}

// Source : One place worth looking.
type Source struct {
	// Kind : What this source holds, used to skip the one that has
	// already looked.
	Kind string
	// Look : Everything in it that could be named. Whole rather than
	// filtered: the matching is done here so that every source agrees
	// about what counts as near enough.
	Look func(ctx context.Context, userID string) ([]Thing, error)
}

// Budget : How long any one source may take before it is left out.
//
// The local ones answer in single figures of milliseconds. This exists
// for the ones that go over the network, so that a calendar having a
// bad morning costs the answer nothing.
const Budget = 1500 * time.Millisecond

// Most : How many candidates are worth handing over.
//
// Past a handful this stops being a resolution and becomes a list to
// read out, which is the thing it exists to avoid.
const Most = 6

// Search : Everywhere worth looking, and how to look.
type Search struct {
	// Sources : Where to look. Order is not significant; they are
	// asked at once.
	Sources []Source
	// Logger : Where a source that failed is recorded. A search is
	// never allowed to fail a turn, so this is the only place a
	// failure appears.
	Logger *slog.Logger
}

// Found : What is called something like this, everywhere worth looking
// -- including the domain whose tool has just failed to find it.
//
// Searching there again looks like repeating the work and is not. The
// tools match a name the way [heard.Best] does: exactly, or one inside
// the other, or by edit distance. Measured against his own reminders,
// "water alarm" scores **0.10** against "Drink water" and is found by
// none of those three. What the two share is a word, which is what
// [Matches] asks and nothing else does. So the reminder tool is right
// that no reminder is called that, and the reminder it means is still
// a reminder.
//
// [except] is what has already looked. It is not used to skip
// anything, for the reason above; it is here so the wording can say
// which tool came up short.
//
// Never an error. This runs on a turn that has already not found what
// it wanted, and a failure here would replace a useful "it is not a
// reminder" with nothing at all. A source that fails is logged and left
// out, and the rest still answer.
func (s Search) Found(ctx context.Context, userID, named, except string) []Thing {
	if userID == "" || strings.TrimSpace(named) == "" {
		return nil
	}

	ctx, stop := context.WithTimeout(ctx, Budget)
	defer stop()

	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		found []Thing
	)
	for _, src := range s.Sources {
		if src.Look == nil {
			continue
		}
		wg.Add(1)
		go func(src Source) {
			defer wg.Done()
			all, err := src.Look(ctx, userID)
			if err != nil {
				if s.Logger != nil {
					s.Logger.WarnContext(ctx, "a source could not be searched for a name",
						slog.String("kind", src.Kind), slog.Any("error", err))
				}
				return
			}
			var near []Thing
			for _, t := range all {
				if Matches(named, t.Named) {
					near = append(near, t)
				}
			}
			if len(near) == 0 {
				return
			}
			mu.Lock()
			found = append(found, near...)
			mu.Unlock()
		}(src)
	}
	wg.Wait()

	// Nearest first, and a live one before a finished one of the same
	// name, so that both the cut at [Most] and the deduplication
	// below keep the best of each.
	sort.SliceStable(found, func(i, j int) bool {
		a, b := nearness(named, found[i].Named), nearness(named, found[j].Named)

		// A name that really is what was said comes first, whatever
		// its state: "that is the one, and it already went off" is a
		// good answer.
		if strong, other := a >= heard.Alike, b >= heard.Alike; strong != other {
			return strong
		}

		// Past that, everything left matched on a shared word and
		// nothing separates them by name. His database holds three
		// water reminders and "water alarm" is equally unlike all
		// three by edit distance; what tells them apart is that two
		// of them are over and one goes off at 11:45 tomorrow.
		// Somebody saying stop it means the one with a future.
		if found[i].Live != found[j].Live {
			return found[i].Live
		}
		return a > b
	})

	found = once(found)
	if len(found) > Most {
		found = found[:Most]
	}
	return found
}

// once : One candidate per name per kind, keeping the first.
//
// The same name repeats because a repeating reminder leaves a row
// behind every time it fires. Three rows called "Drink water" are one
// thing to the person, and asking which of three identical names they
// meant is not a question anybody can answer.
func once(found []Thing) []Thing {
	seen := make(map[string]bool, len(found))
	kept := found[:0]
	for _, t := range found {
		key := strings.ToLower(t.Kind) + "\x00" + heard.Normalise(t.Named)
		if seen[key] {
			continue
		}
		seen[key] = true
		kept = append(kept, t)
	}
	return kept
}

// Matches : Whether a stored name is a plausible reading of a said one.
//
// Three tests, because one is not enough for the thing this is for.
// Likeness alone is the obvious one and it fails the example that
// prompted all this: *"water alarm"* against *"Time to drink water,
// sir"* scores badly by edit distance, because the two are nothing
// alike as strings while plainly being the same thing. What they share
// is a word.
//
// So: near as a whole, or one inside the other, or sharing a word worth
// sharing. A shared word has to be long enough to mean something --
// "the" and "my" are in everything -- which is what [heard.Shortest] is
// already the number for.
func Matches(said, stored string) bool {
	want, have := heard.Normalise(said), heard.Normalise(stored)
	if want == "" || have == "" {
		return false
	}
	if want == have || heard.Likeness(want, have) >= heard.Alike {
		return true
	}
	if len(want) >= heard.Shortest && strings.Contains(have, want) {
		return true
	}
	if len(have) >= heard.Shortest && strings.Contains(want, have) {
		return true
	}
	for _, word := range strings.Fields(said) {
		word = heard.Normalise(word)
		if len(word) < heard.Shortest {
			continue
		}
		for _, against := range strings.Fields(stored) {
			if heard.Normalise(against) == word {
				return true
			}
		}
	}
	return false
}

// nearness : How good a match is, for ordering only.
//
// A shared word scores below outright likeness, so a thing actually
// called what was said comes before a thing that merely mentions it.
func nearness(said, stored string) float64 {
	want, have := heard.Normalise(said), heard.Normalise(stored)
	if like := heard.Likeness(want, have); like >= heard.Alike {
		return like
	}
	if strings.Contains(have, want) || strings.Contains(want, have) {
		return heard.Alike
	}
	return heard.Likeness(want, have)
}
