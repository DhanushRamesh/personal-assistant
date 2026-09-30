package tool_test

import (
	"sort"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
	calendartool "github.com/DhanushRamesh/personal-assistant/internal/tool/calendar"
	"github.com/DhanushRamesh/personal-assistant/internal/tool/conversations"
	"github.com/DhanushRamesh/personal-assistant/internal/tool/memories"
	"github.com/DhanushRamesh/personal-assistant/internal/tool/reminders"
	taskstool "github.com/DhanushRamesh/personal-assistant/internal/tool/tasks"
)

// TestOnlyMemoryIsReadBeforeTheQuestion : Memory is fetched before the
// model is asked; nothing else is.
//
// Named rather than counted, so that a flag appearing or disappearing
// fails here. It has gone both ways in one day: three listings lost
// Prefetch in a rewrite on 28 September 2026 and nothing failed, and
// restoring all three was then rejected by the owner -- the diary,
// the reminders and the conversations are read when a turn is about
// them, and paying for all three on every turn is tokens spent on
// turns that are about none of them.
//
// Memory is the only exception, because it is about the person rather
// than about a domain: there is no question it is irrelevant to.
//
// task_lists was prefetched for a few minutes on 30 September, after
// the assistant denied a list existed without reading them, and the
// owner took it out again: the rule is that a domain is read when it
// is talked about, and prefetching is not how that is arranged. The
// naming stays in the prompt instead.
//
// This test could not see the tasks domain at all until that day: it
// named an expected set while building a registry that left tasks out,
// so a flag appearing there failed nothing. All five are built now.
func TestOnlyMemoryIsReadBeforeTheQuestion(t *testing.T) {
	where := time.UTC
	registry, err := tool.NewRegistry(concat(
		conversations.All(nil),
		memories.All(nil),
		reminders.All(nil, reminders.Clock{Now: time.Now, Location: where}),
		calendartool.All(nil, calendartool.Clock{Now: time.Now, Location: where}),
		taskstool.All(nil, taskstool.Clock{Now: time.Now, Location: where}),
	)...)
	if err != nil {
		t.Fatalf("the registry would not build: %v", err)
	}

	var got []string
	for _, x := range registry.Prefetched(chat.ChannelDirect) {
		got = append(got, x.Name)
	}
	sort.Strings(got)

	want := []string{"memory_list"}
	if len(got) != len(want) {
		t.Fatalf("prefetched = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("prefetched = %v, want %v", got, want)
			break
		}
	}
}

func concat(lists ...[]tool.Tool) []tool.Tool {
	var out []tool.Tool
	for _, l := range lists {
		out = append(out, l...)
	}
	return out
}
