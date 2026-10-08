// Command server runs the server.
package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/DhanushRamesh/personal-assistant/internal/base/client"
	"github.com/DhanushRamesh/personal-assistant/internal/base/store"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/app/api"
	"github.com/DhanushRamesh/personal-assistant/internal/app/api/presence"
	"github.com/DhanushRamesh/personal-assistant/internal/app/profile"
	"github.com/DhanushRamesh/personal-assistant/internal/app/runner"
	"github.com/DhanushRamesh/personal-assistant/internal/app/tool"
	calendartool "github.com/DhanushRamesh/personal-assistant/internal/app/tool/calendar"
	comingtool "github.com/DhanushRamesh/personal-assistant/internal/app/tool/coming"
	"github.com/DhanushRamesh/personal-assistant/internal/app/tool/conversations"
	eventstool "github.com/DhanushRamesh/personal-assistant/internal/app/tool/events"
	leavingtool "github.com/DhanushRamesh/personal-assistant/internal/app/tool/leaving"
	mailtool "github.com/DhanushRamesh/personal-assistant/internal/app/tool/mail"
	mapstool "github.com/DhanushRamesh/personal-assistant/internal/app/tool/maps"
	"github.com/DhanushRamesh/personal-assistant/internal/app/tool/memories"
	placestool "github.com/DhanushRamesh/personal-assistant/internal/app/tool/places"
	"github.com/DhanushRamesh/personal-assistant/internal/app/tool/reminders"
	"github.com/DhanushRamesh/personal-assistant/internal/app/tool/rules"
	situationstool "github.com/DhanushRamesh/personal-assistant/internal/app/tool/situations"
	taskstool "github.com/DhanushRamesh/personal-assistant/internal/app/tool/tasks"
	webtool "github.com/DhanushRamesh/personal-assistant/internal/app/tool/web"
	"github.com/DhanushRamesh/personal-assistant/internal/base/alarm"
	"github.com/DhanushRamesh/personal-assistant/internal/base/announce"
	"github.com/DhanushRamesh/personal-assistant/internal/base/announce/hass"
	"github.com/DhanushRamesh/personal-assistant/internal/base/chat"
	chatmysql "github.com/DhanushRamesh/personal-assistant/internal/base/chat/mysql"
	"github.com/DhanushRamesh/personal-assistant/internal/base/config"
	"github.com/DhanushRamesh/personal-assistant/internal/base/conversation"
	"github.com/DhanushRamesh/personal-assistant/internal/base/embed"
	"github.com/DhanushRamesh/personal-assistant/internal/base/embed/tei"
	"github.com/DhanushRamesh/personal-assistant/internal/base/environment"
	"github.com/DhanushRamesh/personal-assistant/internal/base/environment/platformai"
	"github.com/DhanushRamesh/personal-assistant/internal/base/errand"
	errandmysql "github.com/DhanushRamesh/personal-assistant/internal/base/errand/mysql"
	"github.com/DhanushRamesh/personal-assistant/internal/base/event"
	eventmysql "github.com/DhanushRamesh/personal-assistant/internal/base/event/mysql"
	"github.com/DhanushRamesh/personal-assistant/internal/base/events"
	"github.com/DhanushRamesh/personal-assistant/internal/base/leaving"
	"github.com/DhanushRamesh/personal-assistant/internal/base/llm"
	"github.com/DhanushRamesh/personal-assistant/internal/base/logging"
	"github.com/DhanushRamesh/personal-assistant/internal/base/notice"
	noticemysql "github.com/DhanushRamesh/personal-assistant/internal/base/notice/mysql"
	"github.com/DhanushRamesh/personal-assistant/internal/base/place"
	"github.com/DhanushRamesh/personal-assistant/internal/base/prompt"
	"github.com/DhanushRamesh/personal-assistant/internal/base/routine"
	routinemysql "github.com/DhanushRamesh/personal-assistant/internal/base/routine/mysql"
	"github.com/DhanushRamesh/personal-assistant/internal/base/search"
	"github.com/DhanushRamesh/personal-assistant/internal/base/search/firecrawl"
	"github.com/DhanushRamesh/personal-assistant/internal/base/search/tinyfish"
	"github.com/DhanushRamesh/personal-assistant/internal/base/speech"
	"github.com/DhanushRamesh/personal-assistant/internal/base/storage"
	"github.com/DhanushRamesh/personal-assistant/internal/domain/announcement"
	"github.com/DhanushRamesh/personal-assistant/internal/domain/calendar"
	"github.com/DhanushRamesh/personal-assistant/internal/domain/coming"
	comingmysql "github.com/DhanushRamesh/personal-assistant/internal/domain/coming/mysql"
	"github.com/DhanushRamesh/personal-assistant/internal/domain/google"
	googlemysql "github.com/DhanushRamesh/personal-assistant/internal/domain/google/mysql"
	"github.com/DhanushRamesh/personal-assistant/internal/domain/greet"
	"github.com/DhanushRamesh/personal-assistant/internal/domain/job"
	jobmysql "github.com/DhanushRamesh/personal-assistant/internal/domain/job/mysql"
	"github.com/DhanushRamesh/personal-assistant/internal/domain/mail"
	"github.com/DhanushRamesh/personal-assistant/internal/domain/memory"
	memorymysql "github.com/DhanushRamesh/personal-assistant/internal/domain/memory/mysql"
	"github.com/DhanushRamesh/personal-assistant/internal/domain/outbox"
	outboxmysql "github.com/DhanushRamesh/personal-assistant/internal/domain/outbox/mysql"
	"github.com/DhanushRamesh/personal-assistant/internal/domain/persona"
	"github.com/DhanushRamesh/personal-assistant/internal/domain/remind"
	remindmysql "github.com/DhanushRamesh/personal-assistant/internal/domain/remind/mysql"
	"github.com/DhanushRamesh/personal-assistant/internal/domain/situation"
	"github.com/DhanushRamesh/personal-assistant/internal/domain/tasks"
	"github.com/DhanushRamesh/personal-assistant/internal/domain/vocabulary"
	vocabularymysql "github.com/DhanushRamesh/personal-assistant/internal/domain/vocabulary/mysql"
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
	// What the person is called, for the word said in front of an
	// announcement that arrives out of nowhere. One configured
	// persona for the server, so it is set here rather than carried
	// down to every caller.
	announcement.Addressing(persona.AddressFor(cfg.Assistant.Persona))

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

	// A real alarm on the person's phone, beside every reminder. Nil
	// when there is no address for it, which the answer reports as
	// not being able to reach the phone rather than as a failure.
	// Asking for an alarm and getting it to the phone are two
	// jobs now, and only the first happens in anybody's turn.
	sending := &errand.Sender{
		Store:  errandmysql.New(db.DB),
		Hands:  errand.Hands{},
		Now:    cfg.Assistant.Now,
		Logger: logger.Logger,
	}
	if phone := phoneAlarm(cfg, logger.Logger); phone != nil {
		sending.Hands = alarmHands(phone)
	}
	// Untyped nil when there is nowhere to ask, for the usual
	// reason: the honest answer on a server with no address for
	// the phone is that it cannot reach the phone at all, which is
	// a different sentence from the owner having turned alarms
	// off, and the person deserves the right one.
	var alarms reminders.Alarms
	if len(sending.Hands) > 0 {
		alarms = queue{to: sending, on: cfg.Assistant.Alarms}
	}

	// One OAuth client for every Google API. Nil when no credentials
	// are configured, which the endpoints report as "nothing to
	// connect" rather than failing.
	googleLink := linkToGoogle(cfg, logger.Logger, db)

	// What the assistant can do as well as say. A registry that will not
	// build is a programming mistake, not a configuration one, so it stops
	// the server rather than quietly offering nothing.
	// A rule may run a tool, so the rule tools have to be able to
	// look one up -- including the ones registered beside them.
	// Resolved through a variable set just below, because the
	// registry cannot be an argument to its own contents, and
	// nothing calls a tool before it exists.
	var registry *tool.Registry
	known := func(name string) (tool.Tool, bool) {
		if registry == nil {
			return tool.Tool{}, false
		}
		return registry.Get(name)
	}

	// The engine, named before it exists.
	//
	// A rule is tried the moment it is written, which needs the
	// runner -- and the runner needs the registry, which needs the
	// rule tools. The cycle is real and the only thing in it that
	// can wait is this: a rule cannot be written before the server
	// is listening, by which time the runner is long built.
	var running *job.Runner

	tools, err := tool.NewRegistry(slices.Concat(
		conversations.All(chats),
		situationstool.Tools(deviceEvents, []situation.Check{
			// The three shapes, in the order they are looked for.
			// Somebody trying and not getting through comes first:
			// it is about a person, and the other two are about a
			// device.
			situation.Trying,
			situation.NotCharging,
			situation.Quiet,
		}, cfg.Assistant.Now),
		rules.All(jobmysql.New(db.DB), rules.Clock{
			Now: cfg.Assistant.Now, Location: cfg.Assistant.Location,
		}, jobmysql.ID, known, routinemysql.New(db.DB),
			func(ctx context.Context, j job.Job) job.Trial {
				if running == nil {
					return job.Trial{}
				}
				return running.Try(ctx, j)
			}),
		memories.All(remembering),
		reminders.All(reminderStore, clock, alarms),
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
		comingtool.Tools(comingmysql.New(db.DB), comingtool.Clock{
			Now: cfg.Assistant.Now, Location: cfg.Assistant.Location,
		}),
		[]tool.Tool{leavingtool.Due(settingOff(cfg, comingDiary(googleLink, cfg),
			mapping(cfg), deviceEvents, logger.Logger))},
		[]tool.Tool{comingtool.Due(comingtool.Looking{
			Diary: comingDiary(googleLink, cfg),
			Store: comingmysql.New(db.DB),
			Ask: func(ctx context.Context, _, ask string) (string, error) {
				return askAs(ctx, answerer, cfg.PlatformAI.Vendor,
					cmp.Or(cfg.PlatformAI.FastModel, cfg.PlatformAI.Model),
					persona.Voice(cfg.Assistant.Persona, cfg.Assistant.Name),
					environment.PurposeRemark, ask)
			},
			Location: cfg.Assistant.Location,
			Now:      cfg.Assistant.Now,
			Logger:   logger.Logger,
		})},
		webtool.Tools(searchingFast(cfg, logger.Logger)),
		mapstool.Tools(mapping(cfg), deviceEvents, mapstool.Clock{
			Now: cfg.Assistant.Now, Location: cfg.Assistant.Location,
		}),
	)...)
	if err != nil {
		return err
	}
	registry = tools
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

	// Where an announcement waits for the clients that should hear
	// it. Built here because the runner publishes one too: a
	// conversation's new name belongs to the client whose
	// conversation it is, and to no other.
	owed := outboxmysql.New(db.DB)
	publishing := &outbox.Publisher{
		Store:   owed,
		Clients: func(ctx context.Context, userID string) ([]string, error) { return subscribers(ctx, chats, userID) },
	}

	chatRunner, err := runner.New(runner.Options{
		Repository:    chats,
		Messages:      chats,
		Announcements: announcements,
		Publish:       announcing{publishing, cfg.Assistant.Now},
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

	// The first work here that happens because of the clock rather than
	// because somebody asked. Stopped with the server, so a reminder is
	// never half said during a shutdown.
	here := whereabouts(cfg, logger.Logger)
	reminding := &remind.Loop{
		Store:    reminderStore,
		Presence: here,
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
		Alarm:    alarming(alarms),
		Logger:   logger.Logger,
	}
	remindCtx, stopReminders := context.WithCancel(context.Background())

	var watching sync.WaitGroup
	watching.Add(1)
	go func() {
		defer watching.Done()
		logging.Surviving(remindCtx, logger.Logger, "reminders", func() {
			reminding.Run(remindCtx)
		})
	}()
	// Registered in this order so they run in the other: stop first, then
	// wait for the pass in flight to finish.
	defer watching.Wait()
	defer stopReminders()

	// What is owed to the phone gets there behind the answer,
	// retrying, and survives a restart. Shares the reminder loop's
	// context and wait group, so it stops with it.
	if len(sending.Hands) > 0 {
		watching.Add(1)
		go func() {
			defer watching.Done()
			logging.Surviving(remindCtx, logger.Logger, "errands to the phone", func() {
				sending.Run(remindCtx)
			})
		}()
		logger.Info("errands go to the phone",
			slog.Bool("alarms for reminders", cfg.Assistant.Alarms),
			slog.Duration("looking every", errand.Tick),
			slog.Int("tries", errand.Tries))
	} else {
		logger.Info("nothing is sent to the phone, with nowhere to send it")
	}

	logger.Info("watching for reminders",
		slog.Duration("every", remind.DefaultEvery),
		slog.Duration("grace", remind.DefaultGrace))

	names := vocabularymysql.New(db)
	// What the assistant considered saying unasked, so that a watcher
	// which is working and one which has stopped do not look alike.
	remarked := noticemysql.New(db.DB)

	// A standing description of the person, rewritten from what they have
	// actually said. The memories are facts they gave once and none of
	// them says what somebody is like to talk to; this is the attempt at
	// the rest.
	// What they usually do, for the conditions that read a routine
	// rather than a device. Written by the same pass that rewrites
	// the standing description, so the two cannot disagree.
	usually := routinemysql.New(db.DB)

	describing := &profile.Builder{
		Said:     chats,
		Did:      deviceEvents,
		Diary:    describingDiary(googleLink, cfg),
		Asked:    chats,
		Replies:  chats,
		Where:    cfg.Assistant.Location,
		Memories: remembering.Store,
		Usually:  usually,
		Now:      cfg.Assistant.Now,
		Logger:   logger.Logger,
		// So the description says "their diary" rather than
		// "calendar_events". The registry already knows.
		Domain: func(name string) string {
			if t, ok := tools.Get(name); ok {
				return t.Domain
			}
			return ""
		},
		Ask: func(ctx context.Context, ask string) (string, error) {
			return askOnce(ctx, answerer, cfg.PlatformAI.Vendor, cfg.PlatformAI.Model,
				environment.PurposeProfile, ask)
		},
	}
	watching.Add(1)
	go func() {
		defer watching.Done()
		logging.Surviving(remindCtx, logger.Logger, "the daily description", func() {
			describeDaily(remindCtx, chats, describing, cfg.Assistant.Now, logger.Logger)
		})
	}()
	logger.Info("describing the person", slog.Duration("every", profileEvery))

	// The names they say, for speech recognition to expect. Read from
	// the same week of messages, because a name is only worth the
	// budget if it keeps coming up. Nothing here reaches the engines:
	// the machine with the microphones fetches this and decides what to
	// do with it.
	listening := &vocabulary.Builder{
		Said: chats,
		// The spellings the transcript cannot have. A contact name
		// read off the phone is how they spell it; the same name
		// heard through a microphone is a guess.
		Did:    deviceEvents,
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
		logging.Surviving(remindCtx, logger.Logger, "the daily listening", func() {
			listenDaily(remindCtx, chats, listening, cfg.Assistant.Now, logger.Logger)
		})
	}()
	logger.Info("collecting the names they say daily",
		slog.Duration("every", vocabularyEvery))

	// The readings a phone sends every few minutes, once settling has
	// had everything it will ever want from them.
	watching.Add(1)
	go func() {
		defer watching.Done()
		logging.Surviving(remindCtx, logger.Logger, "forgetting what is old", func() {
			forgetDaily(remindCtx, deviceEvents, cfg.Assistant.Now, logger.Logger)
		})
	}()
	logger.Info("forgetting old position readings daily",
		slog.Duration("every", forgetEvery), slog.Duration("keeping", event.Keep))

	// What is said at the door. A model writes it from what has
	// happened since the two of them last spoke, and falls back to one
	// of the fixed sentences when it cannot in time.
	//
	// The fast model, where one is configured. This runs with somebody
	// standing in a doorway: the budget is a couple of seconds, and a
	// better greeting that arrives after the moment has passed is not a
	// better greeting.
	// How the greeting is made. Nil is the plain one: a salutation
	// for the time of day with anything held back behind it, which
	// is what this said before a model was put in it and what it
	// says again now.
	var greeting *greet.Writer
	if strings.EqualFold(cfg.Assistant.Greeting, "written") {
		greeting = &greet.Writer{
			Logger: logger.Logger,
			Ask: func(ctx context.Context, ask string) (string, error) {
				model := cfg.PlatformAI.FastModel
				if model == "" {
					model = cfg.PlatformAI.Model
				}
				return askAs(ctx, answerer, cfg.PlatformAI.Vendor, model,
					persona.Voice(cfg.Assistant.Persona, cfg.Assistant.Name),
					environment.PurposeGreeting, ask)
			},
		}
		logger.Info("writing the greeting at the door",
			slog.String("model", cmp.Or(cfg.PlatformAI.FastModel, cfg.PlatformAI.Model)),
			slog.Duration("within", greet.Within))
	} else {
		logger.Info("greeting them with a fixed salutation",
			slog.String("greeting", cfg.Assistant.Greeting))
	}

	// Announcements are published to clients, which collect them
	// and decide for themselves whether anybody heard. The server
	// keeps no opinion about rooms: a client knows whether somebody
	// is in front of it and the server cannot.
	//
	// What is left here is the one question a client cannot answer,
	// because it is about a moment rather than a device -- whether
	// a thing has gone past being worth saying at all. Without it a
	// voice client switched off for a week comes back to a hundred
	// stale warnings.
	sweeping := &outbox.Sweeper{
		Store:   owed,
		Clients: everyClient(chats, cfg, logger.Logger),
		Now:     cfg.Assistant.Now,
		Logger:  logger.Logger,
	}
	watching.Add(1)
	go func() {
		defer watching.Done()
		logging.Surviving(remindCtx, logger.Logger, "the announcement sweeper", func() {
			sweeping.Run(remindCtx)
		})
	}()
	logger.Info("publishing announcements to clients",
		slog.Duration("sweeping stale ones every", outbox.Every))

	// A schedule, and the sequence of tasks it runs. New rules
	// only so far: nothing existing has been moved onto it, and
	// reminders move last because they are the feature most used
	// and a new engine is wrong in small ways for a week.
	scheduled := jobmysql.New(db.DB)
	// One reader, used by the conditions a rule checks and by the
	// triggers it waits for. Two would be two answers to the same
	// question.
	look := readings{events: deviceEvents, usually: usually,
		linked: map[string]func(context.Context, string) (string, time.Time, error){
			"link.google": googleHealth(googleLink),
		},
		where: cfg.Assistant.Location, now: cfg.Assistant.Now}

	running = &job.Runner{
		Store: scheduled,
		Doer: doing{
			tools: tools, speaker: speaker, written: announcements,
			to: publishing, events: deviceEvents, here: here, look: look,
			usually:  ordinarily(&profile.Reader{Memories: remembering.Store}, logger.Logger),
			answerer: answerer, cfg: cfg,
			now: cfg.Assistant.Now, logger: logger.Logger,
		},
		Readings: look,
		Journal:  keeping{notes: remarked},
		Where:    cfg.Assistant.Location,
		Now:      cfg.Assistant.Now,
		Logger:   logger.Logger,
	}
	// The one thing the separation must not lose. Nobody is told
	// at the time that the phone refused, because nobody asked it
	// then; so when the sender finally gives up, it says so.
	sending.Told = func(ctx context.Context, userID, said string) {
		o, err := outbox.New(outboxmysql.ID(), userID, conversation.ReminderAnnouncement, said,
			cfg.Assistant.Now())
		if err != nil {
			logger.Error("cannot write down that an alarm never arrived", slog.Any("error", err))
			return
		}
		if err := publishing.Publish(ctx, o); err != nil {
			logger.Error("cannot publish that an alarm never arrived", slog.Any("error", err))
		}
		if err := announcement.Speak(ctx, speaker, conversation.ReminderAnnouncement, said); err != nil {
			logger.Error("cannot say that an alarm never arrived", slog.Any("error", err))
		}
		announcements.Announced(ctx, userID, said)
	}

	watching.Add(1)
	go func() {
		defer watching.Done()
		logging.Surviving(remindCtx, logger.Logger, "the rule engine", func() {
			running.Run(remindCtx)
		})
	}()
	logger.Info("running what is scheduled",
		slog.Duration("every", job.Tick),
		slog.Duration("too late to bother", job.Stale))

	handler := api.New(api.Options{
		Logger:        logger.Logger,
		DB:            db,
		Arrivals:      running,
		Chats:         chats,
		Messages:      chats,
		Reminders:     reminderStore,
		Announcer:     speaker,
		Announcements: announcements,
		Greeting:      greeting,
		Spoke:         chats,
		Reported:      deviceEvents,
		Known:         &profile.Reader{Memories: remembering.Store},
		ThisMachine:   cfg.HomeAssistant.ThisMachine,
		// The same diary and lists the tools use, for a greeting that
		// can mention what is first thing tomorrow. Nil without
		// Google, and a greeting is then about what has happened.
		Diary:          greetingDiary(googleLink, cfg),
		Chores:         greetingChores(googleLink),
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
		Remarks:        remarked,
		Rules:          scheduled,
		Usually:        usually,
		Trying:         running,
		Owed:           owed,
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
			Thinking:           cfg.PlatformAI.Thinking,
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
	return asking{place.Geoapify{
		Key:     cfg.Geoapify.Key.Reveal(),
		URL:     cfg.Geoapify.URL,
		Places:  cfg.Geoapify.Places,
		Timeout: cfg.Geoapify.Timeout,
	}}
}

// phoneAlarm : Somewhere to ask for an alarm, or nothing.
//
// Untyped nil when there is nowhere to ask, for the usual reason: a
// typed one satisfies the interface and then fails on every call,
// and the honest answer on a server with no address for the phone
// is that it cannot reach the phone at all -- which is a different
// sentence from the alarm having failed, and the person deserves
// the right one.
//
// Retrying is no longer wrapped around this. What is owed to the
// phone is a row and a sender, so a phone that is asleep or off
// the tailnet costs nothing at all in the turn that asked.
func phoneAlarm(cfg config.Config, _ *slog.Logger) alarm.Setter {
	where := strings.TrimSpace(cfg.HomeAssistant.AlarmURL.Reveal())
	if where == "" {
		return nil
	}
	return alarm.Phone{
		URL:    where,
		OffURL: strings.TrimSpace(cfg.HomeAssistant.AlarmOffURL.Reveal()),
		By:     cfg.Assistant.Name,
	}
}

// alarmHands : How an alarm errand reaches the phone.
func alarmHands(phone alarm.Setter) errand.Hands {
	return errand.Hands{
		errand.AlarmSet: func(ctx context.Context, e errand.Errand) error {
			a, err := unwrap(e)
			if err != nil {
				return err
			}
			return phone.Set(ctx, a)
		},
		errand.AlarmClear: func(ctx context.Context, e errand.Errand) error {
			a, err := unwrap(e)
			if err != nil {
				return err
			}
			return phone.Clear(ctx, a)
		},
	}
}

// unwrap : The alarm an errand carries.
func unwrap(e errand.Errand) (alarm.Alarm, error) {
	var a alarm.Alarm
	if err := json.Unmarshal(e.Body, &a); err != nil {
		return alarm.Alarm{}, fmt.Errorf("that errand does not describe an alarm: %w", err)
	}
	return a, nil
}

// alarming : The reminder loop's narrower view of the same queue,
// or nothing when there is nowhere to ask.
func alarming(alarms reminders.Alarms) remind.Alarms {
	if alarms == nil {
		return nil
	}
	return alarms
}

// queue : Asking for an alarm, without waiting on the phone.
//
// What the reminder tools and the reminder loop hold instead of
// the phone itself.
type queue struct {
	to *errand.Sender
	// on : Whether reminders are kept as phone alarms at all.
	on bool
}

// On : Whether reminders are kept as phone alarms.
func (q queue) On() bool { return q.on && q.to != nil }

// Want : Asks for an alarm.
func (q queue) Want(ctx context.Context, userID string, a alarm.Alarm) error {
	return q.ask(ctx, userID, errand.AlarmSet, a, q.to.Now())
}

// Drop : Asks for one to be taken off.
func (q queue) Drop(ctx context.Context, userID string, a alarm.Alarm) error {
	return q.ask(ctx, userID, errand.AlarmClear, a, q.to.Now())
}

// Move : Takes one off and puts another on, in that order.
//
// The gap between them is a timestamp rather than a sleep. Two
// starts of the phone's clock activity back to back and the
// second is dropped in silence, and the second and a half that
// costs used to be spent inside somebody's turn.
func (q queue) Move(ctx context.Context, userID string, was, now alarm.Alarm) error {
	at := q.to.Now()
	if err := q.ask(ctx, userID, errand.AlarmClear, was, at); err != nil {
		return err
	}
	return q.ask(ctx, userID, errand.AlarmSet, now, at.Add(alarm.Settle))
}

// ask : Writes one errand down.
//
// Anything still waiting about the same reminder is dropped first:
// an alarm queued for a time that has since moved must never
// arrive.
func (q queue) ask(ctx context.Context, userID string, kind errand.Kind,
	a alarm.Alarm, after time.Time) error {
	if !q.On() || userID == "" {
		return nil
	}
	body, err := json.Marshal(a)
	if err != nil {
		return err
	}
	if kind == errand.AlarmClear {
		if _, err := q.to.Drop(ctx, userID, a.Key); err != nil {
			return err
		}
	}
	// Worth trying until the alarm's own moment. After that an
	// alarm for a time that has passed helps nobody.
	expires := a.At
	if !expires.After(after) {
		expires = after.Add(time.Minute)
	}
	made, err := errand.New(errandmysql.ID(), userID, kind, a.Key, body, after, expires)
	if err != nil {
		return err
	}
	return q.to.Ask(ctx, made)
}

// mapping : The map, or nothing when there is no key.
//
// Untyped nil for the usual reason: a typed one satisfies the
// interface and then fails on every call, and the honest answer on
// a server with no key is that it cannot look at a map.
func mapping(cfg config.Config) mapstool.Maps {
	if !cfg.Geoapify.Configured() {
		return nil
	}
	return place.Geoapify{
		Key:     cfg.Geoapify.Key.Reveal(),
		URL:     cfg.Geoapify.URL,
		Places:  cfg.Geoapify.Places,
		Timeout: cfg.Geoapify.Timeout,
	}
}

// asking : A place namer, as the settler wants one.
//
// Two identical structs and a line to copy between them, which is
// the price of internal/event not importing internal/place. Worth
// paying: the settler is about stays and knows nothing of whose
// map it is.
type asking struct{ namer place.Namer }

func (a asking) Name(ctx context.Context, lat, lon float64) (event.Place, error) {
	got, err := a.namer.Name(ctx, lat, lon)
	return event.Place{Name: got.Name, Address: got.Address, Metres: got.Metres}, err
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
// greetingDiary, greetingChores : The diary and the task lists as the
// greeting wants them, or nothing when Google is not configured.
//
// Separate from diaryOf and listsOf, which return the narrower
// interfaces the tools were written against. A typed nil would satisfy
// an interface and then fail on every call with a nil pointer, so the
// nil is returned untyped and the greeting is simply about what has
// happened instead.
func greetingDiary(link *google.Link, cfg config.Config) presence.Diary {
	if link == nil {
		return nil
	}
	return calendar.New(link, cfg.Assistant.Location, cfg.Assistant.Now)
}

func greetingChores(link *google.Link) presence.Chores {
	if link == nil {
		return nil
	}
	return tasks.New(link)
}

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

// searchingFast : How the web is looked up for somebody who is
// waiting -- the quick service, falling back to the patient one.
//
// There is no searchingPatient yet, and deliberately. The owner's
// split is that the loops nobody waits on use TinyFish directly, so
// they never spend an allowance a conversation may need; but no
// loop asks the web anything today, and a function with no caller
// is a guess about what the next one will want. When one exists it
// is tinyfish.New and nothing else.
//
// Untyped nil when neither key is set, for the reason given below
// about Google: a typed nil satisfies the interface and then fails
// on every call, and the honest answer on a server with no keys is
// that it cannot reach the web.
func searchingFast(cfg config.Config, logger *slog.Logger) search.Service {
	// TinyFish first, which is the opposite of how this was built.
	// The published benchmark had Firecrawl at 510ms against 2.62s,
	// and measured from this machine over nine real searches it is
	// the other way round: TinyFish won seven of them, median 1.95s
	// against 3.3s cold and 0.96s against 1.36s warm. The benchmark
	// was almost certainly run from a US datacentre and this is
	// Chennai.
	//
	// So the one with no monthly allowance is also the quick one,
	// and asking Firecrawl first would spend a capped resource to
	// be slower. It is the fallback instead: for TinyFish's thirty
	// a minute, and for its being down.
	quick := tinyfish.New(string(cfg.Search.TinyFish), cfg.Search.Quick)
	spare := firecrawl.New(string(cfg.Search.Firecrawl), cfg.Search.Patient)

	switch {
	case quick == nil && spare == nil:
		return nil
	case spare == nil:
		// Nothing to fall back to, so nothing is held back from it
		// either: the quick one answers or the search fails.
		return quick
	case quick == nil:
		return spare
	}
	return &search.Pair{
		Quick: quick, Spare: spare,
		Within: cfg.Search.Quick, Rested: cfg.Search.Rested,
		Now: cfg.Assistant.Now, Logger: logger,
	}
}

// comingDiary : The diary as the look-ahead wants it, or nothing when
// Google is not configured. Untyped nil for the reason given above.
func comingDiary(link *google.Link, cfg config.Config) coming.Diary {
	if link == nil {
		return nil
	}
	return calendar.New(link, cfg.Assistant.Location, cfg.Assistant.Now)
}

// describingDiary : The calendar the description reads, or nothing.
//
// Untyped nil without Google, so a description is written from what
// happened rather than from a diary that answers every call with an
// error.
func describingDiary(link *google.Link, cfg config.Config) profile.Diary {
	if link == nil {
		return nil
	}
	return calendar.New(link, cfg.Assistant.Location, cfg.Assistant.Now)
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
//
// googleHealth : Whether the Google permission still works, and
// since when.
//
// Broken is what the account itself records rather than anything
// worked out here: a refresh token dies for reasons nobody on
// this side can see, and the one thing that finds out is the call
// that failed.
//
// No connection configured reads as working, not broken. Nothing
// is wrong with a permission nobody asked for, and a rule warning
// about one would be wrong on every server that has none.
func googleHealth(link *google.Link) func(context.Context, string) (string, time.Time, error) {
	return func(ctx context.Context, userID string) (string, time.Time, error) {
		if link == nil {
			return job.Working, time.Time{}, nil
		}
		account, err := link.Account(ctx, userID)
		if err != nil || account == nil {
			return job.Working, time.Time{}, nil
		}
		if account.BrokenAt != nil {
			return job.Broken, *account.BrokenAt, nil
		}
		return job.Working, time.Time{}, nil
	}
}

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
	// Asked for off on 3 October 2026, once the answer began being
	// spoken as it was written. Nothing is built rather than built and
	// not used, so the absence is visible in the startup log.
	if !cfg.HomeAssistant.SayAside {
		logger.Info("nothing is said while an answer is being worked out, by configuration")
		return nil
	}

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
func startingPersona(ctx context.Context, repo store.Repository, cfg config.Config, logger *slog.Logger) string {
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
// Hourly, not daily. A description is read before every answer, and
// the failure that matters is it saying something that stopped being
// true this morning -- a routine they have dropped, a person they have
// stopped mentioning. Daily meant living with that for a day.
//
// Hourly is affordable only because most hours do nothing: the builder
// declines to rebuild anybody who has said nothing since it last
// described them, so an idle night costs twenty-four cheap reads and
// no model call at all. Without that this would be twenty-four prompts
// a day each carrying a week of messages and four weeks of events.
const profileEvery = time.Hour

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

// forgetEvery, forgetFirst : How often old readings are dropped, and how
// long after startup the first pass runs.
//
// Daily, because what it deletes arrives at 288 rows a day and a day's
// worth is nothing to carry. Twenty minutes after startup rather than
// immediately: the first pass after this ships has the whole backlog to
// get through, and a server's first minutes are better spent answering.
const (
	forgetEvery = 24 * time.Hour
	forgetFirst = 20 * time.Minute
)

// forgetTimeout : How long one pass may take.
//
// Generous for the same reason the pass is batched: nobody is waiting,
// and the first one has a backlog.
const forgetTimeout = 10 * time.Minute

// forgetDaily : Drops position readings older than event.Keep, for as
// long as ctx lives.
//
// Only location.fix, and deliberately only that. Settling never reads
// further back than event.Looking, nothing else reads them at all, and
// they arrive at 288 rows a day for ever. What they are turned into --
// place.stayed, and the crossings of a geofence -- is the history and is
// kept.
//
// A failure is logged and the next day tries again. There is nothing to
// recover: the rows are still there and a day late is a day of rows.
func forgetDaily(
	ctx context.Context,
	forgetting interface {
		Forget(ctx context.Context, kind string, before time.Time) (int64, error)
	},
	now func() time.Time,
	logger *slog.Logger,
) {
	timer := time.NewTimer(forgetFirst)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		timer.Reset(forgetEvery)

		pass, cancel := context.WithTimeout(ctx, forgetTimeout)
		before := now().Add(-event.Keep)
		gone, err := forgetting.Forget(pass, event.Fixed, before)
		cancel()
		if err != nil {
			logger.Warn("cannot forget old position readings",
				slog.Any("error", err), slog.Int64("forgotten_first", gone))
			continue
		}
		if gone == 0 {
			continue
		}
		logger.Info("forgot old position readings",
			slog.Int64("rows", gone), slog.Time("older_than", before))
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
	return askAs(ctx, env, vendor, model, "", why, ask)
}

// askAs : The same, with a system prompt of its own.
//
// For the calls that are not the assistant answering somebody. The
// configured system prompt describes an assistant that acts, and most
// of it is about tools -- when one must have run, what to do with what
// one returned. Handed that and asked to greet somebody at a door, it
// answered "let me check the calendar before saying anything" and
// wrote out a tool call that does not exist. No tools are passed on
// any of these; the rules saying to use them should not be either.
func askAs(
	ctx context.Context,
	env environment.Environment,
	vendor, model, system string,
	why environment.Purpose,
	ask string,
) (string, error) {
	stream, err := env.Run(ctx, environment.Request{
		Prompt: ask, Purpose: why, Vendor: vendor, Model: model,
		SystemPrompt: system,
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

// settingOff : Says when to leave for something with an address.
//
// Everything that decides lives in internal/leaving. This is only
// the wiring: where the diary comes from, how far away a place is,
// who is in the room, and where the words go.
//
// The one announcement on the list that the person could not work
// out faster themselves. They know the film is at ten past six;
// what they do not know, standing in the kitchen, is that the road
// is busy tonight.
// settingOff : The watcher the leaving tool holds, or nothing.
//
// Built once and kept, because its memory of what it has already
// said is a map in the process. A watcher made per call would
// have no memory at all, and the leave-by sentence counts down --
// a different sentence every look -- so nothing comparing words
// could stop it repeating.
//
// It no longer says anything itself. Say is a sink: the words go
// back to the tool, and the rule's relay step publishes them.
func settingOff(
	cfg config.Config,
	diary coming.Diary,
	maps mapstool.Maps,
	reported interface {
		Recent(ctx context.Context, userID string, q event.Query) ([]event.Event, error)
	},
	logger *slog.Logger,
) *leaving.Watcher {
	// Two things are needed and either missing turns the feature
	// off rather than half on. Said once at startup, because a
	// silent assistant and a broken one look identical.
	if diary == nil || maps == nil {
		logger.Info("saying nothing about when to set off, with no diary or no map")
		return nil
	}

	off := &leaving.Watcher{
		Diary: func(ctx context.Context, userID string, from, to time.Time) ([]leaving.Appointment, error) {
			found, _, err := diary.Everywhere(ctx, userID, from, to)
			if err != nil {
				return nil, err
			}
			out := make([]leaving.Appointment, 0, len(found))
			for _, e := range found {
				// All day is not a thing to set off for, and an
				// entry with nowhere to go has nothing to work out.
				if e.AllDay || strings.TrimSpace(e.Where) == "" {
					continue
				}
				out = append(out, leaving.Appointment{
					What: e.Title, When: e.Starts, Where: e.Where,
				})
			}
			return out, nil
		},
		Journey: func(ctx context.Context, userID, where string) (time.Duration, error) {
			return howFar(ctx, maps, reported, userID, where)
		},
		// A sink. Look returns the sentence as well, and the
		// rule's relay step is what publishes it.
		Say: func(context.Context, string, string) error { return nil },
		// Always, because presence is the client's now: the server
		// publishes a copy per client and each decides for itself
		// whether anybody heard.
		Here: func(string) (bool, bool) { return true, true },
		Caution: func(ctx context.Context, userID string) string {
			return lowOnCharge(ctx, reported, userID)
		},
		Now:    cfg.Assistant.Now,
		Logger: logger,
	}

	logger.Info("ready to say when to set off",
		slog.Duration("soon", leaving.Soon),
		slog.Duration("door", leaving.Door))
	return off
}

// howFar : How long it takes to get from where they are to a place
// the diary named.
//
// Two calls to the map, and the first is the one that can be wrong:
// an address out of a calendar is written by whoever made the entry
// and may be anything. Where it cannot be found there is no journey
// and nothing is said, which is right -- a guessed destination gives
// a confident time to the wrong place.
func howFar(ctx context.Context, maps mapstool.Maps,
	reported interface {
		Recent(ctx context.Context, userID string, q event.Query) ([]event.Event, error)
	},
	userID, where string) (time.Duration, error) {

	lat, lon, ok := lastSeenAt(ctx, reported, userID)
	if !ok {
		return 0, fmt.Errorf("leaving: nothing says where they are")
	}
	found, err := maps.Find(ctx, where, mapstool.Country, 1)
	if err != nil {
		return 0, err
	}
	if len(found) == 0 {
		return 0, fmt.Errorf("leaving: no such place as %q", where)
	}
	journey, err := maps.Route(ctx, lat, lon, found[0].Lat, found[0].Lon, place.Driving)
	if err != nil {
		return 0, err
	}
	return journey.Takes, nil
}

// Spare : The charge below which it is worth a word on the way out.
//
// Above the thresholds the phone shouts about on its own, which
// stop at twenty-five, because this is a different question. Not
// "is it about to die" but "will it last the trip", and the moment
// to ask is the one moment they are about to go and are still in
// the room to hear the answer.
const Spare = 40

// lowOnCharge : That the phone has not much left, in words, or
// nothing.
//
// The charger half is the thing the phone used to decide for
// itself, with a variable kept by two Tasker tasks. It is worked
// out here now, from the events the phone already sends, which is
// the direction the owner asked for: the phone reports facts and
// the server decides what they mean together. The phone's own rule
// stays where it is until this has proved itself.
//
// Says nothing it is not sure of. No reading, or a reading older
// than the last hour, is no caution: a number from this morning
// says nothing about the phone in their pocket now.
func lowOnCharge(ctx context.Context, events interface {
	Recent(ctx context.Context, userID string, q event.Query) ([]event.Event, error)
}, userID string) string {
	if events == nil {
		return ""
	}
	level, err := events.Recent(ctx, userID, event.Query{Kind: "battery.level", Limit: 1})
	if err != nil || len(level) == 0 {
		return ""
	}
	if time.Since(level[0].OccurredAt) > time.Hour {
		return ""
	}
	var reading struct {
		Value string `json:"value"`
	}
	if json.Unmarshal(level[0].Payload, &reading) != nil {
		return ""
	}
	percent, err := strconv.Atoi(strings.TrimSpace(reading.Value))
	if err != nil || percent <= 0 || percent >= Spare {
		return ""
	}
	// On charge it will be better by the time they go, and saying so
	// is a warning about a problem that is solving itself.
	if plugged(ctx, events, userID) {
		return ""
	}
	return "your phone is at " + strconv.Itoa(percent) + " per cent"
}

// plugged : Whether the newest thing the phone said about its
// charger was that it had one.
func plugged(ctx context.Context, events interface {
	Recent(ctx context.Context, userID string, q event.Query) ([]event.Event, error)
}, userID string) bool {
	found, err := events.Recent(ctx, userID, event.Query{Prefix: "power.", Limit: 1})
	if err != nil || len(found) == 0 {
		return false
	}
	return found[0].Kind == "power.plugged"
}

// lastSeenAt : Where they were last reported to be.
//
// The newest stay, which is the one thing that carries a coordinate
// and a name together. How long they have been there is not asked
// for and must not be: that number has been wrong -- a stay at home
// reporting nearly fourteen hours straight through an evening out,
// because the phone reports leaving a place and never reports
// coming back -- and a journey does not need it.
func lastSeenAt(ctx context.Context, reported interface {
	Recent(ctx context.Context, userID string, q event.Query) ([]event.Event, error)
}, userID string) (lat, lon float64, ok bool) {
	if reported == nil {
		return 0, 0, false
	}
	found, err := reported.Recent(ctx, userID, event.Query{Kind: event.Stayed, Limit: 1})
	if err != nil || len(found) == 0 {
		return 0, 0, false
	}
	var into struct {
		At string `json:"at"`
	}
	if json.Unmarshal(found[0].Payload, &into) != nil {
		return 0, 0, false
	}
	return event.LatLon(into.At)
}

// subscribed : Who has spoken lately, and what they speak through.
//
// The two questions the announcement plumbing asks, and nothing
// else from the repository behind them.
type subscribed interface {
	Talkers(ctx context.Context, since time.Time) ([]string, error)
	ListClients(ctx context.Context, userID string, revoked bool) ([]client.Client, error)
}

// subscribers : Every client of this person, which is every client
// subscribed to their announcements.
//
// All of them, rather than a list somebody maintains. A client that
// is registered is a thing the person uses, and an announcement it
// cannot act on costs one row and is swept up later -- cheaper than
// the first time something is published to nobody because a new
// client was not added to a list.
func subscribers(ctx context.Context, clients subscribed, userID string) ([]string, error) {
	known, err := clients.ListClients(ctx, userID, false)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(known))
	for _, c := range known {
		out = append(out, c.ID)
	}
	return out, nil
}

// everyClient : Which clients the sweeper looks over.
//
// Every client of everybody who has spoken lately, which is the
// same reach the watchers have. A client belonging to somebody who
// has not been here in a month has nothing pending worth sweeping.
func everyClient(talkers subscribed, cfg config.Config, logger *slog.Logger,
) func(context.Context) ([]string, error) {
	return func(ctx context.Context) ([]string, error) {
		people, err := talkers.Talkers(ctx, cfg.Assistant.Now().Add(-chat.Active))
		if err != nil {
			return nil, err
		}
		var out []string
		for _, person := range people {
			ids, err := subscribers(ctx, talkers, person)
			if err != nil {
				logger.Warn("cannot list a person's clients", slog.Any("error", err))
				continue
			}
			out = append(out, ids...)
		}
		return out, nil
	}
}

// announcing : Turns what a piece of logic decided into an
// announcement, and publishes it.
//
// The adapter between the two halves. A piece of logic says what
// happened, which kind it is and which client caused it; it never
// learns what an outbox is, who else has a client, or whether
// anybody is in the room.
type announcing struct {
	to  *outbox.Publisher
	now func() time.Time
}

// Publish : Puts one announcement out.
func (a announcing) Publish(ctx context.Context, from, userID string,
	kind conversation.Kind, text string) error {
	o, err := outbox.New(outboxmysql.ID(), userID, kind, text, a.now())
	if err != nil {
		return err
	}
	o.From = from
	return a.to.Publish(ctx, o)
}

// doing : Carries out one step of a job.
//
// The runner owns order, lateness and failure; what a step actually
// does is here, because it is the only place that holds both the
// tool registry and the way out to a room.
//
// Two kinds of step for now. A prompt handed to the model is the
// next one, and it needs a list of the tools such a step may use
// before it is safe to add: a scheduled task with all fifty-two
// could cancel reminders at three in the morning.
type doing struct {
	tools   *tool.Registry
	speaker announce.Announcer
	written *announcement.Writer
	to      *outbox.Publisher
	events  *eventmysql.Store
	here    remind.Presence
	// look : Where a condition's readings come from. The runner's
	// own, so that a check and a trigger read the same sources --
	// including the routine, which a reader built here would not
	// have.
	look job.Readings
	// usually : The shape of this person's ordinary day, from the
	// standing description. Nil leaves a rule writing without it,
	// which is how every rule wrote before this.
	usually  func(ctx context.Context, userID string) string
	answerer environment.Environment
	cfg      config.Config
	now      func() time.Time
	logger   *slog.Logger
}

// Do : Carries out one step. Never stops the sequence yet -- the
// step that can is the checking one, which comes with prompts.
// heard : Which kind a rule's announcement is, for where it should go.
//
// The moment is the same either way -- it fires because something has
// just become true. All that differs is whether being out of the room
// means missing it, and the rule is what knows: see [job.Job.Anywhere].
func heard(anywhere bool) conversation.Kind {
	if anywhere {
		return conversation.Reaching
	}
	return conversation.Immediate
}

func (d doing) Do(ctx context.Context, run job.Turn, s job.Step) (job.Outcome, error) {
	if err := s.Valid(); err != nil {
		return job.Outcome{}, err
	}

	switch s.Do {
	case job.Announce:
		// A trial says what it would say and says nothing. The
		// sentence is fixed, so there is nothing to work out and
		// nothing to hold back but the announcing itself.
		if run.Trying {
			return job.Outcome{Said: s.Say}, nil
		}
		// Published, which is where announcements go now, and
		// spoken, which is the only way one is currently heard.
		// The second half goes when a client collects.
		kind := heard(run.Anywhere)
		o, err := outbox.Keeping(outboxmysql.ID(), run.UserID,
			kind, s.Say, d.now(), run.Worth)
		if err != nil {
			return job.Outcome{}, err
		}
		if err := d.to.Publish(ctx, o); err != nil {
			return job.Outcome{}, err
		}
		if err := announcement.Speak(ctx, d.speaker, kind, s.Say); err != nil {
			return job.Outcome{}, err
		}
		d.written.Announced(ctx, run.UserID, s.Say)
		return job.Outcome{Said: s.Say}, nil

	case job.Relay:
		return d.relay(ctx, run)

	case job.Write:
		return d.write(ctx, run, s)

	case job.Check:
		return d.check(ctx, run.UserID, s)

	case job.Call:
		// As the person, on the typed channel. A job is not a
		// voice turn: nothing is waiting in a room for it, and the
		// tools that withhold themselves from a speaker should not
		// withhold themselves here.
		out := d.tools.Call(ctx, s.Tool, tool.Invocation{
			Caller: tool.Caller{UserID: run.UserID, Channel: chat.ChannelDirect},
			Args:   s.Args,
			// What the sequence has already run. The tools refuse
			// to write something they have not read in this turn,
			// and a sequence is one turn.
			Ran: job.Tools(run.Already),
		})
		if out.Outcome == conversation.OutcomeFailed {
			return job.Outcome{}, fmt.Errorf("%s: %s", s.Tool, out.Content)
		}
		d.logger.InfoContext(ctx, "a job ran a tool",
			slog.String("tool", s.Tool), slog.String("outcome", string(out.Outcome)))

		// A tool may end the sequence by finding nothing, in the
		// same word a write step uses for it. Without this a rule
		// that looks for something unusual every two minutes asks
		// a model to say nothing seven hundred times a day: the
		// looking is cheap and deterministic, and the sentence is
		// neither.
		if prompt.Nothing(out.Content) {
			return job.Outcome{Stop: true, Why: s.Tool + " found nothing", Said: run.Before}, nil
		}

		// Kept for the steps after it. A write step composes its
		// sentence out of whatever the sequence just found.
		return job.Outcome{Got: out.Content}, nil
	}
	return job.Outcome{}, fmt.Errorf("job: nothing carries out %q", s.Do)
}

// keeping : Writes each run of a rule where the person can see it.
//
// The same record the unasked remarks already had, and the page
// built on it: *"I should see in UI when it triggered, why it
// didn't speak, if spoken what it spoke."* That question is the
// same one whether the thing deciding was a watcher written in Go
// or a rule in a row, so it is answered from the same table.
type keeping struct{ notes notice.Notes }

// Wrote : Records one run.
func (k keeping) Wrote(ctx context.Context, r job.Record) error {
	if k.notes == nil {
		return nil
	}
	return k.notes.Note(ctx, notice.Noted{
		UserID:  r.UserID,
		At:      r.At,
		Label:   r.Name,
		Reason:  notice.Immediate,
		Outcome: came(r),
		Said:    r.Said,
		Because: r.Why,
	})
}

// came : What a run came to, in the words the record already uses.
func came(r job.Record) notice.Outcome {
	switch {
	case r.Failed:
		return notice.Failed
	case r.Said != "":
		return notice.Said
	case r.Stopped:
		// Nothing was said and nothing went wrong: a condition did
		// not hold, or the model found nothing worth saying. Which
		// of the two is in Because.
		return notice.Declined
	}
	return notice.Held
}

// readings : What an event kind has lately said.
//
// The one place a rule's view of a reading is built, used both by
// a check step and by the runner deciding whether a watching rule
// has just crossed its line.
type readings struct {
	events *eventmysql.Store
	// usually : What the person usually does, for the conditions
	// that read a routine rather than a device. Nil leaves those
	// conditions unknown, which is how they fail: a rule standing
	// on a routine nobody has worked out yet does not fire.
	usually routine.Store
	// linked : How a connection to somewhere else is doing, by the
	// name a gate calls it. Nil leaves those conditions unknown,
	// and an unknown reading never passes.
	linked map[string]func(ctx context.Context, userID string) (string, time.Time, error)
	// where : Their zone, which is what an hour means.
	where *time.Location
	// now : The clock, so the hour a routine describes is placed on
	// today.
	now func() time.Time
}

// Latest : The most recent readings of a kind, newest first.
func (r readings) Latest(ctx context.Context, userID, kind string, n int) ([]job.Reading, error) {
	if routine.Mine(kind) {
		return r.usual(ctx, userID, kind)
	}
	if job.Linked(kind) {
		return r.connection(ctx, userID, kind)
	}
	found, err := r.events.Recent(ctx, userID, event.Query{Kind: kind, Limit: n})
	if err != nil {
		return nil, err
	}
	out := make([]job.Reading, 0, len(found))
	for _, e := range found {
		out = append(out, job.Reading{
			Value: event.Value(e.Payload),
			At:    e.ReceivedAt,
			Known: true,
		})
	}
	return out, nil
}

// usual : A routine as a reading, placed on today.
//
// One reading and never two. The crossing test asks for the
// previous one to decide whether a thing has just become true, and
// a routine never becomes true -- it is a time of day. Returning a
// single reading is what makes [job.Watched] right to refuse these
// as triggers: there is nothing here to cross.
//
// The value is the hour and the time is the hour, which is what
// lets "older than 2h" and "within 30m" read as two hours past
// when they normally go and the half hour after it, with no new
// word in the grammar.
func (r readings) usual(ctx context.Context, userID, kind string) ([]job.Reading, error) {
	if r.usually == nil {
		return nil, nil
	}
	held, err := r.usually.All(ctx, userID)
	if err != nil {
		return nil, err
	}
	for _, f := range held {
		if !strings.EqualFold(f.Name(), kind) {
			continue
		}
		return []job.Reading{{
			Value: strconv.Itoa(f.Hour),
			At:    f.Today(r.clock(), r.where),
			Known: true,
		}}, nil
	}
	// Nothing known. Not an error: a place they have only just
	// started going to has no hour yet, and a condition on it
	// should quietly not hold rather than stop the rule.
	return nil, nil
}

// connection : How a link to somewhere else is doing, as a
// reading.
//
// One reading, like a routine: a connection does not arrive and
// there is nothing to cross. Its time is when it broke, so a rule
// can ask how long it has been down with the operators that are
// already there.
//
// A name nothing answers to is unknown rather than an error. A
// rule asking about a connection this server was never configured
// for should quietly not hold, the same as one asking about a
// kind no device reports.
func (r readings) connection(ctx context.Context, userID, kind string) ([]job.Reading, error) {
	ask, ok := r.linked[strings.ToLower(strings.TrimSpace(kind))]
	if !ok || ask == nil {
		return nil, nil
	}
	state, since, err := ask(ctx, userID)
	if err != nil {
		return nil, err
	}
	if since.IsZero() {
		since = r.clock()
	}
	return []job.Reading{{Value: state, At: since, Known: true}}, nil
}

// clock : Now.
func (r readings) clock() time.Time {
	if r.now == nil {
		return time.Now()
	}
	return r.now()
}

// Rounds : How many times a write step may call tools before it
// has to answer.
//
// A rule runs unattended, possibly every minute. Four rounds is
// more than any sentence has needed and small enough that a rule
// which has started arguing with itself stops on its own.
const Rounds = 4

// relay : Says what the tool before it returned.
//
// No model. The words are already settled by whatever produced
// them, and a rule whose tool writes its own sentence should not
// pay for that sentence twice.
func (d doing) relay(ctx context.Context, run job.Turn) (job.Outcome, error) {
	var said string
	for _, r := range run.Already {
		if text := strings.TrimSpace(r.Got); text != "" {
			said = text
		}
	}
	if said == "" || prompt.Nothing(said) {
		return job.Outcome{Stop: true, Why: "there was nothing to pass on", Said: run.Before}, nil
	}
	if same(said, run.Before) {
		d.logger.InfoContext(ctx, "a job had nothing new to pass on")
		return job.Outcome{Stop: true, Why: "it had already said that", Said: said}, nil
	}
	if err := d.publish(ctx, run, said); err != nil {
		return job.Outcome{}, err
	}
	d.logger.InfoContext(ctx, "a job passed on what it found")
	return job.Outcome{Said: said}, nil
}

// publish : Writes an announcement down, says it, and records it.
//
// The speaking is the half that goes when every client collects.
func (d doing) publish(ctx context.Context, run job.Turn, said string) error {
	// A trial has already worked the sentence out, which is the
	// expensive and interesting half. Publishing it is the half
	// that would reach somebody, and is the only half held back.
	if run.Trying {
		return nil
	}
	userID, worth := run.UserID, run.Worth
	kind := heard(run.Anywhere)
	o, err := outbox.Keeping(outboxmysql.ID(), userID,
		kind, said, d.now(), worth)
	if err != nil {
		return err
	}
	if err := d.to.Publish(ctx, o); err != nil {
		return err
	}
	if err := announcement.Speak(ctx, d.speaker, kind, said); err != nil {
		return err
	}
	d.written.Announced(ctx, userID, said)
	return nil
}

// write : Asks the model for the sentence, then says it.
//
// The model is given the instruction, whatever the earlier steps
// found, and the tools the rule named -- which it may call for
// itself, with arguments it works out from the question in front
// of it.
//
// Named tools rather than all of them. What a rule can reach
// stays readable off the row, which was the point of not handing
// over the toolbox; what it could not do before was decide at the
// time, and a rule written on Monday cannot know what Thursday
// needs.
func (d doing) write(ctx context.Context, run job.Turn, s job.Step) (job.Outcome, error) {
	specs, bad := d.offered(s.Tools)
	if bad != nil {
		return job.Outcome{}, bad
	}
	answer, err := d.converse(ctx, run.UserID,
		writing(s.Ask, run.Already, run.Before, d.routine(ctx, run.UserID)), specs)
	if err != nil {
		return job.Outcome{}, err
	}

	// Silence is an answer. Most looks find nothing, and a rule
	// that had to say something every time it ran would be worse
	// than no rule.
	if prompt.Nothing(answer) {
		d.logger.InfoContext(ctx, "a job had nothing to say",
			slog.String("ask", s.Ask))
		// The memory is carried, not cleared. Two silences look
		// alike here and are not: a check that did not hold means
		// the thing went away, and the rule should speak again
		// when it comes back; this one means the thing is still
		// true and has already been said. Forgetting now would
		// have it announced afresh on the next run, which is the
		// repetition this is for.
		return job.Outcome{
			Stop: true,
			Why:  "there was nothing new to say",
			Said: run.Before,
		}, nil
	}

	said := strings.TrimSpace(answer)

	// The same sentence as last time is the same news as last
	// time. A rule that notices the phone has gone quiet notices
	// it again ten minutes later, and would go on noticing it for
	// as long as the phone stayed quiet.
	//
	// The sentence is the key, which is why this needs nothing to
	// identify the occasion: two runs that mean the same thing say
	// the same words, and two that mean different things do not.
	// Thursday's warning and Friday's are both said; Thursday's
	// twice is said once.
	// A backstop behind the asking. The model is told what it said
	// last time and asked to judge; this catches the case where it
	// judged wrongly and handed back the same words.
	if same(said, run.Before) {
		d.logger.InfoContext(ctx, "a job had nothing new to say",
			slog.String("ask", s.Ask))
		return job.Outcome{Stop: true, Why: "it had already said that", Said: said}, nil
	}

	// The sentence is worked out either way, which is the half
	// worth seeing. Only the saying of it is held back.
	if run.Trying {
		return job.Outcome{Said: said}, nil
	}

	if err := d.publish(ctx, run, said); err != nil {
		return job.Outcome{}, err
	}
	d.logger.InfoContext(ctx, "a job wrote what to say", slog.String("ask", s.Ask))
	return job.Outcome{Said: said}, nil
}

// offered : The tools a rule named, as the model is shown them.
//
// Reading tools only, checked here as well as when the rule was
// written: a tool can gain a write between the two.
func (d doing) offered(names []string) ([]environment.ToolSpec, error) {
	if len(names) == 0 {
		return nil, nil
	}
	address := persona.AddressFor(d.cfg.Assistant.Persona)
	out := make([]environment.ToolSpec, 0, len(names))
	for _, name := range names {
		found, ok := d.tools.Get(name)
		if !ok {
			return nil, fmt.Errorf("a rule names %q, which is not a tool", name)
		}
		if found.Writes {
			return nil, fmt.Errorf("a rule names %q, which changes things", name)
		}
		schema, err := tool.Narrated(found.Params, address).MarshalJSON()
		if err != nil {
			return nil, fmt.Errorf("the schema for %s will not render: %w", name, err)
		}
		out = append(out, environment.ToolSpec{
			Name:        name,
			Description: tool.NarratedDescription(found.Description(), address),
			Parameters:  schema,
		})
	}
	return out, nil
}

// converse : Asks, runs whatever tools come back, and asks again.
//
// The question is put once and then lives in the history, because
// a model asked it twice answers it twice.
func (d doing) converse(ctx context.Context, userID, ask string,
	specs []environment.ToolSpec) (string, error) {
	var (
		history []environment.Turn
		ran     []string
		prompt  = ask
	)

	for round := 0; round < Rounds; round++ {
		stream, err := d.answerer.Run(ctx, environment.Request{
			Prompt:       prompt,
			History:      history,
			Purpose:      environment.PurposeRemark,
			Vendor:       d.cfg.PlatformAI.Vendor,
			Model:        cmp.Or(d.cfg.PlatformAI.FastModel, d.cfg.PlatformAI.Model),
			SystemPrompt: persona.Voice(d.cfg.Assistant.Persona, d.cfg.Assistant.Name),
			Tools:        specs,
		})
		if err != nil {
			return "", err
		}

		// Drained to the end whatever it says, so the provider's
		// goroutine is never left blocked on a send.
		var last *environment.Message
		for msg := range stream {
			m := msg
			last = &m
		}
		switch {
		case last == nil:
			return "", fmt.Errorf("the provider said nothing")
		case last.Kind == environment.KindError:
			return "", fmt.Errorf("%s", last.Text)
		case last.Kind != environment.KindToolCalls:
			return last.Text, nil
		}

		if prompt != "" {
			history = append(history, environment.Turn{
				Role: environment.RoleUser, Text: prompt})
			prompt = ""
		}
		history = append(history, environment.Turn{
			Role: environment.RoleAssistant, ToolCalls: last.ToolCalls})

		results := make([]environment.ToolResult, 0, len(last.ToolCalls))
		for _, c := range last.ToolCalls {
			out := d.tools.Call(ctx, c.Name, tool.Invocation{
				Caller: tool.Caller{UserID: userID, Channel: chat.ChannelDirect},
				Args:   json.RawMessage(c.Arguments),
				Ran:    ran,
			})
			ran = append(ran, c.Name)
			d.logger.InfoContext(ctx, "a job's model called a tool",
				slog.String("tool", c.Name),
				slog.String("outcome", string(out.Outcome)))
			results = append(results, environment.ToolResult{ID: c.ID, Content: out.Content})
		}
		history = append(history, environment.Turn{
			Role: environment.RoleTool, ToolResults: results})
	}

	// Still calling tools after four rounds. Silence rather than a
	// sentence built on a conversation that never finished.
	return "", fmt.Errorf("a rule asked for tools %d times without answering", Rounds)
}

// same : Whether two sentences are the same news.
//
// Loosely, because a model asked the same question twice writes
// the same sentence with the spacing somewhere else. Case and
// runs of whitespace are not the difference between one piece of
// news and another.
func same(said, before string) bool {
	if said == "" || before == "" {
		return false
	}
	return strings.EqualFold(strings.Join(strings.Fields(said), " "),
		strings.Join(strings.Fields(before), " "))
}

// writing : What the model is asked, for a write step.
// ordinarily : Reads the shape of somebody's ordinary day out of
// the standing description.
//
// One section of it, not the whole. The description's other two
// parts are who is in their life and what they keep coming back
// to, which is for answering them; this is the part that says
// whether a thing happening now is worth interrupting them over.
//
// A failure is a line in the log and an empty string. A rule that
// cannot read the description writes the sentence it would have
// written before there was one.
func ordinarily(known *profile.Reader, logger *slog.Logger) func(context.Context, string) string {
	return func(ctx context.Context, userID string) string {
		described, err := known.Profile(ctx, userID)
		if err != nil {
			if logger != nil {
				logger.WarnContext(ctx, "writing without their ordinary day",
					slog.Any("error", err))
			}
			return ""
		}
		return profile.Split(described)[profile.Day]
	}
}

// routine : The shape of their ordinary day, or empty.
//
// A failure costs the sentence its context and not the sentence.
func (d doing) routine(ctx context.Context, userID string) string {
	if d.usually == nil {
		return ""
	}
	return d.usually(ctx, userID)
}

func writing(ask string, already []job.Ran, before string, usually string) string {
	found := make([]string, 0, len(already))
	for _, r := range already {
		if strings.TrimSpace(r.Got) == "" {
			continue
		}
		found = append(found, prompt.Lines(r.Tool+":", r.Got))
	}

	var what string
	if len(found) > 0 {
		what = prompt.Block(prompt.Text("This is what was looked up just now,",
			"and it is everything you have:"), prompt.Block(found...))
	}

	// Whether this is the same news is a judgement about meaning,
	// and the thing that wrote the sentence is the only thing that
	// can make it. Comparing the text was tried first and is not
	// enough: asked the same question three times a minute apart,
	// the model answered "at 22:48 on Tuesday the 6th of October"
	// twice and "at twenty-two forty-eight" in between. The same
	// news, three wordings, and one rephrasing was enough to make
	// the other two look new.
	var again string
	if strings.TrimSpace(before) != "" {
		again = prompt.Text(
			"The last time this rule ran, you said:", strconv.Quote(strings.TrimSpace(before)),
			"If what you have now is the same news as that, answer with "+prompt.Silence+".",
			"Say something only if it has changed or there is something new in it.",
			"Saying the same thing again in different words is still saying it again.",
		)
	}

	// What an ordinary day looks like for them, so that a rule can
	// tell an unusual thing from a Tuesday. It is also what says
	// when not to speak: three in the morning is a different hour
	// to be told something at, and nothing else here knows that.
	//
	// Their day and not the whole description. The rest is who is
	// in their life and what they talk about, which is for
	// answering them rather than for deciding whether to.
	var ordinary string
	if strings.TrimSpace(usually) != "" {
		ordinary = prompt.Block(
			prompt.Text(
				"This is what an ordinary day looks like for them, worked out from weeks of their own devices and their own words.",
				"Use it to judge whether what you have is worth saying: the same thing is remarkable at one hour and unremarkable at another, and a departure from this is only worth mentioning if something follows from it.",
				"It describes what usually happens and is not a rule they have agreed to. Never tell them what their routine is, never say they are late by it, and never treat a day that does not match it as a thing to be explained.",
			),
			strings.TrimSpace(usually),
		)
	}

	return prompt.Block(
		prompt.Text("You are writing one sentence to be said out loud, now.", ask),
		ordinary,
		// Nobody asked for this one, so nobody is listening when it
		// starts. A reply is heard from its first word because
		// somebody is waiting for it; an announcement arrives at
		// somebody in the middle of something else, and the first
		// few words are spent getting their attention rather than
		// carrying anything.
		prompt.Text(
			"This will be said to somebody who is not listening yet, and is not expecting it.",
			"So the thing that matters must not be the first thing in the sentence: whatever a person misses while they look up should be words they can afford to miss.",
			"Lead with what is happening in general, then the particular. Not \"twenty-four percent, sir\" but \"your battery is getting low, sir -- twenty-four percent\".",
			"A word is already being said in front of this to get their attention, so do not open with one of your own and do not address them at the start. Address them once, later in the sentence, if your manner calls for it at all.",
		),
		what,
		again,
		// A rule asking for the battery every minute was handed a
		// week of every kind of event and read out a reading from
		// the day before, as though it were current. Silence is
		// always available and a wrong number never is.
		prompt.Text("Say only what is in front of you, and nothing you have worked out around it.",
			"Readings carry the time they arrived: one from yesterday does not say what is true now.",
			"If what you were asked for is not there, answer with "+prompt.Silence+" rather than",
			"the nearest thing that is."),
		prompt.Text("Answer with the sentence and nothing else.",
			"If there is nothing worth saying, answer with "+prompt.Silence+"."),
	)
}

// check : Reads a condition and stops the sequence unless it holds.
//
// A reading is the latest event of that kind, timed by when this
// server was told rather than by the device's own clock, so that
// "older than" means what it says when a phone has stopped
// talking.
//
// One reading per kind the condition names, which is more than one
// when it has alternatives: a check satisfied by the battery or by
// where somebody is has to read both before it knows. The sources
// that read nothing at all -- presence, the hour, the day, a
// routine -- ask for no kind and are answered from the moment.
//
// Through the runner's own reader rather than a fresh one. Built
// here it is a reader with no routine store, and every condition
// on what somebody usually does would quietly never hold.
func (d doing) check(ctx context.Context, userID string, s job.Step) (job.Outcome, error) {
	conditions, err := job.ParseConditions(s.When)
	if err != nil {
		return job.Outcome{}, err
	}

	// One reading each, unless something is being compared with its
	// own past -- then enough of them to reach back that far. A
	// phone reports its level every few minutes, so a quarter of an
	// hour is a handful and a day would still be inside the cap.
	deep := 1
	if back := conditions.Deep(); back > 0 {
		deep = job.Over
	}

	got := map[string][]job.Reading{}
	for _, kind := range conditions.Needs() {
		latest, err := d.look.Latest(ctx, userID, kind, deep)
		if err != nil {
			return job.Outcome{}, err
		}
		got[kind] = latest
	}

	// Nil presence is the convention the reminder loop already
	// uses: nothing can tell, so nobody is held to be away.
	away := d.here != nil && d.here.Away(ctx, userID)

	// In their own zone, because an hour means nothing anywhere
	// else: ten o'clock is ten where they are standing. Lengths of
	// time are unaffected, being lengths rather than moments.
	passed := conditions.Passes(got, away, d.now().In(d.where()))
	d.logger.InfoContext(ctx, "a job checked a condition",
		slog.String("when", s.When), slog.Bool("passed", passed))
	if passed {
		return job.Outcome{}, nil
	}
	return job.Outcome{Stop: true, Why: "the check " + s.When + " did not pass"}, nil
}

// where : The person's zone, or UTC.
func (d doing) where() *time.Location {
	if d.cfg.Assistant.Location == nil {
		return time.UTC
	}
	return d.cfg.Assistant.Location
}
