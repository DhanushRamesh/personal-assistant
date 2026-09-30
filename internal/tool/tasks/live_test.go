package tasks_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/tasks"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
	taskstool "github.com/DhanushRamesh/personal-assistant/internal/tool/tasks"
)

func india() *time.Location { return time.FixedZone("IST", 5*3600+1800) }

// lists : A stand-in for Google Tasks.
type lists struct {
	held   []tasks.List
	on     map[string][]tasks.Task
	refuse error
}

func (l *lists) All(context.Context, string) ([]tasks.List, error) {
	return append([]tasks.List(nil), l.held...), l.refuse
}

func (l *lists) On(_ context.Context, _ string, which tasks.List) ([]tasks.Task, error) {
	if l.refuse != nil {
		return nil, l.refuse
	}
	return append([]tasks.Task(nil), l.on[which.ID]...), nil
}

// Add : Actually stores it, so the read-back the tool does tests
// something. A double that accepts a write and keeps nothing makes
// every verification look like a failure.
func (l *lists) Add(_ context.Context, _ string, which tasks.List, t tasks.Task) (*tasks.Task, error) {
	if l.refuse != nil {
		return nil, l.refuse
	}
	t.ID = "tsk_" + t.Title
	t.List = which.Title
	l.on[which.ID] = append(l.on[which.ID], t)
	return &t, nil
}

func (l *lists) Tick(_ context.Context, _ string, which tasks.List, id string, done bool) (*tasks.Task, error) {
	if l.refuse != nil {
		return nil, l.refuse
	}
	for i := range l.on[which.ID] {
		if l.on[which.ID][i].ID != id {
			continue
		}
		l.on[which.ID][i].Done = done
		out := l.on[which.ID][i]
		return &out, nil
	}
	return nil, nil
}

// Make : Actually stores it, so the read-back the tool does tests
// something.
func (l *lists) Make(_ context.Context, _ string, title string) (*tasks.List, error) {
	if l.refuse != nil {
		return nil, l.refuse
	}
	made := tasks.List{ID: "l_" + title, Title: title}
	l.held = append(l.held, made)
	return &made, nil
}

// Rename : Actually changes it, so the read-back tests something.
func (l *lists) Rename(_ context.Context, _ string, which tasks.List, title string) (*tasks.List, error) {
	if l.refuse != nil {
		return nil, l.refuse
	}
	for i := range l.held {
		if l.held[i].ID != which.ID {
			continue
		}
		l.held[i].Title = title
		out := l.held[i]
		return &out, nil
	}
	return nil, nil
}

func (l *lists) Remove(_ context.Context, _ string, which tasks.List, id string) error {
	if l.refuse != nil {
		return l.refuse
	}
	kept := l.on[which.ID][:0]
	for _, t := range l.on[which.ID] {
		if t.ID != id {
			kept = append(kept, t)
		}
	}
	l.on[which.ID] = kept
	return nil
}

func (l *lists) RemoveList(_ context.Context, _ string, which tasks.List) error {
	if l.refuse != nil {
		return l.refuse
	}
	kept := l.held[:0]
	for _, x := range l.held {
		if x.ID != which.ID {
			kept = append(kept, x)
		}
	}
	l.held = kept
	delete(l.on, which.ID)
	return nil
}

// Clear : Hides the finished ones, as Google does -- they leave the
// ordinary read rather than being destroyed.
func (l *lists) Clear(_ context.Context, _ string, which tasks.List) error {
	if l.refuse != nil {
		return l.refuse
	}
	kept := l.on[which.ID][:0]
	for _, t := range l.on[which.ID] {
		if !t.Done {
			kept = append(kept, t)
		}
	}
	l.on[which.ID] = kept
	return nil
}

func (l *lists) Move(_ context.Context, _ string, from tasks.List, id string, to tasks.List) (*tasks.Task, error) {
	if l.refuse != nil {
		return nil, l.refuse
	}
	for i, t := range l.on[from.ID] {
		if t.ID != id {
			continue
		}
		l.on[from.ID] = append(l.on[from.ID][:i], l.on[from.ID][i+1:]...)
		t.List = to.Title
		l.on[to.ID] = append(l.on[to.ID], t)
		return &t, nil
	}
	return nil, nil
}

func (l *lists) One(_ context.Context, _ string, which tasks.List, id string) (*tasks.Task, error) {
	if l.refuse != nil {
		return nil, l.refuse
	}
	for i := range l.on[which.ID] {
		if l.on[which.ID][i].ID == id {
			out := l.on[which.ID][i]
			return &out, nil
		}
	}
	return nil, nil
}

// Change : Applies only what was given, as a patch does.
func (l *lists) Change(_ context.Context, _ string, which tasks.List, id string, a tasks.Amend) (*tasks.Task, error) {
	if l.refuse != nil {
		return nil, l.refuse
	}
	for i := range l.on[which.ID] {
		if l.on[which.ID][i].ID != id {
			continue
		}
		if a.Title != nil {
			l.on[which.ID][i].Title = *a.Title
		}
		if a.Notes != nil {
			l.on[which.ID][i].Notes = *a.Notes
		}
		if a.Due != nil {
			l.on[which.ID][i].Due = *a.Due
		}
		out := l.on[which.ID][i]
		return &out, nil
	}
	return nil, nil
}

// two : A store with two lists, for moving and deleting.
func two(a, b []tasks.Task) *lists {
	return &lists{
		held: []tasks.List{{ID: "l1", Title: "My Tasks"}, {ID: "l2", Title: "Shopping"}},
		on:   map[string][]tasks.Task{"l1": a, "l2": b},
	}
}

// one : A store with a single list, as the owner has.
func one(on ...tasks.Task) *lists {
	l := &lists{held: []tasks.List{{ID: "l1", Title: "My Tasks"}}, on: map[string][]tasks.Task{}}
	l.on["l1"] = on
	return l
}

// run : Calls one tool and returns what it answered.
func run(t *testing.T, l *lists, name, args string) tool.Result {
	t.Helper()
	for _, x := range taskstool.All(l, taskstool.Clock{Location: india()}) {
		if x.Name != name {
			continue
		}
		return x.Run(context.Background(), tool.Invocation{
			Args:   []byte(args),
			Caller: tool.Caller{UserID: "usr_1", Channel: chat.ChannelDirect},
		})
	}
	t.Fatalf("no tool called %s", name)
	return tool.Result{}
}

// TestAddingSeveralIsOneCall : The API has no batch, so the loop is
// here -- but the model must not be made to call the tool four times
// for four things. A turn has thirty seconds and the model spends it.
func TestAddingSeveralIsOneCall(t *testing.T) {
	l := one()

	got := run(t, l, "task_add", `{"titles":["bread","eggs","milk"],"saying":"adding those"}`)

	if got.Outcome != "ok" {
		t.Fatalf("outcome = %q: %s", got.Outcome, got.Content)
	}
	if n := len(l.on["l1"]); n != 3 {
		t.Errorf("%d on the list, want all three", n)
	}
	for _, want := range []string{"bread", "eggs", "milk"} {
		if !strings.Contains(got.Content, want) {
			t.Errorf("content = %q, want %q named", got.Content, want)
		}
	}
}

// TestATimeOfDayIsRefusedRatherThanLost : Google records the day and
// discards the hour, and will not give it back. Accepting a time and
// silently dropping it is the worst outcome: nothing reports an error
// and the person believes it was kept.
func TestATimeOfDayIsRefusedRatherThanLost(t *testing.T) {
	got := run(t, one(), "task_add", `{"titles":["call the bank"],"due":"2026-10-02T15:00","saying":"adding"}`)

	if got.Outcome != "failed" {
		t.Errorf("outcome = %q, want it refused: %s", got.Outcome, got.Content)
	}
	if !strings.Contains(got.Content, "never a time") {
		t.Errorf("content = %q, want it to say why", got.Content)
	}
}

// TestTickingOffSaysHowManyAreLeft : The listener's only check that one
// thing went and not the lot.
func TestTickingOffSaysHowManyAreLeft(t *testing.T) {
	l := one(
		tasks.Task{ID: "tsk_a", Title: "milk"},
		tasks.Task{ID: "tsk_b", Title: "bread"},
	)

	got := run(t, l, "task_done", `{"ids":["tsk_a"],"saying":"ticking that off"}`)

	if got.Outcome != "ok" {
		t.Fatalf("outcome = %q: %s", got.Outcome, got.Content)
	}
	for _, want := range []string{"milk", "2 still to do", "1 now"} {
		if !strings.Contains(got.Content, want) {
			t.Errorf("content = %q, want %q in it", got.Content, want)
		}
	}
}

// TestAnIdentifierThatIsNotThereIsSaidPlainly : Not silence, and not a
// claim that it was done.
func TestAnIdentifierThatIsNotThereIsSaidPlainly(t *testing.T) {
	got := run(t, one(tasks.Task{ID: "tsk_a", Title: "milk"}), "task_done",
		`{"ids":["tsk_nope"],"saying":"ticking"}`)

	if got.Outcome != "failed" {
		t.Errorf("outcome = %q, want it refused: %s", got.Outcome, got.Content)
	}
}

// TestTheListSaysTheTotalAndWhatIsLeft : A capped listing that does not
// say its total is how seventy-one conversations came to be reported as
// ten.
func TestTheListSaysTheTotalAndWhatIsLeft(t *testing.T) {
	l := one(
		tasks.Task{ID: "tsk_a", Title: "milk"},
		tasks.Task{ID: "tsk_b", Title: "bread", Done: true},
	)

	got := run(t, l, "task_list", `{"saying":"reading it"}`)

	for _, want := range []string{"2 tasks", "1 still to do", "[id tsk_a]"} {
		if !strings.Contains(got.Content, want) {
			t.Errorf("content = %q, want %q in it", got.Content, want)
		}
	}
}

// TestAMangledListNameIsMatchedAndOwned : Names arrive through speech.
func TestAMangledListNameIsMatchedAndOwned(t *testing.T) {
	l := &lists{
		held: []tasks.List{{ID: "l1", Title: "My Tasks"}, {ID: "l2", Title: "Shopping"}},
		on:   map[string][]tasks.Task{"l2": {{ID: "tsk_a", Title: "milk"}}},
	}

	got := run(t, l, "task_list", `{"list":"shoping","saying":"reading it"}`)

	if got.Outcome != "ok" {
		t.Fatalf("outcome = %q: %s", got.Outcome, got.Content)
	}
	if !strings.Contains(got.Content, "milk") {
		t.Errorf("the wrong list was read: %s", got.Content)
	}
	if !strings.Contains(strings.ToLower(got.Content), "shopping") {
		t.Errorf("content = %q, want it to own which list it took", got.Content)
	}
}

// TestGoogleNotConnectedIsNotAnEmptyList : "You have nothing to do" is
// a claim about their life; "I cannot see your lists" is the truth.
func TestGoogleNotConnectedIsNotAnEmptyList(t *testing.T) {
	l := one()
	l.refuse = errors.New("something went wrong")

	got := run(t, l, "task_list", `{"saying":"reading it"}`)

	if got.Outcome != "failed" {
		t.Errorf("outcome = %q, want a failure rather than an empty list", got.Outcome)
	}
	if strings.Contains(strings.ToLower(got.Content), "nothing on") {
		t.Errorf("a failure was reported as an empty list: %s", got.Content)
	}
}

// TestStartingAList : The one write to a list that cannot lose
// anything.
func TestStartingAList(t *testing.T) {
	l := one()

	got := run(t, l, "task_list_add", `{"title":"Shopping","saying":"starting one"}`)

	if got.Outcome != "ok" {
		t.Fatalf("outcome = %q: %s", got.Outcome, got.Content)
	}
	if len(l.held) != 2 {
		t.Errorf("%d lists, want the new one alongside the old", len(l.held))
	}
	if !strings.Contains(got.Content, "Shopping") {
		t.Errorf("content = %q, want it named", got.Content)
	}
}

// TestAListThatAlreadyExistsIsNotStartedAgain : Speech produces
// "Shoping" for "Shopping", and two lists a letter apart is a mess
// nobody asked for.
func TestAListThatAlreadyExistsIsNotStartedAgain(t *testing.T) {
	l := &lists{
		held: []tasks.List{{ID: "l1", Title: "Shopping"}},
		on:   map[string][]tasks.Task{},
	}

	got := run(t, l, "task_list_add", `{"title":"Shoping","saying":"starting one"}`)

	if len(l.held) != 1 {
		t.Errorf("%d lists, want the near-duplicate refused", len(l.held))
	}
	if !strings.Contains(got.Content, "already a list called Shopping") {
		t.Errorf("content = %q, want it to say which one it meant", got.Content)
	}
}

// TestRenamingSaysBothNames : The person is listening and has nothing
// to compare against, so a change carries both ends.
func TestRenamingSaysBothNames(t *testing.T) {
	l := &lists{
		held: []tasks.List{{ID: "l1", Title: "Shopping"}},
		on:   map[string][]tasks.Task{"l1": {{ID: "tsk_a", Title: "milk"}}},
	}

	got := run(t, l, "task_list_rename", `{"list":"shopping","title":"Groceries","saying":"renaming it"}`)

	if got.Outcome != "ok" {
		t.Fatalf("outcome = %q: %s", got.Outcome, got.Content)
	}
	for _, want := range []string{"Shopping", "Groceries"} {
		if !strings.Contains(got.Content, want) {
			t.Errorf("content = %q, want %q in it", got.Content, want)
		}
	}
	if l.held[0].Title != "Groceries" {
		t.Errorf("the list is called %q", l.held[0].Title)
	}
	// What was on it stays on it.
	if len(l.on["l1"]) != 1 {
		t.Errorf("renaming lost what was on the list: %v", l.on["l1"])
	}
}

// TestRenamingToTheSameNameChangesNothing : And says so, rather than
// reporting a change that did not happen.
func TestRenamingToTheSameNameChangesNothing(t *testing.T) {
	l := &lists{held: []tasks.List{{ID: "l1", Title: "Shopping"}}, on: map[string][]tasks.Task{}}

	got := run(t, l, "task_list_rename", `{"list":"Shopping","title":"Shopping","saying":"renaming"}`)

	if !strings.Contains(got.Content, "already called") {
		t.Errorf("content = %q, want it to say nothing moved", got.Content)
	}
}

// TestRemovingCountsBothEnds : The API answers a deletion with an
// empty body, so the only way to know what went is to count.
func TestRemovingCountsBothEnds(t *testing.T) {
	l := one(
		tasks.Task{ID: "tsk_a", Title: "milk"},
		tasks.Task{ID: "tsk_b", Title: "bread"},
	)

	got := run(t, l, "task_remove", `{"ids":["tsk_a"],"saying":"taking that off"}`)

	if got.Outcome != "ok" {
		t.Fatalf("outcome = %q: %s", got.Outcome, got.Content)
	}
	for _, want := range []string{"were 2 tasks before", "1 task now"} {
		if !strings.Contains(got.Content, want) {
			t.Errorf("content = %q, want %q in it", got.Content, want)
		}
	}
}

// TestClearingLeavesWhatIsStillToDo : It takes the finished ones and
// nothing else.
func TestClearingLeavesWhatIsStillToDo(t *testing.T) {
	l := one(
		tasks.Task{ID: "tsk_a", Title: "milk", Done: true},
		tasks.Task{ID: "tsk_b", Title: "bread"},
	)

	got := run(t, l, "task_clear", `{"saying":"tidying"}`)

	if got.Outcome != "ok" {
		t.Fatalf("outcome = %q: %s", got.Outcome, got.Content)
	}
	if len(l.on["l1"]) != 1 || l.on["l1"][0].Title != "bread" {
		t.Errorf("clearing took the wrong ones: %v", l.on["l1"])
	}
}

// TestClearingNothingSaysSo : Rather than reporting a tidy-up that
// removed nothing.
func TestClearingNothingSaysSo(t *testing.T) {
	got := run(t, one(tasks.Task{ID: "tsk_a", Title: "milk"}), "task_clear", `{"saying":"tidying"}`)

	if !strings.Contains(got.Content, "nothing was cleared") {
		t.Errorf("content = %q", got.Content)
	}
}

// TestMovingSaysBothLists : A task that changed list, and which two.
func TestMovingSaysBothLists(t *testing.T) {
	l := two([]tasks.Task{{ID: "tsk_a", Title: "milk"}}, nil)

	got := run(t, l, "task_move", `{"ids":["tsk_a"],"to":"shopping","saying":"moving it"}`)

	if got.Outcome != "ok" {
		t.Fatalf("outcome = %q: %s", got.Outcome, got.Content)
	}
	for _, want := range []string{"My Tasks", "Shopping"} {
		if !strings.Contains(got.Content, want) {
			t.Errorf("content = %q, want %q in it", got.Content, want)
		}
	}
	if len(l.on["l2"]) != 1 || len(l.on["l1"]) != 0 {
		t.Errorf("the task did not move: from=%v to=%v", l.on["l1"], l.on["l2"])
	}
}

// TestDeletingAListSaysWhatWentWithIt : The answer is the only record
// of what was lost, because Google keeps none and offers no undo.
func TestDeletingAListSaysWhatWentWithIt(t *testing.T) {
	l := two(nil, []tasks.Task{
		{ID: "tsk_a", Title: "milk"},
		{ID: "tsk_b", Title: "bread"},
	})

	got := run(t, l, "task_list_remove", `{"list":"shopping","saying":"removing it"}`)

	if got.Outcome != "ok" {
		t.Fatalf("outcome = %q: %s", got.Outcome, got.Content)
	}
	if !strings.Contains(got.Content, "2 tasks on it went with it") {
		t.Errorf("content = %q, want it to say what was lost", got.Content)
	}
	if len(l.held) != 1 {
		t.Errorf("%d lists left, want the other one gone", len(l.held))
	}
}

// TestTheLastListIsNotDeleted : Google will not leave somebody with
// none, and finding that out as an error after the fact is worse than
// saying so.
func TestTheLastListIsNotDeleted(t *testing.T) {
	l := one(tasks.Task{ID: "tsk_a", Title: "milk"})

	got := run(t, l, "task_list_remove", `{"list":"My Tasks","saying":"removing it"}`)

	if got.Outcome != "failed" {
		t.Errorf("outcome = %q, want it refused: %s", got.Outcome, got.Content)
	}
	if len(l.held) != 1 {
		t.Error("the only list was deleted")
	}
}

// TestChangingATaskSaysBothEnds : The person is listening and has
// nothing to compare against.
func TestChangingATaskSaysBothEnds(t *testing.T) {
	l := one(tasks.Task{ID: "tsk_a", Title: "milk"})

	got := run(t, l, "task_update", `{"id":"tsk_a","title":"oat milk","saying":"changing that"}`)

	if got.Outcome != "ok" {
		t.Fatalf("outcome = %q: %s", got.Outcome, got.Content)
	}
	for _, want := range []string{"milk", "oat milk"} {
		if !strings.Contains(got.Content, want) {
			t.Errorf("content = %q, want %q in it", got.Content, want)
		}
	}
}

// TestChangingOneFieldLeavesTheRest : A patch, not a replacement. An
// update would strip the notes off anything renamed.
func TestChangingOneFieldLeavesTheRest(t *testing.T) {
	l := one(tasks.Task{ID: "tsk_a", Title: "milk", Notes: "the oat one"})

	run(t, l, "task_update", `{"id":"tsk_a","title":"oat milk","saying":"changing"}`)

	if l.on["l1"][0].Notes != "the oat one" {
		t.Errorf("the notes were lost: %q", l.on["l1"][0].Notes)
	}
}

// TestChangingSomethingThatIsNotThereIsRefused : Rather than reported
// as done.
func TestChangingSomethingThatIsNotThereIsRefused(t *testing.T) {
	got := run(t, one(tasks.Task{ID: "tsk_a", Title: "milk"}), "task_update",
		`{"id":"tsk_nope","title":"bread","saying":"changing"}`)

	if got.Outcome != "failed" {
		t.Errorf("outcome = %q, want it refused: %s", got.Outcome, got.Content)
	}
}

// TestATimeOfDayIsRefusedOnAChangeToo : The same reason as adding one.
func TestATimeOfDayIsRefusedOnAChangeToo(t *testing.T) {
	got := run(t, one(tasks.Task{ID: "tsk_a", Title: "milk"}), "task_update",
		`{"id":"tsk_a","due":"2026-10-02T15:00","saying":"changing"}`)

	if got.Outcome != "failed" {
		t.Errorf("outcome = %q, want it refused: %s", got.Outcome, got.Content)
	}
}
