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
	// Draft : The answer this request produced, where that answer was
	// then thrown away and sent back to look at the tools.
	//
	// Kept because nothing kept it. Whether a second look is owed is a
	// judgement about an answer, and for every send-back before 18
	// October 2026 the answer being judged is gone -- 642 of them, with
	// only the question left. See internal/app/doubting, which is
	// measurable on the question alone for exactly that reason and
	// would rather not be.
	Draft string `json:"draft,omitempty"`
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

// Drafted : Keeps the answer request i produced, where that answer is
// about to be thrown away and sent back to look.
//
// Written whatever the request ended as, because the ending is recorded
// at the moment the answer arrives and the decision to send it back
// comes after. An i out of range is ignored.
func (t *Chat) Drafted(i int, answer string) {
	if i < 0 || i >= len(t.Calls) {
		return
	}
	t.Calls[i].Draft = answer
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
