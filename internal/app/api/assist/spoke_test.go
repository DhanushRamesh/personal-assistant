package assist_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/app/api/apitest"
	"github.com/DhanushRamesh/personal-assistant/internal/base/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/base/environment"
)

// lateFinisher : Says something at once and then takes its time
// finishing, which is the shape of every streamed voice turn.
type lateFinisher struct{ linger time.Duration }

func (lateFinisher) Name() string { return "late-finisher" }

func (p lateFinisher) Run(ctx context.Context, req environment.Request) (<-chan environment.Message, error) {
	ch := make(chan environment.Message, 2)
	go func() {
		defer close(ch)
		if req.Purpose != environment.PurposeChat {
			ch <- environment.Final("an answer")
			return
		}
		ch <- environment.Partial("Good evening, sir.")
		select {
		case <-time.After(p.linger):
		case <-ctx.Done():
			return
		}
		ch <- environment.Final("Good evening, sir.")
	}()
	return ch, nil
}

// TestTheWaitEndsAtTheFirstWord : The owner's definition of how long a
// voice turn took. The answer is spoken as it is written, so the person
// stops waiting when the speaker starts, and the turn runs on for
// seconds afterwards. Timing the whole turn describes a wait nobody had.
func TestTheWaitEndsAtTheFirstWord(t *testing.T) {
	const linger = 300 * time.Millisecond
	e := apitest.NewWith(t, apitest.Options{Environment: lateFinisher{linger: linger}})
	// Pieces of the answer go to a voice turn only, and the wait this
	// measures is a person listening to a speaker.
	satellite := e.LoginOn(t, "the satellite", string(chat.ChannelVoice))

	began := time.Now()
	rec := e.AsBody(t, satellite.Token, http.MethodPost, "/api/chat", `{
		"model": "assistant",
		"messages": [{"role": "user", "content": "hi how are you"}],
		"stream": true
	}`)
	whole := time.Since(began)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	chunks := decodeChunks(t, rec.Body.String())
	if len(chunks) == 0 {
		t.Fatal("no chunks")
	}

	last := chunks[len(chunks)-1]
	if last.SpokeAfterMS == nil {
		t.Fatal("the wait was not reported at all")
	}
	if spoke := time.Duration(*last.SpokeAfterMS) * time.Millisecond; spoke >= linger {
		t.Errorf("the wait was timed to the end of the turn, not the first word: "+
			"spoke after %s, and the turn lingered %s of its %s", spoke, linger, whole)
	}

	// Only the last chunk carries it, or a reader would see the
	// number change under it as the answer arrived.
	for i, c := range chunks[:len(chunks)-1] {
		if c.SpokeAfterMS != nil {
			t.Errorf("chunk %d reports a wait before the turn ended: %d ms", i, *c.SpokeAfterMS)
		}
	}
}
