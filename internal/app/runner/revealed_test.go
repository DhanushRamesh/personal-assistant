package runner_test

import (
	"context"
	"testing"

	"personal-assistant/internal/app/runner"
	"personal-assistant/internal/app/tool"
	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/base/thinking/environment"
)

// deferring : A registry that holds calendar_free back until it is
// asked about, which is what it does in the running server: it is not
// in the hot list.
//
// A reading tool, because this is about the deferring and nothing
// else. A write that never landed is sent back for another go, which
// is right and would here look like the deferring having failed.
func deferring(t *testing.T, run func() tool.Result) *tool.Registry {
	t.Helper()
	r, err := tool.NewRegistry(tool.Tool{
		Name:     "calendar_free",
		Domain:   "calendar",
		Lists:    true,
		Purpose:  "Find a free hour.",
		UseWhen:  "They ask when they are free.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params:   tool.Schema{Properties: map[string]tool.Property{}},
		Run:      func(context.Context, tool.Invocation) tool.Result { return run() },
	})
	if err != nil {
		t.Fatal(err)
	}
	// Without this nothing is deferred at all, and the registry
	// behaves as it did before the mechanism existed.
	if err := r.Add(tool.Describing(r)); err != nil {
		t.Fatal(err)
	}
	return r
}

// TestADescribedToolStaysDescribedForTheConversation : The cost this
// exists to remove.
//
// revealed used to start empty on every chat, and a chat is one thing
// the person said. Asked four times about the same cinema booking, the
// model called tool_describe for the same tool four separate times,
// each one after a guessed call that had to be refused, each one a
// round somebody waited through -- while the result it was handed each
// time said "callable from here on".
func TestADescribedToolStaysDescribedForTheConversation(t *testing.T) {
	var runs int
	tools := deferring(t, func() tool.Result { runs++; return tool.OK("Moved it.") })

	h := newHarness(t, &scripted{rounds: []environment.Message{
		calling("calendar_free"),
		environment.Final("Moved it."),
		calling("calendar_free"),
		environment.Final("Moved it again."),
	}}, runner.Options{Tools: tools})

	first := h.submit(t, "move the movie to ten past six")
	h.await(t, first.ID, chat.StatusCompleted, chat.StatusFailed)
	if runs != 0 {
		t.Fatalf("a tool was run from guessed arguments %d times", runs)
	}

	second := h.submit(t, "and call it Meesaya Murukku 2")
	h.await(t, second.ID, chat.StatusCompleted, chat.StatusFailed)
	if runs != 1 {
		t.Errorf("the tool ran %d times in the second chat, want 1: it was "+
			"described in the first and had to be described again", runs)
	}
}

// TestAnUndescribedToolIsStillHeldBack : The first call of a
// conversation is still refused, or the deferring does nothing and the
// model acts on arguments it guessed.
func TestAnUndescribedToolIsStillHeldBack(t *testing.T) {
	var runs int
	tools := deferring(t, func() tool.Result { runs++; return tool.OK("Moved it.") })

	h := newHarness(t, &scripted{rounds: []environment.Message{
		calling("calendar_free"),
		environment.Final("Moved it."),
	}}, runner.Options{Tools: tools})

	tk := h.submit(t, "move the movie to ten past six")
	h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

	if runs != 0 {
		t.Errorf("a tool the model had never been given the arguments for ran %d times", runs)
	}
}

// TestADescribedToolExpires : The other side of it, and the fault that
// put it there.
//
// Owner, 18 October 2026, having asked about his battery, then about a
// film, then about two people, and then said "look again": it read his
// battery. The battery tool had been described three questions earlier
// and was still on the table, so a bare follow-up found something to
// look at again that nobody had mentioned since.
//
// Three of his turns, counting the one being answered. A chain keeps
// its tools; another subject's do not.
func TestADescribedToolExpires(t *testing.T) {
	var runs int
	tools := deferring(t, func() tool.Result { runs++; return tool.OK("Moved it.") })

	// Described on the first turn, then three turns of something else,
	// then called again. By then it should be a name once more, so the
	// call is refused rather than run on guessed arguments.
	h := newHarness(t, &scripted{rounds: []environment.Message{
		calling("calendar_free"),
		environment.Final("Moved it."),
		environment.Final("Nothing, sir."),
		environment.Final("Nothing, sir."),
		environment.Final("Nothing, sir."),
		calling("calendar_free"),
		environment.Final("Moved it again."),
	}}, runner.Options{Tools: tools})

	first := h.submit(t, "move the movie to ten past six")
	h.await(t, first.ID, chat.StatusCompleted, chat.StatusFailed)
	if runs != 0 {
		t.Fatalf("a tool ran from guessed arguments %d times", runs)
	}

	for _, say := range []string{"how was the film", "is it going to rain", "who called me"} {
		tk := h.submit(t, say)
		h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)
	}

	last := h.submit(t, "look again")
	h.await(t, last.ID, chat.StatusCompleted, chat.StatusFailed)
	if runs != 0 {
		t.Errorf("a tool described four turns ago was still on the table: it ran %d times", runs)
	}
}
