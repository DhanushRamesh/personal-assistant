package about

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestAboutSaysTheConfiguredName(t *testing.T) {
	for _, name := range []string{"Configured", ""} {
		r := chi.NewRouter()
		New(name).Mount(r)

		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/about", nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("name %q: status %d, want 200", name, rec.Code)
		}
		var got Response
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("name %q: decoding: %v", name, err)
		}
		if got.Name != name {
			t.Errorf("name = %q, want %q", got.Name, name)
		}
	}
}
