// Command server runs the server.
package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"personal-assistant/internal/base/records/client"
	"personal-assistant/internal/base/records/store"
	"reflect"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"personal-assistant/internal/app/aloud"
	"personal-assistant/internal/app/api"
	"personal-assistant/internal/app/api/assist"
	"personal-assistant/internal/app/api/authn"
	"personal-assistant/internal/app/api/presence"
	"personal-assistant/internal/app/away"
	"personal-assistant/internal/app/catching"
	"personal-assistant/internal/app/elsewhere"
	"personal-assistant/internal/app/glance"
	"personal-assistant/internal/app/live"
	"personal-assistant/internal/app/noted"
	"personal-assistant/internal/app/noticing"
	"personal-assistant/internal/app/pattern"
	"personal-assistant/internal/app/pointing"
	"personal-assistant/internal/app/profile"
	"personal-assistant/internal/app/runner"
	"personal-assistant/internal/app/terminal"
	"personal-assistant/internal/app/tool"
	calendartool "personal-assistant/internal/app/tool/calendar"
	callingtool "personal-assistant/internal/app/tool/calling"
	comingtool "personal-assistant/internal/app/tool/coming"
	"personal-assistant/internal/app/tool/conversations"
	eventstool "personal-assistant/internal/app/tool/events"
	leavingtool "personal-assistant/internal/app/tool/leaving"
	mailtool "personal-assistant/internal/app/tool/mail"
	mapstool "personal-assistant/internal/app/tool/maps"
	"personal-assistant/internal/app/tool/memories"
	"personal-assistant/internal/app/tool/nearest"
	placestool "personal-assistant/internal/app/tool/places"
	"personal-assistant/internal/app/tool/reminders"
	"personal-assistant/internal/app/tool/rules"
	situationstool "personal-assistant/internal/app/tool/situations"
	taskstool "personal-assistant/internal/app/tool/tasks"
	webtool "personal-assistant/internal/app/tool/web"
	"personal-assistant/internal/base/platform/config"
	"personal-assistant/internal/base/platform/logging"
	"personal-assistant/internal/base/platform/storage"
	"personal-assistant/internal/base/records/chat"
	chatmysql "personal-assistant/internal/base/records/chat/mysql"
	"personal-assistant/internal/base/records/conversation"
	"personal-assistant/internal/base/thinking/embed"
	"personal-assistant/internal/base/thinking/embed/tei"
	"personal-assistant/internal/base/thinking/environment"
	"personal-assistant/internal/base/thinking/environment/platformai"
	"personal-assistant/internal/base/thinking/llm"
	"personal-assistant/internal/base/thinking/naming/gliner"
	"personal-assistant/internal/base/thinking/prompt"
	"personal-assistant/internal/base/thinking/search"
	"personal-assistant/internal/base/thinking/search/firecrawl"
	"personal-assistant/internal/base/thinking/search/tavily"
	"personal-assistant/internal/base/thinking/search/tinyfish"
	"personal-assistant/internal/base/world/alarm"
	"personal-assistant/internal/base/world/announce"
	"personal-assistant/internal/base/world/announce/hass"
	"personal-assistant/internal/base/world/calling"
	"personal-assistant/internal/base/world/errand"
	errandmysql "personal-assistant/internal/base/world/errand/mysql"
	"personal-assistant/internal/base/world/event"
	eventmysql "personal-assistant/internal/base/world/event/mysql"
	"personal-assistant/internal/base/world/events"
	"personal-assistant/internal/base/world/leaving"
	"personal-assistant/internal/base/world/notice"
	noticemysql "personal-assistant/internal/base/world/notice/mysql"
	"personal-assistant/internal/base/world/place"
	"personal-assistant/internal/base/world/routine"
	routinemysql "personal-assistant/internal/base/world/routine/mysql"
	"personal-assistant/internal/base/world/speech"
	"personal-assistant/internal/domain/announcement"
	"personal-assistant/internal/domain/calendar"
	"personal-assistant/internal/domain/coming"
	comingmysql "personal-assistant/internal/domain/coming/mysql"
	"personal-assistant/internal/domain/google"
	googlemysql "personal-assistant/internal/domain/google/mysql"
	"personal-assistant/internal/domain/greet"
	"personal-assistant/internal/domain/job"
	jobmysql "personal-assistant/internal/domain/job/mysql"
	"personal-assistant/internal/domain/mail"
	"personal-assistant/internal/domain/memory"
	memorymysql "personal-assistant/internal/domain/memory/mysql"
	"personal-assistant/internal/domain/outbox"
	outboxmysql "personal-assistant/internal/domain/outbox/mysql"
	"personal-assistant/internal/domain/persona"
	"personal-assistant/internal/domain/places"
	placesmysql "personal-assistant/internal/domain/places/mysql"
	"personal-assistant/internal/domain/remind"
	remindmysql "personal-assistant/internal/domain/remind/mysql"
	"personal-assistant/internal/domain/situation"
	"personal-assistant/internal/domain/tasks"
	"personal-assistant/internal/domain/usage"
	usagemysql "personal-assistant/internal/domain/usage/mysql"
	"personal-assistant/internal/domain/vocabulary"
	vocabularymysql "personal-assistant/internal/domain/vocabulary/mysql"
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

// started : When this process began, for the dashboard's health reading.
var started = time.Now()

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

	// Every call to a model, with what it cost, kept where every call
	// passes. Told to the clients once the live hub exists, below.
	calls := usagemysql.New(db)

	answerer, err := buildProvider(cfg, usage.Recorder{Store: calls, Logger: logger.Logger}, logger.Logger)
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
	// What says announcements: the person's own desktop app when it speaks
	// for itself, and Home Assistant's satellite when it does not. The
	// speaker stays the one for Home Assistant's states.
	voices := aloud.New(speaker, logger.Logger)
	// What the person is called, for the word said in front of an
	// announcement that arrives out of nowhere. One configured
	// persona for the server, so it is set here rather than carried
	// down to every caller.
	announcement.Addressing(persona.AddressFor(cfg.Assistant.Persona))

	// What the assistant has been asked to remember. Without an embedding
	// server it still works, matching words rather than meaning, which is
	// worse than the alternative and much better than going blind.
	// Held apart as well as handed over, because setting a memory
	// aside is a thing only the store can do and the recall
	// interface has no business learning about.
	// What the clients are told when somebody's data changes: which
	// area, from the store that wrote it, once the live hub exists --
	// the stores are built before it. Owner, 13 October 2026: *"all the
	// data should be reactive from the server."*
	var changeHub atomic.Pointer[live.Hub]
	changed := func(areas ...string) func(userID string) {
		return func(userID string) {
			if h := changeHub.Load(); h != nil {
				h.AreaChanged(userID, areas...)
			}
		}
	}

	// A chat is several calls in a few seconds; a settings page showing
	// them is told once in a while rather than once a call.
	calls.OnChange(usage.Throttled(usageSettle, changed(live.AreaUsage)))

	remembered := memorymysql.New(db).OnChange(changed(live.AreaMemories, live.AreaProfile))
	remembering := &memory.Recall{
		Store:      remembered,
		Transcript: memorymysql.NewTranscript(db),
		Embedder:   embedder(cfg, logger.Logger),
		Logger:     logger.Logger,
	}
	catchUpEmbeddings(context.Background(), remembering, logger.Logger)

	// One store for what the person's devices saw, shared by the endpoint
	// they report to and the tool that reads it back.
	deviceEvents := eventmysql.New(db).OnChange(changed(live.AreaEvents))
	// The places the person draws on the client's map. Their fixes are
	// tested against these as they arrive, and the crossings written
	// into deviceEvents like any other report.
	drawnPlaces := placesmysql.New(db).OnChange(changed(live.AreaPlaces))
	// What every rule did. Kept apart from the rules themselves
	// because it outlives them: a rule rewritten or deleted keeps
	// its history, and nothing here is ever read by the engine.
	ranLog := jobmysql.NewRuns(db.DB).OnChange(changed(live.AreaRules))

	// One store, shared by the tools that make reminders and the loop that
	// says them.
	reminderStore := remindmysql.New(db).OnChange(changed(live.AreaReminders))
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
	// And a call, which goes to its own path on the same phone.
	// Separate from the alarm because either may be configured
	// without the other: a server with no alarm URL can still ring
	// somebody, and one with no call URL still sets alarms.
	// Nil when there is no address for it, and then the call tools
	// are not offered at all: an assistant that cannot reach the
	// phone should not have a way of saying it is ringing somebody.
	var ringing *caller
	// Untyped nil unless there is somewhere to send a mid-call
	// change, for the usual reason: a typed nil satisfies the
	// interface and then fails on every call.
	var changing callingtool.Changer
	if phone := phoneCall(cfg); phone != nil {
		during := strings.TrimSpace(cfg.HomeAssistant.DuringURL.Reveal())
		callHands(sending.Hands, *phone, during)
		ringing = &caller{to: sending}
		if during != "" {
			changing = ringing
		}
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
		situationstool.Tools(deviceEvents, []situationstool.Named{
			// The two shapes, named so that a rule can ask for one.
			// Somebody asking whether anything is wrong gets them
			// in this order, and the first that finds something
			// answers; a rule about one of them says which.
			//
			// The charger was the third and is now two rules. It
			// was arithmetic on one number against another, which
			// is the one thing the engine already does for nothing
			// -- what is left here are the two that count and
			// group, which it cannot.
			{Name: "trying", Look: situation.Trying},
			{Name: "quiet", Look: situation.QuietExcept(cfg.HomeAssistant.ThisMachine)},
		}, cfg.Assistant.Now),
		rules.All(jobmysql.New(db.DB).OnChange(changed(live.AreaRules)), rules.Clock{
			Now: cfg.Assistant.Now, Location: cfg.Assistant.Location,
		}, jobmysql.ID, known, routinemysql.New(db.DB),
			func(ctx context.Context, j job.Job) job.Trial {
				if running == nil {
					return job.Trial{}
				}
				return running.Try(ctx, j)
			},
			rules.Writer{
				// The fast model, and no persona at all. This one
				// is not speaking to anybody -- it answers with
				// JSON -- and a manner would only be something
				// else for it to spend its reply on.
				Ask: func(ctx context.Context, system, ask string) (string, error) {
					return askAs(askedBy(ctx, environment.FeatureRuleWriter, "", ""), answerer, cfg.PlatformAI.Vendor,
						cmp.Or(cfg.PlatformAI.FastModel, cfg.PlatformAI.Model),
						system, environment.PurposeRemark, ask)
				},
				Kinds: deviceEvents.Kinds,
				// Only what a rule may actually use, so it cannot
				// reach for a tool that writes and be refused for
				// it afterwards.
				Reading: func() []string { return readable(registry) },
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
		callingTools(deviceEvents, ringing, chats, changing, cfg.Assistant.Now),
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
			Ask: func(ctx context.Context, userID, ask string) (string, error) {
				return askAs(askedBy(ctx, environment.FeatureComing, userID, ""), answerer, cfg.PlatformAI.Vendor,
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

	// Where to look when a tool has looked for a name in its own
	// domain and found nothing.
	//
	// A name does not say which domain it belongs to. Owner: "stop
	// the water alarm ... it should figure out and come to a
	// conclusion that it is an alarm kept to drink water at 11:45."
	// Alarm is the word for a thing on the phone, the thing itself
	// is a reminder, and a rule could have written it.
	//
	// Set after the registry rather than built into it, because the
	// sources are domains and the registry is the one thing that
	// must not import them. Only the local stores are here: a
	// reminder, a rule and a memory are rows in MySQL and answer in
	// single figures of milliseconds. Tasks and the diary go to
	// Google, which is what [elsewhere.Budget] is for.
	tools.Elsewhere = elsewhere.Search{
		Sources: []elsewhere.Source{
			elsewhere.Reminders(reminderStore, cfg.Assistant.Location),
			elsewhere.Rules(jobmysql.New(db.DB), rules.Kind, cfg.Assistant.Location),
			elsewhere.Tasks(listsOf(googleLink), cfg.Assistant.Location),
			elsewhere.Diary(diaryOf(googleLink, cfg), cfg.Assistant.Now, cfg.Assistant.Location),
			elsewhere.Remembered(remembering.Store),
		},
		Logger: logger.Logger,
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

	// What connected clients are told as it happens: the satellite's state
	// (below), and every change to a conversation, from the runner and from
	// announcements as they are said.
	liveHub := live.NewHub()
	changeHub.Store(liveHub)
	announcements.Changes = liveHub
	// Something owed: the person's clients are told at once, so one that is
	// there collects and says it then.
	publishing.Told = func(userID string) { liveHub.AreaChanged(userID, live.AreaAnnouncements) }

	// The tools a person's desktop app offers over MCP on /v1/live, added
	// to that person's chats while it stays connected.
	devices := away.NewSet(logger.Logger)

	// Which tools a question is shown in full, chosen by what it means
	// rather than by a list written in advance. Nil where there is no
	// embedder or the setting is off, and a nil picker leaves every
	// turn with the list, which is what the server sent until 18
	// October 2026.
	picking := picker(cfg, registry, logger.Logger)

	// The names he says, kept for speech recognition and read here as
	// well: they are the correct spellings of exactly the words a
	// microphone gets wrong.
	names := vocabularymysql.New(db)

	// What a question names, and which record each name belongs to. Nil
	// where no name service is configured.
	pointed := pointer(cfg, logger.Logger, names, drawnPlaces, deviceEvents)

	chatRunner, err := runner.New(runner.Options{
		Repository:    chats,
		Messages:      chats,
		Announcements: announcements,
		Publish:       announcing{publishing, cfg.Assistant.Now},
		Environment:   answerer,
		Logger:        logger.Logger,
		Publisher:     bus,
		Changes:       liveHub,
		HistoryLimits: historyLimits(cfg, logger.Logger),
		AssistantName: cfg.Assistant.Name,
		Picker:        picking,
		Pointer:       pointed,
		ToolsNearest:  cfg.Assistant.ToolsNearest,
		Persona:       manner,
		Announcer:     voices,
		Cut:           cutOff,
		Tools:         tools,
		Away:          devices,
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
		// A reminder falling due is written down for every client to
		// collect and say, as a rule's announcement is, and not spoken
		// from here (owner, 16 October 2026: *"go with b"*).
		Speaker:  owingReminders{to: publishing, where: cfg.Assistant.Location, now: cfg.Assistant.Now},
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

	// What the assistant considered saying unasked, so that a watcher
	// which is working and one which has stopped do not look alike.
	remarked := noticemysql.New(db.DB).OnChange(changed(live.AreaRemarks))

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
			return askOnce(askedBy(ctx, environment.FeatureProfile, "", ""), answerer,
				cfg.PlatformAI.Vendor, cfg.PlatformAI.Model, environment.PurposeProfile, ask)
		},
	}
	// What he writes for himself, once the description is fresh.
	//
	// Declared here and filled in below, where the jobs store it
	// needs exists. The loop that uses it is started after that and
	// not here: a goroutine started here reads this variable within
	// microseconds, which is nil, and then skips the pass for the
	// life of the server.
	var thinkingUp func(context.Context, string)

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
			return askOnce(askedBy(ctx, environment.FeatureVocabulary, "", ""), answerer,
				cfg.PlatformAI.Vendor, cfg.PlatformAI.Model, environment.PurposeVocabulary, ask)
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

	// What every rule did, which is worth a month and not a year.
	watching.Add(1)
	go func() {
		defer watching.Done()
		logging.Surviving(remindCtx, logger.Logger, "forgetting old rule history", func() {
			forgetOlderDaily(remindCtx, ranLog, runsKeep, "rule history", cfg.Assistant.Now, logger.Logger)
		})
	}()
	logger.Info("forgetting old rule history daily",
		slog.Duration("every", forgetEvery), slog.Duration("keeping", runsKeep))

	// What every model call cost, which is worth a quarter.
	watching.Add(1)
	go func() {
		defer watching.Done()
		logging.Surviving(remindCtx, logger.Logger, "forgetting old model calls", func() {
			forgetOlderDaily(remindCtx, calls, usage.Keep, "model calls", cfg.Assistant.Now, logger.Logger)
		})
	}()
	logger.Info("forgetting old model calls daily",
		slog.Duration("every", forgetEvery), slog.Duration("keeping", usage.Keep))

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
				return askAs(askedBy(ctx, environment.FeatureGreeting, "", ""), answerer,
					cfg.PlatformAI.Vendor, model,
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
	scheduled := jobmysql.New(db.DB).OnChange(changed(live.AreaRules))

	// Nil where there is nothing to think with, which leaves him
	// noticing nothing rather than failing at startup over a thing
	// nobody asked for.
	thinkingUp = noticed(
		// The same lookup the rule tool uses, and nil here until 14
		// October. A rule naming a tool is checked against it, and
		// with nothing to check against the check panicked rather
		// than refusing -- which no rule the assistant wrote had
		// ever reached, because none had ever been written.
		func(name string) (tool.Tool, bool) { return known(name) },
		scheduled, ranLog, remembered, &profile.Reader{Memories: remembering.Store},
		rules.Writer{
			Ask: func(ctx context.Context, system, ask string) (string, error) {
				return askAs(askedBy(ctx, environment.FeatureNoticingWriter, "", ""), answerer,
					cfg.PlatformAI.Vendor, cmp.Or(cfg.PlatformAI.SlowModel, cfg.PlatformAI.Model),
					system, environment.PurposeRemark, ask)
			},
			Kinds: deviceEvents.Kinds,
			// The same list the writer gets when he asks for a rule
			// himself, and missing here until 14 October.
			//
			// A remarked rule is refused unless it reads something
			// before it speaks -- Grounded, which means naming a
			// tool -- and the writer building it was never told a
			// tool name. It could only name nothing and be refused
			// for being ungrounded, or invent one and be refused
			// for naming a tool that does not exist. Neither pass
			// has ever written a rule, and this half of it could
			// not have.
			Reading: func() []string { return readable(registry) },
		},
		// The pass that decides whether anything is worth a rule can
		// now go and look for itself, rather than deciding from a
		// description somebody else wrote.
		//
		// It was answering honestly before and writing nothing: its
		// own bar is that a pattern must be in the evidence it was
		// given, with counts, and the evidence it was given counted
		// one kind at a time. Every single-kind occasion worth having
		// already had a rule on it, written by him. The combinations
		// -- left home, then the helmet connected, then the office --
		// were the only unworked ground and were invisible.
		//
		// Reading only, and the slow model. Owner: *"it should have
		// the provision to fetch and cross-check the facts ... since
		// there is no time constraint it can take its own time, here
		// accuracy >> latency."*
		investigating(answerer, registry, cfg.PlatformAI.Vendor,
			cmp.Or(cfg.PlatformAI.SlowModel, cfg.PlatformAI.Model),
			environment.PurposeProfile,
			[]string{"event_kinds", "event_recent", "event_ledger", "event_untouched", "place_visits"},
			persona.AddressFor(cfg.Assistant.Persona), Looking,
			environment.FeatureNoticing, "looking for something worth a rule", logger.Logger),
		jobmysql.ID, cfg.Assistant.Now, cfg.Assistant.Location, logger.Logger)

	// And now, with something to think with, the loop that rewrites
	// the description and asks whether any of it is worth a rule.
	//
	// Here and not beside the other watchers, which is the whole of
	// the fix: this ran for three days as a goroutine started a
	// hundred lines earlier, taking thinkingUp as an argument before
	// anything had assigned it. The pass was skipped silently on
	// thirty-one of thirty-four descriptions, and ran on the three
	// where the scheduler happened to let the setup get this far
	// first.
	//
	// A nil function argument and a race in one line. Nothing about
	// the rule writer was wrong, and every hour spent looking at the
	// judge was spent on a model that was never called.
	watching.Add(1)
	go func() {
		defer watching.Done()
		logging.Surviving(remindCtx, logger.Logger, "the daily description", func() {
			describeDaily(remindCtx, chats, describing, thinkingUp,
				catchingUp(chats, answerer, registry, cfg, logger.Logger),
				cfg.Assistant.Now, logger.Logger)
		})
	}()
	logger.Info("describing the person", slog.Duration("every", profileEvery))

	// One reader, used by the conditions a rule checks and by the
	// triggers it waits for. Two would be two answers to the same
	// question.
	look := readings{events: deviceEvents, usually: usually,
		linked: map[string]func(context.Context, string) (string, time.Time, error){
			"link.google": googleHealth(googleLink),
		},
		hass:   entityState(speaker),
		counts: deviceEvents.Tally,
		where:  cfg.Assistant.Location, now: cfg.Assistant.Now}

	running = &job.Runner{
		Store: scheduled,
		Doer: doing{
			tools: tools, speaker: voices, written: announcements,
			to: publishing, events: deviceEvents, here: here, look: look,
			usually:  ordinarily(&profile.Reader{Memories: remembering.Store}, logger.Logger),
			shapes:   pattern.Matcher{Events: deviceEvents, Now: cfg.Assistant.Now},
			answerer: answerer, cfg: cfg,
			now: cfg.Assistant.Now, logger: logger.Logger,
		},
		Readings: look,
		Journal:  keeping{notes: remarked, runs: ranLog, log: logger.Logger},
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

	// What connected clients are told as it happens: the voice
	// satellite's state, followed live from Home Assistant. Shares the
	// reminder loop's context and wait group, so it stops with it.
	if watcher, err := hass.NewWatcher(hass.WatcherConfig{
		URL:      cfg.HomeAssistant.URL,
		Token:    cfg.HomeAssistant.Token,
		Entities: []string{cfg.HomeAssistant.Satellite},
		OnState: func(_, state string) {
			liveHub.Publish(live.Event{Kind: live.SatelliteState, State: state, At: cfg.Assistant.Now()})
		},
		Logger: logger.Logger,
	}); err == nil {
		watching.Add(1)
		go func() {
			defer watching.Done()
			logging.Surviving(remindCtx, logger.Logger, "satellite state", func() {
				watcher.Run(remindCtx)
			})
		}()
		logger.Info("following the satellite live", slog.String("satellite", cfg.HomeAssistant.Satellite))
	} else if !errors.Is(err, hass.ErrNotConfigured) {
		logger.Warn("cannot follow the satellite", slog.Any("error", err))
	}

	// A terminal on the laptop wanting attention: a short turn of the
	// fast model decides whether to tell them, and the sentence goes to
	// the laptop that sent the news (owner, 17 October 2026).
	reacting := &terminal.Reactor{
		Answerer: answerer,
		Vendor:   cfg.PlatformAI.Vendor,
		Model:    cmp.Or(cfg.PlatformAI.FastModel, cfg.PlatformAI.Model),
		Voice:    func() string { return persona.Voice(manner.Current(), cfg.Assistant.Name) },
		Address:  func() string { return persona.AddressFor(manner.Current()) },
		Usually:  ordinarily(&profile.Reader{Memories: remembering.Store}, logger.Logger),
		Tools:    devices.For,
		Publish:  announcing{publishing, cfg.Assistant.Now},
		Notes:    remarked,
		Location: cfg.Assistant.Location,
		Now:      cfg.Assistant.Now,
		Logger:   logger.Logger,
	}

	// A line written in a space's notes on the laptop: the fast model
	// decides whether it is a task or an event, and offers it there
	// (owner, 18 October 2026).
	noting := &noted.Offerer{
		Answerer: answerer,
		Vendor:   cfg.PlatformAI.Vendor,
		Model:    cmp.Or(cfg.PlatformAI.FastModel, cfg.PlatformAI.Model),
		Voice:    func() string { return persona.Voice(manner.Current(), cfg.Assistant.Name) },
		Publish:  announcing{publishing, cfg.Assistant.Now},
		Notes:    remarked,
		Location: cfg.Assistant.Location,
		Now:      cfg.Assistant.Now,
		Logger:   logger.Logger,
	}

	handler := api.New(api.Options{
		Logger:        logger.Logger,
		AssistantName: cfg.Assistant.Name,
		StartedAt:     started,
		Restarts:      supervisorRestarts,
		Live:          liveHub,
		Away:          devices,
		Voices:        voices,
		ShowsVoice:    reporting(speaker),
		VoiceState:    cfg.HomeAssistant.VoiceState,
		DB:            db,
		Arrivals:      running,
		Terminals:     reacting,
		Noted:         noting,
		Chats:         chats,
		Messages:      chats,
		Reminders:     reminderStore,
		Announcer:     voices,
		Announcements: announcements,
		Heard:         announcements,
		Greeting:      greeting,
		Spoke:         chats,
		Reported:      deviceEvents,
		Known:         &profile.Reader{Memories: remembering.Store},
		ThisMachine:   cfg.HomeAssistant.ThisMachine,
		// The same diary and lists the tools use, for a greeting that
		// can mention what is first thing tomorrow. Nil without
		// Google, and a greeting is then about what has happened.
		Diary:        greetingDiary(googleLink, cfg),
		Chores:       greetingChores(googleLink),
		Google:       googleLink,
		SettingsURL:  cfg.Google.SettingsURL,
		Location:     cfg.Assistant.Location,
		Now:          cfg.Assistant.Now,
		Runner:       chatRunner,
		Memories:     remembering.Store,
		Vocabulary:   names,
		Events:       bus,
		Models:       reachableModels(cfg),
		DefaultModel: cfg.PlatformAI.Model,
		Persona:      manner,
		Cut:          cutOff,
		Answered:     liveHub,
		DeviceEvents: deviceEvents,
		Places:       drawnPlaces,
		Usage:        calls,
		// "What am I seeing": one quick call from what the client shows,
		// the fast model where one is configured.
		Glance: &glance.Glancer{
			Ask: func(ctx context.Context, system, ask string) (string, error) {
				return askAs(ctx, answerer, cfg.PlatformAI.Vendor,
					cmp.Or(cfg.PlatformAI.FastModel, cfg.PlatformAI.Model), system, environment.PurposeChat, ask)
			},
			Voice:  func() string { return persona.Voice(manner.Current(), cfg.Assistant.Name) },
			Notes:  announcements,
			Logger: logger.Logger,
		},
		Remarks:        remarked,
		Rules:          scheduled,
		Usually:        usually,
		Trying:         running,
		Ran:            ranLog,
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
func buildProvider(cfg config.Config, keeping environment.Recorder, logger *slog.Logger) (environment.Environment, error) {
	switch cfg.Provider.Name {
	case config.ProviderPlatformAI:
		return platformai.New(platformai.Config{
			ClientID:     cfg.PlatformAI.ClientID,
			ClientSecret: cfg.PlatformAI.ClientSecret,
			RefreshToken: cfg.PlatformAI.RefreshToken,
			PortalID:     cfg.PlatformAI.PortalID,
			TokenURL:     cfg.PlatformAI.TokenURL,
			ChatURL:      cfg.PlatformAI.ChatURL,
			Scope:        cfg.PlatformAI.Scope,
			RedirectURI:  cfg.PlatformAI.RedirectURI,
			Vendor:       cfg.PlatformAI.Vendor,
			Model:        cfg.PlatformAI.Model,
			// The fallback for a caller that gives no prompt of its
			// own, which the runner never is -- it builds one per
			// chat and knows whether the reply will be heard.
			// Spoken, so that anything still relying on this
			// default gets exactly what it got before.
			SystemPrompt:       persona.Prompt(cfg.Assistant.Persona, cfg.Assistant.Name, persona.Spoken),
			Thinking:           cfg.PlatformAI.Thinking,
			Recorder:           keeping,
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

// callingTools : The call tools, or none when there is nowhere to
// ring from.
//
// Nothing rather than tools that always fail. A model holding a
// call_place that cannot work will say it is ringing somebody and
// then explain why it could not, which is two sentences worse than
// not having it.
func callingTools(reader callingtool.Reader, ring *caller, heard callingtool.Heard,
	change callingtool.Changer, now func() time.Time) []tool.Tool {
	if ring == nil {
		return nil
	}
	return callingtool.Tools(reader, ring, heard, change, now)
}

// phoneCall : Where a call is placed, or nothing when there is no
// address for it.
func phoneCall(cfg config.Config) *calling.Phone {
	where := strings.TrimSpace(cfg.HomeAssistant.CallURL.Reveal())
	if where == "" {
		return nil
	}
	return &calling.Phone{URL: where}
}

// callHands : How a call errand reaches the phone.
//
// Added to whatever is already there rather than replacing it, so
// that alarms and calls do not have to be configured together.
func callHands(hands errand.Hands, phone calling.Phone, during string) {
	hands[errand.CallPlace] = func(ctx context.Context, e errand.Errand) error {
		var c calling.Call
		if err := json.Unmarshal(e.Body, &c); err != nil {
			return fmt.Errorf("that errand does not describe a call: %w", err)
		}
		return phone.Ring(ctx, c)
	}
	// Its own path, and only when there is one. Changing a call in
	// progress is a different profile on the phone from starting
	// one, and either may be configured without the other.
	if during == "" {
		return
	}
	hands[errand.CallDuring] = func(ctx context.Context, e errand.Errand) error {
		var d calling.During
		if err := json.Unmarshal(e.Body, &d); err != nil {
			return fmt.Errorf("that errand does not describe a change to a call: %w", err)
		}
		return phone.Change(ctx, during, d)
	}
}

// Change : Writes down that the phone should change something about
// the call it is on.
//
// No dropping of what is already queued, unlike an alarm: two of
// these in a row are two things wanted, not a correction.
func (c *caller) Change(ctx context.Context, userID string, d calling.During,
	after time.Duration) error {
	if c == nil || c.to == nil || userID == "" {
		return fmt.Errorf("there is nowhere to ask for that")
	}
	body, err := json.Marshal(d)
	if err != nil {
		return err
	}
	when := c.to.Now().Add(after)
	// Worth doing for twenty seconds from the moment it is wanted.
	// A call put on speaker after it has ended is nothing but
	// confusing.
	made, err := errand.New(errandmysql.ID(), userID, errand.CallDuring,
		"", body, when, when.Add(calling.Soon))
	if err != nil {
		return err
	}
	if err := c.to.Ask(ctx, made); err != nil {
		return err
	}
	// Nudged when its moment comes. The sender looks every thirty
	// seconds otherwise, which is fine for an alarm and far too
	// slow for this -- the errand would expire before the tick
	// that was meant to carry it.
	if after > 0 {
		time.AfterFunc(after, c.to.Wake)
	}
	return nil
}

// caller : Asking for a call, without waiting on the phone.
//
// The same shape as the alarm queue and for the same reason: the
// tool that asks is in the middle of answering somebody, and the
// phone may be asleep on another network.
type caller struct{ to *errand.Sender }

// Ring : Writes down that somebody is to be called.
func (c *caller) Ring(ctx context.Context, userID string, call calling.Call) error {
	if c == nil || c.to == nil || userID == "" {
		return fmt.Errorf("there is nowhere to ask for a call")
	}
	body, err := json.Marshal(call)
	if err != nil {
		return err
	}
	now := c.to.Now()
	// Worth trying for a couple of minutes and no longer. Somebody
	// who asked to ring their mother and whose phone was unreachable
	// for ten wants to hear that it did not happen, not to have it
	// dial while they are in a meeting.
	made, err := errand.New(errandmysql.ID(), userID, errand.CallPlace,
		"", body, now, now.Add(calling.Worth))
	if err != nil {
		return err
	}
	return c.to.Ask(ctx, made)
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
// supervisorRestarts : How many times systemd has restarted this server's
// unit since it was last started by hand -- the number `make status`
// shows. The unit is read from the process's own cgroup, so it is whatever
// unit this is running under; outside systemd there is none to ask.
func supervisorRestarts(ctx context.Context) (int, bool) {
	cgroup, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return 0, false
	}
	var unit string
	for _, part := range strings.Split(strings.TrimSpace(string(cgroup)), "/") {
		if strings.HasSuffix(part, ".service") {
			unit = part
		}
	}
	if unit == "" {
		return 0, false
	}
	out, err := exec.CommandContext(ctx, "systemctl", "--user", "show", "-p", "NRestarts", "--value", unit).Output()
	if err != nil {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	return n, err == nil
}

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
	spare := fallingBack(cfg, logger,
		tavily.New(string(cfg.Search.Tavily), cfg.Search.Quick),
		firecrawl.New(string(cfg.Search.Firecrawl), cfg.Search.Patient))

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

// fallingBack : Two services as one, or whichever of them exists.
//
// A Pair is itself a Service, so three services are two Pairs: TinyFish
// first, and behind it Tavily then Firecrawl. Each nil is checked
// rather than wrapped, because a typed nil inside a Pair satisfies the
// interface and then fails on every call.
//
// Tavily before Firecrawl because Firecrawl's five hundred a month is
// the scarcer allowance and Tavily's thousand is, at this person's two
// to four searches a day, effectively uncapped.
func fallingBack(cfg config.Config, logger *slog.Logger, first, then search.Service) search.Service {
	switch {
	case isNil(first) && isNil(then):
		return nil
	case isNil(then):
		return first
	case isNil(first):
		return then
	}
	return &search.Pair{
		Quick: first, Spare: then,
		Within: cfg.Search.Quick, Rested: cfg.Search.Rested,
		Now: cfg.Assistant.Now, Logger: logger,
	}
}

// isNil : Whether a service is really absent.
//
// A *tavily.Client that is nil is not an untyped nil, so `s == nil` is
// false for it and every call would go to a method on a nil receiver.
func isNil(s search.Service) bool {
	return s == nil || reflect.ValueOf(s).IsNil()
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

// reporting : The announcer as something that can write a state into Home
// Assistant, when it is the one that can.
//
// Silent cannot, and a typed nil handed on as an interface is not nil to
// whatever receives it -- so this returns an untyped nil instead, and the
// caller's check for one holds.
func reporting(a announce.Announcer) assist.Reporter {
	if s, ok := a.(*hass.Speaker); ok && s != nil {
		return s
	}
	return nil
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

// readable : The tools a rule may look things up with.
//
// Everything that does not change anything. A rule runs unattended
// and may run every few minutes; reading is safe to repeat and
// writing is not, and the thing that writes rules should be told
// what it may have rather than corrected afterwards.
func readable(registry *tool.Registry) []string {
	if registry == nil {
		return nil
	}
	var out []string
	for _, t := range registry.All() {
		if !t.Writes {
			out = append(out, t.Name)
		}
	}
	return out
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

// picker : What chooses a turn's tools, warmed in the background.
//
// Warming is thirty-odd calls to the embedding server and takes the
// better part of a minute on this machine, so it does not hold up a
// start: turns that arrive before it is ready fall back to the
// hand-written list, which is what they got before this existed.
//
// The person's own devices offer tools too, and those arrive per chat
// rather than at startup. They are embedded the first time they are
// seen and kept; until then they are offered whatever the ranking
// says, since [nearest.Picker.Keep] never ranks out what it cannot
// rank.
func picker(cfg config.Config, registry *tool.Registry, logger *slog.Logger) *nearest.Picker {
	if cfg.Assistant.ToolsNearest <= 0 || registry == nil {
		return nil
	}
	e := embedder(cfg, logger)
	if e == nil || !e.Available() {
		logger.Warn("no embedding server, so a turn's tools are the usual list",
			slog.Int("tools_nearest", cfg.Assistant.ToolsNearest))
		return nil
	}
	p := &nearest.Picker{Embed: e, Logger: logger}
	go logging.Surviving(context.Background(), logger, "learning what the tools are for", func() {
		began := time.Now()
		added, err := p.Warm(context.Background(), nearest.Server, registry.All())
		if err != nil {
			logger.Warn("cannot learn what all the tools are for",
				slog.Int("learnt", added), slog.Any("error", err))
			return
		}
		logger.Info("learnt what the tools are for",
			slog.Int("tools", added), slog.Duration("took", time.Since(began)))
	})
	return p
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

// pointer : What finds the names in a question and says where each
// belongs -- his own record, the world, or nowhere.
//
// Nothing when no name service is configured, which leaves nothing
// looked up ahead of a question. A service that is configured but not
// answering is not checked here, for the reason embedder does not check
// either: it is reached per request.
//
// The record reads three sources, all of them already kept. The nightly
// list of names he says, the places he has drawn, and the names his own
// devices reported -- which are worth most, because a contact name read
// off the phone is spelt the way he spells it and the same name through
// a microphone is a guess.
func pointer(cfg config.Config, logger *slog.Logger,
	said vocabulary.Store, drawn places.Store, reported event.Watching) *pointing.Pointer {

	if !cfg.Naming.Configured() {
		logger.Info("no name service, so nothing is looked up ahead of a question")
		return nil
	}

	record := &pointing.Record{Sources: []pointing.Names{
		pointing.NamesFunc(func(ctx context.Context, userID string) ([]string, error) {
			terms, _, err := said.Get(ctx, userID)
			return terms, err
		}),
		pointing.NamesFunc(func(ctx context.Context, userID string) ([]string, error) {
			kept, err := drawn.List(ctx, userID)
			if err != nil {
				return nil, err
			}
			out := make([]string, 0, len(kept))
			for _, place := range kept {
				out = append(out, place.Name)
			}
			return out, nil
		}),
		pointing.NamesFunc(func(ctx context.Context, userID string) ([]string, error) {
			events, err := reported.Recent(ctx, userID, event.Query{
				Since: cfg.Assistant.Now().Add(-vocabulary.Reported),
				Limit: namingEventsRead,
			})
			if err != nil {
				return nil, err
			}
			return vocabulary.Heard(events), nil
		}),
	}}

	logger.Info("names in a question are looked up before it is answered",
		slog.String("url", cfg.Naming.URL))
	return &pointing.Pointer{
		Finder:    gliner.New(gliner.Config{URL: cfg.Naming.URL, Timeout: cfg.Naming.Timeout}),
		Known:     record,
		Assistant: cfg.Assistant.Name,
	}
}

// namingEventsRead : The most events read to collect the names his own
// devices have reported.
//
// vocabulary.Heard caps what it keeps; this caps what it is given, so a
// month of location fixes cannot be pulled into memory to find a handful
// of contact names.
const namingEventsRead = 4000

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

// noticingTimeout : How long the rule-writing pass may take.
//
// Its own, and far longer than the description's, because it stopped
// being one question the day it was given tools. It now reads what it
// chooses to read, cross-checks what it finds, and may spend twenty
// rounds doing it. Owner: *"since there is no time constraint it can
// take its own time, here accuracy >> latency."*
//
// A deadline at all, rather than none, because a pass that never
// returns holds the hourly loop for ever and the next hour's pass
// never runs. This is the length at which something has gone wrong,
// not a budget it is meant to spend.
const noticingTimeout = 20 * time.Minute

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
	noticed func(ctx context.Context, userID string),
	caught func(ctx context.Context, userID string),
	now func() time.Time,
	logger *slog.Logger,
) {
	// Said once, at the top, because the alternative is what
	// happened: a pass that is never going to run looks exactly
	// like a pass with nothing to say.
	if noticed == nil {
		logger.Warn("the description will be rewritten but nothing will be noticed from it")
	}

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
			if err := describing.Build(environment.Labelled(pass, environment.Label{UserID: userID}), userID); err != nil {
				logger.Warn("cannot describe them", slog.Any("error", err))
				continue
			}
			logger.Info("described them from what they said",
				slog.String("user_id", userID))

			// And, having just read what they are like, whether any
			// of it is worth a rule of his own. Here rather than on
			// a loop of its own because the description is what it
			// reasons from, and reading a description written an
			// hour ago to decide what to watch for is a pass spent
			// on yesterday.
			if noticed != nil {
				// Its own deadline, off the parent rather than off
				// the description's three minutes: this one goes and
				// looks, and the description's budget was set when
				// neither of them did.
				looking, done := context.WithTimeout(ctx, noticingTimeout)
				noticed(looking, userID)
				done()
			}

			// And what they said that was never written down. Last
			// of the three because it is the only one that writes
			// anywhere the person will see, and because it reads the
			// diary rather than the description: nothing above it
			// changes what it will find.
			if caught != nil {
				looking, done := context.WithTimeout(ctx, noticingTimeout)
				caught(looking, userID)
				done()
			}
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

// usageSettle : The least time between two notices that somebody's
// model calls changed.
//
// A turn with tools is four or five calls in as many seconds, and a
// settings page that read them all again after each would be reading
// for the whole turn.
const usageSettle = 5 * time.Second

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
// runsKeep : How long a rule's own history is worth keeping.
//
// A month. At roughly sixteen hundred runs a day that is about
// fifty thousand rows standing, against a third of a million a
// year if nothing swept them -- and the thing they are for is
// comparing a rule with itself before and after a change, which
// nobody does across a year.
const runsKeep = 30 * 24 * time.Hour

// forgetting : Something that drops what is older than a moment.
type forgetting interface {
	Forget(ctx context.Context, before time.Time) (int64, error)
}

// forgetOlderDaily : Drops what is older than [keep], once a day, for
// as long as ctx lives -- rule history ([runsKeep]) and model calls
// ([usage.Keep]).
//
// A loop of its own for each rather than a second errand inside the
// readings one, so that a failure to sweep one cannot stop the other.
func forgetOlderDaily(ctx context.Context, from forgetting, keep time.Duration, what string,
	now func() time.Time, logger *slog.Logger,
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
		forgetOlder(ctx, from, keep, what, now, logger)
	}
}

// forgetOlder : One pass of forgetOlderDaily. A failure is logged and
// the next day tries again.
func forgetOlder(ctx context.Context, from forgetting, keep time.Duration, what string,
	now func() time.Time, logger *slog.Logger,
) {
	pass, cancel := context.WithTimeout(ctx, forgetTimeout)
	defer cancel()
	before := now().Add(-keep)
	gone, err := from.Forget(pass, before)
	if err != nil {
		logger.Warn("cannot forget old "+what, slog.Any("error", err))
		return
	}
	if gone == 0 {
		return
	}
	logger.Info("forgot old "+what,
		slog.Int64("rows", gone), slog.Time("older_than", before))
}

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
			if err := listening.Build(environment.Labelled(pass, environment.Label{UserID: userID}), userID); err != nil {
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
// Looking : How many rounds of tools the rule-writing pass may take
// before it must answer.
//
// Far more than a rule's own remark gets, and deliberately. That one
// is a reading taken while somebody waits; this is an investigation
// nobody is waiting for, run on a schedule in the background. Owner,
// on this pass: *"since there is no time constraint it can take its
// own time, here accuracy >> latency."*
const Looking = 20

// investigating : A question a model may use tools to answer.
//
// The same shape as a rule's own write step -- ask, run whatever
// tools come back, ask again -- with two differences. The system
// prompt is the caller's rather than the assistant's voice, because
// this one is judging rather than speaking; and every round is
// written down.
//
// The log is not for debugging. Owner, at the start of the day the
// pass was found never to have written a rule: *"every time the
// profiling happens and rule writing, log the things the AI is
// doing, what it says, what events went through, everything -- for a
// few days we'll monitor if the model is thinking the right way. Is
// it neglecting some facts? Or is it triggering useless rules?"*
// A pass that decides in private cannot be held to that.
func investigating(
	env environment.Environment,
	tools *tool.Registry,
	vendor, model string,
	why environment.Purpose,
	names []string,
	address string,
	rounds int,
	feature environment.Feature,
	doing string,
	logger *slog.Logger,
) func(ctx context.Context, userID, system, ask string) (string, error) {
	return func(ctx context.Context, userID, system, ask string) (string, error) {
		ctx = askedBy(ctx, feature, userID, "")
		specs, err := offering(tools, names, address)
		if err != nil {
			return "", err
		}
		var (
			history []environment.Turn
			ran     []string
			asked   = ask
			began   = time.Now()
		)
		logger.InfoContext(ctx, doing,
			slog.String("purpose", string(why)),
			slog.Int("tools_offered", len(specs)),
			slog.Int("shown_characters", len(ask)))

		for round := 0; round < rounds; round++ {
			stream, err := env.Run(ctx, environment.Request{
				Prompt: asked, History: history, Purpose: why,
				Vendor: vendor, Model: model, SystemPrompt: system, Tools: specs,
			})
			if err != nil {
				return "", err
			}
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
				logger.InfoContext(ctx, "it reached a conclusion",
					slog.String("purpose", string(why)),
					slog.Int("rounds", round+1),
					slog.Any("asked_for", ran),
					slog.Duration("took", time.Since(began)),
					slog.String("concluded", last.Text))
				return last.Text, nil
			}

			if asked != "" {
				history = append(history, environment.Turn{
					Role: environment.RoleUser, Text: asked})
				asked = ""
			}
			history = append(history, environment.Turn{
				Role: environment.RoleAssistant, ToolCalls: last.ToolCalls})

			results := make([]environment.ToolResult, 0, len(last.ToolCalls))
			for _, c := range last.ToolCalls {
				out := tools.Call(ctx, c.Name, tool.Invocation{
					Caller: tool.Caller{UserID: userID, Channel: chat.ChannelDirect},
					Args:   json.RawMessage(c.Arguments),
					Ran:    ran,
				})
				ran = append(ran, c.Name)
				// What it asked for and what it was told, both. Which
				// facts reached it is the question the log exists to
				// answer, and the arguments are half of that.
				logger.InfoContext(ctx, "it went and looked",
					slog.Int("round", round+1),
					slog.String("tool", c.Name),
					slog.String("asking", string(c.Arguments)),
					slog.String("outcome", string(out.Outcome)),
					slog.Int("told_characters", len(out.Content)),
					slog.String("told", out.Content))
				results = append(results, environment.ToolResult{ID: c.ID, Content: out.Content})
			}
			history = append(history, environment.Turn{
				Role: environment.RoleTool, ToolResults: results})
		}
		logger.WarnContext(ctx, "it never stopped looking",
			slog.String("purpose", string(why)),
			slog.Int("rounds", rounds),
			slog.Any("asked_for", ran))
		return "", fmt.Errorf("it asked for tools %d times without answering", rounds)
	}
}

// offering : The named tools as specs, refusing any that writes.
//
// The same refusal a rule gets, for the same reason: a pass deciding
// whether to write a rule has no business changing anything while it
// decides.
func offering(tools *tool.Registry, names []string, address string) ([]environment.ToolSpec, error) {
	out := make([]environment.ToolSpec, 0, len(names))
	for _, name := range names {
		found, ok := tools.Get(name)
		if !ok {
			return nil, fmt.Errorf("the rule-writing pass names %q, which is not a tool", name)
		}
		if found.Writes {
			return nil, fmt.Errorf("the rule-writing pass names %q, which changes things", name)
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

// askedBy : ctx, saying which feature is about to ask a model and for
// whom, so the call is kept under the right name.
//
// The person is the one given, else whoever is already on ctx, else the
// caller of the request being served: the greeting is written inside a
// request and is told nobody's id.
func askedBy(ctx context.Context, what environment.Feature, userID, detail string) context.Context {
	if userID == "" {
		if l, ok := environment.LabelOf(ctx); ok {
			userID = l.UserID
		}
	}
	if userID == "" {
		if c := authn.Of(ctx); c != nil && c.User != nil {
			userID = c.User.ID
		}
	}
	return environment.Labelled(ctx, environment.Label{UserID: userID, Feature: what, Detail: detail})
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

// owingReminders : A reminder falling due, written down for every client
// to collect and say -- the desktop app when it is open and unlocked --
// rather than spoken here. Delivered once it is published: whether it was
// heard is each client's to say.
type owingReminders struct {
	to    *outbox.Publisher
	where *time.Location
	now   func() time.Time
}

// Say : Publishes the reminder, as it would have been said.
func (o owingReminders) Say(ctx context.Context, r remind.Reminder) error {
	at := o.now()
	owed, err := outbox.New(outboxmysql.ID(), r.UserID, conversation.ReminderAnnouncement,
		remind.Spoken(r, at, o.where), at)
	if err != nil {
		return err
	}
	return o.to.Publish(ctx, owed)
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
	usually func(ctx context.Context, userID string) string
	// shapes : What works out a named pattern. Nil makes every
	// rule standing on one fail loudly rather than quietly not
	// hold.
	shapes   job.Patterns
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

func (d doing) Do(ctx context.Context, run job.Turn, s job.Act) (job.Outcome, error) {
	switch s.Do {
	case job.DoSay:
		// A trial says what it would say and says nothing. The
		// sentence is fixed, so there is nothing to work out and
		// nothing to hold back but the announcing itself.
		if run.Trying {
			return job.Outcome{Said: s.Text}, nil
		}
		// Published, and that is all: each client collects its own
		// copy and says it when somebody is there -- the desktop app
		// once it is open and unlocked (owner, 16 October 2026: *"go
		// with b"*) -- and is noted in the conversation when it is
		// said. Nothing is spoken through Home Assistant any more.
		kind := heard(run.Anywhere)
		o, err := outbox.Keeping(outboxmysql.ID(), run.UserID,
			kind, s.Text, d.now(), run.Worth)
		if err != nil {
			return job.Outcome{}, err
		}
		if err := d.to.Publish(ctx, o); err != nil {
			return job.Outcome{}, err
		}
		return job.Outcome{Said: s.Text}, nil

	case job.DoRelay:
		return d.relay(ctx, run)

	case job.DoWrite:
		return d.write(ctx, run, s)

	case job.DoTool:
		// As the person, on the typed channel. A job is not a
		// voice turn: nothing is waiting in a room for it, and the
		// tools that withhold themselves from a speaker should not
		// withhold themselves here.
		out := d.tools.Call(ctx, s.Use, tool.Invocation{
			Caller: tool.Caller{UserID: run.UserID, Channel: chat.ChannelDirect},
			Args:   s.With,
			// What the sequence has already run. The tools refuse
			// to write something they have not read in this turn,
			// and a sequence is one turn.
			Ran: job.Tools(run.Already),
		})
		if out.Outcome == conversation.OutcomeFailed {
			err := fmt.Errorf("%s: %s", s.Use, out.Content)
			// Whether the steps after this one were relying on it.
			// A tool that writes was asked to do something and the
			// sequence goes on without it; a tool that reads was
			// asked to find something, and going on without it is
			// a sentence composed from a hole.
			//
			// A name the registry does not know counts as a read.
			// Nothing is known about it, so nothing is risked by
			// the quieter answer.
			if t, found := d.tools.Get(s.Use); !found || !t.Writes {
				err = fmt.Errorf("%w: %w", job.ErrBlind, err)
			}
			return job.Outcome{}, err
		}
		d.logger.InfoContext(ctx, "a job ran a tool",
			slog.String("tool", s.Use), slog.String("outcome", string(out.Outcome)))

		// A tool may end the sequence by finding nothing, in the
		// same word a write step uses for it. Without this a rule
		// that looks for something unusual every two minutes asks
		// a model to say nothing seven hundred times a day: the
		// looking is cheap and deterministic, and the sentence is
		// neither.
		if prompt.Nothing(out.Content) {
			return job.Outcome{Stop: true, Why: s.Use + " found nothing", Said: run.Before}, nil
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
type keeping struct {
	notes notice.Notes
	// runs : The fuller record, which outlives the rule it is
	// about. The remarks row answers "what did it nearly say" and
	// is what the page shows; this answers "what did it do", step
	// by step, and is what tells whether a rule rewritten still
	// behaves as it did.
	runs *jobmysql.Runs
	// log : Where a journal that could not be written is noted. A
	// rule must not stop running because the thing watching it
	// broke.
	log *slog.Logger
}

// Wrote : Records one run, in both places.
func (k keeping) Wrote(ctx context.Context, r job.Record) error {
	if k.runs != nil {
		if err := k.runs.Wrote(ctx, r); err != nil && k.log != nil {
			// Logged and swallowed. The remarks row below is what
			// the person sees, and losing the fuller record must
			// not lose that too.
			k.log.WarnContext(ctx, "cannot write down what a rule did",
				slog.String("rule", r.Name), slog.Any("error", err))
		}
	}
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
	// hass : What one Home Assistant entity says right now. Nil
	// leaves those conditions unknown, which is how a server with
	// no Home Assistant behaves: rules standing on an entity do
	// nothing rather than fire on a guess.
	hass func(ctx context.Context, entity string) (string, time.Time, error)
	// counts : How many of a kind arrived in a window. Nil leaves
	// those conditions unknown.
	counts func(ctx context.Context, userID string, t event.Tally) (int, error)
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
	if job.Hassed(kind) {
		return r.entity(ctx, kind)
	}
	if job.Counts(kind) {
		return r.tally(ctx, userID, kind)
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

// entityState : How a rule reads Home Assistant, or nothing at all
// when there is no Home Assistant to read.
//
// The speaker already holds the address and the token and already
// asks Home Assistant what the satellite is doing before it
// interrupts it. A second client would be a second place for the
// address to be wrong.
//
// Asked of whatever is announcing rather than built from config,
// so a server announcing into silence -- no Home Assistant at all
// -- gets nothing here either, and the two cannot disagree about
// whether there is one.
func entityState(a announce.Announcer) func(context.Context, string) (string, time.Time, error) {
	asker, ok := a.(interface {
		State(ctx context.Context, entity string) (string, time.Time, error)
	})
	if !ok {
		return nil
	}
	return asker.State
}

// tally : How often a thing happened, as a reading.
//
// A number where every other source gives a value, which is what
// lets every comparison already there work on it: ">= 2" is the
// same machinery that reads a battery level, and counting needed
// no new operator.
//
// Nought is known. Everywhere else in this engine nothing known
// means the condition does not hold, and that is right, because
// nothing was proved. Here the answer was looked for and found to
// be none, which is a different thing and the only way a rule can
// be written about something *not* happening.
func (r readings) tally(ctx context.Context, userID, source string) ([]job.Reading, error) {
	if r.counts == nil {
		return nil, nil
	}
	c, err := job.ReadCount(source)
	if err != nil {
		return nil, err
	}
	now := r.clock()
	n, err := r.counts(ctx, userID, event.Tally{
		Kind:  c.Kind,
		Since: now.Add(-c.Window),
		Match: c.Match,
		Group: c.Group,
	})
	if err != nil {
		return nil, err
	}
	return []job.Reading{{Value: strconv.Itoa(n), At: now, Known: true}}, nil
}

// entity : What Home Assistant says an entity is, as a reading.
//
// One reading and never two, like a routine: there is no history
// here to compare against, only what it says at the moment it is
// asked, which is why [job.Gate.Arrives] refuses these as
// triggers.
//
// A missing entity is unknown and not an error. The owner renames
// things in Home Assistant, and the cost of that should be a rule
// that quietly does nothing until he notices -- the same cost as
// every other unknown reading -- rather than an error in the log
// every two minutes for a rule he has forgotten about.
func (r readings) entity(ctx context.Context, kind string) ([]job.Reading, error) {
	if r.hass == nil {
		return nil, nil
	}
	name := job.Entity(kind)
	if name == "" {
		return nil, nil
	}
	state, changed, err := r.hass(ctx, name)
	if err != nil {
		if errors.Is(err, hass.ErrNoEntity) || errors.Is(err, hass.ErrNotConfigured) {
			return nil, nil
		}
		return nil, err
	}
	// Home Assistant's own words for not knowing. A condition
	// against them never holds, and saying so here keeps every
	// gate from having to know them.
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "", "unknown", "unavailable", "none":
		return nil, nil
	}
	return []job.Reading{{Value: state, At: changed, Known: true}}, nil
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

// publish : Writes an announcement down for every client to collect and
// say; each notes it when it does.
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
	return d.to.Publish(ctx, o)
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
func (d doing) write(ctx context.Context, run job.Turn, s job.Act) (job.Outcome, error) {
	specs, bad := d.offered(s.Tools)
	if bad != nil {
		return job.Outcome{}, bad
	}
	// Named for the rule, so what each rule costs can be told apart.
	ctx = environment.Labelled(ctx, environment.Label{
		UserID: run.UserID, Feature: environment.FeatureRuleRemark, Detail: run.Name,
	})
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
		prompt.Text(
			"You are deciding whether to interrupt somebody, and then, only if you are, writing the one sentence they will hear.",
			"The deciding is the part that matters. Saying nothing is always available, costs them nothing, and is the right answer more often than not.",
			ask,
		),
		ordinary,
		// Moved to internal/app/noticing, where the eval that
		// measures it can see the same words. See its doc comment
		// for the morning it swallowed a greeting.
		noticing.Interrupting,
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
		worthSaying,
		prompt.Text("Answer with the sentence and nothing else.",
			"If there is nothing worth saying, answer with "+prompt.Silence+"."),
	)
}

// worthSaying : The question a rule has to put to itself before it
// is allowed to speak.
//
// Owner: **"if the model is going to comment, or say something, it
// should ask itself a question -- whether it is actually useful, or
// does it make sense. Same goes for rule writing too."**
//
// The other half of the bars in noticing, which ask it of a rule
// before the rule exists. This asks it of one firing, because a rule
// that deserved to exist can still have nothing worth saying on a
// Tuesday, and the two have to agree or the careful one is wasted.
//
// Both bars and not one, for the reason noticing.Purpose was split:
// the test that keeps warnings scarce -- that a thing they can do
// nothing about is not worth an interruption -- forbids every remark
// there is. So the question asked here depends on what the rule is
// for, and a rule written to remark is judged on being true and
// particular instead. A single bar here would have quietly undone
// the companion the day it was added.
//
// It needed saying out loud. Everything else in a write step is
// about the sentence -- how to open it, what not to put first, what
// the ordinary day is -- and a model told at length how to phrase
// something will phrase something. Silence was one clause at the
// very end against a prompt that assumed from its first word that
// it was writing.
//
// Measured, that is what it did. `a device has gone quiet` spoke six
// times in 529 runs, which sounds careful until the six are read:
// five of them were one silence announced five times in two hours,
// and the sixth was a laptop being quiet at half past midnight.
// Every one passed a step whose last line offered silence.
var worthSaying = prompt.Text(
	"Before writing a word, put two questions to what is in front of you, and answer them honestly.",
	"Does it make sense? A person hearing it should recognise a real thing that happened. A device describing itself is not a thing that happened, and a reading restated as though it were news is not one either.",
	"Is it worth their attention? For a rule that exists to catch something, that means useful: hearing it now changes what they do, or something they would otherwise get wrong. A thing that is merely true, or that they can do nothing about, is a fact, and they did not ask for it.",
	"Where instead the rule exists to remark on them, useful is the wrong test and is not asked. A remark earns its place by being true and particular to them. If it would fit anybody, or you are filling a silence, it has not earned it.",
	"If either answer is no, say "+prompt.Silence+". Do not look for a smaller thing to say instead, and do not soften something into being worth saying -- the honest answer to a dull hour is nothing.",
	"Do not weigh how long it is since you last spoke. An interruption nobody needed is not improved by being rare, and a thing worth saying is not spoiled by having said something an hour ago.",
)

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
// Holds : Whether a rule's conditions hold, and which one did not.
//
// One fetch for the whole tree rather than one per condition. The
// sources are collected first, however deeply they are nested, so
// a rule asking three things about the battery reads it once --
// which the old shape could not do, because each check step was
// answered on its own and knew nothing of the ones beside it.
func (d doing) Holds(ctx context.Context, userID string, when job.Occasion) (bool, string, error) {
	got, away, now, err := d.readings(ctx, userID, when)
	if err != nil {
		return false, "", err
	}
	held, why := when.Holds(got, away, now)
	d.logger.InfoContext(ctx, "a job checked its conditions",
		slog.String("when", when.Describe()), slog.Bool("held", held))
	return held, why, nil
}

// Judge : What every one of a rule's conditions answered, and why the
// whole did not hold, for a rule being tried; read as [doing.Holds]
// reads them.
func (d doing) Judge(ctx context.Context, userID string, when job.Occasion) (job.Verdict, string, error) {
	got, away, now, err := d.readings(ctx, userID, when)
	if err != nil {
		return job.Verdict{}, "", err
	}
	_, why := when.Holds(got, away, now)
	return when.Judge(got, away, now), why, nil
}

// readings : Everything a rule's conditions read, whether anybody is
// away, and the moment in their own zone -- what answering them
// needs, fetched once for the whole tree.
func (d doing) readings(ctx context.Context, userID string, when job.Occasion) (map[string][]job.Reading, bool, time.Time, error) {
	// One reading each, unless something is being compared with its
	// own past -- then enough of them to reach back that far. A
	// phone reports its level every few minutes, so a quarter of an
	// hour is a handful and a day would still be inside the cap.
	deep := 1
	if back := when.Deep(); back > 0 {
		deep = job.Over
	}

	got := map[string][]job.Reading{}
	for _, kind := range when.Needs() {
		// A pattern is a source like any other, and that is the
		// whole of why it was a small change: it names itself,
		// something works it out, and the judging is the code
		// that was already there. What it answers is a reading of
		// yes or no, which every operator already knows how to
		// compare.
		if job.IsPattern(kind) {
			reading, err := d.patterned(ctx, userID, kind)
			if err != nil {
				return nil, false, time.Time{}, err
			}
			got[kind] = []job.Reading{reading}
			continue
		}
		latest, err := d.look.Latest(ctx, userID, kind, deep)
		if err != nil {
			return nil, false, time.Time{}, err
		}
		got[kind] = latest
	}

	// Nil presence is the convention the reminder loop already
	// uses: nothing can tell, so nobody is held to be away.
	away := d.here != nil && d.here.Away(ctx, userID)

	// In their own zone, because an hour means nothing anywhere
	// else: ten o'clock is ten where they are standing. Lengths of
	// time are unaffected, being lengths rather than moments.
	return got, away, d.now().In(d.where()), nil
}

// patterned : One pattern source, worked out.
//
// An error rather than an unknown reading when there is nothing to
// work it out with. A rule standing on a shape nobody can compute
// is broken, and a broken rule that reads as "did not hold" is a
// rule that will be wrong in silence for as long as nobody checks.
func (d doing) patterned(ctx context.Context, userID, source string) (job.Reading, error) {
	if d.shapes == nil {
		return job.Reading{}, fmt.Errorf("there is nothing on this server to work %q out with", source)
	}
	name, with, err := job.Pattern(source)
	if err != nil {
		return job.Reading{}, err
	}
	held, err := d.shapes.Match(ctx, userID, name, with)
	if err != nil {
		return job.Reading{}, err
	}
	value := job.No
	if held {
		value = job.Yes
	}
	// Known and now: a shape is worked out at the moment it is
	// asked, so there is no older reading of one and nothing for
	// `older than` to be true of.
	return job.Reading{Value: value, At: d.now(), Known: true}, nil
}

// where : The person's zone, or UTC.
func (d doing) where() *time.Location {
	if d.cfg.Assistant.Location == nil {
		return time.UTC
	}
	return d.cfg.Assistant.Location
}

// done : What every rule in force has actually done.
//
// Every rule and not only his own, because the question the owner
// put is about the list as a whole: *"the model should look at the
// current rules and how useful they are... it has to question
// itself -- is that rule useful?"* A judge shown names cannot ask
// that, and a judge shown only its own rules cannot either, since
// it has written none.
//
// A rule whose record cannot be read is listed without one rather
// than dropped. Not knowing what it has done is no reason to let
// the judge believe it is not there.
func done(
	ctx context.Context,
	ran *jobmysql.Runs,
	mine []job.Job,
	now func() time.Time,
	logger *slog.Logger,
) []noticing.Spoke {
	held := make([]noticing.Spoke, 0, len(mine))
	for _, j := range mine {
		s := noticing.Spoke{Job: j}
		if ran != nil {
			records, err := ran.Of(ctx, j.ID, now().Add(-noticing.Settled*8), everyRule)
			if err != nil {
				logger.WarnContext(ctx, "cannot read what a rule has done",
					slog.String("rule", j.ID), slog.Any("error", err))
			}
			s.Ran = len(records)
			for _, r := range records {
				if strings.TrimSpace(r.Said) != "" {
					s.Said++
				}
			}
		}
		held = append(held, s)
	}
	return held
}

// retire : Whether one of his own rules has stopped earning its
// place, and the taking of it back.
//
// Cancelled rather than deleted. A rule that stops is a thing that
// happened and the record of it is worth more than the row: asked
// why he is no longer saying something, the answer is in the table.
func retire(
	ctx context.Context,
	store *jobmysql.Store,
	watcher noticing.Watcher,
	userID string,
	held []noticing.Spoke,
	now func() time.Time,
	logger *slog.Logger,
) {
	if len(held) == 0 {
		return
	}
	id, why, err := watcher.Retired(ctx, userID, held)
	if err != nil {
		logger.WarnContext(ctx, "could not weigh up retiring a rule", slog.Any("error", err))
		return
	}
	if id == "" {
		return
	}
	if err := store.Ran(ctx, id, time.Time{}, job.Cancelled,
		noticing.Mark+"retired: "+why, "", now()); err != nil {
		logger.WarnContext(ctx, "cannot retire the rule he wrote", slog.Any("error", err))
		return
	}
	logger.InfoContext(ctx, "he retired a rule he had written",
		slog.String("rule", id), slog.String("because", why))
}

// supersede : Whether a later memory has replaced an earlier one,
// and the setting aside of it.
//
// Set aside and not deleted, the same as a retired rule: asked why
// he no longer believes something, the answer should be in the row
// rather than gone with it.
func supersede(
	ctx context.Context,
	remembers *memorymysql.Store,
	watcher noticing.Watcher,
	userID string,
	now func() time.Time,
	logger *slog.Logger,
) {
	if remembers == nil {
		return
	}
	// Both tiers. What reaches every turn and what is searched for
	// are equally capable of being out of date, and a fact that has
	// stopped being true has not stopped being wrong because it is
	// only read sometimes.
	var held []memory.Memory
	for _, tier := range memory.Tiers() {
		some, err := remembers.All(ctx, userID, tier)
		if err != nil {
			logger.WarnContext(ctx, "cannot read what he remembers", slog.Any("error", err))
			return
		}
		held = append(held, some...)
	}

	id, why, err := watcher.Superseded(ctx, userID, held)
	if err != nil {
		logger.WarnContext(ctx, "could not weigh up a memory", slog.Any("error", err))
		return
	}
	if id == "" {
		return
	}
	if err := remembers.Stale(ctx, userID, id, why, now()); err != nil {
		logger.WarnContext(ctx, "cannot set a memory aside", slog.Any("error", err))
		return
	}
	logger.InfoContext(ctx, "a memory has been superseded",
		slog.String("memory", id), slog.String("because", why))
}

// everyRule : How many of his rules to read before deciding.
//
// All of them, in practice. He has twenty-five and the question he
// is answering -- do I already watch for this -- is wrong if any are
// left out.
const everyRule = 500

// noticed : The pass where he decides whether anything he has just
// described is worth a rule of his own, and writes it if it is.
//
// Nil where there is nothing to think with or nowhere to keep the
// answer. A server without a model should go on doing everything
// else rather than failing at startup over a thing nobody asked for.
//
// Two models and a store, joined here because this is the only place
// that has all three. The judge decides whether a pattern deserves a
// rule and says so in one sentence; the writer -- the same one the
// rule tools use -- turns that sentence into a rule and refuses it
// if it cannot be made. Nothing he asks for skips the validation
// anybody else's rule goes through.
func noticed(
	look rules.Lookup,
	store *jobmysql.Store,
	ran *jobmysql.Runs,
	remembers *memorymysql.Store,
	reader *profile.Reader,
	writer rules.Writer,
	judge noticing.Ask,
	id func() string,
	now func() time.Time,
	where *time.Location,
	logger *slog.Logger,
) func(context.Context, string) {
	if store == nil || judge == nil || writer.Ask == nil {
		return nil
	}
	watcher := noticing.Watcher{
		Ask: judge,
		Describe: func(ctx context.Context, userID string) string {
			said, err := reader.Profile(ctx, userID)
			if err != nil {
				return ""
			}
			return said
		},
		Now: now,
	}

	return func(ctx context.Context, userID string) {
		// Whose pass this is, for every model call it makes.
		ctx = environment.Labelled(ctx, environment.Label{UserID: userID})
		mine, err := store.Mine(ctx, userID, "", everyRule, job.Active)
		if err != nil {
			logger.WarnContext(ctx, "cannot read his own rules", slog.Any("error", err))
			return
		}

		// What every rule in force has actually done, read once and
		// used by both passes. Owner: *"the model should look at the
		// current rules and how useful they are, and create useful
		// rules."* Before this, both passes saw names.
		held := done(ctx, ran, mine, now, logger)

		// Taking one back comes before writing another, and not only
		// for tidiness: the rules he keeps are what he is shown when
		// deciding whether to write, so a dead one left standing is a
		// reason to write nothing.
		retire(ctx, store, watcher, userID, held, now, logger)

		// And the same for what he has been asked to remember. A
		// memory that outlived the truth is read into every answer,
		// which makes it the worst of the three to leave standing.
		supersede(ctx, remembers, watcher, userID, now, logger)
		// Each purpose in turn, each against its own allowance. A
		// week spent on useful rules does not silence the remarks
		// and a talkative week of remarks does not crowd out a rule
		// that matters.
		for _, purpose := range noticing.Purposes() {
			// Asked before the model is, because the allowance is
			// the whole safeguard and a model call made and then
			// thrown away is the allowance leaking money.
			if !noticing.Allowed(mine, purpose, now()) {
				continue
			}
			if made := wrote(ctx, watcher, writer, purpose, userID, mine, held, look,
				id, now, where, logger); made != nil {
				if err := store.Keep(ctx, *made); err != nil {
					logger.WarnContext(ctx, "cannot keep the rule he wrote",
						slog.Any("error", err))
					continue
				}
				mine = append(mine, *made)
			}
		}
	}
}

// catchingUp : The pass that writes down what was said and never
// written, or nothing where there is no model to ask.
//
// Given the diary and the lists and allowed to write to them, which is
// the only pass here that can. Everything else the assistant does
// unasked either speaks (a rule, a remark) or writes somewhere only it
// reads (a memory, the description). This puts entries in front of the
// person, so the reading tools come with it and the order is fixed in
// the judge: gather, read what is there, then write what is missing.
//
// The slow model and no hurry. Owner, on the passes that go and look:
// *"since there is no time constraint it can take its own time, here
// accuracy >> latency."*
func catchingUp(
	said catching.Said,
	answerer environment.Environment,
	registry *tool.Registry,
	cfg config.Config,
	logger *slog.Logger,
) func(context.Context, string) {
	if answerer == nil || registry == nil {
		return nil
	}
	pass := catching.Pass{
		Ask: investigating(answerer, registry, cfg.PlatformAI.Vendor,
			cmp.Or(cfg.PlatformAI.SlowModel, cfg.PlatformAI.Model),
			environment.PurposeProfile,
			[]string{
				"calendar_events", "calendar_add",
				"task_lists", "task_list", "task_list_add", "task_find",
				"memory_search",
			},
			persona.AddressFor(cfg.Assistant.Persona), Looking,
			environment.FeatureCatching, "looking for what was never written down", logger),
		Said:  said,
		Where: cfg.Assistant.Location,
		Now:   cfg.Assistant.Now,
	}
	return func(ctx context.Context, userID string) {
		looked, err := pass.Run(ctx, userID)
		if err != nil {
			logger.WarnContext(ctx, "cannot look for what was never written down",
				slog.Any("error", err))
			return
		}
		if looked.Read == 0 {
			return
		}
		logger.InfoContext(ctx, "looked back over what they said",
			slog.String("user_id", userID),
			slog.Int("messages", looked.Read),
			slog.String("wrote", looked.Said))
	}
}

// wrote : One purpose's pass, from judging to a rule ready to keep.
//
// Nil where there was nothing worth writing, which is the ordinary
// answer and not a failure.
func wrote(
	ctx context.Context,
	watcher noticing.Watcher,
	writer rules.Writer,
	purpose noticing.Purpose,
	userID string,
	mine []job.Job,
	held []noticing.Spoke,
	look rules.Lookup,
	id func() string,
	now func() time.Time,
	where *time.Location,
	logger *slog.Logger,
) *job.Job {
	thought, err := watcher.Wondered(ctx, userID, purpose, held)
	if err != nil {
		logger.WarnContext(ctx, "could not weigh up a rule",
			slog.String("purpose", purpose.Kind), slog.Any("error", err))
		return nil
	}
	if thought.Asked == "" {
		// Said out loud, because having nothing to say is the
		// ordinary answer and a pass that fell over says nothing
		// in exactly the same way. For five weeks a gateway
		// refusing the configured model read from here as a
		// quiet hour.
		logger.InfoContext(ctx, "nothing was worth a rule",
			slog.String("purpose", purpose.Kind))
		return nil
	}

	var holding strings.Builder
	for _, j := range mine {
		fmt.Fprintf(&holding, "%s: %s\n", j.Name, j.Schedule)
	}
	when, acts, why := writer.Written(ctx, rules.Request{
		UserID: userID, Asked: thought.Asked, Holding: holding.String(),
	}, look)
	if why != "" {
		logger.InfoContext(ctx, "he wanted a rule and could not write it",
			slog.String("asked", thought.Asked), slog.String("why", why))
		return nil
	}

	// A rule that speaks without reading anything says the same
	// sentence for months. Refused here rather than asked for in
	// the prompt, because it is the difference between remarking
	// and reciting.
	if purpose.Grounded && !noticing.Grounded(acts) {
		logger.InfoContext(ctx, "the remark he wrote reads nothing first",
			slog.String("asked", thought.Asked))
		return nil
	}

	name := cmp.Or(thought.Name, thought.Asked)
	made, err := job.Make(id(), userID, name, purpose.Kind, when, acts, now().In(where))
	if err != nil {
		logger.InfoContext(ctx, "the rule he wrote would not hold together",
			slog.String("asked", thought.Asked), slog.Any("error", err))
		return nil
	}
	// Why he wrote it, kept on the rule itself. A rule nobody
	// asked for has to be able to say what it is for, or the
	// only thing to do with it is delete it.
	//
	// In Because and not in Why: Why is overwritten with the
	// outcome of every run, so a reason kept there survives until
	// the first time the rule works.
	made.Because = thought.Why
	logger.InfoContext(ctx, "he wrote himself a rule",
		slog.String("name", name), slog.String("purpose", purpose.Kind),
		slog.String("because", thought.Why), slog.String("when", when.Describe()))
	return &made
}
