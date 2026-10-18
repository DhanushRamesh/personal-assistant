// Package announcement records what the assistant said without having been
// asked.
//
// Everything else the assistant says is written down as part of answering:
// a question arrives, a turn runs, and both halves land in the
// conversation. A reminder falling due and a greeting at the door go
// straight to the speaker and were never written anywhere, so the next
// thing the person said arrived in a conversation that showed no sign of
// them having been spoken to at all.
//
// That is fine until they answer it. "How late was I" after "you should
// have done this at ten to four" is a reply, and a reply to nothing cannot
// be understood. This puts the announcement in front of it.
package announcement

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"personal-assistant/internal/base/records/client"
	"personal-assistant/internal/base/records/store"
	"regexp"
	"strings"
	"time"

	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/base/records/conversation"
	"personal-assistant/internal/base/world/announce"
)

// address : What the assistant calls the person, for the word said
// in front of a cold announcement.
//
// Set once at startup rather than passed down. The persona is one
// configured value for the whole server, and the five places that
// call Speak -- a reminder falling due, a device reporting
// something, a rule, a greeting, the renaming -- have no other
// reason to know a persona exists. Empty until set, which means no
// opening, which is what an unconfigured server did before this.
var address string

// Addressing : Sets what the person is called.
func Addressing(a string) { address = strings.TrimSpace(a) }

// Opening : The word said before an announcement, or empty.
//
// Owner, 7 October 2026: *"I'll be doing something and suddenly
// Jarvis is saying the important stuff fast before I even start to
// listen."*
//
// A reply is heard because they asked and are waiting for it. An
// announcement arrives at somebody mid-sentence in another
// conversation, and the first second of it is spent getting their
// attention rather than carrying anything. So the first second is
// given something it does not matter if they miss.
//
// Their own form of address and nothing invented: it is the one
// word the assistant already says to them, it carries no
// information, and a persona that calls them nothing gets no
// opening rather than a line of English it would never say.
//
// The greeting has had this since it was written -- "Good evening,
// sir." in front of whatever the model wrote -- and it is the
// reason a greeting is easy to follow and an announcement is not.
func Opening(kind conversation.Kind) string {
	if !kind.Cold() || address == "" {
		return ""
	}
	return strings.ToUpper(address[:1]) + address[1:] + "."
}

// Spoken : The announcement as it will be heard.
//
// Exported so that what is said can be tested without a speaker, a
// network and a satellite between the test and the sentence.
func Spoken(kind conversation.Kind, text string) string { return opened(kind, text) }

// opened : The announcement with its opening in front of it, and
// said once.
//
// Owner, hearing one: *"while announcing it is using twice sir --
// like 'Sir, the battery is 25%, sir'."*
//
// Both halves were obeying their own instruction. The persona says
// to address the person once in each reply and never twice, and the
// model does. The opening is added afterwards, by something the
// model cannot see, and the two of them together say it twice.
//
// The opening is the half that stays. It exists because the first
// second of an announcement is spent getting somebody's attention
// rather than carrying anything, and a word at the end of a
// sentence cannot do that job. So the sentence gives its copy up.
func opened(kind conversation.Kind, text string) string {
	lead := Opening(kind)
	text = strings.TrimSpace(text)
	if lead == "" || text == "" {
		return text
	}
	return lead + " " + once(text)
}

// said : The form of address as a word in a sentence, with whatever
// punctuation is hanging off it.
//
// Matched on a word boundary so that a place or a name containing
// the letters is left alone.
var said = regexp.MustCompile(`(?i)\s*,?\s*\b` + regexp.QuoteMeta(addressOf()) + `\b\s*,?`)

// addressOf : The form of address, for building the expression
// above. A separate function because package-level variables are
// built in the order they are written and address is set later, by
// Addressing.
func addressOf() string {
	if address != "" {
		return address
	}
	// Every persona that has one uses this, and a persona with none
	// gets no opening at all, so this expression is never consulted.
	return "sir"
}

// once : The sentence with the form of address taken out of it.
//
// Taken out rather than only trimmed from the end. It lands in the
// middle as often as not -- "The silence from your phone, sir, has
// now stretched to three hours" -- and that one is a second "sir"
// just as much as a trailing one is.
//
// The punctuation around it goes with it, and the sentence is left
// starting with a capital, because "Sir. , the battery" and "sir,
// the battery has dropped" are both worse than what they replaced.
func once(text string) string {
	out := said.ReplaceAllString(text, " ")
	// The space the replacement leaves behind, where the address sat
	// against the punctuation that followed it. Without this, "fifty
	// percent, sir." becomes "fifty percent ." and the satellite
	// reads the gap aloud as a pause.
	for _, tight := range []string{" .", " ,", " !", " ?", " ;"} {
		out = strings.ReplaceAll(out, tight, tight[1:])
	}
	out = strings.Join(strings.Fields(out), " ")
	out = strings.TrimLeft(out, " ,.")
	if out == "" {
		// The whole of it was the address. Nothing is better than an
		// opening with nothing after it.
		return text
	}
	// A capital, because what is now the first word may not have
	// been: taking "Sir," off the front of "Sir, the battery is low"
	// leaves a sentence that has to start somewhere.
	return strings.ToUpper(out[:1]) + out[1:]
}

// Speak : Says an announcement to the audience its kind is for.
//
// The one place that turns the rule into a call. Say reaches the room;
// Reach reaches the room and wherever else the person is, which costs
// the words leaving the network and is worth it for a reminder and for
// nothing else here.
//
// Before this, each caller picked Say or Reach for itself and the
// reasoning lived in a comment on Reach. That is the audience rule
// written in the transport's vocabulary, in a third place, where adding
// an announcement meant guessing from the two existing ones.
//
// Refuses anything that is not an announcement rather than defaulting to
// the room. A reply broadcast to the house is a worse failure than one
// not spoken.
func Speak(ctx context.Context, to announce.Announcer, kind conversation.Kind, text string) error {
	audience, ok := kind.Audience()
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotAnnouncement, kind)
	}
	if to == nil || !to.Available() {
		return ErrNowhereToSay
	}
	// Spoken with its opening; written down without one. What goes
	// into the conversation is read rather than heard, and a word
	// whose whole purpose is to be talked over has no business
	// being in the transcript.
	said := opened(kind, text)
	if audience == conversation.Everywhere {
		return to.Reach(ctx, said)
	}
	return to.Say(ctx, said)
}

// ErrNotAnnouncement, ErrNowhereToSay : Why nothing was said.
var (
	ErrNotAnnouncement = errors.New("announcement: not something said unasked")
	ErrNowhereToSay    = errors.New("announcement: nothing is configured to speak")
)

// Writer : Notes announcements in the conversation that will hear the reply.
type Writer struct {
	// Conversations : Where messages are stored. Required.
	Conversations conversation.Repository
	// Clients : Where the person's clients are read from, to find the one
	// that is listening. Required.
	Clients store.Repository
	// Location : The person's zone, which is what a written hour means.
	// Nil is UTC.
	Location *time.Location
	// Now : The clock, replaceable in tests. Nil uses the real one.
	Now func() time.Time
	// Logger : Where failures go. Nil is silent.
	Logger *slog.Logger
	// Changes : Told when an announcement has been written, so a client
	// showing the conversation shows it as it is said rather than the next
	// time it reads. Nil tells nobody.
	Changes Changes
}

// Changes : Somewhere to say that a conversation has something new in it.
// It must not block.
type Changes interface {
	ConversationsChanged(userID, conversationID string)
}

// Reminded : Notes a reminder or timer said aloud as its time came.
//
// Everywhere, so nothing caused it and there is no conversation to be
// told: it is written wherever the person is listening.
func (w *Writer) Reminded(ctx context.Context, userID, text string) {
	w.said(ctx, userID, "", conversation.ReminderAnnouncement, text)
}

// Arrived : Notes a greeting, and whatever was held back with it, said as
// somebody came into the room.
//
// Asked, and the asker is the room. With one satellite that is the same
// conversation found() would find, so it is left to find it.
func (w *Writer) Arrived(ctx context.Context, userID, text string) {
	w.said(ctx, userID, "", conversation.PresenceAnnouncement, text)
}

// Coming : Notes a warning about something in the diary.
func (w *Writer) Coming(ctx context.Context, userID, text string) {
	w.said(ctx, userID, "", conversation.Coming, text)
}

// Announced : Notes something a device said to say at once.
//
// Written into the conversation like the rest, so "what was that?"
// has something to answer with.
func (w *Writer) Announced(ctx context.Context, userID, text string) {
	w.said(ctx, userID, "", conversation.Immediate, text)
}

// HeardBy : Notes an announcement a client collected and said, in the
// conversation that client is talking in -- where the answer to it will
// arrive. Said now, not when it was published: one held while the laptop
// was locked was heard when it was unlocked, or not at all.
//
// Not for a naming, which is noted in the conversation it named when the
// name is given.
func (w *Writer) HeardBy(ctx context.Context, userID string, c *client.Client, kind conversation.Kind, text string) {
	if w == nil || c == nil || kind == conversation.Renaming || w.Clients == nil {
		return
	}
	id, err := chat.ActiveConversation(ctx, w.Clients, userID, c.ID, c.ActiveConversationID)
	if err != nil {
		w.warn(ctx, "cannot tell which conversation heard this", err)
		return
	}
	w.said(ctx, userID, id, kind, text)
}

// Glanced : Notes a "what am I seeing" asked on a client and answered in
// one quick call, in the conversation that client is talking in, so what
// is asked next goes on from it. Written as a question and its answer,
// not as something said unasked: it was asked -- and as a finished chat,
// so it is shown with the turns.
func (w *Writer) Glanced(ctx context.Context, userID string, c *client.Client, asked, said string, askedAt time.Time) {
	if w == nil || c == nil || w.Clients == nil || w.Conversations == nil {
		return
	}
	asked, said = strings.TrimSpace(asked), strings.TrimSpace(said)
	if asked == "" || said == "" {
		return
	}
	id, err := chat.ActiveConversation(ctx, w.Clients, userID, c.ID, c.ActiveConversationID)
	if err != nil || id == "" {
		w.warn(ctx, "cannot tell which conversation the glance was in", err)
		return
	}
	// A chat of its own, finished as it is made, so the conversation shows
	// it as a turn: written as two messages and nothing else, it was in
	// what the model reads and in no screen (owner, 18 October 2026: *"it
	// responded well, but why didn't I find it in the conversation?"*).
	var chatID string
	if t, err := chat.New(id, chat.ChannelDirect, asked); err == nil {
		t.ClientID = c.ID
		// From when it was asked, so the time it took shows: made once answered, it showed 0.0 s (owner, 18 October 2026:
		// *"how is it so fast, is it not reaching the server?"*).
		if !askedAt.IsZero() {
			t.CreatedAt = askedAt.UTC().Truncate(chat.StoredPrecision)
			t.UpdatedAt = t.CreatedAt
		}
		if err := t.Start(); err == nil {
			started := t.CreatedAt
			t.StartedAt = &started
			if err := t.Complete(said); err == nil {
				if err := w.Clients.Create(ctx, t); err == nil {
					chatID = t.ID
				} else {
					w.warn(ctx, "cannot keep a glance as a chat", err)
				}
			}
		}
	}
	at := w.clock()
	for _, m := range []conversation.Message{conversation.Said(id, asked, at), conversation.Answered(id, said, at)} {
		m.ChatID = chatID
		if _, err := w.Conversations.Append(ctx, m); err != nil {
			w.warn(ctx, "cannot write down a glance", err)
			return
		}
	}
	w.changed(ctx, userID, id)
}

// Renamed : Notes the assistant having given a conversation its name.
//
// Asked, and the conversation is known, so it is given rather than
// looked for. This went straight to the store from the runner before,
// building the message by hand -- the same thing in a second place, with
// the audience rule nowhere.
func (w *Writer) Renamed(ctx context.Context, conversationID, text string) {
	w.said(ctx, "", conversationID, conversation.Renaming, text)
}

// said : Notes that these words were spoken aloud to the given person.
//
// Written against the conversation the voice client is talking in, because
// that is where the answer to it will arrive. A person who is spoken to in
// the room replies in the room.
//
// Never an error the caller has to handle. The words have already been
// said by the time this runs, and failing to write them down is worth a
// line in the log and nothing more: losing the note costs the next turn
// its context, and there is nothing useful to do about it here.
func (w *Writer) said(ctx context.Context, userID, conversationID string, kind conversation.Kind, text string) {
	if w == nil || w.Conversations == nil || w.Clients == nil {
		return
	}
	text = strings.TrimSpace(text)
	// One or the other. A conversation to write into, or somebody whose
	// listening client can be found.
	if text == "" || (userID == "" && conversationID == "") {
		return
	}

	id := conversationID
	if id == "" {
		var err error
		if id, err = w.found(ctx, userID); err != nil {
			w.warn(ctx, "cannot tell which conversation heard this", err)
			return
		}
	}
	if id == "" {
		// Nothing listens by voice. Not a fault: a server with no
		// satellite speaks nowhere, and an aside nobody heard is not
		// part of any conversation.
		return
	}

	at := w.clock()
	m := conversation.Announced(id, kind, text, at.In(w.where()).Format("3:04 pm"), at)
	if _, err := w.Conversations.Append(ctx, m); err != nil {
		w.warn(ctx, "cannot record what was said aloud", err)
		return
	}
	w.changed(ctx, userID, id)
}

// changed : Tells the conversation's owner's clients that it has something
// new in it. The owner is read from the conversation when the caller gave
// only the conversation.
func (w *Writer) changed(ctx context.Context, userID, conversationID string) {
	if w.Changes == nil {
		return
	}
	if userID == "" {
		c, err := w.Clients.GetConversation(ctx, conversationID)
		if err != nil {
			w.warn(ctx, "cannot say the conversation changed", err)
			return
		}
		userID = c.UserID
	}
	w.Changes.ConversationsChanged(userID, conversationID)
}

// listening : The conversation the person's voice client is talking in, or
// empty when they have none.
//
// The voice client rather than any client, because an aside is spoken into
// a room. A reply typed into the browser half an hour later is a different
// exchange, and attaching the greeting to it would put words in front of a
// question that never heard them.
// found : The conversation to write an announcement into when the caller
// has none to give.
//
// The most recently used voice client, which with one satellite is the
// only one. That makes this exact today and a guess the day there is a
// second: two satellites are two rooms, and the one spoken to last is a
// guess at the one being stood in, not a fact about it. When that day
// comes an Asked announcement should be told its client rather than
// coming through here -- Renamed is the shape of it.
//
// Voice only, deliberately. A typed client sees what was said when it
// next reads the conversation, and nothing is spoken to it.
func (w *Writer) found(ctx context.Context, userID string) (string, error) {
	clients, err := w.Clients.ListClients(ctx, userID, false)
	if err != nil {
		return "", err
	}

	var heard *client.Client
	for i := range clients {
		if clients[i].Channel != chat.ChannelVoice {
			continue
		}
		// The most recently used, when there is more than one. Two
		// satellites is two rooms, and the one spoken to last is the
		// better guess at the one being stood in.
		if heard == nil || clients[i].UpdatedAt.After(heard.UpdatedAt) {
			heard = &clients[i]
		}
	}
	if heard == nil {
		return "", nil
	}

	return chat.ActiveConversation(ctx, w.Clients, userID, heard.ID, heard.ActiveConversationID)
}

// clock : Now, in UTC.
func (w *Writer) clock() time.Time {
	if w.Now == nil {
		return time.Now().UTC()
	}
	return w.Now().UTC()
}

// where : The person's zone.
func (w *Writer) where() *time.Location {
	if w.Location == nil {
		return time.UTC
	}
	return w.Location
}

// warn : Records a failure to note something down.
func (w *Writer) warn(ctx context.Context, msg string, err error) {
	if w.Logger != nil {
		w.Logger.WarnContext(ctx, msg, slog.Any("error", err))
	}
}
