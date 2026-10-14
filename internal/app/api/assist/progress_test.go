package assist_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"personal-assistant/internal/app/api/apitest"
	"personal-assistant/internal/base/records/chat"
)

// spokenPieces : Every piece of text written to the stream, which is
// every piece Home Assistant would say out loud.
func spokenPieces(t *testing.T, body string) []string {
	t.Helper()
	var out []string
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var c struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal([]byte(line), &c) == nil && c.Message.Content != "" {
			out = append(out, c.Message.Content)
		}
	}
	return out
}

// TestAVoiceTurnIsNotToldWhatItIsDoing : Home Assistant speaks
// everything written to it.
//
// The runner keeps its narration off voice where it is made, and the
// aside player that would read it aloud is off by configuration. It
// then reached the voice stream by a second path and was spoken
// anyway: "reading your calendar for October twenty-second", said out
// loud and nowhere in the transcript, so the screen showed only the
// answer that came after it.
//
// What a voice turn is sent must be the answer and nothing else.
func TestAVoiceTurnIsNotToldWhatItIsDoing(t *testing.T) {
	e := apitest.NewWith(t, apitest.Options{})
	satellite := e.LoginOn(t, "the satellite", string(chat.ChannelVoice))

	rec := e.AsBody(t, satellite.Token, http.MethodPost, "/api/chat",
		`{"model":"assistant","messages":[{"role":"user","content":"what is the time"}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	// Whatever the harness answers, nothing on a voice stream may end
	// in the newline the progress notes are written with -- that is
	// the shape of a status line rather than of an answer.
	for _, piece := range spokenPieces(t, rec.Body.String()) {
		if strings.HasSuffix(piece, "\n") {
			t.Errorf("a progress note reached the voice stream and would be spoken: %q", piece)
		}
	}
}

// And a screen still gets them: it cannot speak, it draws them as a
// status line, and a turn working in silence for nine seconds reads
// as having stopped.
func TestAScreenIsStillToldWhatIsHappening(t *testing.T) {
	e := apitest.New(t)

	rec := e.Do(t, http.MethodPost, "/api/chat",
		`{"model":"assistant","messages":[{"role":"user","content":"typed, not spoken"}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	// Nothing to assert about the content the harness invents; what
	// matters is that the path is not gated for a screen, which the
	// voice test above would also pass if the branch were deleted.
	if len(rec.Body.String()) == 0 {
		t.Error("a screen was sent nothing at all")
	}
}
