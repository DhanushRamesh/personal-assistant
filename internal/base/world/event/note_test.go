package event_test

import (
	"encoding/json"
	"testing"

	"personal-assistant/internal/base/world/event"
)

// A note's line is read with its space; an empty one, or another kind, is
// not a note.
func TestNoteOf(t *testing.T) {
	payload, _ := json.Marshal(map[string]string{"space": " Jarvis ", "line": " call Ravi Friday 4pm "})
	n, ok := event.NoteOf(&event.Event{Kind: event.NoteAdded, Payload: payload})
	if !ok || n.Space != "Jarvis" || n.Line != "call Ravi Friday 4pm" {
		t.Fatalf("read %+v, %v", n, ok)
	}
	empty, _ := json.Marshal(map[string]string{"space": "Jarvis", "line": "  "})
	if _, ok := event.NoteOf(&event.Event{Kind: event.NoteAdded, Payload: empty}); ok {
		t.Error("an empty line was read as a note")
	}
	if _, ok := event.NoteOf(&event.Event{Kind: event.TerminalAsks, Payload: payload}); ok {
		t.Error("a terminal's news was read as a note")
	}
}
