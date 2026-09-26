package remind

import (
	"context"
	"strings"
	"time"
)

// Unsaid : What the assistant is told about reminders it never managed to
// say.
//
// A reminder whose time passed while nothing could reach the satellite was
// marked missed and then nothing happened at all: never spoken, not on the
// screen, never brought up. It vanished, which is the one thing a reminder
// must not do.
//
// Brought up once. Being told every turn about a thing that did not happen
// last Tuesday is worse than not being told.
func Unsaid(missed []Reminder, loc *time.Location) string {
	if len(missed) == 0 {
		return ""
	}
	if loc == nil {
		loc = time.UTC
	}

	var b strings.Builder
	b.WriteString("These reminders were never said. Their time came while there was no way ")
	b.WriteString("to say them, usually because the server or the speaker was unreachable.\n\n")
	b.WriteString("Begin your reply by telling the person, in one sentence, what each was and ")
	b.WriteString("when it was due, then answer whatever they asked. Do this even if they asked ")
	b.WriteString("something unrelated, even if they only said hello, and even if your reply ")
	b.WriteString("would otherwise be a few words: a reminder nobody was told about is the one ")
	b.WriteString("thing here that must not pass in silence. Say it as something that did not ")
	b.WriteString("happen, not as something still to come. This is the only time you are told, ")
	b.WriteString("so leaving it out loses it for good.")

	for i := range missed {
		b.WriteString("\n- ")
		b.WriteString(missed[i].DueAt.In(loc).Format("Monday 2 January at 3:04 pm"))
		b.WriteString(": ")
		b.WriteString(strings.TrimSpace(missed[i].Body))
	}
	return b.String()
}

// IDs : The identifiers of the given reminders.
func IDs(all []Reminder) []string {
	out := make([]string, 0, len(all))
	for i := range all {
		out = append(out, all[i].ID)
	}
	return out
}

// Missing : The missed reminders a person has not been told about, and a
// way to record that they now have been.
type Missing struct {
	// Store : Where reminders are kept. Nil mentions nothing.
	Store Store
	// Location : The person's zone, for saying when each was due.
	Location *time.Location
	// Now : The clock, replaceable in tests. Nil uses the real one.
	Now func() time.Time
}

// Block : What to add to a prompt, and the reminders it covers.
//
// The caller marks them mentioned once the prompt has actually been sent,
// so a turn that never reaches the model does not use up the one telling.
func (m *Missing) Block(ctx context.Context, userID string) (string, []Reminder, error) {
	if m == nil || m.Store == nil || userID == "" {
		return "", nil, nil
	}

	missed, err := m.Store.Unmentioned(ctx, userID)
	if err != nil || len(missed) == 0 {
		return "", nil, err
	}
	return Unsaid(missed, m.Location), missed, nil
}

// Told : Records that these have been brought up.
func (m *Missing) Told(ctx context.Context, missed []Reminder) error {
	if m == nil || m.Store == nil || len(missed) == 0 {
		return nil
	}

	at := time.Now().UTC()
	if m.Now != nil {
		at = m.Now().UTC()
	}
	return m.Store.Mentioned(ctx, IDs(missed), at)
}
