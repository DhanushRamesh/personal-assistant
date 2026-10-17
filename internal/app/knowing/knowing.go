// Package knowing : What is known of the person kept current and found by
// what bears on a moment -- the server's own sources, their standing
// description and their memories, put into the knowledge store, and the
// nearest of everything kept found for a question (see
// internal/domain/knowledge).
package knowing

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"personal-assistant/internal/app/profile"
	"personal-assistant/internal/domain/knowledge"
	"personal-assistant/internal/domain/memory"
)

// Every : How often the server's own sources are put in the store again.
// Unchanged text is not embedded again, so a look costs a read.
const Every = 15 * time.Minute

// Found : How many pieces a question is given at most, and how alike to
// it each must be.
const (
	Found = 8
	Floor = 0.55
)

// Knowing : Keeps the server's sources in the store, and finds.
type Knowing struct {
	Known *knowledge.Known
	// Profile : Their standing description.
	Profile func(ctx context.Context, userID string) (string, error)
	// Memories : Everything they asked to be remembered.
	Memories func(ctx context.Context, userID string) ([]memory.Memory, error)
	Logger   *slog.Logger
	Now      func() time.Time

	mu   sync.Mutex
	last map[string]time.Time
}

func (k *Knowing) now() time.Time {
	if k.Now != nil {
		return k.Now()
	}
	return time.Now()
}

// About : What is known of them that bears on a question, as the model is
// given it; empty when nothing does or nothing can be found. Their own
// sources are brought up to date first -- at once the first time, in the
// background after.
func (k *Knowing) About(ctx context.Context, userID, question string) string {
	if k == nil || k.Known == nil || userID == "" {
		return ""
	}
	k.mu.Lock()
	if k.last == nil {
		k.last = map[string]time.Time{}
	}
	at, synced := k.last[userID]
	due := !synced || k.now().Sub(at) >= Every
	if due {
		k.last[userID] = k.now()
	}
	k.mu.Unlock()
	switch {
	case !synced:
		k.Sync(ctx, userID)
	case due:
		go k.Sync(context.WithoutCancel(ctx), userID)
	}

	found, err := k.Known.Find(ctx, userID, question, Found, Floor)
	if err != nil {
		k.Logger.WarnContext(ctx, "knowing: cannot find what bears on this", slog.Any("error", err))
		return ""
	}
	return knowledge.Told(found)
}

// Sync : Puts their description, by its sections, and every memory in the
// store; forgets the memories no longer kept.
func (k *Knowing) Sync(ctx context.Context, userID string) {
	embedded := 0
	if k.Profile != nil {
		described, err := k.Profile(ctx, userID)
		if err != nil {
			k.Logger.WarnContext(ctx, "knowing: cannot read their description", slog.Any("error", err))
		} else {
			parts := profile.Split(described)
			for _, part := range profile.Parts {
				if did, err := k.Known.Keep(ctx, userID, knowledge.Profile, string(part), parts[part]); err != nil {
					k.Logger.WarnContext(ctx, "knowing: cannot keep their description", slog.Any("error", err))
				} else if did {
					embedded++
				}
			}
		}
	}
	if k.Memories != nil {
		all, err := k.Memories(ctx, userID)
		if err != nil {
			k.Logger.WarnContext(ctx, "knowing: cannot read their memories", slog.Any("error", err))
		} else {
			keep := map[string]bool{}
			for i := range all {
				keep[all[i].ID] = true
				if did, err := k.Known.Keep(ctx, userID, knowledge.Memory, all[i].ID, strings.TrimSpace(all[i].Text())); err != nil {
					k.Logger.WarnContext(ctx, "knowing: cannot keep a memory", slog.Any("error", err))
				} else if did {
					embedded++
				}
			}
			if refs, err := k.Known.Store.Refs(ctx, userID, knowledge.Memory); err == nil {
				for _, ref := range refs {
					if !keep[ref] {
						_ = k.Known.Store.Remove(ctx, userID, knowledge.Memory, ref)
					}
				}
			}
		}
	}
	if embedded > 0 {
		k.Logger.InfoContext(ctx, "knowing: kept what is known of them", slog.Int("embedded", embedded))
	}
}
