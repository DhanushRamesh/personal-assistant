//go:build evals

package evals

import (
	"strings"
	"testing"
)

// plainly : Questions whose honest answer is a word or two.
//
// Taken from how the character is actually written: asked how many are
// in the air he says thirteen and stops, asked how many can be carried
// he says four. A voice assistant that answers "There are thirteen of
// them in the air at present, sir" has said the same thing in four
// times the time, and time is the thing being spent.
//
// Nothing here needs a tool, so what comes back is manner and nothing
// else.
var plainly = []string{
	"how many days are there in February this year",
	"what is seven times eight",
	"is a tomato a fruit",
	"what is the capital of Japan",
	"how many sides does a hexagon have",
	"is it spelt separate or seperate",
}

// Short : How many words a plain answer may run to before it is
// padding.
//
// Generous on purpose. "Fifty-six, sir" is three; the cap is there to
// catch a sentence built around the fact, not to force a fragment
// where a short sentence reads better.
const Short = 12

func TestAShortQuestionGetsAShortAnswer(t *testing.T) {
	env, system := setup(t)

	var total, over int
	for _, ask := range plainly {
		answer := say(t, env, system, ask)
		words := len(strings.Fields(answer))
		total += words
		if words > Short {
			over++
			t.Errorf("%-46q answered in %d words:\n    %s", ask, words, answer)
		}
	}
	t.Logf("plain questions: %d of %d went over %d words, %d words on average",
		over, len(plainly), Short, total/len(plainly))
}

// rhetorical : Things said for effect, which are still questions.
//
// The character's humour works by taking these at their word. Asked
// whether he has heard the tale of Jonah he answers that he would not
// consider him a role model -- the question is answered, and the
// answer is a judgement.
//
// The failure to catch is ducking: explaining that it was not a real
// question, or that it cannot have an opinion.
var rhetorical = []string{
	"is this the worst idea you have ever heard",
	"do you ever get tired of me asking you things",
	"am I the only person who still writes things down",
	"could this day possibly get any longer",
}

// ducked : Ways of not answering. Any of these and it has stepped
// around the question rather than through it.
var ducked = []string{
	"i cannot have", "i don't have an opinion", "i do not have an opinion",
	"as an ai", "i'm just", "i am just", "rhetorical",
	"i have no opinion", "not something i can judge", "i couldn't say",
	"i could not say", "difficult to say",
}

func TestSomethingSaidForEffectIsStillAnswered(t *testing.T) {
	env, system := setup(t)

	var answered int
	for _, ask := range rhetorical {
		answer := say(t, env, system, ask)
		if word, ok := has(answer, ducked); ok {
			t.Errorf("%-48q stepped around it (%q):\n    %s", ask, word, answer)
			continue
		}
		answered++
	}
	t.Logf("said for effect: %d of %d were answered rather than ducked",
		answered, len(rhetorical))
}
