package chat

import "time"

// CallEnd : How a request to the model ended.
type CallEnd string

const (
	// CallAnswered : It came back with the answer.
	CallAnswered CallEnd = "answered"
	// CallTools : It came back asking for tools.
	CallTools CallEnd = "tools"
	// CallFailed : It came back with an error.
	CallFailed CallEnd = "failed"
	// CallStopped : Its stream closed with nothing at the end of it.
	CallStopped CallEnd = "stopped"
	// CallUnused : It was begun early, beside another, and that other's
	// answer made it unnecessary.
	CallUnused CallEnd = "unused"
)

// Call : One request to the model while the answer was made, and when each
// part of it happened.
//
// The messages a turn writes say what the model asked for, not when it
// was asked or how long it took to reply; nothing else keeps the moment a
// request went out.
type Call struct {
	// AskedAt : When the request went out.
	AskedAt time.Time `json:"asked_at"`
	// HeardAt : When the first thing came back, or nil if nothing did.
	HeardAt *time.Time `json:"heard_at,omitempty"`
	// DoneAt : When it ended, or nil if it never did.
	DoneAt *time.Time `json:"done_at,omitempty"`
	// Ended : How it ended. Empty while it runs.
	Ended CallEnd `json:"ended,omitempty"`
	// Early : Whether it was begun before the request in front of it had
	// finished, in case that one's answer was sent back.
	Early bool `json:"early,omitempty"`
}

// Asking : Records that a request to the model has gone out, returning
// its position for Heard and Ended.
func (t *Chat) Asking(at time.Time, early bool) int {
	t.Calls = append(t.Calls, Call{AskedAt: at.UTC(), Early: early})
	return len(t.Calls) - 1
}

// Heard : Records that the first thing has come back for request i, if it
// has not been recorded already. An i out of range is ignored.
func (t *Chat) Heard(i int, at time.Time) {
	if i < 0 || i >= len(t.Calls) || t.Calls[i].HeardAt != nil {
		return
	}
	when := at.UTC()
	t.Calls[i].HeardAt = &when
}

// Ended : Records how request i ended, once. An i out of range is ignored.
func (t *Chat) Ended(i int, how CallEnd, at time.Time) {
	if i < 0 || i >= len(t.Calls) || t.Calls[i].Ended != "" {
		return
	}
	when := at.UTC()
	t.Calls[i].DoneAt = &when
	t.Calls[i].Ended = how
}
