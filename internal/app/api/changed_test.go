package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	livehub "personal-assistant/internal/app/live"
)

// A write to the clients that succeeds tells them; a reading, a failed
// write and a write elsewhere do not.
func TestAWriteToTheClientsTellsThem(t *testing.T) {
	was := livehub.Settle
	livehub.Settle = time.Millisecond
	defer func() { livehub.Settle = was }()
	hub := livehub.NewHub()
	events, stop := hub.Subscribe()
	defer stop()

	for _, c := range []struct {
		method, path string
		code         int
		told         bool
	}{
		{http.MethodGet, "/v1/clients", http.StatusOK, false},
		{http.MethodDelete, "/v1/clients/abc", http.StatusNotFound, false},
		{http.MethodPost, "/v1/reminders", http.StatusOK, false},
		{http.MethodDelete, "/v1/clients/abc", http.StatusNoContent, true},
		{http.MethodPut, "/v1/personas", http.StatusOK, true},
		{http.MethodPost, "/v1/auth/login", http.StatusOK, true},
	} {
		h := clientsChanged(hub)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(c.code) }))
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(c.method, c.path, nil))
		select {
		case e := <-events:
			if !c.told {
				t.Errorf("%s %s %d: told %+v", c.method, c.path, c.code, e)
			} else if e.Area != livehub.AreaClients {
				t.Errorf("%s %s: area = %q, want clients", c.method, c.path, e.Area)
			}
		case <-time.After(50 * time.Millisecond):
			if c.told {
				t.Errorf("%s %s %d: not told", c.method, c.path, c.code)
			}
		}
	}
}
