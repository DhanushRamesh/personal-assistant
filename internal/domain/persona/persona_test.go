package persona_test

import (
	"strings"
	"testing"

	"personal-assistant/internal/domain/persona"
)

// Every persona has to be describable, since a listing offers them by name
// and summary and an empty one is a row nobody can choose between.
func TestEveryPersonaIsDescribed(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range persona.All() {
		switch {
		case p.ID == "":
			t.Error("a persona has no identifier")
		case p.Name == "":
			t.Errorf("%s has no name", p.ID)
		case p.Summary == "":
			t.Errorf("%s has no summary", p.ID)
		}
		if seen[p.ID] {
			t.Errorf("%s is listed twice", p.ID)
		}
		seen[p.ID] = true
	}
}

// The default has to exist, or choosing nothing chooses something missing.
func TestTheDefaultExists(t *testing.T) {
	p, ok := persona.Find(persona.Default)
	if !ok {
		t.Fatalf("the default persona %q is not in the registry", persona.Default)
	}
	if p.Manner != "" {
		t.Error("the default asks for a manner, so an assistant has one before anyone chose it")
	}
}

// The spoken rules come last, so a manner cannot talk its way past them.
func TestTheSpokenRulesComeAfterTheManner(t *testing.T) {
	prompt := persona.Prompt("jarvis", "Jarvis", persona.Spoken)

	manner := strings.Index(prompt, "butler")
	rules := strings.Index(prompt, "read aloud")
	if manner < 0 || rules < 0 {
		t.Fatalf("prompt is missing the manner or the rules: %q", prompt)
	}
	if rules < manner {
		t.Error("the spoken rules are read before the manner, so the manner overrides them")
	}
}

// Whatever the manner asks for, a reply must not end on a question: Home
// Assistant reads the last character to decide whether to keep listening.
func TestEveryPersonaIsForbiddenToEndOnAQuestion(t *testing.T) {
	for _, p := range persona.All() {
		prompt := persona.Prompt(p.ID, "Jarvis", persona.Spoken)
		if !strings.Contains(prompt, "Never end a reply with a question") {
			t.Errorf("%s may end a reply with a question", p.ID)
		}
	}
}

// The assistant answers to the name it is configured with, not to the name of
// the persona: the owner chooses what it is called and the same binary has to
// serve whatever that is.
func TestThePromptUsesTheConfiguredName(t *testing.T) {
	for name, want := range map[string]string{
		"Jarvis":  "You are Jarvis, a personal assistant.",
		"Friday":  "You are Friday, a personal assistant.",
		"  Ada  ": "You are Ada, a personal assistant.",
	} {
		if got := persona.Prompt("friday", name, persona.Spoken); !strings.HasPrefix(got, want) {
			t.Errorf("Prompt(friday, %q) = %q, want it to start %q", name, got, want)
		}
	}

	// Nameless is a working assistant, not a broken one: it simply never
	// says what it is called.
	if got := persona.Prompt("jarvis", "", persona.Spoken); !strings.HasPrefix(got, "You are a personal assistant.") {
		t.Errorf("an unnamed assistant reads as %q", got)
	}
}

// Whatever the manner, the instructions that keep a reply speakable survive.
func TestEveryPromptStaysSpeakable(t *testing.T) {
	for _, p := range persona.All() {
		prompt := persona.Prompt(p.ID, "Jarvis", persona.Spoken)
		for _, must := range []string{"read aloud", "Do not use markdown"} {
			if !strings.Contains(prompt, must) {
				t.Errorf("%s lost %q", p.ID, must)
			}
		}
	}
}

// A nameless assistant answering plainly must not name itself. The name is
// configuration, and one written into the prompt would be wrong the moment it
// changed. A manner may name a character it is told never to mention, which
// is the opposite of hardcoding one.
func TestNoAssistantNameIsHardcoded(t *testing.T) {
	plain := persona.Prompt(persona.Default, "", persona.Spoken)
	for _, forbidden := range []string{"FRIDAY", "Friday", "JARVIS", "Jarvis"} {
		if strings.Contains(plain, forbidden) {
			t.Errorf("%q is hardcoded in %q", forbidden, plain)
		}
	}
}

// A persona nobody recognises answers plainly rather than stopping the
// server: a settings row with a stale word in it is not a reason to refuse
// to work.
func TestAnUnknownPersonaAnswersPlainly(t *testing.T) {
	if _, ok := persona.Find("alfred"); ok {
		t.Fatal("alfred should not be in the registry")
	}

	unknown := persona.Prompt("alfred", "Jarvis", persona.Spoken)
	plain := persona.Prompt(persona.Default, "Jarvis", persona.Spoken)
	if unknown != plain {
		t.Errorf("an unknown persona gives %q, want the plain prompt", unknown)
	}
}

// The two characterful ones have to be told the things that keep them from
// breaking character or performing it.
func TestTheCharactersAreHeldToTheirManner(t *testing.T) {
	for id, address := range map[string]string{"jarvis": "sir", "friday": "boss"} {
		p, ok := persona.Find(id)
		if !ok {
			t.Fatalf("%s is missing", id)
		}
		if !strings.Contains(p.Manner, address) {
			t.Errorf("%s is not told to say %q", id, address)
		}
		if !strings.Contains(p.Manner, "Do not act out a role") {
			t.Errorf("%s may perform its manner rather than have it", id)
		}
		if !strings.Contains(p.Manner, "Tony Stark") {
			t.Errorf("%s is not forbidden from mentioning the films", id)
		}
	}
}

// The tools are the only way it acts, and it has to be told so. Without
// it, asked to add milk to a shopping list it has no tool for, it replied
// that milk had been added and called nothing.
func TestEveryPersonaIsToldItActsOnlyThroughTools(t *testing.T) {
	for _, p := range persona.All() {
		got := persona.Prompt(p.ID, "Jarvis", persona.Spoken)

		for _, want := range []string{
			"You act only through the tools you are given",
			"Never say you have done something",
			"say plainly that you cannot do it",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("%s is missing %q", p.ID, want)
			}
		}
	}
}

// A refusal can be worked around; a false success cannot even be noticed.
// The prompt says which is worse, because the model has to choose.
func TestItIsToldWhichFailureIsWorse(t *testing.T) {
	got := persona.Prompt(persona.Default, "Jarvis", persona.Spoken)

	if !strings.Contains(got, "always better than saying you have when you have not") {
		t.Errorf("the prompt does not say which way to err:\n%s", got)
	}
}

// TestASentenceWrittenByTheServerSoundsLikeTheSameAssistant : Without
// this, a server-written line lands in the middle of a conversation
// where every other sentence has called the person sir.
//
// The failure case is the clearest one: the single reply the model had
// no hand in is the one that sounds like somebody else.
func TestASentenceWrittenByTheServerSoundsLikeTheSameAssistant(t *testing.T) {
	for _, c := range []struct {
		what, sentence, already, want string
	}{
		{"before the full stop", "That could not be done.", "",
			"That could not be done, sir."},
		{"with no full stop", "Reading your reminders", "",
			"Reading your reminders, sir"},
		{"already said in the reply", "That could not be done.",
			"I am afraid not, sir.", "That could not be done."},
		{"already said, whatever the case", "That could not be done.",
			"I am afraid not, SIR.", "That could not be done."},
		{"a question keeps its mark", "Shall I?", "", "Shall I?, sir"},
		{"nothing to say", "", "", ""},
	} {
		t.Run(c.what, func(t *testing.T) {
			if got := persona.Addressed(c.sentence, "sir", c.already); got != c.want {
				t.Errorf("Addressed(%q, sir, %q) = %q, want %q",
					c.sentence, c.already, got, c.want)
			}
		})
	}

	// A persona that calls them nothing leaves the sentence alone.
	if got := persona.Addressed("That could not be done.", "", ""); got != "That could not be done." {
		t.Errorf("a persona with no address changed the sentence to %q", got)
	}
	if got := persona.Addressed("That could not be done.", "   ", ""); got != "That could not be done." {
		t.Errorf("whitespace was treated as an address, giving %q", got)
	}
}

// TestWhatEachPersonaCallsThem : And that an identifier nobody knows
// is answered with nothing rather than an error, because an assistant
// that answers plainly is a working assistant.
func TestWhatEachPersonaCallsThem(t *testing.T) {
	for _, p := range persona.All() {
		if got := persona.AddressFor(p.ID); got != p.Address {
			t.Errorf("AddressFor(%q) = %q, want %q", p.ID, got, p.Address)
		}
	}
	for _, unknown := range []string{"", "nobody", "SIR", "jarvis "} {
		if got := persona.AddressFor(unknown); got != "" {
			t.Errorf("AddressFor(%q) = %q, want nothing", unknown, got)
		}
	}
}

// TestASpokenReplyAndAWrittenOneAreShapedDifferently : owner, on a
// hundred and twenty words of unbroken prose on a screen --
// *"when I chat I read fast, but when Jarvis speaks it should speak
// less, because hearing takes time."*
//
// The brevity in every manner here is the price of being listened to.
// Charging it to somebody reading was costing the shape of the answer
// and buying nothing.
func TestASpokenReplyAndAWrittenOneAreShapedDifferently(t *testing.T) {
	spoken := persona.Prompt("jarvis", "Jarvis", persona.Spoken)
	written := persona.Prompt("jarvis", "Jarvis", persona.Written)

	if !strings.Contains(spoken, "Do not use markdown") {
		t.Error("a reply that will be read aloud was not told to avoid markdown")
	}
	if !strings.Contains(spoken, "Hearing takes time") {
		t.Error("nothing tells it why brevity costs more out loud")
	}

	if strings.Contains(written, "Do not use markdown") {
		t.Errorf("a reply that will be read on a screen was told to avoid markdown:\n%s", written)
	}
	if !strings.Contains(written, "Use markdown") {
		t.Error("a written reply was not told to shape the answer")
	}
	if !strings.Contains(written, "Never a wall of prose") {
		t.Error("nothing forbids the wall of prose this was written for")
	}
}

// TestTheMannersSentenceLimitIsLiftedInWriting : "Keep to one to four
// sentences" is in every manner, and a manner is tone and bearing.
// A sentence count is not bearing, it is a rule about being heard --
// so in writing the rules after it have to say so out loud, since
// they cannot reach into the manner to remove it.
func TestTheMannersSentenceLimitIsLiftedInWriting(t *testing.T) {
	for _, p := range persona.All() {
		if p.Manner == "" {
			continue
		}
		written := persona.Prompt(p.ID, "Jarvis", persona.Written)
		if !strings.Contains(written, "does not apply here") {
			t.Errorf("%s: the manner caps the length and nothing lifts it in writing", p.ID)
		}
	}
}

// TestTheMannerIsTheSameEitherWay : only the shape of the reply
// changes. Somebody who addresses them as sir out loud and not in
// writing is two assistants.
func TestTheMannerIsTheSameEitherWay(t *testing.T) {
	for _, p := range persona.All() {
		if p.Manner == "" {
			continue
		}
		for _, how := range []persona.Reading{persona.Spoken, persona.Written} {
			if got := persona.Prompt(p.ID, "Jarvis", how); !strings.Contains(got, p.Manner) {
				t.Errorf("%s: the manner is missing when the reply is %v", p.ID, how)
			}
		}
	}
}

// TestNeitherEndsOnAQuestion : the rule against finishing with an
// offer of further help belongs to both. Its reason for avoiding the
// punctuation is the microphone; its reason for existing is that an
// answer followed by "anything else?" is an answer that will not stop.
func TestNeitherEndsOnAQuestion(t *testing.T) {
	for _, how := range []persona.Reading{persona.Spoken, persona.Written} {
		got := persona.Prompt("jarvis", "Jarvis", how)
		if !strings.Contains(got, "Never end a reply with a question") {
			t.Errorf("a reply that is %v may end by offering further help", how)
		}
	}
}
