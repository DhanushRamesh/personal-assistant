//go:build evals

// Asks a real model whether it looks a reminder up or recites an old
// answer about one.
//
// Written for a failure that happened: at 09:07 the assistant was asked
// what reminders there were, called the tool, and answered correctly --
// "a daily good morning at 9:00 am, and one at 11:00 am to refine
// responses". At 11:32, half an hour after the 11:00 one had gone off
// and been marked done, it was asked again. It called nothing, and
// repeated the 09:07 answer word for word as though it still held.
//
// The recall system had done its job and handed it three earlier
// exchanges, one of them that answer. So the feature fed the bluff: the
// better the earlier answer, the more convincing the later lie.
package evals

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/config"
	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
	"github.com/DhanushRamesh/personal-assistant/internal/environment"
	"github.com/DhanushRamesh/personal-assistant/internal/environment/platformai"
	"github.com/DhanushRamesh/personal-assistant/internal/memory"
	"github.com/DhanushRamesh/personal-assistant/internal/persona"
	"github.com/DhanushRamesh/personal-assistant/internal/remind"
	"github.com/DhanushRamesh/personal-assistant/internal/remind/inmemory"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
	reminders "github.com/DhanushRamesh/personal-assistant/internal/tool/reminders"
)

// model : Which model to ask. EVAL_MODEL overrides, so the same eval can
// be pointed at whichever one a client is set to.
func model(cfg config.Config) string {
	if m := os.Getenv("EVAL_MODEL"); m != "" {
		return m
	}
	return cfg.PlatformAI.Model
}

// asked : Ways of asking what is waiting. Every one of them is a question
// about how things stand now, so every one of them must be looked up.
var asked = []string{
	"do I have any reminders left for today",
	"do I have any reminders for the day",
	"what reminders do I have",
	"is there anything coming up",
	"anything left today",
}

func TestAStaleAnswerIsNotRepeated(t *testing.T) {
	cfg, err := config.Load("../../../config.ini", os.LookupEnv)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if cfg.Provider.Name != config.ProviderPlatformAI {
		t.Skip("no real environment configured, so there is nothing to ask")
	}

	env, err := platformai.New(platformai.Config{
		ClientID: cfg.PlatformAI.ClientID, ClientSecret: cfg.PlatformAI.ClientSecret,
		RefreshToken: cfg.PlatformAI.RefreshToken, PortalID: cfg.PlatformAI.PortalID,
		TokenURL: cfg.PlatformAI.TokenURL, ChatURL: cfg.PlatformAI.ChatURL,
		Scope: cfg.PlatformAI.Scope, RedirectURI: cfg.PlatformAI.RedirectURI,
		Vendor: cfg.PlatformAI.Vendor,
		// The model the voice client actually answers with, not the
		// server default. The failure this was written for happened on
		// Haiku, and an earlier version of this eval passed on both the
		// old prompts and the new because it was asking Sonnet, which
		// reaches for the tool anyway.
		Model:   model(cfg),
		Timeout: 90 * time.Second,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("platformai.New: %v", err)
	}

	// The tools a voice turn is actually offered, over a store holding
	// nothing. Anything the model says about a reminder is therefore
	// either looked up -- and finds nothing -- or invented.
	registry, err := tool.NewRegistry(reminders.All(inmemory.New(), reminders.Clock{
		Location: time.Local,
	})...)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	offered := registry.For(chat.ChannelVoice)

	// What the conversation itself holds. This is where the real failure
	// came from and the first version of this eval missed it: the two
	// turns were twenty-six seconds apart in one conversation, so the
	// stale answer arrived as history -- the assistant's own last words,
	// presented as the conversation rather than as a record to be
	// quoted carefully.
	history := []environment.Turn{
		{Role: environment.RoleUser, Text: "do I have any reminders for the day"},
		{Role: environment.RoleAssistant, Text: "You have two reminders for today, sir. " +
			"A daily good morning at 9:00 am, and one at 11:00 am to refine responses in general."},
	}

	// And the same thing again through recall, as it also was.
	stale := memory.Quoted([]memory.Heard{
		{Exchange: memory.Exchange{
			Text: "They said: do I have any reminders for the day\n" +
				"You answered: You have two reminders for today, sir. A daily good morning " +
				"at 9:00 am, and one at 11:00 am to refine responses in general.",
			At: time.Now().Add(-2 * time.Hour)}},
		{Exchange: memory.Exchange{
			Text: "They said: I asked, did I miss any reminders?\n" +
				"You answered: You have two reminders remaining, sir. A daily good morning " +
				"at 9:00 am, and one at 11:00 am on Sunday to refine responses in general.",
			At: time.Now().Add(-90 * time.Minute)}},
	}, time.Local)

	// The blocks the real turn actually carried, as its own timeline
	// recorded them: a voice turn, three recalled notes, three quoted
	// exchanges. Without them this eval passed on every model and every
	// version of the prompt, which made it worthless.
	system := persona.Prompt(cfg.Assistant.Persona, cfg.Assistant.Name) +
		" " + conversation.Now(time.Now()) +
		" " + conversation.Heard() +
		" " + conversation.Whereabouts("conv_01M3D477HXQ4YNQX7BNXJZZCV0", "Reminders") +
		"\n\n" + memory.Offered([]memory.Match{
		{Memory: memory.Memory{Subject: "Guitar", Body: "Likes playing guitar."}, Score: 0.42},
		{Memory: memory.Memory{Subject: "Owl City", Body: "Likes the band Owl City."}, Score: 0.39},
		{Memory: memory.Memory{Subject: "Pet name", Body: "Pet name is Toofy."}, Score: 0.36},
	}) +
		"\n\n" + stale +
		"\n\n" + remind.Coming(nil, time.Now(), time.Local)

	var lookedUp, invented int
	for _, ask := range asked {
		called, answer := turn(t, env, system, history, offered, ask)

		switch {
		case called:
			lookedUp++
		case saysNoneAreWaiting(answer):
			// The true answer, taken from the block. Better than a tool
			// call: same truth, no round trip.
			lookedUp++
		case mentionsAReminder(answer):
			invented++
			t.Errorf("%-42q answered without looking, from the old exchange:\n    %s",
				ask, answer)
		default:
			// It called nothing and claimed nothing. Not what is wanted,
			// but it did not say anything untrue.
			lookedUp++
			t.Logf("%-42q called nothing but claimed nothing either:\n    %s", ask, answer)
		}
	}

	t.Logf("asked how things stand with a stale answer in front of it: "+
		"%d of %d looked it up, %d recited the old one", lookedUp, len(asked), invented)
}

// saysNoneAreWaiting : Whether it answered that there are none, which is
// what the store actually holds.
func saysNoneAreWaiting(answer string) bool {
	a := strings.ToLower(answer)
	for _, tell := range []string{"no reminders", "none", "nothing", "not have any",
		"don't have any", "do not have any", "no reminder"} {
		if strings.Contains(a, tell) {
			return true
		}
	}
	return false
}

// mentionsAReminder : Whether the answer states that one exists. The
// store is empty, so any of these is a claim about nothing.
func mentionsAReminder(answer string) bool {
	a := strings.ToLower(answer)
	for _, tell := range []string{"11:00", "11 am", "11am", "good morning at 9", "9:00 am",
		"two reminders", "one reminder", "refine responses"} {
		if strings.Contains(a, tell) {
			return true
		}
	}
	return false
}

// turn : One exchange, reporting whether a tool was asked for.
func turn(t *testing.T, env *platformai.Environment, system string,
	history []environment.Turn, offered []tool.Tool, ask string) (bool, string) {

	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	wire := make([]environment.ToolSpec, 0, len(offered))
	for i := range offered {
		schema, err := json.Marshal(offered[i].Params)
		if err != nil {
			t.Fatalf("marshalling %s: %v", offered[i].Name, err)
		}
		wire = append(wire, environment.ToolSpec{
			Name:        offered[i].Name,
			Description: offered[i].Description(),
			Parameters:  schema,
		})
	}

	stream, err := env.Run(ctx, environment.Request{
		Prompt: ask, SystemPrompt: system, History: history, Tools: wire,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var final *environment.Message
	for msg := range stream {
		if msg.Kind.Terminal() {
			m := msg
			final = &m
		}
	}
	if final == nil {
		t.Fatalf("%q got no answer at all", ask)
	}
	if len(final.ToolCalls) > 0 {
		return true, ""
	}
	return false, strings.TrimSpace(final.Text)
}
