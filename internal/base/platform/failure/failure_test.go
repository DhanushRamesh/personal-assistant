package failure_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"personal-assistant/internal/base/platform/failure"
)

func TestEveryCodeHasASentence(t *testing.T) {
	codes := []failure.Code{
		failure.Unreachable, failure.Timeout, failure.Unauthorised,
		failure.Forbidden, failure.RateLimited, failure.Unavailable,
		failure.BadRequest, failure.TooLong, failure.BadResponse,
		failure.Unexpected,
	}
	for _, c := range codes {
		if !failure.Known(c) {
			t.Errorf("%q has no sentence", c)
		}
		// Read aloud, so no code, no status number, no jargon.
		if s := failure.Sentence(c); s == "" || strings.Contains(s, "_") {
			t.Errorf("sentence for %q is not speakable: %q", c, s)
		}
	}
}

func TestAnUnknownCodeFallsBackWithoutClaimingToBeKnown(t *testing.T) {
	// Falling back matters because this runs while something is already
	// broken: an unknown code must not become a second failure.
	if got := failure.Sentence("nonsense"); got != failure.Sentence(failure.Unexpected) {
		t.Errorf("sentence = %q, want the unexpected one", got)
	}
	// Known stays false so the caller can log it rather than lose it.
	if failure.Known("nonsense") {
		t.Error("an unknown code reported itself as known")
	}
}

func TestAStatusWithNoMeaningIsUnexpected(t *testing.T) {
	if got := failure.FromHTTP(http.StatusTeapot); got != failure.Unexpected {
		t.Errorf("418 mapped to %q, want unexpected", got)
	}
}

func TestTheDetailKeepsTheServicesOwnWords(t *testing.T) {
	f := failure.FromStatus(http.StatusUnauthorized, "  INVALID_OAUTHTOKEN  ", nil)

	if f.Code != failure.Unauthorised {
		t.Errorf("code = %q, want unauthorised", f.Code)
	}
	// The sentence is chosen by the code, never by what the service said:
	// INVALID_OAUTHTOKEN read aloud tells the listener nothing.
	if strings.Contains(f.Sentence(), "OAUTH") {
		t.Errorf("the raw error leaked into the sentence: %q", f.Sentence())
	}
	if f.Detail != "INVALID_OAUTHTOKEN" {
		t.Errorf("detail = %q, want it trimmed but otherwise exact", f.Detail)
	}
	if !strings.Contains(f.Full(), "401") {
		t.Errorf("full = %q, want the status in it", f.Full())
	}
}

func TestFullIsEmptyWhenThereIsNothingToAdd(t *testing.T) {
	// Empty rather than a placeholder, so a caller can tell the difference
	// between a failure that explains itself and one that does not.
	if got := failure.New(failure.Unreachable, "", nil).Full(); got != "" {
		t.Errorf("full = %q, want empty", got)
	}
}

func TestTheCauseStaysReachable(t *testing.T) {
	f := failure.New(failure.Timeout, "took too long", context.DeadlineExceeded)

	if !errors.Is(f, context.DeadlineExceeded) {
		t.Error("errors.Is cannot see through the failure to its cause")
	}
}

// TestWhatIsSaidAndWhatIsKeptAreDifferent : Every combination of the
// two, because the split is the whole point of this package.
//
// Sentence is what somebody hears and never varies with the detail.
// Full is what the log gets and must not be empty just because half
// of it is missing: a failure with a status and no detail still says
// which status, and one with neither still says which kind it was.
func TestWhatIsSaidAndWhatIsKeptAreDifferent(t *testing.T) {
	for _, c := range []struct {
		what         string
		err          *failure.Error
		full, errors string
	}{
		{"both", failure.FromStatus(503, "dial tcp 127.0.0.1:3306: i/o timeout", nil),
			"dial tcp 127.0.0.1:3306: i/o timeout (HTTP 503)",
			"unavailable: dial tcp 127.0.0.1:3306: i/o timeout (HTTP 503)"},
		{"detail only", failure.New(failure.Timeout, "context deadline exceeded", nil),
			"context deadline exceeded", "timeout: context deadline exceeded"},
		{"status only", failure.FromStatus(500, "", nil),
			"HTTP 500", "unexpected: HTTP 500"},
		{"neither", failure.New(failure.Unexpected, "", nil),
			"", "unexpected"},
	} {
		t.Run(c.what, func(t *testing.T) {
			if got := c.err.Full(); got != c.full {
				t.Errorf("Full() = %q, want %q", got, c.full)
			}
			if got := c.err.Error(); got != c.errors {
				t.Errorf("Error() = %q, want %q", got, c.errors)
			}
			// Whatever is kept, what is said is a whole sentence
			// written for a person.
			said := c.err.Sentence()
			if said == "" {
				t.Error("Sentence() is empty, so there is nothing to say")
			}
			if strings.Contains(said, "HTTP") || strings.Contains(said, c.err.Detail) && c.err.Detail != "" {
				t.Errorf("Sentence() = %q, want it free of the machinery in %q", said, c.err.Full())
			}
		})
	}
}

// TestTheCauseIsStillReachable : Wrapping must not hide what went
// wrong from errors.Is, or a caller cannot tell a cancelled context
// from a real failure.
func TestTheCauseIsStillReachable(t *testing.T) {
	cause := errors.New("the socket closed")
	err := failure.New(failure.Unreachable, "gateway", cause)

	if !errors.Is(err, cause) {
		t.Error("errors.Is cannot reach the cause through the failure")
	}
}
