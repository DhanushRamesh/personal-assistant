// Package coming : Lets the assistant stop warning about something
// in the diary that the person has said they are dealing with.
//
// The look-ahead speaks days before a thing, and then again on the
// day. That second word is right for somebody who forgot and wrong
// for somebody who answered the first one. Owner: *"if I acknowledge
// it, it should act accordingly. 'Ok got it' means no need of that
// reminder any more, because I'm already on it. Or 'cancel that' and
// that reminder should go."*
//
// Which words mean that is the model's to decide and not this
// server's. "Ok got it", "done", "already bought it", "I don't need
// reminding", and the same in any other language, all land here;
// nothing in the code matches a phrase.
package coming

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"personal-assistant/internal/app/tool"
	"personal-assistant/internal/base/chat"
	"personal-assistant/internal/domain/coming"
)

// Warnings : What this needs of the look-ahead's record.
type Warnings interface {
	// Held : Every judgement for entries that have not started.
	Held(ctx context.Context, userID string, from time.Time) ([]coming.Judged, error)
	// Settle : Records that nothing more is to be said about one.
	Settle(ctx context.Context, userID, eventID string, at time.Time) error
	// Trim : Records what is left to do, once part of it is done.
	Trim(ctx context.Context, userID, eventID, still string, at time.Time) error
}

// Clock : What this needs to know about time.
type Clock struct {
	// Now : The moment, in UTC. Nil uses the real clock.
	Now func() time.Time
	// Location : The person's zone, which is what a written hour means.
	Location *time.Location
}

func (c Clock) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

func (c Clock) where() *time.Location {
	if c.Location != nil {
		return c.Location
	}
	return time.UTC
}

// Tools : What the assistant can do about a warning it has given.
func Tools(warnings Warnings, clock Clock) []tool.Tool {
	return []tool.Tool{settled(warnings, clock)}
}

// settled : Stop warning about one thing in the diary.
func settled(warnings Warnings, clock Clock) tool.Tool {
	return tool.Tool{
		Name:   "coming_settled",
		Domain: "coming",
		Purpose: "Stop warning about something in their diary, because they have said they are " +
			"already dealing with it or do not want to be told about it again.",
		UseWhen: "You warned them, unprompted, about something coming up in their diary -- a " +
			"sentence of yours naming the thing, when it is, and what there is to do first -- " +
			"and the next thing they say means there is nothing left to tell them. " +
			"They have done it, they are doing it, they already knew, or they want you to " +
			"stop. Any words that mean that, in any language: ok, got it, fine, noted, done, " +
			"already bought it, stop telling me, cancel that, leave it. " +
			"Then ask one thing before you call it: have they dealt with ALL of what your " +
			"warning asked for, or only part of it? " +
			"All of it, and you call this with nothing and the warning is dropped. " +
			"Part of it, and the rest goes in left: the warning stays and asks only for that. " +
			"A warning that named two things and an answer that mentions one is part of it. " +
			"Look at what you said last before deciding; if it was one of those warnings, a " +
			"short acknowledgement is almost always this and almost never anything else. " +
			"Do not look the thing up first -- you have just said what it is, and the warning " +
			"is yours rather than theirs to find. " +
			"Use it as well as answering them, not instead: say something back, and call this " +
			"so the same warning is not given again nearer the time.",
		Avoid: "Not for taking the thing out of their diary, which is calendar_cancel and is a " +
			"different act entirely: this only stops you mentioning it. " +
			"Not for anything they set themselves. A reminder they asked for is reminder_cancel " +
			"even when they say cancel that, and the difference is whose idea it was: this is " +
			"only ever for a warning you gave first. " +
			"Not when they are asking about the thing, changing it, or saying anything other " +
			"than that they are on top of it.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			// left is required, and that is the point of it. Asked
			// for only when it applied, it was skipped two times in
			// three and a warning naming two things was dropped on
			// an answer about one of them. Made unskippable, the
			// decision is taken every time.
			Required: []string{"left"},
			Properties: map[string]tool.Property{
				"about": {
					Type: "string",
					Description: "What they called the thing, in their own words. Leave it out " +
						"when they are plainly answering the warning you just gave and have " +
						"named nothing, which is the usual case.",
				},
				"left": {
					Type: "string",
					Description: "What is STILL to do after what they have just said. Always " +
						"answer this. Take your own warning, strike out the part they have " +
						"dealt with, and write what remains, the way you would say it aloud " +
						"and under a dozen words. Warned about \"a present to buy and " +
						"something to wear\" and told the present is bought, this is " +
						"\"Something to wear.\" Write " + coming.Silence + " when nothing " +
						"remains, which drops the warning altogether.",
				},
			},
		},
		Examples: []tool.Example{
			{Ask: "ok got it", Args: `{"saying":"noted"}`},
			{Ask: "cancel that reminder", Args: `{"saying":"dropping it"}`},
			{Ask: "I've already bought the present",
				Args: `{"left":"Something to wear.","saying":"noted"}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				About string `json:"about"`
				Left  string `json:"left"`
			}
			_ = json.Unmarshal(in.Args, &args)

			if warnings == nil {
				return tool.Failed("Nothing on this server is looking ahead in your diary.")
			}
			if in.Caller.UserID == "" {
				return tool.Failed("This request did not come from a known person.")
			}

			now := clock.now()
			held, err := warnings.Held(ctx, in.Caller.UserID, now)
			if err != nil {
				return tool.Broke("What is coming could not be read.", err)
			}

			// Only the ones that have actually been warned about and
			// have something left to say. Settling a thing never
			// mentioned would be acting on a warning nobody heard.
			var open []coming.Judged
			for _, j := range held {
				if !j.Spoke.IsZero() && !j.Over(now) {
					open = append(open, j)
				}
			}
			if len(open) == 0 {
				return tool.OK("There is no warning outstanding to drop: nothing has been " +
					"mentioned lately that still has something to say nearer the time.")
			}

			chosen, trouble := pick(open, strings.TrimSpace(args.About), clock.where())
			if trouble != "" {
				return tool.OK(trouble)
			}

			// Part of it done is not all of it done. The warning
			// stays and says only what is left, because dropping it
			// would silence the shirt along with the present.
			if left := remaining(args.Left); left != "" {
				if err := warnings.Trim(ctx, in.Caller.UserID, chosen.EventID, left, now); err != nil {
					return tool.Broke("That could not be settled.", err)
				}
				return tool.OK(fmt.Sprintf("Noted against %q. The warning nearer the time "+
					"will ask only for: %s", chosen.Title, left))
			}

			if err := warnings.Settle(ctx, in.Caller.UserID, chosen.EventID, now); err != nil {
				return tool.Broke("That could not be settled.", err)
			}
			return tool.OK(fmt.Sprintf("Nothing more will be said about %q before it. "+
				"It stays in their diary; only the warning is dropped.", chosen.Title))
		},
	}
}

// pick : Which outstanding warning they meant.
//
// Named nothing, they mean the one just given: that is what an
// answer answers. Named something, it is matched by likeness,
// because what reaches here came through speech and "the wedding"
// has to find "Attending Vignesh's wedding".
func pick(open []coming.Judged, about string, where *time.Location) (coming.Judged, string) {
	if about == "" {
		latest := open[0]
		for _, j := range open[1:] {
			if j.Spoke.After(latest.Spoke) {
				latest = j
			}
		}
		return latest, ""
	}

	var matched []coming.Judged
	for _, j := range open {
		if likeness(j.Title, about) {
			matched = append(matched, j)
		}
	}

	switch len(matched) {
	case 1:
		return matched[0], ""
	case 0:
		return coming.Judged{}, "Nothing outstanding sounds like " + about +
			". These have been mentioned and have something left to say: " +
			listing(open, where) + ". Ask them which they mean, or leave it."
	default:
		return coming.Judged{}, "More than one could be meant by " + about + ": " +
			listing(matched, where) + ". Ask them which."
	}
}

// Enough : The shortest run of letters allowed to match on its own.
//
// Three would let "wed" find "Wednesday standup", which is a
// different thing on a different day.
const Enough = 4

// likeness : Whether a title is near enough to what they said.
//
// Either contains the other, or they share a word, so that "the
// wedding" finds "Attending Vignesh's wedding". Nothing shorter than
// Enough matches on its own, letters or words.
func likeness(title, about string) bool {
	t, a := strings.ToLower(title), strings.ToLower(about)
	if len(a) >= Enough && strings.Contains(t, a) {
		return true
	}
	if len(t) >= Enough && strings.Contains(a, t) {
		return true
	}
	for _, word := range strings.Fields(a) {
		if len(word) < Enough {
			continue
		}
		for _, in := range strings.Fields(t) {
			if strings.EqualFold(strings.Trim(in, ",.!?'\""), word) {
				return true
			}
		}
	}
	return false
}

// listing : Outstanding warnings, as the model should read them back.
func listing(open []coming.Judged, where *time.Location) string {
	parts := make([]string, 0, len(open))
	for _, j := range open {
		parts = append(parts, fmt.Sprintf("%q on %s",
			j.Title, j.Starts.In(where).Format("Monday 2 January")))
	}
	return strings.Join(parts, "; ")
}

// remaining : What is still to do, or empty when nothing is.
//
// The model is made to answer every time, so the answer has to
// carry "nothing" as well as a list. Anything that is only the word
// for nothing, however it is punctuated, is nothing.
func remaining(left string) string {
	trimmed := strings.TrimSpace(left)
	if strings.EqualFold(strings.Trim(trimmed, ".!\"' "), coming.Silence) {
		return ""
	}
	return trimmed
}
