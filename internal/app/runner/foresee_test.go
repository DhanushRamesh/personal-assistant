package runner_test

import (
	"context"
	"strings"
	"testing"

	"personal-assistant/internal/app/foresight"
	"personal-assistant/internal/app/runner"
	"personal-assistant/internal/app/tool"
	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/base/thinking/environment"
)

// seeing : Foresees the calendar for every question, and notes what it was asked.
type seeing struct{ asked []string }

func (s *seeing) Foresee(_ context.Context, _, question string, _ []string) []foresight.Call {
	s.asked = append(s.asked, question)
	return []foresight.Call{{Tool: "calendar_events", Args: `{"days":7}`, Score: 0.9}}
}

func aheadDiary(t *testing.T, ran *string) *tool.Registry {
	t.Helper()
	r, err := tool.NewRegistry(tool.Tool{
		Name: "calendar_events", Domain: "calendar", Lists: true,
		Purpose: "Read events.", UseWhen: "They ask what is on.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params:   tool.Schema{Properties: map[string]tool.Property{"days": {Type: "integer", Description: "Days ahead"}}},
		Run: func(_ context.Context, in tool.Invocation) tool.Result {
			*ran = string(in.Args)
			return tool.OK("Tomorrow: dentist at 10.")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// Off, a foreseen reading is only logged: nothing is fetched or put in
// the prompt.
func TestForeseeingOffFetchesNothing(t *testing.T) {
	var ran string
	env := &noting{scripted: &scripted{rounds: []environment.Message{environment.Final("Nothing, sir.")}}}
	s := &seeing{}
	h := newHarness(t, env, runner.Options{Tools: aheadDiary(t, &ran), Foresight: s})
	tk := h.submitOn(t, chat.ChannelVoice, "what do I have tomorrow")
	h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

	env.mu.Lock()
	defer env.mu.Unlock()
	if len(s.asked) != 1 || ran != "" || strings.Contains(env.prompts[0], "Tomorrow: dentist") {
		t.Errorf("asked %v, ran %q, prompt had the reading: %v", s.asked, ran, strings.Contains(env.prompts[0], "Tomorrow: dentist"))
	}
}

// On, it is fetched with its arguments and put in the prompt, and the
// answer is not sent back to look again: one round.
func TestForeseeingOnFetchesAheadInOneRound(t *testing.T) {
	var ran string
	env := &noting{scripted: &scripted{rounds: []environment.Message{environment.Final("A dentist at ten, sir.")}}}
	h := newHarness(t, env, runner.Options{Tools: aheadDiary(t, &ran), Foresight: &seeing{}, Foreseeing: true})
	tk := h.submitOn(t, chat.ChannelVoice, "what do I have tomorrow")
	h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

	env.mu.Lock()
	defer env.mu.Unlock()
	if ran != `{"days":7}` {
		t.Errorf("ran with %q", ran)
	}
	if !strings.Contains(env.prompts[0], "Tomorrow: dentist at 10.") || !strings.Contains(env.prompts[0], "Read ahead") {
		t.Errorf("the reading is not in the prompt:\n%s", env.prompts[0])
	}
	if len(env.offered) != 1 {
		t.Errorf("asked %d times, want once", len(env.offered))
	}
}
