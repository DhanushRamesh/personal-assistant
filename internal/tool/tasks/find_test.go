package tasks_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/tasks"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
	taskstool "github.com/DhanushRamesh/personal-assistant/internal/tool/tasks"
)

// finding : the task_find tool over a set of lists.
func finding(t *testing.T, l *lists) tool.Tool {
	t.Helper()
	for _, x := range taskstool.All(l, taskstool.Clock{}) {
		if x.Name == "task_find" {
			return x
		}
	}
	t.Fatal("task_find is not among the tools")
	return tool.Tool{}
}

func found(t *testing.T, l *lists, name string) tool.Result {
	t.Helper()
	return finding(t, l).Run(context.Background(), tool.Invocation{
		Caller: tool.Caller{Channel: chat.ChannelVoice, UserID: "usr_1"},
		Args:   json.RawMessage(`{"name":` + strconv(name) + `}`),
	})
}

func strconv(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// twoLists : two lists with a task each.
func twoLists() *lists {
	return &lists{
		held: []tasks.List{{ID: "l1", Title: "Jarvis Improvement"}, {ID: "l2", Title: "My Tasks"}},
		on: map[string][]tasks.Task{
			"l1": {{ID: "t1", Title: "Inference time improvement", List: "Jarvis Improvement"}},
			"l2": {{ID: "t2", Title: "Buy a cutting board", List: "My Tasks"}},
		},
	}
}

// TestItSaysWhichListATaskIsOn : reading one list cannot answer this,
// which is why the assistant said it had no tool for it.
func TestItSaysWhichListATaskIsOn(t *testing.T) {
	got := found(t, twoLists(), "Buy a cutting board")
	if !strings.Contains(got.Content, "My Tasks") {
		t.Errorf("did not say which list it is on: %q", got.Content)
	}
}

// TestANameHeardBadlyStillFindsIt : the name arrives through speech.
//
// "Jarvis improvements" came back from the transcriber as "jars and
// groovements" in a real conversation. A search that insisted on the
// whole string would report the task does not exist.
func TestANameHeardBadlyStillFindsIt(t *testing.T) {
	got := found(t, twoLists(), "inference improvement")
	if !strings.Contains(got.Content, "Inference time improvement") {
		t.Errorf("a near name found nothing: %q", got.Content)
	}
}

// Nothing like it anywhere is a definite answer, and the tool says to
// offer adding it rather than listing what does exist.
func TestNothingLikeItSaysSoAndOffers(t *testing.T) {
	got := found(t, twoLists(), "no tool info")
	if !strings.Contains(got.Content, "No task on any list") {
		t.Errorf("did not answer plainly: %q", got.Content)
	}
	if !strings.Contains(got.Content, "offer to add it") {
		t.Errorf("did not steer towards offering: %q", got.Content)
	}
	if strings.Contains(got.Content, "Buy a cutting board") {
		t.Errorf("recited what does exist instead of answering: %q", got.Content)
	}
}

// TestAListThatCouldNotBeReadIsAdmitted : saying "it is nowhere"
// about lists that were never read is a claim the search cannot make.
func TestAListThatCouldNotBeReadIsAdmitted(t *testing.T) {
	l := twoLists()
	l.missed = []string{"Geyser Guide"}
	got := found(t, l, "no tool info")
	if !strings.Contains(got.Content, "Geyser Guide") {
		t.Errorf("did not admit a list it could not read: %q", got.Content)
	}
}
