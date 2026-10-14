package profile_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"personal-assistant/internal/app/api/apitest"
	"personal-assistant/internal/app/api/profile"
	"personal-assistant/internal/domain/memory/inmemory"
)

// The description can be written by hand at the length the server writes
// it, and one too long is refused as too long, not as a fault.
func TestTheDescriptionIsWrittenByHand(t *testing.T) {
	e := apitest.NewWith(t, apitest.Options{Memories: inmemory.New()})
	long := strings.Repeat("a", 6000)
	body, _ := json.Marshal(map[string]string{"body": long})

	rec := e.Do(t, http.MethodPut, "/v1/profile", string(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("writing 6,000 characters by hand: status %d: %s", rec.Code, rec.Body.String())
	}
	var got profile.Response
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.Body != long {
		t.Fatalf("read back %d characters, err %v", len(got.Body), err)
	}

	edited, _ := json.Marshal(map[string]string{"body": long + " and more"})
	if rec := e.Do(t, http.MethodPut, "/v1/profile", string(edited)); rec.Code != http.StatusOK {
		t.Errorf("editing it: status %d: %s", rec.Code, rec.Body.String())
	}

	tooLong, _ := json.Marshal(map[string]string{"body": strings.Repeat("b", 9000)})
	if rec := e.Do(t, http.MethodPut, "/v1/profile", string(tooLong)); rec.Code != http.StatusBadRequest {
		t.Errorf("9,000 characters: status %d, want 400: %s", rec.Code, rec.Body.String())
	}

	if rec := e.Do(t, http.MethodPut, "/v1/profile", `{"body":""}`); rec.Code != http.StatusOK {
		t.Errorf("clearing it: status %d", rec.Code)
	}
	rec = e.Do(t, http.MethodGet, "/v1/profile", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.Body != "" {
		t.Errorf("after clearing, read %q", got.Body)
	}
}
