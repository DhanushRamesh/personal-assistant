package persona

import (
	"sort"
	"testing"
)

// TestWhatTheMannerWeighs : Printed rather than asserted, as the tool
// suite weighs the tool specs.
//
// Every one of these is in front of the model on every turn, and
// deciding which of them a worker needs means knowing what each costs.
func TestWhatTheMannerWeighs(t *testing.T) {
	jarvis, _ := Find("jarvis")
	blocks := []struct {
		name string
		text string
		who  string
	}{
		{"the manner itself", jarvis.Manner, "whoever speaks to him"},
		{"aloud", aloud, "whoever speaks to him"},
		{"briefly", briefly, "whoever speaks to him"},
		{"simply", simply, "whoever speaks to him"},
		{"written", written, "whoever writes to him"},
		{"replying", replying, "whoever speaks to him"},
		{"answering", answering, "whoever speaks to him"},
		{"listing", listing, "whoever speaks to him"},
		{"following", following, "whoever speaks to him"},
		{"honesty", honesty, "everyone"},
		{"machinery", machinery, "whoever speaks to him"},
		{"naming", naming, "the hand that resolves a name"},
		{"keeping", keeping, "the memory hand"},
		{"noticing", noticing, "whoever speaks to him"},
		{"timekeeping", timekeeping, "whoever routes a question"},
	}

	sort.SliceStable(blocks, func(i, j int) bool { return len(blocks[i].text) > len(blocks[j].text) })

	total := 0
	for _, b := range blocks {
		total += len(b.text)
	}
	t.Logf("%d blocks, %d characters, ~%d tokens\n", len(blocks), total, total/4)
	for _, b := range blocks {
		t.Logf("  %-20s %6d chars  ~%5d tokens  %3d%%   %s",
			b.name, len(b.text), len(b.text)/4, 100*len(b.text)/total, b.who)
	}

	spoken, read := len(Prompt("jarvis", "Jarvis", Spoken)), len(Prompt("jarvis", "Jarvis", Written))
	t.Logf("\n  assembled: spoken %d chars (~%d tokens), written %d (~%d)",
		spoken, spoken/4, read, read/4)
}
