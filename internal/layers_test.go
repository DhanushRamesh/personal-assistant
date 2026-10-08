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
