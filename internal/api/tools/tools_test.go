package tools_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DhanushRamesh/personal-assistant/internal/api/apitest"
	"github.com/DhanushRamesh/personal-assistant/internal/api/tools"
	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
)

// registry : A few tools across two domains, so grouping has something to
// group and the counting has something to count.
func registry(t *testing.T) *tool.Registry {
	t.Helper()
	r, err := tool.NewRegistry()
	if err != nil {
		t.Fatalf("building a registry: %v", err)
	}
	for _, spec := range []struct {
		name, domain string
		writes       bool
	}{
		{"reminder_list", "reminder", false},
		{"reminder_set", "reminder", true},
		{"mail_inbox", "mail", false},
	} {
		if err := r.Add(tool.Tool{
			Name: spec.name, Domain: spec.domain,
			Purpose: "does " + spec.name, UseWhen: "when asked",
			Writes:   spec.writes,
			Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
			Run: func(context.Context, tool.Invocation) tool.Result {
				return tool.Result{}
			},
		}); err != nil {
			t.Fatalf("adding %s: %v", spec.name, err)
		}
	}
	return r
}

func list(t *testing.T, e *apitest.Env) tools.ListResponse {
	t.Helper()
	rec := e.Do(t, http.MethodGet, "/v1/tools", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var out tools.ListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("reply unreadable: %v", err)
	}
	return out
}

// A server with no tools answers with an empty list rather than failing.
func TestNoToolsIsAnEmptyList(t *testing.T) {
	out := list(t, apitest.NewWith(t, apitest.Options{}))
	if out.Total != 0 || len(out.Modules) != 0 {
		t.Fatalf("expected nothing, got %d tools in %d modules", out.Total, len(out.Modules))
	}
}

func TestToolsAreGroupedByDomain(t *testing.T) {
	e := apitest.NewWith(t, apitest.Options{Tools: registry(t)})
	out := list(t, e)

	if out.Total < 3 {
		t.Fatalf("total = %d, want at least the 3 added", out.Total)
	}

	got := map[string][]string{}
	for _, m := range out.Modules {
		for _, x := range m.Tools {
			got[m.Domain] = append(got[m.Domain], x.Name)
		}
	}
	if len(got["reminder"]) != 2 {
		t.Fatalf("reminder domain = %v, want two tools", got["reminder"])
	}
	if len(got["mail"]) != 1 {
		t.Fatalf("mail domain = %v, want one tool", got["mail"])
	}
}

// Whether a tool changes anything is the one property worth seeing at a
// glance, and it is the one a name does not always give away.
func TestWritingToolsAreMarked(t *testing.T) {
	e := apitest.NewWith(t, apitest.Options{Tools: registry(t)})
	for _, m := range list(t, e).Modules {
		for _, x := range m.Tools {
			want := x.Name == "reminder_set"
			if x.Writes != want {
				t.Errorf("%s writes = %v, want %v", x.Name, x.Writes, want)
			}
		}
	}
}

// Domains are listed in a stable order, and tools within them too, so the
// screen does not reshuffle itself between refreshes.
func TestTheOrderIsStable(t *testing.T) {
	e := apitest.NewWith(t, apitest.Options{Tools: registry(t)})
	first, second := list(t, e), list(t, e)

	flat := func(r tools.ListResponse) []string {
		var out []string
		for _, m := range r.Modules {
			for _, x := range m.Tools {
				out = append(out, m.Domain+"/"+x.Name)
			}
		}
		return out
	}
	a, b := flat(first), flat(second)
	if len(a) != len(b) {
		t.Fatalf("different lengths: %d then %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("order changed at %d: %s then %s", i, a[i], b[i])
		}
	}
}

func TestToolsNeedAToken(t *testing.T) {
	e := apitest.NewWith(t, apitest.Options{Tools: registry(t)})
	rec := e.Serve(httptest.NewRequest(http.MethodGet, "/v1/tools", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", rec.Code)
	}
}
