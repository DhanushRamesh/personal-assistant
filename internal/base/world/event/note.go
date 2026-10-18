package event

import (
	"encoding/json"
	"strings"
)

// NoteAdded : A line the owner wrote in a space's notes on the laptop.
//
// Owner, 18 October 2026: *"the notes I keep in a space can be added to
// tasks, or if it's like an event, added to the calendar"*. The app sends
// each line once it has stayed as written; whether it is a task, an event
// or neither is decided here.
const NoteAdded = "note.added"

// Note : One line of a space's notes, read out of its event.
type Note struct {
	// Space : The space whose notes it is in, by name.
	Space string
	// Line : The line as written, its task box or bullet taken off.
	Line string
}

// NoteOf : The line an event carries, when it is a note's; false for any
// other kind, and for an empty line.
func NoteOf(e *Event) (Note, bool) {
	if e == nil || e.Kind != NoteAdded {
		return Note{}, false
	}
	var p struct {
		Space string `json:"space"`
		Line  string `json:"line"`
	}
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return Note{}, false
		}
	}
	n := Note{Space: strings.TrimSpace(p.Space), Line: strings.TrimSpace(p.Line)}
	if n.Line == "" {
		return Note{}, false
	}
	return n, true
}
