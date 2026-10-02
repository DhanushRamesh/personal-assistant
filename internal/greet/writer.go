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
// can go wrong ends in the fixed greeting being said, because a
// greeting that is merely ordinary is far better than a silence while
// a provider is down.
//
// The fallback carries the reminders itself, since those are the part
// that must be said whatever happens.
func (w *Writer) Write(ctx context.Context, t Told) (said string, written bool) {
	fallback := strings.TrimSpace(t.Fallback)
	if len(t.Reminders) > 0 {
		fallback = strings.TrimSpace(fallback + " " + strings.Join(t.Reminders, " "))
	}

	if w == nil || w.Ask == nil {
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
	answer = strings.TrimSpace(answer)
	if answer == "" {
		w.warn(ctx, "the model greeted nobody, saying the usual one", nil, t)
		return fallback, false
	}
	return answer, true
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
