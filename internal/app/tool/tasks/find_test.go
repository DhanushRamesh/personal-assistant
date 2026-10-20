package tasks_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"personal-assistant/internal/app/tool"
	taskstool "personal-assistant/internal/app/tool/tasks"
	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/domain/tasks"
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

// secureAccess : a list holding several tasks, named as he named it.
func secureAccess() *lists {
	return &lists{
		held: []tasks.List{{ID: "l1", Title: "Secure access"}, {ID: "l2", Title: "My Tasks"}},
		on: map[string][]tasks.Task{
			"l1": {
				{ID: "t1", Title: "Show enrolment options in Ulaa About page", List: "Secure access"},
				{ID: "t2", Title: "Provisional access close button when gateway is not running", List: "Secure access"},
				{ID: "t3", Title: "Do a health call when provisional access is set", List: "Secure access"},
			},
			"l2": {{ID: "t4", Title: "Buy a cutting board", List: "My Tasks"}},
		},
	}
}

// TestANameMayBeAListsRatherThanATasks : what he asked for on 20
// October 2026, and what he was told.
//
// *"whara re the tasks for secure accesss"*. This searched task titles
// alone, found none called that, and offered the nearest single task --
// which happened to be on the list of that name. He was told the list
// had one task, and was told the wrong one. It has three here and
// several in his account.
func TestANameMayBeAListsRatherThanATasks(t *testing.T) {
	got := found(t, secureAccess(), "secure accesss")

	if strings.Contains(got.Content, "Nothing is called exactly") {
		t.Fatalf("a list name was searched as a task name: %q", got.Content)
	}
	if !strings.Contains(got.Content, "is a list") {
		t.Errorf("did not say it is a list: %q", got.Content)
	}
	if !strings.Contains(got.Content, "3 tasks") {
		t.Errorf("did not say how many are on it: %q", got.Content)
	}
	for _, want := range []string{
		"Show enrolment options in Ulaa About page",
		"Provisional access close button when gateway is not running",
		"Do a health call when provisional access is set",
	} {
		if !strings.Contains(got.Content, want) {
			t.Errorf("left out %q: %s", want, got.Content)
		}
	}
	// Nothing from the other list.
	if strings.Contains(got.Content, "cutting board") {
		t.Errorf("a task from another list came back: %q", got.Content)
	}
}

// TestATaskStillWinsOverAListItIsNotOn : a name that is a task's is
// still looked for among tasks.
func TestATaskStillWinsOverAListItIsNotOn(t *testing.T) {
	got := found(t, secureAccess(), "Buy a cutting board")
	if strings.Contains(got.Content, "is a list") {
		t.Errorf("a task name was taken for a list: %q", got.Content)
	}
	if !strings.Contains(got.Content, "My Tasks") {
		t.Errorf("did not say which list it is on: %q", got.Content)
	}
}

// TestANameFoundNowhereIsLookedForElsewhere : a word that is not a
// task may be a reminder, a conversation or a rule.
//
// Every other tool that fails to find a name marks what it looked for,
// which has the registry search the other domains and append what it
// finds. task_find never did, so a name that lived in another domain
// came back as simply missing.
func TestANameFoundNowhereIsLookedForElsewhere(t *testing.T) {
	got := found(t, twoLists(), "ring the dentist")
	if got.Looking == nil {
		t.Fatalf("a name found nowhere was not marked as looked for: %q", got.Content)
	}
	if got.Looking.Kind != "task" || got.Looking.For != "ring the dentist" {
		t.Errorf("marked as %+v", got.Looking)
	}

	// A name that was found is not looked for anywhere else.
	if other := found(t, twoLists(), "Buy a cutting board"); other.Looking != nil {
		t.Errorf("a name that was found was still looked for elsewhere: %+v", other.Looking)
	}
	// Nor is a list's name, which is an answer.
	if list := found(t, secureAccess(), "secure accesss"); list.Looking != nil {
		t.Errorf("a list that was found was still looked for elsewhere: %+v", list.Looking)
	}
}
