//go:build evals

package evals

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/embed"
	eventmemory "github.com/DhanushRamesh/personal-assistant/internal/event/inmemory"
	"github.com/DhanushRamesh/personal-assistant/internal/memory"
	"github.com/DhanushRamesh/personal-assistant/internal/memory/inmemory"
	"github.com/DhanushRamesh/personal-assistant/internal/persona"
	remindmemory "github.com/DhanushRamesh/personal-assistant/internal/remind/inmemory"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
	"github.com/DhanushRamesh/personal-assistant/internal/tool/calendar"
	"github.com/DhanushRamesh/personal-assistant/internal/tool/conversations"
	"github.com/DhanushRamesh/personal-assistant/internal/tool/events"
	"github.com/DhanushRamesh/personal-assistant/internal/tool/mail"
	"github.com/DhanushRamesh/personal-assistant/internal/tool/memories"
	"github.com/DhanushRamesh/personal-assistant/internal/tool/places"
	"github.com/DhanushRamesh/personal-assistant/internal/tool/reminders"
	"github.com/DhanushRamesh/personal-assistant/internal/tool/tasks"
)

// TestWhatThePromptWeighs : Not an assertion, a measurement. Prints what
// each part of a turn's request costs, so that making one smaller is
// aimed at the part that is actually big.
func TestWhatThePromptWeighs(t *testing.T) {
	recall := &memory.Recall{Store: inmemory.New(), Embedder: embed.Fake{}}
	registry, err := tool.NewRegistry(slices.Concat(
		conversations.All(nil),
		memories.All(recall),
		reminders.All(remindmemory.New(), reminders.Clock{Location: time.UTC}),
		events.Tools(eventmemory.New(), events.Clock{Location: time.UTC}),
		calendar.All(nil, calendar.Clock{Location: time.UTC}),
		tasks.All(nil, tasks.Clock{Location: time.UTC}),
		mail.All(nil, mail.Clock{Location: time.UTC}),
		places.Tools(nil, places.Clock{Location: time.UTC}),
	)...)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	address := persona.AddressFor("")
	offered := registry.For(chat.ChannelVoice)
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
	fmt.Printf("  everything above     ~%d tokens\n", (schema+len(system))/4)
	fmt.Printf("\n  the ten heaviest tools:\n")
	for _, w := range all[:10] {
		fmt.Printf("    %-22s %5d bytes (~%d tokens)\n", w.name, w.size, w.size/4)
	}
	fmt.Println()
}
