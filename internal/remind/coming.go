package remind

import (
	"context"
	"strings"
	"time"
)

// MostShown : The most reminders put into a prompt.
//
// A bound, not a guess. Somebody with forty reminders does not want them
// recited and the model does not need them all to answer honestly; past
// this it is told there are more and reminder_list is how to see them.
const MostShown = 10

// Coming : What is waiting, for the prompt.
//
// Read afresh every turn, which is the whole point. Asked what reminders
// there were, the assistant twice answered from an exchange two hours
// old -- its own earlier answer, correct when it was given, repeated as
// though it still held. The 11 o'clock it named had gone off at 11
// o'clock. No tool had been called, and nothing made it call one.
//
// A rule telling it to look things up did not hold, and could not be
// made to fail on demand afterwards, so there was no way to know whether
// a stronger rule would hold either. This needs no rule: the true answer
// is already in front of it, so there is nothing to skip and nothing
// older to reach for.
func Coming(waiting []Reminder, at time.Time, loc *time.Location) string {
	if loc == nil {
		loc = time.UTC
	}

	var b strings.Builder
	b.WriteString("Reference, for one kind of question only. The reminders this person ")
	b.WriteString("has waiting, read from the store as this prompt was built, so it is ")
	b.WriteString("what is true now and not what was true when anything else here was ")
	b.WriteString("said.\n\n")
	b.WriteString("Say nothing about any of it unless they ask what reminders they have, ")
	b.WriteString("what is coming, or about one of these in particular. That is the only ")
	b.WriteString("thing this is for.\n\n")
	b.WriteString("Everything else is not such a question, however close it sounds. ")
	b.WriteString("Somebody saying they are tired, or hungry, or that it is late, is ")
	b.WriteString("telling you how they are, not asking what is on their list. Answer ")
	b.WriteString("what they said. Bringing up a reminder they did not ask about is not ")
	b.WriteString("helpful, it is the assistant talking about its own filing, and they ")
	b.WriteString("will hear it when it goes off anyway.\n\n")

	if len(waiting) == 0 {
		b.WriteString("There are none. If they ask, tell them so plainly. Do not reach ")
		b.WriteString("back through the conversation for one you mentioned earlier: if ")
		b.WriteString("it is not here it has gone off, been put off or been called off, ")
		b.WriteString("and saying it is still waiting would be untrue.")
		return b.String()
	}

	b.WriteString("When they do ask, answer from this and from nothing else. Anything ")
	b.WriteString("said earlier about what is waiting -- including your own answer, ")
	b.WriteString("which was right when you gave it -- has been overtaken by this.")

	shown := waiting
	if len(shown) > MostShown {
		shown = shown[:MostShown]
	}
	for i := range shown {
		b.WriteString("\n- ")
		b.WriteString(shown[i].DueAt.In(loc).Format("3:04 pm on Monday 2 January"))
		b.WriteString(repeating(shown[i].Repeats))
		b.WriteString(": ")
		b.WriteString(strings.TrimSpace(shown[i].Body))
	}
	if len(waiting) > len(shown) {
		b.WriteString("\n(and ")
		b.WriteString(plural(len(waiting) - len(shown)))
		b.WriteString(" more; reminder_list shows them all)")
	}
	return b.String()
}

// repeating : How often it comes back, as a phrase, or nothing.
func repeating(r Repeat) string {
	if r == Once {
		return ""
	}
	return ", " + string(r)
}

// plural : A count, as words a sentence can carry.
func plural(n int) string {
	if n == 1 {
		return "one"
	}
	return itoa(n)
}

// itoa : A small number as digits, without dragging in strconv for one use.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	return string(d)
}

// Waiting : What is waiting, and the block describing it.
//
// Shaped like Missing, and for the same reason: reading it is the
// runner's business, knowing what it means is this package's.
type Waiting struct {
	// Store : Where reminders are kept. Nil says nothing.
	Store Store
	// Location : The person's zone, for saying when each is due.
	Location *time.Location
	// Now : The clock, replaceable in tests. Nil uses the real one.
	Now func() time.Time
}

// Block : What to add to a prompt. Empty when there is no store.
func (w *Waiting) Block(ctx context.Context, userID string) (string, error) {
	if w == nil || w.Store == nil || userID == "" {
		return "", nil
	}

	at := time.Now().UTC()
	if w.Now != nil {
		at = w.Now().UTC()
	}

	// Held ones are waiting too: they have not been said, and somebody
	// asking what is coming would be surprised to be told they are not.
	waiting, err := w.Store.List(ctx, userID, Pending, Held)
	if err != nil {
		return "", err
	}
	return Coming(waiting, at, w.Location), nil
}
