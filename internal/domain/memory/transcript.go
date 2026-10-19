package memory

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// MaxQuoted : How many past exchanges are put in front of the model.
//
// Fewer than the curated memories. The transcript is larger and looser, so
// its near misses are nearer and more numerous.
//
// One, from 19 October 2026. Owner: *"the first priority is the current
// conversation!!"* and *"the bge retrieval of past conversation should
// have last preference"*. Last preference is not a sentence that can be
// written into a prompt and relied on -- the block already carries one --
// so it is spent here, on how many get in at all.
//
// It was three, and the third did the damage. Asked "Why did you say
// that?" of the sentence directly above it, the model was handed three
// exchanges scoring 0.818, 0.713 and 0.712. The first two were harmless,
// both of them it saying it could not tell what "that" meant. The third
// was a note about sleeping after 2 AM, four days old, and the answer
// was copied from it almost word for word. At one, it never arrives.
const MaxQuoted = 1

// Least : How alike a past exchange must be before it is offered at all.
//
// A search always returns its nearest, however far away that is: over
// 1146 turns the best match scored 0.655 on average and as little as
// 0.378, and every one of them went into the prompt. What comes back
// below this is not evidence, it is the nearest thing in a pile --
// "Yes, do it." matched "yes" at 0.603, and "You can use step search"
// matched a year-old "I have no tool for that" at 0.600.
//
// Deliberately low. The noise reaches 0.70 and no line separates it
// cleanly, so this cuts only what is indefensible and MaxQuoted does
// the rest. Raising it is a measurement, not an argument.
const Least = 0.55

// IndexBatch : How many exchanges are embedded in one call.
const IndexBatch = 32

// Exchange : Something said, with the reply it drew.
//
// A turn on its own is often unsearchable -- "yes", "try again" -- and means
// something only beside what it answered.
type Exchange struct {
	// MessageID : The message somebody sent. The exchange is keyed by it.
	MessageID string
	// UserID : Who said it.
	UserID string
	// ConversationID : Where it was said.
	ConversationID string
	// Text : The exchange as it is embedded and as it is shown.
	Text string
	// At : When it was said.
	At time.Time
}

// Heard : A past exchange that might bear on a question.
type Heard struct {
	// Exchange : What was said.
	Exchange Exchange
	// Score : How near it was. Comparable only against other scores from the
	// same search.
	Score float64
}

// Transcript : The searchable record of everything said.
type Transcript interface {
	// Unindexed : Exchanges with no vector from the named model, oldest
	// first, with their text already built.
	Unindexed(ctx context.Context, model string, limit int) ([]Exchange, error)

	// Index : Stores the vector for one exchange.
	Index(ctx context.Context, e Exchange, model string, vector []float32) error

	// NearestTo : The user's past exchanges closest to a vector, nearest
	// first. skipConversation is left out, since what was said there is
	// already in front of the model.
	NearestTo(ctx context.Context, userID string, q []float32, model string,
		skipConversation string, limit int) ([]Heard, error)
}

// ExchangeText : How a message and its reply are written down.
//
// Both halves are named, because a search hit is quoted back to the person
// and "you said" and "I said" are not the same claim.
func ExchangeText(said, reply string) string {
	said, reply = strings.TrimSpace(said), strings.TrimSpace(reply)
	if said == "" {
		return ""
	}
	if reply == "" {
		return "They said: " + said
	}
	return "They said: " + said + "\nYou answered: " + reply
}

// Quoted : What past exchanges look like in a system prompt.
//
// Kept apart from the curated memories, and stamped with when they were
// said. These are the record of what happened, not something the assistant
// concluded, and they include whatever speech-to-text got wrong, so they
// are quotable and never assertable.
//
// The time matters as much as the words. Asked when it had been given a
// list, the assistant answered that it held the list but no record of
// when -- which was true, because only the day was passed and only in UTC,
// so an evening in India read as the day before.
func Quoted(heard []Heard, loc *time.Location) string {
	if len(heard) == 0 {
		return ""
	}
	if loc == nil {
		loc = time.UTC
	}

	var b strings.Builder
	b.WriteString(precedence)
	b.WriteString("\n\n")
	b.WriteString("Earlier exchanges found by searching the record of what was actually said. ")
	b.WriteString("They are a transcript, not something you worked out: quote them as what was ")
	b.WriteString("said and when, and never state one as a fact of your own. Some arrived through ")
	b.WriteString("speech-to-text and may contain the wrong word. They were chosen for resembling ")
	b.WriteString("the question, which is not the same as bearing on it, so ignoring all of them ")
	b.WriteString("is often right. Each is stamped with when it was said, in the person's own ")
	b.WriteString("time, and you may say when something was said as readily as what was said.\n\n")

	b.WriteString("One of these is a trap and it is worth naming. Where an exchange says what ")
	b.WriteString("exists -- what reminders there are, what is on a list, what something is set ")
	b.WriteString("to, what a device is doing -- it is evidence of what was true when it was ")
	b.WriteString("said and evidence of nothing else. Your own earlier answer is the worst of ")
	b.WriteString("these, because it was right when you gave it and reads as though it still ")
	b.WriteString("is. It does not. A reminder answered for at nine o'clock has since gone ")
	b.WriteString("off; a list read out yesterday has been added to. Asked how any of it ")
	b.WriteString("stands now, call the tool that knows and answer from what it returns. ")
	b.WriteString("Repeating what is written here instead is not remembering, it is guessing ")
	b.WriteString("with a citation.\n\n")

	b.WriteString("The same goes double for anything here where you said you could not do ")
	b.WriteString("something. Searching by meaning gathers those together: ask about ")
	b.WriteString("changing a thing and back come three old refusals, because refusing is ")
	b.WriteString("what they have in common. They are not evidence about what you can do ")
	b.WriteString("now. What you can do is the tools you have been given this turn, and ")
	b.WriteString("nothing else -- you were given new ones since, and something you could ")
	b.WriteString("not do last week may be one call away today. Read the list you have ")
	b.WriteString("before you tell anybody no.")
	for i := range heard {
		b.WriteString("\n\n[")
		b.WriteString(heard[i].Exchange.At.In(loc).Format("Monday 2 January 2006 at 3:04 pm"))
		b.WriteString("]\n")
		b.WriteString(heard[i].Exchange.Text)
	}
	return b.String()
}

// Said : The past exchanges that might bear on a question.
//
// Nothing from the conversation in progress: it is already in front of the
// model, and offering it back reads as the assistant quoting itself.
func (r *Recall) Said(ctx context.Context, userID, question, skipConversation string) ([]Heard, error) {
	if r == nil || r.Transcript == nil || userID == "" || strings.TrimSpace(question) == "" {
		return nil, nil
	}
	if r.Embedder == nil || !r.Embedder.Available() {
		// No fallback to words here. The curated memories have one, and
		// searching the whole transcript by wording finds whatever repeated
		// a common word rather than whatever bore on the question.
		return nil, nil
	}

	// A question too short to carry a subject is not searched at all.
	//
	// It matches the shape of other short questions and nothing else:
	// measured over 1,334 of his, 1,039 -- 78% -- came back with
	// another question of six words or fewer, at a mean score of
	// 0.662, well clear of any floor.
	//
	// What it cost him, 19 October 2026. Told that Avengers: Doomsday
	// releases on 18 December, he asked "Are you sure?" and was
	// answered about Jailer 2. The one exchange recalled was "They
	// said: Are you sure? You answered: Yes, sir." from five minutes
	// earlier, scoring 0.797 -- a past bare question matched by being
	// a bare question, carrying its own subject in with it.
	//
	// Nothing is lost that is not already there. The conversation
	// itself is in the history, which is where a follow-up's subject
	// belongs; the curated memories are searched as before; and this
	// is the transcript search alone.
	if Brief(question) {
		return nil, nil
	}

	q, err := r.Embedder.Query(ctx, question)
	if err != nil {
		return nil, err
	}

	limit := r.Quoted
	if limit <= 0 {
		limit = MaxQuoted
	}
	heard, err := r.Transcript.NearestTo(ctx, userID, q, r.Embedder.Model(), skipConversation, limit)
	if err != nil {
		return nil, err
	}

	// Nearest is not the same as near. Dropped here rather than left to
	// the model, which is told in the block itself that ignoring all of
	// them is often right and does not.
	kept := heard[:0]
	for _, h := range heard {
		if h.Score >= Least {
			kept = append(kept, h)
		}
	}
	return kept, nil
}

// IndexTranscript : Gives a vector to exchanges that have none.
//
// At startup and after each turn. It reports how many it managed.
func (r *Recall) IndexTranscript(ctx context.Context, limit int) (int, error) {
	if r == nil || r.Transcript == nil || r.Embedder == nil || !r.Embedder.Available() || limit <= 0 {
		return 0, nil
	}

	model := r.Embedder.Model()
	var done int

	for done < limit {
		batch := limit - done
		if batch > IndexBatch {
			batch = IndexBatch
		}

		pending, err := r.Transcript.Unindexed(ctx, model, batch)
		if err != nil {
			return done, err
		}
		if len(pending) == 0 {
			return done, nil
		}

		texts := make([]string, 0, len(pending))
		for i := range pending {
			texts = append(texts, pending[i].Text)
		}

		vectors, err := r.Embedder.Documents(ctx, texts)
		if err != nil {
			return done, fmt.Errorf("memory: embedding %d exchanges: %w", len(texts), err)
		}
		if len(vectors) != len(pending) {
			return done, fmt.Errorf("memory: asked for %d vectors, got %d", len(pending), len(vectors))
		}

		for i := range pending {
			if err := r.Transcript.Index(ctx, pending[i], model, vectors[i]); err != nil {
				r.log(ctx, "cannot store a vector for an exchange", err)
				continue
			}
			done++
		}
	}
	return done, nil
}

// subjectless : Words that name nothing, so a question made only of
// them has no subject to search the transcript with.
//
// Pronouns and function words, and the handful of words a follow-up is
// actually built from -- sure, right, again, more, that one. The
// assistant's own name is stripped before this is consulted, since
// "Okay, Jarvis" is two words of address and none of subject.
var subjectless = words(`a an the of in on at to for and or but with from by about as so
is are was were am be been being do does did done will would can could shall should may might have has had
i me my mine you your yours he him his she her it its we us our they them their this that these those
what which who whom whose when where why how if then than not no yes ok okay
sure right correct true really again more else other another same one two both either neither
say said tell told mean meant think thought know knew get got go went come came take took give gave
do it that one thing anything something nothing now still just only even also please thanks thank`)

// words : A lookup built from whitespace-separated text.
func words(list string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.Fields(list) {
		out[w] = true
	}
	return out
}

// Brief : Whether a question has nothing in it to search the
// transcript with.
//
// Not a count of words. "what did the roofer quote" is five words and
// names a roofer; "Are you sure?" is three and names nothing. The
// first is worth searching and the second matches the shape of every
// other bare question ever asked.
//
// Judged by what is left after the pronouns, the function words and
// the handful a follow-up is built from. Nothing left, nothing to
// search with.
func Brief(question string) bool {
	for _, w := range strings.Fields(strings.ToLower(question)) {
		w = strings.Trim(w, `.,!?;:'"()`)
		if w != "" && !subjectless[w] {
			return false
		}
	}
	return true
}
