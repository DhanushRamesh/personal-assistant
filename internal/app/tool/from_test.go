package tool_test

import (
	"strings"
	"testing"

	"personal-assistant/internal/app/tool"
)

// A read says where its answer came from, in the words a person would
// use for the thing rather than for the machine.
func TestAReadSaysWhereFrom(t *testing.T) {
	x := tool.Tool{Name: "calendar_events", Domain: "calendar"}
	carries := x.Carries()
	if !strings.Contains(carries, "your diary") {
		t.Errorf("a calendar read carries %q, want the person's words for it", carries)
	}
	// Never the machine's names. That is the leak persona.NamesMachinery
	// exists to stop, and it would arrive by this route instead.
	for _, banned := range []string{"calendar_events", "tool", "domain", "API"} {
		if strings.Contains(strings.ToLower(carries), strings.ToLower(banned)) {
			t.Errorf("a read's words name the machinery (%q): %q", banned, carries)
		}
	}
}

// A write is reported by what it did. "From your lists" in front of
// "added" reads as though the adding had been a lookup.
func TestAWriteSaysNothingAboutWhereFrom(t *testing.T) {
	x := tool.Tool{Name: "task_add", Domain: "task", Writes: true}
	if got := x.Carries(); got != "" {
		t.Errorf("a write carries %q, want nothing", got)
	}
}

// A domain with no words for it says nothing, rather than something
// awkward built out of its code name.
func TestAnUnknownDomainSaysNothing(t *testing.T) {
	x := tool.Tool{Name: "whatever", Domain: "thing"}
	if got := x.Carries(); got != "" {
		t.Errorf("an unnamed domain carries %q, want nothing", got)
	}
	if got := tool.From("not a domain"); got != "" {
		t.Errorf("From gave %q for a domain that does not exist", got)
	}
}

// TestEveryDomainThatReadsSaysWhereFrom : The domains a person asks
// about are the ones whose answers have to be checkable.
//
// Listed here by hand rather than read off the registry, so that adding
// a domain and forgetting its words fails here instead of going quiet.
func TestEveryDomainThatReadsSaysWhereFrom(t *testing.T) {
	for _, domain := range []string{
		"calendar", "coming", "task", "reminder", "mail",
		"memory", "event", "calling", "map", "web",
	} {
		words := tool.From(domain)
		if words == "" {
			t.Errorf("%q has no words for where its answers come from", domain)
			continue
		}
		// Not a check that the words differ from the domain: "your
		// mail" and "your reminders" are what he calls those things,
		// and the domains are named after them rather than the other
		// way round.
		if strings.ToUpper(words[:1]) == words[:1] {
			t.Errorf("%q starts a sentence (%q); it is said mid-clause", domain, words)
		}
	}
}
