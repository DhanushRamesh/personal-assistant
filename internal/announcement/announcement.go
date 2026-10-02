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
	"strings"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/announce"
	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
)

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
	if audience == conversation.Everywhere {
		return to.Reach(ctx, text)
	}
	return to.Say(ctx, text)
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
	Clients chat.Repository
	// Location : The person's zone, which is what a written hour means.
	// Nil is UTC.
	Location *time.Location
	// Now : The clock, replaceable in tests. Nil uses the real one.
	Now func() time.Time
	// Logger : Where failures go. Nil is silent.
	Logger *slog.Logger
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
	}
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

	var heard *chat.Client
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
