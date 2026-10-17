//go:build spike

// Package einomodel puts an Environment behind Eino's chat model
// interface, so that Eino's agents drive this server's gateway.
//
// A spike, kept behind a build tag. What it is for is finding out three
// things before anything is migrated: whether the gateway answers
// through Eino at all, whether tool calls survive the round trip, and
// which of the runner's own behaviours have nowhere to live in
// somebody else's loop.
package einomodel

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"personal-assistant/internal/base/thinking/environment"
)

// Model : An Environment as an Eino ToolCallingChatModel.
type Model struct {
	env   environment.Environment
	tools []environment.ToolSpec
}

// New : Wraps env.
func New(env environment.Environment) *Model { return &Model{env: env} }

// WithTools : A copy of the model bound to these tools.
func (m *Model) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	specs := make([]environment.ToolSpec, 0, len(tools))
	for _, t := range tools {
		spec := environment.ToolSpec{Name: t.Name, Description: t.Desc}
		if t.ParamsOneOf != nil {
			js, err := t.ParamsOneOf.ToJSONSchema()
			if err != nil {
				return nil, fmt.Errorf("einomodel: %s parameters: %w", t.Name, err)
			}
			raw, err := json.Marshal(js)
			if err != nil {
				return nil, fmt.Errorf("einomodel: %s parameters: %w", t.Name, err)
			}
			spec.Parameters = raw
		}
		specs = append(specs, spec)
	}
	return &Model{env: m.env, tools: specs}, nil
}

// Generate : One whole answer.
func (m *Model) Generate(ctx context.Context, input []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	stream, err := m.env.Run(ctx, m.request(input))
	if err != nil {
		return nil, err
	}
	var last environment.Message
	for msg := range stream {
		if msg.Kind.Terminal() {
			last = msg
		}
	}
	switch {
	case last.Kind == environment.KindError:
		return nil, fmt.Errorf("einomodel: %s: %s", last.Code, last.Detail)
	case last.Kind == "":
		return nil, fmt.Errorf("einomodel: the stream closed with no answer: %w", ctx.Err())
	}
	return answer(last), nil
}

// Stream : The answer as it is written.
func (m *Model) Stream(ctx context.Context, input []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	stream, err := m.env.Run(ctx, m.request(input))
	if err != nil {
		return nil, err
	}
	reader, writer := schema.Pipe[*schema.Message](1)
	go func() {
		defer writer.Close()
		for msg := range stream {
			switch msg.Kind {
			case environment.KindError:
				writer.Send(nil, fmt.Errorf("einomodel: %s: %s", msg.Code, msg.Detail))
				return
			case environment.KindFinal, environment.KindToolCalls:
				writer.Send(answer(msg), nil)
			}
		}
	}()
	return reader, nil
}

// answer : One of this server's messages as one of Eino's.
func answer(m environment.Message) *schema.Message {
	out := &schema.Message{Role: schema.Assistant, Content: m.Text}
	for i, c := range m.ToolCalls {
		index := i
		// A tool that takes nothing is called with nothing, and the
		// gateway says so with an empty string. Eino unmarshals the
		// arguments whatever the tool takes, and an empty string is
		// not JSON, so the call fails before the tool is reached.
		if strings.TrimSpace(c.Arguments) == "" {
			c.Arguments = "{}"
		}
		out.ToolCalls = append(out.ToolCalls, schema.ToolCall{
			Index: &index,
			ID:    c.ID,
			Type:  "function",
			Function: schema.FunctionCall{
				Name:      c.Name,
				Arguments: c.Arguments,
			},
		})
	}
	return out
}

// request : Eino's flat list of messages as this server's request.
//
// The shapes disagree in one place that matters. Eino hands over every
// message each time, including the system one and every tool result;
// an Environment takes the system prompt apart from the history and
// the last thing said apart from both. So the list is sorted rather
// than copied.
func (m *Model) request(input []*schema.Message) environment.Request {
	req := environment.Request{Tools: m.tools, Purpose: environment.PurposeChat}

	var system []string
	var turns []environment.Turn
	for _, msg := range input {
		switch msg.Role {
		case schema.System:
			system = append(system, msg.Content)
		case schema.User:
			turns = append(turns, environment.Turn{Role: environment.RoleUser, Text: msg.Content})
		case schema.Assistant:
			turn := environment.Turn{Role: environment.RoleAssistant, Text: msg.Content}
			for _, c := range msg.ToolCalls {
				turn.ToolCalls = append(turn.ToolCalls, environment.ToolCall{
					ID: c.ID, Name: c.Function.Name, Arguments: c.Function.Arguments,
				})
			}
			turns = append(turns, turn)
		case schema.Tool:
			// Results get a turn of their own, as the runner's do,
			// and consecutive ones share it because they answer the
			// same round. A turn carrying both works now too -- that
			// is what the asWire fix was -- but keeping them apart
			// says the order out loud instead of relying on it.
			result := environment.ToolResult{ID: msg.ToolCallID, Content: msg.Content}
			if n := len(turns); n > 0 && len(turns[n-1].ToolResults) > 0 {
				turns[n-1].ToolResults = append(turns[n-1].ToolResults, result)
				continue
			}
			turns = append(turns, environment.Turn{
				Role:        environment.RoleTool,
				ToolResults: []environment.ToolResult{result},
			})
		}
	}
	req.SystemPrompt = strings.Join(system, "\n\n")

	// Only a trailing user message is the prompt. Taking the last one
	// from anywhere moved it behind the turn that answered it, and the
	// gateway refused the lot: a tool result arrived as the first
	// message, with no tool call in front of it to belong to. A round
	// that ends on a tool result has nothing newly said, and an empty
	// prompt is simply left out of the wire.
	if n := len(turns); n > 0 && turns[n-1].Role == environment.RoleUser {
		req.Prompt = turns[n-1].Text
		turns = turns[:n-1]
	}
	req.History = turns
	return req
}
