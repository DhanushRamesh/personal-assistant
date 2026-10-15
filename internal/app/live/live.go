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
	"context"
	"strings"
	"sync"
	"time"
)

// Kind : What an event is about.
type Kind string

// SatelliteState : The voice satellite's state in Home Assistant: idle,
// listening, processing, responding, or unknown while Home Assistant
// cannot be heard.
const SatelliteState Kind = "satellite"

// The states a voice satellite reports. They are Home Assistant's own
// words, not this server's: the laptop's satellite entity says them and a
// screen matches on them, so they are spelt the way they arrive.
//
// Written down here because a second satellite publishes them too. HAwake
// on the phone has no entity in Home Assistant, so what it is doing is
// said by the server rather than read from Home Assistant -- and the two
// sources have to agree on the words or a screen sees a state it has no
// picture for.
const (
	Idle       = "idle"
	Listening  = "listening"
	Processing = "processing"
	Responding = "responding"
)

// ConversationChanged : A notice that a conversation has something new in
// it -- a turn started or ended, or a new title -- whichever client asked.
const ConversationChanged Kind = "conversation"

// Changed : A notice that some of a person's data changed -- which
// area, and nothing of what it holds -- so a client showing it reads it
// again. Owner, 13 October 2026: *"all the data should be reactive from
// the server; any change in the server should be immediately notified
// to the client."* A notice and never the data: each client reads what
// it shows through the endpoints it already uses, so nothing is sent to
// a client that is not looking.
const Changed Kind = "changed"

// The areas a Changed notice may name.
const (
	AreaReminders = "reminders"
	AreaRules     = "rules"
	AreaEvents    = "events"
	AreaMemories  = "memories"
	AreaProfile   = "profile"
	AreaRemarks   = "remarks"
	AreaClients   = "clients"
	AreaGoogle    = "google"
)

// Hearing : A notice of the words heard so far while somebody speaks, as
// the speech bridge recognises them -- rewritten as it hears more, and
// final once the turn has ended. Shown, never stored.
const Hearing Kind = "hearing"

// Answered : A notice of a spoken turn something other than the
// assistant answered -- Home Assistant matching one of its own sentences --
// with what was heard and what was said back. Shown by the core, never
// stored: such a turn is not part of the conversation (owner, 15 October
// 2026: *"they should not go to conversation"*).
const Answered Kind = "answered"

// Event : One thing a client is told.
type Event struct {
	Kind  Kind   `json:"type"`
	State string `json:"state,omitempty"`
	// ConversationID : The conversation a notice is about.
	ConversationID string `json:"conversation_id,omitempty"`
	// Text, Final : The words of a hearing notice, and whether they are
	// the last of the turn.
	Text  string `json:"text,omitempty"`
	Final bool   `json:"final,omitempty"`
	// Wait : For a hearing notice, how many seconds the turn will still
	// wait for another word before it is ended, as of when the bridge
	// sent it; absent when it will not be ended for quiet.
	Wait *float64 `json:"wait,omitempty"`
	// Said : For an answered notice, what was said back; Text holds what was heard.
	Said string `json:"said,omitempty"`
	// Area : For a changed notice, what changed: one of the Area names.
	Area string    `json:"area,omitempty"`
	At   time.Time `json:"at"`
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
	// pending, flush : The changed areas not yet sent, and the timer that sends them.
	pending map[areaKey]struct{}
	flush   *time.Timer
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

// Latest : The latest event of a kind, and whether there has been one.
func (h *Hub) Latest(kind Kind) (Event, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	e, ok := h.latest[kind]
	return e, ok
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

// Elsewhere : Tells userID's clients of a spoken turn answered elsewhere,
// for the core to show; nothing is written.
func (h *Hub) Elsewhere(_ context.Context, userID, heard, said string) {
	heard, said = strings.TrimSpace(heard), strings.TrimSpace(said)
	if userID == "" || said == "" {
		return
	}
	h.Notify(Event{Kind: Answered, Text: heard, Said: said, UserID: userID, At: time.Now().UTC()})
}

// AreaChanged : Tells userID's clients that an area of their data
// changed; an empty userID tells everybody's, for a write that knew only
// an id. The notice holds no data, so telling a client that has nothing
// there costs it one reading.
func (h *Hub) AreaChanged(userID string, areas ...string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.pending == nil {
		h.pending = map[areaKey]struct{}{}
	}
	for _, a := range areas {
		h.pending[areaKey{userID, a}] = struct{}{}
	}
	if h.flush == nil {
		h.flush = time.AfterFunc(Settle, h.sendAreas)
	}
}

// Settle : How long notices of changed areas are gathered before they
// are sent, so a burst of writes -- a rule arming a dozen reminders --
// is one notice per area, and never fills a subscriber's buffer.
var Settle = 200 * time.Millisecond

// areaKey : An area changed, and whose.
type areaKey struct{ userID, area string }

// sendAreas : Sends the notices gathered, one per user and area.
func (h *Hub) sendAreas() {
	h.mu.Lock()
	defer h.mu.Unlock()
	at := time.Now().UTC()
	for k := range h.pending {
		h.fanOut(Event{Kind: Changed, Area: k.area, UserID: k.userID, At: at})
	}
	h.pending, h.flush = nil, nil
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
