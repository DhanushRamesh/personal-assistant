// Package catching writes down what was said and never written.
//
// Owner, 18 October 2026: *"when profiling or when you see something in
// conversation .. if something important happened or is going to happen,
// note in calendar or task -- don't ask me."*
//
// The persona half of that is done and works as the words arrive: a date
// said out loud in a turn is written during that turn. This is the other
// half, and it exists because the turn is not where most of them are
// lost. A thing said while the assistant was answering something else, a
// thing said before the rule existed, a thing said in a sentence that
// also asked a question -- the turn has one chance at each of those and
// no second look.
//
// So once a day, after the description is rewritten, it reads back over
// what was actually said and asks what is still only in a conversation.
// The same shape as noticing: one model, told to go and look, with an
// allowance it cannot exceed and a bar low enough to be useful and high
// enough to stay quiet.
//
// It reads the diary before it writes to it. That is not tidiness: the
// live rule has usually written the entry already, so the commonest
// thing this pass can do wrong is write it a second time.
package catching

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"personal-assistant/internal/base/records/conversation"
	"personal-assistant/internal/base/thinking/prompt"
)

// Since : How far back one pass reads.
//
// A day and a half rather than a day. The pass runs daily but not to
// the minute, and an arrangement that falls in the gap between two runs
// is lost for good; seeing a few hours twice costs nothing, because the
// second look reads the diary and finds it already there.
const Since = 36 * time.Hour

// Most : The most entries one pass may write.
//
// Three. The allowance exists for the same reason noticing's does: a
// model asked daily whether anything was missed will find something
// daily, and a diary filling up with things the person half-said is
// worse than one that misses a few. Three is more than a quiet day has
// and fewer than a talkative one would offer.
const Most = 3

// Messages : The most messages shown in one pass, oldest dropped first.
//
// A day of this person's talking is well inside it. The cap is there so
// that a day spent testing, which is most days here, cannot turn one
// pass into the largest request the server makes.
const Messages = 400

// Said : Where what they said is read from.
type Said interface {
	SaidSince(ctx context.Context, userID string, since time.Time, limit int) ([]conversation.Message, error)
}

// Ask : One question put to a model that can go and look, and write.
//
// The same signature noticing uses, so the same investigating helper
// builds both. What differs is which tools it is handed: this one is
// given the diary and the lists, and may write to them.
type Ask func(ctx context.Context, userID, system, ask string) (string, error)

// Pass : One day's look back over what was said.
type Pass struct {
	// Ask : The model that looks and writes. Nil writes nothing, which
	// is right for a server with no model configured.
	Ask Ask
	// Said : Where their messages come from. Required.
	Said Said
	// Where : The timezone their days are counted in. Nil is UTC, which
	// puts an evening on the wrong date.
	Where *time.Location
	// Now : The clock, replaceable in tests. Nil uses the real one.
	Now func() time.Time
}

// Looked : What one pass did, for the log.
type Looked struct {
	// Read : How many messages it was shown.
	Read int
	// Said : What the model said it did, or that there was nothing.
	Said string
}

// Run : Reads back over what was said and writes what is missing.
//
// An empty Said is the ordinary answer on most days and is not a
// failure. A pass that felt obliged to find something would be the
// whole problem.
func (p Pass) Run(ctx context.Context, userID string) (Looked, error) {
	if p.Ask == nil || p.Said == nil {
		return Looked{}, nil
	}
	now := time.Now
	if p.Now != nil {
		now = p.Now
	}
	where := p.Where
	if where == nil {
		where = time.UTC
	}

	said, err := p.Said.SaidSince(ctx, userID, now().Add(-Since), Messages)
	if err != nil {
		return Looked{}, fmt.Errorf("reading what they said: %w", err)
	}
	spoken := theirs(said)
	if len(spoken) == 0 {
		return Looked{}, nil
	}

	answer, err := p.Ask(ctx, userID, Judge, prompt.Block(
		prompt.Text(fmt.Sprintf(
			"It is %s. Here is everything they said in the last day and a half, oldest first.",
			now().In(where).Format("Monday 2 January 2006, 3:04 pm"))),
		Shown(spoken, where),
	))
	if err != nil {
		return Looked{Read: len(spoken)}, err
	}
	return Looked{Read: len(spoken), Said: strings.TrimSpace(answer)}, nil
}

// theirs : The person's own messages, oldest first.
//
// What the assistant said back is left out on purpose. It is the half
// most likely to contain a date -- it reads entries out, it offers, it
// confirms -- and a pass shown its own words would write down the diary
// it had just read aloud.
func theirs(said []conversation.Message) []conversation.Message {
	out := make([]conversation.Message, 0, len(said))
	for _, m := range said {
		if m.Role != conversation.User || strings.TrimSpace(m.Content) == "" {
			continue
		}
		out = append(out, m)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}

// Shown : The messages as the model reads them, a day at a time.
//
// Dated rather than timestamped one by one. What matters to this pass
// is which day a thing was said on, because that is what "tomorrow" and
// "on Thursday" are counted from, and a line of clock times per message
// buys nothing and costs the day's heading.
func Shown(said []conversation.Message, where *time.Location) string {
	if where == nil {
		where = time.UTC
	}
	var b strings.Builder
	day := ""
	for _, m := range said {
		on := m.At.In(where).Format("Monday 2 January")
		if on != day {
			if day != "" {
				b.WriteString("\n")
			}
			b.WriteString(on + "\n")
			day = on
		}
		b.WriteString("  " + m.At.In(where).Format("3:04 pm") + "  " +
			strings.Join(strings.Fields(m.Content), " ") + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
