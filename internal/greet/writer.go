package greet

import (
	"context"
	"log/slog"
	"strings"
	"time"
)

// Writer : Asks a model for the greeting, and falls back when it cannot
// have one in time.
type Writer struct {
	// Ask : How the model is asked. Nil means the fixed greeting is
	// always used, which is how this behaved before there was a model
	// in it.
	Ask Asked
	// Within : How long to wait. Zero uses the constant.
	Within time.Duration
	// Logger : Where a failure goes. Nil is silent.
	Logger *slog.Logger
}

// Write : What to say, and whether the model wrote it.
//
// Never an error. Somebody is standing in the doorway: every way this
// can go wrong ends in the opening being said on its own, because a
// greeting that is merely ordinary is far better than a silence while
// a provider is down.
//
// The opening goes in front of whatever the model wrote rather than
// being asked for, so that it cannot be left out. Anything the model
// wrote in its place is taken off first.
//
// The fallback carries the reminders itself, since those are the part
// that must be said whatever happens.
func (w *Writer) Write(ctx context.Context, t Told) (said string, written bool) {
	opening := strings.TrimSpace(t.Opening)
	fallback := opening
	if len(t.Reminders) > 0 {
		fallback = strings.TrimSpace(fallback + " " + strings.Join(t.Reminders, " "))
	}

	if w == nil || w.Ask == nil {
		return fallback, false
	}

	// Nothing to write from, so nothing is written. The opening was
	// always going to be the whole greeting here; asking anyway
	// spends a call and a doorway's worth of waiting on a prompt
	// whose only honest answer is silence.
	if !t.Anything() {
		return fallback, false
	}

	// A deadline of its own, and not the caller's. The request that
	// asked for this is already gone -- Home Assistant gives up after
	// ten seconds -- and the greeting outlives it on purpose.
	within := w.Within
	if within <= 0 {
		within = Within
	}
	ask, done := context.WithTimeout(context.WithoutCancel(ctx), within)
	defer done()

	answer, err := w.Ask(ask, Prompt(t))
	if err != nil {
		w.warn(ctx, "could not write a greeting, saying the usual one", err, t)
		return fallback, false
	}
	// Nothing is the ordinary answer now, and not a failure. The
	// prompt asks for what comes *after* the greeting, and most
	// arrivals have nothing after it worth hearing; the opening is
	// then the whole greeting, which is what it was built to be.
	answer = Without(answer)
	if Quiet(answer) {
		answer = ""
	}
	if answer == "" {
		return fallback, false
	}
	return strings.TrimSpace(opening + " " + answer), true
}

// warn : A failure at the door is worth a line and nothing more.
//
// With how long it had and how much it was given, because the two
// things that will go wrong here are a provider being slow and a
// prompt having grown.
func (w *Writer) warn(ctx context.Context, msg string, err error, t Told) {
	if w.Logger == nil {
		return
	}
	w.Logger.WarnContext(ctx, msg,
		slog.Any("error", err),
		slog.Int("events", len(t.Events)),
		slog.Int("reminders", len(t.Reminders)))
}
