// Package announce speaks to a person without having been asked.
//
// Everything else the assistant says is a reply: something arrives, something
// goes back. This is the other direction — a timer that has finished, a
// conversation that has just been given a name, a window left open. The
// assistant has to be able to start a sentence, not only finish one.
package announce

import (
	"context"
	"log/slog"
)

// Announcer : Somewhere a sentence can be said aloud.
type Announcer interface {
	// Say : Speaks the message, or reports why it could not.
	//
	// A caller announcing something incidental should log a failure and carry
	// on: not being heard is not a reason to fail the thing being announced.
	Say(ctx context.Context, message string) error

	// Reach : Says it in the room and wherever else the person can be
	// reached, such as their phone.
	//
	// For the few announcements that matter when nobody is in the room.
	// A reminder is the whole of that category: it is the only thing
	// here whose point is to arrive when the person is somewhere else.
	//
	// A greeting is the opposite and must not use this. It is said
	// because somebody just walked up to the laptop, so a copy in their
	// pocket arrives at the one moment it is certainly not needed. The
	// same goes for housekeeping like naming a conversation, which has
	// no business following anybody out of the house -- and getting
	// there means the words leaving the network, which is a cost worth
	// paying for a reminder and not for that.
	Reach(ctx context.Context, message string) error

	// Available : Whether anything is actually wired up. A caller can use
	// this to avoid composing a message nobody will hear.
	Available() bool
}

// Silent : An Announcer with nowhere to speak.
//
// The default, so that an unconfigured server behaves exactly as it did
// before there was anything to announce with, rather than failing.
type Silent struct {
	// Logger : Where the unspoken message goes instead, so that what would
	// have been said is still visible while this is being set up. Optional.
	Logger *slog.Logger
}

// Say : Records the message and reports success. Nothing was spoken.
func (s Silent) Say(ctx context.Context, message string) error {
	if s.Logger != nil {
		s.Logger.DebugContext(ctx, "nothing is configured to speak this",
			slog.String("message", message))
	}
	return nil
}

// Reach : Also nothing. There is nowhere to say it and nowhere to send
// it.
func (s Silent) Reach(ctx context.Context, message string) error {
	return s.Say(ctx, message)
}

// Available : Always false. There is nowhere to say anything.
func (Silent) Available() bool { return false }
