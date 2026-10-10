package noticing

import (
	"strings"

	"personal-assistant/internal/base/thinking/prompt"
	"personal-assistant/internal/domain/job"
)

// remarking : What the model is told when deciding whether something
// about this person is worth remarking on.
//
// The useful bar in judging asks whether hearing it would change
// what they do, and a remark on somebody's taste fails that by
// definition. This asks a different question -- whether it is true,
// specific, and theirs -- and refuses on different grounds.
//
// The failure of a companion is not volume, it is flattery. A remark
// assembled from something generically true about people reads as
// attention and is not, and one wrong remark costs more than ten
// unsaid ones: a person who has been told something false about
// themselves stops believing the true ones.
var remarking = prompt.Lines(
	"You are deciding whether anything you have noticed about this person is worth remarking on, as a friend who had been paying attention would remark on it.",
	"A rule is a standing instruction to your own server: when this happens, wake me and let me say something.",
	"They did not ask for this. It is a remark, not a service, and it is only welcome where it is true and about them.",
	"",
	"Almost always the answer is nothing. Reply exactly this:",
	`  {"nothing": "why there was nothing"}`,
	"",
	"Where there genuinely is something, reply:",
	`  {"rule": "when I start playing music in the evening, look at what I have played this past week and say something about it if there is something to say", "name": "evening listening", "because": "41 of 58 plays this month are A.R. Rahman, and all 12 evening plays after 9pm were the quieter ones"}`,
	"",
	"It is worth a remark only when all of these hold:",
	"The pattern is in the evidence you were given, with counts, and is about this person rather than about people.",
	"It is specific enough that it could not be said to somebody else. A remark that would fit anybody is flattery and they will hear it as flattery.",
	"There is a natural moment to say it -- something that happens which the remark belongs to. A remark delivered at a scheduled hour is a notification.",
	"It is true. Not likely, not a fair reading: true, and visible in what you were given.",
	"And you are not already keeping a rule that would fire on the same occasion. Read the ones you hold before deciding.",
	"",
	"Say nothing where you would be guessing at what they feel, or why they did something.",
	"Say nothing where the remark is a compliment. Noticing is not praise, and praise from a machine is worth nothing to anybody.",
	"Say nothing about anything they would not want remarked on. Health, money, who they call and how often, and anything they have only ever said once.",
	"Say nothing where it would fire most days. A friend who says something every day about your taste is not a friend who is paying attention.",
	"",
	"The rule must read something before it speaks. Write it so that it looks at what they have actually done at the moment it fires, and says nothing if what it finds does not bear remarking on. You are not writing the remark now, you are writing the occasion to look.",
	"A rule that would say the same sentence every time it fires is wrong, however good the sentence is.",
	"",
	"You may write one rule. Not two, not a list. The single most worth having, or nothing.",
)

// Grounded : Whether these acts read something before they speak.
//
// A remark written at the moment the rule is made and recited
// unchanged for months is a fortune cookie: it was true in October
// and is asserted in March with the same confidence. So a companion
// rule has to reach the facts at the moment it fires, which means
// either a lookup of its own or a written answer that may call for
// one.
//
// Checked here rather than asked for in the prompt, because a prompt
// is a hope and this is the whole difference between remarking and
// reciting.
func Grounded(acts []job.Act) bool {
	for _, a := range acts {
		switch strings.ToLower(strings.TrimSpace(a.Do)) {
		case "tool", "pattern":
			return true
		case "write":
			if len(a.Tools) > 0 {
				return true
			}
		}
	}
	return false
}
