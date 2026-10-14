package noticing

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"personal-assistant/internal/base/thinking/prompt"
	"personal-assistant/internal/domain/memory"
)

// Held : How long a memory must have stood before it can be
// reconsidered.
//
// A day. Shorter than a rule's week because a memory is cheaper to
// be wrong about in both directions -- it is one line in a prompt,
// not a thing that interrupts somebody -- and because the commonest
// case is a fact replaced within hours of being written.
const Held = 24 * time.Hour

// Superseded : Which memory a later one has replaced.
//
// Owner: *"old facts should be able to expire."*
//
// The worst of the three places this was missing. A rule that
// outlives its pattern fires when it should not; a sentence that
// outlives its occasion stays silent. **A memory that outlives the
// truth is read into every single answer**, and the model has no
// way to know which of two contradicting lines is the later one.
//
// He has four memories about music and two of them name a favourite
// song.
//
// Only one at a time, and only where a later memory plainly says
// otherwise. Two memories that merely sit oddly together are two
// facts about a person, who is allowed to be more than one thing.
func (w Watcher) Superseded(ctx context.Context, userID string, held []memory.Memory) (string, string, error) {
	if w.Ask == nil || len(held) < 2 {
		return "", "", nil
	}
	now := time.Now()
	if w.Now != nil {
		now = w.Now()
	}

	var lines []string
	known := map[string]bool{}
	for _, m := range held {
		if !m.StaleAt.IsZero() || now.Sub(m.CreatedAt) < Held {
			continue
		}
		known[m.ID] = true
		lines = append(lines, fmt.Sprintf("%s | kept %s | %s: %s",
			m.ID, m.CreatedAt.In(time.Local).Format("2 January"),
			m.Subject, oneLine(m.Body)))
	}
	if len(lines) < 2 {
		return "", "", nil
	}

	answer, err := w.Ask(ctx, userID, superseding, prompt.Block(
		prompt.Text("Everything you have been asked to remember about them:"),
		prompt.Lines(lines...),
	))
	if err != nil {
		return "", "", err
	}

	answer = strings.TrimSpace(answer)
	answer = strings.TrimPrefix(answer, "```json")
	answer = strings.TrimPrefix(answer, "```")
	answer = strings.TrimSuffix(answer, "```")
	var out struct {
		Stands    string `json:"stands"`
		Supersede string `json:"supersede"`
		Because   string `json:"because"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(answer)), &out); err != nil {
		return "", "", nil
	}
	id := strings.TrimSpace(out.Supersede)
	// Only one he was shown. A model naming a memory it was not
	// given has invented one, and acting on that would let an
	// unreadable answer silence something the person asked to keep.
	if id == "" || !known[id] {
		return "", "", nil
	}
	return id, strings.TrimSpace(out.Because), nil
}

// oneLine : A body as one line, cut short. The standing description
// is thousands of characters and nobody is judging it here.
func oneLine(body string) string {
	body = strings.ReplaceAll(strings.ReplaceAll(body, "\n", " "), "\\n", " ")
	body = strings.Join(strings.Fields(body), " ")
	if len(body) > 160 {
		return body[:160] + "..."
	}
	return body
}
