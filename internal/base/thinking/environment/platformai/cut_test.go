package platformai

import (
	"errors"
	"fmt"
	"net"
	"testing"
)

// A reply cut off at the budget is not a finished reply. Until 19
// October 2026 the finish reason was read only when nothing came back,
// so a reply that hit the three thousand tokens with text in it, or a
// tool call whose arguments stopped mid-JSON, passed as normal.
func TestACutOffReplySaysSo(t *testing.T) {
	for _, finish := range []string{"maxtokens", "max_tokens", "MaxTokens", " length "} {
		if !Cut(finish) {
			t.Errorf("%q was not taken as cut off", finish)
		}
	}
	for _, finish := range []string{"stop", "tool_calls", "end_turn", ""} {
		if Cut(finish) {
			t.Errorf("%q was taken as cut off", finish)
		}
	}
	if !errors.Is(ErrCut, ErrCut) {
		t.Error("ErrCut is not itself")
	}
}

// A name that would not resolve is asked again, because nothing was
// asked the first time. "server misbehaving" from the local stub
// happened 36 times in one log and reached him as "I am having some
// difficulty reaching the server, sir".
func TestANameThatWouldNotResolveIsAskedAgain(t *testing.T) {
	temporary := &net.DNSError{Err: "server misbehaving", Name: "accounts.zoho.com", Server: "127.0.0.53:53"}
	if !nameless(temporary) {
		t.Error("a resolver that misbehaved was not taken as worth asking again")
	}
	if !nameless(fmt.Errorf("getting a token: %w", temporary)) {
		t.Error("a wrapped resolver failure was not recognised")
	}

	// A name that genuinely does not exist is a misconfiguration.
	// Asking twice turns one wrong answer into two.
	missing := &net.DNSError{Err: "no such host", Name: "nope.zoho.com", IsNotFound: true}
	if nameless(missing) {
		t.Error("a name that does not exist was asked for again")
	}
	if nameless(errors.New("something else entirely")) {
		t.Error("an unrelated failure was taken as a name that would not resolve")
	}
}
