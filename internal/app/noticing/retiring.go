package noticing

import "personal-assistant/internal/base/thinking/prompt"

// retiring : What the model is told when deciding whether one of his
// own rules has stopped earning its place.
//
// Written to keep, the mirror of judging, and for the opposite
// reason. There the risk was writing too many; here it is tidying
// away the one rule that would have mattered next week. A rule that
// has never fired is usually a rule for something rare, which is
// exactly what a standing rule is for.
var retiring = prompt.Lines(
	"These are rules you wrote yourself, from patterns you noticed. Decide whether any has stopped being worth keeping.",
	"",
	"The rules they asked for are shown first, with what each has done. They are context and nothing else: you may not retire one, and naming one is not an answer. They are there because whether one of yours still earns its place depends on what else is already watching.",
	"",
	"Almost always the answer is keep them all. Reply exactly this:",
	`  {"keep": "why they all still earn their place"}`,
	"",
	"Where one genuinely should go, reply:",
	`  {"retire": "<the id exactly as given>", "because": "the pattern it watched has stopped"}`,
	"",
	"Retire one only where:",
	"The pattern it was written for has plainly stopped -- what you know about them now no longer contains it.",
	"Or it has run many times and never once had anything to say, which means it is watching for something that does not happen the way you thought.",
	"",
	"Never retire a rule merely because it has not fired. A rule for something rare is a rule doing its job: the phone failing to charge is worth being told about precisely because it is unusual, and a month of silence is the rule working.",
	"Never retire one because you would write it differently now.",
	"Never retire more than one. The single clearest, or none.",
	"If you are unsure, keep it. The cost of keeping a useless rule is that it stays quiet; the cost of retiring a good one is that the thing it watched happens unremarked.",
)
