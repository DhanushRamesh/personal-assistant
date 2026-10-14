package google

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// With nowhere to send the browser, the callback leaves it on a page of
// its own saying how it went, never raw JSON.
func TestASignInFinishesOnAPageSayingHowItWent(t *testing.T) {
	h := New(nil, nil, "")
	for _, c := range []struct {
		failure string
		code    int
		says    string
	}{
		{"", http.StatusOK, "You can close this tab"},
		{"access_denied", http.StatusBadRequest, "permission was not given"},
		{"<script>", http.StatusBadRequest, "&lt;script&gt;"},
	} {
		rec := httptest.NewRecorder()
		h.back(rec, httptest.NewRequest(http.MethodGet, "/v1/google/callback", nil), c.failure)
		if rec.Code != c.code {
			t.Errorf("%q: status = %d, want %d", c.failure, rec.Code, c.code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Errorf("%q: content type = %q, want a page", c.failure, ct)
		}
		if body := rec.Body.String(); !strings.Contains(body, c.says) || strings.Contains(body, "<script>") {
			t.Errorf("%q: page = %s", c.failure, body)
		}
	}
}

// Told whose connection changed, and only of a person.
func TestAChangeIsToldForAPerson(t *testing.T) {
	var told []string
	h := New(nil, nil, "").Tells(func(userID string) { told = append(told, userID) })
	h.tell("")
	h.tell("u1")
	if len(told) != 1 || told[0] != "u1" {
		t.Errorf("told %v, want [u1]", told)
	}
}
