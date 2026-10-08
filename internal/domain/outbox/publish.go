package outbox

import (
	"context"
	"fmt"

	"github.com/DhanushRamesh/personal-assistant/internal/base/conversation"
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

	// Asked, and the asker is known: that client and no other.
	//
	// Asked with nobody named falls through to everybody, which is
	// not a tidy answer and is the honest one for now. Asked is
	// carrying two different ideas -- "only the client that caused
	// this", which a renaming means, and "the room rather than the
	// phone", which an immediate announcement means. A device event
	// has no causing client at all, so refusing here silenced every
	// situation. Splitting the two is the fix; until then, reaching
	// too many beats reaching none.
	if rule.Audience == conversation.Asked && o.From != "" {
		return p.Store.Publish(ctx, o, []string{o.From})
	}

	to, err := p.Clients(ctx, o.UserID)
	if err != nil {
		return fmt.Errorf("outbox: %w", err)
	}
	return p.Store.Publish(ctx, o, to)
}
