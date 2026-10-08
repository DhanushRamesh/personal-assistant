// Package speech : Remembers that an utterance was cut off before it ended.
//
// Home Assistant stops listening after fifteen seconds, whatever is still
// being said. The limit is a default inside its own voice activity
// detection, it is never passed when the detector is built, and it is not
// reachable from any setting: assist_pipeline/vad.py gives timeout_seconds a
// default of 15.0, and assist_pipeline/pipeline.py constructs the segmenter
// with silence_seconds alone. Whether it fired is thrown away inside the
// function and never reaches the conversation agent.
//
// So the fact has to come the other way round. The speech-to-text bridge
// knows how long the recording was, because it is the thing that sends it,
// and it says so here before it hands the words back to Home Assistant. By
// the time those words arrive as a question, this already knows they are the
// first part of a sentence rather than the whole of one.
//
// Matched on the text rather than on a time or an identifier. The same
// string travels from the bridge through Home Assistant and back, so it
// matches exactly, and it needs no clock agreement between two processes and
// no identifier that Ollama's wire format has nowhere to carry.
package speech

import (
	"strings"
	"sync"
	"time"
)

const (
	// Remember : How long a report is worth matching against.
	//
	// The question follows the recording almost immediately -- Home
	// Assistant has only to hand it on -- so this is generous. Long enough
	// that a slow turn still matches, short enough that saying the same
	// sentence twice in an afternoon does not inherit the first one's
	// report.
	Remember = 2 * time.Minute

	// Most : How many reports are kept at once.
	//
	// A ceiling rather than a size. Reports expire on their own; this is
	// only so that a bridge gone wrong cannot grow the process.
	Most = 32
)

// Cut : The utterances that were still being spoken when the recording
// stopped.
//
// Safe for use by several callers at once: the bridge writes from its own
// request and chats read from theirs.
type Cut struct {
	mu   sync.Mutex
	seen map[string]report
	now  func() time.Time
}

// report : One cut-off utterance.
type report struct {
	seconds float64
	at      time.Time
}

// New : An empty store.
func New() *Cut {
	return &Cut{seen: make(map[string]report), now: time.Now}
}

// Note : Records that this text was all that was captured before the
// recording was stopped.
//
// Empty text is ignored. A recording can time out having caught no words at
// all, and matching every future empty question against it would mark
// unrelated turns as cut off.
func (c *Cut) Note(text string, seconds float64) {
	key := normalise(text)
	if key == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.forget()
	if len(c.seen) >= Most {
		return
	}
	c.seen[key] = report{seconds: seconds, at: c.now()}
}

// Was : Whether this question is the part of a sentence that was captured
// before the recording stopped, and how long that recording ran.
//
// Reading does not consume the report. Home Assistant can ask the same
// question twice -- a retry, or a chat superseded and started again -- and
// the second attempt was cut off exactly as much as the first.
func (c *Cut) Was(text string) (float64, bool) {
	key := normalise(text)
	if key == "" {
		return 0, false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.forget()
	r, ok := c.seen[key]
	return r.seconds, ok
}

// forget : Drops reports too old to match. Called with the lock held.
func (c *Cut) forget() {
	cutoff := c.now().Add(-Remember)
	for k, r := range c.seen {
		if r.at.Before(cutoff) {
			delete(c.seen, k)
		}
	}
}

// normalise : The form two copies of the same utterance agree on.
//
// Only surrounding space and letter case. Not punctuation, and nothing
// language-aware: the two strings being compared are the same transcript
// carried by two different hops, so they differ in whitespace at most, and a
// cleverer comparison would only find matches that are not there.
func normalise(text string) string {
	return strings.ToLower(strings.TrimSpace(text))
}
