//go:build evals

package evals

import (
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"personal-assistant/internal/base/platform/config"
	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/base/thinking/environment/platformai"
)

// keepable : Plain statements of fact, each worth keeping and none of
// them asking for anything.
//
// He said the first of these on 21 October 2026 and was told "I am
// afraid I cannot do that, sir." Measured afterwards, memory_remember
// was nowhere in the top eight of eighty-eight for it: an address
// reads as a place, and the tool's own words are about when to decide
// to keep something rather than about the sentences that need
// keeping. It was never described, so the model looked at what it had,
// found nothing, and said so.
//
// Now it is offered whatever the ranking says. This is whether that
// was enough.
var keepable = []string{
	"54.197.7.79 is my gateway ip in ULaa US DC",
	"remember that 54.197.7.79 is my gateway ip",
	"my wife's birthday is on the 3rd of March",
	"I prefer the window seat on flights",
	"the office wifi password changes every quarter",
}

// TestAFactWorthKeepingIsKept : it calls the tool rather than saying
// it cannot.
func TestAFactWorthKeepingIsKept(t *testing.T) {
	registry := everything(t)

	cfg, err := config.Load("../../../../config.ini", os.LookupEnv)
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
		Vendor: cfg.PlatformAI.Vendor, Model: cfg.PlatformAI.Model,
		Timeout: 90 * time.Second,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("platformai.New: %v", err)
	}

	pick := newChooser(registry.For(chat.ChannelDirect))
	var kept int
	for _, say := range keepable {
		called, _, said := ask(t, env, registry, chat.ChannelDirect, say, "", pick)
		switch {
		case called == "memory_remember":
			kept++
			t.Logf("kept      %q", say)
		case called != "":
			t.Logf("reached %-18s %q", called, say)
		default:
			t.Logf("said nothing to do: %-28q -> %q", say, first(said, 70))
		}
	}
	t.Logf("kept %d of %d", kept, len(keepable))
	if kept == 0 {
		t.Error("nothing worth keeping was kept, though the tool is now always offered")
	}
}
