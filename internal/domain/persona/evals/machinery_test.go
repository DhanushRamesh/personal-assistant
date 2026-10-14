//go:build evals

package evals

import (
	"io"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/base/platform/config"
	"personal-assistant/internal/base/records/conversation"
	"personal-assistant/internal/base/thinking/environment/platformai"
	"personal-assistant/internal/domain/persona"
)

// Does the inside of its head stay inside its head?
//
// The persona has forbidden this since it was written -- say nothing
// at all about tools, never recite what you cannot do, never describe
// the tools you have, nobody asked for an inventory -- and the owner
// was told "You can rely on me to use the tools I have", and then, to
// "Okay, there it is. Thank you.", this: "I have read the lists. Your
// message contains no question or request that requires a tool, so no
// tool needs calling."
//
// Owner: "why it says tool tool tool. Is this how a companion talks?"
//
// So the rule became a banned word, and a banned word can be checked
// by looking rather than by judging, which is the whole reason this
// eval is cheap enough to run on every change to the manner.

// inside : What belongs in its head and not in his ears.
var inside = regexp.MustCompile(`(?i)\btools?\b|\btool[_ ]call|\bfunction call|\bno tool needs|\bread the lists\b`)

// triage : Saying what kind of question it was asked.
//
// The same leak by another route. Asked how to think about backups it
// opened: "This is a question about how to think about something --
// backups, in this case -- not a request to do anything with your own
// backups or devices. It is a question about the world, and I can
// answer it." A paragraph explaining why it is allowed to reply, in
// front of the reply, and the word "tool" never appears in it.
//
// Deciding what sort of question it is is how it chooses what to do.
// It is not part of the answer, and saying it aloud is the inside of
// its head again.
var triage = regexp.MustCompile(`(?i)this is (a|not a) question|that is (a|not a) question|is a question about|not a request to|question about the world|rather than a request|so I can answer`)

// prodding : Questions that invite it to describe its own machinery.
// The last two are the ones that actually went wrong.
var prodding = []string{
	// The two that invite it to explain why it may answer at all:
	// general knowledge, with nothing of his own in them. Both are
	// real questions he asked, and both were answered with a
	// paragraph about what sort of question they were.
	//
	// The second is reworded. He asked it about WebMCP, and the
	// answer failed this eval on the words "function call" -- which
	// were the subject rather than a leak. A banned word that is
	// also the topic is a false alarm, and an eval that cries wolf
	// on a correct answer gets ignored. The collision is real and
	// is written up; the question here is the same shape without it.
	"how should I think about backups",
	"how do I structure a Vue 3 web application",
	"what can you do for me",
	"can you book me a flight to Delhi next Tuesday",
	"what are you able to control in the house",
	"how do you work",
	"what message did you say before",
	"Okay, there it is. Thank you.",
	"thanks, that is all",
	"what is 17 times 23",
}

func TestItNeverNamesItsOwnMachinery(t *testing.T) {
	cfg, err := config.Load("../../../../config.ini", os.LookupEnv)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if cfg.Provider.Name != config.ProviderPlatformAI {
		t.Skip("no real environment configured")
	}

	env, err := platformai.New(platformai.Config{
		ClientID: cfg.PlatformAI.ClientID, ClientSecret: cfg.PlatformAI.ClientSecret,
		RefreshToken: cfg.PlatformAI.RefreshToken, PortalID: cfg.PlatformAI.PortalID,
		TokenURL: cfg.PlatformAI.TokenURL, ChatURL: cfg.PlatformAI.ChatURL,
		Scope: cfg.PlatformAI.Scope, RedirectURI: cfg.PlatformAI.RedirectURI,
		Vendor: cfg.PlatformAI.Vendor, Model: cfg.PlatformAI.Model,
		Timeout: 90 * time.Second,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("platformai.New: %v", err)
	}

	for _, how := range []struct {
		name string
		read persona.Reading
	}{{"spoken", persona.Spoken}, {"written", persona.Written}} {
		system := persona.Prompt(cfg.Assistant.Persona, cfg.Assistant.Name, how.read) +
			" " + conversation.Now(time.Now())

		var leaked int
		for _, ask := range prodding {
			answer := say(t, env, system, ask)
			if word := inside.FindString(answer); word != "" {
				leaked++
				t.Errorf("%s: %-42q said %q:\n    %s", how.name, ask, word, trim(answer))
				continue
			}
			if said := triage.FindString(answer); said != "" {
				leaked++
				t.Errorf("%s: %-42q classified the question out loud (%q):\n    %s",
					how.name, ask, said, trim(answer))
				continue
			}
			// A pleasantry is answered, not accounted for.
			if closing(ask) && len(strings.Fields(answer)) > 25 {
				leaked++
				t.Errorf("%s: %-42q got %d words for a pleasantry:\n    %s",
					how.name, ask, len(strings.Fields(answer)), trim(answer))
			}
		}
		t.Logf("%-8s %d of %d answers kept the machinery out of it",
			how.name, len(prodding)-leaked, len(prodding))
	}
}

func closing(ask string) bool {
	a := strings.ToLower(ask)
	return strings.Contains(a, "thank") || strings.Contains(a, "that is all")
}

func trim(s string) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}
