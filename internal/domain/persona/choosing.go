package persona

import (
	"regexp"
	"strconv"
	"strings"
)

// spokenNumbers : The options as they are said, by their number.
var spokenNumbers = map[string]int{
	"one": 1, "two": 2, "three": 3, "four": 4, "five": 5, "six": 6, "seven": 7, "eight": 8, "nine": 9,
	"first": 1, "second": 2, "third": 3, "fourth": 4, "fifth": 5, "sixth": 6, "seventh": 7, "eighth": 8, "ninth": 9,
}

// choosing : A bare choice of an option, and nothing else: "3", "option
// three", "the third one", "number 2, please".
var choosing = regexp.MustCompile(`^(?:(?:go with|take|choose|pick) )?(?:the )?(?:option |number |choice )?` +
	`([1-9]|one|two|three|four|five|six|seven|eight|nine|first|second|third|fourth|fifth|sixth|seventh|eighth|ninth)` +
	`(?: one| option)?(?: please| sir)*$`)

// Chooses : The option a bare answer picks, or 0 when it picks none,
// the assistant's name and a "hey" around it aside.
//
// Owner, 20 October 2026, typing "3" under "I'd go with option one ...
// Shall I tell it to choose option one?": the model typed "1", the option
// it had suggested. A number on its own says nothing of what it is the
// number of; the question it answers does, and the runner hands it over
// with the number made plain.
func Chooses(said, name string) int {
	s := strings.ToLower(said)
	s = regexp.MustCompile(`[^a-z0-9' ]+`).ReplaceAllString(s, " ")
	s = strings.Join(strings.Fields(s), " ")
	s = strings.TrimPrefix(s, "hey ")
	if n := strings.ToLower(strings.TrimSpace(name)); n != "" {
		s = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(s, n+" "), " "+n))
	}
	m := choosing.FindStringSubmatch(s)
	if m == nil {
		return 0
	}
	if n, err := strconv.Atoi(m[1]); err == nil {
		return n
	}
	return spokenNumbers[m[1]]
}
