package remind

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// JustSaidWindow : How long a reminder stays the one meant by "that".
//
// A quarter of an hour. Long enough to cover somebody walking back into
// the room to answer, short enough that "snooze that" cannot reach a
// reminder from before lunch and put it off without saying which.
const JustSaidWindow = 15 * time.Minute

// JustSaid : What the assistant is told about reminders it has just spoken.
//
// Without this it has no record of having spoken at all. The firing loop
// goes straight to the satellite and never touches the conversation, so
// the words are in the room but not in the prompt: asked what it just
// said, the assistant does not know, and "snooze that" refers to nothing.
//
// Empty when nothing has been said recently.
func JustSaid(spoken []Reminder, at time.Time, loc *time.Location) string {
	if len(spoken) == 0 {
		return ""
	}
	if loc == nil {
		loc = time.UTC
	}

	var b strings.Builder
	b.WriteString("Reminders you said out loud a short time ago. Their time came and ")
	b.WriteString("you spoke them, unprompted. That was not a turn in this ")
	b.WriteString("conversation, so none of it appears above, but the person heard ")
	b.WriteString("it.\n\n")

	b.WriteString("They may be answering one of these rather than starting something ")
	b.WriteString("new. Take \"that\", \"it\" and \"the reminder\" to mean the most ")
	b.WriteString("recent one listed, unless what they say points to another. Asked ")
	b.WriteString("what you just said, this is it.\n\n")

	b.WriteString("Do not raise any of this yourself. They were there when you said ")
	b.WriteString("it, and being told again is being told twice.")

	for i := range spoken {
		b.WriteString("\n- ")
		b.WriteString(ago(spoken[i], at, loc))
		b.WriteString(": ")
		b.WriteString(said(spoken[i]))
	}
	return b.String()
}

// said : The words that were spoken, as they were spoken.
func said(r Reminder) string {
	body := strings.TrimSpace(r.Body)
	if body == "" {
		body = strings.TrimSpace(r.Title)
	}
	return inQuotes(body)
}

// inQuotes : The text in quotation marks, so the model reads it as
// something said rather than as a further instruction.
func inQuotes(s string) string { return "\"" + strings.ReplaceAll(s, "\"", "'") + "\"" }

// ago : How long ago it was said, in words.
//
// Relative, because that is what makes it the one meant by "that": a
// clock time says when, and "four minutes ago" says it is still the thing
// being talked about. Past an hour the relative form stops helping and the
// clock time is given instead.
func ago(r Reminder, at time.Time, loc *time.Location) string {
	if r.LastFiredAt == nil {
		return "just now"
	}

	since := at.Sub(*r.LastFiredAt)
	switch minutes := int(since.Round(time.Minute) / time.Minute); {
	case since >= time.Hour:
		return "at " + r.LastFiredAt.In(loc).Format("3:04 pm")
	case minutes <= 0:
		return "just now"
	case minutes == 1:
		return "a minute ago"
	default:
		return fmt.Sprintf("%d minutes ago", minutes)
	}
}

// Recently : What the person has just been told, for the prompt.
//
// Shaped like Missing, and for the same reason: reading it is the
// runner's business and knowing what it means is this package's.
type Recently struct {
	// Store : Where reminders are kept. Nil says nothing.
	Store Store
	// Location : The person's zone, for saying when. Nil is UTC.
	Location *time.Location
	// Now : The clock, replaceable in tests. Nil uses the real one.
	Now func() time.Time
	// Within : How far back to look. Zero selects JustSaidWindow.
	Within time.Duration
}

// Block : What to add to a prompt. Empty when nothing was said recently.
func (r *Recently) Block(ctx context.Context, userID string) (string, error) {
	if r == nil || r.Store == nil || userID == "" {
		return "", nil
	}

	at := time.Now().UTC()
	if r.Now != nil {
		at = r.Now().UTC()
	}
	within := r.Within
	if within <= 0 {
		within = JustSaidWindow
	}

	spoken, err := r.Store.LastSpoken(ctx, userID, at.Add(-within))
	if err != nil || len(spoken) == 0 {
		return "", err
	}
	return JustSaid(spoken, at, r.Location), nil
}
