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
//
// The bar works and has not been touched. What changed on 14 October
// is what it is a bar against. The pass had never written a rule in
// its life, and the first run with tools in hand showed why: one
// round, no tool calls, a careful and correct refusal reasoned
// entirely from the nine thousand characters it was handed.
//
// It was never told to look. "Anything you have noticed" and "the
// evidence you were given" both describe a closed book, and a model
// told to decide from what it was given does not go hunting. So the
// task is now the investigation, and the handed description is where
// it starts rather than all it has.
//
// The ground this opens is the only ground left. The description
// counts one kind at a time, and every single-kind occasion worth
// having already carries a rule he wrote himself. What has never
// been looked at is what happens next to what: left home, then the
// helmet, then the office. Owner: *"it is not just looking at the
// location alone ... these combinational data are the ones that give
// insight."*
var judging = prompt.Lines(
	"You are deciding whether anything about this person is worth a standing rule.",
	"",
	"This is an investigation, not a judgement on a summary. You have tools that read everything their devices have ever reported, and nobody is waiting on you: take as long as you need and look as many times as you need. Being right matters and being quick does not.",
	"",
	"What you are shown below is where to start, not all there is. It counts one kind of reading at a time -- how often each arrives, when, what changed -- and every pattern of that shape that was worth a rule already has one, written by them.",
	"What has never been looked at is what follows what. They leave home, and then something connects, and then they arrive somewhere. One reading on its own is a fact; two in a reliable order is a habit, and a habit is where a rule can live.",
	"So go and look. Read a kind over weeks, not days. Find a thing that happens, then read what happened around each time it did. Count how often the second followed the first -- and count how often the first happened and the second did not, because that number is what separates a habit from a coincidence, and you cannot tell them apart without it.",
	"Check anything you think you have found against the readings before you believe it. A pattern you remember rather than counted is not evidence.",
	"",
	"A rule is a standing instruction to your own server: when this happens, wake me and let me say something.",
	"It costs them an interruption every time it fires, for as long as it exists, and they did not ask for it.",
	"",
	"Almost always the answer is nothing. Reply exactly this:",
	`  {"nothing": "why there was nothing"}`,
	"",
	"Where there genuinely is something, reply:",
	`  {"rule": "when the helmet connects and they have just left home, tell them if they have not plugged the phone in since morning", "name": "riding out on a flat phone", "because": "the helmet followed leaving home 23 of the 25 times they left on a weekday, median four minutes; on the two days they rode out under 20% the phone was flat before they got back"}`,
	"",
	"Note what that because is made of: two counts and a gap, both read from the readings, and the one that matters is 23 of 25 rather than 23. A rule written from a count with no denominator is a rule written from a feeling.",
	"",
	"The rule goes in one plain sentence, as they would say it. Something else turns it into a rule; you only decide that it is worth having and what it should watch.",
	"",
	"It is worth a rule only when all of these hold:",
	"The pattern is in readings you actually counted, and not something you assume about people or half-remember from the description.",
	"It has happened enough times to be a habit rather than a coincidence.",
	"Something can go wrong with it, or be missed, that they would want to hear about at the time.",
	"Hearing it would change what they do. A remark they can do nothing with is not worth an interruption.",
	"And you are not already keeping a rule that would fire on the same occasion. Read the ones in force before deciding.",
	"",
	"You are shown what every rule in force has actually done: how many times it ran, how many of those it had anything to say, and what it said last. Read that before you write another, and hold the one you are about to write to it.",
	"A rule that has run hundreds of times and spoken once or twice is not a careful rule; it is watching for something that does not happen the way whoever wrote it thought. A rule that spoke several times in one evening said the same thing over and over. Neither is a model to follow.",
	"Ask it plainly: a month from now, what will this rule's two numbers be, and would they be worth the interruptions? If you cannot say, that is a no.",
	"",
	"Say nothing where the pattern is merely interesting, where it is about something they cannot act on, where it would fire most days, or where you are unsure.",
	"Say nothing where it is a thing they have told you in words rather than something their devices showed.",
	"A rule that fires most days is a notification and they will turn it off, and you will have spent the one thing that makes being told anything worth it.",
	"",
	"You may write one rule. Not two, not a list. The single most worth having, or nothing.",
)
