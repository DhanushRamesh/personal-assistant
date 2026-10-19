package search

import (
	"context"
	"errors"
	"testing"
	"time"
)

// saying : A searcher that answers with one result, or fails.
type saying struct {
	name string
	err  error
	asks int
}

func (s *saying) Where() string { return s.name }
func (s *saying) Search(context.Context, Ask) ([]Result, error) {
	s.asks++
	if s.err != nil {
		return nil, s.err
	}
	return []Result{{Title: s.name}}, nil
}

// serving : A searcher that also reads, so it can stand behind.
type serving struct{ saying }

func (s *serving) Read(context.Context, []string) ([]Page, error) {
	return []Page{{URL: s.name}}, nil
}

// TestTheOneAheadAnswersAndTheRestDoesNot : Google's search answers
// when it can.
func TestTheOneAheadAnswersAndTheRestDoesNot(t *testing.T) {
	first := &saying{name: "gemini"}
	rest := &serving{saying{name: "tinyfish"}}
	a := &Ahead{Asked: first, Rest: rest}

	got, err := a.Search(context.Background(), Ask{Query: "anything"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 || got[0].Title != "gemini" {
		t.Errorf("answered with %v, want the one asked first", got)
	}
	if rest.asks != 0 {
		t.Errorf("the service behind was asked %d times, want none", rest.asks)
	}
	if a.Where() != "gemini then tinyfish" {
		t.Errorf("Where = %q", a.Where())
	}
}

// TestASpentAllowanceCostsOneRefusalAndNoMore : The whole reason this
// is safe to put first.
//
// A free key is twenty requests a day for the project, and his own
// searching is about sixteen. On the twenty-first the answer must come
// from the service behind, and the twenty-second must not even ask.
func TestASpentAllowanceCostsOneRefusalAndNoMore(t *testing.T) {
	first := &saying{name: "gemini", err: errors.New("gemini: " + ErrSpent.Error() + " (HTTP 429)")}
	first.err = errors.Join(ErrSpent, errors.New("HTTP 429"))
	rest := &serving{saying{name: "tinyfish"}}
	now := time.Now()
	a := &Ahead{Asked: first, Rest: rest, Rested: time.Hour, Now: func() time.Time { return now }}

	for i := 0; i < 5; i++ {
		got, err := a.Search(context.Background(), Ask{Query: "anything"})
		if err != nil {
			t.Fatalf("search %d: %v", i, err)
		}
		if len(got) != 1 || got[0].Title != "tinyfish" {
			t.Fatalf("search %d answered with %v, want the service behind", i, got)
		}
	}
	if first.asks != 1 {
		t.Errorf("the spent one was asked %d times, want once and then left alone", first.asks)
	}
	if rest.asks != 5 {
		t.Errorf("the service behind answered %d of 5", rest.asks)
	}

	// An hour later it is tried again, since a daily allowance comes
	// back and nothing here knows when.
	now = now.Add(time.Hour + time.Minute)
	if _, err := a.Search(context.Background(), Ask{Query: "anything"}); err != nil {
		t.Fatalf("after resting: %v", err)
	}
	if first.asks != 2 {
		t.Errorf("the spent one was asked %d times, want it tried again after resting", first.asks)
	}
}

// TestReadingNeverGoesToTheOneAhead : It answers questions; it does not
// fetch pages.
func TestReadingNeverGoesToTheOneAhead(t *testing.T) {
	first := &saying{name: "gemini"}
	rest := &serving{saying{name: "tinyfish"}}
	a := &Ahead{Asked: first, Rest: rest}

	got, err := a.Read(context.Background(), []string{"https://example.com"})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != 1 || got[0].URL != "tinyfish" {
		t.Errorf("read from %v, want the service behind", got)
	}
	if first.asks != 0 {
		t.Errorf("the one ahead was asked to read %d times", first.asks)
	}
}
