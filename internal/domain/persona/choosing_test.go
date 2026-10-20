package persona

import "testing"

func TestChooses(t *testing.T) {
	for said, want := range map[string]int{
		"3": 3, "Option three.": 3, "the third one": 3, "number 2, please": 2, "hey Nova, 1": 1, "go with option 4": 4, "Two.": 2,
		"yes": 0, "3 more tests": 0, "what is option 3?": 0, "": 0, "10": 0,
	} {
		if got := Chooses(said, "Nova"); got != want {
			t.Errorf("Chooses(%q) = %d, want %d", said, got, want)
		}
	}
}
