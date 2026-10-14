// Package assist : Lets Home Assistant use the server as its conversation agent.
//
// Home Assistant reaches a language model through one of its integrations,
// and of those built into it only Ollama's asks for the address of the server
// to call. The server therefore answers in Ollama's shape — a model listing at
// /api/tags and a conversation at /api/chat — because that is the vocabulary
// Home Assistant already speaks. Nothing here runs a model.
//
//	satellite -> Home Assistant -> POST /api/chat   -> a the server chat
//	                            <- newline-delimited JSON, as the answer forms
//
// The answer is streamed for the same reason the server's own clients are given a
// stream: Home Assistant holds the connection open while a chat runs, so a
// chat that takes two minutes survives as long as it keeps saying something,
// and each thing it says can be spoken as it arrives rather than after.
package assist

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"personal-assistant/internal/base/records/store"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"

	"personal-assistant/internal/app/api/authn"
	"personal-assistant/internal/app/api/httpx"
	"personal-assistant/internal/app/asking"
	livehub "personal-assistant/internal/app/live"
	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/base/world/events"
	"personal-assistant/internal/base/world/speech"
)

const (
	// ModelName : The one model the server offers. Home Assistant asks for a
	// model by name and will not finish setting up an integration that
	// offers none, so the server presents itself as one.
	//
	// Not the assistant's name, which is configuration and changes. This is
	// the identifier Home Assistant stores against its conversation agent,
	// so changing it stops voice working until that agent is reconfigured.
	ModelName = "assistant"

	// roleUser : The author of the question in a Home Assistant request.
	roleUser = "user"
	// roleAssistant : The author of every chunk the server sends back.
	roleAssistant = "assistant"

	// keepAliveInterval : How often an empty chunk is sent while a chat is
	// thinking.
	//
	// The connection is held open for as long as the chat runs, and a chat
	// can think for minutes without producing a word. An empty chunk reads
	// as an empty delta at the far end and costs nothing, where silence
	// risks the connection being closed underneath the answer.
	keepAliveInterval = 15 * time.Second

	// maxRequestBody : The largest request accepted. Home Assistant sends
	// the conversation so far, which is larger than a bare prompt but is
	// still only text somebody spoke.
	maxRequestBody = 1 << 20
)

// Runner : The part of the chat runner that this module requires.
type Runner interface {
	// Submit : Starts running a stored chat in the background.
	Submit(t *chat.Chat) error
	// Cancel : Stops a queued or running chat, reporting whether one was
	// found.
	Cancel(id string) bool
	// CancelBecause : The same, saying why, for the transcript.
	CancelBecause(id, reason string) bool
}

// callerLeft : What the transcript says when Home Assistant hung up.
//
// It allows a turn thirty seconds and then closes the connection. Four
// turns in a row were cut that way on 29 September 2026 -- measured at
// 29.98 seconds each -- and every one was written down as "The person
// stopped this before it finished." They had not. The transcript is
// what the assistant reads back and what the owner reads back, and a
// false account of why something ended is worse than none.
const callerLeft = "This was cut short after thirty seconds: whatever asked the question stopped waiting for the answer. Nobody stopped it deliberately."

// Subscriber : Somewhere to listen for a chat's messages as they happen.
type Subscriber interface {
	// Subscribe : Returns a channel of a chat's events and a function that
	// ends the subscription.
	Subscribe(chatID string) (<-chan events.Event, func())
}

// Message : One turn of a conversation, in the shape Home Assistant sends and
// expects back.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest : The body of a conversation request.
//
// Home Assistant sends more than this — the tools it can offer, a context
// size, how long to keep a model loaded — and all of it describes running a
// model locally, which is not what happens here. Unknown fields are therefore
// accepted and ignored rather than refused, so that a future version of Home
// Assistant sending one more of them does not stop it answering.
type ChatRequest struct {
	// Model : Which model to answer as. Only ModelName exists.
	Model string `json:"model"`
	// Messages : The conversation so far, oldest first.
	Messages []Message `json:"messages"`
	// ConversationID : Which conversation to talk in. Empty joins the one this client
	// is active in, which is what Home Assistant does since it knows
	// nothing about conversations.
	//
	// Not part of Ollama's shape. It is added rather than kept on a second
	// endpoint because a caller that does know which conversation it means
	// should not have to switch the client's active conversation first and race
	// anything else using the same token.
	ConversationID string `json:"conversation_id,omitempty"`
}

// ChatChunk : One piece of an answer.
//
// A chunk carrying text has Done false; the last chunk carries no text and
// has Done true. This is what tells the caller the answer is complete.
type ChatChunk struct {
	Model     string    `json:"model"`
	CreatedAt time.Time `json:"created_at"`
	Message   Message   `json:"message"`
	// ErrorCode, ErrorDetail : Set on the final chunk when the chat failed.
	//
	// Beyond Ollama's shape, and omitted when empty, so a client that does
	// not know about them sees exactly what it saw before. What is said
	// aloud stays in Message.Content; these are for a screen.
	ErrorCode   string `json:"error_code,omitempty"`
	ErrorDetail string `json:"error_detail,omitempty"`
	// ChatID : Which chat this stream is, sent once at the start.
	//
	// Also beyond Ollama's shape and also omitted when empty. A screen
	// cannot show what a turn is doing while it does it without knowing
	// which turn to ask about, and this stream is the only thing that
	// knows. Home Assistant ignores it, and nothing spoken changes.
	ChatID string `json:"chat_id,omitempty"`
	// SpokeAfterMS : How long the person waited to hear anything,
	// milliseconds, sent once on the final chunk.
	//
	// Measured to the first chunk carrying text, not to this one. The
	// answer is spoken as it is written, so the turn is over for the
	// person listening long before it is over here, and the duration
	// of the whole turn describes a wait nobody had.
	//
	// Beyond Ollama's shape and omitted when absent, like the two
	// above. It is for a screen and for the log; nothing spoken
	// changes.
	//
	// A pointer because zero is an answer: a turn can begin inside a
	// millisecond, and that is not the same as a turn with nothing to
	// measure.
	SpokeAfterMS *int64 `json:"spoke_after_ms,omitempty"`
	Done         bool   `json:"done"`
	DoneReason   string `json:"done_reason,omitempty"`
}

// ModelsResponse : The body of a model listing.
type ModelsResponse struct {
	Models []Model `json:"models"`
}

// Model : One model in a listing.
type Model struct {
	Name       string       `json:"name"`
	Model      string       `json:"model"`
	ModifiedAt time.Time    `json:"modified_at"`
	Size       int64        `json:"size"`
	Digest     string       `json:"digest"`
	Details    ModelDetails `json:"details"`
}

// ModelDetails : What a caller is told about a model's construction. The server
// has no weights to describe, and answers only so that a listing parses.
type ModelDetails struct {
	Family            string   `json:"family"`
	Families          []string `json:"families"`
	Format            string   `json:"format"`
	ParameterSize     string   `json:"parameter_size"`
	QuantizationLevel string   `json:"quantization_level"`
}

// Notifier : Where what a turn is doing is sent: the live hub.
//
// Publish and not Notify, which is the hub's other door. Publish records
// the event as the latest of its kind, so a client that connects in the
// middle of a turn is told what is happening rather than nothing, and the
// dashboard's reading of the satellite is the current one. Notify is for
// things that happen and are gone -- two alike are two events -- and a
// state is not one of those.
type Notifier interface {
	Publish(e livehub.Event)
}

// Reporter : Where a voice turn's state is written so Home Assistant, and
// anything reading a dashboard, can see it.
type Reporter interface {
	Report(ctx context.Context, entity, state string, attrs map[string]any) error
}

// reporting : How long a state is given to reach Home Assistant.
//
// Short on purpose. This is a light on a dashboard, and a turn must never
// wait on it -- the write is made from its own goroutine and the turn
// does not look back.
const reporting = 3 * time.Second

// Handler : Serves the endpoints Home Assistant calls.
type Handler struct {
	httpx.Responder
	repo   store.Repository
	runner Runner
	asker  *asking.Service
	tell   Notifier
	shows  Reporter
	entity string
	// queued : States waiting to be written, drained in order by one
	// goroutine. See Shows for why the order has to be kept.
	queued chan string
	// turn : Which voice turn is the current one. Counted so that the
	// idle held back while the assistant is still speaking can tell
	// whether anything has started since, and say nothing if it has.
	turn atomic.Int64
	// after : How a held idle waits. Replaced in tests, which cannot
	// spend real seconds proving a delay.
	after func(time.Duration) <-chan time.Time
}

// New : Builds the handler from the store, the runner that executes chats and
// the bus that carries what they say.
func New(logger *slog.Logger, repo store.Repository, runner Runner, bus Subscriber) *Handler {
	h := &Handler{
		Responder: httpx.Responder{Logger: logger},
		repo:      repo,
		runner:    runner,
		asker:     asking.New(logger, repo, runner, bus),
	}
	h.after = time.After
	return h
}

// Tells : Sets where notices of what a voice turn is doing are sent.
//
// Optional, and nothing is published without it. The hub is the only
// listener there has ever been and a server built without one simply does
// not say.
func (h *Handler) Tells(n Notifier) *Handler {
	h.tell = n
	return h
}

// waiting : How many states may be queued for Home Assistant before one is
// dropped. A turn writes three, seconds apart, and a write to a local Home
// Assistant takes milliseconds -- this is deep enough that it is never
// reached and shallow enough to be obviously bounded.
const waiting = 16

// Shows : Sets where a voice turn's state is written in Home Assistant,
// and starts the one goroutine that writes them.
//
// One goroutine, and a queue, because the order is the whole point. The
// first version of this spawned a goroutine per state, and they raced:
// Home Assistant was told processing, idle, responding -- leaving the
// dashboard lit as though a finished turn were still speaking. A state
// that arrives out of order is worse than one that arrives late, because
// nothing after it corrects it.
//
// Optional in both halves: no reporter or no entity writes nowhere, and a
// server configured without one is a working server.
func (h *Handler) Shows(r Reporter, entity string) *Handler {
	h.shows, h.entity = r, strings.TrimSpace(entity)
	if h.shows == nil || h.entity == "" || h.queued != nil {
		return h
	}
	h.queued = make(chan string, waiting)
	go h.writing(h.queued)
	return h
}

// writing : Writes queued states to Home Assistant, one at a time and in
// the order they were asked for.
func (h *Handler) writing(queued <-chan string) {
	for state := range queued {
		ctx, cancel := context.WithTimeout(context.Background(), reporting)
		err := h.shows.Report(ctx, h.entity, state, nil)
		cancel()
		if err != nil {
			h.Logger.Warn("cannot show what the turn is doing",
				slog.String("entity", h.entity), slog.String("state", state), slog.Any("error", err))
		}
	}
}

// doing : Publishes what the voice satellite is doing now.
//
// The satellite's own entity in Home Assistant already says this for the
// laptop, and a second satellite is why this exists: HAwake on the phone
// registers no entity at all, so when a turn comes from there the entity
// never moves and a screen watching it shows nothing happening. Both
// devices reach this endpoint, so this is the one place that sees either.
//
// The two sources overlap on a laptop turn and that is harmless: the hub
// drops a repeat of a state it already holds, so the watcher and this say
// the same thing and only one of them is heard. The case worth knowing is
// a turn ending here while the laptop is genuinely busy -- this would say
// idle early, and the watcher's next change corrects it.
//
// Voice only. A typed turn moves no microphone, and reporting one as the
// satellite listening would be a reading nobody took.
func (h *Handler) doing(speaking bool, state string) {
	if !speaking {
		return
	}
	if h.tell != nil {
		h.tell.Publish(livehub.Event{Kind: livehub.SatelliteState, State: state, At: time.Now().UTC()})
	}
	if h.queued == nil {
		return
	}
	// Queued rather than written here, and never waited for: this is a
	// light on a dashboard, and Home Assistant being slow must not be
	// heard as Jarvis being slow. The writer uses its own context too --
	// the request's is cancelled the moment the turn ends, and the last
	// state written is idle, which would then be the one write that
	// never arrived.
	select {
	case h.queued <- state:
	default:
		h.Logger.Warn("dropped what the turn is doing; Home Assistant is not keeping up",
			slog.String("entity", h.entity), slog.String("state", state))
	}
}

// Mount : Registers the endpoints on r, which must already require
// authentication.
//
// The two paths are fixed by the client calling them: it appends them to the
// address it was configured with, so they cannot be moved under /v1 with the
// rest of the API.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/api/tags", h.Models)
	r.Post("/api/chat", h.Chat)
}

// Models : Lists the models available, of which there is one.
//
// Home Assistant calls this to check its configuration, and refuses to finish
// setting up if it does not answer quickly, so nothing slow belongs here.
func (h *Handler) Models(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(r.Context(), w, http.StatusOK, ModelsResponse{
		Models: []Model{{
			Name:       ModelName,
			Model:      ModelName,
			ModifiedAt: time.Now().UTC(),
			Details: ModelDetails{
				Family:            ModelName,
				Families:          []string{ModelName},
				Format:            "api",
				ParameterSize:     "n/a",
				QuantizationLevel: "n/a",
			},
		}},
	})
}

// Chat : Answers a question from Home Assistant, streaming the answer as it
// forms.
//
// The conversation Home Assistant sends is not stored. The server keeps its own
// log of a conversation and builds a model's history from that, so only the
// question is taken from the request; taking the rest would give the chat two
// disagreeing accounts of what was said.
func (h *Handler) Chat(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// The nearest the server stands to the moment the person stopped
	// talking. Home Assistant calls this within a millisecond or two of
	// finishing the transcript, so the gap it cannot see is the
	// recogniser's, which is measured on the satellite.
	arrived := time.Now()

	flusher, ok := w.(http.Flusher)
	if !ok {
		h.Logger.ErrorContext(ctx, "response writer cannot flush; streaming is impossible")
		httpx.WriteError(ctx, w, http.StatusInternalServerError, "Streaming is not available.")
		return
	}

	var req ChatRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(ctx, w, http.StatusBadRequest, "That request could not be read.")
		return
	}

	prompt := lastQuestion(req.Messages)
	if prompt == "" {
		httpx.WriteError(ctx, w, http.StatusBadRequest, "A question is required.")
		return
	}

	// A named conversation is used when the caller knows one and it is
	// theirs; anything else falls back to whichever this client is active
	// in. The channel comes from the caller, not from this endpoint: this one
	// speaks Ollama's wire format, and a format says nothing about how the
	// words were produced.
	caller := authn.Of(ctx)
	started, err := h.asker.Ask(ctx, asking.Request{
		UserID:               caller.User.ID,
		ClientID:             caller.Client.ID,
		Channel:              caller.Client.Channel,
		Model:                caller.Client.Model,
		ActiveConversationID: caller.Client.ActiveConversationID,
		ConversationID:       req.ConversationID,
		Prompt:               prompt,
	})
	switch {
	case errors.Is(err, asking.ErrNoSuchConversation):
		httpx.WriteError(ctx, w, http.StatusNotFound, "No such conversation.")
		return
	case errors.Is(err, asking.ErrEmptyPrompt):
		httpx.WriteError(ctx, w, http.StatusBadRequest, "A question is required.")
		return
	case errors.Is(err, asking.ErrPromptTooLong):
		httpx.WriteError(ctx, w, http.StatusBadRequest, "That question is too long.")
		return
	case errors.Is(err, asking.ErrNotAccepting):
		httpx.WriteError(ctx, w, http.StatusServiceUnavailable, "Not accepting work at the moment.")
		return
	case err != nil:
		h.Fail(ctx, w, "asking", err)
		return
	}
	defer started.Stop()
	t, live := started.Chat, started.Events

	// Whether anybody is waiting to hear this out loud, which decides
	// both what is streamed and whether a screen is told about it.
	speaking := t.Channel == chat.ChannelVoice
	mine := h.turn.Add(1)
	// Filled in by follow as the answer goes out: when the first words
	// were handed over, and all of them.
	var said spoken
	h.doing(speaking, livehub.Processing)
	// Held back until the assistant has finished speaking, which is a
	// thing nothing ever reports. See [Handler.finished].
	defer func() { h.finished(speaking, mine, said.when, said.text) }()

	h.Logger.InfoContext(ctx, "assist chat accepted", slog.String("chat_id", t.ID))

	// Every failure after this point is reported inside the stream, because
	// the status line has already been sent and cannot be taken back.
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-cache")
	// Tell nginx and similar not to buffer, which would hold the whole answer
	// back until the chat ended and defeat the point of streaming it.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Which chat this is, before anything else on the stream. A screen
	// showing what a turn is doing has to be able to ask, and until this
	// arrives it has nothing to ask about.
	opening := chunk("", false, "")
	opening.ChatID = t.ID
	writeChunk(w, flusher, opening)

	// Pieces of the answer go to a voice turn only. A screen gains
	// nothing from them -- nobody is waiting to hear the first clause
	// -- and the web client, which shares this endpoint, has never
	// been sent them: it drew the fragments and then the finished
	// answer underneath, so "Good evening, sir." arrived twice.
	h.follow(ctx, w, flusher, live, t.ID, speaking, arrived, &said)
}

// spoken : When the assistant started speaking and what it said, which is
// everything needed to work out when it will stop.
type spoken struct {
	when time.Time
	text string
}

// finished : Says the turn is over, once the assistant has stopped talking.
//
// Measured on one real turn: processing ran 7.3 seconds and responding
// lasted 61 milliseconds. The server is done the moment the words have
// been handed over; the assistant then speaks them for several seconds
// that nothing reports back. So a screen showing the assistant speaking
// lit for a sixteenth of a second and went out while it was still
// talking. Owner: "the ball is not glowing when listening and speaking."
//
// Listening cannot be had this way -- the words do not exist yet. Speaking
// can: the server is holding the sentence before any of it is played, so
// how long it will take is a property of something already in hand.
// Owner: "while speaking it knows what is gonna be spoken right."
//
// Held in its own goroutine. The caller is a request Home Assistant is
// waiting on, and sleeping there would keep the connection open for the
// length of the answer.
//
// The turn number is why this cannot say idle about somebody else's turn.
// A held idle and a new question racing is the out-of-order fault again,
// and here no later state corrects it: idle after processing leaves a
// screen dark for the whole of the next answer.
func (h *Handler) finished(speaking bool, mine int64, when time.Time, text string) {
	if !speaking {
		return
	}
	wait := time.Duration(0)
	if !when.IsZero() {
		wait = speech.Saying(text) - time.Since(when)
	}
	if wait <= 0 {
		h.doingIf(mine, livehub.Idle)
		return
	}
	go func() {
		<-h.after(wait)
		h.doingIf(mine, livehub.Idle)
	}()
}

// doingIf : Says state, unless another turn has started since.
func (h *Handler) doingIf(mine int64, state string) {
	if h.turn.Load() != mine {
		return
	}
	h.doing(true, state)
}

// follow : Writes the chat's messages as chunks until it ends or the caller
// leaves.
func (h *Handler) follow(
	ctx context.Context,
	w http.ResponseWriter,
	flusher http.Flusher,
	live <-chan events.Event,
	chatID string,
	speaking bool,
	arrived time.Time,
	said *spoken,
) {
	// The same condition as speaking, under the name this function uses
	// for it: pieces of an answer go to a voice turn only.
	partials := speaking
	keepAlive := time.NewTicker(keepAliveInterval)
	defer keepAlive.Stop()

	// Whether any of the answer has already gone out in pieces. What
	// was streamed must not be sent again whole at the end, or it is
	// spoken twice.
	streamed := false

	// When the first thing that would be spoken went out. What the
	// person waited for is this, not the end of the turn: by the time
	// the turn ends they have been listening for several seconds.
	var spoke time.Time

	// say : Writes a chunk, noting the first one that carries text.
	say := func(c ChatChunk) {
		if c.Message.Content != "" {
			if spoke.IsZero() {
				spoke = time.Now()
				said.when = spoke
				// The first words out is when answering starts, and
				// it is what a screen should show. Not the end of
				// the turn -- by then it has been speaking for
				// several seconds.
				h.doing(speaking, livehub.Responding)
			}
			// Every word of it, because how long it takes to say is
			// worked out from the whole sentence and not from the
			// piece that started it.
			said.text += c.Message.Content
		}
		writeChunk(w, flusher, c)
	}

	for {
		select {
		case <-ctx.Done():
			// Home Assistant hung up, which here means the turn is over: it
			// will not reconnect to collect what it missed, the way a phone
			// resuming a stream does. Letting the chat run on would spend a
			// provider call on an answer nobody can hear, so it is stopped.
			//
			// This is deliberately unlike the SSE stream, where a dropped
			// connection is a phone on bad mobile data and the answer must
			// still be there when it comes back.
			// Said as what it was. Home Assistant allows a turn thirty
			// seconds and then hangs up, and recording that as the
			// person having stopped it puts a thing in the transcript
			// that never happened.
			if h.runner.CancelBecause(chatID, callerLeft) {
				h.Logger.InfoContext(ctx, "assist caller left, chat cancelled",
					slog.String("chat_id", chatID))
			}
			return

		case <-keepAlive.C:
			writeChunk(w, flusher, chunk("", false, ""))

		case ev, ok := <-live:
			if !ok {
				// The bus closed without a terminal event, which leaves the
				// caller waiting on a stream that will never end unless it is
				// closed properly here.
				writeChunk(w, flusher, chunk("", true, "stop"))
				return
			}

			if ev.Kind == events.KindPartial {
				if !partials {
					continue
				}
				// A piece of the answer, put through as it arrives so
				// Home Assistant can start speaking on the first
				// clause. No newline and no trimming: this is the
				// middle of a sentence, and the spaces are part of it.
				if ev.Text != "" {
					say(chunk(ev.Text, false, ""))
					streamed = true
				}
				continue
			}

			if ev.Kind.Terminal() {
				// Only when it has not already gone out in pieces.
				// Sent again whole, the answer would be spoken twice.
				if text := settled(strings.TrimSpace(ev.Text)); text != "" && !streamed {
					say(chunk(text, false, ""))
				}
				last := chunk("", true, reasonFor(ev.Kind))
				// How long the person waited to hear anything.
				// Left absent by a turn that said nothing,
				// which has no wait to report.
				if !spoke.IsZero() {
					waited := spoke.Sub(arrived)
					ms := waited.Milliseconds()
					last.SpokeAfterMS = &ms
					h.Logger.InfoContext(ctx, "the answer began",
						slog.String("chat_id", chatID),
						slog.Duration("spoke_after", waited),
						slog.Duration("turn_took", time.Since(arrived)),
						slog.Bool("in_pieces", streamed))
				}
				// Only a screen can use these, and only a failure has them.
				// Home Assistant ignores fields it does not know, so the
				// spoken answer is unchanged by their being here.
				if ev.Kind == events.KindError {
					if failed, err := h.repo.Get(ctx, chatID); err == nil {
						last.ErrorCode = failed.ErrorCode
						last.ErrorDetail = failed.ErrorDetail
					}
				}
				writeChunk(w, flusher, last)
				return
			}

			// Progress, spoken as it happens. A chat reporting where it has
			// got to is why the connection is held open at all.
			if text := strings.TrimSpace(ev.Text); text != "" {
				say(chunk(text+"\n", false, ""))
			}
		}
	}
}

// chunk : Builds one piece of an answer.
func chunk(text string, done bool, reason string) ChatChunk {
	return ChatChunk{
		Model:      ModelName,
		CreatedAt:  time.Now().UTC(),
		Message:    Message{Role: roleAssistant, Content: text},
		Done:       done,
		DoneReason: reason,
	}
}

// writeChunk : Writes one chunk as a line of JSON and sends it immediately.
func writeChunk(w http.ResponseWriter, flusher http.Flusher, c ChatChunk) {
	if err := json.NewEncoder(w).Encode(c); err != nil {
		return
	}
	flusher.Flush()
}

// settled : Returns text with a trailing question mark turned into a full
// stop.
//
// Home Assistant reads the final character of an answer as a control signal:
// a question mark means "keep the microphone open for a reply", and there is
// no setting to turn that off. So an answer that happens to end in a question
// leaves the satellite listening, and the wake word stops being needed —
// which is the opposite of how the server is meant to be spoken to. Spoken aloud
// the substitution changes only the intonation of the last few words.
func settled(text string) string {
	runes := []rune(text)
	if len(runes) == 0 {
		return text
	}
	switch runes[len(runes)-1] {
	// The ordinary question mark, its fullwidth form, and the Greek question
	// mark, which is the set Home Assistant looks for.
	case '?', '？', '\u037e':
		runes[len(runes)-1] = '.'
		return string(runes)
	}
	return text
}

// reasonFor : Names why a chat stopped, in the vocabulary the caller expects.
func reasonFor(kind events.Kind) string {
	if kind == events.KindFinal {
		return "stop"
	}
	return string(kind)
}

// lastQuestion : Returns the most recent thing the user said.
//
// Home Assistant sends the whole conversation, its own system prompt included,
// and the question is the last user turn in it.
func lastQuestion(messages []Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != roleUser {
			continue
		}
		if text := strings.TrimSpace(messages[i].Content); text != "" {
			return text
		}
	}
	return ""
}
