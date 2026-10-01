// Package tools : Serves what the assistant can actually do.
//
// Nothing else shows this. The tools are registered in code, grouped by a
// domain nobody can see, and half of them are withheld from any given
// request by the deferring mechanism -- so the only way to know what the
// assistant can do, and what it was offered this time, has been to read
// the source.
//
// Read-only and deliberately so. A tool is not configuration: turning one
// off from a screen would mean the assistant's abilities differed from its
// code with nothing to say why.
package tools

import (
	"log/slog"
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"

	"github.com/DhanushRamesh/personal-assistant/internal/api/authn"
	"github.com/DhanushRamesh/personal-assistant/internal/api/httpx"
	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
)

// Listed : One tool, as a listing shows it.
type Listed struct {
	Name    string `json:"name"`
	Purpose string `json:"purpose"`
	// Always : Whether every request is told how to call it, or whether it
	// has to be asked about first.
	//
	// The difference is most of what makes a long tool list affordable, and
	// it is invisible everywhere else.
	Always bool `json:"always"`
	// Writes : Whether it changes anything, or only reads.
	Writes bool `json:"writes"`
}

// Module : The tools of one domain.
type Module struct {
	Domain string   `json:"domain"`
	Tools  []Listed `json:"tools"`
}

// ListResponse : Everything the assistant can do, grouped.
type ListResponse struct {
	Modules []Module `json:"modules"`
	// Total : How many tools there are altogether.
	Total int `json:"total"`
	// Always : How many of them every request carries.
	Always int `json:"always"`
}

// Handler : Serves the tool endpoints.
type Handler struct {
	httpx.Responder
	registry *tool.Registry
}

// New : Builds the handler. A nil registry lists nothing, which is what a
// server with no tools has.
func New(logger *slog.Logger, registry *tool.Registry) *Handler {
	return &Handler{Responder: httpx.Responder{Logger: logger}, registry: registry}
}

// Mount : Registers the endpoints on r, which must already require
// authentication.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/v1/tools", h.List)
}

// List : What the assistant can do, grouped by domain.
//
// Everything registered, not the subset a particular turn would be offered.
// Which tools a turn carries depends on its channel and on what the model
// has already asked about, and answering with one turn's view would make
// the same screen show different things for no visible reason.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	out := ListResponse{Modules: []Module{}}
	if h.registry == nil {
		httpx.WriteJSON(ctx, w, http.StatusOK, out)
		return
	}

	// The caller's own channel decides which tools exist for them at all:
	// a satellite is offered a different set from a typed client, and
	// listing the others would describe abilities this caller does not
	// have.
	channel := authn.Of(ctx).Client.Channel
	if !channel.Valid() {
		channel = chat.ChannelDirect
	}

	byDomain := map[string][]Listed{}
	for _, t := range h.registry.For(channel) {
		domain := t.Domain
		if domain == "" {
			domain = "other"
		}
		byDomain[domain] = append(byDomain[domain], Listed{
			Name:    t.Name,
			Purpose: t.Purpose,
			Always:  tool.Hot(t.Name),
			Writes:  t.Writes,
		})
		out.Total++
		if tool.Hot(t.Name) {
			out.Always++
		}
	}

	domains := make([]string, 0, len(byDomain))
	for d := range byDomain {
		domains = append(domains, d)
	}
	sort.Strings(domains)

	for _, d := range domains {
		tools := byDomain[d]
		sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
		out.Modules = append(out.Modules, Module{Domain: d, Tools: tools})
	}

	httpx.WriteJSON(ctx, w, http.StatusOK, out)
}
