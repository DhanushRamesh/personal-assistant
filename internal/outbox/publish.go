package outbox

import (
	"context"
	"fmt"

	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
)

// Publisher : Puts an announcement where the clients that should
// have it will find it.
//
// Between the logic that decided to speak and the store that keeps
// what is owed, for one reason: working out *who should hear this*
// is neither of their jobs. A watcher knows a thing is worth
// saying and nothing about clients; the store keeps rows and has
// no opinion about audiences.
type Publisher struct {
	// Store : Where what is owed is kept. Required.
	Store Store
	// Clients : Every client of one person. Required.
	Clients func(ctx context.Context, userID string) ([]string, error)
}

// Publish : Works out who should hear this and writes it down for
// each of them.
//
// The audience comes from the kind, in the one table that already
// holds it. Two answers, and the difference is who caused it:
//
//   - Asked, so the client that caused it and nowhere else. A
//     conversation renamed on a laptop means nothing in another
//     room, and a greeting is said because somebody walked up to
//     this machine.
//   - Everywhere, so every client the person has. A reminder's
//     whole point is to arrive when they are somewhere else.
func (p *Publisher) Publish(ctx context.Context, o Owing) error {
	if p == nil || p.Store == nil || p.Clients == nil {
		return fmt.Errorf("outbox: there is nowhere to publish to")
	}
	rule, ok := o.Kind.Rule()
	if !ok {
		return ErrNotAnnouncement
	}

	var to []string
	switch rule.Audience {
	case conversation.Asked:
		if o.From == "" {
			// Nothing to deliver it to. Refused rather than sent
			// to everybody: the whole meaning of Asked is that one
			// client caused this, and guessing which would put a
			// conversation's new name on somebody's speaker.
			return fmt.Errorf("outbox: %s is for the client that caused it, and none is named", o.Kind)
		}
		to = []string{o.From}
	default:
		var err error
		if to, err = p.Clients(ctx, o.UserID); err != nil {
			return fmt.Errorf("outbox: %w", err)
		}
	}
	return p.Store.Publish(ctx, o, to)
}
