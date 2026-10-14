package rules

import (
	"strings"
	"testing"

	"personal-assistant/internal/domain/job"
)

// TestANilLookupRefusesRatherThanPanics : the first rule the assistant
// ever wrote took the server down.
//
// allowed() calls the lookup for every act that names a tool. The path
// the assistant writes its own rules by passed nil, which could not
// show while no rule had ever been written: nothing ever reached an
// act naming a tool. On 14 October two fixes cleared the way, a rule
// was written, and the pass panicked on a nil func.
//
// A missing lookup is a mistake in the caller. It is still not a
// reason to bring down the loop that found it.
func TestANilLookupRefusesRatherThanPanics(t *testing.T) {
	for _, c := range []struct {
		what string
		acts []job.Act
	}{
		{"a look-up act", []job.Act{{Do: job.DoTool, Use: "event_recent"}}},
		{"a write that names tools", []job.Act{
			{Do: job.DoWrite, Ask: "say what the battery is at", Tools: []string{"event_recent"}}}},
	} {
		t.Run(c.what, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panicked on a nil lookup: %v", r)
				}
			}()
			if bad := allowed(c.acts, nil); bad == "" {
				t.Error("a rule naming a tool was allowed with nothing to check it against")
			} else if !strings.Contains(bad, "to check th") {
				t.Errorf("refused for the wrong reason: %q", bad)
			}
		})
	}
}

// And a rule that names no tool at all is unaffected: there is nothing
// to look up, so a missing lookup is not its problem.
func TestNamingNoToolNeedsNoLookup(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	acts := []job.Act{{Do: job.DoSay, Text: "Time to stand up."}}
	if bad := allowed(acts, nil); bad != "" {
		t.Errorf("a rule that looks nothing up was refused: %q", bad)
	}
}
