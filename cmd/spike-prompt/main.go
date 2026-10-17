//go:build spike

// Command spike-prompt weighs what is sent on every turn.
//
// A chat call never carries less than 34,449 tokens, so most of what is
// paid for is the same text sent again. This says which text, by the
// character and by a rough token count, for the ear and for the screen.
//
// No API calls. Everything here is built from the same constructors the
// server uses, so what it measures is what is sent.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"context"
	"log/slog"

	"personal-assistant/internal/base/platform/config"
	"personal-assistant/internal/base/platform/storage"
	"personal-assistant/internal/base/records/chat"
	chatmysql "personal-assistant/internal/base/records/chat/mysql"
	"personal-assistant/internal/base/records/conversation"
	"personal-assistant/internal/base/thinking/embed"
	"personal-assistant/internal/base/thinking/environment"
	"personal-assistant/internal/domain/memory"
	"personal-assistant/internal/domain/memory/inmemory"
	"personal-assistant/internal/domain/persona"

	"personal-assistant/internal/app/tool"
	"personal-assistant/internal/app/tool/calendar"
	comingtool "personal-assistant/internal/app/tool/coming"
	"personal-assistant/internal/app/tool/conversations"
	"personal-assistant/internal/app/tool/events"
	"personal-assistant/internal/app/tool/mail"
	mapstool "personal-assistant/internal/app/tool/maps"
	"personal-assistant/internal/app/tool/memories"
	"personal-assistant/internal/app/tool/places"
	"personal-assistant/internal/app/tool/reminders"
	"personal-assistant/internal/app/tool/tasks"
	webtool "personal-assistant/internal/app/tool/web"
	eventmemory "personal-assistant/internal/base/world/event/inmemory"
	remindmemory "personal-assistant/internal/domain/remind/inmemory"
)

// perToken : Characters to a token, near enough to rank things by.
//
// Four is the usual rule for English. The point here is the ordering and
// the proportions, both of which hold whatever the exact divisor is.
const perToken = 4

func main() {
	recall := &memory.Recall{Store: inmemory.New(), Embedder: embed.Fake{}}
	registry, err := tool.NewRegistry(slices.Concat(
		conversations.All(nil),
		memories.All(recall),
		reminders.All(remindmemory.New(), reminders.Clock{Location: time.UTC}, nil),
		events.Tools(eventmemory.New(), events.Clock{Location: time.UTC}),
		calendar.All(nil, calendar.Clock{Location: time.UTC}),
		tasks.All(nil, tasks.Clock{Location: time.UTC}),
		mail.All(nil, mail.Clock{Location: time.UTC}),
		places.Tools(nil, places.Clock{Location: time.UTC}),
		comingtool.Tools(nil, comingtool.Clock{Location: time.UTC}),
		webtool.Tools(nil),
		mapstool.Tools(nil, eventmemory.New(), mapstool.Clock{Location: time.UTC}),
	)...)
	if err != nil {
		fmt.Println("registry:", err)
		os.Exit(1)
	}

	if len(os.Args) > 1 {
		weighReal(os.Args[1])
		return
	}

	for _, where := range []struct {
		name    string
		channel chat.Channel
		how     persona.Reading
	}{
		{"on the screen", chat.ChannelDirect, persona.Written},
		{"out loud", chat.ChannelVoice, persona.Spoken},
	} {
		fmt.Printf("\n%s\n%s\n", where.name, strings.Repeat("-", len(where.name)))

		manner := persona.Prompt("jarvis", "Jarvis", where.how)
		described, deferred := split(registry, where.channel)
		wire := weigh(described)
		names := namesOf(deferred)

		total := len(manner) + wire + len(names)
		row := func(what string, n int, extra string) {
			fmt.Printf("  %-26s %7d chars  ~%6d tokens  %3d%%  %s\n",
				what, n, n/perToken, 100*n/total, extra)
		}
		row("the manner and its rules", len(manner), "")
		row("tool definitions", wire, fmt.Sprintf("%d described", len(described)))
		row("the names of the rest", len(names), fmt.Sprintf("%d deferred", len(deferred)))
		fmt.Printf("  %-26s %7d chars  ~%6d tokens\n", "", total, total/perToken)

		fmt.Println("\n  the ten heaviest tools:")
		for _, t := range heaviest(described, 10) {
			fmt.Printf("    %-22s %6d chars  ~%5d tokens\n", t.Name, size(t), size(t)/perToken)
		}
	}
}

// weighReal : One real conversation's history, as it is actually sent.
func weighReal(conversationID string) {
	cfg, err := config.Load("config.ini", os.LookupEnv)
	if err != nil {
		fmt.Println("config:", err)
		os.Exit(1)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := storage.Open(ctx, cfg.Database, logger, storage.Options{})
	if err != nil {
		fmt.Println("database:", err)
		os.Exit(1)
	}
	defer db.Close()

	said, err := chatmysql.NewRepository(db).Before(ctx, conversationID, 1<<30)
	if err != nil {
		fmt.Println("reading the conversation:", err)
		os.Exit(1)
	}
	fmt.Printf("\n%s\n%s\n", conversationID, strings.Repeat("-", len(conversationID)))
	weighWindow(said, conversation.DefaultBudget)
}

// split : The tools described in full, and the ones named only.
func split(r *tool.Registry, c chat.Channel) ([]environment.ToolSpec, []tool.Tool) {
	address := persona.AddressFor("jarvis")
	var out []environment.ToolSpec
	for _, x := range r.Offered(c, nil) {
		schema, err := tool.Narrated(x.Params, address).MarshalJSON()
		if err != nil {
			continue
		}
		out = append(out, environment.ToolSpec{
			Name:        x.Name,
			Description: tool.NarratedDescription(x.Description(), address),
			Parameters:  schema,
		})
	}
	return out, r.Deferred(c, nil)
}

func size(t environment.ToolSpec) int {
	b, _ := json.Marshal(t)
	return len(b)
}

func weigh(all []environment.ToolSpec) int {
	n := 0
	for _, t := range all {
		n += size(t)
	}
	return n
}

func namesOf(all []tool.Tool) string {
	names := make([]string, 0, len(all))
	for _, t := range all {
		names = append(names, t.Name)
	}
	return strings.Join(names, ", ")
}

func heaviest(all []environment.ToolSpec, n int) []environment.ToolSpec {
	out := slices.Clone(all)
	slices.SortFunc(out, func(a, b environment.ToolSpec) int { return size(b) - size(a) })
	if len(out) > n {
		out = out[:n]
	}
	return out
}
