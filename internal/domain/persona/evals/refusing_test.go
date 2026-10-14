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

// How long a refusal runs.
//
// Owner, shown four-sentence refusals in the desktop client: "jarvis
// is talking too much..its not his personality..just talk few words."
//
// What he was sent, twice over: "I am afraid I cannot do that, sir. I
// have no way to retrieve or list my earlier answers from this
// conversation. What I can do is answer your questions going forward."
//
// Three instructions asked for that trailing offer -- the manner's
// declining block, its honesty block, and the prompt that sends an
// answer back for naming its machinery. A refusal is now one
// sentence, in both readings: the written manner lifts the sentence
// limit for answers worth the room, and a refusal is not one.

// refused : Things he asked that it cannot do.
var declined = []string{
	"turn over your last 51 real answers",
	"open a terminal and run cloud code",
	"what is the weather tomorrow",
	"play some music on the kitchen speaker",
}

// offering : The shape he does not want a refusal to take.
var offering = []string{"what i can do", "i can help", "what i can offer",
	"instead, i can", "i am able to"}

func TestARefusalIsOneSentence(t *testing.T) {
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

	// Written, because that is where he saw it and where the
	// sentence limit is otherwise lifted.
	system := persona.Prompt(cfg.Assistant.Persona, cfg.Assistant.Name, persona.Written) +
		" " + conversation.Now(time.Now())

	var long, offered int
	for _, ask := range declined {
		answer := strings.TrimSpace(say(t, env, system, ask))
		n := sentences(answer)
		low := strings.ToLower(answer)

		var trailing string
		for _, o := range offering {
			if strings.Contains(low, o) {
				trailing = o
				break
			}
		}
		if n > 2 {
			long++
		}
		if trailing != "" {
			offered++
		}
		t.Logf("  %-42q %d sentences%s\n      %s", ask, n,
			map[bool]string{true: "  (offered: " + trailing + ")"}[trailing != ""], answer)
	}
	t.Logf("refusals over two sentences: %d of %d; with a trailing offer: %d",
		long, len(declined), offered)
	if long > 1 || offered > 1 {
		t.Errorf("refusals are still long: %d over two sentences, %d offering", long, offered)
	}
}

// sentences : How many it runs to, counted the way a listener hears
// them.
//
// A stop followed by a space, rather than every stop. Counting every
// one made "four and a half hours" two sentences and "9 a.m." three,
// which is the sort of miscount that turns a brevity check into
// noise nobody reads.
func sentences(s string) int {
	n := 0
	for _, part := range sentenceEnd.Split(strings.TrimSpace(s), -1) {
		if strings.TrimSpace(part) != "" {
			n++
		}
	}
	return n
}

var sentenceEnd = regexp.MustCompile(`[.!?]+\s+`)
