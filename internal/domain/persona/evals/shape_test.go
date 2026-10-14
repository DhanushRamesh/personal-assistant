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

// indent : A written answer laid out under the heading, so markdown
// stays readable in the test output.
func indent(text string) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for i, line := range lines {
		lines[i] = "    " + line
	}
	return strings.Join(lines, "\n")
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
const Heard = 45

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
// Sentences : The most a spoken answer may run to.
//
// One, or two where the second carries something the first could not.
// A count rather than a principle, because a principle the model has
// to weigh against the answer in front of it is one somebody hears
// broken once a day: the rule said four, the median answer was two,
// and the tail ran to ten.
const Sentences = 2

// counting : First, Second, Third used to enumerate.
var counting = regexp.MustCompile(`(?i)\s(First|Second|Third|Firstly|Secondly)[,:]\s`)

// marker : A line that is already a markdown list or heading.
var marker = regexp.MustCompile(`^\s*(#{1,6}\s|[-*+]\s|\d+[.)]\s)`)

// enumerated : A list someone has typed into a paragraph.
//
// Only where it runs inside the prose. "1. First, do this" is a
// markdown list whose item happens to begin with the word, and
// flagging that would be flagging the thing the rule asks for. So
// the lines that are already a list are skipped and the rest are
// read; RE2 has no lookbehind, so one pattern cannot tell the two
// apart.
func enumeratedIn(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if marker.MatchString(line) {
			continue
		}
		if found := counting.FindString(line); found != "" {
			return strings.TrimSpace(found)
		}
	}
	return ""
}

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

	// One of his own instead of the three, for trying a sentence
	// both ways without editing the file. The limits still apply:
	// the point of asking is to find out whether it holds.
	//
	//	make evals-shape ASK="what is on my calendar tomorrow"
	questions := broad
	mine := strings.TrimSpace(os.Getenv("ASK"))
	if mine != "" {
		questions = []string{mine}
	}

	for _, ask := range questions {
		heard := say(t, env, spoken, ask)
		read := say(t, env, written, ask)

		// Both answers in full when he asked the question himself.
		// The counts are what the eval judges; the words are what
		// he wanted to see, and a word count is no use for deciding
		// whether a sentence sounds right.
		if mine != "" {
			t.Logf("\n  ASKED    %s\n\n  SPOKEN   (%d words, %d sentences)\n    %s\n\n  WRITTEN  (%d words)\n%s\n",
				ask,
				len(strings.Fields(heard)), sentences(heard), heard,
				len(strings.Fields(read)), indent(read))
		}

		words := len(strings.Fields(heard))
		switch {
		case words > Heard:
			t.Errorf("SPOKEN %-46q ran to %d words:\n    %s", ask, words, heard)
		case sentences(heard) > Sentences:
			t.Errorf("SPOKEN %-46q ran to %d sentences, and the rule is %d:\n    %s",
				ask, sentences(heard), Sentences, heard)
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
		// A list typed into a paragraph. The commonest way a wall
		// appears, and the one the rule had to name outright: asked
		// how to think about backups, it wrote "First, they must be
		// automatic. Second, they must be kept somewhere else.
		// Third, test them" as prose, and the reader has to take it
		// apart again before it is any use.
		if said := enumeratedIn(read); said != "" {
			t.Errorf("WRITTEN %-45q wrote a list as a sentence (%q):\n    %s", ask, said, read)
		}

		// The claim the whole change rests on, and the one that has
		// held on every run: the same question, answered at more
		// length for somebody who can skim it.
		//
		// Only for the three it was measured on. A question of his
		// own may be one where both answers are a line -- "what is
		// 2 plus 2" is not longer for being read -- and failing him
		// for asking would make the switch useless for what it is
		// for.
		if mine != "" {
			continue
		}
		if len(strings.Fields(read)) <= len(strings.Fields(heard)) {
			t.Errorf("%-46q was no longer written than spoken (%d vs %d words)",
				ask, len(strings.Fields(read)), len(strings.Fields(heard)))
		}

		t.Logf("%-46q heard %d words, read %d words%s",
			ask, words, len(strings.Fields(read)),
			map[bool]string{true: " (longer when read, as intended)"}[len(strings.Fields(read)) > words])
	}
}
