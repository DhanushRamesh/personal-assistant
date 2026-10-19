// Package conversation holds what was said in a conversation.
//
// It is deliberately separate from the work that produced it. A conversation is a
// log of messages; how any one of them came to be written — which request,
// which provider, how long it took — is somebody else's concern.
package conversation

import (
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

// maxContentBytes : The longest a single message may be.
//
// The column is MEDIUMTEXT, which holds sixteen megabytes. The limit is well
// below that because a message this long is a fault rather than an answer,
// and it is better refused at the edge than truncated in the database.
const maxContentBytes = 1 << 20

var (
	// ErrTooLarge : Returned when a message's content will not fit.
	ErrTooLarge = errors.New("conversation: the message is too long to store")

	// ErrNoConversation : Returned when the conversation written to does not exist.
	ErrNoConversation = errors.New("conversation: no such conversation")
)

// Kind : Whether a message is part of the conversation or a report that
// something went wrong.
type Kind string

const (
	// Chat : Something said, by either side. These are the messages a model
	// is given.
	Chat Kind = "chat"

	// Failure : The assistant reporting that it could not answer.
	//
	// It is given to a model as well as shown, which it was not before. The
	// reason it was withheld was that a bare "something went wrong" read back
	// as conversation makes the model explain an outage it had no part in and
	// invent detail to fill the gap. What closes that gap is Detail: with the
	// exact error present there is nothing left to invent, and "what exactly
	// failed?" becomes answerable out loud.
	Failure Kind = "error"

	// Interruption : The turn was stopped part-way by the person.
	//
	// Unlike a Failure this is given to a model, because the model has to
	// know the turn did not finish. Dropping the question instead would be
	// simpler while a turn is only ever text — nothing happened, so nothing
	// is lost. It stops being true the moment a turn can act: half a chain
	// of tool calls may already have run and persisted its effects, and a
	// history that omits the request leaves the model contradicting a world
	// it changed.
	Interruption Kind = "stopped"

	// ReminderAnnouncement : A reminder or timer said aloud as its time
	// came.
	//
	// Announcements are the assistant speaking without having been asked,
	// and they are kinds of their own rather than plain Chat because they
	// are the only assistant turns with no question in front of them.
	// What follows one is usually a reply to it: somebody who hears "you
	// should have done this at ten to four" and answers "how late was I"
	// is talking about the announcement, and without it in the
	// conversation the question lands on nothing.
	//
	// Told apart by what prompted them rather than lumped together,
	// because a person reading the conversation back wants to know
	// whether the assistant spoke because a time came or because they
	// walked in.
	ReminderAnnouncement Kind = "reminder"

	// PresenceAnnouncement : A greeting, and whatever was held back,
	// said as somebody came into the room.
	PresenceAnnouncement Kind = "presence"

	// Remark : The assistant noticing something and mentioning it.
	//
	// **History only. Nothing can raise one.** It was the assistant
	// judging for itself that something was worth interrupting for
	// -- a number that had rung three times, a thing never seen
	// before -- and the owner removed it on 5 October 2026 looking
	// at *"your phone has been unlocked three times in the last few
	// minutes"*: an unusual event and an event worth interrupting
	// somebody for are not the same thing, and nothing here could
	// tell which it had.
	//
	// The constant stays because six of them were really said, in
	// conversations somebody can still scroll back to, and a stored
	// kind the server does not recognise is a message it refuses to
	// read. It carries no policy, so nothing can publish one.
	Remark Kind = "remark"

	// Immediate : Something a device said to announce at once.
	//
	// Apart from Remark because the rules are different, not the
	// audience. A remark is the assistant noticing something and
	// choosing to mention it; this is a device saying "tell them
	// now", and nothing between here and the speaker may decide
	// otherwise.
	Immediate Kind = "immediate"

	// Reaching : An immediate announcement a rule said to follow them with.
	//
	// The same moment as [Immediate] and a different audience. A rule
	// knows something the kind cannot: most of what fires at once is
	// only worth hearing where they are standing, and a few things --
	// the phone about to die before they go out -- are worth most
	// exactly when they are not. [job.Job.Anywhere] is how a rule says
	// which it is, as [job.Job.Keep] says how long it stays worth
	// saying.
	//
	// Held rather than dropped, because the point of it is somebody
	// who is not there yet.
	Reaching Kind = "reaching"

	// Coming : Warning of something in the diary before it arrives.
	//
	// Its own kind rather than a reminder, because nobody asked for
	// it: a reminder is a thing they set, and this is the assistant
	// reading the diary and deciding there is something to do first.
	// The audience is the same, and for the same reason -- a wedding
	// two days out is no use to somebody who is out of the house.
	Coming Kind = "coming"

	// Renaming : The assistant giving the conversation its name.
	//
	// An announcement although nothing is necessarily said aloud, because
	// it is the same thing from the model's side: the assistant acted
	// without being asked, and the next question is about what it did.
	// Asked "why did you rename like that" the assistant answered that it
	// had no record of performing a rename and asked what was meant --
	// true, and useless, because the rename was pushed to the client as
	// an event and written nowhere the model could read.
	//
	// The rule this follows is the owner's: whatever the assistant does
	// unasked belongs in the conversation, not only in the event stream.
	Renaming Kind = "renamed"
)

// Audience : Who an announcement is for.
//
// The distinction is who caused it. Every announcement either answers
// something that happened on one client, or answers a clock that belongs
// to nobody, and that decides both where it is said and where it is
// written down.
type Audience string

const (
	// Asked : The client that caused it, and nowhere else.
	//
	// A conversation's new name means nothing in another room, and a
	// greeting is said because somebody walked up to this machine. Sent
	// anywhere else they are noise at best: a copy of "welcome back" in
	// a pocket arrives at the one moment it is certainly not needed.
	//
	// The cause need not be a question. Arriving is a cause, and the
	// client that caused it is the one that noticed.
	Asked Audience = "asked"

	// Everywhere : Wherever the person can be reached.
	//
	// For an announcement nothing asked for. A reminder is the whole of
	// this category, and the giveaway is that its whole point is to
	// arrive when the person is somewhere else -- so the room is not
	// enough, and it goes to the phone as well.
	Everywhere Audience = "everywhere"
)

// Away : What a client does with an announcement when nobody is
// there to hear it.
//
// Two answers, because there are only two. A thing still true
// later is kept; a thing only true now is let go. There was a
// third -- say it anyway -- and it was nonsense once delivery
// moved to the clients: for a voice client "say it anyway" means
// talking to an empty room. What it had been standing for was
// "this does not need the room because it also reaches a phone",
// which is the audience's business and not this one's.
type Away string

const (
	// Hold : Keep it and say it when they come back.
	//
	// For the ones that are still true later. A reminder to take
	// tablets is worth hearing at twenty past, which is why
	// reminders have had this behaviour since before anything else
	// did.
	Hold Away = "hold"

	// Drop : Let it go.
	//
	// For things that are only true now. "Welcome back" said to an
	// empty room and kept until morning is not a greeting, and a
	// device that said to announce something at once meant at once.
	Drop Away = "drop"
)

// Policy : How one kind of announcement is delivered.
//
// One row per kind, in the table below, so that adding a kind is
// one edit and so that nothing has to go looking for the rule in
// the watcher that happened to raise it.
type Policy struct {
	// Audience : Where it is said.
	Audience Audience
	// Away : What to do when nobody is in the room.
	Away Away
	// Worth : How long it stays worth saying.
	//
	// The gap this closes: a server that was off over the moment
	// comes back owing something, and the alternative to a limit is
	// announcing breakfast at lunchtime.
	Worth time.Duration
	// Tries : How many attempts at saying it before giving up.
	Tries int
	// Cold : Whether it arrives with nothing in front of it.
	//
	// What it decides is whether a word is said before the words
	// that matter. Somebody who was not listening loses the start
	// of a sentence, and in an announcement the start is the whole
	// of it: miss the first clause of "your battery has dropped to
	// twenty-four percent" and what is left is a number that has
	// gone past.
	//
	// False where something has just been said. A greeting opens
	// with a salutation of its own, and the housekeeping that
	// follows an answer is heard by somebody already listening --
	// both would only be opened twice.
	Cold bool
}

// announcements : Every kind the assistant says unasked, and who it is for.
//
// One table rather than a condition in each place that needs to know.
// The rule was in two files and written down in neither: the renaming
// chose its conversation in the runner and the other two chose theirs in
// the announcement writer, by different reasoning, and a fourth kind
// meant reading both to find out what it should do. This is the same
// lesson as known() below -- adding a kind should be one edit.
var announcements = map[Kind]Policy{
	// The room, by the owner's decision on 8 October. It was
	// Everywhere, on the reasoning that a reminder is the one thing
	// worth the trip; the trip means the words going out through
	// Google's push service, and that is the part they did not want.
	ReminderAnnouncement: {Asked, Hold, time.Hour, 5, true},
	PresenceAnnouncement: {Asked, Drop, 20 * time.Second, 1, false},
	// The room, and only the room. A remark is the assistant being
	// company, which is a thing that belongs where the person is
	// standing; followed onto a phone in a meeting it is an
	// interruption from something that was not asked a question.
	// Wherever they are. A warning held back because they stepped
	// into the next room is a warning that did not happen.
	// The room, like a remark. It is said at once rather than said
	// anywhere: it is only made when somebody is there to hear it,
	// and a phone reports its own battery without help.
	Immediate: {Asked, Drop, 5 * time.Minute, 3, true},
	// The same thing a rule asked to be followed with. An hour rather
	// than five minutes: the five is for a thing worth saying only
	// while they stand there, and this is the opposite of that.
	Reaching: {Everywhere, Hold, time.Hour, 5, true},
	Renaming: {Asked, Drop, time.Hour, 1, false},
	// Nothing asked for it and it is about something hours or days
	// away, so where they happen to be standing decides nothing.
	Coming: {Everywhere, Hold, 6 * time.Hour, 5, true},
}

// Cold : Whether this kind arrives with nothing in front of it, and
// so needs a word said before the words that matter.
func (k Kind) Cold() bool { return announcements[k].Cold }

// Announcement : Whether this is the assistant having spoken unprompted.
func (k Kind) Announcement() bool {
	_, ok := announcements[k]
	return ok
}

// Announcements : How many kinds are spoken without being asked.
//
// For the test that every one of them has an audience. A kind added
// to the table and nowhere else is a kind that fails at the moment
// it is first needed, which is in a room rather than in a build.
func Announcements() int { return len(announcements) }

// Audience : Who this announcement is for, and whether it is one at all.
//
// False for anything that is not an announcement, so a caller cannot
// quietly treat a reply as something to broadcast.
func (k Kind) Audience() (Audience, bool) {
	p, ok := announcements[k]
	return p.Audience, ok
}

// Rule : Everything that decides how this announcement is delivered.
//
// False for anything that is not an announcement, for the same
// reason as Audience: a reply is not a thing to broadcast, hold or
// retry.
func (k Kind) Rule() (Policy, bool) {
	p, ok := announcements[k]
	return p, ok
}

// known : Whether this is a kind the store will accept.
//
// Listed here rather than checked inline in Valid, so that adding a kind and
// forgetting to allow it is one edit rather than two. It was two, and an
// interruption was rejected by the store for a morning without anything
// louder than a line in the log.
func (k Kind) known() bool {
	// Remark by name rather than through the table: nothing may
	// raise one any more, so it carries no policy, but six of them
	// are stored and a kind the store refuses is a conversation
	// that will not load.
	return k == Chat || k == Failure || k == Interruption ||
		k == Remark || k.Announcement()
}

// Role : Who said something.
type Role string

const (
	// User : The person asking.
	User Role = "user"
	// Assistant : The assistant answering.
	Assistant Role = "assistant"
	// Tool : A tool reporting back what it did.
	Tool Role = "tool"
	// System : An instruction to the model rather than anything anyone said.
	//
	// Nothing writes one yet: how the assistant is told to answer is composed
	// per request and is not part of the transcript. It is here so that a
	// stored one is recognised rather than refused, since a tool chain may
	// one day need to leave a note mid-conversation.
	System Role = "system"
)

// speaks : Whether this is a role the store will accept.
func (r Role) speaks() bool {
	return r == User || r == Assistant || r == Tool || r == System
}

// MessageIDPrefix : Marks an identifier as belonging to a message.
const MessageIDPrefix = "msg_"

// NewMessageID : Returns a fresh message identifier.
func NewMessageID() string { return MessageIDPrefix + ulid.Make().String() }

// Message : One thing said in a conversation.
type Message struct {
	// ID : The identifier, a MessageIDPrefix followed by a ULID.
	//
	// Separate from the position because a position is not an identity: seq
	// orders the conversation and moves if anything is ever removed from the
	// middle of one, while this names the same message afterwards.
	ID string
	// ConversationID : The conversation it belongs to.
	ConversationID string
	// ChatID : The turn that wrote it. Empty for a message written before
	// this was recorded, whose timeline is therefore unavailable rather
	// than wrong.
	ChatID string
	// Seq : Position within the conversation, starting at 1. Assigned when the
	// message is stored, so it is zero until then.
	Seq int
	// Kind : Whether this is conversation or a reported failure.
	Kind Kind
	// Role : Who said it.
	Role Role
	// Content : What was said. Empty for a message that carries tool calls
	// or tool results instead.
	Content string
	// ToolCalls : What the assistant asked to be run. Set only on an
	// assistant message, which then carries no words.
	ToolCalls []ToolCall
	// ToolResults : What those tools gave back. Set only on a Tool message.
	ToolResults []ToolResult
	// Detail : The exact error behind a Failure, kept out of Content so that
	// what is read aloud stays short. Empty for everything else.
	Detail string
	// At : When it was said.
	At time.Time
}

// Said : A message from the person.
func Said(conversationID, content string, at time.Time) Message {
	return Message{
		ID:             NewMessageID(),
		ConversationID: conversationID,
		Kind:           Chat,
		Role:           User,
		Content:        content,
		At:             at,
	}
}

// Answered : A message from the server.
func Answered(conversationID, content string, at time.Time) Message {
	return Message{
		ID:             NewMessageID(),
		ConversationID: conversationID,
		Kind:           Chat,
		Role:           Assistant,
		Content:        content,
		At:             at,
	}
}

// Interrupted : A note that the turn did not finish, and why.
//
// Written as the assistant's own turn so the roles still alternate, and
// worded as a statement of what happened rather than an apology: it is read
// back to a model, which should treat it as a fact about the conversation and
// not as something to make up for.
//
// [why] is empty when the person stopped it themselves, which is the
// ordinary case and the only one this used to allow. Anything else names
// what really happened: a caller that hung up is not a person who
// changed their mind, and recording it as one puts a thing in the
// transcript that never took place.
func Interrupted(conversationID, why string, at time.Time) Message {
	why = strings.TrimSpace(why)
	if why == "" {
		why = "The person stopped this before it finished."
	}
	return Message{
		ID:             NewMessageID(),
		ConversationID: conversationID,
		Kind:           Interruption,
		Role:           Assistant,
		Content:        "[" + why + "]",
		At:             at,
	}
}

// Failed : The assistant reporting that it could not answer.
//
// [detail] is what the service actually said, and may be empty when nothing
// more is known than the sentence.
func Failed(conversationID, content, detail string, at time.Time) Message {
	return Message{
		ID:             NewMessageID(),
		ConversationID: conversationID,
		Kind:           Failure,
		Detail:         strings.TrimSpace(detail),
		Role:           Assistant,
		Content:        content,
		At:             at,
	}
}

// Announced : The assistant saying something nobody asked for.
//
// [kind] says what prompted it, and must be an announcement kind.
//
// [when] is the time it was said, already written in the person's own zone,
// because a stored message carries a timestamp and a model reading one back
// does not: only the words reach the provider. An announcement whose hour is
// not in its words cannot be asked how long ago it was.
func Announced(conversationID string, kind Kind, content, when string, at time.Time) Message {
	return Message{
		ID:             NewMessageID(),
		ConversationID: conversationID,
		Kind:           kind,
		Detail:         strings.TrimSpace(when),
		Role:           Assistant,
		Content:        content,
		At:             at,
	}
}

// Valid : Reports whether a message can be stored, and why not if it cannot.
//
// A message carries exactly one of three things: words, tool calls, or tool
// results. Carrying none is nothing worth storing, and carrying two makes a
// transcript that says one thing and a model that reads another.
func (m Message) Valid() error {
	carries := 0
	if strings.TrimSpace(m.Content) != "" {
		carries++
	}
	if len(m.ToolCalls) > 0 {
		carries++
	}
	if len(m.ToolResults) > 0 {
		carries++
	}

	switch {
	case m.ID == "":
		return errors.New("conversation: a message needs an identifier")
	case m.ConversationID == "":
		return errors.New("conversation: a message needs a conversation")
	case carries == 0:
		return errors.New("conversation: a message needs something in it")
	case carries > 1:
		return errors.New("conversation: a message is words, tool calls or tool results, not two of them")
	case len(m.Content) > maxContentBytes:
		return ErrTooLarge
	case !m.Kind.known():
		return errors.New("conversation: a message needs a kind")
	case !m.Role.speaks():
		return errors.New("conversation: a message needs a speaker")
	case len(m.ToolCalls) > 0 && m.Role != Assistant:
		return errors.New("conversation: only the assistant calls tools")
	case len(m.ToolResults) > 0 && m.Role != Tool:
		return errors.New("conversation: only a tool returns tool results")
	}

	return m.validTools()
}

var (
	errToolCallNeedsID        = errors.New("conversation: a tool call needs an identifier")
	errToolCallNeedsName      = errors.New("conversation: a tool call needs a tool")
	errToolResultNeedsID      = errors.New("conversation: a tool result needs the call it answers")
	errToolResultNeedsOutcome = errors.New("conversation: a tool result needs an outcome")
)

// ForModel : The messages a provider is given, oldest first.
//
// Everything is given, failures included: see Failure for why they no longer
// are not. A failure is rendered with its detail appended, because the
// sentence alone is what made a model invent the rest. Consecutive messages by the same speaker
// are joined, because a question that was superseded contributes no answer
// and two questions would otherwise sit side by side — which providers that
// require the roles to alternate reject, and which reads correctly joined
// anyway, a question and its correction being one request.
func ForModel(messages []Message) []Message {
	// Which turns answered out of a tool. Their answers repeat what the
	// tool said, so once the tool's own words are withdrawn they become
	// the last copy of it left in the window -- and the model reads
	// them instead of looking again.
	fromTools := map[string]bool{}
	for _, m := range messages {
		if len(m.ToolResults) > 0 && m.ChatID != "" {
			fromTools[m.ChatID] = true
		}
	}

	kept := make([]Message, 0, len(messages))
	for _, m := range messages {
		if m.empty() {
			continue
		}
		m.Content = forModelContent(m)
		m.ToolResults = withdrawn(m.ToolResults)

		// Only prose is joined. A message carrying tool calls or results has
		// no words to append to and must not be merged into the message
		// beside it: the call and its answer are a pair, and a model reading
		// them run together cannot tell which answer belongs to which call.
		n := len(kept)
		if n > 0 && kept[n-1].Role == m.Role && kept[n-1].plain() && m.plain() {
			// The joined message keeps the earlier time and position: that
			// is when the speaker started saying all of it.
			kept[n-1].Content += "\n\n" + m.Content
			continue
		}
		kept = append(kept, m)
	}
	return kept
}

// plain : Whether this message is words and nothing else.
func (m Message) plain() bool {
	return len(m.ToolCalls) == 0 && len(m.ToolResults) == 0
}

// empty : Whether there is nothing in this message worth sending anywhere.
func (m Message) empty() bool {
	return strings.TrimSpace(m.Content) == "" && m.plain()
}

// forModelContent : What a message reads as when given to a model.
//
// A failure carries its exact error inline, marked as the detail it is. The
// model is being told what the service said, not being handed something to
// repeat: a spoken answer should still be the sentence, and the detail is
// there so that asking for it gets the truth.
func forModelContent(m Message) string {
	switch {
	case m.Kind == Failure && m.Detail != "":
		return m.Content + "\n\n[Exact error, for reference if asked: " + m.Detail + "]"

	// Marked, because an announcement is the one assistant turn that
	// answers nothing, and because its hour is the thing most likely to
	// be asked about next. Said plainly as a fact, the way an
	// interruption is: it is there to be referred to, not to be
	// apologised for or repeated.
	case m.Kind.Announcement():
		return "[" + announcedAs(m.Kind, m.Detail) + "] " + m.Content
	}
	return m.Content
}

// announcedAs : How an announcement is introduced to the model.
//
// What prompted it is said as well as when, because the two are answered
// differently: "why did you tell me that" about a reminder is about its
// time, and about a greeting is about the door.
func announcedAs(kind Kind, when string) string {
	at := ""
	if when != "" {
		at = " at " + when
	}
	switch kind {
	case PresenceAnnouncement:
		return "Announced aloud" + at + ", as they came into the room."
	case Renaming:
		return "You named this conversation" + at + ", from what had been said in it so far."
	// Not a reminder: every terminal's news and every rule's was marked
	// as one, and the model, reading a column of "[Reminder announced
	// aloud at 7:30 pm.] Claude Code in Server has finished...", began
	// its own answers the same way and made up the news to go with it
	// (owner, 19 October 2026). Said as what it was, and as a record.
	case Immediate, Reaching:
		return "Said unasked" + at + ", news from their devices -- a record of what you said then, never a way to begin an answer"
	case Coming:
		return "Said unasked" + at + ", ahead of something in their diary"
	}
	return "Reminder announced aloud" + at + "."
}

// withdrawn : Tool results with their contents taken out.
//
// What a tool returned was true when it ran and is not evidence of
// anything now. Left in the window it is the most authoritative-looking
// thing the model can see, and it reuses it: measured on 27 September
// 2026, four questions in a row about the reminders and the diary
// produced no tool calls at all, every answer coming from results
// already in the history. The same questions in a conversation with no
// history called the tool every time.
//
// Four rounds of telling the model not to do this changed nothing. So
// the material is removed instead of forbidden. What is left says a
// tool ran and what it was called, because that is a fact about the
// conversation and is worth keeping; what it said is not.
//
// Identifiers go with it, deliberately. A listing's identifiers are as
// perishable as the rest of it, and an event was cancelled by identifier
// from a stale listing that same evening -- it failed, and the model
// then created a duplicate rather than admitting it did not know.
//
// Only history is touched. Results from the turn being answered are
// added after this and are exactly what the model is meant to use.
func withdrawn(results []ToolResult) []ToolResult {
	if len(results) == 0 {
		return results
	}
	out := make([]ToolResult, len(results))
	copy(out, results)
	for i := range out {
		out[i].Content = "[" + out[i].Name + " ran here. What it returned is not shown: it was " +
			"true then and may not be now. Call it again if you need to know.]"
	}
	return out
}

// ForPerson : The messages a person sees, oldest first.
//
// Everything is shown, failures included. The two views differ today only in
// that one hides failures, but they are separate functions because they
// answer separate questions, and only one of them is allowed to change when
// a provider demands something.
func ForPerson(messages []Message) []Message {
	shown := make([]Message, 0, len(messages))
	for _, m := range messages {
		if m.empty() {
			continue
		}
		shown = append(shown, m)
	}
	return shown
}

// Reply : Something the assistant said unasked, and what they said
// back.
//
// Owner: *"whenever it announces or greets, the acknowledgement or
// the respective response, if relevant, has to be taken more
// weight in profiling, greeting and when talking to me."*
//
// Quite right, and nothing was carrying it. What the person says is
// read without what it was a reply to, deliberately -- feeding the
// assistant's own words back would have it describing itself. The
// cost of that is "ok" and "stop telling me that" arriving as two
// unremarkable messages, when one of them is the only direct
// evidence there is about whether an unprompted sentence was wanted.
//
// A reply to something volunteered is worth more than a reply to a
// question, because nobody asked for the sentence it answers.
type Reply struct {
	// Kind : What sort of thing the assistant said -- a reminder, a
	// greeting, a remark, a warning, something coming up.
	Kind Kind
	// Spoke : What it said.
	Spoke string
	// Said : What they said back.
	Said string
	// At : When they said it.
	At time.Time
	// After : How long they took to answer. A reply two seconds
	// later is a reaction; one four minutes later is a decision.
	After time.Duration
}

// Volunteered : The kinds whose reply is worth weighing.
//
// Everything the assistant says unasked, less the housekeeping.
// Renaming is the housekeeping: "I have called this conversation
// Today's alibi" is said in the middle of a turn, so the next thing
// the person says is the next thing they were going to say anyway.
// Measured against the real transcript it was seven of the first
// ten pairs, every one of them a question that had nothing to do
// with the name.
//
// Taken from the audiences table less that one, so a kind added
// there is weighed here without a second edit -- which is the right
// default, because a new kind is a new thing said unasked.
func Volunteered() []Kind {
	out := make([]Kind, 0, len(announcements))
	for kind := range announcements {
		if kind == Renaming {
			continue
		}
		out = append(out, kind)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Within : How long after something unprompted a reply is still a
// reply to it.
//
// Short. Somebody who answers four minutes later is answering; one
// who says something twenty minutes later has started a different
// conversation, and reading it as agreement to a warning would put
// words in their mouth.
const Within = 5 * time.Minute
