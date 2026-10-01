package runner

import "testing"

// Describing a tool is the deferring mechanism costing a round, not the
// turn making progress: the model knows no more about the person
// afterwards than it did before.
func TestDescribingAToolIsNotProgress(t *testing.T) {
	cases := map[string]struct {
		ran  []string
		want bool
	}{
		"nothing ran":            {nil, false},
		"only describing":        {[]string{"tool_describe"}, false},
		"describing twice":       {[]string{"tool_describe", "tool_describe"}, false},
		"a real tool":            {[]string{"task_lists"}, true},
		"describing then a read": {[]string{"tool_describe", "task_lists"}, true},
	}
	for name, c := range cases {
		if got := did(c.ran); got != c.want {
			t.Errorf("%s: did = %v, want %v", name, got, c.want)
		}
	}
}
