//go:build evals

package evals

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/app/tool"
	"github.com/DhanushRamesh/personal-assistant/internal/app/tool/calendar"
	"github.com/DhanushRamesh/personal-assistant/internal/app/tool/conversations"
	"github.com/DhanushRamesh/personal-assistant/internal/app/tool/events"
	"github.com/DhanushRamesh/personal-assistant/internal/app/tool/mail"
	"github.com/DhanushRamesh/personal-assistant/internal/app/tool/memories"
	"github.com/DhanushRamesh/personal-assistant/internal/app/tool/places"
	"github.com/DhanushRamesh/personal-assistant/internal/app/tool/reminders"
	"github.com/DhanushRamesh/personal-assistant/internal/app/tool/tasks"
	"github.com/DhanushRamesh/personal-assistant/internal/base/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/base/embed"
	eventmemory "github.com/DhanushRamesh/personal-assistant/internal/base/event/inmemory"
	"github.com/DhanushRamesh/personal-assistant/internal/domain/memory"
	"github.com/DhanushRamesh/personal-assistant/internal/domain/memory/inmemory"
	"github.com/DhanushRamesh/personal-assistant/internal/domain/persona"
	remindmemory "github.com/DhanushRamesh/personal-assistant/internal/domain/remind/inmemory"
)

// TestWhatThePromptWeighs : Not an assertion, a measurement. Prints what
// each part of a turn's request costs, so that making one smaller is
// aimed at the part that is actually big.
func TestWhatThePromptWeighs(t *testing.T) {
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
	)...)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	// Without it the registry defers nothing and offers everything,
	// which is not what a request carries. The server adds it the
	// same way.
	if err := registry.Add(tool.Describing(registry)); err != nil {
		t.Fatalf("adding tool_describe: %v", err)
	}

	address := persona.AddressFor("")
	// What is actually sent, not every tool the channel has. Most
	// are named in the catalogue and described only when asked for.
	offered := registry.Offered(chat.ChannelVoice, nil)
	var schema, describe, biggest int
	var worst string
	type weighed struct {
		name string
		size int
	}
	all := make([]weighed, 0, len(offered))
	for _, x := range offered {
		params, err := tool.Narrated(x.Params, address).MarshalJSON()
		if err != nil {
			t.Fatalf("%s: %v", x.Name, err)
		}
		desc := tool.NarratedDescription(x.Description(), address)
		size := len(params) + len(desc) + len(x.Name)
		schema += size
		describe += len(desc)
		all = append(all, weighed{x.Name, size})
		if size > biggest {
			biggest, worst = size, x.Name
		}
	}
	slices.SortFunc(all, func(a, b weighed) int { return b.size - a.size })
	system := persona.Prompt("", "Jarvis")

	fmt.Printf("\n  tools offered        %d\n", len(offered))
	fmt.Printf("  all tool specs       %d bytes  (~%d tokens)\n", schema, schema/4)
	fmt.Printf("    of which prose     %d bytes\n", describe)
	fmt.Printf("  biggest single tool  %s, %d bytes (~%d tokens)\n", worst, biggest, biggest/4)
	fmt.Printf("  system prompt        %d bytes  (~%d tokens)\n", len(system), len(system)/4)
	// The one argument every tool carries, described in full on each
	// of them.
	saying, _ := tool.Narrated(tool.Schema{Properties: map[string]tool.Property{}},
		address).MarshalJSON()
	fmt.Printf("  the saying argument  %d bytes each, x%d = %d bytes (~%d tokens)\n",
		len(saying), len(offered), len(saying)*len(offered),
		len(saying)*len(offered)/4)

	catalogue := registry.Catalogue(chat.ChannelVoice, nil)
	fmt.Printf("  deferred catalogue   %d bytes  (~%d tokens)\n",
		len(catalogue), len(catalogue)/4)
	fmt.Printf("  everything above     ~%d tokens\n",
		(schema+len(system)+len(catalogue))/4)
	fmt.Printf("\n  every tool, heaviest first:\n")
	for _, w := range all {
		fmt.Printf("    %-24s %5d\n", w.name, w.size)
	}
	fmt.Println()
}
