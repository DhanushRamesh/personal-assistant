package doubting

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"personal-assistant/internal/base/records/chat"
)

// Sent : The cases hidden in one chat's record of its own requests.
//
// A send-back leaves a shape nothing had to be written down for: a
// request that ended in prose, followed by another request. What that
// next one ended in is the label -- tools means the doubt was owed,
// prose means the round changed nothing.
//
// At most one case per chat, from the first such pair. A turn that was
// sent back twice is one decision made twice over the same answer, and
// counting it twice would weight the long turns.
func Sent(t chat.Chat) (Case, bool) {
	for i := 0; i+1 < len(t.Calls); i++ {
		if t.Calls[i].Ended != chat.CallAnswered {
			continue
		}
		// A request begun before the one in front of it finished is
		// not an answer to it: it is the round that was already
		// running when the send-back was decided, and reading it as
		// the answer would label the case by the wrong call.
		next := i + 1
		for next < len(t.Calls) && t.Calls[next].Early {
			next++
		}
		if next >= len(t.Calls) {
			return Case{}, false
		}
		return Case{
			ChatID:  t.ID,
			Prompt:  strings.TrimSpace(t.Prompt),
			Draft:   strings.TrimSpace(t.Calls[i].Draft),
			Channel: string(t.Channel),
			At:      t.CreatedAt,
			Looked:  t.Calls[next].Ended == chat.CallTools,
		}, true
	}
	return Case{}, false
}

// Rows : What a listing of chats gives, as little of it as this needs.
//
// A row rather than a chat.Chat, so the eval can read the two columns
// it wants straight out of the database without the repository loading
// a conversation's worth of everything else behind each one.
type Rows struct {
	ID        string
	Prompt    string
	Channel   string
	CreatedAt time.Time
	Calls     string
}

// Cases : Every send-back in a set of rows, oldest first.
//
// A row whose calls will not parse is skipped rather than refused: it
// is one turn of thousands, and a set is none the worse for missing it.
func Cases(rows []Rows) []Case {
	out := make([]Case, 0, len(rows))
	for _, r := range rows {
		var calls []chat.Call
		if err := json.Unmarshal([]byte(r.Calls), &calls); err != nil {
			continue
		}
		c, ok := Sent(chat.Chat{
			ID: r.ID, Prompt: r.Prompt, Channel: chat.Channel(r.Channel),
			CreatedAt: r.CreatedAt, Calls: calls,
		})
		if ok {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, k int) bool { return out[i].At.Before(out[k].At) })
	return out
}
