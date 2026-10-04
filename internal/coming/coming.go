// Package coming : Says what is in the diary before it arrives.
//
// The diary is the one thing the person reads constantly and the one
// thing that never speaks first. Reminders have had a loop since the
// beginning; appointments have had none, so an appointment is known
// only to somebody who thinks to ask. Measured over a week, the diary
// was read a hundred and four times and never once offered.
//
// What makes this more than an offset is the owner's own description:
// *"a movie at 6 is fine a few hours before, maximum a day. Attending
// a wedding has to be said a day or two before, because I need to
// plan -- buy a gift, iron the shirts. It takes effort. So the
// reminder start depends on what action it is."*
//
// So the lead time is a judgement about how much doing there is
// before a thing, and nothing in this package knows what a wedding or
// a film is. A model reads the entry once, when it first appears, and
// says how much warning it needs and why. That decision is kept, so
// the model is asked once per entry rather than once per tick.
package coming

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

const (
	// Every : How often the diary is looked at.
	//
	// Leads are measured in hours and days, so a minute's precision
	// buys nothing and costs a calendar read a minute. Five minutes
	// is under three hundred reads a day and still puts "time to
	// leave" within five minutes of where the model asked for it.
	Every = 5 * time.Minute

	// Ahead : How far into the diary a look reads.
	//
	// Longer than the longest lead anything may be given, or a thing
	// needing a week's warning would first be seen six days too late.
	Ahead = 21 * 24 * time.Hour

	// Longest : The most warning anything may be given.
	//
	// A ceiling on the model rather than a judgement of its own. A
	// fortnight's notice of a dentist is not preparation, it is a
	// thing said so early that it is forgotten by the time it
	// matters.
	Longest = 7 * 24 * time.Hour

	// Shortest : The least warning worth treating as warning.
	//
	// Under this it is not notice, it is narration: a thing announced
	// four minutes before it starts is heard at the same moment it is
	// seen.
	Shortest = 5 * time.Minute

	// Within : How long one judgement may take, including the model.
	Within = 30 * time.Second

	// Silence : What the model says when a thing needs no warning.
	//
	// Most of the diary is like this. A holiday, a birthday, a block
	// somebody else put there: nothing is owed and nothing is said.
	Silence = "NOTHING"
)

// Lead : One moment before something, worth a word of its own.
//
// Several, because the owner's two examples are not the same shape. A
// film wants one word, a few hours out. A wedding wants two: days
// before, while there is still time to buy something and get a shirt
// ready, and again on the day, when the only thing left to do is
// leave. Deciding how many is part of what is being judged, so it is
// the model's to say and not a number in this file.
type Lead struct {
	// Before : How long before it starts.
	Before time.Duration
	// Why : What there is to do by then, in the model's own words.
	// Shown on the page and used to write the sentence.
	Why string
}

// Judged : What was decided about one entry in the diary.
type Judged struct {
	// UserID : Whose diary.
	UserID string
	// EventID : Google's identifier for it.
	EventID string
	// Shape : What the entry was when it was judged.
	//
	// An entry can be renamed or moved, and a judgement about the
	// thing it used to be is worthless: "dinner" becoming "dinner
	// with the Raos at their place" is a different amount of doing.
	// A changed shape throws the judgement away and asks again.
	Shape string
	// Title, Starts : Enough to say the sentence without reading the
	// diary again.
	Title  string
	Starts time.Time
	// Leads : When to speak, longest warning first.
	Leads []Lead
	// Said : How many of them have been spoken.
	//
	// A count rather than a set, because they are spoken in order and
	// a count cannot disagree with itself.
	Said int
	// Spoke : When the last warning about this was said. Zero until
	// one is, which is most of them.
	Spoke time.Time
	// Settled : When they said they were on top of it.
	//
	// Owner: *"'ok got it' means no need of that reminder any more,
	// because I'm already on it."* A warning given three days out
	// and acted on should not say itself again on the day.
	Settled time.Time
	// At : When the judgement was made.
	At time.Time
}

// Shape : What an entry is, for deciding whether a judgement still
// holds.
//
// The title and the start, which are the two things that change what
// preparation it needs. Not the notes or the place: moving a dinner
// across town does not change that it is a dinner, and re-asking the
// model every time somebody tidies a description would cost a call a
// tick for no decision.
//
// And which question was asked. A judgement is only as good as the
// wording that produced it, and the first wording was wrong: it put
// "a birthday nobody has to attend" among the things needing no
// warning, so every birthday was judged silent. Owner: *"birthdays
// should be told a few days before to buy a gift."* Fixing the words
// fixed nothing, because a hundred entries were already decided
// under the old ones and nothing would ask again.
//
// So the wording is part of the shape. Change the question and every
// judgement made under the old one stops matching and is made again,
// which is the only behaviour that is ever right: a kept answer to a
// question nobody asks any more is worse than no answer.
func Shape(title string, starts time.Time) string {
	return strings.TrimSpace(strings.ToLower(title)) + "@" +
		starts.UTC().Format(time.RFC3339) + "#" + asked
}

// asked : A short fingerprint of the question, so that changing it
// invalidates what it decided.
var asked = func() string {
	sum := sha256.Sum256([]byte(Asking))
	return hex.EncodeToString(sum[:4])
}()

// Due : Which lead, if any, is owed now.
//
// Returns the index of the first unspoken lead whose moment has come,
// and whether there is one. Later leads are not skipped past: if two
// came due while the server was off, the earlier is said first and
// the next on the following tick, because they say different things
// and the order is the point.
func (j Judged) Due(now time.Time) (int, bool) {
	for i := j.Said; i < len(j.Leads); i++ {
		if !now.Before(j.Starts.Add(-j.Leads[i].Before)) {
			return i, true
		}
	}
	return 0, false
}

// Over : Whether an entry has nothing left to say.
//
// Everything was said, or it has started, or they have said they are
// dealing with it. A thing already under way is not something to
// give warning of, an entry never judged in time has missed its
// moment rather than earned a late one, and somebody who answered
// the first warning does not need the second.
func (j Judged) Over(now time.Time) bool {
	return j.Said >= len(j.Leads) || !j.Settled.IsZero() || !now.Before(j.Starts)
}

// Sentence : What the person is told.
//
// Written here rather than by the model, which has already done the
// judging. Asking it twice would cost a second call and risk a
// second opinion.
func (j Judged) Sentence(i int, now time.Time, where *time.Location) string {
	if i < 0 || i >= len(j.Leads) {
		return ""
	}
	if where == nil {
		where = time.UTC
	}

	when := Away(j.Starts.Sub(now))
	at := j.Starts.In(where).Format("3:04 pm")
	if j.Starts.In(where).YearDay() != now.In(where).YearDay() {
		at = j.Starts.In(where).Format("Monday, 3:04 pm")
	}

	out := fmt.Sprintf("%s is %s, at %s.", strings.TrimSpace(j.Title), when, at)
	if why := strings.TrimSpace(j.Leads[i].Why); why != "" {
		out += " " + why
	}
	return out
}

// Away : A stretch of time as somebody would say it.
func Away(d time.Duration) string {
	switch {
	case d < time.Hour:
		return "in " + plural(int(d.Round(time.Minute).Minutes()), "minute")
	case d < 24*time.Hour:
		return "in " + plural(int(d.Round(time.Hour).Hours()), "hour")
	default:
		return "in " + plural(int((d+12*time.Hour).Hours()/24), "day")
	}
}

// plural : A number and its noun, singular when it is one.
func plural(n int, noun string) string {
	if n <= 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
