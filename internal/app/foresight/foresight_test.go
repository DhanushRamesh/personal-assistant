package foresight_test

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/app/foresight"
	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/base/thinking/embed"
)

// words : An embedder whose meaning is the words: alike as they share them.
type words struct{}

func (words) Query(_ context.Context, text string) (embed.Vector, error) {
	v := make(embed.Vector, 64)
	for _, w := range strings.Fields(strings.ToLower(strings.Trim(text, "?.!"))) {
		h := 0
		for _, c := range w {
			h = h*31 + int(c)
		}
		v[(h%64+64)%64]++
	}
	return embed.Normalize(v), nil
}
func (w words) Documents(ctx context.Context, t []string) ([]embed.Vector, error) { return nil, nil }
func (words) Model() string                                                       { return "words" }
func (words) Available() bool                                                     { return true }

type past []chat.Asked

func (p past) FirstTools(context.Context, string, time.Time) ([]chat.Asked, error) {
	return p, nil
}

func seer(p past) *foresight.Seer {
	return &foresight.Seer{Embed: words{}, Past: p, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func tools(calls []foresight.Call) []string {
	var out []string
	for _, c := range calls {
		out = append(out, c.Tool)
	}
	return out
}

// A question like an example is foreseen; one like nothing is not.
func TestAQuestionLikeAnExampleIsForeseen(t *testing.T) {
	s := seer(nil)
	got := s.Foresee(context.Background(), "u", "am I free on Friday?", nil)
	if strings.Join(tools(got), ",") != "calendar_events" || got[0].Args != `{"days":7}` {
		t.Errorf("foresaw %+v", got)
	}
	if got := s.Foresee(context.Background(), "u", "open Spotify", nil); len(got) != 0 {
		t.Errorf("foresaw %v for a command", tools(got))
	}
}

// Their own past questions count as examples.
func TestTheirOwnPastQuestionsCount(t *testing.T) {
	s := seer(past{{Prompt: "anything special on October twenty two", Tool: "calendar_events"}})
	if got := tools(s.Foresee(context.Background(), "u", "anything special on October twenty two", nil)); strings.Join(got, ",") != "calendar_events" {
		t.Errorf("foresaw %v", got)
	}
}

// A short follow-up to a turn that used a reading is given it again; a
// long question is judged on its own.
func TestAShortFollowUpIsGivenWhatTheTurnBeforeUsed(t *testing.T) {
	s := seer(nil)
	got := s.Foresee(context.Background(), "u", "What about yesterday?", []string{"calendar_events"})
	if len(got) != 1 || got[0].Tool != "calendar_events" || !got[0].FollowUp {
		t.Errorf("foresaw %+v", got)
	}
	if got := s.Foresee(context.Background(), "u", "open the terminal and read me the last error it printed", []string{"calendar_events"}); len(got) != 0 {
		t.Errorf("foresaw %v for a long question", tools(got))
	}
}
