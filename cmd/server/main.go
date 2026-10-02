// Command server runs the server.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"slices"
	"sync"
	"syscall"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/announce"
	"github.com/DhanushRamesh/personal-assistant/internal/announce/hass"
	"github.com/DhanushRamesh/personal-assistant/internal/announcement"
	"github.com/DhanushRamesh/personal-assistant/internal/api"
	"github.com/DhanushRamesh/personal-assistant/internal/calendar"
	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	chatmysql "github.com/DhanushRamesh/personal-assistant/internal/chat/mysql"
	"github.com/DhanushRamesh/personal-assistant/internal/config"
	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
	"github.com/DhanushRamesh/personal-assistant/internal/embed"
	"github.com/DhanushRamesh/personal-assistant/internal/embed/tei"
	"github.com/DhanushRamesh/personal-assistant/internal/environment"
	"github.com/DhanushRamesh/personal-assistant/internal/environment/platformai"
	"github.com/DhanushRamesh/personal-assistant/internal/event"
	eventmysql "github.com/DhanushRamesh/personal-assistant/internal/event/mysql"
	"github.com/DhanushRamesh/personal-assistant/internal/events"
	"github.com/DhanushRamesh/personal-assistant/internal/google"
	googlemysql "github.com/DhanushRamesh/personal-assistant/internal/google/mysql"
	"github.com/DhanushRamesh/personal-assistant/internal/llm"
	"github.com/DhanushRamesh/personal-assistant/internal/logging"
	"github.com/DhanushRamesh/personal-assistant/internal/mail"
	"github.com/DhanushRamesh/personal-assistant/internal/memory"
	memorymysql "github.com/DhanushRamesh/personal-assistant/internal/memory/mysql"
	"github.com/DhanushRamesh/personal-assistant/internal/persona"
	"github.com/DhanushRamesh/personal-assistant/internal/place"
	"github.com/DhanushRamesh/personal-assistant/internal/profile"
	"github.com/DhanushRamesh/personal-assistant/internal/remind"
	remindmysql "github.com/DhanushRamesh/personal-assistant/internal/remind/mysql"
	"github.com/DhanushRamesh/personal-assistant/internal/runner"
	"github.com/DhanushRamesh/personal-assistant/internal/speech"
	"github.com/DhanushRamesh/personal-assistant/internal/storage"
	"github.com/DhanushRamesh/personal-assistant/internal/tasks"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
	calendartool "github.com/DhanushRamesh/personal-assistant/internal/tool/calendar"
	"github.com/DhanushRamesh/personal-assistant/internal/tool/conversations"
	eventstool "github.com/DhanushRamesh/personal-assistant/internal/tool/events"
	mailtool "github.com/DhanushRamesh/personal-assistant/internal/tool/mail"
	"github.com/DhanushRamesh/personal-assistant/internal/tool/memories"
	placestool "github.com/DhanushRamesh/personal-assistant/internal/tool/places"
	"github.com/DhanushRamesh/personal-assistant/internal/tool/reminders"
	taskstool "github.com/DhanushRamesh/personal-assistant/internal/tool/tasks"
	"github.com/DhanushRamesh/personal-assistant/internal/vocabulary"
	vocabularymysql "github.com/DhanushRamesh/personal-assistant/internal/vocabulary/mysql"
)

// main : Runs the server, or the named command.
// serviceName : What this process calls itself in its own logs.
//
// Not the assistant's name, which is configuration: this identifies the
// process to whoever is reading the journal, and stays the same whatever the
// assistant is called today.
const serviceName = "assistant"

// embeddingCatchUpTimeout : How long embedding what has no vector may take
// at startup, before the server gets on with answering.
const embeddingCatchUpTimeout = 60 * time.Second

// embeddingCatchUpLimit : The most memories embedded in one catch-up.
const embeddingCatchUpLimit = 500

// transcriptCatchUpLimit : The most exchanges indexed in one catch-up.
//
// The transcript is indexed from nothing the first time, and the server
// should start answering rather than finish the backlog. What is left is
// picked up after each turn and at the next restart.
const transcriptCatchUpLimit = 2000

func main() {
	if len(os.Args) > 2 && os.Args[1] == "createuser" {
		osExitOnError(runCreateUser(os.Args[2]))
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "createuser" {
		fmt.Fprintln(os.Stderr, "usage: personal-assistant createuser <username>")
		os.Exit(1)
	}

	if len(os.Args) > 2 && os.Args[1] == "google" {
		osExitOnError(runGoogleCheck(os.Args[2]))
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "google" {
		fmt.Fprintln(os.Stderr, "usage: personal-assistant google <username>")
		os.Exit(1)
	}

	if err := run(); err != nil {
		// Configuration is read before the logger exists, so this cannot be
		// a structured record.
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

// storageDB : The database handle a command works through.
type storageDB = storage.DB

// runCreateUser : Opens the database and creates a user, without starting the
// server.
func runCreateUser(username string) error {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		return err
	}
	logger, err := logging.New(os.Stdout, logging.Config{
		Level: "warn", Format: cfg.Log.Format, Service: serviceName,
	})
	if err != nil {
		return err
	}

	db, err := storage.Open(context.Background(), cfg.Database, logger.Logger, storage.Options{})
	if err != nil {
		return err
	}
	defer db.Close()

	if cfg.Database.AutoMigrate {
		if err := storage.Migrate(context.Background(), db, logger.Logger); err != nil {
			return err
		}
	}
	return createUser(username, db)
}

// run : Loads configuration, opens the dependencies and serves until
// interrupted, returning the first error that prevents any of it.
func run() error {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		return err
	}

	logger, err := logging.New(os.Stdout, logging.Config{
		Level:     cfg.Log.Level,
		Format:    cfg.Log.Format,
		AddSource: cfg.Log.AddSource,
		Service:   serviceName,
		Version:   version(),
		Env:       string(cfg.Env),
	})
	if err != nil {
		return err
	}
	// So that packages logging without an injected logger use this format.
	slog.SetDefault(logger.Logger)

	// Config redacts the database password, so this is safe to emit.
	logger.Info("configuration loaded", slog.Any("config", cfg))

	// Opened here so an unusable database stops startup rather than failing
	// on the first request.
	db, err := storage.Open(context.Background(), cfg.Database, logger.Logger, storage.Options{
		// Statements carry user data, so they are recorded outside production only.
		LogStatements: !cfg.Env.IsProduction(),
	})
	if err != nil {
		return err
	}
	defer func() {
		if err := db.Close(); err != nil {
			logger.Error("closing database", slog.Any("error", err))
		}
	}()

	if cfg.Database.AutoMigrate {
		if err := storage.Migrate(context.Background(), db, logger.Logger); err != nil {
			return err
		}
	}

	chats := chatmysql.NewRepository(db)

	// Carries a chat's messages to whoever is listening for them.
	bus := events.NewBus(logger.Logger)
	defer bus.Close()

	answerer, err := buildProvider(cfg, logger.Logger)
	if err != nil {
		return err
	}
	logger.Info("provider selected", slog.String("provider", answerer.Name()))

	// One manner, shared by the runner that speaks in it and the API that
	// changes it. Held in memory because it is read on every prompt, and
	// started from what was last chosen, falling back to configuration when
	// nothing has been.
	// Somewhere to say a thing nobody asked for. Silent unless Home
	// Assistant is configured, which is not a fault: it is how the server
	// behaved before it could speak first.
	speaker := announcer(cfg, logger.Logger)

	// What the assistant has been asked to remember. Without an embedding
	// server it still works, matching words rather than meaning, which is
	// worse than the alternative and much better than going blind.
	remembering := &memory.Recall{
		Store:      memorymysql.New(db),
		Transcript: memorymysql.NewTranscript(db),
		Embedder:   embedder(cfg, logger.Logger),
		Logger:     logger.Logger,
	}
	catchUpEmbeddings(context.Background(), remembering, logger.Logger)

	// One store for what the person's devices saw, shared by the endpoint
	// they report to and the tool that reads it back.
	deviceEvents := eventmysql.New(db)

	// One store, shared by the tools that make reminders and the loop that
	// says them.
	reminderStore := remindmysql.New(db)
	clock := reminders.Clock{Now: cfg.Assistant.Now, Location: cfg.Assistant.Location}

	// One OAuth client for every Google API. Nil when no credentials
	// are configured, which the endpoints report as "nothing to
	// connect" rather than failing.
	googleLink := linkToGoogle(cfg, logger.Logger, db)

	// What the assistant can do as well as say. A registry that will not
	// build is a programming mistake, not a configuration one, so it stops
	// the server rather than quietly offering nothing.
	tools, err := tool.NewRegistry(slices.Concat(
		conversations.All(chats),
		memories.All(remembering),
		reminders.All(reminderStore, clock),
		taskstool.All(listsOf(googleLink), taskstool.Clock{
			Now: cfg.Assistant.Now, Location: cfg.Assistant.Location,
		}),
		calendartool.All(diaryOf(googleLink, cfg), calendartool.Clock{
			Now: cfg.Assistant.Now, Location: cfg.Assistant.Location,
		}),
		mailtool.All(mailboxOf(googleLink), mailtool.Clock{
			Now: cfg.Assistant.Now, Location: cfg.Assistant.Location,
		}),
		eventstool.Tools(deviceEvents, eventstool.Clock{
			Now: cfg.Assistant.Now, Location: cfg.Assistant.Location,
		}),
		placestool.Tools(deviceEvents, placestool.Clock{
			Now: cfg.Assistant.Now, Location: cfg.Assistant.Location,
		}),
	)...)
	if err != nil {
		return err
	}
	manner := persona.NewSetting(startingPersona(context.Background(), chats, cfg, logger.Logger))

	// Added after the rest, because it describes them and so has to be
	// built from the finished registry. It describes itself to nobody:
	// a model that has been told about tool_describe does not need to
	// ask what tool_describe takes.
	if err := tools.Add(tool.Describing(tools)); err != nil {
		return err
	}
	logger.Info("tools registered", slog.Any("tools", tools.Names()),
		slog.Int("described_always", tool.HotCount()))

	// Shared between the endpoint the speech-to-text bridge reports to and
	// the runner that reads the report. One store, because a report written
	// by one request has to be found by the next.
	cutOff := speech.New()

	chatRunner, err := runner.New(runner.Options{
		Repository:    chats,
		Messages:      chats,
		Environment:   answerer,
		Logger:        logger.Logger,
		Publisher:     bus,
		HistoryLimits: historyLimits(cfg, logger.Logger),
		AssistantName: cfg.Assistant.Name,
		Persona:       manner,
		Announcer:     speaker,
		Cut:           cutOff,
		Tools:         tools,
		Memory:        remembering,
		Now:           cfg.Assistant.Now,
		Recently: &remind.Recently{
			Store:    reminderStore,
			Location: cfg.Assistant.Location,
			Now:      cfg.Assistant.Now,
		},
		Missing: &remind.Missing{
			Store:    reminderStore,
			Location: cfg.Assistant.Location,
			Now:      cfg.Assistant.Now,
		},
		Aside: aside(cfg, logger.Logger),
	})
	if err != nil {
		return err
	}

	// Chats the previous process was running are no longer being worked on.
	if err := chatRunner.Recover(context.Background()); err != nil {
		return err
	}

	// What the assistant says without being asked is written into the
	// conversation the voice client is talking in, so that answering it
	// is possible at all. Without this a greeting and the reminders
	// behind it are spoken into a conversation that shows no trace of
	// them, and "how late was I" has nothing to refer to.
	announcements := &announcement.Writer{
		Conversations: chats,
		Clients:       chats,
		Location:      cfg.Assistant.Location,
		Now:           cfg.Assistant.Now,
		Logger:        logger.Logger,
	}

	// The first work here that happens because of the clock rather than
	// because somebody asked. Stopped with the server, so a reminder is
	// never half said during a shutdown.
	reminding := &remind.Loop{
		Store:    reminderStore,
		Presence: whereabouts(cfg, logger.Logger),
		Speaker: remind.Everywhere{
			To: []remind.Speaker{remind.Aloud{
				Announcer:     speaker,
				Location:      cfg.Assistant.Location,
				Now:           cfg.Assistant.Now,
				Announcements: announcements,
			}},
			Logger: logger.Logger,
		},
		Location: cfg.Assistant.Location,
		Logger:   logger.Logger,
	}
	remindCtx, stopReminders := context.WithCancel(context.Background())

	var watching sync.WaitGroup
	watching.Add(1)
	go func() {
		defer watching.Done()
		reminding.Run(remindCtx)
	}()
	// Registered in this order so they run in the other: stop first, then
	// wait for the pass in flight to finish.
	defer watching.Wait()
	defer stopReminders()

	logger.Info("watching for reminders",
		slog.Duration("every", remind.DefaultEvery),
		slog.Duration("grace", remind.DefaultGrace))

	names := vocabularymysql.New(db)

	// A standing description of the person, rewritten from what they have
	// actually said. The memories are facts they gave once and none of
	// them says what somebody is like to talk to; this is the attempt at
	// the rest.
	describing := &profile.Builder{
		Said:     chats,
		Did:      deviceEvents,
		Asked:    chats,
		Where:    cfg.Assistant.Location,
		Memories: remembering.Store,
		Now:      cfg.Assistant.Now,
		Logger:   logger.Logger,
		Ask: func(ctx context.Context, ask string) (string, error) {
			return askOnce(ctx, answerer, cfg.PlatformAI.Vendor, cfg.PlatformAI.Model,
				environment.PurposeProfile, ask)
		},
	}
	watching.Add(1)
	go func() {
		defer watching.Done()
		describeDaily(remindCtx, chats, describing, cfg.Assistant.Now, logger.Logger)
	}()
	logger.Info("describing the person daily", slog.Duration("every", profileEvery))

	// The names they say, for speech recognition to expect. Read from
	// the same week of messages, because a name is only worth the
	// budget if it keeps coming up. Nothing here reaches the engines:
	// the machine with the microphones fetches this and decides what to
	// do with it.
	listening := &vocabulary.Builder{
		Said:   chats,
		Store:  names,
		Now:    cfg.Assistant.Now,
		Logger: logger.Logger,
		Ask: func(ctx context.Context, ask string) (string, error) {
			return askOnce(ctx, answerer, cfg.PlatformAI.Vendor, cfg.PlatformAI.Model,
				environment.PurposeVocabulary, ask)
		},
	}
	watching.Add(1)
	go func() {
		defer watching.Done()
		listenDaily(remindCtx, chats, listening, cfg.Assistant.Now, logger.Logger)
	}()
	logger.Info("collecting the names they say daily",
		slog.Duration("every", vocabularyEvery))

	handler := api.New(api.Options{
		Logger:         logger.Logger,
		DB:             db,
		Chats:          chats,
		Messages:       chats,
		Reminders:      reminderStore,
		Announcer:      speaker,
		Announcements:  announcements,
		Google:         googleLink,
		SettingsURL:    cfg.Google.SettingsURL,
		Location:       cfg.Assistant.Location,
		Now:            cfg.Assistant.Now,
		Runner:         chatRunner,
		Memories:       remembering.Store,
		Vocabulary:     names,
		Events:         bus,
		Models:         reachableModels(cfg),
		DefaultModel:   cfg.PlatformAI.Model,
		Persona:        manner,
		Cut:            cutOff,
		DeviceEvents:   deviceEvents,
		Naming:         naming(cfg, logger.Logger),
		Tools:          tools,
		RequestTimeout: cfg.Server.RequestTimeout,
		// Development only: `flutter run` serves the UI from its own port so
		// that hot reload works. In production the server serves it, so every
		// call is same-origin.
		AllowCrossOrigin: !cfg.Env.IsProduction(),
	})

	srv := &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           handler,
		ReadHeaderTimeout: cfg.Server.ReadHeaderTimeout,
		IdleTimeout:       cfg.Server.IdleTimeout,
	}

	return serve(srv, chatRunner, logger, cfg.Server.ShutdownTimeout)
}

// buildProvider : Returns the engine that answers chats, as configuration
// selects it.
func buildProvider(cfg config.Config, logger *slog.Logger) (environment.Environment, error) {
	switch cfg.Provider.Name {
	case config.ProviderPlatformAI:
		return platformai.New(platformai.Config{
			ClientID:           cfg.PlatformAI.ClientID,
			ClientSecret:       cfg.PlatformAI.ClientSecret,
			RefreshToken:       cfg.PlatformAI.RefreshToken,
			PortalID:           cfg.PlatformAI.PortalID,
			TokenURL:           cfg.PlatformAI.TokenURL,
			ChatURL:            cfg.PlatformAI.ChatURL,
			Scope:              cfg.PlatformAI.Scope,
			RedirectURI:        cfg.PlatformAI.RedirectURI,
			Vendor:             cfg.PlatformAI.Vendor,
			Model:              cfg.PlatformAI.Model,
			SystemPrompt:       persona.Prompt(cfg.Assistant.Persona, cfg.Assistant.Name),
			Timeout:            cfg.PlatformAI.Timeout,
			InsecureSkipVerify: cfg.PlatformAI.InsecureSkipVerify,
		}, logger)

	default:
		// Answers from a script, so everything around an answer can be worked
		// on without credentials or a network.
		return &environment.Stub{Delay: 300 * time.Millisecond}, nil
	}
}

// reachableModels : The models the configured provider can call, which are
// the ones a client may be set to answer with.
//
// The provider says which models it will answer with and the catalogue says
// what each one holds. Neither knows the other: a model the endpoint routes
// but nobody has catalogued is left out, since there would be nothing to
// tell a person about it.
func reachableModels(cfg config.Config) []llm.Model {
	if cfg.Provider.Name != config.ProviderPlatformAI {
		return nil
	}

	refs := platformai.Models()
	out := make([]llm.Model, 0, len(refs))
	for _, ref := range refs {
		if m, ok := llm.Find(ref.Vendor, ref.ID); ok {
			out = append(out, m)
		}
	}
	return out
}

// naming : What to ask about a place the person never drew a geofence
// around, or nil when no key is configured.
//
// Nil rather than something that always fails, so a stay somewhere new
// is simply written down as its coordinates -- which is what it was
// before anything could name it -- and nothing logs a warning every
// time somebody goes somewhere.
func naming(cfg config.Config, logger *slog.Logger) event.Naming {
	if !cfg.Geoapify.Configured() {
		logger.Info("places are named by their coordinates", slog.String("why", "no geoapify key"))
		return nil
	}
	logger.Info("naming places through Geoapify", slog.String("url", cfg.Geoapify.URL))
	return place.Geoapify{
		Key:     cfg.Geoapify.Key.Reveal(),
		URL:     cfg.Geoapify.URL,
		Timeout: cfg.Geoapify.Timeout,
	}
}

// announcer : Where the assistant says something without being asked.
//
// Home Assistant when it is configured, and nowhere otherwise. Nowhere is a
// working server: it answers when spoken to, which is all it could ever do
// before.
// diaryOf : The calendar, or nothing when no Google account can be
// connected.
//
// A typed nil would satisfy the interface and then fail on every call
// with a nil pointer, so the nil is returned untyped and the tools say
// there is no calendar. That is the honest answer on a server with no
// Google client, and it is the same answer they give before anybody has
// connected one.
// listsOf : The person's to-do lists, or nothing when Google is not
// configured.
func listsOf(link *google.Link) taskstool.Lists {
	if link == nil {
		return nil
	}
	return tasks.New(link)
}

func mailboxOf(link *google.Link) mailtool.Mailbox {
	if link == nil {
		return nil
	}
	return mail.New(link)
}

func diaryOf(link *google.Link, cfg config.Config) calendartool.Diary {
	if link == nil {
		return nil
	}
	return calendar.New(link, cfg.Assistant.Location, cfg.Assistant.Now)
}

// linkToGoogle : The standing permission to reach a person's Google
// account, or nil when this server has no client credentials.
//
// Nil rather than an error: a server with no Google client is a working
// server that cannot reach Google, exactly as it was before any of this
// existed.
func linkToGoogle(cfg config.Config, logger *slog.Logger, db *storage.DB) *google.Link {
	link, err := google.New(google.Config{
		ClientID:     cfg.Google.ClientID,
		ClientSecret: cfg.Google.ClientSecret,
		Redirect:     cfg.Google.Redirect,
		Scopes:       cfg.Google.Scopes,
	}, googlemysql.New(db), cfg.Assistant.Now)
	if err != nil {
		if !errors.Is(err, google.ErrNotConfigured) {
			logger.Warn("cannot set up the Google connection", slog.Any("error", err))
		}
		logger.Info("no Google account can be connected")
		return nil
	}
	link.Logger = logger
	logger.Info("a Google account can be connected", slog.Any("scopes", link.Wanted()))
	return link
}

// whereabouts : What tells the firing loop whether anybody is in the room.
//
// Nil when nothing is configured, which means every reminder is said aloud
// -- what this did before there was any way to tell, and the safe way round.
func whereabouts(cfg config.Config, logger *slog.Logger) remind.Presence {
	p, err := hass.NewPresence(hass.PresenceConfig{
		URL:    cfg.HomeAssistant.URL,
		Token:  cfg.HomeAssistant.Token,
		Entity: cfg.HomeAssistant.PresenceEntity,
	})
	if err != nil {
		if !errors.Is(err, hass.ErrNotConfigured) {
			logger.Warn("cannot ask Home Assistant who is in the room", slog.Any("error", err))
		}
		logger.Info("reminders are said whoever is in the room")
		return nil
	}
	logger.Info("reminders are held back when nobody is in the room",
		slog.String("asking", p.Describe()))
	return p
}

func announcer(cfg config.Config, logger *slog.Logger) announce.Announcer {
	speaker, err := hass.New(hass.Config{
		URL:       cfg.HomeAssistant.URL,
		Token:     cfg.HomeAssistant.Token,
		Satellite: cfg.HomeAssistant.Satellite,
		Notify:    cfg.HomeAssistant.Notify,
		Logger:    logger,
	})
	if err != nil {
		if !errors.Is(err, hass.ErrNotConfigured) {
			logger.Warn("cannot reach Home Assistant to speak through",
				slog.Any("error", err))
		}
		return announce.Silent{Logger: logger}
	}

	logger.Info("announcing through Home Assistant",
		slog.String("satellite", cfg.HomeAssistant.Satellite),
		slog.Any("also_notifying", cfg.HomeAssistant.Notify))
	return speaker
}

// aside : Where the assistant says what it is about to do, mid-turn.
//
// Nil when no media player is configured, which answers exactly as before:
// silence until the answer. It is deliberately not the satellite. Speaking
// through the satellite mid-turn ends the turn, because an announcement and
// an answer share the completion callback that marks a turn finished.
func aside(cfg config.Config, logger *slog.Logger) runner.Aside {
	said, err := hass.NewAside(hass.AsideConfig{
		URL:         cfg.HomeAssistant.URL,
		Token:       cfg.HomeAssistant.Token,
		MediaPlayer: cfg.HomeAssistant.MediaPlayer,
		Engine:      cfg.HomeAssistant.TTSEngine,
		Voice:       cfg.HomeAssistant.TTSVoice,
		SettleWait:  cfg.HomeAssistant.AsideSettleWait,
		Language:    cfg.HomeAssistant.TTSLanguage,
	})
	if err != nil {
		if !errors.Is(err, hass.ErrNoAside) {
			logger.Warn("cannot reach Home Assistant to say what is being done",
				slog.Any("error", err))
		}
		logger.Info("nothing is said while an answer is being worked out")
		return nil
	}

	logger.Info("saying what is being done", slog.String("through", said.Describe()))
	return said
}

// embedder : What turns text into vectors.
//
// Nothing when none is configured, which leaves memory matching words. A
// server that is configured but not answering is not checked here: it is
// reached per request, and one that is down at startup and up a minute later
// should not leave the assistant matching words until it is restarted.
func embedder(cfg config.Config, logger *slog.Logger) embed.Embedder {
	if !cfg.Embedding.Configured() {
		logger.Info("no embedding server, so memories are found by their wording")
		return embed.Off{}
	}

	client := tei.New(tei.Config{
		URL:     cfg.Embedding.URL,
		Model:   cfg.Embedding.Model,
		Timeout: cfg.Embedding.Timeout,
	})
	logger.Info("embedding memories", slog.String("model", client.Model()))
	return client
}

// catchUpEmbeddings : Gives a vector to memories written while the embedding
// server was away.
//
// At startup, and never fatal. A memory with no vector is found by its
// wording until this succeeds, so failing here costs accuracy rather than
// the memory.
func catchUpEmbeddings(ctx context.Context, remembering *memory.Recall, logger *slog.Logger) {
	ctx, cancel := context.WithTimeout(ctx, embeddingCatchUpTimeout)
	defer cancel()

	done, err := remembering.Embed(ctx, embeddingCatchUpLimit)
	switch {
	case err != nil:
		logger.Warn("cannot embed the memories that have no vector yet",
			slog.Any("error", err))
	case done > 0:
		logger.Info("memories embedded", slog.Int("memories", done))
	}

	// The transcript is the larger of the two and is indexed from nothing
	// the first time, so it is bounded and finishes in the background over
	// however many restarts it takes.
	indexed, err := remembering.IndexTranscript(ctx, transcriptCatchUpLimit)
	switch {
	case err != nil:
		logger.Warn("cannot index the exchanges that have no vector yet",
			slog.Any("error", err))
	case indexed > 0:
		logger.Info("exchanges indexed", slog.Int("exchanges", indexed))
	}
}

// startingPersona : The manner to begin in.
//
// What was last chosen, or configuration when nothing has been. A database
// that will not answer is not a reason to refuse to start: the configured
// manner is a working assistant, and the next choice will store itself.
func startingPersona(ctx context.Context, repo chat.Repository, cfg config.Config, logger *slog.Logger) string {
	stored, err := repo.Setting(ctx, persona.SettingName)
	if err != nil {
		logger.Warn("cannot read the stored manner, using the configured one",
			slog.Any("error", err))
		return cfg.Assistant.Persona
	}
	if stored == "" {
		return cfg.Assistant.Persona
	}
	return stored
}

// historyLimits : The ceilings the conversation sent to the environment is held
// under, for the provider configuration selects.
//
// Each comes from whatever knows it: the message count from the service, the
// context window from the model behind it, and the size budget from us. A
// model missing from the catalogue contributes nothing and is reported, since
// the budget then governs alone.
func historyLimits(cfg config.Config, logger *slog.Logger) conversation.Limits {
	limits := conversation.Limits{Bytes: conversation.DefaultBudget}

	if cfg.Provider.Name != config.ProviderPlatformAI {
		return limits
	}

	limits.Count = platformai.MaxMessages

	vendor, id := cfg.PlatformAI.Vendor, cfg.PlatformAI.Model
	model, known := llm.Find(vendor, id)
	if !known {
		logger.Warn("model is not in the catalogue, so only the byte budget bounds the history",
			slog.String("vendor", vendor), slog.String("model", id))
		return limits
	}
	limits.ContextTokens = model.ContextTokens

	return limits
}

// serve : Starts srv and blocks until an interrupt arrives, then drains
// in-flight requests before returning.
func serve(srv *http.Server, chatRunner *runner.Runner, logger *logging.Logger, shutdownTimeout time.Duration) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.InfoContext(ctx, "server listening",
			slog.String("addr", srv.Addr),
			slog.String("log_level", logger.Level().String()),
		)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		logger.Info("shutdown signal received, draining",
			slog.Duration("grace_period", shutdownTimeout))
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	// Stop accepting requests first, then let running chats record where they
	// got to. A chat cut off without that is left reading running for ever.
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", slog.Any("error", err))
		return err
	}
	if err := chatRunner.Shutdown(shutdownCtx); err != nil {
		logger.Error("running chats did not stop cleanly", slog.Any("error", err))
		return err
	}

	logger.Info("shutdown complete")
	return nil
}

// version : Returns the VCS revision stamped into the binary by the Go
// toolchain, or a placeholder when it is unavailable.
func version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			if len(s.Value) > 12 {
				return s.Value[:12]
			}
			return s.Value
		}
	}
	return "dev"
}

// profileEvery : How often the description is rewritten.
//
// Daily. It reads a week each time, so running it more often spends
// model calls to re-read mostly the same material; running it less
// often leaves it describing somebody from before whatever changed.
const profileEvery = 24 * time.Hour

// profileFirst : How long after startup the first rebuild runs.
//
// Not at once. Starting the server should not cost a model call, and a
// restart to change something unrelated should not rewrite the
// description as a side effect.
const profileFirst = 10 * time.Minute

// vocabularyEvery : How often the names are collected again.
//
// Nightly, like the description. The list only changes when somebody
// new comes up, which is not an hourly event, and each pass costs a
// call to the model.
const vocabularyEvery = 24 * time.Hour

// vocabularyFirst : How long after startup the first pass runs.
//
// After the description rather than beside it, so that a restart does
// not fire two model calls at once.
const vocabularyFirst = 20 * time.Minute

// vocabularyTimeout : How long one pass may take.
const vocabularyTimeout = 3 * time.Minute

// profileTimeout : How long one rebuild may take.
//
// Generous, because it reads a week of messages into one prompt and
// nobody is waiting on the answer.
const profileTimeout = 3 * time.Minute

// describeDaily : Rewrites everybody's description, for as long as ctx
// lives.
//
// Everybody who has said something recently rather than everybody with
// an account: somebody who has not spoken in a week keeps what they
// had, instead of having it rewritten from nothing.
func describeDaily(
	ctx context.Context,
	talkers interface {
		Talkers(ctx context.Context, since time.Time) ([]string, error)
	},
	describing *profile.Builder,
	now func() time.Time,
	logger *slog.Logger,
) {
	timer := time.NewTimer(profileFirst)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		timer.Reset(profileEvery)

		pass, cancel := context.WithTimeout(ctx, profileTimeout)
		who, err := talkers.Talkers(pass, now().Add(-profile.Window))
		if err != nil {
			logger.Warn("cannot tell who has been talking", slog.Any("error", err))
			cancel()
			continue
		}
		for _, userID := range who {
			if err := describing.Build(pass, userID); err != nil {
				logger.Warn("cannot describe them", slog.Any("error", err))
				continue
			}
			logger.Info("described them from what they said",
				slog.String("user_id", userID))
		}
		cancel()
	}
}

// listenDaily : Collects everybody's names again, for as long as ctx
// lives.
//
// Everybody who has said something recently, like the description:
// somebody who has not spoken in a week keeps the names they had
// rather than having them collected from nothing.
func listenDaily(
	ctx context.Context,
	talkers interface {
		Talkers(ctx context.Context, since time.Time) ([]string, error)
	},
	listening *vocabulary.Builder,
	now func() time.Time,
	logger *slog.Logger,
) {
	timer := time.NewTimer(vocabularyFirst)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		timer.Reset(vocabularyEvery)

		pass, cancel := context.WithTimeout(ctx, vocabularyTimeout)
		who, err := talkers.Talkers(pass, now().Add(-vocabulary.Window))
		if err != nil {
			logger.Warn("cannot tell who has been talking", slog.Any("error", err))
			cancel()
			continue
		}
		for _, userID := range who {
			if err := listening.Build(pass, userID); err != nil {
				logger.Warn("cannot collect the names they say", slog.Any("error", err))
				continue
			}
			logger.Info("collected the names they say",
				slog.String("user_id", userID))
		}
		cancel()
	}
}

// askOnce : Puts one prompt to the provider and returns what it said.
//
// The runner has its own copy of this for naming and condensing, where
// the model comes from the chat being answered. Nothing is being
// answered here, so the configured default is used instead.
// The vendor travels with the model, and both or neither. Naming a
// model without one leaves the vendor empty on the wire -- the provider
// only reads its own default when no model was named at all -- and the
// answer is "The server would not accept that request", which says
// nothing about which field was wrong.
func askOnce(
	ctx context.Context,
	env environment.Environment,
	vendor, model string,
	why environment.Purpose,
	ask string,
) (string, error) {
	stream, err := env.Run(ctx, environment.Request{
		Prompt: ask, Purpose: why, Vendor: vendor, Model: model,
	})
	if err != nil {
		return "", err
	}

	// Drained to the end whatever it says, so the provider's goroutine is
	// never left blocked on a send.
	var last *environment.Message
	for msg := range stream {
		m := msg
		last = &m
	}
	if last == nil {
		return "", fmt.Errorf("the provider said nothing")
	}
	if last.Kind == environment.KindError {
		return "", fmt.Errorf("%s", last.Text)
	}
	return last.Text, nil
}
