//go:build evals

package evals

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"personal-assistant/internal/base/platform/config"
	"personal-assistant/internal/domain/persona"
)

// broad : Questions whose honest answer has parts to it.
//
// The ones that went wrong. Asked what it could do, it replied with a
// hundred and twenty words in a single paragraph: fifteen separate
// abilities, no list anywhere in it. Spoken that is the right shape
// and far too long; written it is the wrong shape at any length.
var broad = []string{
	"what can you do",
	"how should I think about backing up my photos",
	"what is the difference between a reminder and a task here",
}

// Heard : How many words a spoken answer to one of those may run to.
//
// A judgement and not a derived limit, and worth saying so: nothing
// truncates a spoken reply. Home Assistant's fifteen-second cap is on
// listening, not on speaking, so the only thing a long answer costs
// is the person's time -- which is the whole point.
//
// Measured before the rule about many-part questions was added,
// "how should I think about backing up my photos" came back at 119
// words -- near fifty seconds of being talked at. With the rule it
// settles at 59 to 66 across runs.
//
// So the cap sits at 75: above where the model now lands, well below
// where it was, and far enough from both that a passing run means the
// rule held rather than that the sampling was kind. A cap the model
// straddles tests nothing but the weather.
const Heard = 75

// wall : A paragraph that works through four sentences without a
// break.
//
// Counted rather than measured in characters, which is what this did
// first and was wrong: a Markdown paragraph is one long line by
// definition, so every correctly formatted answer looked like a wall.
// The rule being tested is the one in the prompt -- more than about
// three sentences in a row -- and that is a count of sentences.
func wall(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		if strings.Count(line, ". ")+strings.Count(line, "? ")+
			strings.Count(line, "! ") >= 4 {
			return true
		}
	}
	return false
}

// Shaped : How long a written answer gets before it owes the reader
// some structure.
const Shaped = 80

// structure : Markdown that gives an answer its shape.
var structure = regexp.MustCompile(`(?m)^\s*(#{1,6}\s|[-*+]\s|\d+\.\s|\|)|\*\*`)

// TestTheSameQuestionIsShapedForTheEar AndTheEye : owner -- *"when I
// chat I read fast, but when Jarvis speaks it should speak less,
// because hearing takes time."*
//
// Both halves are measured against the real model, because the rules
// contradict the manner on purpose and a prompt that merely contains
// a sentence is not a prompt the model obeyed.
func TestTheSameQuestionIsShapedForTheEarAndTheEye(t *testing.T) {
	env, _ := setup(t)
	cfg, err := config.Load("../../../../config.ini", os.LookupEnv)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	spoken := persona.Prompt(cfg.Assistant.Persona, cfg.Assistant.Name, persona.Spoken)
	written := persona.Prompt(cfg.Assistant.Persona, cfg.Assistant.Name, persona.Written)

	for _, ask := range broad {
		heard := say(t, env, spoken, ask)
		read := say(t, env, written, ask)

		words := len(strings.Fields(heard))
		switch {
		case words > Heard:
			t.Errorf("SPOKEN %-46q ran to %d words:\n    %s", ask, words, heard)
		case structure.MatchString(heard):
			t.Errorf("SPOKEN %-46q used markup that cannot be heard:\n    %s", ask, heard)
		}

		// Shape is only owed where there is something to shape.
		// The prompt asks for a list when naming more than two
		// things, not for markup in every answer, and a written
		// reply of two sentences is allowed to be two sentences.
		// Requiring it unconditionally failed on a correct short
		// answer about one run in three.
		switch read := strings.Fields(read); {
		case len(read) >= Shaped && !structure.MatchString(strings.Join(read, " ")):
			t.Errorf("WRITTEN %-45q ran to %d words with no shape at all:\n    %s",
				ask, len(read), strings.Join(read, " "))
		}
		if wall(read) {
			t.Errorf("WRITTEN %-45q contains a wall of prose:\n    %s", ask, read)
		}

		// The claim the whole change rests on, and the one that has
		// held on every run: the same question, answered at more
		// length for somebody who can skim it.
		if len(strings.Fields(read)) <= len(strings.Fields(heard)) {
			t.Errorf("%-46q was no longer written than spoken (%d vs %d words)",
				ask, len(strings.Fields(read)), len(strings.Fields(heard)))
		}

		t.Logf("%-46q heard %d words, read %d words%s",
			ask, words, len(strings.Fields(read)),
			map[bool]string{true: " (longer when read, as intended)"}[len(strings.Fields(read)) > words])
	}
}
