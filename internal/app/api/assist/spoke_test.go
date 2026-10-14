package assist_test

import (
	"context"
	"net/http"
	"personal-assistant/internal/app/runner"
	"testing"
	"time"

	"personal-assistant/internal/app/api/apitest"
	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/base/thinking/environment"
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

// TestTheWaitEndsWhenTheSpeakerStarts : the owner's definition of how
// long a voice turn took -- when he stopped waiting, not when the turn
// finished.
//
// It used to end at the first word, because the answer was spoken as
// it was written. It no longer is: a spoken answer is held whole so
// the server can read it before anybody hears it, and the wait now
// ends when the holding does.
//
// A lingerer inside [runner.Holding] is held to the end, so the wait
// is the turn. One past it is let go early, and the wait is the cap
// rather than the turn -- which is the whole reason the cap exists.
func TestTheWaitEndsWhenTheSpeakerStarts(t *testing.T) {
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
	// Held: this answer finishes well inside the cap, so nothing is
	// said until it is whole and the wait is the turn.
	spoke := time.Duration(*last.SpokeAfterMS) * time.Millisecond
	if spoke < linger {
		t.Errorf("an answer inside the holding time was spoken before it was whole: "+
			"spoke after %s of a turn that lingered %s", spoke, linger)
	}
	if spoke > whole+time.Second {
		t.Errorf("the wait outran the turn: spoke after %s of %s", spoke, whole)
	}

	// Only the last chunk carries it, or a reader would see the
	// number change under it as the answer arrived.
	for i, c := range chunks[:len(chunks)-1] {
		if c.SpokeAfterMS != nil {
			t.Errorf("chunk %d reports a wait before the turn ended: %d ms", i, *c.SpokeAfterMS)
		}
	}
}

// TestASlowAnswerIsLetGoRatherThanHeldForEver : the cap, and the
// reason there is one.
//
// Holding a spoken answer until it is whole is what lets the server
// read it first, and it is only cheap on average. Measured over 725
// real turns the head start streaming bought was a median of 0.14
// seconds; 54 of them were over two seconds and the worst was 17.95,
// speaking at 7.3s of a turn that ran to 25.2. Home Assistant ends a
// turn at thirty.
//
// So an answer that takes longer than [runner.Holding] to finish is
// let go and spoken from there, exactly as it was before. This turn
// lingers well past the cap and must be heard long before it ends.
func TestASlowAnswerIsLetGoRatherThanHeldForEver(t *testing.T) {
	linger := runner.Holding + 2*time.Second
	e := apitest.NewWith(t, apitest.Options{Environment: lateFinisher{linger: linger}})
	satellite := e.LoginOn(t, "the satellite", string(chat.ChannelVoice))

	began := time.Now()
	rec := e.AsBody(t, satellite.Token, http.MethodPost, "/api/chat", `{
		"model": "assistant",
		"messages": [{"role": "user", "content": "good evening"}]
	}`)
	whole := time.Since(began)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	chunks := decodeChunks(t, rec.Body.String())
	last := chunks[len(chunks)-1]
	if last.SpokeAfterMS == nil {
		t.Fatal("the wait was not reported at all")
	}
	spoke := time.Duration(*last.SpokeAfterMS) * time.Millisecond
	if spoke >= linger {
		t.Errorf("a slow answer was held to the end: spoke after %s of a turn that lingered %s (%s whole)",
			spoke, linger, whole)
	}
	t.Logf("held %s, then let go; the turn ran %s", spoke.Round(time.Millisecond), whole.Round(time.Millisecond))
}
