package internal_test

import (
	"os/exec"
	"strings"
	"testing"
)

// prefix : Where this module's own packages live.
const prefix = "personal-assistant/internal/"

// above : What each layer is allowed to import, itself included.
//
// base is the floor: chat, prompt, event, logging and the rest of the
// things that twenty or thirty packages lean on. Nothing there may
// reach upward, which is what makes it safe to change anything above
// it -- a bug in domain cannot be a bug in base.
//
// domain is what the assistant knows about: reminders, rules, the
// diary, the person's mail. It stands on base.
//
// app is the wiring -- the HTTP surface, the tools, the thing that
// runs a turn. Everything may be reached from here and nothing
// reaches back.
var above = map[string][]string{
	"base":   {"base"},
	"domain": {"base", "domain"},
	"app":    {"base", "domain", "app"},
}

// within : The order inside base, which is not flat either.
//
// Twenty-seven packages in one directory hid a three-deep ordering.
// Go would have refused a cycle whatever the layout -- it rejects
// them at compile time -- but the ordering was there to be read and
// nothing read it.
//
//   - platform -- logging, config, storage, auth, failure. The floor
//     of the floor, and it may import nothing.
//   - world    -- what happened out there: events, places, what was
//     heard, what somebody usually does. Stands on platform.
//   - thinking -- reaching a model and feeding it: environment, llm,
//     prompt, embed, search. Stands on platform.
//   - records  -- what is kept: the chat, the person, their devices,
//     the conversation. Stands on platform and thinking, because a
//     conversation is made of prompts.
var within = map[string][]string{
	"platform": {"platform"},
	"world":    {"platform", "world"},
	"thinking": {"platform", "thinking"},
	"records":  {"platform", "thinking", "records"},
}

// TestNoLayerReachesUpward : The tree says what the import graph is,
// and this is what keeps the two the same.
//
// Without it the layering is a directory naming convention, which
// lasts until the first person in a hurry. The graph was measured
// before the directories were made: chat was imported by 29 packages
// and imported none, and that is the property being protected.
func TestNoLayerReachesUpward(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps=false",
		"-f", "{{.ImportPath}}{{range .Imports}} {{.}}{{end}}", "./...").Output()
	if err != nil {
		t.Fatalf("listing the packages: %v", err)
	}

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		from, ok := layerOf(fields[0])
		if !ok {
			continue // cmd, or a package outside the layered tree
		}
		for _, imported := range fields[1:] {
			to, ok := layerOf(imported)
			if !ok || allowed(from, to) {
				continue
			}
			t.Errorf("%s (%s) imports %s (%s): %s may only import %v",
				short(fields[0]), from, short(imported), to, from, above[from])
		}
	}
}

// TestBaseIsOrderedWithin : The floor has its own floor.
//
// Checked separately because it is a different claim: the one above
// says nothing below may reach up, and this one says the four groups
// inside base have an order and keep to it.
func TestBaseIsOrderedWithin(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps=false",
		"-f", "{{.ImportPath}}{{range .Imports}} {{.}}{{end}}", "./...").Output()
	if err != nil {
		t.Fatalf("listing the packages: %v", err)
	}

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		from, ok := groupOf(fields[0])
		if !ok {
			continue
		}
		for _, imported := range fields[1:] {
			to, ok := groupOf(imported)
			if !ok || permitted(from, to) {
				continue
			}
			t.Errorf("%s (%s) imports %s (%s): %s may only import %v",
				short(fields[0]), from, short(imported), to, from, within[from])
		}
	}
}

// groupOf : Which group inside base a package sits in.
func groupOf(importPath string) (string, bool) {
	rest, found := strings.CutPrefix(importPath, prefix+"base/")
	if !found {
		return "", false
	}
	group, _, _ := strings.Cut(rest, "/")
	if _, ok := within[group]; !ok {
		return "", false
	}
	return group, true
}

// permitted : Whether a package in from may import one in to.
func permitted(from, to string) bool {
	for _, ok := range within[from] {
		if ok == to {
			return true
		}
	}
	return false
}

// TestEveryPackageIsInALayer : A package put straight into internal/
// belongs to no layer and is checked by nothing.
func TestEveryPackageIsInALayer(t *testing.T) {
	out, err := exec.Command("go", "list", "./...").Output()
	if err != nil {
		t.Fatalf("listing the packages: %v", err)
	}
	for _, p := range strings.Fields(string(out)) {
		// Only this module's own internal packages. The test runs
		// from inside internal/, so ./... is everything below it.
		if !strings.HasPrefix(p, prefix) {
			continue
		}
		// This package itself, which is where the rule lives rather
		// than something the rule applies to.
		if strings.TrimSuffix(p, "/") == strings.TrimSuffix(prefix, "/") {
			continue
		}
		if _, ok := layerOf(p); !ok {
			t.Errorf("%s is not in base, domain or app: put it in the layer it belongs to", short(p))
		}
	}
}

// layerOf : Which layer a package sits in, and whether it is in one.
func layerOf(importPath string) (string, bool) {
	rest, found := strings.CutPrefix(importPath, prefix)
	if !found {
		return "", false
	}
	layer, _, _ := strings.Cut(rest, "/")
	if _, ok := above[layer]; !ok {
		return "", false
	}
	return layer, true
}

// allowed : Whether a package in from may import one in to.
func allowed(from, to string) bool {
	for _, ok := range above[from] {
		if ok == to {
			return true
		}
	}
	return false
}

// short : A package path without the module in front of it.
func short(importPath string) string {
	if rest, found := strings.CutPrefix(importPath, prefix); found {
		return rest
	}
	return importPath
}
