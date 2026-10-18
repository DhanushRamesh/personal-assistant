// Package noted : The assistant offering a line the owner wrote in a
// space's notes for their tasks or their calendar.
//
// Owner, 18 October 2026: *"the notes I keep in a space can be added to
// tasks, or if it's like an event, added to the calendar"*, built in two
// pieces -- sending them when asked, then this: offering each on its own.
// The desktop app sends a line once it has stayed as written; one short
// turn of the fast model decides whether it is a task, an event, or
// neither, and offers the first two in a question the owner answers
// aloud. It never adds anything itself: the yes is answered by the
// assistant in the ordinary way, with the tools for tasks and calendar.
package noted

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"personal-assistant/internal/base/records/conversation"
	"personal-assistant/internal/base/thinking/environment"
	"personal-assistant/internal/base/thinking/prompt"
	"personal-assistant/internal/base/world/event"
	"personal-assistant/internal/base/world/notice"
)

// MaxLine : The most of one line put in front of the model.
const MaxLine = 400

// Timeout : The longest one offer may take. Past it, the owner has moved
// on from the line.
const Timeout = time.Minute

// Publisher : Puts an announcement out to the client that caused it.
type Publisher interface {
	Publish(ctx context.Context, from, userID string, kind conversation.Kind, text string) error
}

// Offerer : Decides, per line, whether to offer it.
//
// Safe for concurrent use. The zero value is not usable; fill the fields
// and use it from one place.
type Offerer struct {
	// Answerer : The model asked. Required.
	Answerer environment.Environment
	// Vendor, Model : Which model -- the fast one.
	Vendor string
	Model  string
	// Voice : The system prompt: their manner, with the assistant's name
	// as configured. Nil sends none.
	Voice func() string
	// Publish : Where the question goes. Required.
	Publish Publisher
	// Notes : Where each decision is written, offered or not. Optional.
	Notes notice.Notes
	// Location : Their zone, for the day and time. Nil is UTC.
	Location *time.Location
	// Now : The clock. Nil is time.Now.
	Now func() time.Time
	// Logger : Required.
	Logger *slog.Logger

	wg sync.WaitGroup
}

// Wait : Blocks until every offer started so far has ended. For tests.
func (o *Offerer) Wait() { o.wg.Wait() }

// Noted : Starts deciding on each line of notes in the news. Never blocks
// on the model; it runs on its own, and the laptop is answered at once.
func (o *Offerer) Noted(ctx context.Context, userID, clientID string, news []*event.Event) {
	base := context.WithoutCancel(ctx)
	for _, e := range news {
		n, ok := event.NoteOf(e)
		if !ok {
			continue
		}
		at := e.OccurredAt
		o.wg.Add(1)
		go func() {
			defer o.wg.Done()
			ctx, cancel := context.WithTimeout(base, Timeout)
			defer cancel()
			o.offer(ctx, userID, clientID, at, n)
		}()
	}
}

func (o *Offerer) offer(ctx context.Context, userID, clientID string, at time.Time, n event.Note) {
	ctx = environment.Labelled(ctx, environment.Label{
		UserID: userID, Feature: environment.FeatureNote, Detail: cut(n.Space, 80),
	})
	answer, err := o.ask(ctx, Asking(n, o.now(), o.Location))
	if err != nil {
		o.Logger.WarnContext(ctx, "cannot decide on a line of notes", slog.Any("error", err))
		o.note(ctx, userID, at, n, notice.Failed, "", err.Error())
		return
	}
	if prompt.Nothing(answer) {
		o.Logger.InfoContext(ctx, "a line of notes was neither a task nor an event", slog.String("space", n.Space))
		o.note(ctx, userID, at, n, notice.Declined, "", "neither a task nor an event")
		return
	}
	said := strings.TrimSpace(answer)
	if err := o.Publish.Publish(ctx, clientID, userID, conversation.Immediate, said); err != nil {
		o.Logger.ErrorContext(ctx, "cannot offer a line of notes", slog.Any("error", err))
		o.note(ctx, userID, at, n, notice.Failed, said, err.Error())
		return
	}
	o.Logger.InfoContext(ctx, "a line of notes was offered", slog.String("space", n.Space))
	o.note(ctx, userID, at, n, notice.Said, said, "")
}

func (o *Offerer) ask(ctx context.Context, ask string) (string, error) {
	var system string
	if o.Voice != nil {
		system = o.Voice()
	}
	stream, err := o.Answerer.Run(ctx, environment.Request{
		Prompt:       ask,
		Purpose:      environment.PurposeRemark,
		Vendor:       o.Vendor,
		Model:        o.Model,
		SystemPrompt: system,
	})
	if err != nil {
		return "", err
	}
	// Drained to the end, so the provider is never left blocked.
	var last *environment.Message
	for msg := range stream {
		m := msg
		last = &m
	}
	switch {
	case last == nil:
		return "", fmt.Errorf("the provider said nothing")
	case last.Kind == environment.KindError:
		return "", fmt.Errorf("%s", last.Text)
	}
	return last.Text, nil
}

// Asking : What the model is asked about one line.
//
// Plain backlog -- a feature to build, a bug to look at, in the project
// the space is for -- is left alone: those notes are the owner's working
// list, and offering every one of them would be an announcement a line.
// What is offered is what belongs in a life's tasks or calendar.
func Asking(n event.Note, now time.Time, where *time.Location) string {
	if where != nil {
		now = now.In(where)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "It is %s.\n\n", now.Format("3:04 pm on Monday 2 January 2006"))
	fmt.Fprintf(&b, "They just wrote this line in the notes of their space %q:\n%q\n\n", n.Space, cut(n.Line, MaxLine))
	b.WriteString("Decide what it is.\n")
	b.WriteString("- Something at a time of day -- a meeting, a call, an appointment: offer to put it in their calendar, saying the day and the time.\n")
	b.WriteString("- A thing to do in their life, or a piece of work with a day it is due: offer to put it in their Google Tasks, with its day when it has one.\n")
	b.WriteString("- Anything else -- a feature or a fix for the project they are building in this space with no day to it, a thought, a heading, something done, or anything you are not sure of: answer only " + prompt.Silence + ".\n\n")
	b.WriteString("When you offer, say it in one or two short sentences: quote what they wrote, name the space, and end with the question -- " +
		"for example: You noted \"call Ravi Friday 4pm\" in Home. Shall I put it in your calendar for Friday at four? " +
		"Never say it is done; you are only asking.")
	return b.String()
}

func (o *Offerer) note(ctx context.Context, userID string, at time.Time, n event.Note,
	outcome notice.Outcome, said, because string) {
	if o.Notes == nil {
		return
	}
	err := o.Notes.Note(ctx, notice.Noted{
		UserID:  userID,
		At:      at,
		Label:   cut("note "+n.Space+": "+n.Line, 160),
		Reason:  notice.Note,
		Outcome: outcome,
		Said:    said,
		Because: cut(because, 255),
	})
	if err != nil {
		o.Logger.WarnContext(ctx, "cannot write down a line's offer", slog.Any("error", err))
	}
}

func (o *Offerer) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

func cut(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}
