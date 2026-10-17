package event_test

import (
	"encoding/json"
	"testing"

	"personal-assistant/internal/base/world/event"
)

func terminalEvent(kind, payload string) *event.Event {
	return &event.Event{Kind: kind, Payload: json.RawMessage(payload)}
}

// The app's own fields are read as it sends them.
func TestATerminalsNewsIsRead(t *testing.T) {
	got, ok := event.TerminalOf(terminalEvent(event.TerminalFinished, `{"terminal":"t1","name":"Server",
		"space":"Personal assistant","cwd":"/home/x","command":"npm run build","seconds":84,
		"tail":"done\n\n","conversation":" They last asked Claude Code: fix it ","value":"unknown"}`))
	if !ok {
		t.Fatal("not read")
	}
	if got.ID != "t1" || got.Name != "Server" || got.Space != "Personal assistant" || got.Folder != "/home/x" ||
		got.Command != "npm run build" || got.Seconds != 84 || got.Tail != "done" || got.Exit != nil ||
		got.Conversation != "They last asked Claude Code: fix it" {
		t.Fatalf("read as %+v", got)
	}
}

// Another kind is not a terminal's, and news with no terminal named
// cannot be kept to one reaction a terminal.
func TestOnlyATerminalsNewsIsRead(t *testing.T) {
	if _, ok := event.TerminalOf(terminalEvent("battery.low", `{"terminal":"t1"}`)); ok {
		t.Error("another kind was read as a terminal's")
	}
	if _, ok := event.TerminalOf(terminalEvent(event.TerminalAsks, `{"name":"Server"}`)); ok {
		t.Error("news with no terminal was read")
	}
	if _, ok := event.TerminalOf(nil); ok {
		t.Error("nothing was read as news")
	}
}

// A shell closed cleanly with nothing run is somebody closing a tab. With
// a code, or after a command, it may be something that stopped.
func TestAShellClosingCleanlyIsOrdinary(t *testing.T) {
	cases := map[string]bool{
		`{"terminal":"t","exit":0}`:                    true,
		`{"terminal":"t"}`:                             true,
		`{"terminal":"t","exit":1}`:                    false,
		`{"terminal":"t","exit":0,"command":"make"}`:   false,
		`{"terminal":"t","exit":130,"command":"make"}`: false,
	}
	for payload, want := range cases {
		got, _ := event.TerminalOf(terminalEvent(event.TerminalEnded, payload))
		if got.Ordinary() != want {
			t.Errorf("%s: ordinary %v, want %v", payload, !want, want)
		}
	}
	finished, _ := event.TerminalOf(terminalEvent(event.TerminalFinished, `{"terminal":"t","exit":0}`))
	if finished.Ordinary() {
		t.Error("a finished command was taken for a shell closing")
	}
}
