package runner

import (
	"context"
	"log/slog"

	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/base/records/conversation"
	"personal-assistant/internal/base/thinking/environment"
	"personal-assistant/internal/domain/announcement"
)

// title : Gives a conversation a name, once, from what has been said in it.
//
// Runs after the answer has been recorded and announced, like condensing,
// because nobody is waiting for it. Every failure is logged and dropped: a
// conversation without a name is exactly what every conversation was before
// this existed.
func (r *Runner) title(ctx context.Context, t *chat.Chat) {
	if t.ConversationID == "" || r.repo == nil {
		return
	}

	// Only once, and never over a name somebody chose. A conversation renamed
	// by hand keeps that name however much is said in it afterwards.
	c, err := r.repo.GetConversation(ctx, t.ConversationID)
	if err != nil {
		r.logger.ErrorContext(ctx, "cannot read the conversation to name it",
			slog.Any("error", err))
		return
	}
	if c.Title != "" {
		return
	}

	said, err := r.messages.All(ctx, t.ConversationID)
	if err != nil {
		r.logger.ErrorContext(ctx, "cannot read the conversation to name it",
			slog.Any("error", err))
		return
	}

	// One exchange is the least that can be named. Naming a conversation from
	// the question alone produces a label for a subject nobody has answered
	// yet, which is often not what it turns out to be about.
	if len(conversation.ForModel(said)) < 2 {
		return
	}

	askCtx, cancel := context.WithTimeout(ctx, r.condenseTimeout)
	defer cancel()

	answer, err := r.ask(askCtx, t.Model, environment.PurposeTitle, conversation.TitlePrompt(said))
	if err != nil {
		r.logger.WarnContext(ctx, "cannot name the conversation",
			slog.String("conversation_id", t.ConversationID), slog.Any("error", err))
		return
	}

	// A model asked for a bare label will sometimes answer with a sentence.
	// No name is better than a bad one, and it will not be asked again.
	name := conversation.CleanTitle(answer)
	if name == "" {
		r.logger.InfoContext(ctx, "the model did not offer a usable name",
			slog.String("conversation_id", t.ConversationID),
			slog.String("answer", answer))
		return
	}

	if err := r.repo.RenameConversation(ctx, c.UserID, t.ConversationID, name); err != nil {
		r.logger.ErrorContext(ctx, "cannot store the conversation's name",
			slog.String("conversation_id", t.ConversationID), slog.Any("error", err))
		return
	}

	r.logger.InfoContext(ctx, "conversation named",
		slog.String("conversation_id", t.ConversationID), slog.String("title", name))

	r.noteTitle(ctx, t, name)
	r.changed(ctx, t.ConversationID)
	r.publishTitle(ctx, t, c, name)
	r.announceTitle(ctx, t, name)
}

// noteTitle : Writes the naming into the conversation it named.
//
// On every channel, not only the one that heard it said. The name shows
// up in the listing either way, and being able to see a thing is not the
// same as the assistant being able to answer for it: asked why it had
// renamed, it said it had no record of doing so, because it had none.
//
// The conversation is known here, so there is no guessing which one was
// listening the way an announcement has to.
func (r *Runner) noteTitle(ctx context.Context, t *chat.Chat, name string) {
	if r.announcements == nil || t.ConversationID == "" {
		return
	}

	// Through the same writer the other two announcements use, and told
	// which conversation rather than letting it look: this one is Asked
	// and the asker is known. Writing it here by hand was the second
	// place the audience rule lived, and the one that disagreed.
	//
	// Logged and dropped inside the writer, like everything else in
	// here. The conversation has its name; failing to write the note
	// down costs the next turn its context and is not worth failing the
	// turn over.
	r.announcements.Renamed(ctx, t.ConversationID,
		conversation.TitleAnnouncement(name))
}

// Announcements : Somewhere to note what the assistant said unasked.
//
// An interface rather than the writer itself, so the runner keeps
// knowing nothing about clients or conversations beyond the one it is
// running in.
type Announcements interface {
	// Renamed : Notes that this conversation was given a name.
	Renamed(ctx context.Context, conversationID, text string)
}

// Publishing : Where an announcement goes for the clients that
// should hear it.
//
// Narrow on purpose. The runner says what happened and which
// client caused it; who should hear that is worked out from the
// kind, in one table, somewhere else entirely.
type Publishing interface {
	// Publish : Puts one announcement out. from is the client that
	// caused it, which for a naming is the client whose
	// conversation was named.
	Publish(ctx context.Context, from, userID string,
		kind conversation.Kind, text string) error
}

// publishTitle : Puts the naming where that client will find it.
//
// Owner: *"this event is client specific only, not all clients
// should get that."* A conversation renamed on a laptop is that
// laptop's business -- read on a phone it is a line about a thing
// nobody there did, and said on a speaker it is noise. The kind is
// Asked and the asker is named here, so it reaches one client and
// no others.
//
// Logged and dropped on failure, like everything else in here. The
// conversation has its name; failing to publish the fact costs a
// notification and is not worth failing the turn over.
func (r *Runner) publishTitle(ctx context.Context, t *chat.Chat, c *chat.Conversation, name string) {
	if r.publishing == nil || t.ClientID == "" || c == nil {
		return
	}
	err := r.publishing.Publish(ctx, t.ClientID, c.UserID,
		conversation.Renaming, conversation.TitleAnnouncement(name))
	if err != nil {
		r.logger.WarnContext(ctx, "cannot publish the conversation's new name",
			slog.String("conversation_id", t.ConversationID), slog.Any("error", err))
	}
}

// announceTitle : Says the new name aloud, when the person had no way to see
// it.
//
// Only for a chat that arrived by voice. Anywhere else the name appears in
// the listing the moment it is set, and saying it would be noise.
func (r *Runner) announceTitle(ctx context.Context, t *chat.Chat, name string) {
	if t.Channel != chat.ChannelVoice || r.announcer == nil || !r.announcer.Available() {
		return
	}

	// Useful the first time and tiresome by the twentieth, so it can be
	// turned off. Unset means on, since somebody who cannot see a listing has
	// no other way of knowing what a conversation is called.
	if r.repo != nil {
		if got, err := r.repo.Setting(ctx, conversation.AnnounceTitlesSetting); err == nil && got == "off" {
			return
		}
	}

	if err := announcement.Speak(ctx, r.announcer,
		conversation.Renaming, conversation.TitleAnnouncement(name)); err != nil {
		r.logger.WarnContext(ctx, "cannot announce the conversation's name",
			slog.Any("error", err))
	}
}
