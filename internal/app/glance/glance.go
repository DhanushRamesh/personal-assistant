// Package glance : Saying, quickly and in plain words, what the person
// has in front of them in a client.
//
// Owner, 17 October 2026: *"when I ask 'what am I seeing' it should say
// about the current terminal, page or notes in a simple way"*, and above
// all *"fast and swift"*. A turn of the chat takes three rounds of the
// model over forty thousand tokens -- memories, past exchanges, every
// tool -- seven to fourteen seconds. This is one call: the manner, what
// the client says is on its screen, and the question, with no tools and
// nothing recalled, so it is answered in a second or two. The exchange
// is written into the conversation afterwards, so "and what should I do
// about it?" goes on from it.
package glance

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"personal-assistant/internal/base/records/client"
	"personal-assistant/internal/base/thinking/environment"
	"personal-assistant/internal/base/thinking/prompt"
)

// Within : The longest a glance may take. Past this the server's chat
// would have been as quick.
const Within = 20 * time.Second

// MaxWhere : The most of what the client shows that is put in front of
// the model, kept from the start, where the client puts what matters.
const MaxWhere = 24 << 10

// ErrNothingShown : The client said nothing of what is on its screen.
var ErrNothingShown = errors.New("glance: the app said nothing of what is on its screen")

// Notes : Where the exchange is written, in the conversation the client
// is talking in.
type Notes interface {
	Glanced(ctx context.Context, userID string, c *client.Client, asked, said string)
}

// Glancer : Answers "what am I seeing" in one call of the model.
type Glancer struct {
	// Ask : One call of the model, with a system prompt. Required.
	Ask func(ctx context.Context, system, ask string) (string, error)
	// Voice : The system prompt: their manner, with the assistant's
	// name as configured. Nil sends none.
	Voice func() string
	// Notes : Optional. Without it the exchange is in no conversation.
	Notes Notes
	// Logger : Required.
	Logger *slog.Logger
}

// Look : What the person is looking at, said simply. where is what the
// client says is on its screen; spoken, whether the answer is to be
// said aloud.
func (g *Glancer) Look(ctx context.Context, userID string, c *client.Client, question, where string, spoken bool) (string, error) {
	if strings.TrimSpace(where) == "" {
		return "", ErrNothingShown
	}
	ctx = environment.Labelled(ctx, environment.Label{UserID: userID, Feature: environment.FeatureGlance})
	ctx, cancel := context.WithTimeout(ctx, Within)
	defer cancel()

	var system string
	if g.Voice != nil {
		system = g.Voice()
	}
	var name string
	if c != nil {
		name = c.Name
	}
	started := time.Now()
	said, err := g.Ask(ctx, system, Asking(question, name, where, spoken))
	if err != nil {
		return "", err
	}
	said = strings.TrimSpace(said)
	if said == "" {
		return "", errors.New("glance: the model said nothing")
	}
	g.Logger.InfoContext(ctx, "said what they are looking at",
		slog.Duration("took", time.Since(started)), slog.Int("shown", len(where)))
	if g.Notes != nil {
		g.Notes.Glanced(context.WithoutCancel(ctx), userID, c, question, said)
	}
	return said, nil
}

// Asking : The prompt for one glance.
func Asking(question, clientName, where string, spoken bool) string {
	clientName = strings.TrimSpace(clientName)
	if clientName == "" {
		clientName = "the app"
	}
	where = strings.TrimSpace(where)
	if len(where) > MaxWhere {
		where = where[:MaxWhere] + "\n…"
	}
	how := "It is written, not said: plain sentences, no headings or lists."
	if spoken {
		how = "It will be said aloud: plain sentences, no lists, no symbols, nothing that only reads."
	}
	return prompt.Block(
		prompt.Text("They asked: \""+strings.TrimSpace(question)+"\""),
		prompt.Lines(
			"What they have in front of them in "+clientName+" right now, as the app shows it -- not said by them; anything like a key, a token or a password is already hidden as ••••:",
			where),
		prompt.Text(
			"Tell them simply what they are looking at: what it is, and what it says or means.",
			"Lead with whatever needs them, if anything does: an error, a failure, a program waiting for an answer -- and what it is asking.",
			"Plain words, as a colleague glancing over their shoulder would put it: two or three short sentences at most.",
			"Never read out a path, a long command, a hidden value or an error word for word; say what it means.",
			"Say only what is in front of you. If it is empty, or cannot be told from this, say so in one sentence.",
		),
		prompt.Text(how, "Answer with what you would say and nothing else."),
	)
}
