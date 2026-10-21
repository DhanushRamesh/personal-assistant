package runner

import "testing"

// TestANameTheTurnLeftAlone : the half-done instruction.
//
// Owner, 21 October 2026: *"read the Jarvis Server terminal and ask
// the Laptop client to do that"*. It read the Jarvis Server twice and
// answered with what it found; the laptop client was never typed into
// and never mentioned.
//
// The server already knew the question had named it -- the name
// service found "laptiop client" and wrote it in the log, as it had
// been doing since 19 October against the day there was a use for it.
func TestANameTheTurnLeftAlone(t *testing.T) {
	named := []string{"laptiop client"}
	acted := ` read_terminal {"terminal":"Jarvis Server","space":"Jarvis"}`
	said := "Claude Code is rewriting `read_notes` now. It will show the numbers when done."

	if got := dropped(named, acted, said); got != "laptiop client" {
		t.Errorf("dropped = %q, want the name nothing was done about", got)
	}
}

// TestANameActedOnOrSpokenOfIsNotDropped : the cases that must not fire.
func TestANameActedOnOrSpokenOfIsNotDropped(t *testing.T) {
	for _, c := range []struct {
		what, name, acted, said string
	}{
		// Typed into, though he said it differently.
		{"acted on, misheard", "laptiop client",
			` type_into_terminal {"terminal":"Laptop client","text":"carry on"}`, "Told it, sir."},
		// Searched for: a name acted on is in the query.
		{"carried in a search", "Jailer 2",
			` web_search {"query":"Jailer 2 release date"}`, "The 15th of October, sir."},
		// Not acted on, but answered about, which is doing what was asked.
		{"answered about", "Alekhya",
			` event_recent {"kind":"call"}`, "You called Alekhya at 11:09 pm, sir."},
		// The space named, and read.
		{"a space read", "secure accesss",
			` read_notes {"space":"Secure access"}`, "Three things outstanding, sir."},
	} {
		if got := dropped([]string{c.name}, c.acted, c.said); got != "" {
			t.Errorf("%s: dropped = %q, want nothing", c.what, got)
		}
	}
}

// TestEveryWordOfANameHasToBeThere : "Jarvis Server" is not answered
// by saying "Jarvis".
func TestEveryWordOfANameHasToBeThere(t *testing.T) {
	if got := dropped([]string{"Jarvis Server"}, "", "Jarvis is running, sir."); got == "" {
		t.Error("half a name counted as the whole of it")
	}
	if got := dropped([]string{"Jarvis Server"}, "", "The Jarvis Server is running, sir."); got != "" {
		t.Errorf("a name said in full was called dropped: %q", got)
	}
}

// TestANameInAnotherAlphabet : names are compared as names, so this
// works where a word list would not.
func TestANameInAnotherAlphabet(t *testing.T) {
	if got := dropped([]string{"அலேகியா"}, "", "நான் பார்த்தேன், ஐயா."); got == "" {
		t.Error("a Tamil name the turn left alone was not noticed")
	}
	if got := dropped([]string{"அலேகியா"}, "", "அலேகியா 11 மணிக்கு அழைத்தார்."); got != "" {
		t.Errorf("a Tamil name answered about was called dropped: %q", got)
	}
}
