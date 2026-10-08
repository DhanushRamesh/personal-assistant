package logging

import (
	"context"
	"log/slog"
	"runtime/debug"
)

// Surviving : Runs work, turning a panic in it into a logged fault
// rather than a dead process.
//
// An HTTP handler has had this since the beginning -- see
// middleware.Recoverer -- and the loops that run unattended never
// did. There are twenty-odd of them: the rule engine, reminders, the
// outbox sweeper, the collectors that fetch a calendar and a mailbox.
// A panic in any one of them took the whole server with it, and with
// it voice, every rule and every reminder, because one tasklist came
// back shaped differently than the day before.
//
// The supervisor puts the process back, which is the other half of
// this and not a substitute for it: coming back costs every loop its
// place and whatever turn was in flight. One loop failing should cost
// one loop.
//
// What it does not do is pretend the work happened. The panic is
// logged with its stack at error level, under the name given, and the
// caller carries on with whatever comes after -- which for a ticking
// loop is the next tick, and for a one-shot is nothing at all.
func Surviving(ctx context.Context, logger *slog.Logger, what string, work func()) {
	defer func() {
		rec := recover()
		if rec == nil {
			return
		}
		if logger == nil {
			// Some of the places this is needed are domain packages
			// that take a service client and nothing else, and
			// threading a logger through them to reach a path that
			// should never run would be the tail wagging the dog.
			// The default handler is the one configured at startup.
			logger = slog.Default()
		}
		logger.LogAttrs(ctx, slog.LevelError, "panic recovered",
			slog.String("in", what),
			slog.Any("panic", rec),
			slog.String("stack", string(debug.Stack())),
		)
	}()
	work()
}

// Repeatedly : Wraps a function so every call of it survives a panic.
//
// For a loop whose body is called many times, where wrapping the
// whole loop would mean one panic ending it for good. The next call
// happens as it would have.
func Repeatedly(ctx context.Context, logger *slog.Logger, what string, work func()) func() {
	return func() { Surviving(ctx, logger, what, work) }
}
