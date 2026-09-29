package tool_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/DhanushRamesh/personal-assistant/internal/tool"
)

// ids : A list-of-identifiers property, the shape every batch removal uses.
func ids() tool.Schema {
	return tool.Schema{
		Required: []string{"ids"},
		Properties: map[string]tool.Property{
			"ids": {
				Type:        "array",
				Description: "Which ones to remove.",
				MinItems:    tool.Bound(1),
				MaxItems:    tool.Bound(25),
				Items: &tool.Property{
					Type:        "string",
					Pattern:     `^[a-z0-9_]{5,64}$`,
					Description: "An identifier from a listing.",
				},
			},
		},
	}
}

// TestArrayRendersAsJSONSchema : The model is shown a real array schema,
// with the element's own rules inside it.
func TestArrayRendersAsJSONSchema(t *testing.T) {
	raw, err := ids().MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	t.Logf("%s", raw)

	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	p := got["properties"].(map[string]any)["ids"].(map[string]any)
	if p["type"] != "array" {
		t.Errorf("type = %v, want array", p["type"])
	}
	if p["minItems"] == nil || p["maxItems"] == nil {
		t.Errorf("bounds missing: %v", p)
	}
	item, ok := p["items"].(map[string]any)
	if !ok || item["pattern"] == nil {
		t.Errorf("the element's own rules were dropped: %v", p["items"])
	}
}

// TestArrayValidation : What a list may and may not contain.
func TestArrayValidation(t *testing.T) {
	for _, tc := range []struct {
		name, args, want string
	}{
		{"one is fine", `{"ids":["evt_abc12"]}`, ""},
		{"several are fine", `{"ids":["evt_abc12","evt_def34","evt_ghi56"]}`, ""},
		{"empty is refused", `{"ids":[]}`, "at least 1"},
		{"not a list", `{"ids":"evt_abc12"}`, "must be a list"},
		{"a bad element is named by position", `{"ids":["evt_abc12","<the_second_one>"]}`, "ids[1]"},
		{"a placeholder is still caught", `{"ids":["<id_for_the_event>"]}`, "description of the thing"},
	} {
		err := tool.Validate(ids(), []byte(tc.args))
		switch {
		case tc.want == "" && err != nil:
			t.Errorf("%s: unexpected refusal: %v", tc.name, err)
		case tc.want != "" && err == nil:
			t.Errorf("%s: was accepted and should not have been", tc.name)
		case tc.want != "" && !strings.Contains(err.Error(), tc.want):
			t.Errorf("%s: %v — want mention of %q", tc.name, err, tc.want)
		}
	}
}
