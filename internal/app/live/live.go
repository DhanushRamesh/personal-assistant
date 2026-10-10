// Package live : What clients are told the moment it happens, over their
// open connection rather than when they next ask.
//
// One hub per process. Sources publish events into it; each connected
// client subscribes and is sent the latest event of every kind first, so
// a client that has just connected knows where things stand without
// waiting for the next change. Notices are the other sort of event: they
// say that something happened, are sent once and are not remembered, and
// may be meant for one user only.
package live

import (
	"sync"
	"time"
)

// Kind : What an event is about.
type Kind string

// SatelliteState : The voice satellite's state in Home Assistant: idle,
// listening, processing, responding, or unknown while Home Assistant
// cannot be heard.
const SatelliteState Kind = "satellite"

// ConversationChanged : A notice that a conversation has something new in
// it -- a turn started or ended, or a new title -- whichever client asked.
const ConversationChanged Kind = "conversation"

// Event : One thing a client is told.
type Event struct {
	Kind  Kind   `json:"type"`
	State string `json:"state,omitempty"`
	// ConversationID : The conversation a notice is about.
	ConversationID string    `json:"conversation_id,omitempty"`
	At             time.Time `json:"at"`
	// UserID : Who a notice is for. Empty is everyone.
	UserID string `json:"-"`
}

// For : Whether a subscriber signed in as userID is to be sent the event.
func (e Event) For(userID string) bool {
	return e.UserID == "" || e.UserID == userID
}

// buffer : How many events a subscriber may fall behind by before it is
// dropped. A client that cannot keep up is disconnected, never allowed to
// hold up the others; reconnecting starts it again from the latest state.
const buffer = 32

// Hub : Fans events out to subscribers and remembers the latest of each
// kind. Safe for concurrent use.
type Hub struct {
	mu     sync.Mutex
	latest map[Kind]Event
	subs   map[chan Event]struct{}
}

// NewHub : An empty hub.
func NewHub() *Hub {
	return &Hub{latest: map[Kind]Event{}, subs: map[chan Event]struct{}{}}
}

// Publish : Records e as the latest of its kind and sends it to every
// subscriber. An event identical in kind and state to the latest is not
// sent again.
func (h *Hub) Publish(e Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if last, ok := h.latest[e.Kind]; ok && last.State == e.State {
		return
	}
	h.latest[e.Kind] = e
	h.fanOut(e)
}

// Notify : Sends a notice to every subscriber, without remembering it or
// comparing it with the last: two alike are two things that happened.
func (h *Hub) Notify(e Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.fanOut(e)
}

// ConversationsChanged : Tells userID's clients that a conversation of
// theirs has something new in it.
func (h *Hub) ConversationsChanged(userID, conversationID string) {
	h.Notify(Event{Kind: ConversationChanged, ConversationID: conversationID, UserID: userID, At: time.Now().UTC()})
}

// fanOut : Sends e to every subscriber, dropping any that has fallen
// behind. The caller holds the lock.
func (h *Hub) fanOut(e Event) {
	for ch := range h.subs {
		select {
		case ch <- e:
		default:
			delete(h.subs, ch)
			close(ch)
		}
	}
}

// Subscribe : Returns a channel of events, starting with the latest of
// every kind, and the function that ends the subscription. The channel is
// closed when the subscription ends, including when the hub drops a
// subscriber that fell behind.
func (h *Hub) Subscribe() (<-chan Event, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := make(chan Event, buffer+len(h.latest))
	for _, e := range h.latest {
		ch <- e
	}
	h.subs[ch] = struct{}{}
	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if _, ok := h.subs[ch]; ok {
			delete(h.subs, ch)
			close(ch)
		}
	}
}

// Subscribers : How many are connected, for a log line or a test.
func (h *Hub) Subscribers() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}
