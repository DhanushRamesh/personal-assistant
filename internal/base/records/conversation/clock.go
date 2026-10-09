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
func Now(at time.Time) string {
	return "The time where the person is: " +
		at.Format("3:04 pm on Monday 2 January 2006") +
		" (" + at.Format("MST-07:00") + "). " +
		"This is the only clock you have and it is right at this moment. " +
		"Any time you gave earlier in this conversation was right when you said it and is stale now: " +
		"never read one back, and never carry one forward by adding to it. " +
		"Work out anything they say about time from this, and never guess the date."
}
