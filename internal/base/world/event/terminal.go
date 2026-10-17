package event

import (
	"encoding/json"
	"strings"
)

// TerminalFinished, TerminalAsks, TerminalEnded : What the desktop app
// reports when one of the owner's terminals wants attention.
//
// Owner, 17 October 2026: *"if a process is done in a terminal in any
// space, the output is sent to [the assistant], and .. it can say stuff
// and act proactively"*. The app decides only that something happened;
// whether it is worth a sentence is decided here.
//
//   - finished: a command that ran for ten seconds or more ended.
//   - asks: a program rang the bell or sent a notification -- Claude
//     Code waiting for the owner, most often.
//   - ended: the shell itself exited.
const (
	TerminalFinished = "terminal.finished"
	TerminalAsks     = "terminal.asks"
	TerminalEnded    = "terminal.ended"
)

// Terminal : One terminal's news, read out of its event.
//
// The fields are the app's own, as it sends them. The tail has had
// anything that looks like a key, a token or a password hidden before it
// left the laptop.
type Terminal struct {
	// Kind : Which of the three it is.
	Kind string
	// ID : The terminal, as the app knows it. Stable while it is open,
	// and what one terminal's news is told apart from another's by.
	ID string
	// Name, Space, Folder : What the owner calls it, the space it is
	// in, and the folder it was in.
	Name   string
	Space  string
	Folder string
	// Command : What ran, when something did.
	Command string
	// Seconds : How long it ran; zero when the app did not say.
	Seconds float64
	// Exit : The shell's exit code, for ended; nil when not given.
	Exit *int
	// Text : The notification's own words, for asks. May be empty.
	Text string
	// Tail : Its last lines of output.
	Tail string
	// Conversation : Claude Code's own record of the conversation in it,
	// sent with its asking: what was asked of it, what it did, what it
	// said. Empty for anything else.
	Conversation string
}

// TerminalOf : The news an event carries, when it is a terminal's.
//
// False for any other kind, and for one with no terminal named: news
// that cannot be told apart from another terminal's cannot be kept to
// one reaction a terminal.
func TerminalOf(e *Event) (Terminal, bool) {
	if e == nil {
		return Terminal{}, false
	}
	switch e.Kind {
	case TerminalFinished, TerminalAsks, TerminalEnded:
	default:
		return Terminal{}, false
	}
	var p struct {
		Terminal string   `json:"terminal"`
		Name     string   `json:"name"`
		Space    string   `json:"space"`
		Cwd      string   `json:"cwd"`
		Command  string   `json:"command"`
		Seconds  *float64 `json:"seconds"`
		Exit     *int     `json:"exit"`
		Text     string   `json:"text"`
		Tail     string   `json:"tail"`
		Conv     string   `json:"conversation"`
	}
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return Terminal{}, false
		}
	}
	t := Terminal{
		Kind:    e.Kind,
		ID:      strings.TrimSpace(p.Terminal),
		Name:    strings.TrimSpace(p.Name),
		Space:   strings.TrimSpace(p.Space),
		Folder:  strings.TrimSpace(p.Cwd),
		Command: strings.TrimSpace(p.Command),
		Exit:    p.Exit,
		Text:    strings.TrimSpace(p.Text),
		Tail:    strings.TrimRight(p.Tail, " \n\t"),

		Conversation: strings.TrimSpace(p.Conv),
	}
	if p.Seconds != nil {
		t.Seconds = *p.Seconds
	}
	if t.ID == "" {
		return Terminal{}, false
	}
	return t, true
}

// Ordinary : Whether this is a shell closing in the ordinary way, which
// is nothing to react to.
//
// A shell that exits cleanly with nothing run is somebody typing exit or
// closing a tab. Ended with a code, or after a command, it may be
// something that stopped which should not have.
func (t Terminal) Ordinary() bool {
	return t.Kind == TerminalEnded && t.Command == "" && (t.Exit == nil || *t.Exit == 0)
}
