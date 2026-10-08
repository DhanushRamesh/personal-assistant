// Package api : Assembles the server's HTTP interface from its modules.
//
// Nothing is served from here. Each group of endpoints lives in its own
// package below this one — authn, clients, conversations, chats, assist, health —
// holding its own handlers and wire types, and this package's only job is to build
// them from one set of dependencies, decide the middleware they sit behind,
// and mount them on one router.
//
// Keeping the assembly in one place is what makes the shape of the API
// readable: routes() below is the whole surface, and a module cannot quietly
// register an endpoint somewhere else.
package api

import (
	"log/slog"
	"net/http"
	"personal-assistant/internal/base/store"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	announcementsapi "personal-assistant/internal/app/api/announcements"
	"personal-assistant/internal/app/api/assist"
	"personal-assistant/internal/app/api/authn"
	"personal-assistant/internal/app/api/chats"
	"personal-assistant/internal/app/api/clients"
	"personal-assistant/internal/app/api/conversations"
	"personal-assistant/internal/app/api/events"
	googleapi "personal-assistant/internal/app/api/google"
	"personal-assistant/internal/app/api/health"
	"personal-assistant/internal/app/api/memories"
	"personal-assistant/internal/app/api/middleware"
	"personal-assistant/internal/app/api/presence"
	profileapi "personal-assistant/internal/app/api/profile"
	remarksapi "personal-assistant/internal/app/api/remarks"
	"personal-assistant/internal/app/api/reminders"
	rulesapi "personal-assistant/internal/app/api/rules"
	speechapi "personal-assistant/internal/app/api/speech"
	toolsapi "personal-assistant/internal/app/api/tools"
	vocabularyapi "personal-assistant/internal/app/api/vocabulary"
	"personal-assistant/internal/app/tool"
	"personal-assistant/internal/base/announce"
	"personal-assistant/internal/base/conversation"
	"personal-assistant/internal/base/event"
	"personal-assistant/internal/base/llm"
	"personal-assistant/internal/base/routine"
	"personal-assistant/internal/base/speech"
	"personal-assistant/internal/domain/google"
	"personal-assistant/internal/domain/greet"
	"personal-assistant/internal/domain/memory"
	"personal-assistant/internal/domain/persona"
	"personal-assistant/internal/domain/remind"
	"personal-assistant/internal/domain/vocabulary"
)

// DefaultRequestTimeout : The per-request deadline applied when Options does
// not name one.
const DefaultRequestTimeout = 30 * time.Second

// The interfaces the modules require, republished here so that a caller
// building a Server does not have to import the module that defines each one.
type (
	// Pinger : The part of a database handle that the readiness check
	// requires.
	Pinger = health.Pinger
	// Runner : The part of the chat runner that the API requires.
	Runner = chats.Runner
	// Subscriber : Somewhere to listen for a chat's messages as they happen.
	Subscriber = chats.Subscriber
)

// Options : The dependencies and settings a Server is built from.
type Options struct {
	// Logger : Receives request records and handler errors. Required.
	Logger *slog.Logger
	// DB : Checked by the readiness endpoint. Required.
	DB Pinger
	// Chats : Stores and retrieves chats. Required.
	Chats store.Repository

	// Messages : Reads what a turn wrote, for the timeline of an answer.
	// Optional; without it a timeline carries only what the chat records.
	Messages conversation.Repository

	// Reminders : What is waiting to be said. Optional; without it the
	// listing is empty and nothing can be called off from the screen.
	Reminders remind.Store

	// Google : A person's standing permission to reach their own Google
	// account. Nil means nothing can be connected, which every Google
	// endpoint reports rather than failing.
	Google *google.Link

	// SettingsURL : Where a browser is sent after Google hands it back,
	// such as http://localhost:8000/settings/google. Empty answers the
	// callback in place instead of redirecting.
	SettingsURL string

	// Greeting : What writes the words said at the door, when there is
	// anything better to say than one of the fixed sentences. Optional.
	Greeting *greet.Writer
	// Spoke, Reported, Known : What the greeting is written from -- when
	// the assistant last spoke to them, what their devices reported
	// since, and what is known about them. Optional, all three.
	Spoke    presence.Spoke
	Reported presence.Reported
	Known    presence.Known
	// ThisMachine : The source the assistant's own machine reports
	// under, whose events describe it rather than the person.
	ThisMachine string
	// Diary, Chores : What is written down for the days ahead, for a
	// greeting that can say what is coming. Optional, both.
	Diary  presence.Diary
	Chores presence.Chores

	// Announcements : Where what the server said of its own accord is
	// noted in the conversation, so the person can answer it. Optional;
	// without it a greeting is spoken and not written down.
	Announcements presence.Announcements

	// Memories : Where the standing description of the person is kept, so
	// they can read it and correct it. Nil leaves the endpoint answering
	// as though there were none.
	Memories memory.Store

	// Vocabulary : Where the names heard in conversation are kept, for
	// the machine with the microphones to fetch. Nil serves an empty
	// list rather than failing: speech still works without it, having
	// only the hand-kept words to go on.
	Vocabulary vocabulary.Store

	// Announcer : Where the server speaks of its own accord. Optional;
	// without it a greeting is composed and not said.
	Announcer announce.Announcer
	// Tools : Everything the assistant can do, for the screen that shows
	// it. Nil lists nothing.
	Tools *tool.Registry
	// Remarks : What the assistant considered saying unasked. Nil
	// serves an empty list, which is what a server with nothing
	// watching the room should show.
	Remarks remarksapi.Store
	// Rules : Where the standing rules are read from.
	Rules rulesapi.Store
	// Usually : What the person usually does, as conditions a rule
	// may stand on.
	Usually routine.Store
	// Trying : Runs a rule on demand without it saying anything.
	Trying rulesapi.Trier

	// Owed : What each client still owes somebody to say, which it
	// collects for itself. Named apart from Announcements above:
	// that one is where a spoken thing is written into the
	// conversation, this one is the queue it came out of.
	//
	// Read by two: the endpoint a screen polls, and the greeting,
	// where the voice client collects its own at the door.
	Owed announcementsapi.Store

	// DeviceEvents : Where the person's own devices report what happened
	// to them, which is what lets the assistant say something first.
	//
	// Named apart from Events, which is the bus carrying a chat's own
	// messages to clients. Two different senses of the word met here.
	DeviceEvents events.Store
	// Arrivals : Told which kinds of reading have just landed, for
	// the rules that wait on one rather than on a time. Optional.
	Arrivals events.Arrivals

	// Naming : What to ask about a place the person never drew a
	// geofence around. Optional: without it a stay somewhere new is
	// written down as its coordinates.
	Naming event.Naming

	// Cut : Where the speech-to-text bridge reports that a recording was
	// stopped while somebody was still speaking. Home Assistant does not
	// pass that on, so it arrives by this side door instead.
	Cut *speech.Cut

	// Location : The person's zone, for deciding what hour it is to them.
	// Nil is UTC.
	Location *time.Location
	// Now : The clock, replaceable in tests. Nil uses the real one.
	Now func() time.Time
	// Runner : Executes chats. Required.
	Runner Runner
	// Events : Carries a chat's messages to clients listening for them.
	// Required for streaming.
	Events Subscriber

	// Models : The models the configured provider can reach, which are the
	// ones a client may be set to answer with. Empty offers none, so a
	// client keeps whatever the server is configured with.
	Models []llm.Model
	// DefaultModel : The identifier of the model answering a client that has
	// chosen none, so a listing can name it.
	DefaultModel string
	// Persona : The manner the assistant answers in, shared with the runner
	// so that choosing one here is answered in by the next prompt.
	Persona *persona.Setting

	// RequestTimeout : The per-request deadline. Zero selects
	// DefaultRequestTimeout.
	RequestTimeout time.Duration

	// AllowCrossOrigin : Whether a browser on this machine may call the API
	// from another origin.
	//
	// For development only, where `flutter run` serves the UI from its own
	// port so that hot reload works. In production the server serves the UI
	// itself, so every call is same-origin and this stays false.
	AllowCrossOrigin bool
}

// Server : the server's HTTP interface. It implements http.Handler.
type Server struct {
	logger           *slog.Logger
	requestTimeout   time.Duration
	allowCrossOrigin bool
	router           chi.Router

	health        *health.Handler
	authn         *authn.Handler
	clients       *clients.Handler
	conversations *conversations.Handler
	chats         *chats.Handler
	reminders     *reminders.Handler
	presence      *presence.Handler
	profile       *profileapi.Handler
	vocabulary    *vocabularyapi.Handler
	google        *googleapi.Handler
	assist        *assist.Handler
	speech        *speechapi.Handler
	events        *events.Handler
	memories      *memories.Handler
	tools         *toolsapi.Handler
	remarks       *remarksapi.Handler
	rules         *rulesapi.Handler
	announcements *announcementsapi.Handler
}

// New : Builds a Server from opts and registers its routes.
func New(opts Options) *Server {
	if opts.RequestTimeout <= 0 {
		opts.RequestTimeout = DefaultRequestTimeout
	}

	s := &Server{
		logger:           opts.Logger,
		requestTimeout:   opts.RequestTimeout,
		allowCrossOrigin: opts.AllowCrossOrigin,
		router:           chi.NewRouter(),

		health:        health.New(opts.Logger, opts.DB),
		authn:         authn.New(opts.Logger, opts.Chats),
		clients:       clients.New(opts.Logger, opts.Chats, opts.Models, opts.DefaultModel, opts.Persona),
		conversations: conversations.New(opts.Logger, opts.Chats, opts.Messages),
		chats:         chats.New(opts.Logger, opts.Chats, opts.Messages, opts.Runner, opts.Events),
		reminders:     reminders.New(opts.Logger, opts.Reminders),
		presence: presence.New(opts.Logger, opts.Announcer, opts.Reminders,
			opts.Announcements, opts.Location, opts.Now).
			Writes(opts.Greeting, opts.Spoke, opts.Reported, opts.Known, opts.ThisMachine).
			Expects(opts.Diary, opts.Chores).
			Collects(opts.Owed),
		profile:    profileapi.New(opts.Memories, opts.Now, opts.Logger),
		vocabulary: vocabularyapi.New(opts.Vocabulary, opts.Logger),
		google:     googleapi.New(opts.Logger, opts.Google, opts.SettingsURL),
		assist:     assist.New(opts.Logger, opts.Chats, opts.Runner, opts.Events),
		speech:     speechapi.New(opts.Logger, opts.Cut),
		events: events.New(opts.Logger, opts.DeviceEvents, opts.Now).
			Naming(opts.Naming).Arrivals(opts.Arrivals),
		memories:      memories.New(opts.Logger, opts.Memories),
		tools:         toolsapi.New(opts.Logger, opts.Tools),
		remarks:       remarksapi.New(opts.Logger, opts.Remarks),
		rules:         rulesapi.New(opts.Logger, opts.Rules, opts.Usually, opts.Trying),
		announcements: announcementsapi.New(opts.Logger, opts.Owed, nil),
	}
	s.routes()
	return s
}

// ServeHTTP : Dispatches r to the matching route.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

// routes : Registers the middleware stack and mounts every module.
//
// Middleware order is significant: RequestID must run before RequestContext,
// which must run before anything that logs, so that every record produced
// while serving a request carries its identifier.
func (s *Server) routes() {
	s.router.Use(chimw.RequestID)
	s.router.Use(chimw.RealIP)
	s.router.Use(middleware.RequestContext)
	s.router.Use(middleware.RequestLogger(s.logger))
	s.router.Use(middleware.Recoverer(s.logger))
	// After the logger, so that a preflight is recorded like any other
	// request rather than vanishing; before authentication, because a
	// preflight cannot carry a token and would be refused before it was
	// answered.
	s.router.Use(middleware.CrossOrigin(s.allowCrossOrigin))
	s.router.Use(chimw.Timeout(s.requestTimeout))

	// Open: probed by infrastructure, and the one call that issues a token.
	s.health.Mount(s.router)
	s.authn.Mount(s.router)
	// Google redirects a browser here, and a redirect carries no
	// Authorization header. The state it arrives with is what stands in
	// for one.
	s.google.MountPublic(s.router)

	// Everything else needs one. Grouped so that authentication is applied
	// once here rather than remembered by each module.
	s.router.Group(func(r chi.Router) {
		r.Use(s.authn.Require)
		s.clients.Mount(r)
		s.conversations.Mount(r)
		s.chats.Mount(r)
		s.reminders.Mount(r)
		s.presence.Mount(r)
		s.profile.Mount(r)
		s.vocabulary.Mount(r)
		s.google.Mount(r)
		s.assist.Mount(r)
		s.speech.Mount(r)
		s.events.Mount(r)
		s.memories.Mount(r)
		s.tools.Mount(r)
		s.remarks.Mount(r)
		s.rules.Mount(r)
		s.announcements.Mount(r)
	})
}
