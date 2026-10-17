package knowing_test

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"personal-assistant/internal/app/knowing"
	"personal-assistant/internal/base/thinking/embed"
	"personal-assistant/internal/domain/knowledge"
	"personal-assistant/internal/domain/knowledge/inmemory"
	"personal-assistant/internal/domain/memory"
)

// Their description, by section, and every memory are kept and found by
// meaning; a memory no longer kept is forgotten.
func TestTheirDescriptionAndMemoriesAreKeptAndFound(t *testing.T) {
	store := inmemory.New()
	mems := []memory.Memory{{ID: "m1", Body: "Commits are pushed only when they say."}, {ID: "m2", Body: "Their cousin is Vishnu."}}
	k := &knowing.Knowing{
		Known:    &knowledge.Known{Store: store, Embed: embed.Fake{}},
		Profile:  func(context.Context, string) (string, error) { return "", nil },
		Memories: func(context.Context, string) ([]memory.Memory, error) { return mems, nil },
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	got := k.About(context.Background(), "u", "Commits are pushed only when they say.")
	if !strings.HasPrefix(got, "[memory: m1]\nCommits are pushed only when they say.") {
		t.Errorf("found %q", got)
	}
	mems = mems[1:]
	k.Sync(context.Background(), "u")
	if refs, _ := store.Refs(context.Background(), "u", knowledge.Memory); strings.Join(refs, ",") != "m2" {
		t.Errorf("kept %v after m1 was forgotten", refs)
	}
}
