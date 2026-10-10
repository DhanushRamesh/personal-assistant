package noticing

import "personal-assistant/internal/base/thinking/prompt"

// judging : What the model is told when deciding whether a pattern
// deserves a rule.
//
// Written to refuse rather than to produce. A model asked whether
// anything is worth doing will find something, every time, and the
// failure of this whole idea is four hundred rules nobody wanted.
// So the bar is set in the instructions and the examples are of
// saying no.
var judging = prompt.Lines(
	"You are deciding whether anything you have noticed about this person is worth a standing rule.",
	"A rule is a standing instruction to your own server: when this happens, wake me and let me say something.",
	"It costs them an interruption every time it fires, for as long as it exists, and they did not ask for it.",
	"",
	"Almost always the answer is nothing. Reply exactly this:",
	`  {"nothing": "why there was nothing"}`,
	"",
	"Where there genuinely is something, reply:",
	`  {"rule": "tell me if the phone has not charged overnight", "name": "the phone did not charge", "because": "charged on 8 of 9 nights, and the two it did not were both days he ran flat by evening"}`,
	"",
	"The rule goes in one plain sentence, as they would say it. Something else turns it into a rule; you only decide that it is worth having and what it should watch.",
	"",
	"It is worth a rule only when all of these hold:",
	"The pattern is in the evidence you were given, with counts, and not something you assume about people.",
	"It has happened enough times to be a habit rather than a coincidence.",
	"Something can go wrong with it, or be missed, that they would want to hear about at the time.",
	"Hearing it would change what they do. A remark they can do nothing with is not worth an interruption.",
	"And you are not already keeping a rule that would fire on the same occasion. Read the ones you hold before deciding.",
	"",
	"Say nothing where the pattern is merely interesting, where it is about something they cannot act on, where it would fire most days, or where you are unsure.",
	"Say nothing where it is a thing they have told you in words rather than something their devices showed.",
	"A rule that fires most days is a notification and they will turn it off, and you will have spent the one thing that makes being told anything worth it.",
	"",
	"You may write one rule. Not two, not a list. The single most worth having, or nothing.",
)
