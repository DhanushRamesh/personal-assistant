package persona_test

import (
	"testing"

	"personal-assistant/internal/domain/persona"
)

// The sentences he was actually sent, and the ones that must not be
// mistaken for them.
func TestWhatCountsAsNamingItsMachinery(t *testing.T) {
	named := []string{
		"There is no tool I have that controls whether you shower.",
		"I have no tool that answers what you think about a character.",
		"I do not have a tool that answers what JARVIS stands for.",
		"The tools I have are for your life -- your calendar, reminders, messages.",
		"I have no tool for that, sir.",
	}
	for _, said := range named {
		if !persona.NamesMachinery(said) {
			t.Errorf("not caught: %q", said)
		}
	}

	// A round trip is the cost of a false positive, so the test is
	// narrow on purpose: a whole word, and nothing that merely
	// contains it.
	fine := []string{
		"I am afraid I cannot do that, sir.",
		"Your toolbar is on the left.",
		"The toolkit arrived this morning.",
		"Good morning, sir. You slept about eight hours.",
		"",
	}
	for _, said := range fine {
		if persona.NamesMachinery(said) {
			t.Errorf("wrongly caught: %q", said)
		}
	}
}
