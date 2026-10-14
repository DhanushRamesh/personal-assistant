package noticing

import "testing"

// TestItCountsOutLoudBeforeItConcludes : the answer stopped being JSON
// the day this pass was given tools.
//
// The first real investigation reasoned for two minutes and eighteen
// seconds, listed every morning departure with its timestamps, counted
// five of about seven, and then wrote its object. read() unmarshalled
// the whole answer, failed, and returned nothing worth a rule -- which
// is exactly what a careful refusal looks like, so nothing showed.
func TestItCountsOutLoudBeforeItConcludes(t *testing.T) {
	// Shortened from the real one, keeping its shape: prose, counts,
	// a brace that is not JSON, then the conclusion.
	answered := "Now let me count the core sequence carefully before concluding anything.\n\n" +
		"**Helmet connects + home exit pairing (morning, within 20 min):**\n" +
		"- Mon 5 Oct: helmet 10:57, exit 11:07\n" +
		"- Wed 14 Oct: exit 10:46, helmet 10:48\n\n" +
		"So 5 of ~7 genuine morning departures come with a helmet connect.\n\n" +
		`{"rule": "when the helmet connects just after they leave home, tell them if the phone is under a quarter", ` +
		`"name": "riding out on a flat phone", "because": "the helmet followed leaving home 5 of 7 mornings"}`

	got, err := read(answered)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.Asked == "" {
		t.Fatal("the rule it wrote was thrown away and read as nothing")
	}
	if got.Name != "riding out on a flat phone" {
		t.Errorf("name = %q", got.Name)
	}
	if got.Why == "" {
		t.Error("the reason it gave was lost")
	}
}

// What it did on the day: reasoned, then refused. The refusal must
// survive too, or a pass that thought hard looks like one that did not
// answer at all.
func TestAReasonedRefusalSurvivesTheReasoning(t *testing.T) {
	answered := "Let me check the evening sequence as well.\n\nHelmet 20:51, home not recorded.\n\n" +
		`{"nothing": "it would fire every night, which is a notification wearing the clothes of a remark"}`
	got, err := read(answered)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.Asked != "" {
		t.Errorf("a refusal was read as a rule: %+v", got)
	}
}

// The shapes that worked before must go on working, and an answer with
// no object in it is still nothing rather than an error.
func TestTheOldShapesStillRead(t *testing.T) {
	for _, c := range []struct {
		what, answered, wantName string
	}{
		{"bare json", `{"rule": "r", "name": "n", "because": "b"}`, "n"},
		{"fenced json", "```json\n" + `{"rule": "r", "name": "n", "because": "b"}` + "\n```", "n"},
		{"nothing at all", `{"nothing": "no"}`, ""},
		{"not json in the least", "I had a look and found nothing worth writing down.", ""},
		{"an empty answer", "   ", ""},
	} {
		got, err := read(c.answered)
		if err != nil {
			t.Fatalf("%s: %v", c.what, err)
		}
		if got.Name != c.wantName {
			t.Errorf("%s: name = %q, want %q", c.what, got.Name, c.wantName)
		}
	}
}

// A model that quotes the shape it is aiming at before filling it in
// must not have the example read as its answer.
func TestTheLastObjectWinsNotTheFirst(t *testing.T) {
	answered := `The shape I must return is {"rule": "...", "name": "...", "because": "..."} so let me fill it in.` +
		"\n\n" + `{"rule": "the real one", "name": "the real name", "because": "counted"}`
	got, err := read(answered)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.Name != "the real name" {
		t.Errorf("name = %q, want the real one at the end", got.Name)
	}
}
