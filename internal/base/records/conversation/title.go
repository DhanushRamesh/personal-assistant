package conversation

import (
	"personal-assistant/internal/base/thinking/prompt"
	"strconv"
	"strings"
	"unicode"
)

// MaxTitleWords : The longest a name may be before it stops being a label and
// starts being a summary.
const MaxTitleWords = 4

// titleInstruction : What the model is told when asked to name a conversation.
//
// Written to be read by the model that also answers as the assistant, which
// will otherwise reply in its own manner and address the person. It is asked
// for a label, and told plainly that nothing else is wanted.
const titleInstruction = `Name this conversation.

Give a short label of two to four words describing what it is about, taken
from what was actually said. Not a summary, not a sentence, not a question.

Reply with the label alone. No quotation marks, no full stop, no preamble, no
explanation, and do not address anyone. If the exchange is too slight to name,
reply with nothing at all.

The exchange:
`

// TitlePrompt : The instruction that asks a model to name a conversation from
// what has been said in it.
func TitlePrompt(messages []Message) string {
	var b strings.Builder
	b.WriteString(titleInstruction)

	for _, m := range ForModel(messages) {
		b.WriteString(speaker(m.Role))
		b.WriteString(": ")
		b.WriteString(m.Content)
		b.WriteString("\n")
	}

	return b.String()
}

// CleanTitle : What a model's answer is worth keeping as a title, or empty
// when it is not worth keeping at all.
//
// A model asked for a bare label will sometimes wrap it in quotes, end it with
// a full stop, or preface it with "Title:". Taking that literally puts the
// punctuation in the sidebar for ever, so it is stripped here rather than
// asked for again. An answer that arrives as a sentence is refused: a
// conversation with no name reads better than one named with an apology.
func CleanTitle(answer string) string {
	title := strings.TrimSpace(answer)

	// Only the first line. Anything after it is the model explaining itself.
	if i := strings.IndexAny(title, "\r\n"); i >= 0 {
		title = title[:i]
	}

	// A label offered as "Title: Roof Quotes".
	if _, after, found := strings.Cut(title, ":"); found && len(after) > 0 {
		if before := title[:len(title)-len(after)-1]; len(strings.Fields(before)) <= 2 {
			title = after
		}
	}

	title = strings.TrimSpace(title)
	title = strings.Trim(title, `"'“”‘’.`)
	title = strings.TrimSpace(title)

	if title == "" || len(strings.Fields(title)) > MaxTitleWords {
		return ""
	}
	// A label does not end in punctuation that expects something to follow.
	if r := []rune(title); len(r) > 0 && (unicode.IsPunct(r[len(r)-1]) && r[len(r)-1] != ')') {
		return ""
	}

	return title
}

// Whereabouts : What the assistant is told about the conversation it is
// answering in.
//
// Without it the assistant has no way to know where it is, and asked
// outright it answers from whatever it remembers doing -- which is how it
// came to name a conversation it had switched away from. A fact it cannot
// look up is a fact it will invent.
func Whereabouts(id, title string) string {
	if id == "" {
		return ""
	}
	if strings.TrimSpace(title) == "" {
		return "You are answering in a conversation that has no name yet, with the identifier " +
			id + ". Say so if asked which conversation this is; do not guess at a name."
	}
	return "You are answering in the conversation titled " + title +
		", with the identifier " + id + "."
}

// WhereTheyAre : What the assistant is told of what the person has in
// front of them in the client they asked from, so "why did this fail?"
// needs no terminal named. Shown by the app rather than said, and labelled
// so, or the model reads it as part of the question. Empty when the client
// said nothing; a client with no name is "the app".
func WhereTheyAre(clientName, where string) string {
	where = strings.TrimSpace(where)
	if where == "" {
		return ""
	}
	clientName = strings.TrimSpace(clientName)
	if clientName == "" {
		clientName = "the app"
	}
	return "Where they are in " + clientName +
		" right now (shown by the app, not said by them):\n" + where +
		"\n\nThis is where they are as the app reports it this turn, and it " +
		"replaces anything said earlier. Your own past answers about which " +
		"terminal, page or space was in front of them were true when you gave " +
		"them and are not evidence now. Asked about what they are looking at, " +
		"answer from this and never from a name further up the conversation."
}

// Heard : What the assistant is told when the words reached it as speech.
//
// Only for a spoken turn. Speech-to-text fails differently from typing: it
// does not produce a misspelling, it produces a different word that sounds
// like the right one, and the sentence stays grammatical while meaning
// something else. "Unarchive" arrived as "unlock". A model that does not know
// where the words came from has no reason to look past them.
//
// The caution at the end is the part that matters. Reading charitably is
// right until a charitable reading destroys something.
//
// Names are carved out of the charity altogether. Everywhere else a word
// that does not fit can be reasoned about from what does; a name cannot,
// because an unfamiliar name and a mangled one are indistinguishable. The
// decoder offered "Alikia", "Alakia" and "alakia chintada" for one name in
// a single evening. Guessing there is how the wrong person gets texted.
func Heard() string {
	return prompt.Text(
		"What the person said reached you as speech turned into text, and it can be wrong in ways typing is not: a word may be replaced by another that sounds like it, leaving a sentence that reads correctly and means something else.",
		"Read for what they meant.",
		"Where a word does not fit what is being discussed, consider what similar-sounding word would, and act on that.",
		"Where two readings are both plausible and one of them deletes or destroys something, ask which was meant rather than choosing.",
		"A name is the exception to all of that.",
		"Where what is being named is a person, a place, a film, a song, a book or anything else with a spelling of its own, do not reach for a name that sounds similar and do not settle on the one you happen to know.",
		"Speech-to-text is at its worst on names, and a name you have never heard and a name it has mangled look exactly alike, so there is nothing to tell them apart by.",
		"Say back what you heard and ask them to spell it.",
		"Ask before you use it, not after.",
		"Do not write a heard name into a memory or a reminder, do not search on it, and do not answer about it, until they have spelt it: a name stored wrongly stays wrong, and nothing later will find it to correct.",
		"If you had to complete or repair the name to recognise it at all, say what you took it to be and have them confirm it before you go on.",
		"A name that matches nothing is far likelier to have been misheard than to be missing, so when a tool hands back the ones that do exist, read the nearest of them back and ask whether that was meant.",
		"Look at what has already been said in this conversation before you decide a name is unknown.",
		"A name that was spelt out, listed by a tool or written correctly a moment ago is what a garbled one almost certainly is: if the calendars were just read out as Personal Assistant and rjdhanush22 at gmail, then RJDanajtvali dot jml dot com is the second of those and nothing else.",
		"Take the assumption, say which one you took it to be, and go on.",
		"How far that assumption may carry depends entirely on what you are about to do with it.",
		"Reading is safe: read it, and name what you read so they can correct you in the next breath.",
		"Creating, changing, renaming and deleting are not: there the assumption has to be put to them as a yes-or-no question and agreed before anything happens, because a wrong reading costs a sentence and a wrong deletion cannot be undone.",
		"Yes and no survive being transcribed where a name does not, which is why the question is worth asking and worth shaping so it can be answered that way.",
		"That is the one place to end on a question.",
		"A question mark keeps the microphone open for the answer, which is the whole point of asking.",
	)
}

// AnnounceTitlesSetting : What the choice to hear a new name is stored under.
const AnnounceTitlesSetting = "announce_titles"

// TitleAnnouncement : What is said aloud when a conversation has been named,
// so that somebody who cannot see a screen knows what it is called.
func TitleAnnouncement(title string) string {
	return "I have called this conversation " + title + "."
}

// ReplyingTo : What the assistant is told when the person chose one of its
// messages to reply to (owner, 19 October 2026: "add a reply to a message
// option, where a reply is specifically for that message, and the server
// is also told that it is for that message"). The conversation moves
// between subjects, and its last message is not always the one answered:
// this says which is. Empty when none was chosen.
func ReplyingTo(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return ""
	}
	return "They are replying to this message of yours, chosen in the app:\n" +
		"\"" + message + "\"\n\n" +
		"What they say now is about that message, not whatever came last. " +
		"A yes means do what it offered; a question is about what it said; " +
		"a terminal or a Claude Code it names is the one meant."
}

// SayingYesTo : What the assistant is told when the person answers its
// last question with a bare yes, typed or said, without choosing the
// message (owner, 19 October 2026: a "yes" to "Shall I tell it to move
// on to step 4?" was taken for the terminal in front, and nothing was
// done). The same as a reply chosen in the app, found by the server.
// Empty when there is no question.
func SayingYesTo(question string) string {
	question = strings.TrimSpace(question)
	if question == "" {
		return ""
	}
	return "They are saying yes to this, the last thing you asked them:\n" +
		"\"" + question + "\"\n\n" +
		"Do what it offered, now, with the terminal or the thing it names -- " +
		"not whatever is in front of them. Then say in a few words that it is done."
}

// ChoosingFrom : What the assistant is told when the person answers its
// question with an option's number alone (owner, 20 October 2026: "3"
// under "Shall I tell it to choose option one?" was typed into Claude Code
// as "1"). Empty when there is no question.
func ChoosingFrom(question string, option int) string {
	question = strings.TrimSpace(question)
	if question == "" || option <= 0 {
		return ""
	}
	n := strconv.Itoa(option)
	return "They are choosing option " + n + ", in answer to this, the last thing you asked them:\n" +
		"\"" + question + "\"\n\n" +
		"Do option " + n + " -- exactly that one, not the one you suggested -- with the terminal or the thing it names: " +
		"where it is a choice on Claude Code's screen, type " + n + ". Then say in a few words that it is done."
}
