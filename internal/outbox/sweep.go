package outbox

import (
	"context"
	"log/slog"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/logging"
)

// Every : How often what is still pending is looked over.
//
// Slow on purpose. Nothing here delivers anything; it only closes
// off what can no longer be said, and a minute either way makes no
// difference to a thing that is already too late.
const Every = time.Minute

// Sweeper : Closes off announcements no client can still say.
//
// What is left of the server's part once delivery moved to the
// clients. It does not speak, does not know about rooms, and never
// decides whether anybody heard anything: a client does that and
// says so. This only answers the one question a client cannot,
// because it is about a moment rather than a device -- has this
// gone past the point of being worth saying at all.
//
// Without it a voice client that was switched off for a week comes
// back and is handed a hundred stale warnings.
type Sweeper struct {
	// Store : What is owed. Required.
	Store Store
	// Clients : Which clients to look over.
	Clients func(ctx context.Context) ([]string, error)
	// Every : How often to look. Zero selects Every.
	Every time.Duration
	// Now : The clock, replaceable in tests.
	Now func() time.Time
	// Logger : Where failures go. Nil is silent.
	Logger *slog.Logger
}

// Run : Sweeps until the context ends.
func (s *Sweeper) Run(ctx context.Context) {
	if s == nil || s.Store == nil || s.Clients == nil {
		return
	}
	every := s.Every
	if every <= 0 {
		every = Every
	}
	tick := time.NewTicker(every)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			logging.Surviving(ctx, s.Logger, "an outbox sweep", func() { s.Once(ctx) })
		}
	}
}

// Once : One look over what is still pending.
//
// Returns how many were closed off, which is the number worth
// watching: a server that expires everything is a server whose
// clients have stopped collecting.
func (s *Sweeper) Once(ctx context.Context) int {
	if s == nil || s.Store == nil || s.Clients == nil {
		return 0
	}
	clients, err := s.Clients(ctx)
	if err != nil {
		s.log("cannot read which clients to sweep", err)
		return 0
	}

	now := s.clock()
	var closed int
	for _, client := range clients {
		owing, err := s.Store.Pending(ctx, client, Limit)
		if err != nil {
			s.log("cannot read what a client still owes", err)
			continue
		}
		for _, d := range owing {
			if !d.Owing.Stale(now) {
				continue
			}
			if err := s.Store.Mark(ctx, client, d.Owing.ID, Expired,
				d.Tries, "its moment passed before it could be said", now); err != nil {
				s.log("cannot close off a stale announcement", err)
				continue
			}
			closed++
			s.note(d)
		}
	}
	return closed
}

// clock : Now, in UTC.
func (s *Sweeper) clock() time.Time {
	if s.Now == nil {
		return time.Now().UTC()
	}
	return s.Now().UTC()
}

// log : A failure of this machinery.
func (s *Sweeper) log(msg string, err error) {
	if s.Logger != nil {
		s.Logger.Error(msg, slog.Any("error", err))
	}
}

// note : Something that was owed and will never now be said.
//
// Warned rather than logged quietly. Somebody asking "why did I not
// hear about that" deserves a line that answers it.
func (s *Sweeper) note(d Delivery) {
	if s.Logger != nil {
		s.Logger.Warn("an announcement went stale before a client could say it",
			slog.String("kind", string(d.Owing.Kind)),
			slog.String("say", d.Owing.Text),
			slog.String("client", d.ClientID))
	}
}
