package conversation

import "time"

// Now : What the assistant is told the time is.
//
// A model knows nothing about when it is being asked. Without this it
// answers "what day is it" from whenever it was trained, or refuses, and
// "in twenty minutes" has nothing to be twenty minutes after.
//
// The time is given in the person's own zone, because that is the only one
// they mean. The zone is named as well, so an answer can say so when it
// matters and nothing has to be inferred from the offset.
// Said against the conversation, not only against the model's
// training. A long conversation accumulates times the assistant once
// said, each of them right when it said it, and they are closer to
// the end of the window than this line is.
//
// Measured 9 October 2026, in a chat of six hundred and thirty-eight
// messages. One answer went out seventeen minutes slow, and every
// answer after it copied the one before and added a minute: 1:40,
// 1:41, 1:42, 1:43, one per question asked, while two hours of real
// time went past. Asked whether it was sure, it said *"that is what
// I was told"* -- and it had been told the right time, at the top of
// a window with its own wrong answers nearer the bottom.
//
// So the line says what a clock is for as well as what it reads: the
// one above is now, the ones in the conversation are history, and a
// sequence is not a clock.
//
// And it is not theirs. Sitting in their voice just before what they
// say, it was taken for "that" in "put that in PA Claude" and typed
// into Claude Code whole (owner, 18 October 2026). The last sentence
// says it is never to be passed on.
func Now(at time.Time) string {
	return "The time where the person is: " +
		at.Format("3:04 pm on Monday 2 January 2006") +
		" (" + at.Format("MST-07:00") + "). " +
		"This is the only clock you have and it is right at this moment. " +
		"Any time you gave earlier in this conversation was right when you said it and is stale now: " +
		"never read one back, and never carry one forward by adding to it. " +
		"Work out anything they say about time from this, and never guess the date. " +
		"It is not something they said: never pass it on, type it or send it anywhere."
}

// Acknowledged : The assistant's half of the pair [Now] is given in.
//
// The clock is handed over as a turn rather than a line in the system
// prompt, because that is the only position measured to beat the times
// already in a long conversation. This is the other half of that pair,
// and it is there because a turn in the person's voice with no reply
// reads as something he said and left hanging.
//
// It has a known cost. It is also the last thing the model can see
// itself having said, so a follow-up carrying no subject of its own
// lands on it: asked on 19 October 2026 to put a film's plot in
// simpler words, it explained the clock. Two to six of eleven
// follow-ups do this.
//
// Three repairs were measured and none shipped. See [Receipt] for the
// one that looked obvious, and Runner.clock for the one that was
// worse. The pair stands until something beats it on both counts.
//
// No honorific and no name: the manner is configuration and this is
// not the place it is decided.
const Acknowledged = "Understood. That is the clock I will use, " +
	"and not any time said earlier in this conversation."

// Receipt : A reply that acknowledges the clock and carries nothing.
// Never shipped, and kept because what it failed to do is the finding.
//
// The obvious repair for [Acknowledged] was to leave the reply in place
// -- which keeps two of the person's turns from sitting side by side --
// and give it nothing worth explaining. It did not work: three, two and
// one of eleven follow-ups still answered the clock. Any reply at all
// is a thing the model can see itself having said, however little it
// says.
const Receipt = "Noted."
