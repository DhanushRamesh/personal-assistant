package tool_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
)

// both : Reachable from everywhere.
func both() []chat.Channel {
	return []chat.Channel{chat.ChannelVoice, chat.ChannelDirect}
}

// plain : A tool that does nothing, under a given name.
func plain(name string, channels ...chat.Channel) tool.Tool {
	if len(channels) == 0 {
		channels = both()
	}
	return tool.Tool{
		Name:     name,
		Purpose:  "Do the " + name + " thing. A second sentence that the catalogue leaves out.",
		UseWhen:  "They ask for it.",
		Channels: channels,
		Params:   tool.Schema{Properties: map[string]tool.Property{}},
		Run: func(context.Context, tool.Invocation) tool.Result {
			return tool.OK("done")
		},
	}
}

// withDescribe : A registry of the given tools plus tool_describe.
func withDescribe(t *testing.T, tools ...tool.Tool) *tool.Registry {
	t.Helper()
	r, err := tool.NewRegistry(tools...)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if err := r.Add(tool.Describing(r)); err != nil {
		t.Fatalf("Add: %v", err)
	}
	return r
}

// names : The names of some tools.
func names(tools []tool.Tool) []string {
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		out = append(out, t.Name)
	}
	return out
}

// has : Whether a name is in a list.
func has(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// A hot tool is described; a cold one is only named.
func TestOnlyTheHotOnesAreDescribed(t *testing.T) {
	r := withDescribe(t, plain("reminder_set"), plain("task_move"))

	offered := names(r.Offered(chat.ChannelVoice, nil))
	if !has(offered, "reminder_set") {
		t.Errorf("reminder_set is one of the hot twelve but was not offered: %v", offered)
	}
	if has(offered, "task_move") {
		t.Errorf("task_move is not hot but was described anyway: %v", offered)
	}

	deferred := names(r.Deferred(chat.ChannelVoice, nil))
	if !has(deferred, "task_move") {
		t.Errorf("task_move should be named in the catalogue: %v", deferred)
	}
}

// TestDescribeIsAlwaysOffered : Without it, a deferred tool is named in
// the prompt and reachable by nothing.
//
// It is not one of the hot twelve -- nobody calls it often -- so the
// first version of this deferred it along with everything else and the
// whole mechanism was unreachable.
func TestDescribeIsAlwaysOffered(t *testing.T) {
	r := withDescribe(t, plain("task_move"))
	if offered := names(r.Offered(chat.ChannelVoice, nil)); !has(offered, tool.DescribeName) {
		t.Fatalf("%s was not offered, so nothing can ever be described: %v",
			tool.DescribeName, offered)
	}
}

// TestNothingIsDeferredWithoutDescribe : A registry assembled without
// the mechanism behaves as it did before the mechanism existed.
func TestNothingIsDeferredWithoutDescribe(t *testing.T) {
	r, err := tool.NewRegistry(plain("task_move"), plain("reminder_set"))
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if offered := names(r.Offered(chat.ChannelVoice, nil)); len(offered) != 2 {
		t.Errorf("offered %v, want both: with no way to describe a tool, none may be held back", offered)
	}
	if deferred := r.Deferred(chat.ChannelVoice, nil); len(deferred) != 0 {
		t.Errorf("deferred %v, want none", names(deferred))
	}
	if r.Catalogue(chat.ChannelVoice, nil) != "" {
		t.Error("a catalogue was written for a registry that defers nothing")
	}
}

// With everything hot there is nothing to hold back, so the mechanism
// stays out of the prompt entirely.
func TestNoCatalogueWhenEverythingIsHot(t *testing.T) {
	r := withDescribe(t, plain("reminder_set"), plain("calendar_add"))
	if c := r.Catalogue(chat.ChannelVoice, nil); c != "" {
		t.Errorf("catalogue written with nothing deferred: %q", c)
	}
	if offered := names(r.Offered(chat.ChannelVoice, nil)); has(offered, tool.DescribeName) {
		t.Errorf("%s offered with nothing to describe: %v", tool.DescribeName, offered)
	}
}

// A tool asked about is described from then on, and drops out of the
// catalogue so it is not asked about twice.
func TestAskingAboutOneDescribesIt(t *testing.T) {
	r := withDescribe(t, plain("task_move"), plain("reminder_set"))
	revealed := map[string]bool{"task_move": true}

	if offered := names(r.Offered(chat.ChannelVoice, revealed)); !has(offered, "task_move") {
		t.Errorf("a revealed tool was not described: %v", offered)
	}
	if deferred := names(r.Deferred(chat.ChannelVoice, revealed)); has(deferred, "task_move") {
		t.Errorf("a revealed tool is still in the catalogue: %v", deferred)
	}
}

// TestDescribeRevealsRatherThanRepeats : It names what is now callable
// and stops there.
//
// Returning the schema here looked helpful and was not. Revealing a
// tool puts its real description in the next request, so the copy was
// redundant -- and a tool result is a stored message, so roughly
// fifteen hundred tokens of duplicate description were then re-sent
// with every later turn of the conversation.
func TestDescribeRevealsRatherThanRepeats(t *testing.T) {
	r := withDescribe(t, plain("task_move"))
	d, _ := r.Get(tool.DescribeName)

	got := d.Run(context.Background(), tool.Invocation{
		Caller: tool.Caller{Channel: chat.ChannelVoice},
		Args:   json.RawMessage(`{"names":["task_move"]}`),
	})
	if got.Outcome != conversation.OutcomeOK {
		t.Fatalf("outcome %q: %s", got.Outcome, got.Content)
	}
	if !strings.Contains(got.Content, "task_move") {
		t.Errorf("did not name the tool: %q", got.Content)
	}
	if len(got.Reveal) != 1 || got.Reveal[0] != "task_move" {
		t.Errorf("Reveal = %v, want the tool it described", got.Reveal)
	}
	// The schema belongs in the tool list, not in a stored message.
	if strings.Contains(got.Content, "properties") || strings.Contains(got.Content, tool.Saying) {
		t.Errorf("repeated the schema into the conversation: %q", got.Content)
	}
	if len(got.Content) > 200 {
		t.Errorf("content is %d bytes; it should be a sentence: %q", len(got.Content), got.Content)
	}
}

// A name that does not exist is reported as missing rather than invented,
// and the outcome says some of it did not happen.
func TestAnUnknownNameIsPartial(t *testing.T) {
	r := withDescribe(t, plain("task_move"))
	d, _ := r.Get(tool.DescribeName)

	got := d.Run(context.Background(), tool.Invocation{
		Caller: tool.Caller{Channel: chat.ChannelVoice},
		Args:   json.RawMessage(`{"names":["task_move","task_teleport"]}`),
	})
	if got.Outcome != conversation.OutcomePartial {
		t.Errorf("outcome %q, want partial when a name was not found", got.Outcome)
	}
	if !strings.Contains(got.Content, "task_teleport") {
		t.Errorf("did not say which name was missing: %q", got.Content)
	}
	if has(got.Reveal, "task_teleport") {
		t.Errorf("revealed a tool that does not exist: %v", got.Reveal)
	}
}

// TestItWillNotDescribeWhatTheChannelCannotReach : Describing a tool is
// not the same as being allowed to call it, but naming one out of reach
// still tells somebody about a thing that is not theirs.
func TestItWillNotDescribeWhatTheChannelCannotReach(t *testing.T) {
	r := withDescribe(t, plain("task_move", chat.ChannelDirect))
	d, _ := r.Get(tool.DescribeName)

	got := d.Run(context.Background(), tool.Invocation{
		Caller: tool.Caller{Channel: chat.ChannelVoice},
		Args:   json.RawMessage(`{"names":["task_move"]}`),
	})
	if got.Outcome != conversation.OutcomeFailed {
		t.Errorf("outcome %q, want a refusal for a tool this channel cannot reach", got.Outcome)
	}
	if len(got.Reveal) != 0 {
		t.Errorf("revealed %v across a channel boundary", got.Reveal)
	}
}

// Asking for everything defeats the point, so there is a cap.
func TestItRefusesToDescribeEverythingAtOnce(t *testing.T) {
	r := withDescribe(t, plain("a_one"), plain("a_two"))
	d, _ := r.Get(tool.DescribeName)

	got := d.Run(context.Background(), tool.Invocation{
		Caller: tool.Caller{Channel: chat.ChannelVoice},
		Args:   json.RawMessage(`{"names":["a","b","c","d","e","f","g"]}`),
	})
	if got.Outcome != conversation.OutcomeFailed {
		t.Errorf("outcome %q, want a refusal past the cap", got.Outcome)
	}
}

// The catalogue names every deferred tool and gives one line each, so a
// model can tell which to ask about without being given all of them.
func TestTheCatalogueNamesThemAndSaysWhatToDo(t *testing.T) {
	r := withDescribe(t, plain("task_move"), plain("calendar_free"))
	c := r.Catalogue(chat.ChannelVoice, nil)

	for _, want := range []string{"task_move", "calendar_free", tool.DescribeName} {
		if !strings.Contains(c, want) {
			t.Errorf("the catalogue does not mention %q: %q", want, c)
		}
	}
	if strings.Contains(c, "A second sentence") {
		t.Errorf("the catalogue carried the whole purpose, not one line: %q", c)
	}
}

// TestWithheldNoticesACallThatWasNotOffered : Being left out of the
// request does not stop the model calling it.
//
// Measured on 30 September 2026: thirteen tools were offered, the
// model read mail_search in the catalogue, called it with the one
// argument it could guess, and the call arrived. The endpoint forwards
// a call for a tool it was never given, so the runner has to notice.
func TestWithheldNoticesACallThatWasNotOffered(t *testing.T) {
	r := withDescribe(t, plain("task_move"), plain("reminder_set"))

	if !r.Withheld(chat.ChannelVoice, nil, "task_move") {
		t.Error("task_move is deferred but was not reported as withheld")
	}
	if r.Withheld(chat.ChannelVoice, nil, "reminder_set") {
		t.Error("reminder_set is hot, so it was offered and is not withheld")
	}
	if r.Withheld(chat.ChannelVoice, map[string]bool{"task_move": true}, "task_move") {
		t.Error("a revealed tool was reported as withheld")
	}
	if r.Withheld(chat.ChannelVoice, nil, "task_teleport") {
		t.Error("a tool that does not exist cannot be withheld")
	}
}

// Nothing is withheld by a registry that defers nothing, so the check
// costs nothing where the mechanism is not in use.
func TestNothingIsWithheldWithoutDescribe(t *testing.T) {
	r, err := tool.NewRegistry(plain("task_move"))
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if r.Withheld(chat.ChannelVoice, nil, "task_move") {
		t.Error("withheld a tool in a registry that cannot defer")
	}
}

// A stored tool_describe call says which tools it described, so the
// next chat of the conversation does not have to describe them again.
func TestWhatADescribeCallAskedFor(t *testing.T) {
	got := tool.Described(`{"names":["calendar_update","memory_search"],"saying":"looking it up"}`)

	if len(got) != 2 || got[0] != "calendar_update" || got[1] != "memory_search" {
		t.Errorf("read %v, want both names", got)
	}
}

// A call whose arguments will not read described nothing, so it
// revealed nothing.
func TestAMalformedDescribeCallRevealsNothing(t *testing.T) {
	for _, args := range []string{``, `{`, `{"names":"calendar_update"}`, `{"names":[]}`, `null`} {
		if got := tool.Described(args); len(got) != 0 {
			t.Errorf("%s read as %v, want nothing", args, got)
		}
	}
}
