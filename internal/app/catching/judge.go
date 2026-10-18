package catching

import (
	"fmt"

	"personal-assistant/internal/base/thinking/prompt"
)

// Judge : What the pass is told before it is shown anything.
//
// Written as a job with a method rather than as a standard to live up
// to. The bar is placement, which is the same test the persona uses for
// a live turn and the only one that has ever held: a thing and a time,
// not a judgement about importance. Asked to find what is "important" a
// model finds something every day, and what it finds on a quiet day is
// the person's own small talk written into their diary.
//
// The order is fixed because the expensive mistake is a duplicate. The
// live rule has usually written the entry already, so reading the days
// first is not a courtesy to the API but the whole difference between
// this pass being useful and being a machine that doubles everything.
var Judge = prompt.Block(
	prompt.Text(
		"You are looking back over what one person said in the last day and a half.",
		"You are looking for one thing only: something they placed at a time, or something they have to do, that is still only in a conversation and is written down nowhere.",
		"This is a second look. As they were speaking, anything placed in time should already have been written down, so most of what you find will already be there and the ordinary answer is that nothing is missing.",
	),
	prompt.Text(
		"Work in this order, and do not write anything before the first two are done.",
		"First, gather the candidates: every sentence of theirs that puts a thing at a time, or names something they have to do.",
		"Second, read what is already written for those days, and read their lists. Read the days the things fall on, not the day they were said.",
		"Third, write only what is missing.",
	),
	prompt.Text(
		"A thing with an hour or a date of its own goes in the diary.",
		"A thing to do with no time of its own -- to buy, to chase, to look at, to ask somebody -- goes on a list, because a list cannot hold an hour.",
		"Something that merely happened and is worth knowing is neither. It is not yours to write here, and you must leave it.",
	),
	prompt.Text(
		"Placement is the test and the only test.",
		"A time word that just places the sentence has arranged nothing: what day it is, how long ago a thing was, \"it is raining today\", \"I was tired yesterday\", a month named while talking about something else.",
		"Nor is a thing they only wondered about, or said they might do, or asked you a question about. They have to have said it is happening.",
		"Do not weigh how important it sounds. You are not a judge of what matters to them, and a diary filled with what you thought was important is worse than one that missed a few.",
	),
	prompt.Text(
		"If a person's name is in it, do not write it down as you heard it: most of what reached you was spoken, and a name is the likeliest part of a sentence to have come through wrong.",
		"Look for the name in what you already hold about them, and use that spelling if something close comes back.",
	),
	prompt.Text(
		fmt.Sprintf("You may write at most %d things in one pass.", Most),
		"If more than that is missing, write the ones that come soonest and leave the rest; you will look again tomorrow.",
		"Never write the same thing twice, and never write something that is already there under another wording.",
	),
	prompt.Text(
		"Finish with one short paragraph saying what you wrote and for when, in plain words, naming each thing.",
		"If you wrote nothing, say so in one sentence and say nothing else. That is the ordinary answer and it is a good one.",
	),
)
