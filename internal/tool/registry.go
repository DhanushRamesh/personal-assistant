package tool

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
)

// Registry : Every tool the assistant has.
//
// The zero value is empty and usable.
type Registry struct {
	tools map[string]Tool
	order []string
}

// NewRegistry : A registry holding the given tools, or the first reason one
// of them could not be held.
func NewRegistry(tools ...Tool) (*Registry, error) {
	r := &Registry{tools: map[string]Tool{}}
	for _, t := range tools {
		if err := r.Add(t); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// Add : Registers a tool, refusing one that is not usable.
//
// Refused at registration rather than at the moment somebody speaks. A tool
// with no description is one the model will guess about, and finding that out
// from a wrong answer is far more expensive than finding it out at startup.
func (r *Registry) Add(t Tool) error {
	if r.tools == nil {
		r.tools = map[string]Tool{}
	}

	switch {
	case strings.TrimSpace(t.Name) == "":
		return fmt.Errorf("tool: a tool needs a name")
	case strings.TrimSpace(t.Purpose) == "":
		return fmt.Errorf("tool: %s needs a purpose", t.Name)
	case strings.TrimSpace(t.UseWhen) == "":
		return fmt.Errorf("tool: %s needs to say when to use it", t.Name)
	case len(t.Channels) == 0:
		return fmt.Errorf("tool: %s reaches no channel, so nothing can call it", t.Name)
	case t.Run == nil:
		return fmt.Errorf("tool: %s does nothing", t.Name)
	}

	for name, p := range t.Params.Properties {
		switch {
		case strings.TrimSpace(p.Description) == "":
			return fmt.Errorf("tool: %s takes %s without saying what it is", t.Name, name)
		case strings.TrimSpace(p.Type) == "":
			return fmt.Errorf("tool: %s takes %s without saying its type", t.Name, name)
		}
	}
	for _, required := range t.Params.Required {
		if _, ok := t.Params.Properties[required]; !ok {
			return fmt.Errorf("tool: %s requires %s, which it does not take", t.Name, required)
		}
	}

	// Nothing supplies arguments to a prefetch: it runs before anybody
	// has been asked anything. A tool that needs them would be called
	// with none and answer about the wrong thing, which is worse than
	// not being called.
	if t.Prefetch && len(t.Params.Required) > 0 {
		return fmt.Errorf("tool: %s is prefetched but requires %s, and a prefetch has nobody to ask",
			t.Name, strings.Join(t.Params.Required, ", "))
	}

	if _, taken := r.tools[t.Name]; taken {
		return fmt.Errorf("tool: %s is registered twice", t.Name)
	}

	r.tools[t.Name] = t
	r.order = append(r.order, t.Name)
	return nil
}

// Get : The tool with the given name, and whether there is one.
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// All : Every tool, in the order they were registered.
func (r *Registry) All() []Tool {
	out := make([]Tool, 0, len(r.order))
	for _, name := range r.order {
		out = append(out, r.tools[name])
	}
	return out
}

// For : The tools a given channel may reach, in registration order.
//
// This is the gate, and it is the first of two. A tool not returned here is
// never described to the model, so it cannot be called by something that
// does not know it exists. Call checks again, because what the model is told
// is a prompt, and a prompt is not a boundary.
func (r *Registry) For(c chat.Channel) []Tool {
	out := make([]Tool, 0, len(r.order))
	for _, name := range r.order {
		if t := r.tools[name]; t.Reaches(c) {
			out = append(out, t)
		}
	}
	return out
}

// Prefetched : The tools to run before the model is asked, in the
// order they were registered.
//
// Channel-filtered like everything else: a tool a channel cannot reach
// is not fetched for it either.
func (r *Registry) Prefetched(c chat.Channel) []Tool {
	out := make([]Tool, 0, len(r.order))
	for _, name := range r.order {
		if t := r.tools[name]; t.Prefetch && t.Reaches(c) {
			out = append(out, t)
		}
	}
	return out
}

// Names : Every tool's name, sorted, for a log or an error.
func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.tools))
	for name := range r.tools {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Call : Runs a tool for a caller, refusing one the caller cannot reach.
//
// The channel is checked here as well as in For, which looks like the same
// check twice and is not. For decides what the model is told about; this
// decides what actually runs. A model that has been told wrongly, or has
// invented a name, or is repeating a call from earlier in a conversation that
// arrived by a different channel, is stopped here.
func (r *Registry) Call(ctx context.Context, name string, in Invocation) Result {
	t, ok := r.Get(name)
	if !ok {
		return Failed(fmt.Sprintf(
			"There is no tool called %q. The ones available are: %s.",
			name, strings.Join(r.Names(), ", ")))
	}

	if !t.Reaches(in.Caller.Channel) {
		// Said out loud rather than hidden. A tool that silently does nothing
		// leaves the model to invent a reason it failed, and it will invent
		// one that sounds plausible.
		return Failed(fmt.Sprintf(
			"%s cannot be used from this channel. Tell the person it has to be done another way.",
			name))
	}

	// Read before write, for every domain that has something to read.
	// The reading is done here when it can be, so the model is handed
	// what is there rather than sent to fetch it.
	if sent := r.readFirst(ctx, t, in); sent != nil {
		return *sent
	}

	// Taken off before anything else looks at the arguments: it is added
	// to every schema on the way out and is not an argument of any tool.
	_, in.Args = TakeSaying(in.Args)

	// Checked before running, and answered rather than refused. A model told
	// which argument was wrong and what was allowed corrects itself on the
	// next hop; one told only that the call was invalid guesses again.
	if err := Validate(t.Params, in.Args); err != nil {
		return Failed(fmt.Sprintf("%s was not called correctly: %s. Call it again with that fixed. "+
			"This call never ran, so it says nothing about what exists: do not tell them the thing "+
			"is not there on the strength of it.", name, err))
	}

	return t.Run(ctx, in)
}
