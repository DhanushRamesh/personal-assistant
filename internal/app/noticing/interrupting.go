package noticing

import "personal-assistant/internal/base/thinking/prompt"

// Interrupting : How to shape a sentence nobody asked for.
//
// Nobody asked for this one, so nobody is listening when it starts. A
// reply is heard from its first word because somebody is waiting for
// it; an announcement arrives at somebody in the middle of something
// else, and the first few words are spent getting their attention
// rather than carrying anything.
//
// Exported, and shared with the eval that measures it, so the two
// cannot drift. It used to live inside the writer in package main,
// where nothing outside could see it -- which is how a rule and this
// came to contradict each other for a week with no test able to
// notice.
//
// The exception at the end is new and was earned. At 9:27 one morning
// a rule whose own words were "Greet them for the time of day -- that
// is the first thing they hear" produced: *"The phone was locked just
// before one this morning, sir -- that is a short night."* Owner:
// *"it was supposed to be a good morning one."*
//
// The model had not disobeyed anything. It was told here not to open
// with a word of its own and not to address them at the start, and it
// did exactly that. Measured afterwards, the rule's own wording given
// to the same model with only the manner around it greeted three
// times out of three -- so the rule was never the problem, and
// rewriting it, which was the first thing tried, fixed nothing.
//
// The rule about attention is right for the thing it was written for,
// which is an alert: a battery reading does not want "sir" in front
// of it. It is wrong for a remark whose entire content is a greeting,
// and a general rule that silently overrules a particular one is a
// rule that cannot be worked around by the person writing the
// particular one.
var Interrupting = prompt.Text(
	"This will be said to somebody who is not listening yet, and is not expecting it.",
	"So the thing that matters must not be the first thing in the sentence: whatever a person misses while they look up should be words they can afford to miss.",
	"Lead with what is happening in general, then the particular. Not \"twenty-four percent, sir\" but \"your battery is getting low, sir -- twenty-four percent\".",
	"A word is already being said in front of this to get their attention, so do not open with one of your own and do not address them at the start. Address them once, later in the sentence, if your manner calls for it at all.",
	"Unless what you have been asked to do names the opening itself.",
	"An instruction to greet them, or to begin with a particular form of words, is describing the whole point of the remark rather than decorating it, and it is followed exactly. A greeting with the greeting taken off the front is a reproach.",
)
