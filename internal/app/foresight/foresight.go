// Package foresight : Which reading a question is likely to need, guessed
// from its meaning before the model is asked, so it can be fetched ahead
// and the turn answered in one round rather than two.
//
// Owner, 17 October 2026: a turn that has to look something up takes two
// rounds of the model, ten seconds against five; fetching the common
// readings on every turn would cost their tokens on every turn too. So
// only what the question looks like it needs: the question's meaning
// against examples of each kind -- written here, and the person's own past
// questions that were answered with that tool -- and a short follow-up to
// a turn that used it ("what about yesterday?").
//
// Measured on three weeks of spoken questions as it would have run then,
// with only earlier questions as examples: of those answered first with
// the calendar, 67% foreseen; reminders 60%; places 75%; something
// fetched on 14% of turns. A question not foreseen costs what it did
// before; one foreseen wrongly costs a few kilobytes on that turn.
package foresight

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/base/thinking/embed"
)

// Domain : One reading that may be fetched ahead.
type Domain struct {
	// Tool : The tool that reads it.
	Tool string
	// Args : What it is called with, ahead.
	Args string
	// Examples : Questions it answers, written by hand. The person's own
	// past questions are added to these.
	Examples []string
	// Threshold : How alike, at least, a question must be to one of the
	// examples, from 0 to 1.
	Threshold float64
}

// Domains : The readings fetched ahead, chosen by use: the tools spoken
// questions reached for first most, that answer from their own reading.
var Domains = []Domain{
	{
		Tool: "calendar_events", Args: `{"days":7}`, Threshold: 0.78,
		Examples: []string{"what's on my calendar today", "what do I have tomorrow", "any meetings this afternoon",
			"am I free on Friday", "what's my schedule this week", "when is my next meeting",
			"do I have anything tomorrow morning", "what's on the diary for Monday", "is there anything planned tonight",
			"when is the dentist appointment", "what events do I have next week", "what time is my meeting"},
	},
	{
		Tool: "reminder_list", Args: `{}`, Threshold: 0.78,
		Examples: []string{"what reminders do I have", "any reminders for today", "what did I ask you to remind me",
			"list my reminders", "when is my next reminder", "what are my alarms", "do I have any reminders tomorrow",
			"is there a reminder set for this evening"},
	},
	{
		Tool: "place_visits", Args: `{}`, Threshold: 0.80,
		Examples: []string{"where was I today", "where did I go yesterday", "what places did I visit this week",
			"how long was I at the office", "when did I leave home", "when did I get to work",
			"where have I been this morning", "how long did I stay at the gym"},
	},
}

// FollowUpWords : How short a question may be to be taken as following
// the turn before it.
const FollowUpWords = 6

// PastDays : How far back the person's own questions are taken as
// examples.
const PastDays = 60

// Refresh : How often their past questions are read again.
const Refresh = 6 * time.Hour

// Past : Where the person's past questions are read.
type Past interface {
	FirstTools(ctx context.Context, userID string, since time.Time) ([]chat.Asked, error)
}

// Call : A reading to fetch ahead, and why.
type Call struct {
	Tool string
	Args string
	// Score : How alike the question was to the nearest example.
	Score float64
	// FollowUp : Chosen because the turn before used it.
	FollowUp bool
}

// Seer : Guesses the readings a question needs.
type Seer struct {
	Embed  embed.Embedder
	Past   Past
	Logger *slog.Logger
	Now    func() time.Time

	mu       sync.Mutex
	examples map[string][]embed.Vector // by tool, the hand-written ones
	past     map[string]pastVectors    // by user
}

type pastVectors struct {
	at   time.Time
	tool map[string][]embed.Vector
}

func (s *Seer) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Foresee : The readings this question looks like it needs. previous is
// the tools the turn before it in the conversation used, if it was recent.
// Nothing, when meanings cannot be had.
func (s *Seer) Foresee(ctx context.Context, userID, question string, previous []string) []Call {
	question = strings.TrimSpace(question)
	if s == nil || s.Embed == nil || !s.Embed.Available() || question == "" {
		return nil
	}
	v, err := s.Embed.Query(ctx, question)
	if err != nil {
		s.Logger.WarnContext(ctx, "foresight: cannot embed the question", slog.Any("error", err))
		return nil
	}
	hand := s.handWritten(ctx)
	own := s.pastOf(ctx, userID)
	short := len(strings.Fields(question)) <= FollowUpWords

	var out []Call
	for _, d := range Domains {
		best := 0.0
		for _, e := range hand[d.Tool] {
			best = max(best, embed.Similarity(v, e))
		}
		for _, e := range own[d.Tool] {
			best = max(best, embed.Similarity(v, e))
		}
		followUp := short && contains(previous, d.Tool)
		if best >= d.Threshold || followUp {
			out = append(out, Call{Tool: d.Tool, Args: d.Args, Score: best, FollowUp: followUp && best < d.Threshold})
		}
	}
	return out
}

// handWritten : The examples' meanings, made once.
func (s *Seer) handWritten(ctx context.Context) map[string][]embed.Vector {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.examples != nil {
		return s.examples
	}
	out := map[string][]embed.Vector{}
	for _, d := range Domains {
		for _, ex := range d.Examples {
			v, err := s.Embed.Query(ctx, ex)
			if err != nil {
				s.Logger.WarnContext(ctx, "foresight: cannot embed an example", slog.Any("error", err))
				return out
			}
			out[d.Tool] = append(out[d.Tool], v)
		}
	}
	s.examples = out
	return out
}

// pastOf : The meanings of the person's own past questions, by the tool
// each reached for first; read again every Refresh.
func (s *Seer) pastOf(ctx context.Context, userID string) map[string][]embed.Vector {
	if s.Past == nil || userID == "" {
		return nil
	}
	s.mu.Lock()
	kept, ok := s.past[userID]
	s.mu.Unlock()
	if ok && s.now().Sub(kept.at) < Refresh {
		return kept.tool
	}
	asked, err := s.Past.FirstTools(ctx, userID, s.now().AddDate(0, 0, -PastDays))
	if err != nil {
		s.Logger.WarnContext(ctx, "foresight: cannot read past questions", slog.Any("error", err))
		return kept.tool
	}
	wanted := map[string]bool{}
	for _, d := range Domains {
		wanted[d.Tool] = true
	}
	seen := map[string]bool{}
	tools := map[string][]embed.Vector{}
	for _, a := range asked {
		q := strings.TrimSpace(a.Prompt)
		key := a.Tool + "\x00" + strings.ToLower(q)
		if !wanted[a.Tool] || q == "" || seen[key] {
			continue
		}
		seen[key] = true
		v, err := s.Embed.Query(ctx, q)
		if err != nil {
			break
		}
		tools[a.Tool] = append(tools[a.Tool], v)
	}
	s.mu.Lock()
	if s.past == nil {
		s.past = map[string]pastVectors{}
	}
	s.past[userID] = pastVectors{at: s.now(), tool: tools}
	s.mu.Unlock()
	return tools
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
