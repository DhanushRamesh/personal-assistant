# DEVELOPMENT

Instructions for anyone working on this server, human or AI coding agent.

**Read this before making changes.** It records decisions the owner has made
and how they want work carried out. These are not suggestions. Where this file
and your own judgement disagree, follow this file, or ask.

Last updated: 2026-09-21

---

## 1. What this project is

This is a personal AI assistant server owned by one person.

Work arrives over an API. An agent reasons about the request and uses tools to
carry it out. Progress and results are reported back. Requests are
long-running, so the API accepts a request, returns a chat identifier
immediately, and the work continues in the background.

It is a platform, not a chatbot. The AI provider, the tools, the clients
and the voice layer are all meant to be replaceable. The stable core is the
agent, its memory, and its chats.

**The assistant's name is configuration, not a constant.** It lives in
`[assistant] name` and is currently **Jarvis**. It was FRIDAY for most of this
project's life, and before that an early planning document called it JARVIS;
the point of the setting is that none of that has to matter to the code.
Nothing in the server may hardcode a name, and a test in
`internal/provider/platformai` fails if one reappears in spoken text.

---

## 2. What this is for

The primary interface is voice, through earbuds or a smart speaker. Everything
else follows from that.

```
  spoken                "Jarvis, check my merge requests"
     ↓
  earbuds → phone       speech recognised as text
     ↓  POST /v1/chats
  the assistant                runs the chat
     ↓  pushed as they happen
  "Let me take a look."         → spoken
  "Found four, reading them."   → spoken
  "Two look risky. …"           → spoken, and the chat is done
```

Four requirements follow, and they are not negotiable:

**Messages are pushed, not polled.** A client that has to ask repeatedly
stands in silence while the assistant works and then hears everything at once.
Pushing is what lets a message reach the user the moment it exists.

**A transient message must be something that happened.** The channel exists
for real progress — an agent naming the tool it is using, a long job
reporting where it has got to. It is not for filler. The Platform AI
provider used to invent `"Let me look into that."` before every answer and
repeat `"Still working on it."` while waiting; both are gone. Read aloud on
every single question, a made-up phrase grates, and it is worse than silence
because it sounds like an answer beginning. The client shows that the assistant is
working without being told. Do not add canned progress back.

**Messages are whole utterances, not tokens.** Token-by-token streaming is
useless to a speech synthesiser, which needs complete, well-formed sentences.
A provider emits `"Let me take a look at that."` as one message, and the
client's rule is simply that each message it receives is spoken. This is why
`Provider.Run` streams messages rather than text fragments.

**A message is written to be spoken aloud.** Not `Calling GitLab.getMergeRequests`
but `Let me check your merge requests`. Anything a user hears, including the
text of a failure, is phrased as speech.

Interruption follows too: saying "stop" while the assistant is speaking must cancel
the chat, so cancellation has to work mid-run rather than only between steps.

Server-sent events carry this, with the cancel endpoint as the return path.
WebSocket is not needed for it and earns its place only if audio is one day
streamed upward instead of being recognised on the phone. Alexa is a different
shape, being request-response with a deadline of a few seconds and unable to
hold a stream open at all; it needs its own approach and should not shape the
earbuds path, which is the primary one.

## 3. Working agreement

How the owner wants work done. Violating these is worse than writing no code.

### Build one piece at a time, and stop

Do one piece of work, report it, and **wait for explicit approval before
starting the next**. Do not build ahead. Do not bundle several packages into
one turn. Do not fill a wait on the owner with unrequested work.

If you are blocked on something only the owner can do, say so and stop.

### Propose, do not presume

Long multi-step plans are for discussion, not for executing unprompted.
Proposing a plan is not permission to begin it.

### Do not add attribution to commits

Git commit messages must **not** carry `Co-Authored-By: Claude` or any similar
trailer. Pull request descriptions must not carry a "Generated with" footer.
The history reads as the owner's own work.

### Earlier planning documents are context, not specification

A long design document was pasted into an early session describing
milestones, endpoints and architecture. The owner has said explicitly that it
was **background context only** and should not be followed literally. Treat it
as illustrative. Ask before implementing anything from it.

---

## 4. Decisions already made

Do not reopen these without being asked. The rationale is recorded so a later
reader can tell a decision from an accident.

| Decision | Rationale |
|---|---|
| **Go** with `net/http` + **chi** | chi is a router, not a framework. Handlers stay `http.HandlerFunc`, so no framework type leaks into the agent and tool layers. Gin and Echo spread `*gin.Context`; Fiber is built on `fasthttp` and breaks the `net/http` ecosystem. |
| **MySQL**, not PostgreSQL | Owner's decision, made after the tradeoffs were laid out. The known costs: no `pgvector` for semantic memory, no `LISTEN`/`NOTIFY` for event fan-out, weaker JSON indexing. Accepted. Do not re-argue this. |
| **No Docker** for now | Owner's decision. Use the MySQL already installed on the machine. Do not add `docker-compose.yml` or containerise anything unless asked. |
| **`config.ini`** for configuration | With environment variables overriding it. See section 5. |
| **`log/slog`** for logging | Standard library. No zap, no zerolog, no logrus. |
| Events use an **in-process bus** in V1 | MySQL has no `LISTEN`/`NOTIFY`. Single process makes this a non-problem. Revisit only if the assistant ever runs more than one process. Do not add Redis before then. |
| **GORM** for persistence | Owner's decision, made after the tradeoffs were laid out. The known costs: `AutoMigrate` is not a migration system, generated SQL is opaque, and the ORM's natural idiom (`db.Save`) bypasses domain invariants. Accepted. Do not re-argue this. |
| Domain types stay **free of GORM** | The mitigation for the above. Persistence uses its own row structs with GORM tags, mapped to and from domain types at the repository boundary. A domain struct must never embed `gorm.Model` or carry a `gorm:` tag. |
| GORM logs through **`internal/logging`** | GORM's default logger writes its own format to stdout, bypassing structured logging and credential redaction entirely. |
| Providers **invent no progress** | Owner's decision, after hearing it. A transient message is for something that actually happened; a phrase the code made up before every answer is filler, and spoken aloud each time it grates. The Platform AI provider now streams the reply and nothing else. The stub keeps canned updates, because its job is to exercise the transient path without a network. |
| **No signup endpoint** | Owner's decision. The assistant is on a public URL with the owner's Platform AI credentials behind it, so anyone who found the address and registered could spend the quota. Accounts are created with `make prod-createuser` over SSH instead. The web UI has a login page and no signup page. Do not add one. |
| **The server serves the web UI** | The Flutter web bundle is served at `/` by the assistant itself, with the API staying under `/v1`. One origin, so no CORS in production, one artefact to deploy, and Caddy already terminates TLS in front of it. Cross-origin requests are permitted outside production only, so that `flutter run -d chrome` can reach a local server with hot reload. |
| **Flutter** for the client | Owner's decision, made after the tradeoffs were laid out. One codebase for the Android app and the web UI. The known costs: STT and TTS come from community packages rather than the framework, server-sent events need a different path per platform, and Flutter Web renders text to a canvas so selecting and copying an answer is awkward. Accepted. Do not re-argue this. |
| Hands-free uses a **wake word**, not speaker recognition | Owner's decision. The requirement is that the assistant ignores surrounding noise and other people's conversation — which is the question "is this addressed to the assistant?", not "who is speaking?". A phrase answers it cheaply and reliably. Recognising a particular person is voice biometrics: it needs raw audio and an enrolled voiceprint, and it refuses to recognise you when you have a cold. Assistants use a phrase for these reasons. |
| The wake word is **the name at the front of the sentence**, with no model | Owner's decision, after Picovoice turned out to require a business email. Recognition runs continuously and only an utterance beginning with "the assistant" is acted on. It needs no account, no key and no model file, and it is said in one breath with the question — which a separate detector, having to hand the microphone over first, cannot do. The costs, accepted: audio streams continuously to the browser's recogniser rather than staying on the device, it is heavier on battery, and it will occasionally wake on the name said in conversation. An on-device detector can replace `NameFirst` without touching anything above it. |
| The voice layer is built and tested **on the web first** | Owner's decision. `flutter_tts` and `speech_to_text` both wrap the Web Speech API in Chrome, so speaking and listening can be exercised without a phone, and the same code serves Android. Only what is genuinely phone-only — audio routed to a Bluetooth headset, the earbud button, staying alive in the background — waits for a device, and the owner verifies those. |
| The earbud button is **Android platform code** | It is why Flutter was chosen over a PWA, and it is the one thing Flutter does not smooth over: media-button capture and background listening are reached through a plugin or a platform channel, not shared Dart. Budget for it as Android work. |
| **Voice and text are two interfaces over one core** | Owner's decision. Typing is the interface the core is judged on: it must be accurate given exactly what was asked, and a feature is not finished because it works when spoken. Speech is then made clear at the voice layer -- a better microphone, better transcription, a warning added to a spoken turn -- and never by shaping the core around what the decoder gets wrong. `heard()` is the pattern: it is composed into the prompt only for a voice turn and the typed path never sees it. Do not justify a core design by a speech-to-text failure. |
| Semantic memory is **embeddings compared in Go**, not in the database | Owner's decision. MySQL Community has no distance function, but the search does not have to happen in the database: vectors are stored as `BLOB`, held in memory, and scored in Go. Ten thousand memories at 384 dimensions is 15 MB and about a millisecond a scan, so the missing `VECTOR` support never binds. Chosen over `FULLTEXT` because word matching misses a typed paraphrase -- "what did the roofer charge" against a memory worded "quote for the roof" shares one word with an unrelated memory about roof tiles. |
| The laptop is the server **until memory matures** | Owner's decision. The e2-micro is not the target while this is being built, so nothing about memory is to be shaped by its 1 GB. Deploying it there is a later problem and may mean a smaller model behind the same interface. |
| The embedding model runs in a **container**, as an exception to no-Docker | Owner's decision, taken on accuracy: the laptop has 31 GB and a container can hold a model worth using, where fitting one in-process was a compromise for a host that is not the target yet. The only server-side dependency is an HTTP call behind `internal/embed`, so the model can be swapped or moved in-process later without anything above it changing. |
| The embedding model is **local**, not a hosted API | Owner's decision. No evidence Platform AI exposes an embeddings endpoint, and memory should not stop working when a quota or a network does. |
| Deciding a memory is relevant is **the model's job**, not a score's | Measured. Comparing vectors narrows a store to a few candidates but cannot say whether any belongs: a question with nothing stored still has a nearest memory, and it scores in the same range as a real match. Over twelve questions whose answer was stored and eight with nothing stored, no cut-off separated them; a cross-encoder rejected all eight but found only eight of twelve; the model shown three candidates found twelve of twelve and left all eight alone. Vectors shortlist, the model judges. `make evals` covers it. |
| Memories are written **when they are worth writing**, asked for or not | Owner's decision, revising written-only-when-asked. Having to say "remember this" is the thing being designed away. What makes it safe is the second half of the instruction: it must say what it has noted, in the same reply. A memory written silently is one nobody can correct; a memory announced is one the owner can reject in the next sentence. |
| The bar for writing one is **durable, not merely true** | The transcript already recalls everything said, so a passing fact needs no memory: it can be quoted from the record. A memory is for what should be *believed* afterwards -- a preference, a constraint, a decision and its reason, a figure agreed. "It is raining" is true and worth nothing tomorrow. |

### Hosting

Running on a **Google Cloud e2-micro** always-free VM, reached at
`https://friday-server.duckdns.org`. The requirement that decided it was **no
cold starts**: The assistant is voice-driven, so a sleeping server is
unusable, which ruled out the Render, Koyeb and Fly.io free tiers. Oracle
Cloud Always Free was the first candidate and was abandoned when its signup
declined the card. Section 11 has the shape of the deployment.

**The deployment target is `linux/amd64`.** The e2-micro is x86_64, not ARM —
an earlier assumption that the free tier was ARM produced a binary that would
not run. `make prod-deploy` sets this; do not introduce anything
architecture-specific.

---

## 5. Code conventions

### Testing

Every package has tests, and they live in the package they cover — a test
for `internal/api/chats` is in `internal/api/chats`, not somewhere that
happens to have the helpers. Tests assert behaviour that matters, not
implementation detail. Prefer a test that would catch a real bug over one that
raises the coverage number.

The API's tests are external test packages (`package chats_test`), which is
what lets them share `internal/api/apitest`. That fixture assembles the real
server, so a module's tests drive it through the real middleware and the real
routes rather than a stack rebuilt for testing; a module cannot then pass its
own tests while being mounted wrongly. `internal/api` also has a `package api`
file for the two things that need the unexported router.

`internal/chat/memory` is a real in-memory repository rather than a fixture,
because the runner and the API both need one and a copy in each drifts apart.

Where an interface has two implementations, the cases they must both pass go
in a shared suite rather than in either one's tests -- `internal/remind/
storetest` is the first. They have drifted before: an in-memory store that
accepted what MySQL refused made a test pass where the server would have
failed, and nothing said so until it was running. The suite runs in memory
always and against MySQL when it is reachable.

Where a test encodes a non-obvious requirement, say why in a comment. Existing
examples worth imitating:

- redaction matches attribute keys **exactly**, not by substring, so that
  `token_count` survives while `token` is redacted;
- a rejected state transition must leave the object unmutated;
- a generated DSN is round-tripped back through the driver's parser using a
  password containing `@ : / ?`;
- every chat endpoint is asked for another user's chat and must answer 404,
  which is how the SSE stream was found to be missing its ownership check;
- the route table is asserted whole, so a refactor cannot lose an endpoint;
- `views` pins the published JSON field names, since those are the contract a
  client reads, and asserts that no password hash or token hash appears.

Before proposing work complete, all of these must pass:

```bash
gofmt -l .        # must print nothing
go vet ./...
go test ./... -race
```

### The database tests need their own database

`internal/storage` and `internal/chat/mysql` talk to a real MySQL. They
migrate the schema and write rows they never clean up, so they run against
`assistant_test` and never the database the server uses.

They did not always. They took the defaults from `config.Load`, which name the
server's own database, and quietly filled it with a hundred and forty fixture
users, four hundred chats and six hundred messages before anyone looked. The
counts were only noticed while migrating the database to its new name.

Two things stop it recurring. The tests set `ASSISTANT_DATABASE_NAME`
themselves, and `refuseLiveDatabase` skips unless the name ends in `_test`, so
a mistake in the override cannot write anywhere real.

Create it once, as a MySQL administrator:

```sql
CREATE DATABASE assistant_test CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
GRANT ALL PRIVILEGES ON assistant_test.* TO 'assistant'@'127.0.0.1';
```

Without it the tests skip, which is why the suite still passes on a bare
checkout. `ASSISTANT_TEST_DATABASE` points them somewhere else if needed.

### Errors

Collect and report all problems at once where a user is going to act on them,
rather than failing at the first. Configuration does this; validation of user
input should too.

Error messages name the thing the reader can actually change. Configuration
errors give both spellings: `[database] port (env ASSISTANT_DATABASE_PORT)`.

Errors shown to the user are written in plain language. Internal detail stays
in the logs.

### Logging

Use `internal/logging`. Do not construct `slog` handlers elsewhere.

- Prefer the context-taking methods — `InfoContext(ctx, ...)`, not `Info(...)`.
  Attributes carried on the context are only attached when the context is
  passed.
- Attach identifiers to the context once, at the edge, with
  `logging.WithAttrs`. Do not thread a logger through function signatures.
- Never log a credential. Attribute keys in `internal/logging/redact.go` are
  redacted automatically; for anything else wrap the value in `logging.Secret`.
- Log levels: 5xx responses at error, 4xx at warn, health checks at debug.

### Structure

`cmd/server` wires the process together and owns its lifecycle: configuration,
logger, dependencies, HTTP server, graceful shutdown. It holds no routes and
no handlers.

Endpoints live under `internal/api`, one package per resource. Each holds its
own handlers and its own wire types on a `Handler` built from just the
dependencies it needs, and exposes them through a `Mount(chi.Router)` method:

    internal/api/          api.go — builds every module and mounts it; no handlers
    internal/api/httpx/    reading and writing bodies; knows nothing of the assistant
    internal/api/views/    the wire shapes of domain values, in one place because
                           a session detail carries chats and a login carries both
                           a user and a client
    internal/api/middleware/  request id, request logging, panic recovery
    internal/api/authn/    logging in, and the caller put on the context
    internal/api/health/   liveness and readiness
    internal/api/clients/  /v1/clients, /v1/me
    internal/api/sessions/ /v1/sessions
    internal/api/chats/    /v1/chats, including the SSE stream
    internal/api/assist/   /api/tags and /api/chat, for Home Assistant
    internal/api/apitest/  the shared fixture the modules' tests are built on

Dependencies point one way: every module may use `httpx`, `views` and
`authn`, and nothing may import a sibling resource. A shape two modules both
need goes to `views`; logic two modules both need goes to the domain, which is
why `chat.EnsureSession` lives in `internal/chat` rather than in `sessions`
— both `authn` and `chats` call it, and `authn` cannot depend on `sessions`
without a cycle.

`api.go` is therefore the whole surface in one screen. A module cannot
register an endpoint anywhere else, and `routes_test.go` asserts the complete
route table so that one cannot be lost or added unnoticed while code is moved
about.

Keeping all of it out of package main is what allows the whole interface to be
exercised in tests without starting a process. Those tests live in
`internal/api` and drive the assembled router, so they cover the middleware
stack and the routing as well as the handlers.

### The client is a web page, and only reads and writes text

`client/` is a Flutter web app: the sessions, the transcript inside one, a box
to ask something, and a settings page for the account and the clients holding
a token. Nothing else. Voice arrives through the Home Assistant satellite, so
the client does not listen, does not speak, and has no wake word.

It is not the client that was removed in `6255778`. That one was built around
voice — a wake word, Whisper on the device, a foreground service to stay
awake — and twenty-five of its fifty-eight files were that machinery. Reviving
it would have brought back a voice stack that the satellite replaced, under
the name the rename removed.

What was worth keeping was taken from that commit rather than written again:
the design system in `client/lib/design`, and the API client in
`client/lib/api`, which already covers every endpoint the server has. Both
were renamed on the way in. The rest is still in the history for anyone who
wants the Android build or the accent measurements.

Web only, and no build step beyond Flutter's. The bundle is meant to be served
by the assistant itself, so the page's own origin is the server's address and
nothing has to be configured; during development `--dart-define=ASSISTANT_URL`
points it somewhere else and the loopback CORS rule lets it through.

### Every page has an address

The settings pages are at `/settings/account`, `/settings/clients`,
`/settings/reminders` and `/settings/server`, and the conversation is at `/`.
Typing one opens it, the back button walks back out of it, and a link to a
page can be kept.

`client/lib/routing.dart` holds the whole of it: a route is either the
conversation or one settings page, `AppRoute.parse` turns a path into one, and
anything unrecognised lands on the conversation rather than on nothing. It is
hand-written rather than a routing package because there are five addresses;
a package would be more configuration than this is code. The screens know
nothing about it: `HomeScreen` is handed an `onSettings`, `SettingsScreen` is
handed its page and an `onModule`, and `main.dart` is the only place that maps
a route to what is on screen.

Two things are needed to make that work in a browser and neither is obvious.
Flutter's default on the web puts routes after a hash, so a page reads as
`/#/settings/account` and never reaches the server at all; `usePathUrlStrategy`
turns that off, behind a conditional import because it is web-only code.
And a plain file server answers `/settings/account` with 404, because no such
file exists — only an app that understands the address. `client/serve.py`
hands anything that is not a file on disk to `index.html`, which is what makes
a reload of a settings page work. A missing asset still gets its 404: falling
back for those too would turn a broken script tag into a silent blank page.

### Archiving and deleting are different things, and both exist

Archiving puts a session away: it keeps everything said in it, stops appearing
in the listing, and is never where a prompt lands. Deleting removes the row,
and the chats and the transcript follow it by the cascades already on their
foreign keys. There is no undo on the second, which is why the first exists
and is what a client offers first.

The two listings are separate rather than one with a filter. `EnsureSession`
takes the first row of the ordinary listing to decide where a prompt goes, so
an archived session reaching that would put a prompt back into a conversation
the user had put away.

**Removing the session a client is in starts a fresh one**, rather than
falling back to the most recent survivor as `EnsureSession` would. Having just
put a conversation away, being dropped into an unrelated older one reads as
the wrong thing happening. The server decides this and returns the session the
client is now in, because it is the side that knows whether what was removed
was the active one; a client that guessed would end up disagreeing with it
about where the next prompt lands.

`active_session_id` has no foreign key, so both paths clear it from every
client pointed at the session before the row goes. Left behind, it is an
identifier nothing can resolve, and the next prompt fails on the chats foreign
key instead of anything that explains itself.

### A client is one credential, and cannot raise its own

A client exists because a token exists. Signing in again on the same install
presents the client id it kept and gets that client's token re-issued, so a
browser signed out and back in stays one client rather than leaving a trail of
them, each still holding a working token. The previous token stops working,
which is the point: one client, one credential.

An id that is unknown, revoked or somebody else's falls through to registering
a new client rather than failing. The password is what authorises signing in,
and a stale identifier in a browser's storage should cost a fresh registration,
not a refusal. A revoked one is never revived — reviving it by signing in would
make revoking it mean nothing.

**A client cannot change its own channel.** The endpoint is authenticated by
the very token whose privileges it would raise, so a client able to set its own
could promote itself out of whatever the channel restricts. That was true when
the endpoint was written and is the one thing that would have made the channel
worthless as a gate. Correcting one is done from another client, which is the
realistic flow anyway: Home Assistant cannot call the API at all, and the
browser is where its channel gets fixed.

Worth being plain about what the channel is and is not. Today it enforces
nothing: it is read once per prompt, copied onto the chat, and never read back.
The security in this system is the token and revocation. The channel becomes a
control when tools arrive, which is why it has to be un-self-raisable before
then rather than after.

### Tools will be reachable differently by voice and by hand

Not built yet, recorded so the shape is not lost. When the assistant can call
tools, which tools it may call depends on how it was reached: a voice turn
gets a restricted set, while the interface and the API get all of them.

The reason is that voice has no confirmation step worth the name. A spoken
"yes" to something misheard is the whole authorisation, where a client can
show what is about to happen and wait. So anything destructive, anything that
spends money, and anything that leaves the house belongs to the channels that
can ask properly.

**A client declares its channel when it registers**, and every chat it submits
records it. `voice` or `direct`, and omitting it means direct — the answer that
grants less, so something with a microphone has to say so.

The client, not the endpoint. `/api/chat` speaks Ollama's wire format, and a
format says nothing about how the words were produced: anything able to speak
it can call it, and one day something typed will. What does know is the thing
holding the token, because a token is issued to one thing and that thing either
has a way to confirm before acting or it has not.

The chat carries a copy rather than a reference to the client, because the
runner — which is what would enforce a tool set — is handed a chat and nothing
else, and by then the request is over.

Some clients cannot declare themselves. Home Assistant is handed a token
through a configuration screen with no field for it, so its client registers
as `direct` — the answer that grants more — and stays wrong until somebody
notices. `POST /v1/clients/{id}/channel` is how that is corrected, and the
settings screen offers it beside Revoke.

That is a gap worth naming rather than a solved problem: nothing in the Home
Assistant setup says "this is voice", so reissuing that token starts it as
direct again, silently. The listing shows each client's channel so the mistake
is visible; correcting it is a click.

### One endpoint takes a prompt, and it is Ollama-shaped

`POST /api/chat`. Nothing else accepts one. `/v1/chats` reads and cancels;
sessions, clients and identity are unchanged.

There were two, and the second existed only because the first was thought of
as the voice one. It is not: Ollama's shape is a wire format, and the web
client speaks it as happily as Home Assistant does. Two ways in meant two
places to keep the superseding rule, the session rule and the channel rule in
step, and they had already drifted once.

Two things the `/v1` shape carried that Ollama's does not, both added rather
than lost:

- `session_id` on the request. Home Assistant omits it and lands in whichever
  session the client is active in; anything that knows which conversation it
  means says so, rather than having to switch the client's active session
  first and race whatever else holds the same token.
- `error_code` and `error_detail` on the final chunk. Omitted when empty, so a
  caller that does not know about them sees exactly what it saw before. What
  is said aloud stays in the message; these are for a screen.

**The endpoint is synchronous.** It answers when the chat is over, so there is
no submitting and polling. A client holds the response open and reads the
answer as it is produced, and stops by abandoning it. Anything that needs to
interrupt from elsewhere still uses `POST /v1/chats/{id}/cancel`.

### There is no server-sent-events stream, and no chat_updates

Both went together. The stream existed so a client could rejoin an answer it
had started; `chat_updates` existed so the stream could replay what a dropped
connection had missed. With one synchronous endpoint there is nothing to
rejoin: the answer arrives on the same response that asked for it.

Progress is still produced and still published — the runner announces it and
the endpoint writes it out as it comes. It is simply not stored. Where a chat
had got to is worth hearing while it runs and worth nothing afterwards, and
the answer itself was never in that table: it is on the chat, and the
conversation is in `messages`.

### A failure is said one way and recorded another

Two audiences want different things from the same failure. Somebody waiting
for an answer wants one sentence telling them whether to try again; whoever
is fixing it wants the exact words the service used and the status it used
them with. One line for both is either useless aloud or leaks internals into
a room.

So `internal/failure` maps a code to a fixed sentence and keeps the service's
own words beside it as the detail. The sentence is spoken and shown; the
detail waits behind "more info". The code is stored too, so the wording can
change later without the stored rows disagreeing with the live ones, and so
failures can be counted — "how often is this rate limiting" is a question
about codes, not about prose.

The shape is taken from the error module in `ulaa-ai-assistant`: a map of
codes to messages, an HTTP status table, a fallback that logs rather than
guesses, and the exact error carried alongside as `envError`.

**A failure is now given to the model, which it was not before.** The reason
it was withheld still stands on its own: a bare "something went wrong" read
back as conversation makes the model explain an outage it had no part in and
invent detail to fill the gap. What closes that gap is the detail. With the
exact error present there is nothing left to invent, and asking out loud what
precisely failed is answerable rather than a guess. That was the point of
keeping it.

### Home Assistant is reached on loopback, not on a LAN address

A reboot moved this machine from 192.168.0.102 to .107 and the voice stack
stopped: Home Assistant kept dialling the satellite at the old address, and
the server kept trying to announce at it. Both said so clearly and neither
could do anything about it.

Everything here runs on the one machine, and both containers use host
networking, so the address they use for each other is 127.0.0.1. A loopback
address cannot drift when DHCP hands out a different lease.

### A spoken failure says the exact error; a typed one keeps it aside

Typed, a failure shows the sentence and keeps the service's own words under
"more info". Spoken, there is no "more info": a sentence on its own leaves the
person with a failure and no way to reach what caused it, so both are said
aloud. Reading service jargon out is ugly and is still better than
withholding it.

### A blocked answer is not a bad request

Both arrive as a 400 and they want opposite things said. Nothing is wrong with
a request whose answer was blocked, trying again will not help, and telling
somebody their request was refused sends them looking for a mistake they did
not make. They are told apart by what the service said, because the status
cannot tell them apart.

That was found by asking for song lyrics: the endpoint answered "Output
blocked by content filtering policy", and the assistant reported that the
service would not accept the request.

### A spoken turn is told its words may be the wrong ones

"Can you unarchive any of the two conversations" arrived as "can you unlock
any of the two conversations". Speech-to-text does not misspell. It replaces a
word with another that sounds like it and leaves a sentence that reads
correctly and means something else, which is the failure typing never
produces and the one nothing here was prepared for.

A spoken turn now carries a note saying so: read for what was meant, and where
a word does not fit what is being discussed, consider what similar-sounding
word would. It is the same reason a typo in typed text is understood — the
reader knows the text is imperfect and reads through it. The model had no way
to know.

Only for a spoken turn. Typing means what it says, and quietly reinterpreting
a word somebody chose deliberately is worse than taking it literally.

The note stops short of guessing where guessing is expensive: where two
readings are both plausible and one of them destroys something, it asks.
Charitable reading is right until a charitable reading deletes a conversation.

A name is carved out of that charity entirely. Everywhere else a word
that does not fit can be reasoned about from the words that do; a name
cannot, because a name nobody has heard of and a name the decoder has
mangled look exactly alike. "Alekhya" came back as Alikia, Alakia and
alakia chintada in one evening. So a spoken turn is told to say back what
it heard and ask for the spelling.

It is told to ask **before** using it, not after. Given the rule in
general terms it still wrote "Sowmiya" into a memory and searched on
"Karunakaran", both from what it heard: two of five. Naming the acts --
do not write it into a memory or a reminder, do not search on it, do not
answer about it until they have spelt it -- made it five of five. A name
stored wrongly stays wrong, and nothing later will find it to correct.

This is the one place the assistant may end on a question. The spoken
rules forbid that because Home Assistant reopens the microphone on a
question mark; here reopening it is the entire point.

Only a spoken turn. Typing spells the name already, and there is an eval
that a typed one is never asked.

Primed separately at the other end, in voice-setup, where the decoder had
never been given the words the tools use. That priming is a list of words, not
example sentences, and it holds no names: the words that repeat are worth the
budget, and names are endless. It is drawn from the stored user messages, so
it primes what has actually been said rather than what was imagined. The
server is unaffected either way -- nothing here knows the decoder exists.

### Recall reaches everything said, not only what was written down

Owner's decision, revising "written only when asked". The assistant should
be able to reach anything that was ever said to it, not only the facts
somebody thought to save. Forgetting to ask it to remember is exactly the
case it is meant to cover.

This needs no new hoarding. Every message is already stored and condensing
deletes nothing -- it adds a summary and leaves the transcript alone -- so
the whole record exists and is merely unsearchable. Indexing it is what is
missing, and `message_vectors` is that index: one row per exchange, keyed
by the message somebody sent.

An exchange, not a message. A third of what gets said is unsearchable
alone -- "yes", "try again", "do both" -- and means something only beside
what it answered, so the row holds the message and the reply it drew. The
text is stored rather than rebuilt, so the words that were embedded and
the words that are shown cannot drift apart.

Searching reads every vector for that person and scores them in Go, capped
at the most recent `MaxScanned`. The cap is what stops a transcript of
years making every turn slow; it has never been reached.

The conversation in progress is left out. It is already in front of the
model, and offering it back reads as the assistant quoting itself.

Measured on the real transcript: 191 exchanges indexed, and five questions
worded to share nothing with what was said each found the right one --
"who sings the song Fireflies" found an exchange about Adam Young and Owl
City that does not contain the word Fireflies.

That is also safer than extracting facts automatically. An extractor
invents: it writes down a claim nobody made and it comes back later as
truth. The transcript returns what was actually said, with when it was
said, and can be quoted rather than asserted.

So recall has three sources, and they are different things:

- the always tier, in every prompt;
- curated memories, which are what the assistant **believes** -- distilled,
  correctable, and the only ones that can be edited or forgotten;
- the transcript, which is what **happened** -- complete, never edited, and
  including everything the decoder misheard.

An advisor needs both of the last two: something to reason from, and a
record to check itself against. They must stay distinguishable in the
prompt, because "you told me" and "I concluded" are not the same claim.

### The assistant is told what time it is

It had no way to know. A model answers "what day is it" from whenever it
was trained, or refuses, and "in twenty minutes" has nothing to be twenty
minutes after. Every prompt now carries the time, the day, the date and
the zone.

`[assistant] timezone` is an IANA name and everything stored stays UTC,
including the database connection. This is not about storage: it is what
"seven in the morning" means. A name the machine cannot load stops the
server rather than quietly becoming UTC, because a reminder at the wrong
hour every day is worse than a refusal that says why.

It was refused before this, in those words: "I do not have access to real
time data, sir, so the current time is not available to me." Spoken, on
25 September. Home Assistant has `prefer_local_intents` on and might have
answered it locally, and did not -- the question reached here and was
turned down. Do not assume a local intent covers something without
checking the transcript for it.

### The greeting is composed here, not by the model

`POST /v1/presence/arrived` says somebody has walked in. Home Assistant
does the deciding -- it watches how strong the owner's watch sounds and
works out when it has crossed into the room -- and what arrives here is
the conclusion. This chooses the words and speaks them through the same
announcer the reminders use.

The greeting is the hour and nothing else. Then any reminders held back
while they were out, then any that were never said at all. That is the
whole of it.

Two or three wordings per part of the day, never the same one twice
running. One fixed line per band is the same sentence every morning for
ever, and a greeting somebody can recite along with is not one.

"Welcome back" is in none of them. It used to be the small hours and the
late evening, and it reads as "you have returned" when all it meant was
"it is late" -- said, in testing at four in the morning, to somebody who
had not moved. If it comes back it belongs to a long absence, which is a
fact about the person rather than about the clock.

What is still to come is deliberately left out. A reminder waiting for
four o'clock is not news at half past one, and counting them at the door
turns a greeting into a status report.

A miss is said in words rather than as a count, and saying it here is
the telling -- it is marked mentioned, so the same miss is not raised
again in the next sentence.

**What is said unprompted is an announcement, and there are two kinds.**
Anything the assistant says without being asked is an announcement:
`conversation.ReminderAnnouncement` for a reminder or timer whose time
came, `conversation.PresenceAnnouncement` for a greeting and whatever
was held back with it as somebody walks in.

Told apart rather than lumped together, because a person reading the
conversation back wants to know whether it spoke because a time came or
because they came in, and the two are asked about differently: "why did
you tell me that" is about the clock for one and about the door for the
other.

They are shown in the conversation, merged with the chats in time order.
A greeting between two questions belongs between them -- read in any
other order the answer to an announcement comes before it. The server
returns them alongside the chats on `GET /v1/conversations/{id}` rather
than as chats: an announcement has no question, no status and nothing to
cancel, so making it a `Summary` would mean inventing all three. The
client labels it by what prompted it (Reminder, Welcome) and marks it
"announced", because nothing above it explains why it is there.

An unknown kind from a newer server reads as "Announcement" rather than
breaking the transcript.

The kind was briefly `aside` on 27 September 2026, renamed the same
afternoon to the owner's word for it. No migration: the kind never left
this development database, and its three rows were corrected by hand.

**What is said unprompted is written into the conversation.** Everything
else the assistant says is recorded as part of answering: a question
arrives, a turn runs, and both halves land in the conversation. A
reminder falling due and a greeting at the door went straight to the
speaker and were written nowhere, so the next thing the person said
arrived in a conversation that showed no sign of them having been
spoken to.

That is only a gap until they answer it. "How much time was I delayed",
after being told at the door that something was due at ten to four, is a
reply -- and a reply to nothing cannot be understood.

`internal/aside` writes it down. It resolves the conversation itself,
from the user's **voice** client: an aside is spoken into a room, and the
reply comes back from the same room. A reply typed into the browser half
an hour later is a different exchange and must not have the greeting put
in front of it. Nothing listening by voice means nothing is written, and
no conversation is created for a client that never heard anything.

It is written only after the words were heard, the same rule the store
follows: a note of a sentence nobody was told is read back as context by
the next turn, which then answers something that was never said.

The message is `conversation.Aside`, a kind of its own, because it is
the only assistant turn with no question in front of it. Its `Detail`
carries the hour it was said, in the person's zone, and `forModelContent`
renders it as `[Said aloud at 3:50 pm, unprompted.]`. **The hour has to be
in the words.** `toProviderTurns` sends the model `Role` and `Text` and
nothing else -- a stored message has a timestamp and a model reading one
back does not, so an aside whose hour is not written into it cannot be
asked how long ago it was.

Consecutive assistant messages are already merged by `ForModel`, so an
aside beside an answer raises no alternation problem.

## Every write is read back, and the answer has to say so

**The owner's rule, 27 September 2026: after every change, read it back
and say what moved, from what to what. After every deletion, count and
say there were n and now there are n-1.** Spoken aloud, because they
are listening rather than looking and have no way to check otherwise,
and because they trust it.

**The verification lives in Go, not in the prompt.** Every mutating
tool now reads before, writes, reads back, and returns both ends. The
model is not asked to make a second call: a check it can skip is worth
nothing, and today it skipped everything it was asked to do by prose.
`tool.Changed`, `tool.Removed` and `tool.Unverified` give every domain
the same sentence shapes.

`Unverified` is the case worth building all of it for. A write that
reported success and did not survive being read back is reported as a
failure, not a success. Before this, every one of these tools would
have said "done".

**Saying it is enforced too, because asking did not work.** Measured:
handed "the time was 9:00 pm, is now 10:00 pm", the model answered
"Moved to 10:00 pm, sir." The before is the half the person cannot
reconstruct.

So a `Result` carries `MustSay` and `Else`: the facts the answer has to
contain, and the sentence appended when it does not. `tool.Ensure`
applies it in the runner once the turn finishes. Appended rather than
rewritten -- what the model said is usually better, and this only adds
back what it dropped. Matching is a plain substring, deliberately: a
false negative costs one redundant clause, a false positive costs the
person the thing they asked to be told.

Covered: reminders (set, update, snooze, cancel), memories (remember,
update, forget), conversations (new, switch, rename, archive, delete),
calendar (add, cancel).

### A turn that changed nothing may not say it did

**2 October 2026.** Reviewing a stored conversation about a cinema
booking, two answers reported changes that had not happened.
`calendar_update` was refused for never having been described -- the
result said in so many words that it was not run -- and the assistant
answered "Meesaya Murukku 2 at 6:10 PM on Sunday the 4th, sir". The
owner then said "It's not updated in calendar" twice while it kept
insisting. The same turn later asserted a birthday from a
`memory_search` that had also been refused.

Everything above catches the bad write and hands the model the words to
report it. The model does not report it. So the denial is no longer
asked for: the runner counts, over the whole chat, the calls to tools
with `Writes` set and how many of them ran without failing. If it tried
and none took, `tool.Unchanged` appends "I have not actually made that
change" to the answer.

**Counted per chat, not per round**, or the server would call its own
corrections failures: refused for writing before reading, then reading
and writing properly, is a change made.

**Appended without reading the answer.** Deciding whether a sentence
claims success means matching English words, and a server that matches
on English stops working the moment somebody speaks Tamil to it -- the
same rule as everywhere else here. A model that already admitted it
costs the person one repeated clause; the alternative is being lied to.

A write that ran and found the value already correct counts as having
taken. The state is what was asked for, which is the only part the
person cares about, and `Changed` already says "nothing had actually
moved: it already held those values".

### A write that moved nothing says what it found

**2 October 2026.** `Changed` had a branch for a write that read back
identical -- "nothing had actually moved: it already held those
values. Say so rather than reporting a change" -- and it was the one
place here that asked rather than enforced. Asked, it was ignored.
Told "it's a three hour movie, so it's from 06:10 to 10:10", the
assistant set three hours, was handed that sentence, and answered
"Meesaya Murukku 2 from 6:10 PM to 9:10 PM, sir. That is three hours."
A settled fact, with nothing to say it was already so. The owner had
asked for ten and was left believing their film ran until then.

`Result.Regardless` carries a sentence appended whether or not the
answer already says it, and `Owed.Always` is how `Ensure` applies it.
It exists because that nothing changed is not a value that can be
looked for in a sentence, and the alternative -- matching the English
words a model would use to admit it -- is the thing this server does
not do anywhere.

**A turn that gave up on a change is sent back once.** The denial is
the safety net; this is the repair. Asked to rename a conversation,
the assistant was refused for writing before reading, handed the
listing and told "call it again with an identifier from this" --
fifteen lines, the last thing it read -- and answered "Done, sir. This
conversation is now called name test." It had everything it needed and
simply stopped. So when a turn has attempted a write, landed none, and
is now answering, it gets one more round with all of that still in
front of it. Once, like being sent back to look at the tools.

The rename failing is also what made the conversation look renamed
afterwards: the automatic naming only ever names an untitled
conversation, the explicit rename had not taken, so the titler did its
job on what was still untitled and announced it. That read as the
server overriding a name somebody chose, and it was the opposite.

**A read the server did while refusing is not work.** `Result.Read`
has to reach the next round's `Ran` or the model is handed what is
there and refused again for not having fetched it; but appending it
before the round is judged charges the correction against the hop
budget, which is the starvation that budget was changed to avoid. It
is appended after. Reasoned rather than measured: the scenarios where
it actually starves a turn need four correcting rounds in a row and
nothing short of that discriminates, so there is no test here that
fails without it.

**It names the values, not just the fact.** "Nothing changed" invites
the person to assume what they asked for was already true, and when it
is not, the number is the only thing that shows them. One sentence,
because it is heard: "was already" at the front carries across the
rest of the list.

**The end of an event was never read back at all.** `calendar_update`
compared the name, the start, the place and the notes. A length given
in minutes moves the end and none of those, so a real change was
reported as nothing having moved. It is read back now, as a clock time
rather than a length, because that is what somebody leaving the house
for a film has in mind. Whole-day events are left out: Google stores
their end as the day after, and "ends on Monday" for something on
Sunday is worse than silence.

**The contradiction itself is prompt work, and that is where it had to
go.** "Three hours" and "six until ten" are both internally consistent
arguments; no validator sees anything wrong and only somebody reading
the sentence does. So `calendar_add` and `calendar_update` carry one
line saying to work out whether the two agree, and if they do not, to
say so and say which was used. It is the last resort by the rule above
and it is used here because the server genuinely cannot see it.

### A write is read first, and the server does the reading

The other half of the same rule. A write is refused until its own
domain has been read in the same turn, because without it the model
writes from what it remembers or from an identifier it made up -- four
events were nearly deleted by calls carrying
`<id_for_first_single_day_event>`.

**2 October 2026: the refusal now carries the listing.** Asking for it
cost two rounds of a turn that has thirty seconds -- guess, be
refused, read, write -- and in one conversation about a cinema booking
that happened four times. `readFirst` runs the domain's listing itself
and hands back what is there, and `Result.Read` tells the runner to
count it as read so the next round is the write.

**The protection is unchanged.** What the guard defends is that a real
listing was in front of the model when it chose an identifier, not
which call fetched it. A model that wants a wider range than the
default still has to ask for one, exactly as before, and the refusal
says so.

**Only a listing the server can call itself**, meaning one with no
required arguments. A domain reachable only through a search is asked
for as it always was: the server has no business guessing what to
search for.

**Hot listings first** when a domain has more than one -- the diary has
its events and its calendars. The hot list is the measured record of
which one people actually call, which is as good an answer as exists
to which is a domain's main listing.

**A listing that failed is not recorded as read**, or the guard would
be handing the next call permission on the strength of nothing. Its
reason is passed on all the same, so the model can say what is wrong
now rather than meeting the same failure a round later.

## How prompt text is written

`internal/prompt` joins the pieces: `Text` for sentences of one
paragraph, `Block` for paragraphs separated by a blank line, `Lines`
for a list. Empty pieces vanish, so a caller may pass something
conditional without guarding it.

Everything the model is told used to be one expression of string
literals joined by `+`, carrying its own spacing: `"...given. " +
"Nothing else..."`. That is fragile in a way the compiler cannot see.
A missing trailing space joins two words silently, a doubled one is
invisible in the source, and where a paragraph begins depends on
remembering to end a literal with a newline. Now the separator is the
function and the pieces are a list, so the spacing cannot be got wrong
by forgetting something.

One sentence per line, which also makes a diff show which sentence
changed rather than which line wrapped.

**Converted against a byte-for-byte check.** The rendered prompt was
captured before and after each step and diffed; a first attempt at
this mangled it -- dropped sentences and turned real newlines into
literal backslash-n -- and the diff is the only reason that was caught
rather than shipped. Anything converted later should be done the same
way.

Done: the persona rules, the manners, `conversation.Heard`,
`reminders.WhenUnasked`, and `calendar_events`. Not yet: the
`Purpose`, `UseWhen` and `Avoid` fields on the remaining tool
definitions -- about seventy fields across five files -- which are the
same idea at a smaller scale.

**Anything newly written uses the constructors from the start.** The
unconverted fields are a backlog, not a licence: adding another `+`
chain to them makes the sweep bigger. Said by the owner on 28
September 2026, after a new `UseWhen` was written the old way.

## When the tool list outgrows the turn

Not a problem at 22 tools, which cost about 17,900 characters of
description -- roughly 4,500 tokens -- charged against the history
budget by `alongside`. It becomes one when a domain is added that is
rarely used and costs that every turn. Gmail is the likely trigger.

**The plan, agreed 27 September 2026: names always, schemas on
demand.** A name and one line is about 15 tokens against 800 for a
definition. The tools used constantly stay loaded; the rest are
registered as a name and a line, with a `tool_load` that returns the
real schema in the same format as the ordinary tool list.

**Never shortlist by relevance alone.** Sending only the tools that
look relevant fails silently: the model never learns the others
existed and simply answers worse. Names present means it can always
find out what it is missing. That is what keeps this a latency cost
rather than an accuracy one.

**The enforcement is an error, not an instruction.** Calling an
unloaded tool must fail with "X exists but is not loaded; call
tool_load with X first". `Registry.Call` already answers that shape
for a tool that does not exist, and would grow a case for one that
does but is not loaded.

The number to watch is not the count. It is whether tools still need
long `Avoid` text pointing at each other: seven of twenty-two do
today, which is already a little high and means a boundary is wrong
somewhere.

### What was built, and how long a description lasts

Built as `tool_describe` rather than `tool_load`, and it hands over
names rather than schemas: revealing a tool puts its real description
in the next request's tool list, so returning the schema here would be
a second copy -- and a tool result is a stored message, so that copy
would be re-sent with every later turn. `hot` in
`internal/tool/deferred.go` is the list described on every request,
with the measurement behind each entry beside it.

**2 October 2026: a description now lasts the conversation, not the
utterance.** The set of revealed tools was a local in `execute`, which
runs once per thing the person says. Asked four times about the same
cinema booking, the model called `tool_describe` for `calendar_update`
four separate times -- each one after a guessed call that had to be
refused, each one a round somebody waited through -- while the result
it was handed each time said "callable from here on".

**Read back out of the history rather than kept anywhere.** Both ways
a tool gets revealed leave a tool call in the conversation:
`tool_describe` names them in its arguments, and a tool called before
it was described is described afterwards. So `alreadyDescribed` scans
the window for tool calls and there is no state to evict, nothing to
key by conversation, and nothing lost to a restart.

**Scanned from the window, not the whole conversation**, which bounds
it for free. A tool last mentioned far enough back to have been
condensed away is not what this conversation is doing any more, and the
request stops carrying its description. That also stops a long
conversation from drifting back towards describing everything, which is
the cost the mechanism exists to avoid.

## One mechanism for reading before being asked

**Accuracy before latency, always.** The owner's rule, 27 September
2026. A prefetch refetches every turn unless a tool says otherwise, and
no tool does. If a read becomes too slow the answer is to make it
cheap, not to make it stale.

A tool sets `Prefetch: true` and the runner reads it before the model
is asked anything, rendering the answer into the prompt. That is the
whole contract. It replaced three hand-written blocks for one domain,
which would have become sixty for twenty domains; a new domain now
marks its listing and touches nothing in the runner.

Only argument-free tools may prefetch, checked when the registry is
built: nothing supplies arguments to a read nobody asked for, and one
called with none would answer about the wrong thing.

**A tool carries its own framing, in `WhenUnasked`.** This was learned
the hard way and the numbers are worth keeping. Asked "I am tired",
three fresh conversations each time:

| | volunteered a reminder |
|---|---|
| the hand-written block it replaced | 0 of 3 |
| bare listings, general warning at the top of the prompt | 2 of 3 |
| bare listings, warning in a tool result | 2 of 3 |
| per-tool framing, plus a general header as well | 1 of 3 |
| per-tool framing alone | 0 of 3, then 1 of 3 |

Two things fall out of that. A warning has to sit against the listing
it is about -- several thousand tokens away it is not heeded, and in a
tool result it is read as data rather than instruction. And a second,
more general warning on top of a specific one made things worse, not
better.

The framing belongs to the tool because only the tool knows what its
answer is for, and that is what keeps the runner free of every domain.

**This is at or near the old behaviour, not provably better than it.**
Three-sample runs cannot separate 0 from 1 in 3. What is certain is
the reading itself: twelve prefetches over four turns, three runs, no
misses -- where the model deciding for itself managed one or two in
four.

**Refuse-before-read is a different fault, and prefetching is not the
answer to it.** Asked to move a task to John's list, the assistant
answered *"I do not have a list called John's on record, sir"* -- a
claim about what exists, made without looking, about a list it had
never read.

`readFirst` cannot catch that. The guard lives in `Registry.Call`, so
it fires when a tool is **attempted**, and refusing to act attempts
nothing. The rule is read-before-write; this is refuse-before-read.

`task_lists` was prefetched for a few minutes as a fix, and the owner
took it out again: a domain is read when it is talked about, and
prefetching is not how that is arranged. The rule is carried in words,
in the same two places the calendar's is -- the tool's own `UseWhen`,
and a block in the persona.

**`naming`, and it covers every domain rather than one.** Saying a
thing does not exist is a claim about what somebody has and needs a
tool to have just run. The first answer to a name not recognised is to
look; the second is to name the nearest and ask; denying it is not one
of them. Said once for calendars, task lists, reminders, memories and
conversations together, because a paragraph per domain is how a prompt
becomes a wall nobody weighs evenly.

Whether it holds is not yet known. Prompt has failed on this class of
thing four times, and the honest position is that this is the
remaining lever rather than a fix that has been shown to work.

**The test could not see the tasks domain at all.** It named the
expected prefetch set while building a registry from four domains and
leaving out the fifth, so a flag appearing there failed nothing. Fixed
the same day, which is the second time this test has been wrong about
the thing it exists to check.

**Only memory prefetches, and that is the owner's decision.** 28
September 2026. `calendar_events`, `reminder_list` and
`conversation_list` had the flag, lost it silently in a rewrite, and
were restored -- and the owner then said to take it off all three:
*"it just increases the tokens and its unwanted"*. Reading three
domains on a turn that is about none of them is paid for on every
turn, and most turns are about one domain or none.

Memory is the exception, because it is about the person rather than
about a domain: there is no question it is irrelevant to.

Where a domain must still be read every time it is relevant -- the
calendar, for anything to do with a date -- the rule is carried in
words instead, in the tool's own `UseWhen` and in the persona. That is
weaker than fetching it, and it is the trade the owner chose.

**Nothing tested which tools prefetch, which is how three lost the
flag unnoticed.** The whole suite passed; the only symptom was the
assistant answering about dates without looking. `TestOnlyMemoryIsRead
BeforeTheQuestion` names the expected set, so a flag appearing or
disappearing fails there.

## Home Assistant answers first, and Jarvis never hears it

**Its replies are in the assistant's voice now.**
`custom_sentences/en/jarvis_voice.yaml` overrides 29 responses across
10 intents. A custom sentence file is merged into the whole intents
dictionary, `responses` included, and `default_agent` reads
`intent_responses` from the merged result -- so overriding a built-in
response needs no code and no fork.

Measured after a `conversation.reload`:

    "hello"                    -> Good day, sir.
    "who made you"             -> I was put together by the Home Assistant
                                  community, sir, and furnished here by you.
    "what is the time"         -> 3:50 PM, sir.
    "are you always listening" -> Only when you say the wake word, sir.

Only wording changed. The time keeps Home Assistant's own template with
the address appended, so the computed part is untouched, and every
sentence these answer still matches exactly as before.

**The manner now has a second home, and that is the cost.** The server
holds it in `internal/persona`; these lines are a hand-kept copy and
will drift. They are here rather than in the server because the server
never sees these questions -- Home Assistant answers and stops. The
alternative is routing each one to `conversation.jarvis` the way
`jarvis_timers.yaml` routes the timers, which keeps one voice and
spends a model round trip on every "hello". Fixed text was chosen for
the ones where nothing contextual is lost.

**Written to both copies.** `~/voice-setup/home-assistant/custom_sentences/`
is the repository and `~/homeassistant/custom_sentences/` is what Home
Assistant reads; the live directory is root-owned, so the file goes in
with `docker cp`. Editing only the repository copy is a mistake already
made once, with automations.yaml, and the walk-out test that followed
was run against unchanged config.


The Jarvis pipeline has `prefer_local_intents: True`. Home Assistant
matches the question against its own sentences before handing anything
over, and when one matches it answers itself. Measured on 29 September
2026, from the satellite's own log:

    "what is the time"  ->  "2:38 PM"
    "hello"             ->  "Hello from Home Assistant."

Neither reached this server. Jarvis would have said "2:38 pm, sir".

There are 41 built-in intents in `home_assistant_intents/data/en.json`,
and most are device control that Home Assistant should keep. Four
groups collide with what this assistant is for:

- **Time and date.** `HassGetCurrentTime`, `HassGetCurrentDate`. Two
  sentence patterns each, and they take the commonest question there
  is.
- **Timers.** Eight intents -- start, cancel, cancel all, pause,
  unpause, increase, decrease, status -- over roughly 320 patterns.
  Anything phrased as a timer is answered by Home Assistant's timers,
  which are a different set of things from the reminders this server
  keeps.
- **Lists.** `HassShoppingListAddItem`, `HassListAddItem` and their
  complete and remove variants. The persona says plainly that the
  assistant keeps no shopping list; Home Assistant will quietly add to
  one anyway, and answer "Added milk".
- **Small talk.** `HassRespond` answers hello, "who made you", "what
  can I say", "are you always listening". `HassNevermind` answers
  nothing at all.

The typed client is unaffected: it talks to this server directly and
never passes through Home Assistant. So the same question can be
answered by two different assistants depending on whether it was typed
or spoken, which is exactly what makes this hard to notice.

Turning `prefer_local_intents` off sends everything to Jarvis, and
costs the device control that only Home Assistant can do. Not yet the
owner's decision.

## Listening that will not end is two faults, not one

The owner, 29 September 2026: it wakes, it hears the question, and
then it keeps listening until the room happens to fall silent.

Home Assistant decides the end of a command in
`assist_pipeline/vad.py`, and the numbers there are worth knowing
because none of them are configurable:

| | |
|---|---|
| `relaxed` | 0.7 s of silence |
| `default` | 0.7 s of silence -- **the same** |
| `aggressive` | 0.25 s |
| `before_command_speech_threshold` | 0.2 |
| `in_command_speech_threshold` | 0.5 |

Two separate things follow. **The trailing wait** is the silence
window, and `default` was buying nothing that `relaxed` did not:
switched to `aggressive`, which is the only one of the three that is
actually shorter.

**The listening that never ends is the other threshold.** Anything the
VAD scores above 0.5 while a command is running counts as speech and
restarts the silence timer. A room that keeps producing such sound
never yields 0.25 seconds of quiet, and the window length is
irrelevant. Measured on `voice_only` with nobody deliberately
speaking: median 100 ms level of 323, bursts to 4357, peaks near
clipping. It is the bursts that hold the door open.

**There is no setting for "only my voice, and only when I am near".**
Nothing in the pipeline does speaker identification, and the VAD is a
neural classifier rather than a level gate, so turning the gain down
suppresses the owner along with the room. What exists is: shorten the
window (done), suppress noise before the VAD reaches it
(`mic_noise_suppression`, still `Off`), and the echo-cancel filter --
which the satellite only actually started using at 14:37 today, when
the container was first recreated rather than restarted. Any judgement
about the noise from before that time was made on the raw microphone.

Changed one thing, on the owner's own rule: two audio settings at once
is what turned "working but slow" into "not waking" on 28 September.

## A different network is a different place

The third presence piece, 29 September 2026, and it is not the one
that was planned.

The intention was to name the home network and require the laptop to
be on it. Asked which SSID meant home, the owner said *"it might be
both, or any wifi"* -- so no list of names can say where home is, and
the whole approach was wrong.

What the name does say, reliably, is that the laptop **moved**. A
different network is a different place, whichever place it is. So the
new rule decides nothing about where anybody is: it says that whatever
was true before is no longer known to be true, and turns
`input_boolean.in_the_room` off so the arrival rule has to establish
presence again from an actual measurement.

If the owner is sitting in front of the laptop when it joins the new
network, the arrival rule fires within half a second and the greeting
follows. That is the homecoming that was being missed -- without ever
knowing which network home is.

`sensor.laptop_network` is published beside the heartbeat, from
`iwgetid -r` rather than nmcli: it reads the interface directly and
does not care what brought the link up.

Guarded against the walk past `unknown`. A publisher restart or Home
Assistant coming up moves the entity from unknown to its value, and
neither is the laptop going anywhere.

Tested by setting the entity to another name: the rule fired,
`in_the_room` went off and straight back on, and the server received
the arrival twenty seconds later. The end state reads `present`, which
is right -- the owner is at the laptop, in the new place.

**What is still not covered.** A watch switched off or left on its
charger; and moving between two places on the same network name, which
a phone would catch and this cannot.

## The mind does not know the person has a phone

**Owner, 2 October 2026, after I had twice put this in the wrong
place:** *"the jarvis server should not know anything about whether
the phone is near or not. Let us treat the jarvis server as the mind
and the home assistant server as the body. The body has the mouth and
the ear. The ear should say the announcement only when the phone is
near the body, not the mind. The decision must be decided by the body,
so the logic must reside in the body."*

Both attempts put proximity in the mind -- first the home geofence,
then a distance between the phone and the laptop, where the laptop
learned its own position from a voice turn. Both worked. Both were in
the wrong place. The mind composes what to say; whether the room can
hear it is a fact about the room, and the room belongs to the body.

So `internal/presence` is deleted -- 806 lines, including code written
the same afternoon -- along with the arrival greeting in the events
handler, the voice turn that taught the server where it was, and the
`assistant.here` event. The mind keeps one question and asks the body
for the answer: `remind.Presence` reads a Home Assistant sensor and is
told yes or no, never why.

**Measured before choosing the beacon.** The laptop's adapter sees the
watch at -74 dBm and **cannot see the phone at all** -- not
advertising, not connected, not known to BlueZ. An Android phone does
not advertise over Bluetooth unless an app makes it: classic Bluetooth
is discoverable only while the settings screen is open, and BLE
advertising needs an app to do it.

The owner chose the Companion app's BLE Transmitter, which broadcasts
an iBeacon continuously. It is the body's beacon and nothing else: no
events pass through it, so the rule that Tasker reports to the server
and never through Home Assistant stands untouched.

**Still to build, all of it in the body.** The laptop's existing BLE
scanner watches for the phone's beacon instead of the watch and
publishes its signal. A Home Assistant script owns the gate, so the
mind asks for something to be said and the body decides whether to
play it -- returning whether it spoke, because a held reminder the
body drops in silence is lost. And the arrival automation calls
`/v1/presence/arrived` again, which is what it did before the decision
was moved into the mind.

**Nothing greets anybody until that is done.** The watch automation is
off and the phone's beacon does not exist yet. Silence is the right
state to be in: what it replaces was wrong six times in a day.

## Present, away, and the third answer

The second of the three presence pieces, 29 September 2026.
**The third answer was removed again on 2 October 2026** -- see
*Presence fails closed* below. What is still true here is the
`reason` attribute, `sensor.bluetooth_adapter`, and the argument
that a fault is not an absence; what changed is what to do about it.

`input_boolean.in_the_room` had two states, so every fault in the
chain arrived as the same word: away. A dead publisher, a suspended
laptop, a blocked radio -- each one marked the owner absent, held
reminders back from somebody sitting right there, and greeted them on
the way out of the fault.

`sensor.owner_presence` is `present`, `away` or **`unknown`**, and
carries a `reason` attribute saying which. Away means the measurement
worked and the watch is not here. Unknown means it could not be
trusted, which is a different thing.

Three parts to it:

- **`sensor.watch_adapter`**, published alongside the heartbeat:
  `ready`, `soft_blocked`, `hard_blocked` or `missing`, read from
  `/sys` rather than by running rfkill so it costs nothing per beat.
  Without it, "no watch in range" and "no radio" were the same
  silence.
- **The template sensor**, which is unknown when the publisher is not
  beating or the adapter is not ready, and otherwise passes through
  the raw verdict.
- **A guard on the away rule.** The template alone only describes; the
  guard is what makes `unknown` protective. The rule now refuses to
  mark anybody away unless the publisher is alive and the adapter is
  ready, so a fault can no longer drive a departure or the greeting
  that follows it.

Measured: publisher stopped, `owner_presence` went to `unknown` with
*"the publisher is not running"*, and back to `present` on restart --
where before the signal froze at -45 dBm and read as somebody standing
at the desk.

**The server holds reminders on a positive absence only.**
`Presence.Away` accepts `off` and `away`; `unknown` and `unavailable`
are silence, and silence is not a reason to keep quiet next to
somebody. `presence_entity` now points at `sensor.owner_presence`.

**What this does not fix.** A watch switched off, or on its charger in
the next room, still reads as away: no signal here separates that from
having left. Nor the 24-to-54-second gaps where BlueZ simply stops
reporting while the adapter is fine -- those are indistinguishable
from stepping out, which is why the forty-five second line was drawn
where it was. The remaining piece is the wifi SSID, for leaving with
the laptop.

## The two figures came back

Owner, on being told six minutes to notice a departure was by design:
*"what window drain? Yesterday until we switched to the phone it was
1:45."* They were right and I was defending a number that had replaced
a better one.

The watch had been judged on two figures: a median of three samples for
arriving, and a median of a hundred and fifty -- thirty seconds -- for
leaving, with away meaning the settled figure under **-73 for a minute
and three quarters**. That threshold was measured, and the comment on
it recorded the measurement: *-70 for ninety seconds marked somebody
away twelve times in three hours while they sat at the desk... at -73
there are fifteen dips rather than thirty-two, and nine of the ten long
ones survive.*

The detour through the phone replaced both with one figure, the
strongest reading of nine hundred samples. That is right for a sparse
beacon in a pocket with a body between it and the laptop -- a maximum
carries the verdict through the blocked stretches. It is wrong for a
watch heard continuously five times a second, and it is slow by
construction: a maximum holds the best reading for the whole window
however far away somebody walks. **When the watch came back this
afternoon, the phone's machinery and the phone's constants came with
it, and three hours of measurement specific to the watch were thrown
away without anything saying so.** The comments written today assert
"three minutes" as though it had been chosen for the watch.

**Re-measured before restoring it**, because -73 was tuned against a
figure the server had stopped computing. Twenty-five minutes at the
desk, 7,407 readings, no gaps:

    settled: median -59, strongest -50, weakest -69

    line   below   dips   dips >= 1:45
    -65      1%       7              0
    -68      0%       1              0
    -70      0%       0              0
    -73      0%       0              0

Zero false absences at -73 with four decibels of headroom, and the
doorway settles around -75 and outside around -82, so both fall below
it. Underpowered against the original three hours -- at the old rate of
roughly three long dips an hour, twenty-five minutes would expect one
-- so this confirms -73 is not too tight rather than proving it is
exactly right.

`binary_sensor.watch_is_near` is on as soon as the fast figure says so,
and off only after the settled figure has been under -73 for 1:45.
Departure is about two minutes again.

## Presence and arriving are two questions, not one

Owner, after seventeen minutes out: *"I went out and came back after
20 mins, I didn't get greeting msg."* It had fired. The automation
triggered, the server answered in 6.9 seconds, the satellite went
`responding` at 21:09:20 and `idle` at 21:09:23, and it said *"Good to
see you, sir. Did you manage to get some rest today?"* to an empty
room. They were still walking in.

**Measured, standing still in each place.** The first attempt read
`sensor.watch_signal` and got fourteen identical values, because that
sensor is the strongest reading of the last three minutes and cannot
say where somebody is now. Read from the adapter instead:

    at the desk   -54 to -62, median -56
    at the door   -71 to -81, median -75

Nine decibels of clear air between them.

**So there are two lines, because there are two questions.** `-85` is
whether the house may speak to them at all, and wants to be generous:
anywhere indoors counts, and holding a reminder back from somebody in
the next room is the expensive mistake. Arriving wants to be close, and
`-65` sits with margin at both ends of that gap.

`welcome_when_they_come_back` now waits for the signal to pass `-65`
before it calls the server, and greets anyway after two minutes --
somebody who comes in and sits across the room never crosses it, and a
greeting two minutes late beats a house that decided they had not
really arrived. The signal sensor being a three-minute maximum is
exactly right here: it rises the instant they come close.

**The model call made an old problem visible.** The fixed greeting was
instant, so the four seconds between crossing a threshold and speaking
did not exist. Three to seven seconds of composing, and the words can
be finished before anybody is in earshot.

**Still not walked outdoors.** Both measurements are indoors, which is
the point of them, and says nothing about where the signal sits in the
street. Under fail closed, too generous greets an empty room and too
mean silences an occupied one.

## Four reasons the greeting said absurd things

Run against real readings it said *"you have been moving around quite a
bit this past hour or two -- everything alright?"* to somebody who had
not moved fifty-eight metres in two days. The owner: *"I don't want
useless and absurd ones."* Four causes, all measurable, all fixed.

**It was shown the assistant's own machine.** The presence publisher
reports the laptop's network so the person's phone can be compared
against it. Those are facts about the laptop, and at a door they were
the loudest thing in the window and the least true. `Query.NotFrom`
leaves a source out, and `[homeassistant] this_machine` names which.
Not by kind: the same `network.joined` from the phone is about them.

**Nothing folded repeats.** Three hours produced thirteen events,
almost all one network going and coming back. Folded now -- one line
per thing with how many times and when it last happened. Folded and
not dropped, because the count is the signal: somebody ringing three
times in twenty minutes is exactly the thing worth asking about, and a
rule that threw repeats away would throw that away first.

**It had no idea what was ordinary.** The remaining noise was the
phone roaming between a router and its extender inside one house --
real, about the person, and not news. Nothing in the window could say
so, so each line now carries how many times that same thing has
happened in four weeks, and the prompt says what the number is for: a
thing that happens dozens of times a month is the texture of their
life; what is worth remarking on is what is rare or far more than
usual. A thing with no number has never happened before, which is its
own reason to look. Counted in the same read as the window, because
this runs with somebody standing in a doorway.

**And it tried to use a tool.** Asked to greet somebody it answered
*"let me check the calendar before saying anything"* and wrote out a
tool call that does not exist. The system prompt was the full persona,
most of which is about tools -- when one must have run, what to do
with what one returned, never saying what a day holds without having
looked. The rules were not wrong; they were the wrong rules, and no
tools are passed on this call at all.

`persona.Voice` is the greeting's own system prompt: who it is, how it
sounds, how to speak aloud, and nothing about acting. 1514 characters
against 9423. `spokenRules` split into `aloud` and `replying` to build
it, and the split earns itself twice -- `replying` holds "never end on
a question", which is exactly backwards at a door, where asking after
somebody is the entire point.

**Measured after:** the same nine-hour window gives *"Quiet afternoon,
by the look of it."* Six live arrivals in a row, all written by the
model, 3.2 to 7.6 seconds.

**The first greeting after a restart still falls back.** The provider
pays TLS and a token refresh on its first call, which took 8.2 seconds
against 1.6 once warm. Everything after it is inside the budget. Worth
a warm-up call at startup if restarts ever stop being rare.

## A stay is written while it is still happening

Owner, 2 October 2026: *"sending location.fix has no meaning here, it's
just timed data -- can we send place.stayed."* It already was: the
greeting omits only `location.fix`, and `place.stayed` goes to the
model like everything else. What the question turned up is that there
were none, and why.

Measured: 102 readings over two days, and **the furthest any of them
got from the first was 58 metres**. One cluster, eight hours and
eleven minutes, still open. Nothing was broken; there was nothing to
record, because `Stays` returns only what has ended -- *whatever is
still open is somebody who has not left yet*.

That is right for history and wrong for now. The most describable thing
about a day is usually the part of it that has not finished, and
somebody at their desk since ten was invisible until they got up.

**`SoFar` returns the open one too**, carrying `Open`, which reaches the
payload as `still`. It has to have lasted `Settled` like any other, or
every arrival anywhere would start a stay that is mostly thrown away.

**Which needed `Amend`.** A stay's dedupe key is its start, so it keeps
that key as it grows and comes back as a duplicate on every reading --
correctly, since a duplicate is what it is. `Record` skips it, so
without an amend an open stay would freeze at the length it had when it
was first noticed, and close at that length. `Settle` now records what
is new and amends what `Record` reports as already seen. One row per
stay, rewritten as it grows, rather than a row every five minutes.

First one ever written, minutes after this shipped:

    place.stayed  {"at":"12.91100,80.06241","still":true,
                   "value":"home","minutes":501}
    occurred_at 06:28   received_at 14:49

Named "home" from the geofence, which still outranks everything.

**And the greeting works stays out itself rather than reading them.**
A stay is written when the reading that ended it arrives, up to five
minutes after somebody walked out of a place -- and they walk in here
within a minute or two of that. So the stay most worth asking about is
usually the one not yet in the table. The greeting reads the fixes and
calls `SoFar` directly, which costs one more query and removes the
timing entirely; `place.stayed` is omitted from its event list so the
written one and the computed one cannot both appear. It reads back
further than its own window, because a stay that began before the two
of them last spoke still ended inside it -- somebody who left for work
before breakfast did not start their day in the window.

Stays are not named at the door: naming is a call to a mapping service
and there is no room for one while somebody is standing there. The
coordinates stand in, and the one written in the background has a name
on it by the time anything else reads it.

## The greeting says what is coming, within reason

Owner: *"the calendar and tasks also should be sent, so that if I have
any event tomorrow or today or any task I have to complete will also
be notified."*

This partly reverses what is written above -- *what is still to come is
deliberately left out... counting them at the door turns a greeting
into a status report.* That was too strong. Counting them is still
wrong; saying one of them is not. Somebody walking in at eleven with a
meeting at nine is better off hearing it now than at nine.

So the diary to the end of tomorrow, and the tasks that are due by then
or were due already. **The rest of the tasks are a number, never a
list** -- sixteen open tasks is a normal number to have and a terrible
thing to be told at a door -- and the number is given to the model to
judge by, with the instruction not to say it. At most one thing is
mentioned, and only when hearing it now beats finding it out later.

**Read in parallel and bounded.** Measured against Google: the diary
takes 1.6 to 1.9 seconds and the task lists 1.0 to 1.2, so in sequence
they are three seconds in front of a model call that already takes two
to four. Together they get `Fetching`, 2.5 seconds, and whichever does
not answer is left out. A greeting without tomorrow's meeting in it is
still a greeting.

**Where the time at the door actually goes**, measured after, because
the first estimate was wrong and the logging now says so on every
greeting:

    total 14.5s   google 2.07s   model 4.43s   speaking ~8s
    total  9.3s   google 1.71s   model 1.74s   speaking ~5.8s

**Speaking is the largest part**, and it is not the plumbing -- it is
the greeting having got longer. Caching the diary would save under two
seconds of a ten to fourteen second door, so it is not the first thing
to fix. If this is too slow in use, the cheaper fix is the prompt: one
clause fewer is seconds fewer to say.

## The greeting is written, not chosen

Owner, 2 October 2026, on what it is for: *"treat Jarvis like a person
-- I met him two hours back, he greeted me, after that things happened,
Jarvis came to know them, and after two hours I see him, he greets
again and asks me things that he knew after that, like how a human
would ask."*

It was one of a handful of fixed sentences picked by the hour. That is
a doorbell: it knows somebody arrived and nothing else, and says the
same thing whether they have been gone five minutes or since Tuesday.
Now a model writes it, from what has happened since the two of them
last had anything to do with each other.

**The window is `LastSpoke`, and the choice of anchor is the design.**
Not how long they were out, and not when they were last greeted: when
the assistant last said anything to them, across every conversation and
every client. A conversation at the desk at four leaves nothing to
catch up on at five, even though nobody went anywhere. And announcements
count, so the greeting closes its own window -- a second arrival ten
minutes after the first has ten minutes to talk about and says hello.
That behaviour falls out of the definition instead of needing a timer,
which matters because presence flapped eleven times in a day before it
failed closed.

**The instruction teaches noticing, not kinds.** Something that happened
far more often than usual, lasted far longer, involved the same person
again and again, or is out of keeping with their ordinary day. No event
kind is named, so `call.missed` works the day Tasker starts sending it
with no wording change -- the same rule as the description's. One remark
at most: somebody who comes in and is asked three questions has been
interrogated. And it is told never to list events, read out a count, or
use the names events are written under -- it noticed something, it did
not read a log.

**Reminders are said by the model now**, not appended as fixed text
behind it. The owner's reason: once a model is writing the sentence
there is no call for a second voice behind it, and a person says "you
were going to do X at four" rather than reciting a row. The risk that
replaces is losing one, so the instruction is to say every one and the
prompt says that leaving one out is the whole of the harm. The fallback
path appends them verbatim, because that path exists for when nothing
is writing anything.

**Everything fails to the fixed sentence.** No model, a failure, an
empty answer, or taking too long all end in one of the old greetings
with the reminders behind it. Somebody is standing in a doorway and a
plain hello beats a silence while a provider is down.

**`Within` was measured, not chosen.** Two and a half seconds was the
first guess and it cut off nearly every attempt -- the first live
arrival fell back with "the provider said nothing". Measured on this
gateway with a real 2600-character prompt, three runs each:
`claude-sonnet-4-6` 2.0, 2.1, 2.4 seconds; `claude-haiku-4-5` 1.2, 1.8,
4.0. Five seconds, so a slow one still lands.

**And the fast model is not faster here.** `[platformai] fast_model`
exists and is deliberately empty: haiku has the better median and the
worse tail, and at a door the tail is what gets noticed. Worth
re-measuring rather than reasoning about if the gateway changes.

Live, with a seven-hour gap and a phone reporting three missed calls
from one person, it wrote: *"Good evening, sir. Priya called three times
this afternoon -- did you mean to ring her back?"*

**What it has nothing to say yet.** Tasker sends five kinds today --
`location.fix`, `network.*`, `place.*` -- and none of them is
interesting at a door. The calls in that example came from a hand-made
event. Until the phone reports calls, app use and the rest, this will
mostly say good evening, which is what it should say when nothing
happened.

## Who was called is knowable after all

I told the owner three times that Android would not say who an
outgoing call was to, having tested three routes and generalised from
them: the Phone Offhook event gives %CNUM as 0, the State Phone Call
profile gives 0, and `content query` on the call log from a Run Shell
action is refused outright --

    %CLOG = Error while accessing provider:call_log
            java.lang.SecurityException

Tasker holds READ_CALL_LOG, but a shell command it spawns runs in a
sandbox that does not inherit the app's permissions.

The owner found the fourth route. Tasker has **outgoing-specific
variables** -- `%CONUM`, `%CONAME`, `%CODUR` -- which it fills from
the call log *inside its own process*, where the permission does
apply. They describe the last outgoing call rather than the live one,
and are documented as reliable right after it finishes, which is
exactly when the Phone Idle task runs.

    before   {"value": "",                 "number": "0",   "seconds": 29}
    after    {"value": "Alekhya Chintada", "number": "+91…", "seconds": 21}

And the duration got more accurate with it: twenty-one seconds against
the twenty-nine that dial-to-hangup gave, because `%CODUR` is the
connected time from the log and does not count the ringing.

**The lesson is about the reasoning, not the API.** Three failures of
three mechanisms were treated as a property of the platform. They were
a property of the three mechanisms.

**One thing this broke.** A call carries the number beside the name,
and `Readable` only masks the *value*: the digits reached the prompt
through `rest`, the field renderer that prints everything else. Any
field that is a telephone number is now dropped there too. Nothing is
lost, because when the number is the only identity there is, it is
already the value.

## A caller with no name still has to be somebody

Owner: *"what about unknown numbers?"* They would all have shared one
label. `%CNAME` is empty for a caller who is not in the contacts, so
every stranger became the same unlabelled `call.missed` -- and four
calls from one stranger looked exactly like four calls from four,
which is the opposite of the one thing worth noticing about an unknown
number.

The device sends both now, and the server prefers the name. Tasker
cannot express "this one or that one" inside a parameter, so the
choice belongs where it can be tested -- the same reasoning as the
payload unfolding above.

**An unsubstituted variable counts as no name.** Tasker leaves the
reference in place when a variable has no value, so a missing name
arrives looking like `%CNAME`. Until now that landed as a real reading
of somebody called that. Every fault during the phone's setup was one
of these, which is why the handler already logs the reason a batch was
refused -- this is the same failure getting past the door rather than
being turned away at it.

Verified live: a name with a number keeps the name; an empty value and
an unsubstituted value both take the number; neither present is left
alone rather than invented.

**And the number is never read out.** Owner: *"the AI should not read
to me like +9166767, it should just say unknown number."* The label
keeps the number, because that is what tells one stranger from another
and four calls from one is the whole of what is worth noticing; only
what reaches a prompt is changed. `event.Readable` turns a bare
telephone number into "an unknown number", and both the greeting and
the description count on the value while showing that instead.

The description matters more than it looks: it is read before every
answer, so a number in it would follow the owner into every
conversation.

Seven digits is the floor, so a battery level or a count is not
mistaken for a telephone number; fifteen is the ceiling, which is what
E.164 allows. Spaces, brackets, hyphens and a country code all pass.

Live, with four calls from one unseen number: *"Good to see you, sir.
There have been a few calls from an unknown number this evening -- did
you catch any of those?"*

## Where a page is looking belongs in the address

Owner, after the events list stopped showing position readings:
*"have UI url bar query param filter too, so I can just refresh and
state is there."*

`AppRoute` carried only which settings page was open. Which section of
"What it knows" was showing, what the events were filtered to and what
was typed in the search box were all local state, so every refresh
came back to the top of an unfiltered list of memories. That is the
wrong place to come back to for a page whose whole use is narrowing
something down and then going away to check it.

The route now carries a map of query parameters, and the knows page
reads its section, kind and search out of it and writes them back:

    /settings/knows?section=events&kind=call.missed

**A map rather than a field per page.** The settings screens are the
only things with state worth keeping, each wants something different,
and a closed type would have to grow every time one of them learned a
new filter.

**Empty values are dropped**, so a cleared filter leaves the address
instead of sitting in it as `kind=`, and a plain look at the events is
a plain address worth sharing.

**And it reads as well as writes.** `didUpdateWidget` follows the
address when it moves without the page -- the back button, or a pasted
link -- rather than only pushing to it, which would make the back
button change the address and nothing else.

An unknown section name opens the first one rather than failing, for
the same reason an unknown path opens the conversation: a hand-edited
or shared address should land somewhere usable.

## A label is the value, in all three places

The owner, before the first call had arrived: *"does the profiling
understand?"* It did not. Put a week of calls in front of
`profile.Rhythm` and it produced

    call.received "seconds=140 value=Amit": 1 time, on 1 of 10 days
    call.received "seconds=209 value=Amit": 1 time, on 1 of 10 days

-- a man who rings most mornings, read as six unrelated events.
`valueOf` rendered the whole payload when it held anything besides a
value, so the label changed every time: counted once, paired with
nothing, and described as something that happened rather than
something he does. With the value alone it is

    call.received "Amit": 6 times, on 6 of 10 days, usually around 9am

which is a routine, and the thing the counted half exists to produce.

It was silently wrong for `place.stayed` too, whose minutes change
every five of them while the stay is open.

**This is the third place the same mistake was made in a day.** The
events tool omits `location.fix` because a unique label is noise; the
greeting keys its folding and its four-week counts on the value alone
for the same reason; and this did not. The rule, written down once:
**what an event is counted by is its kind and its value, and nothing
else in the payload may reach the label.** Everything else is detail
for whoever reads the line.

**And zero is nothing too.** The first real missed call arrived
correctly -- a name and a number, both populated -- and forty-three
seconds later the same profile fired again with both set to the string
"0". That is set, so the phone-side guard for being set let it
through, and left alone it would have built four weeks of history for
somebody called 0 who rings constantly. A name is not a number and a
telephone number is not one digit, so zero says nothing either way.

When neither the name nor the number says anything the value is
cleared rather than left holding whichever placeholder arrived, so
what counts these sees one nameless thing instead of a person called
%CNAME and another called 0.

**The screen was the one place left showing them.** Owner, looking at
the events list: *"is it necessary to have location.fix on events
table, it looks like junk."* In the table, yes -- it is what stays are
computed from, and Near, Settled and Adrift have changed twice in a
week, so keeping the readings is what lets every past stay be worked
out again. Visible, no: three hundred coordinates a day pushed
everything that happened off the end of the list.

`GET /v1/events` leaves them out unless `kind=location.fix` is asked
for by name, and the kind counts still say how many there are, so a
person can see they exist and go and look. That is the difference
between the screen and the tool: a person typing the kind wants the
coordinates, and a model offered three thousand of something will
reach for it.

## Every field is named for what it holds

Owner, reading the events table: *"in all the events it is generically
said as value, instead we can give the correct name, useful for AI --
not just it, all the event params must be easy for AI to understand."*

The value itself has to stay called `value`, because the rule above
depends on it: what an event is counted by is its kind and its value.
It is the one field whose name is structural rather than descriptive.
Everything around it was named by whoever added the event, and a model
reads all of it.

So, audited across the phone, the laptop and the server:

| was | is | why |
| --- | --- | --- |
| `network.joined` / `network.left` | `wifi.joined` / `wifi.left` | the value is the name of a wireless network, and `network.joined "Dhanush"` reads as a person having arrived |
| `place.stayed` `at` | *(dropped)* | a pair of decimals that the counting cannot use and the model would read out; still recoverable from the readings |
| bluetooth `value` | the device, as it describes itself | below |

**The kind carries the context the value does not have.** A value is
one word with nothing around it, so whatever tells the reader what
kind of thing that word is has to be in the kind. `wifi.joined` says
the word is a network; `network.joined` left it open.

**A device says what it is, on the device.** A bluetooth name like
`btsb-007nb` means nothing to a model, and the obvious fix -- a lookup
in the server -- was refused: *"not in Jarvis, I want to add in Tasker
so Jarvis stays generic. The event should describe itself."* The
profile sets a variable to the bluetooth name, overwrites it with
"bike helmet" when the name matches, and sends that. The server learns
nothing about helmets, and the next device is one `If` on the phone
rather than a deploy.

The payload is `{"value": "...", "connected": "true"}`. Connected
rather than two kinds, because Tasker's BT Connection event fires on
both and hands the task `%bt_connected` to tell them apart; splitting
it on the phone would mean two profiles that can drift.

**And the variable names were looked up, not guessed.** Owner, after
the third wrong guess at a Tasker variable: *"always do a web search,
so you find out the variable names."* `%bt_name`, `%bt_address` and
`%bt_connected` are documented; `%bt_signal_strength` and
`%bt_battery_level` exist too and are not sent, because neither is
something to notice.

## A tool is only as wide as its description

Owner, with a screenshot: *"I think event tool has no proper
description."* They had asked *"with whom did I talk today?"* and been
told

> I have no tool that reads your conversations or call logs, sir.

The calls were in the table. `event_recent` would have returned them.
Its description listed where they had been, when they got in and
whether they had moved -- places and networks, which is what existed
when it was written -- and never people, so a question about people
did not match it and the assistant reported an absence instead of
looking.

**The description has to be about the record, not about today's
kinds.** Every event added from now on is a kind this text does not
name. So it names the phone, the watch and the laptop as the things
that report, says the kinds grow without the description changing, and
says plainly: when you are unsure whether something about their own
day was reported, call this once without a kind and read what it
lists, rather than telling them there is no record of it.

**Which is a licence to over-reach, so it is paired.** Measured: the
first wording took the eval from 38 of 38 to 40 of 43 -- "I am a bit
tired this evening" and "that took longer than I expected" both went
looking. The `Avoid` now draws the line at what the sentence is rather
than what it is about: nothing here is for something they are telling
you rather than asking about, and a remark about how they feel or how
long something took is to be answered, not looked up. That is 42 of
43.

**The last one is the harness, and it found something.** "My sister's
flight lands on the 3rd of March" stopped being remembered and started
calling nothing. The eval registers conversations, memories and
reminders, so when a description says *not for what is coming, which
is the calendar*, it points at a tool that is not there. Registering
the calendar proved it -- the flight goes to `calendar_events` -- and
also showed that three plain remarks go there too: *"it is raining
here today"*, *"I am a bit tired this evening"*, *"that took longer
than I expected"*. The calendar tools had the over-reach the events
tool just lost; that was fixed in the turn after this one, and the
calendar is registered in the eval now.

**The events tool is in the eval now.** Five cases, the first of them
the question that failed. A description is the part of this server
most easily wrong and least visibly so, and the only way to notice is
to ask a real model.

## A time said is not a time to look up

Owner, immediately: *"fix the calendar tool description too."*

`calendar_events` was reaching for the diary on *"it is raining here
today"*, *"I am a bit tired this evening"* and *"that took longer than
I expected"*. That was the standing rule working as written -- **the
calendar is the date and time hub, so any date or time mentioned means
read the diary first** -- and the wording was *anything to do with a
date or a time*, which "today" and "this evening" are.

The rule is right and stays. What was wrong is the test it offered.
The question is not whether a time was said; it is whether something
is being **placed** at a time. A birthday, a trip, a deadline, a day
they are turning over -- all of those have somewhere to be in a diary.
A word that only says when they are speaking has not arranged
anything, and three of the four over-reaches were exactly that.

So `UseWhen` reads *anything that puts something at a date or a time*,
and `Avoid` names the three sentences and says plainly: the test is
whether something is being placed at a time, not whether a time was
said.

**Measured both ways, which is the only reason to believe it.**
Narrowing a description is how a rule gets quietly lost, so the eval
gained five cases the rule exists for before anything was changed: a
plan with a date, a question about a named day, an availability
question, an appointment to write down, and a trip being turned over.
With the calendar registered and those cases in, 45 of 48 before and
48 of 48 after, twice.

The flight stopped failing on its own. *"My sister's flight lands on
the 3rd of March"* now accepts three first moves -- remember it, write
it down, or read the diary -- because with a calendar registered,
looking before answering is the rule, and what would be wrong is none
of the three.

## Every tool description, put in front of a model

Owner: *"now check all other tool descriptions the same way."*

The eval covered eleven tools out of twenty-nine. Tasks, mail, places
and the calendar were never asked about, so nothing in them had ever
been measured. All four are registered now, with eighteen more cases:
the task-reminder-event line, each of the five mail tools answering
the thing only it can answer, where-they-went against what-happened,
and the writes that need an identifier a read has to give first.

**45 of 48 before this, 66 of 66 after, three runs clean.** What the
three cost, and what each one turned out to be:

**Two tools claimed the same day.** `place_visits` answered "what have
I been up to today", which is not a question about a place. Both it
and `event_recent` said *also before remarking on their day
unprompted*, and `event_recent`'s own `UseWhen` listed "where they
have been" -- written before `place_visits` existed and left there.
Now the line is drawn in both: `place_visits` answers *where*,
`event_recent` answers *what else*, and a question about the day as a
whole is `event_recent` even though places are part of the answer.

**"Remind me to call the plumber at four" read the diary first.** It
went to `calendar_free`, which was obeying two rules at once: its own
*before putting anything in the diary at a time they proposed*, and
the persona's *anything to do with a date or a time is read from the
calendar before you answer*. A reminder takes up no time and clashes
with nothing, so there was nothing to check and the look cost a second
of a thirty-second turn.

Three attempts, which is the part worth recording. Saying it in
`calendar_free`'s `Avoid` did nothing. Saying it in its `UseWhen` as
well did nothing. Narrowing the persona from *placed at a time* to
*takes up the day* fixed it and broke the sister's flight, which takes
up nothing either. What worked was putting it on the **right** tool
rather than the wrong one: `reminder_set` now says nothing needs
looking up first, and names that sentence. **A rule lands where the
model is being told what to do, not where it is being told what not
to.**

**And the rule about remembering was being crowded out.** "The plumber
and I agreed twelve thousand for the bathroom" was remembered every
time with eleven tools registered and about half the time with
twenty-nine -- no wording changed, only how much else there was to
read. One tool's description is a smaller share of the prompt with
every tool added, and a rule that applies whatever is being discussed
does not belong in one. `persona.keeping` says it once, next to
`noticing` and `timekeeping`, which are there for the same reason.

**One pass is not a measurement.** The first clean run was followed by
a 65: *"I am a bit tired this evening"* read the diary, having not
done so for several runs. The three sentences it should be measured
against were written into `calendar_events`' own `Avoid` and nowhere
else, and the pull was coming from the persona, which the model reads
first and which only said the rule in the abstract. Naming the same
three sentences in `timekeeping` settled it: three runs at 66 since.
**A borderline case that passes once has not passed.**

**What this says about adding tools.** Twenty-nine descriptions were
written one at a time, each sound on its own, and three of them
disagreed with each other in ways nobody could see by reading them.
Every new tool is also an edit to every existing one. The eval is the
only place that shows it.

## A call with no number is still a call with somebody

Owner, pointing at a ninety-minute row in the events table: *"this is a
one and half hr call, it's noted as unknown, but it's from Alekhya in
Google Meet -- how to capture them."*

    call.received  {"value": "", "number": "0", "seconds": 5360}

The duration was right and the person was missing, for a structural
reason rather than a bug. Meet registers its call with Android's
telecom framework, so `PHONE_STATE` fires and the Phone Offhook and
Phone Idle profiles run -- but there is no telephone number in it, so
`%CNUM` is 0 and the contact lookup has nothing to look up. The guard
that turns 0 into nothing is why it read as unknown rather than as a
person called 0.

**The name comes from the notification, and the device resolves it.**
AutoNotification Intercept, filtered to `com.google.android.apps.tachyon`
by package rather than by app name, sets `%VOIPWITH` from `%antitle`.
Measured on a real call, the three lines it wrote were

    Created::Alekhya Chintada::Tap to return to the call
    Created::Alekhya Chintada::Tap to return to the call
    Cancelled::Alekhya Chintada::Tap to return to the call

so the title is the person. `%ancategoryname` and `%anpeople` came back
unset and are not used. Jarvis learns nothing about Meet.

**What the test call actually exposed was worse than a missing name.**
A nine-second Meet call to the same person produced

    call.made  {"value": "Alekhya Chintada", "number": "+91…", "seconds": 27}

Twenty-seven seconds was *the previous day's* call to her. The outgoing
payload was `{"value":"%CONAME","number":"%CONUM","seconds":%CODUR}` --
every field from the cellular call log, which a Meet call never enters.
It looked right only because the last call happened to be to the same
person; had it been to somebody else, the Meet call would have been
filed under their name. The branch between `call.made` and
`call.unanswered` turned on `%CODUR` too, so an unanswered Meet call
would have been recorded as answered for a stranger's duration.

**`%CODUR` is right about the call log and wrong about the call.** It
was adopted because it is more accurate than dial-to-hangup for a
cellular call, and that still holds. It is simply not evidence about a
call that was never written there.

**So each task now builds its payload before it sends it.** Both Phone
Idle tasks set a local `%payload` to the telephony version, then
overwrite it when the call was a Meet one:

    %payload = {"value":"%CONAME","number":"%CONUM","seconds":%CODUR,"via":"phone"}
    %payload = {"value":"%VOIPWITH","number":"","seconds":%OSECS,"via":"meet"}   if %VOIPWITH is set

`call.made` now fires on `%CODUR > 0` **or** `%VOIPWITH` set;
`call.unanswered` on `%CODUR < 1` **and** `%VOIPWITH` not set, because a
Meet call that never connected posts no ongoing-call notification.
Incoming is the same shape with `%CRING`/`%CRNUM`/`%SECS`, guarded
additionally by `%CRNUM ~ 0` -- telephony having no number at all is
what a VoIP call looks like from that side.

**A Meet duration is dial-to-hangup, and that is the price.** There is
no connected time to read for a call the log never saw, so `%OSECS` and
`%SECS` include the ringing. Accepted knowingly: a wrong name is worse
than a long duration.

**`%VOIPWITH` is cleared by whichever task used it**, inside its own
`If` block, so the one that did not handle the call cannot clear it
first. The residual risk is a Meet notification that never leads to a
Phone Idle run -- a missed Meet call -- leaving a name set for the next
call. The incoming side is protected by `%CRNUM ~ 0`; the outgoing side
is not, and a timestamp on `%VOIPWITH` is the fix if it ever shows up.

**And `via` is in the payload now.** A ninety-minute Meet call and a
ninety-minute phone call are not the same thing, and the field costs
nothing because the sender already knows which it was.

## Unknown is a name, and the channel is a field

Two decisions on 3 October 2026, both the owner's, both reversing
something written a day earlier.

**A caller with no name is "unknown".** Asked for directly: *"I will
get the value as contact name if contact name is there, else I want it
as unknown."* Until now the number became the value, because a label
has to tell one stranger from another -- four calls from one of them
is the thing worth noticing, and that was measured and written down.

That is exactly what this gives up. Every caller without a contact now
shares one label, so eleven calls from "unknown" could be one person or
nine. The owner was shown that in the choice and took it anyway, and
the reason is simple enough: the number was visible on the events
screen, and they would rather it never be a label at all.

The number stays in the payload. It stopped being what the event is
counted by; it did not stop being the record. `event.Unknown` holds the
word, next to `event.UnknownNumber`, which is still what a bare number
is called when it is spoken.

**And a number sent *as* the name is not a name either.** Writing the
cases out in full found the hole: the substitution fires when the
value is empty, which is what a cellular call sends for a stranger --
but a call from an app takes its value from the notification title,
and Meet puts the person's number there when they are not in the
contacts. That arrives set, so it would have sailed through and become
the label, which is the exact thing being removed. `event.Number`
already existed to decide whether a string is a bare telephone number
and nothing else; `labelled` uses it now, so the rule holds for every
sender rather than only for the cellular path, and the WhatsApp
profile inherits it. The number is moved into the `number` field
rather than dropped, because the sender had nowhere else to put it.

**Every call event carries how the call came.** *"I want via for
everything, because I will be adding WhatsApp call and other calls
too."* `call.missed` was the one left without it, and it has
`"via":"phone"` now.

**The channel is the sender's to name, not the call task's.** The Meet
payload said `"via":"meet"`, which would have meant editing both Phone
Idle tasks for every new app. `Meet Who` sets `%VOIPVIA` beside
`%VOIPWITH`, and the payload carries `"via":"%VOIPVIA"`. A WhatsApp
profile is then one more notification profile setting the same two
variables, and nothing in the call tasks changes. Same rule as the
helmet: the sender describes itself.

**A stale name expires after two minutes.** A missed Meet call posts a
notification but never reaches Phone Idle, so nothing cleared
`%VOIPWITH`, and the next outgoing cellular call would have been filed
under that name with `via` meet.

`Meet Who` stamps `%VOIPAT` with `%TIMES` on every Meet notification,
and both Phone Idle tasks begin with

    Variable Clear %VOIPWITH   If %TIMES-%VOIPAT > 120   (Maths)

placed before the `If %OSTART Set` / `If %CSTART Set` block, so it runs
on every call end whether or not that task handles the call. Nothing
else changed: every condition downstream still reads `%VOIPWITH Set`,
and a name that has expired is simply not set any more.

**Found live while testing it.** `%VOIPWITH` was still holding
"Alekhya Chintada" from the 11:57 call, hours later -- the trap was not
hypothetical, it was already armed. Verified by setting `%VOIPAT` to 1
and running the task by hand: the variable went from "Alekhya
Chintada" to empty. That also confirmed the part worth doubting, that
Tasker evaluates `%TIMES-%VOIPAT` as arithmetic in a Maths condition
rather than comparing it as text.

**It fails safe when `%VOIPAT` has never been set.** An invalid
expression makes the condition false, so nothing is cleared -- which is
the old behaviour, not a new fault. `%VOIPAT` is written by the same
task that writes `%VOIPWITH`, so in practice one is never set without
the other.

## Four hours nine kilometres from home, recorded as home

Owner: *"I went to a place, stayed there for almost 4 hours, but it
didn't capture place.stayed, and didn't set the geofence value for
that either."*

The stay was captured. It is the name that was wrong:

    12:20  place.stayed  {"value": "home", "minutes": 240}
    12:30 - 15:30        12.9654,80.1265   steady for three hours
    home is              12.9110,80.0624   about nine kilometres away

**Two faults, and the first was made this morning.** `Called` matches
a stay to a place the person has already named, by being near one, and
reads the coordinates out of previous stays' `at`. Those were removed
a few hours earlier on the grounds that *"a pair of decimals is the one
thing in here nothing downstream can use"*, and `JSON_REMOVE` was run
over the existing rows. The comment inside `Called` says what `at` is
for. It was not read.

So `LatLon` failed on every known stay, `Called` returned nothing
every time, and naming fell through to the only thing left.

**The second fault is what it fell through to.** `In` names a stay
from a `Fence`, and a Fence is a **window in time** -- from a
`place.entered` to the `place.exited` that closes it. The exit had not
fired. An open window has no end, so it went on putting "home" on
everywhere he went for the rest of the day.

Neither alone would have been visible. Together they renamed an
afternoon.

**The coordinates are back, and the thing that was right about
removing them is kept.** A coordinate must not reach a prompt --
"twelve point nine six five four, eighty point one two six five" is
unsayable. That belongs next to the masking of telephone numbers, not
in deleting the record: `event.Position` recognises a pair of
decimals and `rest` drops it, exactly as `event.Number` drops a
telephone number.

**And a fence is now believed only until the readings disagree.**
`Elsewhere` asks whether a place of that name is already on record
with coordinates and this stay is nowhere near it. If so the fence has
outlived its exit and its name is not used. Silent when the place has
never been named, which is honest: nothing here knows where it is, so
nothing here can contradict the fence.

The tolerance is six times `Near` -- nine hundred metres. A hand-drawn
geofence is wider than the scatter of a few readings and a campus is
more than one stay's worth of ground; this has to be wrong only when
somewhere is plainly somewhere else.

**There were two of these functions, and the fix went into the wrong
one.** `Settler.called` names the stays that get written; the greeting
had its own copy in `presence.go`. The guard went into the copy that
writes nothing, so the stays kept the wrong name and the change looked
like it had failed. `event.Names` is now the only place a stay is
named and both call it.

**No recompute was needed.** `Settle` works the stays out again from
the readings on every fix and amends what it wrote before -- the
comment on it says why there is no cursor: *"a cursor that is ever
wrong loses a day of somebody's life quietly."* One location fix after
the fix shipped, the afternoon corrected itself:

    before   17:50  {"value": "home",               "minutes": 240}
    after    17:50  {"value": "Ammaniammal School", "minutes": 185}
             21:15  {"value": "Muthu Supermarket",  "minutes": 10}
             21:35  {"value": "home", "still": true, "minutes": 35}

185 minutes is what the raw readings say to the minute. The 240 was
the open fence merging the afternoon into the stay at home. The
ten-minute stop on the way back had not been recorded at all, and is a
supermarket -- which is the kind of thing worth being asked about.

Both new names came from Geoapify, which is what the naming fallback
is for: no geofence there, so the name comes from the coordinates --
and the coordinates had to be in the payload for any of it to work.

## One call, not fifteen

Owner, with a screenshot of `memory_forget` fired thirteen times in a
row: *"we can make the tools take array of values right... all tools."*

Each call took ten milliseconds and answered with a sentence: *"there
were 28 memories before, and there are 27 now. Tell them both
numbers."* Thirteen of those, to clear out a set of old cost notes.

The pattern was already here and two tools had missed it.
`task_remove`, `task_done`, `task_move` and `calendar_cancel` all take
an `ids` array and say *"Give every one you mean in a single call, not
one call each: they are counted and read back together, so the person
is told once how many went."* `memory_forget` and `reminder_cancel`
took one identifier.

**The schema could not express what memory_forget needs.** It takes an
identifier *and* the subject that identifier is expected to have, so a
wrong one forgets nothing. Batched, those have to stay paired, and
`Property` could describe an array of strings but not an array of
objects. Two parallel arrays matched by position is precisely the
shape that lets them drift, so `Property` gained `Properties` and
`Required` instead.

**Both check everything before changing anything.** A batch with one
bad identifier changes nothing rather than half of it; otherwise the
person is told it failed with no way to know which half went.

**And `RemovedMany` rather than `Removed`.** `Removed` calls three
gone when one was asked for a partial success and says so -- rightly,
since "one fewer" is the surprise there. For a batch the surprise is
the opposite, and it already existed for the task tools.

What comes back is a count: *"Forgotten 3 memories, including 'Milk
cost'"*. Reciting fifteen subjects is what taking a list was meant to
stop, and a test fails if more than one is named.

**What was left alone, and why.** `mail_read` takes one on purpose --
"a body is long, and reading several fills the answer with text nobody
asked for". The update tools take one because applying the same change
to many is rarely what anybody means. 71 of 71 on the eval after.

## Every hop was ready for a stream except this one

Owner, looking at the latency breakdown: *"stream:true in platform ai
works I guess, check."* It does.

    event: generate
    data: {"messages":[{"content":"Ready"}],"object":"stream.chunk"}

The client had never asked. Its own comment said *"the service answers
in one piece"*, which was true of how it was being called and not of
the service.

**Checked before building, because one answer could have killed it.**
A streamed reply only reaches a speaker if every hop after it carries
one:

| hop | streams |
| --- | --- |
| gateway to here | yes, once asked |
| here to Home Assistant | the endpoint already did |
| Home Assistant's Ollama integration | sends `stream=True`, reads deltas |
| its pipeline to TTS | `tts_stream.supports_streaming_input` |
| Piper | `supports_synthesize_streaming = True` |

Piper was asked directly over Wyoming rather than believed from the
integration's source. Everything between the model and the speaker was
waiting for text that arrived in one lump.

**A tool call arrives in pieces and has to survive being put back
together.** The name and the identifier come once, in the first chunk,
and the arguments as fragments of a JSON string -- `{"days`, `: 1`,
`, "kind`, `": "mus`, `ic"}` -- which are JSON only in that order. The
test holding this is the events the live endpoint actually sent,
recorded verbatim.

**`KindPartial`, because an update is the opposite thing.** The runner
speaks a `KindUpdate` beside the turn: it is a sentence about the
work. A partial is the answer itself and must never be narrated, or it
is heard twice -- once in fragments and once whole. For the same
reason the assist endpoint stops sending the finished answer once any
of it has gone out in pieces.

**And a sentence that comes with a tool call is not streamed.** A
model asking for a tool sometimes says what it is about to do; that is
progress, spoken beside the turn by the aside. Put through as a
partial it would be spoken as the answer and again as the aside. In
every streamed tool call measured, the call arrives before any text of
its own, so a text fragment is only put through while no call has
appeared in this round.

**What is still wrong.** A retried call passes no callback, so
whatever the first attempt streamed has already been said -- if it
streamed half an answer and then failed, the retry's answer is heard
after it. The retries only fire on a dead connection or a refused
token, both of which happen before any text, so this is a hole rather
than a fault seen.

## A round that can be sent back may not be spoken

Owner, the evening streaming went in: *"its repeating two times."*

It was. Two answers to one question, in the log under one chat id:

    23:17:00.058  chat_01M41E0HR...  pieces=32  out_tokens=75  reply_bytes=309
    23:17:04.768  chat_01M41E0HR...  pieces=35  out_tokens=75  reply_bytes=309

Not Home Assistant, which guards this properly -- it speaks the
finished text only `if not self._streamed_response_text`. Ours.

**The runner can send an answer back for another round, and streaming
had already spoken the one it rejected.** There are two such guards.
An answer reached without touching a tool, on a turn where tools were
offered, goes back once to look at them. A turn that tried a write,
landed none, and then answered goes back once to finish the job. Both
existed long before streaming and both were invisible: only the last
answer was ever sent. With partials, the rejected answer is already in
the speaker, and the round that replaces it -- usually near enough the
same words -- is heard as a repeat.

**Whether a round's answer is final is known before the model is
asked.** Both guards test `reached`, `pressed`, `nudged` and the write
tally, and a round that answers without tool calls leaves the tally
exactly as it found it. So `Request.Pieces` is decided at the call
site, and a round that could still be sent back is read whole:

    Pieces: !returnable(reached, pressed, nudged, wrote, tools)

**The round that matters is streamed anyway.** The guard for an
untouched tool cannot fire once a tool has been asked for, so every
answer that follows tool results -- the long ones, the ones worth
streaming -- goes out in pieces. What is held back is the first round
of a turn that uses no tools at all, which is the short answer where
streaming saves least.

**The pieces are still counted when they are not sent**, because
whether the wire streamed at all is not visible anywhere else: a reply
read and put back together looks exactly like one that arrived whole.


## It was streaming to Piper the whole time

Owner: *"check the Piper streaming support."* Every hop supports it,
and every hop was using it.

| hop | asked | answer |
| --- | --- | --- |
| Piper | Wyoming `describe` | `supports_synthesize_streaming: True`, 2.5.2 |
| Wyoming TTS entity | `async_supports_streaming_input` | returns that flag straight through |
| the agent | `ollama/conversation.py` | `_attr_supports_streaming = True` |
| the pipeline | `tts_output.stream_response` | both of the above, so true |
| the satellite | `satellite.py:574` | `tts_start_streaming == "1"` -> `play_tts()` |

**I had concluded it was not working, from the wrong event.** The
reasoning was that `TTS_START` matched `INTENT_END` to the millisecond,
so speech could not have begun early. It always matches: `TTS_START`
marks the pipeline opening its TTS stage, which happens straight after
the intent stage whatever the answer did. It is not when anybody hears
anything.

**The speaker starts at `INTENT_PROGRESS`.** In the satellite's log,
six hours of it:

    17:53:48.085  INTENT_START
    17:53:51.236  INTENT_PROGRESS {'tts_start_streaming': '1'}
    17:53:51.236  Playing TTS response: .../api/tts_proxy/nVrEB-A6gNlM...
    17:54:00.072  INTENT_END / TTS_START / TTS_END

Speech began 8.8 seconds before the answer was finished. To time a
voice turn, diff `INTENT_START` against `Playing TTS response`, never
against `TTS_START` or `TTS_END`.

**Two of the three early starts measured were flattered by a bug.**
Both runs with the largest head start were the double-answer turns of
the same evening: what began playing early was the answer the runner
then sent back. The honest number from that batch is the single-answer
run, which started 0.09s early because its answer was short enough that
the sixty-character threshold was not crossed until it was nearly
done. The head start is real and it is worth having; the size of it
has not been measured on a clean turn yet.

## A turn is over when the speaker starts

Owner: *"the first token arrival (as voice) is considered as the
response start time -- from my voice end till Jarvis voice start --
treat it as metadata while streaming."*

So the number that describes a voice turn is not how long it ran. The
answer is spoken as it is written, and by the time the turn ends the
person has been listening for several seconds. Timing the whole turn
describes a wait nobody had.

**The clock starts when the request arrives.** That is the nearest the
server stands to the person's last word: Home Assistant calls `/api/chat`
a millisecond or two after it finishes the transcript. The part the
server cannot see is the recogniser's, and that is measured on the
satellite.

**It stops at the first chunk carrying text**, whatever kind it is. An
aside is spoken too, so the wait ends when the aside goes out, not when
the answer does.

**`spoke_after_ms` on the final chunk**, alongside `error_code` and
`chat_id`: beyond Ollama's shape, omitted when absent, ignored by Home
Assistant. A pointer rather than a plain integer, because zero is a
real answer -- a turn can begin inside a millisecond -- and absent
means the turn said nothing worth timing. Logged as `the answer began`,
with the whole turn's duration beside it so the two can be compared.


## The aside is off

Owner, after the first clean measurement: *"currently stop aside
reciting."*

`[homeassistant] say_aside`, false by default. Nothing is built rather
than built and left unused, so the startup log says it: *nothing is
said while an answer is being worked out, by configuration*.

**What it was for has mostly gone.** It covered a wait. The answer is
now spoken as it is written, so the silence it filled ends a second or
two after it begins, and what is left is a sentence in front of every
slow turn.

**And it was landing on top of the answer.** Measured on the turn that
prompted this:

    23:44:44.476  the tool call arrives, carrying "checking what you
                  have been up to today"
    23:44:47.217  the first piece of the answer goes out
    23:44:47.435  the speaker starts the answer
    23:44:47.961  the aside stops being audible

Half a second of both at once. `settleAside` exists to prevent exactly
this -- its own comment says the two "go through different players and
nothing sequences them" -- but it gates the finished answer, and a
streamed piece is published straight through without passing it.

**Nothing else changes.** The sentence is still published to the chat,
so a screen shows what a turn is doing, and the model is still asked
for `saying` on a tool call. It is only not spoken.

**A measurement this invalidates.** `spoke_after_ms` times the first
text on the chat stream. The aside never travelled on that stream --
it went through the announce player -- so on a turn with an aside the
person heard the assistant before the number says they did. With the
aside off the number is the whole truth again, which is the other
reason not to leave this half-done.


## Answered at 7.2s was a 5.9 second wait

Owner, looking at a turn in the timeline: *"its 7.2s, but log the
first token received time too there."*

The timeline had one number for the answer, and it was the wrong one.
`Answered` is stamped when the answer was written down, which happens
once, whole, at the end. The answer is spoken as it is written, so the
person had been listening to it for more than a second by then.

**A column, because the moment is not recoverable afterwards.**
`chats.answer_began_at`, migration 00032. Nothing in the messages
holds it: the reply is one row written at the end, and when it started
existing is not in it. Null for every turn before this and every turn
that streams nothing -- a failure, a screen with nobody listening.
Null is not measured, never zero.

**Stamped on the first piece and stored at completion**, so it costs
no write of its own. `Chat.Begun` ignores every piece after the first,
because the ones after it are the same answer still arriving.

**A step of its own, in front of the answer.** `began`, rendered as
*Started answering*. Putting it on the answer step as another field
would have hidden the one number worth reading behind the one that is
not.

**Two clocks for one thing, deliberately.** `spoke_after_ms` on the
assist stream measures from the HTTP request arriving, which is the
nearest the server stands to the person's last word. `answer_began_at`
is on the chat's own clock, so its offset lines up with the tool calls
above it in the timeline. They answer different questions -- how long
did the person wait, and where in the turn did the waiting end -- and
collapsing them into one would make the timeline disagree with itself.


## Turning the aside off did not stop it being recited

Owner, after four questions re-run as an audit: the narration was
still coming out of the speaker. `say_aside = false` stopped the
announce player; it did not stop the sentence reaching Piper, because
Home Assistant speaks everything written to the chat stream and the
narration was being written to it as progress. What Piper said:

    "I'll check what you're currently listening to.
     You have not been playing any music today, sir."

**There were two narrations, and the wrong one was being spoken.**
`saying` is an argument on every tool: described, exampled, under a
length, taken off the call before it runs. Beside it, the model also
writes free prose, and that prose is what the provider was sending on
as progress. Measured over the four turns, the prose was a restatement
of the question every time -- *"I need to look at what you have
planned for tomorrow before I can answer that, sir"* in front of an
answer about tomorrow.

So the prose is dropped, kept only in the log, and `saying` is
announced instead.

**The audit, four questions asked before and after:**

| asked | before | now | first voice | answer proper |
| --- | --- | --- | --- | --- |
| what reminders do I have | 10.44s | 6.66s | 6.70s | 6.28s |
| events in September 2026 | 9.66s | 12.97s | 12.13s | 11.99s |
| nice day tomorrow | 14.04s | 9.89s | 3.28s | 7.02s |
| the platform I'm hearing | 13.42s | 13.30s | 10.74s | 10.63s |

Totals are not comparable -- the replies came back different lengths,
and September answered with 382 characters against 253 before, which
is most of why it got slower. First voice is the column that matters
and has no before, because the number did not exist.

**`STREAM_RESPONSE_CHARS = 60` decides every row.** Home Assistant
will not start Piper until the deltas it has accumulated exceed sixty
characters, and the count only grows when something arrives. The
narration in front of *nice day* was 83 characters, cleared the bar on
its own, and bought 6.6 seconds. The one in front of *September* was
44, cleared nothing, and the speaker waited for the answer: 12.13s
against an answer that began at 11.99s. A narration under sixty
characters buys nothing at all.

**Which leaves 132 examples in the wrong shape.** Every tool's
examples carry a `saying`, and of the 132 of them not one is over
sixty characters; the mean is 18. The argument's own description now
asks for twelve to twenty words, and an example beats a rule -- that
is why the argument exists at all, recorded further up this file. The
description is changed and the examples are not, so the next thing to
measure is which one the model follows.


## The first word costs five seconds, and the tools are not why

Owner, given the breakdown of a turn: *"we have to make the platform
ai return the first word faster -- what can we do"*, then *"what if we
use a faster model."*

Three quarters of a voice turn happens before any word of the answer
exists: the model picks a tool (4.18s mean), the tool runs (0.88s,
nearly all of it Google), and then the model waits again before the
answer's first token (3.82s). Everything that is not the model is
under a tenth of a second.

**The obvious suspect was the prompt, and it was wrong.** Forty-two
tools are offered on a voice turn, 112KB of schema, about 28,000
tokens -- roughly 90% of what the model is handed. Removing all of it
saves 0.85 seconds.

| tools | bytes | first word |
| --- | --- | --- |
| 42 | 112,260 | 2.72s |
| 20 | 40,936 | 2.79s |
| 8 | 14,855 | 2.05s |
| 0 | 0 | 1.87s |

There is a 1.9 second floor that no prompt change touches. So
deferring tool schemas, which this file has had a plan for since the
list started growing, is not worth building for 0.85s. It also
explains the earlier prompt-caching result, where cutting 4,412 input
tokens to 3 made the call *slower*: it was optimising something that
was not costing anything.

**The model is the lever.** The same question, the same forty-two
tools, three runs each:

| model | runs | mean |
| --- | --- | --- |
| claude-sonnet-4-6 | 5.24 / 4.69 / 5.32 | 5.08s |
| claude-haiku-4-5 | 4.01 / 3.32 / 3.04 | 3.46s |
| gpt-4o-mini | 2.68 / 2.12 / 5.12 | 3.31s |

gpt-4o-mini's mean flatters it: three runs spanning 2.12s to 5.12s is
not a latency you can promise anybody. Haiku is both faster and
steadier. `claude-3-5-haiku` is refused by the gateway.

**And it does not cost accuracy.** The tool-choice suite against
Haiku: 71 of 71. Sonnet's last recorded run was 65 of 66, but the
suite has grown since, so that is not a controlled comparison -- what
can be said is that Haiku gets every case in the current suite right.

**Set per client, not in the file.** The model picker in Settings
writes to the client row, so the Home Assistant client answers on
Haiku while the web client and Tasker stay on Sonnet. Voice is where
the wait is heard; a screen can afford five seconds. `[platformai]
model` stays as the fallback for whatever has not chosen.

**Two measurements kept as tests**, behind the evals tag, because
both questions will be asked again: `TestWhatThePromptWeighs` prints
what each part of a request costs, and `TestWhatTheFirstWordCosts`
times the first fragment against the size of the tool list. Neither
asserts anything -- a gateway's latency moves on its own, and a
threshold here would fail for reasons outside this repository.


## A voice stream has no status line

Owner, hearing *"starting a new conversation"* in front of *"a new
conversation has been created, sir"*: **"no need for aside now, turn
it off."**

Reversing the choice made an hour earlier, which was to keep the
narration and make it shorter and more useful. Hearing it decided it:
on a turn whose answer is one sentence, a sentence in front of it
saying the same thing is not a head start, it is the answer twice.

**Home Assistant speaks everything written to the stream.** That is
the whole rule, and it is why this kept coming back in different
shapes -- first the aside player, then the model's free prose, then
the saying argument. A narration written to a voice stream is recited
whether or not anybody asked for one, because there is no such thing
as a status line you can only see.

So the narration is announced on a screen and not on a voice turn.
Aloud it belongs to the aside player, which is off by default, and
which `settleAside` sequences against the answer so the two cannot
overlap -- the thing writing it to the stream could never do.

**The test for it had to be rewritten before it was worth anything.**
The first version read the conversation messages, where a narration
never goes, and passed with the fix removed. The second reads the
event bus through the harness's `listen`, and failed with the fix
removed for the right reason. `submit` makes a direct chat, not a
voice one, which is the other thing the first version got wrong.


## Haiku, measured on the voice path

The same five questions, said aloud, on claude-haiku-4-5 set against
the Home Assistant client in Settings. The per-round numbers are the
provider's own `first_piece_after`, logged for this.

| asked | pre-streaming | sonnet | haiku |
| --- | --- | --- | --- |
| what reminders do I have | 10.44s | 6.66s | 5.31s |
| events in September 2026 | 9.66s | 12.97s | 8.06s |
| nice day tomorrow | 14.04s | 9.89s | 10.45s |
| the platform I'm hearing | 13.42s | 13.30s | 7.39s |

**Its virtue is steadiness more than speed.** The answer round's time
to first token across the five: 2.02, 2.28, 2.66, 2.59, 2.10 -- a
spread of six tenths of a second. Sonnet's equivalents ranged from
2.16 to 7.26. Round one averages 3.11s against 4.18s. Over the four
comparable questions the turn fell from 10.71s to 7.80s.

**Mouth to speaker barely moved, and the reason is worth keeping.**

| | sonnet | haiku |
| --- | --- | --- |
| reminders | 7.40s | 5.68s |
| September | 12.47s | 7.50s |
| nice day | 3.62s | 10.19s |
| platform | 11.03s | 7.51s |
| mean | 8.63s | 7.72s |

*Nice day* spoke at 3.62 seconds on Sonnet because an 83-character
narration cleared Home Assistant's sixty-character gate and covered
the whole tool-running period. With the narration gone the speaker
waits for the answer, and the head start over the four is now between
0.11 and 0.93 seconds. A covered wait was traded for an honest one.
The mean improved by nine tenths of a second, not by three.

**The send-back is now the largest single cost.** Two of the five
answered without touching a tool, were sent back to look at them, and
paid for a whole extra round: 2.78s on *nice day*, 3.13s on *tell me
something about today*. Both then called the right tool, so the guard
earns its place -- but it fires on something like two turns in five
and costs about three seconds when it does, which is more than the
whole difference between the two models.

Neither send-back was spoken, and no narration was spoken. Both fixes
from earlier in the day held.


## Saying "look first" up front made it look less

The send-back costs a whole round -- 2.78s and 3.13s on two of five
measured turns -- and its instruction never depended on what the first
round said, so it looked like it could simply be said first. It
cannot.

Eight eval cases are right to answer with no tool, and they were the
risk I measured: none of them reached for a tool with the instruction
in front, and none without. Clean. Then the full suite:

| | score |
| --- | --- |
| without it | 71 of 71 |
| with it, first run | 70 of 71 |
| with it, second run | 69 of 71 |

Both regressions are the same shape, and neither is a question:

    "my sister's flight lands on the 3rd of March"  called nothing
    "I have to submit the form before Friday"       called nothing

The wording was *"if the answer turns on anything about them"*, ending
*"plenty of questions need no tool"*. A statement has no answer that
turns on anything, so the model did nothing -- against the standing
rule that anything placed in the future is offered to the diary.

**The over-reach probe could not have caught this.** It asked whether
the instruction makes the model reach when it should not. The damage
was the opposite, on cases the probe did not contain. Eight cases
chosen for one failure mode said nothing about the other, and the
clean result read as permission.

Reverted. The send-back stays and keeps costing its round. What is
kept is the measurement: `EVAL_LOOK_FIRST=1` runs the suite with the
instruction in the prompt, so the next attempt at the wording is one
command from an answer rather than a guess.


## An afternoon of testing is not a character

The description written on 4 October 2026 said he *"repeats himself
heavily, sometimes cycling through the same request a dozen times in
a row"*, *"loses patience quickly"*, *"checks both repeatedly within a
single session without anything having changed"*.

None of it was about him. It was a day of saying the same five
questions into the microphone to time them.

**A prompt rule was already there and did not hold.** It says
*"describe them, not what they asked the assistant to do"*. So the
repetition is taken out before the model sees it: a run of the same
thing inside thirty minutes becomes one thing.

**Removed rather than counted.** "Said eleven times" is still
evidence, and it invites exactly the sentence this exists to prevent.
Real insistence survives: the same question tomorrow is outside the
window and is kept.

**Matched on the letters and digits alone**, case, spacing and
punctuation dropped. Speech punctuates differently every time, so
matching the characters would fold nothing -- and the test is the same
in any language, which matching the meaning would not be.

On the real week: 400 messages to 356, 11% gone. The top of what went
is *"switch to a new conversation"* twelve times and *"what's the time
now"* five times, which is machinery rather than a person.


## What stopped is in no list of what happened

Owner, on what profiling is for: *"imagine you are a stalker and you
want to know about me with these details."* Then, on how to send a
month of events: preprocess them if it helps.

Everything the description was written from answers *what do they do*.
A model can read that off a list. Nothing answered *what did they stop
doing*, and nothing could: a thing that stopped appears in no list of
what happened. It exists only as the difference between two counts, so
it is computed rather than asked for.

`Changed` compares the last week against the three before it, per
label, and reports three things: no longer happening, new this week,
and happening at a different rate. Rates are per week, because the two
periods are different lengths.

**Nothing in it knows the name of a single kind of event**, which is
the standing rule here and the owner's first correction when this was
being designed: the incoming events describe themselves, so a prompt
or a function that names kinds rots the day a new sender is added. A
thing is whatever label it carries, and what matters is whether that
label used to turn up and does not any more.

**Three guards, each for a way of being wrong:**

- `Enough`, three occurrences. Two of anything is a coincidence, and
  announcing that somebody has stopped doing something on the strength
  of two is worse than saying nothing.
- `Spread`, two distinct days. Three in one evening is one occasion,
  not a habit that has since ended.
- `Swing`, twice or half. Devices miss things and weeks differ; a
  description noting every wobble is noise that reads as insight.

**And it says it does not know why.** A thing can stop because they
stopped doing it or because a device stopped reporting it, and the
counts cannot tell those apart. The prompt says so rather than letting
the model pick the interesting one.

**It reports nothing for the first four weeks**, when everything a
device has ever sent is new. Saying otherwise would describe the day
the phone was set up. There are three days of events today, so this
produces nothing at all yet -- which is the correct output and is what
the test pins.

**On sending a month of events at all.** Measured: 632 events over
three days is 11,400 tokens raw, but 449 of them are position
readings whose values are all distinct, so they fold to nothing. They
are already turned into stays by code and already withheld. The
remaining 181 fold 3.4 to 1, and better with volume, because the same
places and people recur. At the owner's expected volume a month of raw
events is around 300,000 tokens, so raw was never an option -- the
model sees counts, never rows.


## A dossier, not a summary

Owner, on what the description is for: *"imagine you are a stalker
and you want to know about me."* The brief was the problem. It said
*"write a short description, under two hundred words"*, which asks for
a summary, and a summary is the opposite of what was wanted.

Now: *work out what is true of this person and keeps being true. Not
a summary. Miss nothing that recurs.*

**Two parts, with fixed headings.**

`WHAT IS SO` is one line per thing, as many as the evidence supports,
and every line carries how many times and over what stretch of days.
A line that cannot take a count does not belong in it.

`WHAT THEY ARE LIKE` is prose, under a hundred and fifty words, and
is the only part allowed to say what the counts merely imply.

Prose was the whole of it before. Prose cannot be checked, so a wrong
line lives until somebody notices and says so; a line with a count can
be argued with. The owner chose prose knowing that, and this keeps it
while giving it something underneath.

**The method is seven generic rules** and names nothing: what repeats
is a pattern, what co-occurs explains itself, a repeated gap is a
duration, the first and last of a day is its shape, what stopped
matters more than what continues, what is missing is a fact, and what
cannot be made sense of is reported as unexplained rather than guessed
at or dropped.

**A test fails if the prompt names any kind of thing** -- wifi,
calendar, music, call, location and so on. It caught one on the first
run: *"a name they chose for a geofence is not an address"*. Nobody
would have noticed that by reading it.

**And it says to describe them, not their use of the assistant.**
What they keep asking about says what they care about; how many times
they asked, how they phrased it and whether they repeated themselves
are facts about a tool being used. The fold handles repetition inside
half an hour, and this covers the rest.


## The description was listed as one note among forty

Owner: *"while I ask anything in the conversation this profiling must
be given a weightage, because these are the real facts about me."*

It was not getting any. The standing description is kept as an
ordinary memory, which is deliberate -- `memory_list` shows it and
they can have it changed or dropped like anything else -- but the
listing rendered it as one more line of `<id>  <text>`, among single
facts somebody mentioned once. It read as a note and was weighed as
one.

It is not that kind of thing. The others are something they said; this
is what a month of their own devices and a week of their words add up
to, counted. So the listing gives it its own heading and says what it
is: counted evidence, why the assistant can speak about their habits
without looking anything up, and where it disagrees with what the
model would otherwise assume, it wins.

**It keeps its identifier**, printed with it rather than left out,
because it is still an ordinary memory underneath and neither
changing a line of it nor dropping the whole thing is possible
without one.

**And the total still counts it**, said as "counting the one above".
A number followed by one fewer line reads as a memory having gone
missing.


## Someone in the room, not something reading out events

Owner: *"Jarvis should talk to me on event arrival -- if I get a
missed call for the third time from a number it can say 'sir, you are
getting calls from this number'. It should act like someone is in my
room."* And then, on what counts: *"it's not just reminding, it can
say facts too -- if I hear a song it can say it's a nice song."*

Somebody in your room does not read out what you just did, because
you were there. They mention the thing you would not have noticed. So
nothing here reacts to an event happening -- events happening is the
normal state of the world, hundreds a day. What earns a remark is an
event being different.

**Two shapes, and nothing names a kind of event.**

`Often`: the same label three times inside a quarter of an hour.
Three rather than two, because two of anything is a coincidence and a
remark made on a coincidence is worse than one not made.

`Novel`: a label with nothing like it in the record. This is the
owner's song: a thing he has not played before is worth a word, and
so is a number that has never called and a place he has never been.
One rule, no kinds.

**Two kinds are excluded, as mechanism rather than judgement.** The
raw position readings, because they arrive every few minutes and
every value is distinct, so unchecked every one of them would look
like something never seen before. And the music churn the states are
worked out from.

**The gate is the whole design.** What kills a feature like this is
not missing something; it is being a nuisance by Tuesday, after which
the remarks worth hearing are ignored along with the rest. Three
refusals, cheapest first: nobody is there to hear it, something was
said inside the last twenty minutes, or this exact thing has been
said already today.

Not said for ever -- somebody calling repeatedly today and again next
month is two pieces of news, and a gate that remembers for ever turns
the second into silence.

**Nothing is said when it cannot tell whether the room is empty.**
The same rule the reminder loop has, for the same reason: an
assistant that cannot tell talks to the wall.

**Being quiet is decided three times**, and finding candidates is
deliberately the generous one. After this a model may answer with
nothing -- which is a success, not a wasted call -- and the gate may
refuse the moment. A candidate that is never made cannot be rescued
by either.


## Wiring the remarks: a tick, three refusals and a way to decline

**A tick, not an arrival.** Events come in bursts -- six in two
seconds when somebody walks through a door -- and reacting to each
would make six remarks out of one moment. The watcher looks every
thirty seconds at the last five minutes. The window is longer than
the tick on purpose, so something arriving late or during a restart
is still seen; saying it twice is the gate's job, not the window's.

**One remark per look.** Two things worth saying in the same moment
are two remarks, and the second is still worth saying in twenty
minutes if it was worth saying now.

**The model is given a word to refuse with.** `NOTHING`. A model
asked to say nothing produces something anyway -- an apology, or a
sentence about having nothing to say -- and that would be spoken
aloud. Given something to return it returns it, and the recogniser
is generous about case, quotes and a full stop, because the cost of
misreading a refusal is reading the refusal out.

Silence is the ordinary answer and a success. The prompt says so
twice.

**Presence had to learn a new answer.** `Away` folds "cannot tell"
into "not away", deliberately: it chooses between losing a reminder
and wasting a sentence, and losing the reminder is worse. A remark
chooses between different mistakes -- saying nothing costs nothing,
and talking to an empty room is what makes somebody switch the
feature off. So `Here` answers properly, with "present" and "known"
separately, and the watcher refuses unless both are true.

**Its own announcement kind, `remark`.** Not a reminder, which is a
time coming round, and not a greeting, which is somebody arriving.
Audience `Asked`: the room and nowhere else. A remark is the
assistant being company, and company followed onto a phone in a
meeting is an interruption from something nobody asked a question.

It is written into the conversation like the others, so that "who
was it?" has something to refer to.

**Off unless there is somewhere to say it and something watching the
room**, and it says so at startup -- a silent assistant and a broken
one look identical in a log.

**It starts five minutes after the server does**, so a restart in the
middle of an evening of changes does not produce a remark about
whatever happened to be arriving at the time.


## The phone knows how the names are spelt

Owner: *"vocabulary should get updated with events."*

The list of names given to speech recognition was built from the
transcript, which is the one place the spellings cannot be trusted.
Everything in it has already been through a microphone: a week of
somebody saying "Alekhya" produces whatever the engine heard, and the
model is then asked to guess back from the mishearing to the name.

Their devices have the answer. A contact name is read off the phone's
own address book, a place is one they named themselves, a track is
what the player calls it. None of it has been through speech
recognition, and all of it is exactly the words that keep being got
wrong -- which is the entire purpose of the list.

So the four weeks of events are read alongside the week of words, the
values are taken most-reported first, and the prompt says plainly
which spelling wins: *where one of these is also in what they said
above but spelt differently, this is the spelling and that is the
mishearing.*

**Offered, not forced.** The rules about everyday words hold either
way. A network called Home and a song called Cars are as bad primed
from a device as from a transcript, and priming one of those makes
the ordinary word worse -- which is how "what is the time" once came
back as "what is the thing".

**Three shapes are never names**, and each is here because it would
otherwise be handed to a speech engine as a word to expect: a
coordinate, a telephone number, and the word stored when a caller had
no name at all. Nothing else knows what kind of event anything is.

**Most-reported first, capped at sixty candidates.** There are a
hundred places on the final list. A name reported twenty times is one
they live with; a name reported once may be a stranger who rang.


## New without being frequent, and the track that is not the value

The owner's own example -- *"if I hear a song it can say it's a nice
song"* -- could not fire. The player's events are working material
and the watcher was omitting all of it, which is right for prompt
size and wrong here: that is the only place a track is named.

**So the only thing omitted now is the position readings.** There are
more of them than of everything else together and every value is a
coordinate; the rest of the working material is read. The watcher
folds by label anyway, so the volume that made churn a problem
elsewhere is not a problem here.

**But working material is a state, not an occurrence.** A player
reporting playing, then paused, then playing is one song being
listened to, not three of anything, and counting the toggles would
have the assistant remark on somebody using a pause button. So such
a thing may be `Novel` and may never be `Often`. New without being
frequent, which is exactly the half that was wanted.

**And the track is not the value.** A player reports the artist as
the value and the track in a field beside it:

    {"via": "spotify", "from": "Liked Songs",
     "track": "Poongatre Poongatre",
     "value": "Yuvan Shankar Raja, Benny Dayal, Na.Muthukumar"}

A label is the kind and the value and nothing else -- the standing
rule, so that two of a thing are counted as two of the same thing --
so novelty here is per artist, not per song. Letting the track into
the label to get the owner's example working would have broken the
counting for every kind of event at once.

Instead the candidate carries what the label leaves out, and the
remark is written from both: the counting uses the label, the
sentence uses the rest. A remark about music that cannot name the
song is not worth making, and nothing in the code knows that
`track` means a song.


## The sender says whether it is a state

Owner: *"we can add screen lock/unlock event?"*

Nothing in the server needs changing to take a new kind of event.
That is the whole of the design and it held -- except in one place,
which the question found.

A screen is unlocked fifty times a day. Under the repeat rule that
is a nuisance from the first hour, and what stopped the same problem
for a music player was a list of kinds inside the watcher. A list
means every toggle the person invents needs a line in this server
before it behaves, which is exactly the coupling everything else
here avoids.

**The answer was already in the payload.** A sender describing a
state writes the state down:

    "playing": "false"      a player
    "connected": "true"     a radio
    "locked": "false"       a screen

So a field that is true or false, other than the value itself, says
the event is a state rather than something that happened. A state
may be new -- the half worth having -- and may never be frequent,
because playing, paused, playing is one song and not three of
anything.

**The value itself does not count.** A thing whose whole value is
"true" is a sender saying something happened, not a sender
describing the state of something.

**And the rule it must not swallow:** a missed call carries fields
too, and none of them says true or false, so the third call from one
number is still counted. There is a test for each half, because this
is a rule that could quietly disable the feature it sits inside.

Screen events themselves still need a sender. Nothing here is
waiting on code: post the kind with a value naming the device and
`locked` beside it, and the watcher will treat it correctly the
first time it arrives.


## The largest thing in a day is the part nothing reports

Owner, on why the phone's locking was worth sending at all: *"it can
understand the pattern that at night the screen will be locked around
1 am and will be opened again by 9 am... next day it will be around
11 pm and open by 8 am... it can understand like I'm sleeping. I want
the model to think like a detective."*

It could not. Everything in the evidence counts things that happened,
and what tends to follow what is looked for inside ninety minutes, so
an eight hour silence was invisible to every part of it. Not a gap in
the data -- the largest thing in the day.

Silences over four hours are now listed. Three was tried first and
caught the ordinary sparseness of an afternoon; four is long enough
that something has actually stopped.

**And then the owner asked the right question: *"why are you writing
code, the prompt should be able to do it."***

Half right, and the half that matters. The model never sees the raw
events -- it sees counts, and there is no gap to find in *"wifi.joined
home: 51 times, usually around 9am"*. Sending the events instead does
not scale: three days is 11,400 tokens and a month is around 300,000.
So something has to make a silence visible at all.

But the first version grouped the silences by hour, took medians and
ranked them, which is the pattern-finding the model should be doing.
It also destroyed what it was meant to show: a median hides that
weekends differ, that the hour has been drifting later, that one week
was nothing like the others.

So they are listed, dated, one line each, and nothing else:

    - Tue 1 Sep 1:00 am to Tue 1 Sep 9:00 am, 8 hours

The prompt says which questions to ask of them -- what hour they
begin, how long they run, whether they fall on the same days, whether
they have been moving -- and says plainly that what they are is not
for the evidence to decide: the same shape at night and in the
afternoon are not the same thing, and somebody who works nights would
be described wrongly by anything that assumed.

**A test went with it.** It asserted that a single long gap produced
nothing, which was this package deciding what counted as a routine.
That is the model's job and the rule is already in the prompt -- two
occurrences are a coincidence. The test now checks the opposite: one
silence is listed, and the listing draws no conclusion.


## Silence has four causes and they look identical

Owner: *"the automatic speaking by Jarvis -- I should see in UI when
it triggered, why it didn't speak, if spoken what it spoke."*

The ordinary output of the whole feature is nothing, and nothing can
mean four things: nothing unusual arrived, the moment was refused,
the model decided it was not worth saying, or the watcher has stopped
running. The fourth is the one worth knowing about and it is
invisible beside the other three.

So every candidate that gets as far as being considered is written
down: `chats.remarks`, migration 00033, read at `/v1/remarks` and
shown as a fifth section of the Knows page -- **Unasked**. When,
about what, why it was interesting, what came of it, and either what
was said or why it was not.

**Only the considered ones.** A tick that finds nothing writes
nothing, or this would be two and a half thousand rows a day
recording that nothing happened. At the gate's current settings it
is perhaps fifty rows a day, most of them refusals, twenty thousand
a year.

**The gate had to learn to say why.** It returned a bare yes or no,
and asking it afterwards would have given a different answer: the
clock moves, and a gate that refused because something was said
thirty seconds ago refuses for another reason a minute later. It
returns the reason with the refusal now -- *"they are not in the
room"*, *"something was said 4 minutes ago"*, *"this was said 3 hours
ago"*.

**A route table test caught the new endpoint** before anything else
did, which is what it is for.


## Some things are said because of what they are

Owner: *"there are some definitive unasked announcements -- if my
battery goes below 25%, 10%, 5% it should say no matter what, and
when the battery or phone is getting too hot it should say."*

Everything built so far decides whether to speak by how unusual
something is. A battery at five per cent has been at five per cent
before and is not unusual at all; it is worth saying because of what
it is. No amount of counting finds that.

**The sender says so.** A payload field, `urgent`, true or the word
true. Which level is worth a warning is the person's own preference
and belongs on the device that knows it -- a threshold compiled into
this server would be wrong for the next person and wrong for him in
a year. The phone decides it is urgent; the server decides how to
say it. Nothing here knows what a battery is, and the same mark
works for heat, for a door, for anything a device is ever taught to
insist about.

**Three things it bypasses, each for a stated reason.**

The quiet between remarks exists so company does not become a
nuisance. A battery about to die is not company, and somebody not
told because something else was said four minutes ago is worse off
than somebody interrupted twice.

The once-a-day rule goes with it: reaching five per cent twice in a
day is two warnings, not one and a silence.

And the model's refusal, which is the whole point of asking a model
and is exactly wrong here. A model weighing whether a dying battery
is worth mentioning is a model that will one day decide it is not.

**What it does not bypass is there being nobody to tell.** It used
to: a remark was company and belonged in the room, while a warning
followed them onto the phone. That was reversed later the same week
-- see *An immediate announcement is its own mechanism* -- and the
kind is now `immediate`, audience `Asked`.


## Told twice that he had had no calls, having had five

Owner: *"I got a call from Kliffy, why is it not saying that."* Then,
in the transcript: *"I didn't get a call from anyone today."* --
*"That is correct, sir. You have not received any calls today."* He
had had five that morning. Told to look again, it repeated itself
word for word.

The tool did exactly what it was told. The model asked for kind
`call`, which is how a person says it, and only a kind ending in a
dot counted as a family. `call` matched nothing, because what is
stored is `call.received` and `call.made`.

**The answer contradicted itself and nobody noticed:**

    Nothing of kind call was reported today. The kinds that do
    exist, with how many of each altogether, are: ... call.received
    (13), call.made (6) ...

The sentence says there are none and then counts nineteen. The model
read the first half and believed it, which is the right thing to do
with a tool result: the whole design rests on a tool being believed
over what the model remembers.

**A kind with no dot in it is a family now**, matched as a prefix
rather than by appending one, so a sender that one day reports a bare
`call` is found by the same question. A full dotted name is still
exact, and there is a test for each half -- widening the first must
not widen the second.

**What makes this the worst kind of failure.** Not that it was
wrong, but that it was confidently wrong about the person's own day,
twice, and the evidence to correct it was inside the sentence it was
reading. Everything else here is built on the assistant looking
things up rather than remembering: *"saying a thing does not exist is
a claim about what they have, and it needs a tool to have just run."*
A tool that runs and returns nothing is worse than no tool at all.


## The caution cannot be moved to the front

Owner, on the send-back costing a greeting four seconds: *"we can
take the guard off for now, just enforce that guard in the prompt
itself so that the model is well cautious on the first round."*

Tried twice, measured both times, worse both times.

| | score |
| --- | --- |
| guard only, nothing in the prompt | 71 of 71 |
| first wording, two runs | 70, then 69 |
| second wording | 68 |

The first wording asked whether *"the answer turns on anything about
them"* and closed with *"plenty of questions need no tool"*. It lost
two statements -- a flight landing, a form due Friday -- which have
no answer to turn on anything and which the closing permission waved
through.

The second named the failure instead of describing a category, and
added a line for exactly those two: *"if they have said something is
happening, or told you something to keep, that is a thing to put
somewhere."* It still lost the flight, and gained two over-reaches
it had not had before -- `memory_remember` for *"I had dosa for
breakfast"* and for *"it is raining here today"*.

**Why it cannot work, which is clear in hindsight.** The send-back
fires on an observation that cannot be made in advance: the model has
already answered, and answered without touching a tool. Said before
the fact the same words are a guess about what the turn will need,
and a guess applied to every turn moves cases in both directions.
Three runs, three different sets of damage.

**So the guard stays.** It is right a quarter of the time -- 90 of
363 -- and the other three quarters cost three to five seconds. The
fix is not to delete it or to rephrase it but to stop paying for it
serially: a streamed first round shows text before it shows a tool
call, so the moment text appears the second round can start
alongside. That is work rather than wording, and it is what this
note exists to point at.


## The send-back stops being serial

It is right about one turn in four -- 90 of 363 -- and the other
three cost three to five seconds for an answer barely different from
the one thrown away. On a plain "good morning" it doubled the turn to
seven and a half seconds; the same greeting minutes later, when it
did not fire, took four and three quarters.

It cannot be prevented. The caution it applies was moved into the
prompt twice and made tool choice worse both times, because it fires
on something that cannot be known in advance.

**But it can stop being serial.** The round the send-back would ask
for is begun at the same moment as the round in front of it, and the
loop takes it as though it had just asked.

**Waiting for the first fragment of prose was tried first, and was
useless.** The reasoning was sound -- a streamed tool call arrives
before any text, so the first text is proof of a send-back -- and the
measurement killed it:

    round 1:  after=2.4416s   first_piece_after=2.4413s

Three tenths of a millisecond between the first fragment and the end
of the round. On a short answer the gateway delivers the whole thing
in one burst, so there is no window to start anything in. The
overlap fired, logged `already_running=true`, and saved nothing: 2.44
plus 2.31 was still 4.75.

**So it starts at once**, which spends a round on the three turns in
four where the guard changes nothing. Owner, asked directly what to
optimise for: *"accuracy high, latency low, cost is not my
concern."* This is the shape of that -- the turn costs the longer of
two rounds rather than the sum.

**A round begun early is only ever right for the send-back**, which
is the whole hazard. If the round beside it goes on to call a tool,
the history changes underneath it and what it holds is the answer to
a question nobody is asking. So there are two variables rather than
one: a round is dropped at the top of every turn unless the
send-back branch has accepted it, and that branch is the only place
that may.

**The provider stopped deciding what is spoken.** It used to drop the
pieces of a round that could still be sent back, on the caller's say
so. Now it always sends them and the runner decides, because the
runner needs to see them either way -- reading the first fragment is
how it learns the round is prose at all.

**Two tests, and the second one changed shape.** One pins that the
second round starts while the first is still writing; it fails with
the overlap removed, reporting the rounds a full pause apart. The
other began life pinning that a round which calls a tool starts
nothing early -- which stopped being true the moment speculation
became unconditional. What it pins now is the invariant that
actually matters: a discarded round is discarded whole, its tool
calls never read, so a write asked for twice runs once.

**The test providers had to stop queueing.** They played a fixed
script in order, so a round that may be thrown away ate the next
entry. A real provider answers what it is asked and keeps no place
in a queue; they recognise the send-back's question now and answer
it without taking a turn.

**And an existing test caught a real mistake.** A sed meant to
restore one line restored it in two places, so every send-back
started an extra round -- three calls where there should be two.
`TestItIsSentBackOnlyOnce` had it before anything else did.


## The tool list was already as small as it should be

Owner, after the gateway turned out to cost only 0.28s: *"count
them."* Which of the tools are cold enough to defer, and what would
it save.

The answer was 28% of the weight and four tenths of a second. The
answer was wrong, and the way it was wrong is worth keeping.

**A registry built without `tool_describe` defers nothing.** It
cannot: a tool named and not described would be reachable by
nothing, so `defers` returns false and `Offered` returns everything.
Both probes built their registry the way the eval suite does, which
leaves it out -- so they measured 42 tools and 112KB, and production
sends 18 and 58KB.

| | measured | actually sent |
| --- | --- | --- |
| tools | 42 | 18 |
| bytes | 112,185 | 58,511 |
| cost of the list | 1.27s | 0.4 to 0.9s |

**Corrected, the conclusion reverses.** The thirteen cold tools were
almost all deferred already. Of the eighteen actually sent, two are
cold -- `mail_thread` at one call and `place_visits` at four -- and
both are hot by measurement rather than by judgement: deferred,
`mail_thread` had the model answer that it had *"no tool to compare
incoming mail against your replies"* with the tool named one line
above it, and `place_visits` had it work around to `event_recent`
and describe an afternoon as *"moving between the Dhanush and
Dhanush_EXT networks"*.

Eleven per cent of the weight, perhaps a tenth of a second, against
two failures already paid for once. Nothing to do.

**Both probes now ask the registry what it would actually send**, so
the next person to ask this question gets the real number. The
budget for a short turn, finally honest:

    0.28s   the gateway
    1.41s   the model to its first token
    0.7s    eighteen tool schemas
    0.2s    writing a short answer


## A warning is weighed per event, not per spell of time

Owner, on the last gate still holding one back: *"if it's urgent it
should say every time, regardless of twenty minutes since the last
and twice within ten minutes -- if an urgent event is captured, say
it."*

Right, and it could not simply be let through. The watcher re-reads
the last five minutes every thirty seconds, which is why anything is
weighed once at all: without it one song was recorded ten times as
held. Removed for warnings, one warning would be *said* ten times.

So the test changes rather than disappearing. An ordinary remark is
weighed once per label per ten minutes -- the same thing twice in an
afternoon is not twice as interesting. A warning is weighed once per
*event*: the label and the moment it happened. Two warnings a minute
apart are two events and both are said; the same one found again on
the next tick is the same moment and is not.

The record is kept the same ten minutes, which is longer than the
five an event can be found in, so nothing returns after it is
forgotten.

**That leaves it with almost nothing in its way:** not the quiet
between remarks, not the once-a-day rule, not the model's refusal.
The room is the exception, and the next section says why.


## An immediate announcement is its own mechanism

Owner: *"I think we have to split the urgent ones and don't mix with
it -- it's not urgent, name it like immediate announcement.
Currently battery full, reaching 25, 20, 15, 10, 5 and overheating
are immediate announcements. They should be told even though it's
been said a few mins back too, provided presence is true. For eg if
100 is reached, it announces, I plug out, it becomes 99 in 5 secs
and I plug back in it becomes 100 in 10 secs -- it has to announce
again. It's simple: **event arrives -> check if present ->
announce.** This is separate, and other unasked is separate."*

So the word changed and so did the shape. `notice.Told` is
`notice.Immediate`; `Candidate.Urgent` is `Candidate.Immediate`;
`conversation.Warning` is `conversation.Immediate`;
`announcement.Writer.Warned` is `Writer.Announced`. The payload
field accepts `immediate` and still accepts `urgent`, because
`urgent` is already written into Tasker tasks on a phone this
repository cannot edit, and a sender that goes quiet because a
server renamed a key is a worse outcome than a second spelling.

The two mechanisms share a gate and nothing else:

| | unasked remark | immediate announcement |
| --- | --- | --- |
| who decides | the server, from what is unusual | the sender, by marking the event |
| quiet between remarks | 20 minutes | none |
| same thing again | once a day | every time |
| weighed | once per label per 10 minutes | once per *arrival* |
| the room | must be in it | must be in it |

**The room applies to both, and that is a reversal.** It used to be
that a warning followed the person out: "where they are decides how
it is said, not whether." Three sentences into the owner's
description is *provided presence is true*, and it is right. The
phone already shows its own battery; a sentence spoken to an empty
kitchen is not an announcement, it is the announcement being used
up. `Gate.Allow` now refuses an absent or unknown room before it
looks at anything else, immediate included, and skips only the quiet
and the once-a-day count afterwards.

`conversation.Immediate` is also now audience `Asked` rather than
`Everywhere`, for the same reason.


## The narration was the wait it was covering

Owner: *"calculate the full latency... I want ultra low latency."*

Measured across 31 complete voice runs on 3 October 2026, wake word to
reply:

| stage | seconds |
| --- | --- |
| wake word to recording | 0.8 |
| speech | 0.7 - 3 |
| speech ends to text in hand | 1.8 - 3 |
| model decides on a tool | 2 - 4 |
| the tool itself | 0.01 - 0.5 |
| **asides and their gaps** | **2.5 - 12** |
| model writes the answer | 2 - 4 |

Two different systems are being measured. "Hello" and "what is the
time" are caught by Home Assistant's local intents, answered in six
milliseconds, and never reach this server at all: 3.4 to 5 seconds
end to end. Anything that does reach it costs 10 to 27.

**The largest single cost was the narration.** The median turn spent
**4.5 seconds after its tool had already returned**, and the worst
spent twelve:

    16:11:13.057  tool ran
    16:11:14.700  said what is being done
    16:11:16.212  said what is being done
    16:11:19.832  said what is being done
    16:11:21.835  held the answer back until the aside finished

The aside exists to cover a wait, and it was being said whether or not
there was one -- then the finished answer was held until it stopped
playing. Insurance against a slow turn had become a tax on every turn.

**So nothing is said until the turn has been slow.** `AsideAfter` is
1.2 seconds of hesitation before any aside is spoken; a turn that
finishes inside it says nothing beside itself, which is what fast
should sound like. And every turn now stops its narration when it
ends, not only a failed one: narration of finished work, in front of
the answer to it, is the same waste without the insult.

`AnswerGap` falls from 2 seconds to 800ms and `AsideGap` from 1.5 to
400ms. Both were chosen when the narration was unconditional and
nobody had measured it. The invariant a test holds -- the beat before
the answer is the longest in the turn, or the answer sounds like more
narration -- still holds.

**What this does not touch.** Speech-to-text is still batch: the
recording is posted after the person stops talking, so 1.8 to 3
seconds are spent on a transcript that streaming would have had ready.
And a tool-using turn is still two gateway round trips. Those are the
next two, worth about 2 and 3 seconds, and both are real work rather
than a constant.

## A rule that was never in the prompt

Owner, with a screenshot of seven calls read out one by one: *"do you
think the response is apt, like listen friendly?"*

It was not. Five clock times, five durations, seventy-one words, and
the only thing worth saying -- an hour-and-a-half call at half past
one in the morning -- arrived last, after three six-second misses from
the same person enumerated individually.

**`persona.listing` already forbade exactly that**, and had since it
was written: *what a tool returns is for you to read, not to read
out*. So the wording was tightened -- group what is alike, say the
unusual one first, one or two clock times at most -- and measured
against a new eval holding that day's seven calls.

**Nothing changed. 0 of 3 before, 0 of 3 after.** Which is what gave
it away: `listing` was declared and never passed to `prompt.Block`.
Go says nothing about a package-level variable nobody reads, so the
rule had existed only in the source, for weeks, while every spoken
answer went out without it.

Added to the block: **3 of 3**.

**And the dead code was hiding a broken rule.** The moment it reached
the prompt, `TestNoAssistantNameIsHardcoded` failed: the example in it
read *"five tasks on Jarvis Improvement"*. The one rule this file
states three times, broken in the source for weeks, invisible because
the test could only see what the prompt contained.

**A note on measuring against a model.** The honesty eval dropped to 6
of 8 on the run after this change and was 8 of 8 on the next two, with
the same code. It was variance, and it was nearly recorded as a
regression. One run is not a measurement -- the same lesson the tool
evals gave on 3 October, and it applies to every number in this file
that came from asking a model.

## Anything put in the future, and a name that is not guessed into it

Owner, 3 October 2026: *"whenever I say something like that is gonna
happen in future, like tomorrow or next week or today evening, Jarvis
should automatically ask should I add it as an event. And if it
involves something like a name, it should refer to the names or proper
nouns said before and ask if that's what I'm referring to, else it
should ask me the spelling."*

**The list of kinds is gone.** `noticing` used to name what counted --
a meeting, an appointment, a call, a visit, a trip, a deadline -- and
that list can never be complete. It is the shape of instruction the
owner has now asked twice not to have: *"keep the instruction generic,
because I'm going to add many events now and each one cannot be
instructed."* It reads *anything that is going to happen at a time*.

**What is kept is the other narrowing, and it is the one doing the
work.** A time word that merely places the sentence has arranged
nothing. The three the eval holds -- "it is raining here today", "I am
a bit tired this evening", "that took longer than I expected" -- are
the same three that `timekeeping` and `calendar_events` are measured
against, and they still pass after the widening. That is the whole
risk of this change and it is the thing that was checked.

**A name heard aloud is not a name to write down.** The existing rule
is that guessing is right when reading and asking is right when
changing, because being wrong while reading costs one correction. A
name written into the diary is not one correction: it stays there
spelt as it was misheard, and everything afterwards matches against
it.

So before offering, the spelling is looked up in what the person has
said before -- `memory_search` and `conversation_find`, which exist --
and offered back with where it came from: *"Alekhya, the one you
mentioned on Tuesday?"* With nothing close, it asks how it is spelt
rather than guessing.

**Looked up, not listed.** The obvious source was `internal/vocabulary`,
which already holds every proper noun from the past week, spelt as the
model believes it is spelt. It was offered as a tool and declined in
favour of searching what is already there: nothing new to build, and
no second place for names to live. The cost is that it is a search
over prose rather than a lookup in a list, so it is slower and will
sometimes miss.

**69 of 69 on the eval, twice.** Three cases added: dinner with a
person tomorrow, the plumber on Thursday, a form due before Friday.
What the eval cannot see is the part the owner actually asked for --
whether it *says* "shall I put that in?", and whether it asks for a
spelling -- because the harness measures which tool is reached for,
not what is said. Those two need a conversation to judge.

## What is playing, without 240 events an hour

Owner, 3 October 2026: *"now location, wifi, bluetooth, calls (meet
and cellular) are captured right. Now I want the Spotify playing --
what I play, when I play, when I stop."*

**Two senders, and they must not write the same kind.** The owner
chose both the phone and the Spotify API. If both emitted
`music.played` every track would be counted twice, so they are given
different jobs:

| kind | from | job |
| --- | --- | --- |
| `music.started`, `music.stopped` | the phone's notification | the moments, exact to the second |
| `music.played` | the Web API | the record: every device, real metadata |
| `music.session` | folded from the record | the history |

**`value` is the artist, not the track.** A title is unique and a
unique label is noise -- the rule that keeps `location.fix` off the
events tool and the one `valueOf` got wrong three times. Counting on
the artist gives "Ilaiyaraaja: 6 times, usually around 9pm"; counting
on the title gives six hundred things that happened once.

**The volume is the whole design, for the third time.** Two hours a
day is about 34 tracks, 12,000 rows a year. `music.played` will be
pruned at seven days and hidden from the events tool exactly as the
position readings are, and only the folded session is kept. Readings
are working material; sessions are the history.

**What the notification actually says, measured rather than assumed.**
A discovery profile wrote one line per notification and the owner
played, paused for half a minute, resumed and stopped:

    Created  Face to Face  Daft Punk  Liked Songs  true   <- playing
    Created  Face to Face  Daft Punk  Liked Songs  false  <- paused, and it stayed false
    Created  Face to Face  Daft Punk  Liked Songs  true   <- resumed
    Cancelled                                             <- stopped

Three things came out of it, two of which contradicted the plan.

`%antitle` is the **track**, `%antext` the **artist** and `%ansubtext`
where it came from. Cleanly split, nothing to parse.

`%anmediabutton_names()` is **not** the transport. It returns the
custom actions -- toggle shuffle, start radio -- and never Play or
Pause, so the state signal it was chosen for does not exist.

`%anpersistent` **is** that signal. It held false across the whole
pause and flipped back on resume. And `Cancelled` fires only at a real
stop: never at a track change, which the first sample proves, where
false was followed straight by the next track.

**Fourteen notifications for two tracks.** Spotify rewrites its
notification about seven times a song, so a two-hour session would
fire the profile some 240 times. The phone therefore reports edges
only, guarded by a `%MUSICON` flag: `music.started` on the first
`persistent=true` while nothing is playing, `music.stopped` on
`Cancelled`. Two events a session instead of 240, and the track by
track record is the API's job.

**A notification outlives what it was about.** Measured: the owner
stopped the music at 14:10 and `Cancelled` did not arrive until
**14:17:19**. Both events were correct and the record was a lie --
seven minutes of listening where there had been one. Android keeps the
media session alive for minutes after playback ends, so the
disappearance of the notification is the teardown and not the stop.

The owner's suggestion was to report the pause instead, which fires at
the right moment. It cannot be used on its own: `persistent` goes
false at the end of **every** track, so an evening of twenty songs
would report forty pauses that never happened. Telling a pause from a
gap needs a wait, which is the complication that idea was avoiding.

So the moment is carried rather than inferred. Spotify refreshes its
notification about seven times a song; each refresh overwrites
`%MUSICLAST` with the clock. When the teardown finally comes, the stop
event carries that number instead of being stamped with its arrival.

**And the server believes a sender that says when.** `occurred_at`
already exists on a batch, and the comment where it is read says why a
phone rarely fills it in: it wants RFC 3339 in UTC from a tool whose
clock variable is epoch seconds, and it is the field most likely to
arrive unsubstituted. `stamped` takes it from inside the payload
instead, in whichever of the two forms it was written, and removes it
so it cannot reach a prompt disagreeing with the time beside it.

Generic on purpose: nothing in the server knows this was for music.
Anything that finds out late can say so. Believed only within reason
-- older than six hours, more than two minutes ahead, before 2020, or
not a time at all, and the arrival time stands instead. Milliseconds
sent as seconds land in the year 56000 and are caught by the floor;
an unsubstituted `%MUSICLAST` is caught by the leading percent.

**The phone stopped interpreting, after three attempts proved it
could not.** It was deciding whether a notification meant paused,
resumed, skipped or stopped. It cannot. The notifications arrive in
bursts, several inside one second, and a pause is indistinguishable
from the half second between two tracks until you see what comes
next. Each rule added fixed one case and invented another:

- matching on the title missed a resume, so `music.resumed` was added
- `music.resumed` then fired at the end of every track, because a gap
  looks exactly like a pause
- `Abort New Task` -- Tasker's default -- was silently dropping whole
  notifications when two arrived together, which is why a pause
  sometimes recorded and sometimes did not
- `Run Both Together` fixed that and let two instances race, giving
  `paused`, `resumed`, `paused` inside two seconds for one skip

Eleven actions became four. The phone sends `music.state` with the
track and whether it is playing, only when one of those has changed,
and makes no claim about what it meant. Two instances racing now
produce two identical states, which deduplicate to nothing instead of
contradicting each other. The judgement moves here, where the next
event is already known and a wrong rule is a test rather than an
evening of tapping conditions into a phone.

**Holding a kind back means saying how to ask for it.** Live, twice:
*"What am I hearing right now?"* was answered with a song that had
stopped twenty minutes earlier. `music.state` had just become churn,
so a call with no kind returned no music at all -- and rather than say
so, the model used what it had itself said earlier in the same
conversation. The rule against that was already in the tool's `Avoid`
and in a memory, and it still lost to having nothing to look at.

So the kind parameter says what is held back and how to get it, and
two examples carry the shape: what is playing now is `music.state`
with a limit of 1, and a newest state that is not playing means
nothing is. Both are in the eval, which is at 71 of 71 -- the second
one, *what was I listening to this afternoon*, is there because an
example that only covers "now" teaches the model that the kind is for
one question.

**The tool and the greeting leave the churn out.** `event.Churn`
names the kinds that are working material rather than history --
`location.fix`, `music.playing`, `music.paused` -- and both the events
tool and the greeting omit them. A track a song is a hundred rows an
evening against a couple of dozen of everything else, and a greeting
built from them would be nothing but what was playing.

Music differs from the readings in one way, and the difference is the
whole of the rule. A coordinate answers no question anybody asks, so
`location.fix` is omitted even when named. *"What was I listening to
this afternoon"* is a real question and the tracks are the only answer
to it, so naming the kind gets them. It is being handed them unasked
that buries the day.

**Still to do.** The API half needs an app registered at Spotify and
an OAuth link mirroring `internal/google`; the 2026 quota change was
checked and does not restrict a one-user app. Then the poll, the
prune, the fold, and a tool for what is playing this second -- which
is a tool and not a poll, because the player endpoint rate-limits at
anything under about fifteen seconds.

## The profile is taught a method, not a list of patterns

Owner, 2 October 2026, on what the counted half is for: *"when I leave
home, at the same time the Dhanush network is left -- it will happen
many times and it forms a pattern, this means the model should conclude
that is the user's home wifi"*, and *"mostly try to keep the instruction
generic, because I'm going to add many events now and each one cannot be
instructed -- we must teach the model what we expect and the model
proves the intelligence by giving the niche insights."*

That is what the counted half was always for, and three things stopped
it working.

**Raw position readings were in it.** `location.fix` arrives every five
minutes, so four weeks of it is eight thousand rows against a hundred of
everything else: it would fill `Doings` on its own and the description
would be written from coordinates. Measured before fixing: 55% of what
the profile read. `Omit` was built for the events tool this same day and
not used here, which is the same mistake twice in one afternoon.

**Pairing was adjacency.** `whatFollows` compared each event with the
one directly after it, so a relationship was only found when nothing
else happened in between -- and the useful ones are exactly the
opposite. A network dropping as somebody leaves a place is two devices
reporting one departure, and anything landing between them hid it. It is
a window now, `Reach` events ahead or `Soon` of time, whichever ends
first. Bounded because a window is quadratic in how densely events
arrive and a chatty device should cost a constant, not a square.

**The instruction did not say what to do with any of it.** The prompt
now teaches the method and never an example: two kinds of event that
keep happening together are telling you what one of them is; a thing
that only ever appears beside another has its meaning given to it by
that other; a gap that repeats is a duration they live by. And
explicitly, that this applies to kinds it has never seen, because the
devices change and nobody will explain them -- what an unfamiliar kind
means is what it coincides with. Counts are what make it safe: two
occurrences are a coincidence, a dozen across a dozen days is a fact.

Measured after, on two days of real data, it finds
`network.joined "Dhanush", then place.entered "home": 2 times, usually
within 52 minutes`.

**What is not fixed.** The paired lines are crowded by
network-to-network churn -- the laptop reconnecting to itself -- which
fills all twelve slots before a place reaches them. Ranking is by count
alone, and a pair that crosses two kinds is more informative than one
within a kind.

## The description is corrected hourly, not rewritten daily

Two reversals, both the owner's, both worth reading next to what they
replace.

**It is given the description it wrote last time.** *A standing
description of the person* says the opposite -- rebuilt from raw
messages never from the previous profile, or it drifts into describing
somebody invented last Tuesday. The owner's reason for the reversal is
the failure the safeguard could not see: a description rebuilt from
scratch never notices that something in it has stopped being true,
because it never has the claim in front of it to drop. So the wording
does the work the absence used to -- it is given as *a draft to
correct*, everything in it is up for deletion, a claim being written is
not evidence for itself, and a routine that has stopped is named as the
most useful thing to remove.

**Hourly rather than daily**, because a description is read before every
answer and the failure that matters is it being a morning out of date.

That is only affordable because most hours do nothing. The builder
declines to rebuild anybody who has said nothing since it last described
them, so an idle night costs twenty-four cheap reads and no model call.
Without it this is twenty-four prompts a day each carrying a week of
messages and four weeks of events.

The mark is kept in the builder and not read from the stored
description's timestamp, which was the first attempt and wrong: that
timestamp comes from the memory store's clock and the messages from the
builder's, and comparing two clocks that agree only by accident gives a
rebuild that either never runs or always does. A restart forgets the
mark and costs one extra rebuild, which is the right way round.

Messages only, deliberately. Events arrive on their own all day, and a
laptop rejoining a network would keep this rebuilding through a night
nobody was awake for.

## An announcement is for the client that caused it, or for everywhere

Owner's question, 2 October 2026: announcements have a classification
beyond predefined and model-written -- *"to the asked client or all
clients"*. They were right, and the distinction was already in the code
three times, under three names, agreeing by luck.

**The rule, now written down once.** `conversation.Audience` is `Asked`
or `Everywhere`, and `announcements` is the table saying which each kind
is. `Kind.Announcement()` reads the same table, so adding a kind is one
edit -- the lesson `known()` had already learned in this same file.

- `Asked` -- the client that caused it, and nowhere else. The cause need
  not be a question: arriving is a cause, and the asker is the room that
  noticed. A conversation's new name means nothing in another room.
- `Everywhere` -- nothing asked for it, a clock fired, and the person
  could be anywhere. `reminder` is the whole of this category.

**Where it was before.** In three places and named in none:

- `runner/title.go` wrote the renaming to the conversation that asked,
  by hand, with its own `conversation.Announced` call.
- `announcement.Writer.said` wrote the other two to the most recently
  used voice client, by a different argument.
- `Announcer.Reach` carried the rule in the transport's vocabulary --
  its doc comment is the audience rule, written as "does it also go to
  the phone".

Three callers each chose `Say` or `Reach` for itself, so a fourth
announcement meant inferring the rule from the two that existed.

**`announcement.Speak` is now the only place the audience becomes a
call.** `Everywhere` is `Reach`, anything else is `Say`, and a kind that
is not an announcement at all is **refused** rather than defaulting to
the room: a reply broadcast to the house is a worse failure than one not
spoken. No caller touches `Say` or `Reach` directly any more.

**`Writer.said` can be told its conversation** instead of always looking
for one, and `Renamed` is the shape of being told. The lookup is renamed
`found` and says what it assumes: with one satellite it is exact, and
the day there is a second it becomes a guess -- two satellites are two
rooms, and the one spoken to last is a guess at the one being stood in.
When that day comes, an `Asked` announcement should be handed its client
rather than coming through there at all.

**What this does not do.** One satellite, so `hass.Announcer.Satellite`
stays a single entity and `Asked` and `Everywhere` reach the same
speaker today. The difference is real only for the phone, through
`Notify` -- which is empty in this deployment, so nothing leaves the
room at all at the moment. Typed clients are still pull-only: there is
no push path for an announcement, and the per-chat event bus carries a
chat's own answer and nothing else.

## Each setting has one home

Owner's question, 2 October 2026: *"each configuration and changes are
in their own scope right across components?"* Mostly yes. The split
holds where it matters -- the publisher measures and holds no policy,
Home Assistant holds all of it (the threshold, the dwells, fail closed,
the override), and this server knows nothing about radios at all: there
is no RSSI, no dBm and no threshold anywhere in it, and `-85` exists in
exactly one file. Four leaks, all now closed.

**The publisher's beat against Home Assistant's staleness window.**
`BEAT_EVERY = 5` in Python and a hardcoded `< 20` in Jinja: two halves
of one decision, in two languages, in two repositories, with neither
file naming the other. Raising `PRESENCE_BEAT_EVERY` past about fifteen
would have marked a perfectly healthy publisher dead -- and since
presence now fails closed, that is the house going permanently silent
over a setting that looks right. The publisher now puts its interval on
the heartbeat as an attribute and the template reads it, taking three
intervals or fifteen seconds, whichever is longer. Proven by changing
it: a ten-second beat moved the window to thirty by itself.

**Entity names that were configurable on one side only.**
`WATCH_ENTITY`, `PRESENCE_BEAT_ENTITY`, `BLUETOOTH_ADAPTER_ENTITY` and
`LAPTOP_NETWORK_ENTITY` let the publisher rename what it wrote, while
Home Assistant hardcodes the same names in Jinja and cannot be told
otherwise. Setting one renamed the sensor and left every template
reading the old one. A knob that works on one side is worse than no
knob, because it advertises itself; they are plain constants now. A
name that must match something else is a contract, not a setting.

**Windows counted in samples, named as if they were seconds.**
`TOKEN_WINDOW` and `TOKEN_GONE` are multiples of `EVERY`, so changing
the poll rate silently changes both. They say `_SAMPLES` now. The
comment on them had already claimed one minute when it meant six
seconds, for exactly this reason.

**One Home Assistant token, in one file.** It was byte-identical in
`config.ini` and in `voice-setup/presence/.env`, so rotating one left
the other working and the breakage would land later and somewhere else.
`[homeassistant] token_file` now reads `~/.config/jarvis/ha-token`,
which the presence publisher and the wake word recorder read too --
outside both repositories, because it belongs to neither. Any secret
here accepts `<key>_file`; setting both it and the inline key is
**refused** rather than resolved by precedence, since a credential in
two places with a rule about which wins is the problem being removed.
Read once at startup, so a file costs nothing per request. The trailing
newline an editor or a shell redirect leaves is trimmed: a token with
one on the end fails authentication far from here, saying only that it
was rejected.

## Readings are working material; stays are the history

Owner's observation, 2 October 2026: *"5 minute once location is stored
in database right, the database will get full -- place.stayed must be
noted."* Correct on both counts, and measured before acting: twelve
`location.fix` rows an hour, every hour, 288 a day. At 785 bytes a row
that is **80MB a year, growing for ever, read by nothing.** There was no
retention anywhere in this server, for any table.

Nothing reads `location.fix` except `Settler.Settle`, and that never
looks further back than `Looking`, 36 hours. So `event.Keep` is seven
days and `forgetDaily` drops what is older, in batches of a thousand so
one statement cannot hold a lock across a year of backlog. Seven rather
than two for the days the server is off, a phone delivering late, and
being able to look at yesterday by hand when a stay comes out wrong.
`place.stayed`, `place.entered` and `place.exited` are kept for ever:
they are small, and they are the history.

**The events tool no longer returns them, or offers them.** Asked for by
name it answers that nothing was found, and they are struck from the
kind listing so the model cannot be talked into asking again. Three
hundred coordinates is not an answer to "where was I on Tuesday", and
left in they push everything worth seeing off the end of the limit.

**Why the logic stays on the server and not in Tasker.** Doing the
settling on the phone was considered and rejected. It saves no battery
-- Tasker must poll at the same rate to decide -- and it costs the
ability to re-derive: `Near`, `Settled` and `Adrift` are all a week old
and have changed twice, and the raw readings are what lets every past
stay be recomputed when they change again. The obvious compromise,
sending only when the phone has moved, is worse than it looks: a gap
longer than `Adrift` closes a cluster, so sitting still for six hours
would deliver one reading, close on the next, and be thrown away for
being under `Settled`. The long stays would be the ones lost.

## A place can be bigger than Near

`Stays` clusters on distance from where the cluster started, and `Near`
is 150 metres -- right for a desk, wrong for a building. Walk from one
end of a shopping centre to the other and the cluster breaks: two
visits, and a departure from somewhere nobody left. Indoor readings
drift fifty to a hundred metres unaided, which can break it sitting
still.

Geometry cannot fix it. A radius wide enough for a mall swallows the
restaurant next door. So the **name** does it: `Merged` joins
consecutive stays that came back with the same name, and only across a
gap no longer than `Adrift`. Leaving and returning in the evening stays
two visits; the short clusters of a journey are dropped for being under
`Settled` before this runs, so driving home leaves a gap far wider than
`Adrift` and nothing merges.

Naming therefore happens before merging, one lookup per cluster -- a
building that breaks into three costs three. That is the price of not
guessing a radius that fits every place at once.

The merged middle is weighted by how long each half lasted. An hour at a
table and two minutes by the door is a visit to the table; an unweighted
midpoint puts it in the corridor, which matters because the next visit
is matched to this one by how far apart their middles are.

**What this does not fix.** It needs both halves to come back with the
same name, and a reverse lookup at opposite ends of a large building can
answer with two -- the building at one end, a unit inside it at the
other. Nothing here can then tell they are one place. A geofence drawn
round it always can, and geofences already outrank everything, which is
the argument for drawing one.

## Presence fails closed

Owner's decision, 2 October 2026, reversing *Present, away, and the
third answer*: **"its a kind of security one, so it should be fail
closed -- any unknown case, it fails closed."**

`sensor.owner_presence` has two states again. Every fault that used to
read as `unknown` -- a dead publisher, a suspended laptop, a blocked or
missing radio, a watch that cannot be heard -- now reads as `away`.

The earlier reasoning was not wrong, it was aimed at the wrong target.
A fault genuinely is not an absence, and if the question were *where is
the owner* then `unknown` is the honest answer. But the question this
sensor answers is *may the house speak out loud into this room*, and
there the two mistakes are not symmetrical: a missed greeting costs
nothing, and reading a reminder aloud to whoever is in the room when
the owner is not is the one that cannot be taken back.

**The liveness checks had to stay, and only change their answer.**
`sensor.watch_signal` keeps its last value when the publisher dies, so
deleting the publisher and adapter checks would have made a crashed
publisher read `present` for ever -- fail open, the exact outcome being
designed out. The adapter must now read `ready` exactly; `unknown` and
`unavailable` used to pass and no longer do.

**`input_boolean.assume_present` is what pays for it.** Fail closed
means a watch on its charger silences the house, and the owner asked
for a switch in the Home Assistant UI for that: *"i may forget my watch
or have it on charger -- if i turn it on, it means i'm available."* On
beats everything, checked first. Off is not an absence; off hands the
question back to the watch, which answers it fail closed. There is no
third position forcing `away`, because the default already is one. No
`initial:`, so a restart leaves it where it was.

**The greeting gained the dwell the sensor gave up.** With no
`unknown`, a publisher restart is a real `away` and a real return, and
the automation would greet it. Measured over the previous 24 hours,
`owner_presence` entered `unknown` eleven times. So
`welcome_when_they_come_back` now requires the absence to have lasted
**two minutes**, from `trigger.from_state.last_changed`. The smoothing
is deliberately in the automation and not in the sensor: whatever reads
the sensor to decide about speaking aloud should see the strict answer.

**This also fixed a miss nobody had noticed.** The old trigger was
`away -> present`, but leaving the house took the sensor
`present -> away -> unknown` as the watch went from far to inaudible,
so a return came back as `unknown -> present` and never fired. In the
24 hours before this change, 11 returns were greeted and **6 were
not**. With `unknown` gone, every return is `away -> present`.

Verified by rendering all nine cases: wrist, another room, watch
off/flat/charging, phone Bluetooth back on, radio soft-blocked, adapter
missing, publisher dead, laptop suspended -- eight `away`, one
`present` -- and the ninth, the switch on, `present` through all of
them.

**What this still does not fix.** Nothing distinguishes a watch on its
charger from a watch that left the house; that is what the switch is
for, and it has to be remembered. The `-85` threshold has never been
walked out of doors. And the udev rule that stops USB autosuspend
killing the adapter is written but not yet installed, which under fail
closed now means silence rather than a frozen `present`.

## A frozen sensor is worse than a missing one

Built 29 September 2026, the first of three pieces agreed for
presence.

`sensor.watch_signal` is only ever set to `unavailable` by a publisher
that is alive and has counted 150 missing samples. A publisher that
has died, or a laptop that has suspended, writes nothing at all --
and Home Assistant keeps the last reading for ever. Proven by stopping
the service: the sensor sat at **-45 dBm**, which reads as somebody
standing at the desk, and stayed there. Frozen on "here" means
reminders spoken to an empty room with no clock to expire it.

**The absence of a message is the message.** `watch.py` now counts
upward into `sensor.watch_publisher_heartbeat` every five seconds,
before the reading and regardless of it, so a blocked radio leaves the
heartbeat beating while the signal goes unavailable and the two
failures can be told apart. The value counts rather than repeats
because Home Assistant records `last_updated` only when the state
differs, and a heartbeat that repeated itself would look as stale as
one that had stopped.

`binary_sensor.watch_publisher_alive` decides from how long ago that
last moved. **Measured: publisher stopped, `off` 24 seconds later**
(a 20-second threshold plus up to 10 seconds of trigger granularity),
and back to `on` within seconds of restarting it.

No watchdog, deliberately. A timer has to guess what "stuck" looks
like; an elapsed clock cannot be wrong.

**Two things cost time here.** A trigger-based template needs the
plural `triggers:` key with `trigger:` inside -- the legacy `trigger:`
key with a modern inner body is accepted by `check_config` and then
silently creates no entity. And adding `template:` to a configuration
that did not have it needs a **restart**, not `template.reload`: the
integration was never set up, so there was nothing for the reload to
reload. Neither produced a log line.

Still to come at the time: three states instead of two, and the wifi
SSID so leaving with the laptop is visible. The first was built and
then reversed -- see *Presence fails closed*. The second was not
built: the laptop is the thing doing the measuring, so it cannot
watch itself leave.

## voice_only is not there when PulseAudio starts

The satellite crash-looped nineteen times on 29 September 2026:

    IndexError: no soundcard with id voice_only

Two things had to happen together. The container was recreated rather
than restarted, so `.env` finally took effect and
`AUDIO_INPUT_DEVICE="voice_only"` was applied for the first time --
that setting had been sitting unapplied for a day. And PulseAudio had
restarted at 14:25:32 without loading `module-echo-cancel`, so
`voice_only` did not exist.

**The module fails at startup and succeeds a minute later, with
identical arguments.** `~/.config/pulse/default.pa` is correct;
loading it by hand afterwards worked first time. The master device it
attaches to -- `alsa_input.pci-0000_00_1f.3.analog-stereo` -- is
created asynchronously by udev detection, and the `load-module` line
runs before it is there. Nothing retries.

So the satellite has a hard dependency on a device that is missing for
the first moments of every session, and it responds by exiting rather
than waiting. `docker ps` shows `restarting`, and the only way to see
why is `docker logs`.

**Loading it by hand needs the quotes kept.** `pactl load-module
module-echo-cancel ... aec_args="a=1 b=2"` from a shell loses them,
PulseAudio splits at the space, and it answers "Failed to parse module
arguments" -- which looks like a broken argument and is not. Single
quotes round the whole `aec_args="..."` are what survive. This cost a
detour: the parse error was mistaken for the startup failure, and they
have different causes.

Not fixed, only restored by hand. What it wants is the same shape as
the Bluetooth watchdog: something that checks `voice_only` exists,
loads the module if it does not, and retries until the master device
has appeared.

## A stray listener is the quietest fault there is

Twice in two days a port held by something unrelated broke a whole
subsystem, and neither said so anywhere anybody looks.

The satellite would not start for twelve and a half hours because an
orphaned process still held 6053. The embedding server restarted **411
times** on 28 September 2026 because `python3 -m http.server 9000`,
started at two in the afternoon for a different project entirely and
then orphaned, held the Prometheus port that
text-embeddings-inference binds alongside its own. It got as far as
"Warming up model" every time and exited 0.

**The only visible symptom was one grey line under an answer:**
*"Matched by wording, not meaning: the embedding server was not
answering."* Memory search silently fell back to the full-text index
for a day. Nothing was broken enough to notice: answers still came
back, just worse ones.

Two things worth keeping:

- **Move our own port, do not evict theirs.** The fix was
  `--prometheus-port 9090`, not killing somebody else's server. A
  default port is a port somebody else will take, and arguing over it
  is a fight that recurs.
- **`docker ps` says "Up 8 seconds" for a container that has never
  worked.** Restart counts are the thing to read: `docker inspect
  --format '{{.RestartCount}}'`. Uptime on a restarting container is
  time since the last death.

## A turn has thirty seconds, and the transcript used to lie about it

Four turns in a row were cut on 29 September 2026. Each was recorded as
*"[The person stopped this before it finished.]"* and the person had
not touched any of them.

**Measured: 29.98, 29.98, 29.98, 29.97 seconds from `chat started` to
`chat cancelled`.** Whatever asks the question closes the connection
at thirty seconds, the server sees the caller leave, and stops the
turn -- correctly, since nobody is left to hear the answer.

Where the thirty lives is still unknown. It is not
`assist_pipeline` (`DEFAULT_PIPELINE_TIMEOUT` is five minutes), not
the Ollama integration (`DEFAULT_TIMEOUT` is five seconds and is only
used for the model listing), not the `ollama` client (no timeout
passed), and not the satellite. The keep-alive chunks the assist
endpoint already sends do not prevent it, which says it is a limit on
total duration rather than on idle time. Worth finding, because
raising it is the one fix that needs no work here.

**What was fixed is the lie.** `conversation.Interrupted` takes the
reason now; empty still means the person stopped it, which is the
ordinary case. A caller hanging up says so instead.

Stopping with a reason used to mean failing -- `finishStopped` called
`Fail` whenever a reason was set, because the only reason that existed
was the server shutting down. Now only the shutdown fails: the machine
took away a turn that was going to work. Everything else stays
cancelled, because the turn did not finish and nothing went wrong with
it. The reason still reaches the transcript, from the active map,
which is safe to read there because the entry is deleted only after
`execute` returns.

**The thirty seconds goes on the model, not on the tools.** Across the
four cut turns the tools cost microseconds to milliseconds, bar one
1.5-second calendar read. The shape is always the same: about five to
seven seconds for the first model call, a tool that returns at once,
and then a second model call that had not answered twenty-five seconds
later.

So a turn needing two model calls is already close to the limit, and
the way out is fewer of them. "Archive everything" over 71
conversations is the worst case there is: one call per conversation,
each with a model round trip in front of it. Batch arrays --
`conversation_archive` taking a list, as `calendar_cancel` already
does -- would make it two calls instead of seventy.

## A standing description of the person

Owner's idea, 29 September 2026: *"capturing my personality and
understanding my behaviour ... like my P.A knowing all of me"*. Every
day, read the conversations and write down what they talk about, the
people they know, and how they behave.

The memories already held are facts given once -- a birthday, a pet's
name, that the coffee is filter coffee. True, scattered, and none of
them says what somebody is like to talk to. `internal/profile` is the
rest.

**Prose, and the owner chose it knowing the cost.** Offered counted
observations instead -- *"active 9am to 1am"*, *"reminders are the
commonest subject, 31 of 120"* -- which a model uses just as well and
which can be checked. They chose prose. What that buys is a
description that reads like somebody who knows them; what it costs is
that a wrong line cannot be found by looking, only by noticing.

Three things keep it honest, none of which limits what it may say:

- **Rebuilt from raw messages every time, never from the previous
  profile.** A description built from its own last version drifts:
  each day's wording becomes the next day's evidence, and after a week
  it describes somebody invented on the Tuesday.
- **Nothing is written from fewer than twenty messages.** A confident
  description drawn from a handful of sentences goes into every prompt
  afterwards and is acted on. A quiet week leaves the existing one
  standing rather than replacing it with something worse.
- **It is an ordinary memory**, in the always tier under a fixed
  subject. `memory_list` shows it, and the owner can have it changed
  or dropped like any other. That is the only correction mechanism
  prose has.

A week is read rather than a day: a day is one mood and a handful of
subjects, and what recurs cannot be told from what came up once.

**The first scheduled work in this server.** Naming, condensing and
indexing all run after a turn; nothing ran on a clock. `describeDaily`
is a timer, first firing ten minutes after startup -- not at once,
because restarting to change something unrelated should not spend a
model call -- and daily after that. It describes whoever has spoken in
the last week rather than everybody with an account, so somebody
silent keeps what they had.

`SaidSince` and `Talkers` are new, and are the first reads that cross
conversations: reading somebody over a week is a different question
from reading a conversation, and no number of per-conversation calls
answers it.

### The description is written from what they did, not only what they said

**Owner, 2 October 2026:** *"the personality should be based on my
conversation, events too -- what i do, where i go, what i ask often,
what i do often, where i go often, what activity i do sequentially
usually... the goal is jarvis has to know me completely more than
myself and should be able to predict my thinking."*

So the profile reads two things over the same week: their messages, as
it always did, and what their devices reported. `profile.Rhythm` turns
the second into counts -- how many times, on how many of the days, at
roughly what hour, and what tends to follow what -- and the prompt
asks for a fourth thing alongside the other three: their week.

**Counted, not described, and deliberately the opposite of the profile
itself.** The description is prose, which reads like somebody who
knows them and cannot be checked. The evidence under it should be the
kind of thing a person can argue with. A model handed "entered the
office 3 times on 3 of 3 days, usually around 10am" writes about a
habit; handed a list of raw rows it invents one.

**Repeats close together count once, and the collapsing is generic.** A
laptop that rejoined the same network twenty-five times in a day
otherwise reads as the most significant thing in the week. The rule is
two hours between occurrences of the same thing, with no list of which
kinds are the boring ones: the server has no business holding that
list, and it would be wrong the moment a new device started reporting.

**A routine is only stated where it recurs.** Two things next to each
other once are a coincidence; the sequence lines need at least two
occurrences and a gap under ninety minutes, and an hour of day is
given only when the occurrences actually cluster. "Usually around 2pm"
for something that happens at any hour is the kind of detail that gets
written into a description and then believed.

**Prediction is asked for as expectation, never as fact.** The prompt
says to write "usually" and "tends to" where the week supports it, and
to say nothing where it does not -- a routine invented from two
coincidences is acted on exactly as a real one is.

**The places are theirs, and that is why they are allowed.** The
profile has never been permitted to say where somebody lives or comes
from, after a rebuild wrote "almost certainly Indian, likely based in
or around Hyderabad" from a girlfriend's address. A geofence the owner
named themselves is a different thing: it is something they said, not
something inferred. So the places may be written about by the names
they gave them, and the ban on residence, origin, age and health
stands unchanged.

**Two windows, because the two questions need different amounts of
time.** The words are a week: what somebody is talking about is this
week's business, and a month of it describes a person who has moved
on. The counted half is four weeks, because what somebody does every
Tuesday cannot be seen in seven days -- one Tuesday is an anecdote,
and the difference between a routine and a coincidence is how many
times it came round.

**Where they go is an arrival paired with the departure after it.**
Counting arrivals answers "where do they go" and not "when are they
there", and the second is the half that lets anything be ready before
it is asked for. `place.entered` and `place.exited` are paired by
suffix -- the server's own naming for its own events -- and anything
else is counted but not paired, which is the safe direction. A visit
under five minutes is passing the door and its length says nothing; one
running over sixteen hours is a departure nobody reported, not a night
at the office.

**What they asked for is counted from the tool calls, not the words.**
Their own words vary and a model counting them finds nothing; the tools
run in answer are a fixed vocabulary, so forty diary readings look like
forty diary readings however each question was phrased.
`tool_describe` is left out -- that is the deferring mechanism costing a
round, not something anybody wanted. The prompt says to write what they
keep wanting in their own terms and never by the name of a tool, because
a description saying somebody "frequently triggers calendar_events" is
written about a system.

**What this is worth today, honestly.** The asked half works from the
first day: 707 tool calls over four weeks, led by reminders and the
diary. The device half does not -- two days of events exist, most of
them a laptop's wifi flapping -- and the first real run surfaced
leftover `test.spool` rows and one `network.left "%ssid2"` from an
unsubstituted Tasker variable. The mechanism is in place; it has
almost nothing to say about where they go until there are a few weeks
behind it.

## Ten minutes somewhere is somewhere they went

**Owner, 2 October 2026:** *"if im there in a location for more than 10
mins it gets recorded."* Geofences only record the places somebody
thought to draw a circle around; everywhere else -- a mall, a friend's
house, the place they eat every Thursday -- produced nothing at all.

So the phone reports `location.fix` every five minutes and the server
works out `place.stayed` from the run of them. It is written back as an
ordinary event, so it shows on the events screen and counts towards the
description of them like anything else they did.

**Five-minute readings for a ten-minute rule.** At a ten-minute
interval a ten-minute stop falls between two readings and leaves
nothing behind. Five guarantees at least two readings inside it.

**A hundred and fifty metres, measured rather than chosen.** Three
readings taken while sitting perfectly still were spread over about
twenty metres: that is network location's accuracy and it is the floor
on what any radius has to absorb. A hundred and fifty swallows it
without putting a café and the shop next door in one place. Distance is
flat-earth arithmetic, which over that range is wrong by centimetres
against a reading that is wrong by twenty metres.

**A cluster is measured from its first reading, not its middle.**
Against the middle, a slow walk drags the centre along with it and a
mile of pavement reads as one place.

**Only stays that have ended are written.** A run of readings still
arriving from the same place is somebody who is still there, and its
length is not known yet. And a stay ends at its last reading, never at
the one that ended it: that one was taken somewhere else. Thirty
minutes of silence closes a stay too, so a phone that went flat at the
office does not record a night there.

**A place names itself the first time somebody stays there**, and every
later stay within three hundred metres borrows that name, carrying the
original's coordinates in `at` so the next one can be measured against
it. Without this, two evenings at the same restaurant are two different
places -- the readings are twenty metres apart and the coordinates never
repeat -- and nothing counting where somebody goes would ever count to
two. Geoapify can replace that name with a real one later without
changing any of this.

**A place they drew themselves outranks everything else.** Owner, 2
October 2026: *"my geo fences are the first priority, if its not in geo
fence then geoapify will name it."* So a stay whose middle falls inside
a `place.entered`/`place.exited` stretch takes that stretch's name, and
only a stay outside every one of them falls through to borrowing a
neighbour's label, and after that to its own coordinates.

Judged on the middle of the stay rather than either end, since the
crossing and the first reading inside it are minutes apart. A crossing
never closed runs to now -- somebody has not stopped being at the
office because they have not left yet -- and a departure with no
arrival is ignored, or an unbounded stretch swallows every stay before
it. Where two overlap the shorter wins: a court drawn inside home is
the more particular answer, and it is what somebody would say.

A stay named by a geofence still carries its coordinates in `at`, so a
later stay at the same place borrows the name even when the crossing
itself was missed. The geofences are unreliable in exactly that way,
and this costs nothing.

**Geoapify names what is left.** Owner, 2 October 2026: *"if its not
in geo fence, then geoapify will name it."* So the order is: a
geofence they drew, then the name an earlier stay at the same spot was
given, then Geoapify, then the coordinates. The second step is there
because it keeps two evenings at one restaurant together and costs no
lookup; the last is what everything falls back to.

Called once per stay that has ended, not once per reading -- a handful
of requests a day against a free allowance in the thousands.
`internal/place` takes six fields out of about forty and picks the most
particular: a building's name, else a street, else a neighbourhood,
suburb, district, city. An answer that grows a field cannot change what
it does, and the rest of somebody's address is never carried around by
accident.

**A business first, the street second.** Owner, 2 October 2026: *"for
knowing the location from coords we can use reverse geocoding for
street name, place api for place name."* The Places API answers with
what somebody would say they went to -- `Phoenix Marketcity Chennai`,
55 metres away -- and reverse geocoding answers with the road they got
there on. One credit when a business is found, two when the street has
to be asked for instead.

Only the nearest named place within a hundred and fifty metres counts,
which is the same distance that makes two readings one place, so the
two agree. Their own neighbourhood has nothing within a kilometre but
a petrol station and a workshop, so most stays near home will get a
street and that is all there is.

**Three Geoapify categories silently poison the whole query.**
`accommodation`, `sport` and `tourism`: including any one of them
returns an empty answer rather than an error, and takes every other
category in the call with it. `commercial` alone finds twenty places
at a shopping mall; `commercial,accommodation` finds none. Found by
pairing each category against one known to work, after a list that
looked perfectly reasonable named nothing anywhere.

Worth remembering as a shape of fault rather than a fact about
categories: a filter that is wrong in this way does not fail. It
returns nothing, for ever, and the feature goes on looking as though
it is running.

**Everything about it fails quietly.** No key configured and the namer
is nil rather than something that always fails, so nothing logs a
warning every time somebody goes somewhere. No answer, no network, or
an empty result -- the sea and a motorway both produce one -- and the
stay keeps its coordinates, which is what it had before any of this
existed. A refusal is reported, so a key that has run out says so
rather than every place quietly losing its name.

**Why the geofences winning matters more than it sounds.** Asked about
the owner's home coordinates, Geoapify answers with their street. That
is their address, and the description of them is forbidden from saying
where they live. Their own geofence names that place "home" before
Geoapify is ever asked, which is the order they chose for other reasons
and which happens to be the one that keeps an address out of the daily
profile.

**Worked out again from the readings every time, with no cursor kept.**
The same readings produce the same stays and the same keys, so a stay
already written is recognised as a resend. The work is wasted rather
than wrong, which is the cheaper mistake: a cursor that is ever wrong
loses a day of somebody's life quietly.

**Six Tasker faults sat in front of this**, and the last one is worth
recording because it took an hour. `Send Location` had `If %far ~ 1` on
its Perform Task, with `%far` computed in a JavaScriptlet from
`local('location')` -- a variable Get Location v2 does not set. So the
distance came out `NaN`, `NaN > 100` is false, and `far` was 0 for
ever. It sent exactly once, on the first run, when the stored previous
location was still empty and the comparison was skipped entirely. One
reading in sixteen hours, and nothing in the log said why: Tasker's run
log marked the action `IfFail`, which means the condition was not met,
not that anything failed.

## Asking where somebody went, without reading out wifi names

**2 October 2026.** Asked *"do you know where I went today?"*, the
assistant called `event_recent`, was handed forty-two entries of which
forty were a laptop's wifi reconnecting, and answered: *"You left home
at ten twenty-two this morning and came back at ten fifty, sir. Since
then you have been at home, moving between the Dhanush and Dhanush_EXT
networks."*

The first half was right and the second half was a network adapter.
The owner: *"moving between networks is not relevant to the
question."*

It was not the model's fault. There was no tool that answered the
question asked, so it assembled an answer from what it could reach.
`place_visits` is that question asked properly: one list of places and
times, with nothing in it that is not somewhere they were.

**It merges the two records.** Geofence crossings are the places they
drew; stays are everywhere else. A stay inside a geofence carries that
geofence's name and the same hours, so the crossing wins where they
overlap -- it is the boundary actually being crossed, where a stay is
an inference from scattered readings.

**Hot from the first day, with no measurement behind it.** The
measured rule is that a tool is described on every request only if the
numbers say so. This one is there for the reason `mail_thread` is: a
tool named but not described is a tool the model works around, and
working around this one is the bug it exists to fix.

### Two faults the real data found that the tests had not

**A departure nobody reported left somebody at the office for ever.**
The first real run said *"office, from 19:14 and still there"* at
eleven the next morning, with two arrivals home in between. Android
drops geofence departures. Arriving somewhere else now ends wherever
they were: they cannot be in both, and the arrival that was reported
is better evidence than the departure that was not. An arrival still
open after sixteen hours is said to have no departure recorded, rather
than being given an invented leaving time.

**Somewhere left and returned to was listed twice.** `Fences` kept the
order places were opened in and emitted every still-open one by walking
that list, so a name opened twice produced two open stretches from the
one arrival. Closing writes to the map now, and what is left in the map
at the end is what is open -- at most one of them, since arriving
anywhere closes the rest.

Both were invisible to the tests and obvious in one run against a real
day. Worth remembering when the next piece of this is built.

## Guess at a misheard name when reading, ask when changing

**Owner, 2 October 2026:** *"by voice its definitely not accurate, so
i want jarvis to take relevant context without asking me a question...
in that case it can assume the default context. remember this is only
for reading info; while creating, updating, deleting, it should ask
like how its asking currently."*

The occasion: asked *"do you know when I moved between Das and
Dasabhift?"*, with `Dhanush` and `Dhanush_EXT` on record and plainly
what was meant, the assistant answered *"the names you said don't
match the networks I have on record for you... did you mean when you
moved between those two?"* It had recognised the likeness and stopped
anyway, over a question about the past.

**The asymmetry is the whole rule.** A wrong guess on a question costs
one correction, said out loud in the same breath. A wrong guess on a
change costs them their data. So a read takes the nearest match and
says which it took; a write asks, exactly as it already does.

**Generated from the `Writes` flag, not written into each
description.** `Tool.Description` appends the guessing instruction to
every tool that does not write. Written per tool it would be left off
the next one somebody adds and would drift between two that need the
same words; derived from the flag, a new reading tool gets it by
existing and a new writing tool cannot accidentally get it.

This refines rather than replaces the earlier rule that a name is
matched by likeness and never refused on an exact-match miss. That
said what to match; this says when matching is enough on its own.

### A maps domain, later

Noted by the owner on 2 October 2026 and not built: the Geoapify key
buys more than naming. Worth tools of their own when the time comes.

**Routing**, one credit a leg, gives travel time from where they
actually are to where they have to be. That is what turns "the film is
at ten past six" into "you need to leave in twenty minutes", and
nothing else on offer changes what can be said about their day as
much.

**Map Matching**, one credit per hundred readings, snaps the day's
positions onto real roads -- three credits for a day of five-minute
fixes. It would clean up a track for a map, and tells you cheaply
whether somebody was driving or sitting still.

**Map tiles** at a quarter credit each, roughly three and a half for
one map view, for the screen they want that draws the day's movement.
The free plan requires a "Powered by Geoapify" follow-link on any page
showing it.

Not worth it: geocoding, autocomplete, IP geolocation, postcodes,
route matrices, route planning, isolines, geometry, elevation. None of
them answers a question anybody would ask the assistant.

## The names it expects to hear

Owner's ask, 30 September 2026: *"the vocabulary should be shared by
deepgram and fast whisperer ... create a timer to make a server call
to get the proper nouns list from the conversation and update the
proper nouns in the vocabulary list ... I wanna see the vocabulary
list in home assistant"*.

Speech recognition is primed with a list of words to expect, and a
word left off it is not merely unhelped: it comes back as whatever
common words sound like it. `Alekhya` arrived as "a leg near" until
somebody typed it in. So the list only ever held the mistakes the
owner had already been annoyed by.

`internal/vocabulary` reads the same week of messages the description
reads and asks the model for the proper nouns in them -- people, pets,
places, brands. Those are the words a general model has no reason to
know and the ones it gets wrong; ordinary words are already in the
decoder and priming them costs budget a name needs. It is served at
`GET /v1/vocabulary`.

**Not a memory, though it is built the same way from the same
messages.** A memory is for the model to read; this is for
faster-whisper and Deepgram, which never see a prompt. Putting it in
the memories table would mean fifty names either sitting in every
system prompt or surfacing as search results whenever somebody is
asked about. It has its own table for that reason, and not the
settings table either, whose value column is `VARCHAR(255)`.

**Replaced wholesale each night, never added to.** A list that only
grows fills with names from one conversation in March and crowds out
the people somebody actually talks about. The budget is small:
Deepgram refuses more than a hundred keyterms in total.

**`Clean` exists because an answer is not a contract.** What comes
back carries bullets, numbering, a leading sentence and explanations
in brackets, and all of it would be handed to a speech engine as words
somebody says. Lines that are not names are dropped: too long, too
many words, starting with punctuation, or holding sentence
punctuation.

**The server stores and serves; it does not reach the engines.** Those
live on the machine with the microphones, along with the hand-kept
half of the list, in `~/voice-setup/vocabulary`. A daily systemd timer
there fetches this and applies it. Two halves because neither machine
can do the other's work.

One thing learned by breaking it: **an empty answer must never replace
names already in place.** The server answers with nothing both when a
week genuinely held no names and when its nightly job has not run yet,
and from the fetching end the two are identical. The first run dropped
three good names that way. A stale list decays slowly and shows its
date on the dashboard; an emptied one fails at once and silently.

## Look it up every time

**The owner's standing rule, 27 September 2026: every statement about
how things are comes from a tool call in the current turn. Never from
memory, never from earlier in the conversation.** The reason is that
the data changes every moment -- by them, by another device, by the
clock -- so a remembered answer is only ever a guess about the past.
This applies to every tool, not the calendar alone.

It is in `persona.go` under honesty, worded as the rule above the
others, with the cost stated both ways: looking again when nothing has
changed costs a second, not looking when something has costs the truth.
Three specific traps are named because each has happened: reasoning
that a past date must be empty, reusing something read out one turn
ago, and padding a tool's answer with a remembered item that the tool
did not return.

**Withdrawing old tool results did not fix it either, and the numbers
are recorded here so it is not tried again on a hunch.** `ForModel`
takes the contents out of tool results from earlier turns, leaving the
tool's name and a note to call it again. Four questions in a fresh
conversation, three runs each:

| | tool called |
|---|---|
| without withdrawal | 1, 2, 2 of 4 |
| with withdrawal | 1, 1, 1 of 4 |
| with withdrawal and the answers marked stale as well | 1, 1, 1 of 4 |

No improvement, possibly a little worse. The reason is plain in
hindsight: removing what the tool said does not remove the assistant's
own answer repeating it, and that is the copy being reused. Marking
those answers as stale was tried and dropped, since it bought nothing
and cost tokens on every turn.

**It is kept anyway, for a different reason.** A listing's identifiers
are as perishable as the rest of it, and withdrawing them stops an
identifier from an old listing being used. That is not hypothetical: on
27 September 2026 an event was cancelled by an identifier from a stale
listing, the cancel failed, and the model created a duplicate rather
than admitting it did not know. Withdrawal forces a fresh listing
first. That benefit is real and separate from the one it was built for,
which it did not deliver.

**So the read half of the golden rule is still unsolved.** Writes are
guarded in code and cannot be skipped. Reads are guarded by words, and
the words do not reliably hold. Anything tried next should be measured
against the table above before it is believed.

**Prose has not been enough, and the cause is structural.** Measured
the same afternoon: in a conversation with history, four questions in a
row about the diary and the reminders produced no tool calls at all --
every answer came from context. The same question in a conversation
with no history called the tool immediately, every time.

So the model is not ignoring an instruction it failed to read. It is
reusing material that is in front of it, and the material is stale
tool results and its own earlier answers, which this server puts there.
The fix worth trying is to stop serving them: redact the content of
tool results from earlier turns when building the window, leaving a
marker that a listing was returned and may be out of date. Removing
what is being reused is the only lever that has moved this.

That is partial by itself. The model's own prose answers also carry the
data and cannot be redacted without wrecking the transcript.

**And it is not only tool results. Recall is the other mouth.** Both
recall blocks search by meaning, so a question of the shape *what
conversations are there* pulls back every previous time that question
was asked -- each one stored as `They said: ... You answered: ...`,
each answer right when it was given. On 27 September 2026 the owner
caught this: three such exchanges came back scoring 0.61, 0.61 and
0.60, no tool was called, and the model wrote a fourth answer in the
same shape. The transcript block already carried three paragraphs
saying exactly why that is wrong. It lost anyway.

So both blocks now **open** with four sentences fixing the order --
everything here is information, not an answer; if a tool can tell you,
the tool decides; read this, then call the tool anyway; where they
disagree the tool is right. Not because the reasons were missing, but
because they were at the bottom of a long block and the rule needed to
be at the top and short enough to survive the rest of the prompt. The
owner's words for it: *"the recalled are just info, tool call is the
first priority -- if there is a tool, use it"*, and then, sharpening it
into a decision with two branches: *"the model has to answer whether
the available tools list can answer the current query -- if yes use
tool, else say what you got"*.

Both branches are stated because one alone leaves the other unsaid. A
model told only to prefer tools, asked something no tool covers, still
has to answer from something, and will offer a note as a present fact
rather than as a thing that was said. So the second branch -- answer
from what is here and say where it came from -- is not a softening of
the rule, it is the remainder of it.

**The timeline hid the fix, which cost more than the fix was worth.**
A prefetched listing goes into the prompt, not onto the wire as a tool
result, so it left no tool call for "How this answer was made" to
draw. The screen showed *Recalled* then *Answered* and omitted the one
step the answer was actually built from. On 27 September 2026 the
owner read that screen three times running and concluded, correctly
from what it showed, that no tool had been called -- while the log and
the database both said it had, down to a conversation created
seconds earlier that no memory could have held.

An assistant that is right and cannot be seen to be right is not
trusted, and the owner is strict here precisely because he trusts it.
So `chat.Recalled` gained `Tools`, prefetch records each listing it
read, and the timeline emits a `read_first` step per tool, ahead of
the recall step. Whatever is enforced in code has to be visible in the
timeline, or the next silent mechanism buys the same argument again.

**Memory had no listing tool, so there was no call to make.** Asked
what it remembered, the model answered from recall -- not because it
skipped a tool but because none of the four memory tools could answer
the question. `memory_search` requires an `about` and returns the
nearest few; nothing returned all of them. A rule saying *call the
tool if one can answer* is satisfied by doing nothing when none can,
which is the correct behaviour and the wrong outcome.

So `memory_list` exists: every tier, every memory, `Prefetch: true`.

**Memory is the only thing prefetched, and that is a decision, not an
oversight.** All four domains were, briefly. Prefetching the diary cost
between 2.4 and 3.2 seconds of Google call on every single turn,
including turns that had nothing to do with the diary, and the three
listings between them meant the model had no reason to call a tool for
any ordinary read -- which also left nothing to narrate while it
worked. Memory is local, wanted on every turn whatever is asked, and
answers in under a millisecond. The owner's instruction on 27
September 2026: *"only memory has to be prefetched"*.

**What that gives back is the risk the prefetch was removing.** For
reminders, conversations and the diary the model must now choose to
call the tool, and choosing is the thing it was measured getting wrong.
What stands in its place is prompt text: the two-branch rule at the
head of both recall blocks. That is the weaker half and is known to be
-- see below. If those domains start being answered from recall again,
this is the paragraph that explains why.

Its renderer opens with **"There are N memories in total"**, because a
listing that only prints rows invites the model to report the number
of rows as the number of things. That is not hypothetical: the same
day, `conversation_list` returned its default ten rows out of sixty-two
conversations, with no total anywhere, and the answer was "you have
ten conversations, sir" -- a tool call that happened, a listing that
was current, and a false answer regardless. Every capped listing owes
its total.

**Prompt text is the weaker half of this fix and is known to be.**
The load-bearing half is `Prefetch`, which removes the decision
instead of arguing with it. Three tools have it -- `reminder_list`,
`conversation_list`, `calendar_list` -- so those domains are read
before the question is even put. **Memory has no prefetch**, which is
why *what have you saved in memory* is still answered from recall
alone. That is the next gap, not a mystery.

## Tasks, and the line between three things that sound alike

Built 30 September 2026, on a scope granted three days earlier and
never used.

**What a task is not.** An event occupies time and happens whether or
not anybody acts: four o'clock is gone either way. A reminder is a
sentence with a moment attached, said aloud and then spent. A task is
neither -- it takes up no time, there is no instant at which it fails,
and it waits until somebody ticks it off.

**The API cannot hold a time, and this decides everything.** The
reference on the `due` field: *"Only date information is recorded; the
time portion of the timestamp is discarded when setting this field. It
isn't possible to read or write the time that a task is scheduled for
using the API."* Confirmed against the owner's own data, which comes
back as midnight every time. The web UI does let a time be set, so
Google keeps it somewhere the public API cannot see.

So tasks cannot absorb reminders -- not as a design preference, but
because the field does not exist. `task_add` refuses a time of day
rather than accepting it and dropping the hour: a write that silently
loses half of what was asked for is worse than a refusal, because
nothing reports it and the person believes it was kept.

**Four tools out of fourteen methods.** `task_lists`, `task_list`,
`task_add`, `task_done`. Left out: deleting a list, because it takes
every task on it with no confirmation and no undo and a mis-heard word
should not reach that; `clear`, which hides completed tasks in bulk
and reports nothing; `move`, for reordering and subtasks, which nobody
asks for out loud. Deleting a single task is also left out for now --
ticking it off is what somebody means nine times in ten, it keeps the
record, and it can be undone.

**What the API does not give.** No search, so finding one means
listing and matching, which `heard` already does. No batch, so the
arrays are in the tool and the loop is underneath. No change feed at
all, unlike Calendar -- a task ticked off on a phone is invisible here
until the next look. And `maxResults` defaults to twenty without
saying so, on a list that can hold twenty thousand: the same trap that
had `conversation_list` report seventy-one as ten, so it is set
explicitly and the total is always said.

**How the model is meant to choose.** Follow the words first: "remind
me" is a reminder, "put it in the diary" is an event, "add it to my
list" is a task. Only when nothing was said does it fall to what is
attached -- a duration means an event, a clock time means a reminder,
neither means a task. That is a judgement about meaning, so it lives
in the tool descriptions rather than in any matcher.

What makes it safe is not the rule but the answer: every write names
where it went. "On your list", "in the diary", "I will say that at
ten twenty" are three sentences nobody can confuse, so a wrong choice
is caught in the same breath instead of found next week.

## The calendar

Two scopes to begin with, both non-sensitive, and the division between
them is the whole shape of the package. `calendar.app.created` gives a
calendar of its own and complete control of that one;
`calendar.freebusy` says when the person is busy across every calendar,
without saying what they are busy with. A third, `calendar.readonly`,
was added later and is sensitive; see below.

So it can write, but only where it cannot disturb anything, and it can
see that four o'clock is taken without seeing whose meeting it is.
Confirmed non-sensitive in the console on 27 September 2026, which is
why they were chosen first: no verification, no published privacy
policy, and no seven-day refresh token. `calendar.events` and
`calendar.readonly` are sensitive and bring all three back at once.

**The calendar's identifier is remembered, not discovered.** The
permission that makes this safe is also what stops it looking:
`CalendarList.list` returns 403 under `app.created`. There is no way to
ask Google which calendar is mine. The first version listed them and
failed on the first real call; `google_accounts.calendar_id` holds it
now, written down before the calendar is used, because made-and-not-
recorded means a second one next time and a person with five calendars
called Jarvis.

A calendar deleted by hand shows up as a 404 when next used, and
another is made in its place.

**Granting permission again used to clear it, and that made a second
calendar.** `Put` built a row with `calendar_id` left at its zero value
and handed it to GORM's `Save`, which writes every column of a struct
including the ones nobody set. So each completed consent screen set
`calendar_id` to NULL, the next calendar write found none recorded,
and made another. It is not a once-off: in testing mode with a
sensitive scope the refresh token dies every seven days, so this was a
fresh orphan calendar every week, each quietly taking the writes meant
for the last.

Found on 28 September 2026 with two calendars named Jarvis on the
account -- the older one holding a birthday the assistant had saved,
unreachable ever since. `Put` is now an upsert over named columns, and
`TestPutKeepsTheCalendar` fails against the old code.

**There is a `calendar_update`, and there had better be.** Without one
the only way to rename an event is to cancel it and add another, which
loses the identifier and leaves two events behind when the cancel
fails. That is not hypothetical: on 27 September 2026 the model was
asked to rename an event, correctly said it could not, offered to
remove and recreate, and the remove failed with "there is no such
event to cancel" -- leaving "Meeting for haircut" and "My Favorite
Date" side by side on the same day.

It patches rather than replaces, so anything not named keeps what it
had, and moving the start alone keeps the length it had, which is what
somebody means by "move it to four".

The before is copied, not held by pointer. A Diary handing back a
pointer into its own storage would have the write mutate what the diff
compares against, and the change would silently report that nothing
moved. The test double did exactly that and caught it.

**`calendar_list` never reports an empty day.** It sees only what the
assistant put there, so the tool says that nothing was written down
here and points at `calendar_free` for whether the person is actually
busy. **`calendar_free` never says what they are doing**, because that
is not knowable with these scopes and a model left to guess will
furnish a meeting.

There is no tool for changing the person's real events. There is no
permission for one -- `calendar.readonly` reads and nothing more -- and
offering it would have the model promising what it cannot do.

**Reading them is a different matter, and for a few hours there was a
scope with nothing behind it.** `calendar.readonly` was granted on 27
September 2026 and wired to nothing. Asked that night to read the
events on `rjdhanush22@gmail.com`, the assistant answered that it
could not -- true of its tools, false of its permission, and exactly
what the honesty rule asks for when no tool covers a question. The
fault was ours for granting a scope and leaving it unused.

`calendar_events` now takes an optional `calendar`. Named, it reads
that one of the person's own; left out, it reads the assistant's, as
before. The two are worded apart on purpose: the person's own comes
back stating the count and saying plainly that it cannot be changed,
and **without identifiers**, because an identifier is only useful for
changing a thing and offering one for a read-only calendar invites a
call that must fail. A name that matches nothing brings back the names
that do exist, rather than being answered as an empty day.

**Nothing refuses on a name that did not match.** Spoken input is
wrong more often than it is right, so a name matching nothing means
the microphone failed, not that the thing is missing. The pattern is
one mechanism across every domain: the tool fetches the candidates
itself, hands them back, and says to name the likeliest and ask a
question answerable with yes or no. `internal/heard` does the
matching -- exact, then one name inside another, then edit distance at
`Alike` 0.6, refusing to choose when two are within `Clearer` 0.15 of
each other. `tool.WhichOne`, `tool.WhichOfThese` and `tool.Confirm`
give every domain the same words for it.

**What the conversation already holds settles most of it.** The
calendars had been read out by name one turn earlier when
`RJDanajtvali.jml.com` arrived; the answer was sitting in the window.
A name spelt out, listed by a tool or written correctly a moment ago
is what a garbled one almost certainly is, and the model is told to
look there before deciding a name is unknown.

**How far the assumption may carry depends on what is done with it.**
The owner's rule on 28 September 2026: *"these kinds of assumptions
are safe for get calls, but for create, update, delete, confirmation
should be asked"*. So a read proceeds on a sound-alike match and names
what it read, letting the person correct it in the next breath; a
create, change, rename or deletion has to be agreed first, as a
yes-or-no. A wrong reading costs a sentence and a wrong deletion
cannot be undone.

The model cannot tell an exact match from a near one by looking at the
answer, so the tools say which it was: `tool.BySound` is appended
whenever what was found is not what was said, and it carries the rule
with it.

Fetching is the tool's job, not the model's. Told to go and look, the
model has been seen to skip the looking and answer that the thing does
not exist -- which about stored state is a claim it cannot support.
The spoken rules say never to end on a question; this is now the
second stated exception, because yes and no survive transcription
where a name does not, and the question mark is what holds the
microphone open for the answer.

**A calendar named aloud is matched by likeness, never literally.**
Speech-to-text writes an address down as it sounded:
`rjdanesh22rjmail.com` was what came back for `rjdhanush22@gmail.com`,
and `javas` for Jarvis. Matching exactly refuses both, and refusing
reads to the owner as the assistant being stupid rather than as the
microphone being imperfect. The order is exact identifier, exact name,
the words people actually use for their own calendar -- main, primary,
mine -- then containment, then edit distance over the longer name at
`Alike` = 0.6.

**Two calendars equally near is a question, not a coin toss.** When
the best match beats the next by less than `Clearer` = 0.15, nothing
is chosen: reading the wrong calendar answers something nobody asked.

**Nothing found is also a question.** The tool comes back with the
calendars that do exist and an instruction to name the likeliest and
ask, explicitly *not* to say there is no such calendar. The owner's
words on 28 September 2026: *"it could first fetch calendars, try to
match something that closely resembles mine, not even exactly; if not
sure, it can ask me, I can say yes, and then it can go further"*. The
prompt half matters as much as the matching: the model had refused
without calling anything at all, so `calendar_events` now says to pass
a name through however wrong it sounds, and that claiming a calendar
does not exist is a claim about what the person has, which needs a
tool to have just run.

**An example beats a schema, and examples written before an argument
existed teach the model to leave it out.** The `saying` argument was
filled on the first call of a turn and on none after it, and the cause
was thirty-seven worked examples that did not have it -- including
four the same evening written to demonstrate the calendar argument.
Its description saying "which the examples above leave out but you
must always include" was noticing the problem and writing round it.
Every example carries a saying now, and
`TestEveryExampleMatchesItsSchema` checks examples against the
narrated schema -- the one the model is actually shown -- and fails an
example without one.

**A stay of four days was four events, because nothing could say
otherwise.** `calendar_add` took a start, a length in minutes and an
all-day flag, so asked for the 26th to the 29th the model wrote four
one-day entries and then could not change them as one thing. That was
the schema's fault and not the model's. Both `calendar_add` and
`calendar_update` take `ends` now, and the read-back covers the whole
span: reading only the first day makes a multi-day event look missing
from every day but one.

**The thinking sound and the aside cannot both be had.** What is said
mid-turn plays through the satellite's music player, because that is
the one player the voice pipeline never touches -- but the thinking
sound ducks exactly that player to half volume at INTENT_START, and
unducks only at `_tts_finished`, after the answer. So for the whole
window the aside occupies, it is ducked: the aside came out quiet
while the answer was at full volume. The safety property is the cause.

`switch.laptop_lva_thinking_sound` is off from 28 September 2026. The
aside is the better version of the same signal -- it says what is
happening rather than only that something is -- and turning the tick
off costs nothing and needs no patch. Turning it back on brings the
quiet aside back with it.

**A turn that fails stops narrating.** Owner, 1 October 2026, of a
failed turn: "when it failed it didn't acknowledge even". It had done
worse than not acknowledge. The chat gave up at 09:38:02 and the
apology was ready at 09:38:03, but three asides were still queued
behind the minimum gap, so what the person actually heard was "Let me
check the proper way to create that list, sir", then "checking how to
create a list", then "creating that list" -- and only at 09:38:10,
seven seconds after it had already given up, "I am having some
difficulty reaching the server, sir". It promised the work three
times after failing to do it.

The asides are detached from the turn's context on purpose, so a
sentence is not cut off mid-word when the answer lands. That is right
for a turn that succeeds and wrong for one that does not.
`speech.stop()` sets a flag the queued goroutines check, and
`finishWith` calls it when the chat ended failed or cancelled. Checked
twice -- before queueing and again after the gap -- because most of an
aside's life is spent waiting behind the one in front, which is
exactly when a turn goes wrong.

The flag is an `atomic.Bool` rather than state under the mutex. The
mutex is held for as long as a sentence plays, so a caller taking it
to abandon the queue would first wait out the very sentence it is
trying to prevent the next one of.

A sentence already playing is left to finish: cutting speech off
mid-word sounds like a fault of its own, and it is one sentence rather
than the queue behind it.

`finishStopped` was passing `nil` for the turn's speech, so timeouts
and cancellations went on narrating; it is given the real one now.
That is the larger half. A cancelled turn is usually one the person
interrupted by speaking again, and its narration continuing over the
answer to their new question is the clearest way to sound like a
machine talking to itself.

**What is owed has to be something a person would actually say.**
`Added` owed the whole rendering of the event -- "Gunalan's Birthday,
all day on Friday 5 February" -- and no model writes that sentence, so
the check never matched and every single creation was followed by the
server repeating itself. What the owner heard was: *"Gunalan's birthday
is marked on the 5th of February, sir. It is Gunalan's Birthday, all
day on Friday 5 February."* It owes the name alone now, which is the
part anybody repeats and the part they would notice missing. The
lesson generalises: an obligation phrased as the server's own wording
is an obligation that can never be met.

**The sentences the server adds are spoken in the persona.** What
`Ensure` appends is written in Go, not by the model, and it is read
aloud as though the assistant said it -- so without help it arrives in
nobody's voice: a butler's reply ending in a line of flat server
English. The address is woven in before the full stop, and only when
the reply has not already used it, since a persona that says sir says
it once and a reply that says it twice sounds like two people talking.
A persona that addresses nobody gets nothing added.

The alternative was to hand the answer back to the model and ask it
again, in its own words. That is kept in reserve: it costs a round
trip on a voice turn and it can be got wrong twice, where this cannot
be got wrong at all.

**Counts of the same thing are owed once, not once per call.** Four
events removed in one turn owed four pairs of numbers -- eight then
seven, seven then six, six then five, five then four. Read out in a
row they are gibberish, and they cascade: the answer said "had 8
events and now has 4", which satisfies neither the first pair nor the
second, and each sentence appended supplied the number that made the
next one match. What came out was four contradictory-sounding
sentences after a correct one. `tool.Merged` collapses tallies of the
same noun into the net change before `Ensure` runs, keeping the place
of the first so the answer still reads in the order things happened.
Different nouns stay apart: reminders and events are not one tally.

**A tool that was refused is not evidence about the world, and it was
read as such.** Told to delete four one-day events, the assistant made
four `calendar_cancel` calls carrying
`<id_for_first_single_day_event>` and the like, had all four refused
by the identifier pattern, and then told the owner the events did not
appear to exist. They did exist; it had never looked. Nothing was
deleted, which is the pattern earning its keep, but the answer was
false and the failure was its own.

Three things came out of it. A value shaped like a placeholder is now
named as one -- "a description of the thing rather than its
identifier. List them and use the identifier exactly as it came back"
-- rather than being called the wrong shape. Every refused call now
carries the sentence "this call never ran, so it says nothing about
what exists". And the honesty rules say it in general: a tool that
failed did not run, so it is not evidence that a thing is missing, and
where it asks you to look something up first, do that before asking
for anything to be removed.

**An identifier the model does not have is invented rather than
asked for.** A call went out with the literal
`<identifier_for_the_existing_event>`. Google refused it, for the
wrong reason. Event identifiers now carry a pattern, the way memory
identifiers always have, so the refusal says what is actually wrong.

**A calendar and an event are different things, and the tool names
have to say so.** The tool that reads events was called
`calendar_list`. Asked on 28 September 2026 to list the calendars, the
model called `conversation_find` and then `conversation_list`, and
answered, correctly, that what it had found were conversations rather
than calendars. It was not guessing: nothing offered to list
calendars, and the one tool whose name suggested it reads events. It
is `calendar_events` now, and `calendar_calendars` and
`calendar_rename` exist.

`calendar_calendars` needs both scopes at once -- `calendar.readonly`
to list them, since `app.created` forbids it -- and marks which one
the assistant writes to, because read access and write access look
alike until something is refused. `calendar_rename` patches rather
than replaces, so the timezone and description survive, and it renames
the assistant's own calendar only: the permission reaches no other, and
refusing here gives the model a sentence to pass on instead of a scope
error.

## Reaching Google

One OAuth client for every Google API. What may be done with it is
decided by the scopes asked for, which grow as tools are added, not by
having a client each. `internal/google` holds the permission and hands
out `*http.Client`s; nothing above it sees a token.

**The consent screen asks for offline access and forces the prompt.**
Without offline there is no refresh token at all. Without forcing it,
Google returns one only the first time a person ever grants this
client -- so reconnecting after a disconnect yields an access token
that dies within the hour and nothing that outlives it, which looks
like a bug an hour after it looked like success.

**The client is in Testing, on purpose.** A client in Testing issues
refresh tokens that expire after seven days -- *unless the app requests
only name, email address and profile*. It requested only those to begin
with, so nothing expired.

Publishing to production is blocked by Google on a homepage URL and a
privacy policy URL, which need a public domain nobody here owns. The
owner chose on 27 September 2026 to leave it in Testing until a
sensitive scope forced the question, and later the same day asked for
the sensitive scopes anyway, knowing the cost. **The seven days are now
running**: the connection needs re-granting every week until the app is
published, and `./personal-assistant google <user>` is how to find out
that it has lapsed before a tool does.

## The sensitive scopes

`calendar.readonly` and `tasks`, granted on 27 September 2026 because
the owner wanted the real calendars and the real task lists, not only
the one calendar the assistant made for itself.

They are sensitive, so Google shows an unverified-app warning that has
to be clicked past, and the Tasks API had to be enabled separately in
the console. **Neither is granted by asking for it in `config.ini`
alone.** Three things must all agree: the scope is saved on the Data
Access screen, the API is enabled, and the consent screen is completed
with every box ticked. The boxes on the granular consent screen **start
unticked**, and pressing Continue past them grants nothing while
looking exactly like success. This has now caught us more than once;
the way to be sure is to read `.checked` off the page, and afterwards
to ask Google what the token actually allows rather than trusting what
was written down.

What that grant sees: eleven calendars, of which two are the owner's
own -- `rjdhanush22@gmail.com`, the primary, and the assistant's own
`Jarvis` -- and the rest are read-only Classroom and holiday
subscriptions. One task list, `My Tasks`.

**There is no Birthdays calendar in the list**, but there are birthday
events. Google only lists the contacts' birthday calendar when it is
switched on, and it is not; the birthdays that exist are ordinary
events on the primary calendar carrying `eventType: birthday`, an
all-day event with an annual recurrence. So a tool that goes looking
for a calendar called Birthdays finds nothing, and a tool that reads
the primary calendar finds them without trying.

**Most of what is on the primary calendar was not put there by hand.**
Of fifteen events across three years, eight are `eventType:
fromGmail` -- cinema bookings and hotel stays that Google extracted
from confirmation mail. They are read-only by Google's rule and cannot
be created through the API at all. Anything that counts, summarises or
tidies the diary has to expect that most of it is not the owner's own
writing, and that it cannot write back the same kind of thing.

`tasks` is read and write; `calendar.readonly` is read only. So the
assistant can be trusted with a task list and can only look at the
diary -- which is the right way round for a first attempt at either.

In Testing with an External audience, only listed test users may grant
access at all, so the owner's own address is on the test user list.
Without it the consent screen refuses outright.

**And the exception, found the same day.** A word list is not code, it
is English. `calendar_events` was given an `Insist` predicate that
matched dates, months, "birthday", "when is" and so on, so the server
could refuse an answer until the calendar had been read. The owner
threw it out: *"its not guaranteed that i will speak only english"*.
A matcher that only works in one language is a bug waiting for the
first sentence in another, and it fails silently -- the question
simply is not recognised and the old wrong answer comes back.

So this is the carve-out in the owner's own rule: what cannot genuinely
be done in code is done in the prompt, and only then. The whole
mechanism was removed -- the predicate, the registry's `Insisted`, the
runner pressing an answer back.

**What is used instead, and why those.** Two levers, both already
measured in this file:

- Framing against the listing. `memory_list` is prefetched on every
  turn and had no `WhenUnasked` at all, which is what produced the
  failure: asked when Alekhya's birthday was, the assistant read the
  memory block, found a birthday belonging to somebody else and a note
  about Alekhya that was not a birthday, and answered *"I don't have
  Alekhya's birthday on record, sir"*. It never opened the calendar.
  The block now says what it is -- what you were told, not a record of
  their life -- and that anything with a date in it is in the calendar
  whether or not it also appears here.
- Examples. `calendar_events` now carries "when is Alekhya's birthday"
  with a year-wide range. An example teaches the shape of a question
  rather than its words, which is the part that survives the person
  speaking another language.

**Code before prompt, wherever code can do it.** The owner's rule, 28
September 2026: *"wherever something that can be enforced by code
rather than prompt should be done"*. A prompt is asked; code is
obeyed. Prompt wording is for what genuinely cannot be settled in the
server -- tone, judgement, what to say -- and only then.

Reading every calendar is the example that produced the rule. The old
`UseWhen` told the model it could name a calendar and it did not, so
the answer was a question back to the owner. The default moved into
the server instead, and no wording is needed for it any more.
`readFirst` is the same idea for writes: the registry refuses a write
whose domain has not been listed this turn, rather than asking the
model to list it.

**Every calendar is read by default.** Asked *"check if any event is
there on October 2nd"* the assistant answered *"Nothing written here
for that day, sir. That only covers what I have put down; shall I
check your own calendar as well?"* -- while 2 October is Gandhi
Jayanti and the holiday calendar said so. Offering to look is the
tell: it knew there was somewhere else and stopped.

`Diary.Everywhere` reads all of them at once and merges them by start
time. At once rather than one after another, because one calendar
takes several seconds and five in a row would outlast the question.
Each event carries the name of the calendar it sits on, which is what
tells a holiday apart from something the person arranged, and only the
assistant's own carry identifiers, which is what keeps a holiday from
being offered up for deletion. Naming a calendar still narrows it to
that one.

A calendar that will not open is named in the answer. A day called
empty on the strength of a calendar that failed to load is a wrong
answer rather than a missing one.

**The calendar is the date and time hub.** The owner's rule, 28
September 2026: anything to do with a date or a time -- asked about,
mentioned in passing, or only discussed -- is read from the calendar
before it is answered. Not only "what is on Tuesday": a birthday named
while talking about something else, a trip, a deadline, the weekend.
What was said may already be written down, or may clash with something
that is, and neither can be known without looking.

This is looking, not offering. `noticing` decides when to offer to
write something down, and still wants a thing and a time; this decides
when to read, and the answer is wider. The rule is in the tool's
`UseWhen` and in `timekeeping` in the persona, in both places because
framing next to the tool is what has been measured to hold.

**A whole-day event is dates, and a timed one is timestamps, and a
patch may not swap one for the other.** `Diary.Update` always wrote
`DateTime`. For a birthday -- which Google stores as `Date` -- that is
a request to change the kind of event it is, and the API refuses it
outright. Moving Alekhya's birthday to 10 September 2027 failed twice
on 28 September 2026 for this reason, and adding the same date as a
new event succeeded a minute later, because `toGoogle` had the branch
all along and `Update` did not.

`Amend` now carries `AllDay`, read off the event the tool has already
fetched rather than asked of the model, and `patchFor` chooses the
shape. It does not count towards `Empty`: it states what the event
already is rather than asking for anything, and counting it made
"nothing was given to change" unreachable. The construction was split
out of `Update` into `patchFor` so this is testable without Google,
which is the only reason it is a separate function.

**The failure was reported as something that never happened.** What
the owner heard was *"The 10th of September 2026 is already past, sir
-- Google will not accept a date in the past"*, and the model defended
it when questioned. Nothing in the tool result said that; the tool
said only that the call failed. A bare failure carries no explanation,
and the model supplied a plausible one. The persona already says *a
tool that failed told you nothing about the world* for this reason,
and it was not enough -- the invented reason was specific, confident
and repeated. Worth watching whether failures need to carry what went
wrong in words rather than leaving the gap to be filled.

**A refresh token dies for reasons invisible from here** -- the
password changed, six months passed unused, it was revoked from a
settings page. `invalid_grant` is the only error treated as fatal;
everything else is a passing failure and must not mark a working
connection as broken. When it is fatal the account is marked broken
once, with words naming the three likely causes, so every tool does not
discover it separately and report it unhelpfully.

**What Google granted is recorded, not what was asked for.** A person
can untick part of a consent screen and Google issues a token for the
rest without complaint. `TokenSource` takes the scopes a caller needs
and names the missing ones, because the only fix is for the person to
grant them and they cannot if nobody says which.

**A rotated refresh token is written down before the access token is
used.** Google occasionally issues a new one; the copy in the database
is the only one that survives a restart.

Granting a *different* Google account is refused rather than absorbed,
compared on Google's `sub` rather than the address, since an address can
change hands.

**The callback cannot require a token, and mounting it as though it
could was a real fault.** What arrives there is a browser Google has
just redirected, and a redirect carries no `Authorization` header. It
was inside the authenticated group with a comment claiming the browser
carried the session -- which is true of a cookie and false of a bearer
token. It answered 401 and the connection could never have completed.
Found by trying it rather than by reading it.

The state is what stands in for a token: 32 bytes from `crypto/rand`,
issued against one person, spent on first use, dead after ten minutes.
That is what OAuth state is for.

`./personal-assistant google <user>` says whether the connection still
works, and proves it by exchanging the stored token for a live one
rather than reporting what was last written down. Everything else here
has had the same shape of fault at least once -- something stopped and
nothing noticed -- so the question has a command.

The tests stand a server in Google's place through `Config.Endpoint`.
Written after the first pass reached accounts.google.com for real and
two tests passed for the wrong reason.

**Speaking outlives the request that asked for it.** The announcement
blocks until the words have finished playing, and the caller does not
wait that long: Home Assistant's `rest_command` gave up after ten
seconds, which closed the connection and cancelled the request. A
greeting with three held reminders behind it is longer than that.

Cancelled halfway, nothing was spoken and nothing was recorded, so the
held ones stayed held and the next arrival did exactly the same again.
Found on 27 September 2026 with three reminders unheard through two
arrivals: the owner's watch was on the desk and the reminder they were
waiting for never came.

So the handler speaks and records on a context detached from the
request, with `SpeakingFor` as its own deadline. Values are kept, so a
failure is still traceable to the arrival that caused it; only the
cancellation is dropped. The `rest_command` timeout was raised to match,
which changes nothing about correctness and stops Home Assistant logging
an error for work that succeeded.

**The model is not asked, and this was tried both ways.** A greeting
that lands after somebody has sat down is not a greeting: detection
takes about two seconds and a Platform AI call has measured between two
and five. Writing it in advance solves the timing -- the next one is
composed as soon as the last is used -- and was built and tried on 27
September 2026 with the person's memories and recent conversation fed
in. It was dropped, by the owner, after three rounds of it going wrong
in three different directions:

- Given a free hand it wrote *"Get Lucky has been in my head since you
  last mentioned it"*, claiming an inner life it does not have.
- Told to be plain it wrote *"the coffee is on"*, turning the fact that
  the owner likes filter coffee into a claim about the room.
- Told what it may not claim, it retreated to the bare greeting every
  time, which is what the plain version does for nothing.

The lesson is narrower than "the model cannot do it". Every personal
remark that is not a claim about the world turns out to be either an
opinion or a wish, and the room to be warm without being false is
smaller than it looks. Worth remembering before adding it again.

Every call greets. It did once refuse to greet the same person twice
within ten minutes, on the reasoning that nothing upstream promises to
ask only once -- and that guarded against the wrong thing. Of three real
arrivals in a quarter of an hour it refused two, which is the same
silence as the fault it was there to prevent and harder to explain.

Whether somebody has really been away is the caller's to judge, and the
caller judges it on thirty unbroken seconds of a faint signal, which is
better evidence than any clock here. A greeting that could not be spoken
says so rather than reporting success.

**Presence cannot see a return when the laptop travels.** 28 September
2026: the owner came home from the office and was not greeted.
Nothing was broken. `sensor.watch_signal` had not been absent for a
moment since 20:40, averaging -55 to -60 dBm across two and a half
hours, and `input_boolean.in_the_room` had read `on` the whole time.
The arrival rule fires on a transition into the room, and there was no
transition to fire on.

The reason is what the sensor measures: the watch's distance from
*this laptop*. The laptop went to the office too, so the watch and the
laptop never separated, and a pair that never separates cannot record
a departure or a return. It works when the laptop stays put and the
person leaves; it is blind when they leave together.

Nothing in the thresholds will fix this -- the signal was strong
throughout, correctly. Telling home from elsewhere needs a second
signal that knows where the laptop is: the Wi-Fi network it is joined
to is already to hand, and a phone tracked by the Home Assistant
companion app is the other obvious one. Not built, and not yet the
owner's decision which.

**Not in the transcript yet.** The owner asked for what the assistant
said to be recorded, and it is not: a `chat` requires a prompt, and the
whole history is prompt-and-answer pairs. Recording something the
assistant said unprompted needs that model changed, which is why
reminders do not appear there either. Left undone rather than faked with
a prompt nobody spoke.

**And the ask is now wider than greetings.** 28 September 2026: *"a
conversation rename event should also go into the conversation --
every event should go into the conversation"*. Not only what the
assistant said unprompted, but what happened: a conversation renamed,
a reminder fired, an event written into the diary.

The obstacle recorded here -- that the message model cannot hold a
thing with no prompt -- had already been solved and this note was out
of date. `ReminderAnnouncement` and `PresenceAnnouncement` are exactly
that: assistant messages with no question above them. Naming a
conversation only needed to join them.

**A rename is an announcement.** `conversation.Renaming`, written by
`noteTitle` on every channel rather than only the one that heard it
said. Seeing the new name in the listing is not the same as the
assistant being able to answer for it: asked *"why did you rename like
that?"* it replied that it had no record of performing a rename and
asked what was meant. That was true. The name went to the client as an
event and into the listing, and nowhere a model could read.

The model is told *"You named this conversation at 11:31 pm, from what
had been said in it so far"*, because the question that follows a
rename is why, and the reason is the half that answers it.

Still to come, on the same rule: an event written into the diary, a
reminder cancelled, a memory saved. Each is a tool result today and
lands only in the turn that caused it.

### Reminders are said through Home Assistant, to the room and to the phone

Owner's request, 29 September 2026: *"i want the announcement to be
heard on phone too"*. Too, not instead -- the speaker in the room is
still the one expected, and the phone is an addition.

`Config.Notify` lists Home Assistant notify services, and `Say` calls
each after the satellite has spoken. The message sent is the literal
word `TTS` with the words in `tts_text`, which is how the Android
companion app is asked to say something rather than show it; a phone
that only shows it is a phone somebody has to look at, and that is not
what an announcement is for. `media_stream` is the music stream rather
than the notification one, because the notification stream is silenced
by every do-not-disturb rule, and an announcement nobody hears is the
fault this exists to fix.

**These leave the house, and nothing else here does.** A push
notification does not travel over the local network or the VPN: Home
Assistant hands it to its push service, which reaches the phone
through Google. That is why it arrives on mobile data, on somebody
else's wifi, and with Tailscale disconnected -- and why the words are
seen by somebody other than the owner on the way. Worth knowing before
adding a second service to the list.

Failures are dropped rather than returned. The announcement has
already been made in the room by the time the phone is tried, and
reporting failure would have the caller retry, saying everything twice
where it was already heard.

Still one-way and unconditional: every announcement goes to both. The
obvious next step is `input_boolean.in_the_room` deciding -- the room
when they are in it, the phone when they are not -- which would also
answer reminders being held back entirely when nobody is there.



Owner's decision. The satellite is the only place a reminder lands.
Pushing to a browser was planned and dropped as not worth it: the person
is not at the screen when a reminder matters, and the server would have
needed a channel it has never had.

`Everywhere` stays, holding one speaker. It is the seam a second one
would go in, and it costs nothing to leave.

The browser therefore still goes stale when something is said out loud,
which the client's own `refresh` comment already describes. That was going
to be fixed by the same stream and now is not.

### A reminder announces itself with no sound before it

Tried on 27 September 2026 and dropped the same night. A short sound
first, and only one for a run of them however many came due together,
which is the part that would have been easy to get wrong.

Home Assistant supplies the sound, and the owner's verdict on hearing it
was a cowbell. The idea went with it rather than the sound alone: no
heads-up before a reminder, and `Preannounce` stays false.

Written down because it is an obvious thing to add and it has already
been decided against. Adding one means picking a sound first.

### Speaking and asking need different timeouts

Home Assistant holds `assist_satellite.announce` open until the satellite
has finished playing -- "Block until announcement is finished", in the
comment in its own `entity.py`. The call therefore takes as long as the
speaking does, which is nothing like how long a state poll takes.

One shared ten-second timeout covered both, and ten seconds is about a
hundred and fifty characters of speech against a `MaxBody` of five
hundred. A longer reminder played perfectly well and then reported
failure. The loop leaves a failed one pending on purpose, so it was said
again two seconds later, and again, until the grace hour ran out.

So there are two: `Timeout` for anything that only asks, and
`SpeakTimeout` for the one call that speaks. The client carries no
timeout of its own -- one number cannot be both -- and each request takes
its own deadline. The speaking deadline starts after the wait for quiet,
which is longer than it and is not the speaking.

Nothing here makes a double-say impossible: a call that fails after the
words are out will always look like one that never said them. It removes
the way it actually happened.

### The reminders screen shows what is coming, and what happened if asked

`GET /v1/reminders` lists what is pending, what is held and what was
missed. `?past=true` adds the ones that were said, which the screen
asks for behind a Show past link, off by default: a screen that opens
on a log buries the two or three things actually coming.

A past reminder is one that reached somebody. A cancelled one never
did -- it stopped existing before its time -- and listing it among
things that happened says it happened. So `past` leaves them out.
`all=true` is still there for the whole record, and nothing on the
screen asks for it.

Held was left out of that list when holding was added, which made a
reminder kept back because nobody was in the room invisible -- not
spoken, and on no screen. That is the same fault this page exists to
prevent, reintroduced by the feature next to it.

An entry says which it is rather than looking the same: said, called
off, waiting for you, missed. One that has already happened offers no
Snooze and no Cancel, since there is nothing left to do to it.

### The reminders screen shows what is coming, not what happened

`GET /v1/reminders` lists what is still pending, and `DELETE
/v1/reminders/{id}` calls one off. `?all=true` includes the rest, for
anything that wants it; the screen does not. Everything that ever fired is
a log, and nobody opens a settings screen to read one.

Cancelling from the screen removes the row rather than showing it struck
through. The list is what is coming, and something called off is not.

A default scope was planned for this screen and left out. With one
satellite, user and client land in the same place, so the setting would
configure nothing anybody could hear.

### A reminder that was never said must not vanish

Home Assistant being unreachable at ten o'clock used to lose the
reminder entirely. It stayed pending and was retried every two seconds,
which is right; but past the grace hour it was marked missed, and then
nothing at all happened. It was never spoken, the settings screen asked
only for pending ones, and no code anywhere read the missed status back.

Three things, all absent and all promised:

- **A late one says so.** "This is late. It was due at ten o'clock." One
  said at a quarter to eleven that sounded exactly like one said at ten
  was acted on as though it were ten. The day is named when it crossed
  midnight.
- **Missed ones are on the screen**, marked, with Dismiss rather than
  Cancel: nothing is called off about something that already failed to
  happen.
- **They are mentioned once** in the next conversation, then never again.

The mention is marked as given only once an answer exists. Marking it
while composing the prompt spent the one telling on a turn that then
failed, and on one where the model simply left it out -- which is how a
miss came to be recorded as told without ever being told.

The wording had to be forceful to survive. "Mention them once, briefly,
at the end of your reply" lost to the persona's instruction to be brief
and to a prompt full of memories. Telling it to *begin* the reply with
it, and that leaving it out loses it for good, carries four openings out
of four -- including "hello" and "thank you", which give it the least
excuse to add anything.

### Snoozing, and what "that" means

A reminder that rings at four can be pushed back: "snooze that", or
"push my four o'clock to five" before it rings. Both, because the second
is the first with the identifier given rather than assumed. Ten minutes
is what "snooze" alone means, which is what the word means on every
clock radio ever made.

The microphone stays shut after a reminder, for now. The satellite can
reopen it -- `assist_satellite.start_conversation` sets
`msg.start_conversation`, which `satellite.py` already honours, playing
the reminder and then listening -- but a microphone that opens in the
room after every reminder hears the room. So a snooze is asked for with
the wake word, like anything else.

That leaves "that" to be resolved, and today nothing can: the firing loop
goes to the announcer and never touches the conversation, so the
assistant has no record of having spoken. Asked what it just said, it
does not know. Two things fix it, and they are the same two things:

- **A block saying what was just said aloud**, on the pattern of the
  missed-reminder one. It gives "that" a referent, the way a person has
  one, because the thing was said a minute ago and both parties know it.
  `remind.JustSaidWindow` is a quarter of an hour; past that the
  assistant asks which rather than guessing at an hour-old reminder. It
  is carried every turn while it is recent, unlike the missed block,
  which is spent on one telling: this one is context, not a message.
  It is also told *not* to raise it -- the person was there.
- **The tool defaults to it.** `reminder_snooze` with no identifier takes
  the last one spoken, so an identifier never has to survive a voice turn.
  Two fired close together and it refuses and names them, with their
  identifiers, so the question is asked rather than answered wrongly and
  the answer can be acted on in the same turn.

The block says what "that" means only where one thing was said. Where two
were, it says to ask. It used to say "the most recent one listed", which
names nothing when two came due together: the firing loop takes the clock
once per pass and stamps every reminder in it with the same moment, so
the order falls through to the identifier, and the most recent is
whichever was created last. The tool refused to choose anyway, and a
prompt that tells the model to choose while the tool refuses to is two
minds about the same question.

A repeating one is not moved. Snoozing a daily seven o'clock by ten
minutes would make it ten past seven tomorrow, and twenty past the day
after. The snooze is a one-shot of its own and the series is left alone.
`remind.Later` is where that decision lives, so the spoken tool and the
`POST /v1/reminders/{id}/snooze` endpoint cannot come to disagree, and
both say which of the two happened: reporting "put off" for a daily
alarm that has not moved would be a bluff.

The store refuses to snooze a repeating one at all (`remind.Snoozable`),
which is what makes the rule an invariant rather than a convention two
callers happen to follow.

### What was said and what was never said are different answers

`reminder_recent` answers "did I miss anything", over the last hour by
default, because somebody who has stepped out and come back is asking
about the time they were gone.

It keeps two lists apart and labels them: ones that were spoken, and ones
that were never spoken because nothing could say them at the time. Run
together they would tell the person they heard something they did not,
which is the same fault as any other thing the assistant claims happened
and did not. The spoken list comes from `LastSpoken`, which is bounded,
so when the bound is reached the tool says there may be more rather than
implying the list is everything.

A held one is the third category: not said yet, and not missed either.

### A reminder is not said to an empty room

A reminder whose time comes while nobody is in the room is **held**
rather than spoken, and said when they walk back in, after the greeting.

`held` is its own status because it is its own fact. Missed means nothing
could say it, and drives the mention-it-once path. Held means something
could and chose to wait. Its due time is left alone: that is what the
reminder was for, and what the person is told when they get it.

**The rule is fail-safe, and that is the whole design.** `remind.Presence`
asks one question and anything short of a confident, current "they are
elsewhere" comes back false, so the reminder is spoken. An error, an
unavailable entity, a state nobody recognises, nothing configured at all
-- every one of those speaks. The two mistakes do not cost the same:
speaking to an empty room wastes a sentence, and withholding a reminder
from somebody sitting right there loses it until they think to ask. The
signal this rests on has already been seen to drift ten decibels in half
an hour, which is the argument rather than a hypothetical.

Home Assistant is asked at firing time rather than told in advance. It is
the only thing that knows, and asking it means there is no second copy
here to go stale. `[homeassistant] presence_entity` names what to ask;
empty means never hold, which is what this did before.

A repeating one is never held. Its next turn is along soon enough, and
holding one would queue a morning alarm to go off the moment somebody
walked past at lunchtime.

`Fired` accepts a held reminder as well as a pending one, which it did
not at first: the delivery was spoken and then could not be recorded, so
it stayed held and would have been said again on the next arrival. The
log said so -- "said a held reminder but could not record it" -- which is
the honest half of that failure working.

### What is waiting goes in the prompt, not behind a tool call

Asked twice what reminders there were, the assistant answered from an
exchange two hours old -- its own earlier answer, correct when given,
repeated as though it still held. The eleven o'clock it named had gone
off at eleven o'clock. The timeline proves no tool was called.

The rule against this already existed, in the persona and in the
transcript block, and did not hold. Worse, it could not be made to fail
on demand afterwards: an eval was tried against Sonnet and Haiku, with
and without conversation history, with and without the voice block and
recalled notes, and every one of twenty asks looked it up. So there was
no way to tell whether a stronger rule would hold either.

So the rule was replaced with the answer. `remind.Coming` puts the
current reminders into every prompt, read as the prompt is built. There
is nothing to skip, nothing to decide, and nothing older to reach for:
the true answer is already there. It costs a line or two of text and it
removes the class rather than discouraging it.

It is also faster than the tool it replaces. A reminder question used to
cost a second model round trip; now it is answered from what is already
in front of the model.

**It had to be told not to volunteer them.** Putting the list in every
prompt made it salient, and "I'm feeling so tired" was answered with
"the Go down and eat reminder will be due shortly at 1:10". Two things
pulled that way: the block said "answer from this list and from nothing
else", which reads as an instruction to use it, and the persona has a
licence to raise things somebody would want flagged.

So the block now opens by saying it is for one kind of question only,
and that being told how somebody is -- tired, hungry, that it is late
-- is not that question. Unrelated remarks are quiet again. Something
directly related still gets a mention, which is the behaviour the owner
asked for elsewhere and is left alone.

**One caveat, honestly.** The very first live turn after this went in
still bluffed, with the block present and saying "answer from this list
and from nothing else". Three recalled exchanges outweighed it. Every
turn since has been right, and the conversation has since filled with
correct answers, so what is not known is how it behaves when the stale
claim is fresh and repeated. Putting the truth in front of the model
makes the failure much harder; it does not make it impossible.

### A late reminder is said in the past tense

"Time to take your tablets" heard at noon sounds like now. Held and
missed reminders are both heard after the moment has gone, so they need
different words, and the words have to be in the past.

The work is split by what each side can do. The **model** writes the
past form when the reminder is set -- `say_if_late`, "You should have
taken your tablets" -- because that is grammar and the server cannot
conjugate. The **server** puts the hour in when it speaks, because that
is arithmetic and arithmetic is not the model's to get wrong. Out comes
"You should have taken your tablets at 10:00 am, sir."

The model is told to leave the time out of `say_if_late` entirely.
Anything it wrote there would be a guess: when a reminder is set nobody
knows how late it will be heard, or whether it will be late at all.

The alternative was rephrasing at delivery, with a model call when a
late one is spoken. It would read better still -- only then is the
actual delay known, so it could say "two hours ago" -- and it was not
taken. Every attempt that night at letting the model phrase something
warm produced a claim that was not true: "the coffee is on", "it has
been in my head". A reminder is the wrong place to risk that, and a
model call at delivery is also a thing that can fail at the moment it
is needed.

Reminders made before this have no past form, and neither does one the
model leaves out. Those fall back to the hour in front of the body:
"You should have heard this at 10:00 am, sir. Time to take your
tablets." Said first, because otherwise the words arrive sounding like
now.

### A length of time is the server's arithmetic, not the model's

`reminder_set` takes exactly one of `seconds_from_now`, `minutes_from_now`
or `at`.

Seconds were missing at first, and the model reported that faithfully:
asked for thirty seconds it answered "timers can only be set for whole
minutes". It was not being awkward -- there was no argument that could
carry it. A tool's limits become the assistant's excuses, stated with
confidence, and are hard to tell from the model refusing.

The two lengths exist so that "in twenty minutes" needs no arithmetic from
the model: it is told the current time in the prompt, and asking it to add
twenty minutes to that is a step that can go wrong silently. A time of day
still has to be written out, and is read in the person's own zone.

Several spellings of a written time are accepted, because a model writes
the same moment several ways and refusing it over a missing "T" would be
refusing the reminder. A time already gone is refused, and the refusal
says what time it is now so the next attempt can be right -- except within
two minutes, since the model works the time out from what it was told and
a little passes while it does.

### The first work that happens because of the clock

Everything in this server until now ran because somebody asked. The
reminder loop is the first thing that runs when nobody is. It asks what is
due every five seconds, which is one indexed read, and is stopped with the
server so nothing is half said during a shutdown.

Three outcomes, and the difference between them matters:

- **Said.** Delivered, then either finished or moved to its next time.
- **Missed.** A one-shot too late to be worth saying. Kept rather than
  removed, so it can be mentioned once.
- **Moved.** A repeating one whose turn was too late. Moved to its next
  time, not marked missed -- marking it missed would end a daily reminder
  for good -- and no firing is counted, because nobody heard it.

Something nobody could deliver stays pending and is tried again. That
cannot go on for ever: once it is older than the grace window it becomes
missed like anything else. A speaker that cannot deliver therefore has to
say so rather than returning quietly, which is why `Nowhere` refuses
instead of succeeding.

`Fired` only touches a reminder that is still pending. Two passes cannot
both say the same thing, and a pass that says something and then fails to
record it is logged loudly, because that is the one way a reminder is
heard twice.

### A reminder only ever says something

Owner's decision. A reminder carries words and speaks them: a timer that
has finished, something to be told at seven. It does not run instructions
unattended.

That was the other half of the idea and it was dropped deliberately. A
task firing with nobody watching runs with the owner's authority and can
reach tools, and a misread instruction at three in the morning has nobody
to catch it. Announcing is most of the value and almost none of the risk.

A timer and a schedule are one thing stored one way, differing only in how
the time was written. Two stores would mean two firing loops and two sets
of bugs.

### An answer records how it was made

`messages.chat_id` says which turn wrote a message, and `chats.recalled`
holds what memory offered and what it scored. `GET /v1/chats/{id}/steps`
puts them in order: asked, recalled, each tool call and its result with a
duration, answered.

Nothing in it is re-derived. A search run now could disagree with the one
the model was shown, and a timeline that disagrees with what happened is
worse than none.

It paid for itself on the first real answer. Asked "what did the builder
charge for the upstairs work", the assistant replied "there is nothing on
record" -- and the timeline showed the roof memory had been offered at
0.588, then found again by `memory_search`, and refused twice. From the
log alone that is indistinguishable from never having found it, which
needs an entirely different fix.

The cause was the answering rule: "use a note only if it contains what is
being asked for, rather than merely a related subject". It was written to
stop a near miss being read as an answer and it refused a direct hit,
producing a false statement as well as a miss. It now says to judge
whether it is the same thing rather than whether the words match, and
never to say nothing is on record when a note is plainly about what was
asked.

The eval had passed throughout, because it composed the prompt with the
default manner while the server runs Jarvis. An eval that measures a
prompt must use the manner the server is configured with, or it measures a
prompt nobody sends.

### A false success becomes a fact

Asked to add milk to a shopping list it has no tool for, the assistant
answered "Milk has been added to your shopping list, sir" and called
nothing. That answer was written into the transcript, indexed, recalled on
the next turn, and cited: "Milk is already on your shopping list, sir."
Then again. A bluff is not a single wrong answer; it is evidence for the
next one.

The prompt now says the assistant acts only through tools, that nothing
else it says changes anything, and that before any sentence claiming
something was done it must check a tool did it in that same turn. It also
says it holds no shopping list, no to-do list and no calendar, whatever
old conversations look like -- the general rule did not reach lists,
because the transcript is full of lists being read out and noted down.

The other half is state. Told only not to claim actions, it moved to
asserting how things stand: "milk is already on your list", from three old
grocery exchanges. So it is also told not to say what is on a list or what
a device is doing unless a tool said so just now, and that something said
in an earlier conversation is what was said then, not what is true today.

Measured under `make evals`: eight actions there is no tool for, six
questions about state it cannot see, and three of each again with a
transcript block in front of it -- because without one they pass in a
vacuum and fail in use.

**Not solved.** It still sometimes says "I have noted that" without
calling anything, in the same breath as correctly refusing the list. Three
rounds of sharpening moved it from a flat false success to a soft
one. A prompt reduces this; it does not enforce it. Enforcing it means
checking the answer against the tools that ran, which nothing does yet.

### The point of memory is advice, not recall

Stated by the owner. The assistant is not meant to do only what it is told.
It should offer insights, point out a better option, bring up what was
forgotten, and say what could go wrong before something is done -- an
advisor rather than an instrument.

Recall is the ground floor of that and not the thing itself. Answering
"what did the roofer quote" when asked is retrieval. Saying "you agreed
forty thousand and this quote is sixty" without being asked is advice, and
it needs three things retrieval does not: memories of decisions and their
reasons rather than only facts, a search run against what the assistant is
about to say rather than only what the person said, and permission to
volunteer.

That last one was not free. `memory.Offered` instructed the model to use a
note "only if it contains what is being asked for, rather than merely a
related subject", which is what stops a near miss being read as an answer
and is also what forbids advice.

It now does both jobs, stated as two separate instructions, because the
rule for one forbids the other. Answering is unchanged. Warning is narrow
on purpose: only when the person says what they are about to do, only when
a note disagrees with it, one line, attributed, nothing beyond what the
note says, and nothing at all when the note agrees.

Widening it did not cost the filter. Measured in one run: three of three
notes used when they fit, five of five unrelated questions still left
clean, three of three real conflicts raised, four of four harmless
intentions left alone. Told "I am going to tell the roofer sixty thousand
is fine" it answers "the agreed price was forty thousand rupees, not sixty
thousand, based on the roof quote you saved"; told "I am going to pay the
roofer the forty thousand we agreed" it says nothing about the note.

The licence is on the curated memories only. The transcript keeps its
quoting rule and no licence to warn: it carries whatever speech-to-text got
wrong, and a warning founded on a misheard sentence is worse than none.

The bar is unchanged: an advisor that invents a concern is worse than one
that says nothing. Anything volunteered rests on something stored and says
what it rests on.

### Memory is three layers, and only the middle one is hard

What the assistant is asked to remember outlives the conversation it was
said in. `internal/memory` holds it in two tiers. `always` goes into every
system prompt whole, under a ceiling in both count and bytes, because
anything composed into every prompt needs a size that cannot run away.
`recall` is searched with whatever was just asked, and the nearest few are
offered. `internal/tool/memories` is the deliberate path: remember, search,
update, forget.

Nearest is not relevant, and that is the whole difficulty. A question with
nothing stored about it still has a nearest memory, and it scores in the
same range as a real match. Measured over twelve questions whose answer was
stored and eight with nothing stored, the two groups overlapped: a genuine
match scored as low as 0.414 while an unrelated one reached 0.504, so no
cut-off separates them. A cross-encoder rejected all eight but found only
eight of twelve. The model, shown three candidates and told that none of
them fitting is the usual case, found twelve of twelve and left all eight
alone. So the search shortlists and the model decides, and `memory.Offered`
carries the instruction that makes that work.

That instruction is load-bearing and invisible, so it is measured. The
inline shape -- notes in the system prompt of the call that also answers --
scores three of three used and five of five left out of the answer, under
`make evals`.

Writing happens only when asked. A missing memory is noticed and can be
added; a wrong one comes back as fact and nothing prompts anyone to doubt
it.

`EmbedOne` exists because `Embed` works a backlog oldest first: calling it
for a memory just written would embed something else and report success.

### The assistant is told where it is

Asked which conversation it was in, it named one it had switched away from
earlier. It had no way to know: nothing told it, so it answered from what it
remembered doing, and remembering having switched somewhere is not the same as
being there. A fact that cannot be looked up is a fact that gets invented.

The system prompt now carries the conversation's name and identifier, composed
per chat since it changes per chat while the manner does not. An unnamed
conversation is said to be unnamed rather than left out, because a gap is what
invites a guess.

It is one sentence against a whole transcript, and the transcript can win. In
a conversation already containing the earlier wrong answer, the model repeated
itself rather than reading the prompt. A conversation with nothing to copy
answers exactly. Being told where it is removes the reason to guess; it does
not overrule what it has already said.

### A tool that half-obeys has to say so

Asked to make a conversation, switch to it and answer there, the assistant did
the first two and answered in the old one. That is the rule working: a switch
takes effect from the next message, so the model's history does not change
underneath it mid-turn. It still read as a fault, because nothing was said
about it.

The rule stays. What changed is that the tool result now instructs the model
to explain, in as many words: tell the person the reply lands in the old
conversation, and if they asked for something to be said in the new one, say
plainly that it cannot happen until their next message rather than answering
as though it had.

Written as an instruction because a statement was not enough. The old result
said "it takes effect from the next thing the person says", the model read it,
acted on it, and mentioned none of it. A tool result is read by a model, so it
is written for one.

### The reserve is measured, not assumed

`ReserveTokens` was a flat 2,048 standing for the system prompt, the tool
schemas and the reply together. That was true while there were no tools. Seven
of them cost about 1,200 tokens; a dozen cost more than the whole reserve, and
the arithmetic protecting the context window would have been wrong with
nothing failing to say so.

It now covers the reply alone, and what is sent alongside is counted:
`ReserveFor(bytes)` adds the system prompt and every tool offered. A model with
a small window and forty tools is given measurably less conversation than one
with none, which it was not before.

Measured per chat, because both halves move: the manner can be changed while
the server runs, and the tools one channel may reach are not the tools another
may.

The endpoint takes at most 500 tools in a call, which is not the limit that
matters. At roughly six hundred bytes each, a hundred tools cost as much as
the entire conversation budget on every turn, used or not.

### Where a prompt lands is the server's to decide

The browser used to name the conversation on every prompt. That made
`conversation_switch` useless there: the switch was real, and the very next
message named the old conversation again and overrode it. The tool reported
success truthfully and nothing anybody could see had changed, which is the
exact failure the honest-outcome rule is meant to prevent -- a true statement
that is worthless.

So the browser names nothing. Where a prompt lands comes from the client's
active conversation, which is the rule voice already followed, and clicking a
conversation in the listing activates it as it always did. After a turn the
client asks which conversation is now active and follows it, since a tool can
move it while answering and the server is the only thing that knows.

A chat also records which client sent it. The channel was recorded and the
client was not, which was enough while a chat only produced words; a tool that
acts on the client itself has no other way to know which one to act on.

### Whether a description works is measurable

`make evals` asks a real model what it would do with fifteen things a person
might say, and checks which tool it reaches for and what it fills in. Behind a
build tag, because it costs real calls.

Everything else in the suite checks that the machinery runs. This checks the
only thing that decides whether the assistant is any good, and it cannot be
asserted against a stub: a stub has no opinion about a description. A failure
here is usually a description to fix rather than code.

A third of the cases expect no tool at all. A model that reaches for one at
every question is as wrong as one that never does, and only the negative cases
catch it.

Several tools can be right for one thing said, so a case lists the acceptable
ones. That was learnt from the first run: asked to delete something by name
the model called `conversation_find` where the case expected
`conversation_list`, and looking the name up is the better of the two. The
expectation was wrong, not the model.

### The loop, and what stops it

A turn is no longer one call. The model is asked, and if it asks for tools
they are run, both halves are written to the conversation, and it is asked
again from what came back. The question is not repeated on the second round:
it is in the history by then, and sending it again would have the model answer
it twice.

Five rounds at most. The last one is offered no tools at all, so a model that
has run out has to answer from what it gathered rather than being cut off
mid-chain. The person gets a reply either way, and the model has to say what
it managed rather than what it intended.

The call and its answer are written before the model is asked again. A chain
cut in the middle -- a restart, a deadline, the person saying stop -- then
reads as what was done rather than as a question nobody answered. That is the
same reason an interruption is recorded rather than the prompt being deleted.

### A wrong argument is answered, not refused

A call whose arguments do not fit the schema is not run, and the model is told
which argument, what was wrong with it and what was allowed. It corrects
itself on the next round. One told only that the call was invalid guesses
again. The correction costs a round trip and turns a wrong answer into a right
one, which is the whole trade.

Ulaa fights the same problem in prose, with a sub-prompt spelling out that
identifiers need quotation marks and a worked example of the mistake. A schema
and a correction make that unnecessary.

### This endpoint wants tool results together, not apart

Tool results go back as an array on one message, not as one message each
carrying a tool_call_id. OpenAI's shape is the latter and this endpoint is not
OpenAI, whatever the request otherwise looks like. Sent apart, the vendor
behind it rejects the whole conversation: "tool_use ids were found without
tool_result blocks immediately after".

That message was only legible because the error parser now reads
`error.api_error.message` before `error.message`. The outer one said "Error in
AI API Request", which names nothing. It is the third time today that the
useful half of a failure was being thrown away.

### Tool calls end a stream, they do not accompany an answer

An environment now answers in one of three ways: words, a failure, or a
request to run tools. `KindToolCalls` is terminal like the other two, and the
caller is expected to run what was asked and come back.

A model asking for a tool sometimes sends a sentence alongside it. That
sentence describes what it is about to do, not what happened, so the tool
calls win: reading the sentence out and stopping would tell the person about
work that never ran.

Offering no tools sends no `tools` field rather than an empty array. An empty
array tells the model something, and what it has been told is unclear.

A remembered exchange goes back as this endpoint matches it: the calls on an
assistant message, and each result as its own message carrying the identifier
of the call it answers. The outcome travels inside the result's text, because
the wire has nowhere else to put it and a result that reads as plain prose is
one the model will report as having worked.

### A tool is a shape, not a paragraph

Prose quality cannot be enforced and structure can, so a tool is described in
fixed parts: what it does, when to reach for it, when not to, and worked
examples. The descriptions that go wrong are the ones saying what a tool is
without saying when it applies, and the repair is always the same — the
description grows louder until it is shouting IMPORTANT at the model. Ulaa's
tab delegate does exactly that, and its sub-prompt then has to spell out that
identifiers need quotation marks. A place for "use when" is what makes the
shouting unnecessary.

Registration refuses a tool that cannot be described: no purpose, no use-when,
no channel, an argument with no description or no type, a required argument
the tool does not take. All of it is findable at startup, and finding it out
instead from a wrong answer costs far more.

The schema is a typed structure that renders itself as JSON Schema, rather
than a raw string. A malformed schema then cannot be written, and a test can
walk the properties to check every one is described.

### The channel is checked twice, and the refusal is spoken

`For` decides which tools the model is told about; `Call` decides which
actually run. That looks like the same check twice and is not: the first is a
prompt, and a prompt is not a boundary. A model that has been told wrongly,
that invents a name, or that repeats a call from earlier in a conversation
that arrived by another channel, is stopped by the second.

A refused tool says so rather than quietly doing nothing. Told nothing, a
model invents a reason for the failure, and it invents a plausible one. The
same is true of a name that does not exist, which is answered with the names
that do, so the model can correct itself instead of guessing again.

### A message is words, tool calls or tool results, and never two

A turn that calls a tool produces two more messages: the assistant asking for
it, and the answer coming back. Both are kept. A model given a result without
the call that produced it cannot tell what it looked up from what it guessed,
and a person reading the transcript cannot tell what was done on their behalf.

A message carries exactly one of the three. Carrying none is nothing worth
storing; carrying two makes a transcript that says one thing while the model
reads another, since an assistant that explains itself and acts in the same
breath gives the person words that may not describe what happened. Ulaa draws
the same line with `content?: never`.

An outcome is `ok`, `failed` or `partial`. Three rather than two, because the
middle one is where an assistant starts bluffing: four lights asked for, three
turned off, one unreachable. Told only success or failure a model reports
either "done" or "nothing happened", and both are untrue. A failure carries
the real error verbatim, for the same reason a provider failure does.

Two places already knew how to mangle this and had to be taught not to.
`ForModel` dropped any message with no words in it, which is every tool call.
It also joined consecutive messages from the same speaker, which would have
run a call into the prose beside it. And a window trimmed by size cuts from
the oldest end, which can take an assistant's tool calls away and leave the
answers behind them: a service rejects a result that answers nothing, so the
window now drops results whose call did not survive.

The payloads are JSON columns rather than tables. They are read and written
whole and never queried into, so a row per argument would buy nothing and cost
a join on every turn.

### A conversation names itself, and says so out loud

Every conversation was untitled, which made the listing useless and made
switching by voice impossible: there was nothing to say and nothing to match
against. So after the first complete exchange the model is asked for a label
of two to four words, once, and never over a name somebody chose by hand.

A model asked for a bare label decorates it anyway -- quotation marks, a full
stop, "Title:" in front. That is stripped rather than asked for again. An
answer that arrives as a sentence is refused outright: no name reads better in
a listing than an apology does, and it will not be asked twice.

Naming from the question alone was rejected. It labels a subject nobody has
answered yet, and that is often not what the conversation turns out to be
about.

### Announcing interrupts, so it waits

The server could only ever reply: something arrives, something goes back.
Speaking first is a separate capability and is built as one, `internal/
announce`, silent unless Home Assistant is configured. Naming a conversation
is its first use and the least of them; a timer that has finished is the same
machinery.

`assist_satellite.announce` interrupts. Home Assistant drops whatever the
satellite is playing and says the new thing instead, so an announcement
following an answer cuts the answer off part-way. It was found the way such
things are: a recitation stopped dead so the assistant could say what it had
named the conversation.

The satellite reports what it is doing, and `TTS_START` leaves it responding
until the audio finishes, so announcing now waits for idle first. A satellite
that never falls quiet is left alone and the announcement is dropped, which is
the right outcome for anything incidental.

Idle is not the end, though. The state flips when the satellite stops feeding
the speaker, so an announcement sent the moment it reads idle lands on the
tail of the last sentence and the two run together as one. It now waits a
further second of quiet, and reads the state again afterwards: speech
starting during that second means the gap never happened, so the wait begins
again. A held second is the difference between two things said and one long
one.

`Settle`, defaulting to `DefaultSettle`. Negative restores announcing on the
first idle reading, which is what every test that is not about the pause
passes -- a suite that spends a second a case proving something else stops
being run.

### A request says why it is being made

`Purpose` marks a request as the person's question, or as the assistant's own
housekeeping. Nothing downstream could tell them apart, which surfaced as two
tests asserting on "the last prompt the model was given" and getting the
naming prompt that arrived after the answer. It is also how housekeeping could
one day go to a cheaper model than the one answering.

### The manner is held in memory and stored beside it

It is read on every prompt, so it lives in memory; it is expected to still be
chosen tomorrow, so it is written to a table. The in-memory copy is a cache of
a stored choice, not the choice itself.

A table of name and value rather than a column. There is no row the manner
belongs to: not the user's, since the same assistant answers whoever asks, and
not the client's, since it is the same assistant at every one of them. Which
model answers is the opposite case and stays on the client, because a spoken
answer has to arrive before the satellite gives up waiting.

Configuration gives the manner to start in the first time; once something has
been chosen the stored choice wins. A database that will not answer at startup
is not a reason to refuse to start, so the configured manner is used and
logged.

The write happens after the change is applied, not before. What answers is the
copy in memory, and a failed write should not leave the assistant speaking in
a manner nobody chose — it costs the choice its permanence and nothing else,
so it is logged rather than returned.

### A manner is behaviour, not adjectives

The personas are written from what the films show rather than from the words
usually attached to the characters. A model told to be witty is insufferable;
one told to report a disaster as a mild inconvenience is funny by itself. So
the instruction is never "be dry" but "understatement is the whole of your
humour", and never "be helpful" but "you do not refuse, and you do not
lecture".

The most widely copied Jarvis prompt online would break this assistant. It
asks for bullet points, a polite greeting, anticipated follow-up questions and
step-by-step reasoning. Read aloud, the first three are noise and the third
ends on a question mark, which is the character Home Assistant reads to decide
whether to keep the microphone open.

So the spoken rules are appended after the manner and say they win: where a
persona would ask permission it states its intent instead. Jarvis asked to
deploy something untested answers "That is unwise, sir, and I would run it
against a staging environment first. I will prepare the deployment as
directed" — disapproving, not refusing, and not ending on a question.

"Address the person as sir, at most once and often not at all" was read as
permission never to say it, and neither character used its form of address at
all. It is now a habit with a bound rather than a licence.

Each persona is also told not to act out a role and never to name the films,
which is what keeps a manner from becoming an impression of one.

### Package names follow the reference implementation

`catalog` becomes `llm` and `provider` becomes `environment`, matching the
services in the assistant this one borrows its model from. An `Environment` is
one configured place to send a prompt: an endpoint, its credentials, the wire
format it speaks and the model behind it.

`failure` stays as it is. Go will not have a package called `error`: importing
one binds that name in file scope, which shadows the predeclared type, and
every file that imports it then fails on `func f() error` with "error is not a
type". Aliasing each import would work and would mean the package is never
called by its own name anywhere, which is not a rename.

The `[provider]` section of the configuration keeps its name. Renaming a key
in a file the server reads does not fail loudly: the section is simply not
found, and the server falls back to the stub without an answer as to why.

### Two endpoints, two shapes of failure

A failure carries the exact error so that the model can be asked what went
wrong and answer truthfully. That only works if the exact error survives, and
for one of the two endpoints it did not.

The chat endpoint nests its message, `{"error": {"message": ...}}`. The token
endpoint puts a string where the other puts an object,
`{"error": "Access Denied", "error_description": ...}`. Reading only the first
shape made the unmarshal fail on the type, which was taken to mean there was
no message, and the conversation recorded "token response was not usable"
instead of "You have made too many requests continuously."

One of those tells a person to wait and the other tells them nothing. Both
shapes are read now.

### A vendor an endpoint speaks to is not a model it routes

### The settings page says what it means

"Server default" named nothing, so the listing carries the identifier of the
model that answers when a client has chosen none, and the field reads as that
model's name.

The channel and the model are the same kind of choice — one of a short list,
worth reading before picking — so they are the same control, each under a
label. Two bare words side by side said neither what they were nor that they
could be changed. A client cannot raise its own channel, so on the current
client that field is locked with the reason in the menu rather than offered
and then refused.

"this one" becomes "you are signed in here".

The model picker first offered every model whose vendor the endpoint reaches.
That inference is wrong, and wrong in the direction that breaks things: this
endpoint talks to Anthropic and refuses `claude-haiku-4-5`, while answering
happily to `claude-haiku-4-5-20251001`, which is the same model under its
dated name. It rejects Google as a vendor outright. Choosing a refused model
left that client failing every prompt afterwards, which is how it was found —
by breaking voice.

So the endpoint names the models it answers with, one by one, and the
catalogue says what each one holds. Neither list knows the other; they are
joined on vendor and identifier, and a test fails if a routed model is not
catalogued, because the join would otherwise drop it in silence.

Every entry was verified by asking the endpoint through the real provider
code. Doing that with curl gave 401s, since a handmade request is missing
whatever the client sends; driving the provider itself was both easier and
the only way to test what actually runs. One model reported rejected on the
first attempt and answered on the second, the first failure having been in
minting the token rather than in the model. One probe is not proof.

### A session is a conversation

Migration 00007 renamed conversations to sessions. This renames them back,
because the word has since been taken: everywhere else in this field a session
is one exchange with a service, a single call and its reply. Ulaa's
`SessionContext` is exactly that. What this holds is a thread of talk that
outlives any number of those.

It went all the way: the package, the types, the tables, the columns, the
indexes, the routes, and the `sess_` prefix on every stored identifier. A
conversation identified by `sess_` would be the kind of detail that is
puzzling a year later, and both prefixes are five characters so nothing had to
be widened.

Two words were not ours and had to be put back after the rewrite:
`@@session.time_zone`, which is MySQL's, and `session_token` in the log
redaction list, which is the web's. The second would have stopped a real
session token being redacted, so the rename would have quietly removed a
protection. Sweeping a word out of a codebase catches the places it means
something else, and those are found by reading the diff rather than by the
compiler.

The Down blocks of every migration here are documentation: nothing runs them
and no test exercises them. This one is the most involved of them, since it
drops constraints, renames, rewrites identifiers and rebuilds indexes.

### A client chooses its model

The model was configuration, so everything got the same one. What suits one
client does not suit another: a spoken answer has to reach the satellite
before it stops waiting, while a browser can wait for something slower and
better.

Per client rather than per account, because the client is the thing the
difference belongs to, and because it is already loaded on every request to
authenticate it. Per session was the alternative and is more flexible, but
voice creates sessions implicitly and would always land on a default.

The choice is copied onto the chat when it is accepted rather than read back
from the client when it runs. A chat recovered after a restart then goes to
the model it was accepted for, and a listing says which model answered rather
than which one that client would use today.

A client may set its own model, unlike its channel. The channel decides what
the assistant may do about a prompt, so a client raising it would grant itself
privileges; which model answers grants nothing, and the client you are sitting
at is the obvious place to change it from.

Only models the configured provider can reach are offered, and setting one it
cannot is refused. A model that cannot be called is a setting that breaks
every later prompt from that client, and it would break them at the moment
somebody speaks rather than at the moment the mistake is made.

### Each ceiling comes from whatever knows it

The three limits are declared in three places, because three different things
know them:

| ceiling | declared by | why there |
| --- | --- | --- |
| 100 messages | `platformai.MaxMessages` | a property of that endpoint's array |
| context window | `internal/catalog` | a property of the model, not the endpoint |
| 60,000 bytes | `session.DefaultBudget` | what we are willing to spend |

The catalogue is a list of models with what is known about each. The same
model reached through two endpoints has the same window, and two models behind
one endpoint do not, so the window cannot live with the provider. A model
missing from the list declares nothing and is logged; the byte budget then
governs alone, which is the safe direction.

Condensing starts at ninety-five percent of whichever ceiling is nearest and
leaves forty-five messages as they were said. The gap left above the trigger
is room for condensing to fail two or three times without overrunning.

`Plan` caps the history at one below the message limit. The prompt is added to
the array after the history is chosen, so a history filled to the limit is one
message too many on the wire — which is a failure the service reports as
`ARRAY_SIZE_OUT_OF_RANGE` and nothing here would have explained.

In practice the count is what binds. A spoken turn is a few dozen bytes, so a
hundred of them reach the message limit at a twentieth of the byte budget.

### A stopped turn is drawn as stopped

The server records an interruption as a message of its own and leaves any
partial answer beside it. The client rendered neither: it drew the answer, and
a cancelled chat has no answer and no error, so the turn came out as an empty
bubble under the word Assistant.

A turn whose status is cancelled now says so. Whatever was said before the
stop is still shown, with the note under it; a turn stopped before it said
anything shows the note alone. Nothing is hidden, because a turn that happened
should read as having happened.

### The summary is the session's, and the transcript is untouched

The condensation lives on the session as `summary` and
`summarised_through_seq`, not as a message. Nobody said it, so putting it in
the transcript would mean hiding it from the person, giving it a position in
a sequence it was never spoken in, and superseding one message with another
every time it is rewritten. As a column it is plainly derived: droppable,
rebuildable, and it leaves `messages` a record of what was actually said.

Condensing runs after the turn is recorded and announced, never before the
next one. The person is waiting on the answer, not on the housekeeping, and on
voice a summariser in front of the reply would be heard as the assistant
hanging. It still holds the runner's slot, so it competes with nothing.

Every failure in it is logged and dropped. A summary that could not be written
costs the next turn its oldest context and nothing else, and the turn after
tries again.

The provider is given the summary as `Request.Summary`, not as a leading
`Turn`. Where it belongs in a request is the provider's business: this one
has a field for the system prompt and it goes there, while a service without
one would render it as a leading system message.

### Three ceilings, and condensing before any of them is reached

A long conversation used to lose its early half in silence: the history was
held to sixty thousand bytes and the oldest messages fell off the front.
Nothing broke, which was the problem — the assistant simply forgot the
morning.

Three separate things cap a history, and they come from three places:

| ceiling | belongs to | who knows it |
| --- | --- | --- |
| message count | the wire format | the adapter for that service |
| context window | the model behind the service | the provider |
| byte budget | us | configuration |

They are kept as one `session.Limits` and all of them hold at once. The count
is what Ulaa hard-codes as `slice(-100)` inside its platform-AI format; ours
stays a declared number so a service without that cap is not held to someone
else's. The context window is converted to bytes at four bytes a token, which
is an approximation the reserve is sized to absorb; counting exactly needs the
model's own tokeniser, and being wrong by a little is what the reserve is for.

`Due` reports when the earliest part should be condensed — at ninety percent
of whichever ceiling is nearest, not when one is hit. `Plan` then sends the
summary in place of what it covers. The boundary between them is moved back to
the start of a turn, so a question is never condensed apart from its answer;
once a tool call and its result are messages, the same rule is what keeps a
result from being sent without the call that asked for it.

Ulaa clips instead and repairs the damage afterwards — dropping messages until
the first is a user's, collapsing the same speaker twice in a row, choosing
between two adjacent tool results by matching ids against the last tool call.
Choosing the boundary correctly is cheaper than mending it.

### A message has an identifier, and a position

`(session_id, seq)` was the key. It is unique and stable while the table is
only appended to, and stops being an identity the moment anything edits or
removes a message part-way through a session: seq is a position, and positions
move. Editing a turn and re-running from it is why this is wanted, and the
reference has to survive it.

So `id` names the message and `seq` orders it, unique within a session. The
constructors set the identifier; `Append` fills one in when a message was built
as a literal, before validating, so a caller does not have to know which fields
the store supplies.

Rows written before the column got a synthesised identifier rather than a real
ULID, because SQL cannot make one. SHA2 is hexadecimal and every hex character
is in the Crockford alphabet a ULID uses, so they are the right shape and
stable per message; they are not sortable by time, and nothing relies on that
because ordering is `seq`.

### A stopped turn is marked, not erased

Saying "stop" cancels the turn. What stays behind is the question, whatever
the assistant managed to say, and a message recording that the person stopped
it — `session.Interruption`, written as the assistant's own turn so the roles
still alternate.

Deleting the question instead would be simpler, and while a turn is only ever
text it would also be harmless: nothing happened, so nothing is lost. That
stops being true the moment a turn can act. Half a chain of tool calls may
already have run and left its effects behind, and a history with the request
removed leaves the model contradicting a world it changed.

So this is the one thing the model is told about that a `Failure` never is.
A failure read back as conversation becomes the model explaining an outage it
had no part in; an interruption read back is a fact about the conversation
that the next turn needs.

### Comments

Follow Go doc comment convention.

- Every exported type, function, constant, method and non-obvious struct field
  carries a doc comment. So does an unexported one whose purpose is not
  evident from its name.
- A doc comment begins with the identifier's name, then a colon, then the
  description:

  ```go
  // Open : Connects to MySQL, configures the pool and verifies the
  // connection. The caller must Close the returned DB.
  func Open(...) (*DB, error)

  // DB : An open database handle wrapping a *gorm.DB.
  type DB struct {
      // sqlDB : The underlying pool, kept for Ping and Stats.
      sqlDB *sql.DB
  }
  ```

  The word after the colon is capitalised, and the text is a description in
  its own right rather than a continuation of a sentence begun by the name.
  Write `// Redacted : The placeholder substituted for a redacted value.`, not
  `// Redacted : is the placeholder...`.
- Comments describe what the code is and does. They do not narrate how a
  decision was reached, what was considered and rejected, or what was asked
  for. That history belongs in commit messages and in this file, where it can
  be looked up deliberately.
- Keep them short. A one-line `// why` above a genuinely non-obvious statement
  earns its place; a paragraph of reasoning does not.

The reader of a comment has the file in front of them and none of the
surrounding session.

### Time

UTC everywhere — in Go, in MySQL sessions, in stored columns. The development
machine is in IST, so a bug here will not be visible locally until it is
visible in production.

### Identifiers

Prefer ULIDs over random UUIDs for anything time-ordered. They sort by creation
time, so history comes back ordered from an index scan without a sort.

---

## 6. Chats

The design agreed before any of it was built. Not yet implemented.

### Shape of the API

A request is a chat. Submitting one returns immediately; the work continues in
the background.

```
POST /v1/chats            {"prompt": "..."}   -> 202, the chat with id and status
POST /v1/chats?wait=30s                       -> holds the connection up to 30s,
                                                 returning the finished chat if it
                                                 lands in time, else the pending one
GET  /v1/chats/{id}                           -> the chat, with its response once done
GET  /v1/chats                                -> recent chats, without response bodies
POST /v1/chats/{id}/cancel                    -> stops a chat that has not finished
```

`wait` is a convenience for testing by hand, not a second execution mode. The
chat is created and run the same way either way.

### States

```
pending ──→ running ──→ completed
   │           ├──────→ failed
   └───────────┴──────→ cancelled
```

`completed`, `failed` and `cancelled` are terminal; nothing moves a chat out
of them. `waiting_approval` joins this set when tools need permission.

### Model

Implemented in `internal/chat`.

```go
type Chat struct {
    ID     string   // "chat_" + ULID
    Prompt string

    Status   Status
    Response string  // set when completed
    Error    string  // set when failed, written for a user to read

    CreatedAt  time.Time
    UpdatedAt  time.Time
    StartedAt  *time.Time  // nil until it runs
    FinishedAt *time.Time  // nil until terminal
}
```

`StartedAt` and `FinishedAt` are pointers because "has not started" is a
different fact from "started at the zero time".

```sql
CREATE TABLE chats (
  id          CHAR(31)    NOT NULL,   -- 'chat_' + 26-char ULID
  prompt      TEXT        NOT NULL,
  status      VARCHAR(20) NOT NULL,
  response    MEDIUMTEXT  NULL,
  error       TEXT        NULL,
  created_at  DATETIME(3) NOT NULL,
  updated_at  DATETIME(3) NOT NULL,
  started_at  DATETIME(3) NULL,
  finished_at DATETIME(3) NULL,
  PRIMARY KEY (id),
  KEY idx_chats_status_created (status, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

`DATETIME(3)` keeps milliseconds, which plain `DATETIME` truncates away.

Timestamps set by the domain are truncated to `chat.StoredPrecision`, which is
one millisecond, before they are stored. Go clocks to the nanosecond and MySQL
rounds a `DATETIME(3)` to the millisecond, so without this a chat in memory
stops matching the row just written from it, and every later comparison between
the two is quietly wrong. Truncating rather than rounding means the value the
domain holds is exactly the value that will be stored. The cost is that a chat
beginning and finishing within the same millisecond reports no duration, which
no real chat does. The
identifier is stored with its prefix and readable, rather than as a `BINARY(16)`
ULID, so the table can be read directly during development. A ULID primary key
already orders by creation time, so listing needs no sort. The secondary index
serves the runner's query, oldest pending first.

Deliberately absent until something needs them: `user_id`, the model used,
token counts, retry counts.

### Users, clients, sessions, chats

```
user                    the person the assistant belongs to
  |
  +-- clients           chrome, postman, a command line, a phone, a speaker
  |                     a token and nothing else: owns nothing,
  |                     but holds which session IT is in
  |
  +-- sessions          owned by the person, reachable from any client
        |
        +-- chats       a prompt is a chat
              |
              +-- messages
```

**A client is one logged-in thing, not a piece of hardware.** A laptop running
a browser, Postman and a command line is three clients. That is the useful
unit: each has its own token, so Postman's can be revoked without disturbing
the browser, and each has its own active session, so testing in one does not
interrupt a conversation in the other.

**Sessions belong to the user, not the client.** An exchange begun on a phone
continues at a desk. Every client sees every session.

**The active session belongs to the client.** A person may be speaking to a
speaker in one room while typing at a laptop in another, and those threads
must not collide. Switching on one client leaves the others where they were.
Which sessions exist is a property of the person; which one a client is
currently in is a property of the client.

**The user is the boundary.** Every ownership check asks whether it is the
same user, never the same client. A client is only which credential was
presented.

Naming a session on a single prompt sends it there without switching what the
client is in.

### Authentication

A client authenticates with a token, sent as `Authorization: Bearer fri_...`.
The token both names the client and proves it, so nothing is sent alongside:
an identifier presentable without the token would be a name with no password.

**Logging in and registering a client are one act.** `POST /v1/auth/login`
takes a username, a password and a client name, and returns a token. A token
exists only for a client, and a client may be created only by someone who
proved who they are, so there is nothing to register separately and no
registration secret to share around.

The token is returned once and never again, because only its SHA-256 is
stored. A plain hash rather than a password hash, since the token is 256 bits
of randomness: there is no dictionary to try, and bcrypt on every request
would cost a hundred milliseconds to defend against nothing.

**Passwords are bcrypt**, at a cost chosen so one check takes roughly two
hundred milliseconds. A password is chosen by a person and therefore
guessable, so here the slowness is precisely the point. `auth.PasswordCost`
exists so tests can lower it; production must leave it alone.

An unknown username spends the same time as a wrong password, through
`DummyPasswordCheck`. Answering faster for an account that does not exist
tells whoever is guessing which usernames are real.

`DELETE /v1/clients/{id}` revokes any of the caller's own clients, which is
the reason clients exist apart from the user: a phone left in a taxi is
revoked from the laptop at home. The row is kept rather than deleted, so a
revocation is visible in a listing.

Every refusal reads alike and answers 401. A token never issued and one since
revoked are logged apart, so a problem stays diagnosable, but answered
identically, so neither is discoverable.

**The first user is created from the terminal**, with `personal-assistant createuser
<username>`. There is no endpoint. One that creates the first user must either
be open, which lets a stranger claim the assistant, or be guarded by a shared
secret, which is the same problem one level up. A command run by whoever
already has the machine avoids both and is needed exactly once.

**A bearer token is only as private as the connection carrying it.** Over
plain HTTP anyone on the network reads it and becomes that client. TLS must
sit in front of the assistant before it is reachable from anywhere but the machine it
runs on.

### Sessions

A chat belongs to a session, and the provider is given what was said
earlier in it. Without that, a second prompt arrives with nothing before it:
"no, make it four" reached the model with nothing to make four, and it said so.

`POST /v1/chats` creates a session when the caller names none, and returns
its identifier so a follow-up can continue it.

**A new prompt supersedes whatever is still running in that session.**
Someone who speaks over an answer wants the new thing rather than both, and two
answers cannot be listened to at once. The superseded chat is cancelled, not
deleted: everything it said is still stored, it is simply never spoken.

**A cancelled chat still contributes its prompt to history.** This is what makes
a correction work at all. The question being corrected was cancelled the instant
the correction arrived, so a history of only completed chats would omit the very
thing the correction refers to.

Consecutive turns by the same speaker are joined into one. A cancelled chat
contributes a prompt with no answer, so two questions can end up adjacent, and
models that require alternating roles reject that. Joining also reads correctly:
a question followed by its correction becomes a single request.

### A chat, not a task

The owner's words: *"i mena tere shoudn ot be any task word..if i ask its a
chat..for a chat i get response"*. Migration `00009` renames `tasks` to
`chats`, `internal/task` is `internal/chat`, `/v1/tasks` is `/v1/chats`, and
identifiers begin `chat_` instead of `task_` — the same length, so no column
changed width.

`task_messages` became **`chat_updates`**, not `chat_messages`. `messages` is
now the conversation itself, and two tables a letter apart holding entirely
different things is a trap worth spending a better name on.

One caveat was raised before the rename and overruled, and it is recorded
because it will come due. A chat is one prompt through to one answer, which
today is exactly one call to the provider — but Ulaa's `runLLMSession` makes
*several* `/chat` calls when tools are involved, looping until the model
answers with text instead of a tool call. When the assistant grows tools, one chat
will contain several chat calls. The outer thing is what carries the status,
the cancel, the supersede rule and the stream; the inner one is
`platformai.attemptChat` and is not stored.

Two constraint names still predate all this — `fk_devices_user` and
`fk_conversations_user`, from before clients and sessions were renamed. They
are invisible except in `information_schema` and were left alone rather than
widening this change.

### The conversation is a log, not a by-product

Until now a session's history was read back out of the `chats` table: one turn
from the `prompt` column, one from `response`. That works only while every
exchange is exactly one question and one answer. There was nowhere to put a
failure the user had already seen, and nowhere for the shape to grow.

The owner asked for the shape used in `ulaa-ai-assistant`
(`src/background/services/conversation`), having read it: *"see how llms
adapters, conerstation are naintainer.,tht how i wnat my derver to be"*. Three
things were taken from it and one was left behind.

**Taken: messages are the stored unit.** `internal/session` holds a `Message`
with a `Role` and a `Kind`, and the `messages` table is a log per session with
a position assigned on insert. Chats remain the unit of work; they are no
longer the unit of record.

**Taken: one store, two views.** `ForModel` is what a provider is given,
`ForPerson` is what a person is shown. They differ today only in that failures
are withheld from the model, but they are separate functions because they
answer separate questions, and only one may change when a provider demands
something.

**Taken: a failure is a message.** the assistant could not answer, the person watched
it happen, and their next sentence refers to it — so it is in the log and on
the screen. It is never sent to a model: read back as conversation it becomes
the model explaining an outage it had no part in, and inventing detail to fill
the gap.

**The exact error is never said, on any channel.** Owner's rule, 28
September 2026, on hearing the assistant read out *"platformai:
requesting token: Post https://accounts.zoho.com/oauth/v2/token: dial
tcp: lookup accounts.zoho.com on 127.0.0.53:53: server misbehaving"*.
What they want is *"Sir, I am having some difficulty reaching the
server"*, with the error kept and given if they ask for it.

The voice channel used to append the detail, on the reasoning that
there is no "more info" on a speaker so withholding it left the person
with nowhere to go. That reasoning was wrong in its premise: asking is
where they go, and asking already worked. `ForModel` renders a failure
with `[Exact error, for reference if asked: ...]`, so *"what went
wrong"* is answered from the conversation rather than guessed at. The
sentence is now the whole of what is said, typed or spoken.

**A failure that threw its cause away.** The environment failing to
start logged the error and called `Fail` with a bare sentence, so the
one thing the person might ask about was not in the conversation at
all. It carries the cause as detail now. Worth checking any other
`Fail` for the same shape.

**Failures speak in the persona's voice.** They are the one reply the
model has no hand in, and they were landing in flat server English in
the middle of a butler's conversation. `persona.Addressed` adds the
address once, and the sentences were rewritten in the first person and
about "the server" rather than "the service". The helper moved out of
`internal/tool`, which had the only copy, so there is one.

**Left behind: tool calls.** The owner's instruction was *"don think about
tool cals now"*, so there is no `tool` role and no `tool_calls` column. Adding
them later is a migration, which is the right price for not guessing their
shape now.

**Left behind: the vocabulary of chats.** Also the owner's: *"actually no need
for the term chat"*. The log is `internal/session`, keyed by session, with no
`chat_id` column — a message does not record which request produced it. The
existing `chats` table, `/v1/chats` and the runner keep their names; whether
those are renamed too is still open.

**How a question stays out of its own history.** The runner appends the
question, gets its position back, and reads everything *before* that position.
There is no identifier to exclude and no ordering to get right — the question
cannot reach the model twice by construction.
`TestTheQuestionIsNotAlsoInItsOwnHistory` fails without it; checked by
breaking it.

**Positions are assigned by reading the maximum and retrying.** Two appends to
one session can choose the same position; the loser is refused by the primary
key and takes the one the winner just claimed.
`TestConcurrentAppendsGetDistinctPositions` runs five at once against real
MySQL.

Migration `00008` carries existing history across, so upgrading a running
The assistant loses nothing: each finished chat contributes its question, then the
answer or the failure it ended in, ordered by identifier — a ULID, so by time.
On the development database that turned 511 chats into 688 messages.

**The whole session is sent.** The owner's words: *"i dont think the full
conversation is sent to platform ai..it shoudl include all messages ..mine
fridya's"*. A person expects an assistant to remember what they said this
morning.

What replaced the old twenty-turn cap is a byte budget,
`session.DefaultBudget`, sixty thousand bytes — roughly fifteen
thousand tokens, a long day of talking. A budget rather than a turn count
because what costs money and eventually exceeds the model's context is the
text, not the number of times the speaker changed. The oldest turns are dropped
first; a single turn longer than the whole budget is cut rather than dropped,
since dropping it would leave the model answering about a subject it never saw.

**No time is sent with a message, and this was tried.** The same request asked
for the date and time of every message, so each one went out as
`[Mon 22 Sep 2026, 2:32 pm IST] what was said`, with a paragraph appended to
the system prompt explaining that the brackets were context and were never to
be read back.

The model read them back. An answer about Iron Man arrived beginning
`[Tue 22 Sep 2026, 4:17 pm IST] Iron Man is one of the most iconic
superheroes...` — the convention was demonstrated on every single message, and
demonstration beats instruction. The owner's ruling: *"time shou niot be sent
to ai"*.

So nothing decorates a message on its way out: `provider.Turn` is a role and
text, and the system prompt is exactly what was configured.
`TestMessagesReachTheModelUnadorned` and the `[`-prefix check in
`TestHistoryCarriesBothSpeakers` are what keep it that way.

Times are still recorded — `messages.created_at`, in UTC — because they are
worth having and cost nothing. They just do not reach a model. Anything that
wants to answer a question about *when* will have to put the time somewhere a
model cannot mistake for a pattern to copy, which is a different design and
not this one.

### Running a chat

`internal/runner` owns execution. Two details of it are load-bearing.

A chat's lifetime is **not** derived from the request that submitted it. An
HTTP request's context ends when its response is sent, which would cancel the
chat at the moment the caller was told it had started. The runner holds its own
context instead.

The final write uses a context that **outlives the run**, via
`context.WithoutCancel`. Recording that a chat was cancelled is itself a
database write, and a cancelled context cannot make one, so a naive
implementation leaves cancelled chats stuck reading `running` forever.

Concurrency is capped. A chat waits for a slot before it starts, so a queued
chat stays `pending` rather than appearing to run while it waits.

A transient message that cannot be stored is logged and the run continues. The
answer is what matters; losing a line of progress is not worth discarding it.

### Consequences of chats being long-running

- **Startup recovers orphans.** A process that dies mid-chat leaves a row
  reading `running` that nothing will ever move. At startup every `running`
  chat is failed with an explanation, which is exact while the assistant is a single
  process. More than one process would instead need a `heartbeat_at` column
  and a reaper for stale rows.
- **A chat has a deadline.** Past a maximum duration the runner cancels it and
  records the failure, so a wedged call cannot occupy a slot indefinitely.

### Consequences of responses being large

- **Reads come in two shapes.** One selects the small columns, for listing and
  for polling; one selects everything, for fetching a finished result. A plain
  `SELECT *` through GORM would drag every response body along with it.
- **`response` holds the final answer only.** Intermediate steps, tool calls
  and progress belong in a separate table, or the column becomes a transcript
  that grows and is paid for on every read.

### Providers

A chat is carried out by a provider: Claude, GPT, or another. Which one runs a
given chat is a routing decision; the chat does not care.

A provider run is a stream. It yields zero or more transient messages, then
exactly one final message or one error, then ends.

```
pending ──→ provider running ──────────────────────────→ ended
                 "Checking your merge requests…"    update
                 "Found 4, reading the diffs…"      update
                 "Here is what I found: …"          final
```

```go
// Kind : Whether a message is progress, the result, or a failure.
type Kind string

const (
    KindUpdate Kind = "update"  // transient; more will follow
    KindFinal  Kind = "final"   // the result; the stream ends
    KindError  Kind = "error"   // the run failed; the stream ends
)

type Message struct {
    Kind Kind
    Text string
    At   time.Time
}

type Provider interface {
    Name() string
    Run(ctx context.Context, req Request) (<-chan Message, error)
}
```

The contract is part of the interface: the provider owns the channel and
closes it, a stream ends after exactly one `final` or one `error`, and
cancelling the context ends the run. A final message completes the chat; an
error fails it. Cancellation is what will serve both `POST /chats/{id}/cancel`
and interrupting the assistant mid-sentence by voice.

A channel was chosen over an iterator because it is what a Go reader expects
and selects naturally against cancellation. The cost is that a caller must
drain the stream or cancel the context, or the provider's goroutine leaks;
that obligation belongs in the doc comment on Run.

The provider's own running and ended states are the lifetime of the stream and
are not stored. The chat's `running` and terminal statuses already record it.

### Transient messages are stored

They are not merely streamed. A client that reconnects mid-chat can catch up,
a finished chat can be asked what it said while working, and a poor answer can
be examined step by step. Streamed and forgotten, they are gone whenever
nobody happens to be listening, which with a voice client is most of the time.

```sql
CREATE TABLE chat_messages (
  chat_id    CHAR(31)    NOT NULL,
  seq        INT         NOT NULL,
  kind       VARCHAR(16) NOT NULL,
  text       MEDIUMTEXT  NOT NULL,
  created_at DATETIME(3) NOT NULL,
  PRIMARY KEY (chat_id, seq),
  CONSTRAINT fk_chat_messages_chat
    FOREIGN KEY (chat_id) REFERENCES chats(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

The primary key orders a chat's messages and stores them together. The final
message is **not** duplicated here: it lives in `chats.response`, and an error
in `chats.error`, so a large answer is stored once. Replaying a run means
reading the messages and then the chat's own result.

`kind` is kept even though only updates are written today, because tool calls
and their results will be recorded the same way.

### Streaming to a client

`GET /v1/chats/{id}/stream` sends server-sent events. Every message a client
receives is one whole sentence, ready to be spoken.

The handler subscribes to the bus **before** reading the chat. Reading first
leaves a gap in which a chat can finish unheard, and the client then waits for
a message that was already sent. Having subscribed, it replays the messages
already stored, sends the outcome and closes if the chat has finished, and
otherwise follows the live stream, skipping anything it already replayed.

A client reconnecting sends `Last-Event-ID` and resumes from there, so a phone
on mobile data does not hear the same sentence twice.

Publishing never blocks. A client that has stopped reading loses events rather
than delaying the chat producing them; the database holds the durable record
either way. Comments are sent on an idle stream, because proxies and mobile
networks close a silent connection and a real agent will think for minutes
without speaking.

### The satellite is linux-voice-assistant, not wyoming-satellite

**A turn that ends says so.** Owner's request, 29 September 2026: a
small sound when it goes back to idle. Every other sound the satellite
makes marks something starting -- the wake chime rises, the processing
sound holds -- and nothing marked the end. A turn that produced no
speech, because nothing was heard or there was nothing to say, ended
in a silence identical to the satellite never having woken. The only
way to find out whether it was still listening was to keep talking.

Not a rare case: 23 `stt-no-text-recognized` events in the log for one
day, each one a turn that ended with nobody told.

`_play_idle_sound` hangs off `_tts_finished`, which is the single
point every turn passes through -- a spoken answer reaches it as the
TTS done_callback, and a silent turn reaches it from `RUN_END` when
`_tts_played` is false. One hook covers both.

Two things it must not do. It plays with no `done_callback`, because a
sound played with one reaches `_tts_finished`, which is what plays it.
And it is skipped while `_stopping`, because `stop()` calls
`tts_player.stop()`, that reaches `_tts_finished` too, and the stop
already has a sound of its own -- two sounds one after the other say
less than either alone.

`sounds/make-idle-sound.py` generates it: one tone, G4, 55ms, at half
the amplitude of the stop sound. Single so it cannot be mistaken for
the stop sound's two falling notes, and quiet because it lands in the
silence after every single turn.



The microphone in front of Home Assistant is OHF-Voice's
`linux-voice-assistant`. It is the ESPHome satellite implementation running on
Linux -- the same code path the Home Assistant Voice PE hardware uses -- and it
reaches Home Assistant over the ESPHome API rather than Wyoming.

None of it lives in this repository. The satellite, the patches it and Home
Assistant need, and the switch that turns the stack on and off are tracked
separately in [voice-setup](https://github.com/DhanushRamesh/voice-setup),
cloned at `~/voice-setup`. That repository records which upstream versions the
patches were written against, because both edit upstream source by matching
exact text.

It replaced `wyoming-satellite`, which has since been removed. Wyoming needed
two patches inside its virtualenv and four shell scripts to do things this has
as settings: a pre-roll buffer so a wake word and a question can be one
sentence, a detector reset so it does not wake itself after every reply,
chimes, and microphone level management. Its wake word also had to be tuned
by hand, where MicroWakeWord reports 0.996 against a 0.900 threshold on the
same voice and microphone.

None of this improves recognition. Home Assistant's setup offers American and
British English and no Indian English, so speech-to-text remains the
unsolved part.

### Home Assistant talks to the assistant in Ollama's shape

Home Assistant reaches a language model through one of its integrations. Of
those built into it — `openai_conversation`, `anthropic`,
`google_generative_ai_conversation`, `ollama` — **only Ollama's asks for the
address of the server to call.** The others hardcode their vendor's endpoint.
Pointing the OpenAI one at the assistant would need `extended_openai_conversation`
from HACS, which is a community add-on and one more thing to keep working.

So the assistant answers in Ollama's shape, and `internal/api/assist` is that
translation:

```
GET  /api/tags   -> the one model, named "assistant"
POST /api/chat   -> a chat, answered as newline-delimited JSON
```

Both paths are fixed by the caller. The Ollama client appends them to the
address it was configured with, so they cannot be moved under `/v1` with the
rest of the API. `routes_test.go` records them for that reason.

Four things follow from the protocol being someone else's:

**Unknown fields are ignored, not refused.** Home Assistant sends `tools`,
`keep_alive`, `options`, `think` and `format`, all of which describe running a
model on the machine being called. `httpx.DecodeJSON` rejects fields a request
does not define, which is right for the assistant's own API and wrong here: a new
version of Home Assistant sending one more field would stop the assistant answering
at all. `assist` decodes with a plain decoder and says why.

**Authentication is the bearer token that already exists.** The Ollama
integration has an optional API key and sends it as `Authorization: Bearer`,
which is what `authn.Require` already reads. A `fri_` token pasted into that
box is the whole of the setup, and a wrong one gets a 401 that Home Assistant
reports as an authentication failure rather than a broken server.

**Home Assistant's copy of the conversation is discarded.** It sends the whole
exchange on every turn, including its own system prompt. The assistant keeps its own
log and builds a model's history from that, so only the last user turn is
taken. Using both would give one chat two disagreeing accounts of what was
said.

**The answer is streamed, and that is what makes long work possible.** The
connection is held open while the chat runs, so a chat taking two minutes
survives as long as it keeps saying something, and each transient message is
spoken as it arrives. An empty chunk every fifteen seconds keeps a silent chat
from having its connection closed underneath it. This is the same reason
The assistant's own clients are given a stream, and it is why `/api/chat` does not
reuse the 202-and-poll shape of `POST /v1/chats`.

One thing setup depends on: `GET /api/tags` must answer within five seconds,
because that is the timeout Home Assistant applies while validating the
configuration. Nothing slow belongs in it.

**An answer must not end in a question mark.** Home Assistant decides whether
to reopen the microphone from the last character of the reply, in
`conversation/chat_log.py`:

```python
last_msg.content.strip().endswith(("?", ";", "？"))
```

There is no setting for it. A closing "is there anything else?" therefore
leaves the satellite listening and makes the wake word unnecessary, which is
the opposite of how the owner wants to speak to it: the name is said every
time.

**A caller that hangs up has its chat cancelled.** This is the opposite of
the SSE stream, where a dropped connection is a phone on bad mobile data and
the answer has to still be there when it reconnects. Home Assistant never
reconnects: when it drops the request the turn is over, and leaving the chat
running would spend a provider call on an answer nobody can hear. It is also
what makes saying "stop" mid-question worth anything — the satellite abandons
the pipeline, Home Assistant drops the connection, and the assistant stops the work.

Two things guard against it. The system prompt asks for no closing question,
which also shortens replies that are being read aloud. That is a request, not
a guarantee — the model still offers help after a greeting — so
`assist.settled` turns a trailing question mark into a full stop before the
answer is sent. It belongs in `assist` rather than in the provider because it
is a property of Home Assistant's protocol, not of the assistant's answers, and no
other client cares.

### Home Assistant stops listening after fifteen seconds

**The limit is real, hardcoded, and never reported.**
`assist_pipeline/vad.py` gives `VoiceCommandSegmenter.timeout_seconds` a
default of 15.0, and `assist_pipeline/pipeline.py` builds the segmenter
passing `silence_seconds` and nothing else, so the default always
stands. `AudioSettings` has no timeout field, so no pipeline setting,
no satellite control and nothing in the interface reaches it. Measured
1 October 2026 over 39 utterances: 10% ended at exactly 15.0s, with
nothing at all between 9s and 15s -- a gap and then a spike, which is
what truncation looks like and what a natural distribution does not.

Worse than the cut is the silence about it. The segmenter sets
`timed_out`, `_speech_to_text` consumes it, and `STT_END` carries only
`stt_output.text`. So a fragment and a finished sentence arrive
identical, and an assistant given the first half of a shopping list
answers it as though it were the whole.

Nothing downstream can recover the fact. Home Assistant will not carry
it, and it reaches the server as Ollama's wire format, which has
nowhere to put it. The one component that knows is the speech-to-text
bridge, because it is the thing that sends the audio and therefore
measures it. It reports to `POST /v1/speech/cut` **before** handing the
transcript back, so the report is already waiting when the same words
arrive moments later as a question -- there is no race to lose.

`internal/speech` matches on the transcript text rather than on a
timestamp or an identifier. The same string travels bridge -> Home
Assistant -> server, so it matches exactly, and it needs no clock
agreement between two processes. Reports expire after two minutes and
the store is bounded, so a bridge gone wrong cannot grow the process.
An empty transcript is never recorded: a recording can run its full
length and catch no words, and matching that would mark every future
empty question as a fragment.

No text heuristic. Guessing from missing punctuation would be
English-only, and the duration is the better signal anyway.

This is not the same fault as `finished_speaking_detection =
aggressive`, which ends a turn on a pause. One cuts on silence, the
other at a fixed ceiling, and fixing either leaves the other.

A patch making `timeout_seconds` configurable is prepared for
upstream. Until it lands there is no local fix: bind-mounting a
patched `vad.py` would shadow the file after a Home Assistant upgrade
and run an old detector against new pipeline code, which fails quietly
and at the worst moment.

### Platform AI

`internal/provider/platformai` answers using Zoho Platform AI, selected with
`[provider] name = platformai`. It was written from the working client in
`~/workspace/ulaa_defter/product_package/go_src/project_assistant`.

The service is request and response: one call returns one complete answer, with
nothing in between. The assistant's interface streams because a user listening through
earbuds needs to hear something long before the answer arrives, so this
provider produces its own progress: an acknowledgement at once, a reassurance
every fifteen seconds while the call is outstanding, then the reply as the
final message. Silence is the thing to avoid, not a shortage of detail.

The system prompt asks for plain spoken sentences and forbids markdown,
headings and code fences. They are noise when heard rather than read.

Three things about the service are easy to get wrong:

- The system prompt is sent in a field named `context`, not `content`.
- A reply's content arrives either as a plain string or as an array of blocks
  each carrying text, and both must be handled.
- An error envelope carries a message the service wrote to be read, so that
  reaches the user. Anything else — transport, decoding, an HTML error page —
  is reported in general terms, because it means nothing to a listener and can
  carry internal detail.

**Credentials must be scrubbed from transport errors.** The token endpoint
takes the client secret and refresh token as query parameters, and a
`*url.Error` carries the whole URL, so wrapping one as it comes writes the
credentials into the log in plain text. This happened and was fixed. Redaction
in `internal/logging` cannot catch it: that matches attribute keys, and this is
a secret buried inside an error's text. `scrubURL` removes the query string
while keeping the operation, host and cause.

**Use the public endpoints**, `accounts.zoho.com` and `platformai.zoho.com`.
They serve the same paths as the internal ones and are reachable from
anywhere, so the assistant can use this provider from a cloud host. The internal
addresses are reachable only from the corporate network and are not used.

Credentials are realm-specific. The ones issued on the internal accounts
domain are rejected with `invalid_client` against `accounts.zoho.com`; a
Self Client on api-console.zoho.com, with scope
`PlatformAI.organizations.all`, is what works. Certificate verification stays
on, since the public endpoints present ordinary certificates.

Credentials live in `config.ini`, which is git-ignored, or in
`ASSISTANT_PLATFORMAI_CLIENT_SECRET` and `ASSISTANT_PLATFORMAI_REFRESH_TOKEN`.

### A hand-built Transport loses every default

`http.DefaultTransport` sets `IdleConnTimeout` to ninety seconds. A
`&http.Transport{}` written out to set one field gets zero for the rest,
and zero there means keep idle connections for ever. One kept past the
far end's own idle timeout is dead, and the next request down it fails
with EOF before anything can answer.

That is what lost a turn: thirty-five seconds of quiet, then a POST that
ended in eighty-two milliseconds. The first of its kind in the log, which
fits -- it needs the other end to drop a connection during an idle gap.

So the Transport now also sets `IdleConnTimeout`, a dialer keep-alive and
`Proxy: http.ProxyFromEnvironment`. The last changes nothing today, since
no proxy variables are set, and silently bypassing a proxy is not a thing
to leave waiting to be discovered.

That narrows the window rather than closing it: the far end can close a
connection at any instant, and Go will not retry a POST by itself. So a
call that fails with the connection gone is made once more. It is safe
exactly because nothing was asked -- no tool ran, the model was never
reached -- and it is told apart from an answer by the error, since an
answer of any kind, refusal included, means the request arrived. Once
only: a service that is down would otherwise be asked twice a turn, and
double the wait before anybody is told.

### A refused token renews itself

An access token can be refused before it was believed to have expired.
Zoho invalidates one when another is issued for the same client, so
authorising from anywhere else — or a second copy of the assistant running —
revokes ours silently, long before the expiry that was calculated from
`expires_in`.

A 401 therefore drops the cached token, mints a fresh one and repeats the
call. **Once only**: a refresh token that has itself been revoked would
answer every attempt the same way, and retrying for ever would hang the
chat rather than fail it.

**A machine code is never read aloud.** The service's own wording is
passed on because it was written to be read, but `INVALID_OAUTHTOKEN` was
not — it is for whoever runs the server, and means nothing spoken to
somebody waiting for an answer. A message shaped like a code
(SHOUTING_SNAKE_CASE, no spaces) is replaced by one that says what is
actually wrong; a message shaped like a sentence is passed through
unchanged, since it is usually more informative than anything substituted
for it.

### The first provider is a stub

Implemented in `internal/provider`.

It emits a couple of fixed updates and a final message. That makes the whole
pipeline visible end to end with no API key and no network, so the chat
lifecycle, cancellation and streaming can be debugged on their own. A real
provider then replaces it behind the same interface without anything else
changing.

## 7. Configuration

Three layers, each overriding the one before:

```
built-in defaults  <  config.ini  <  environment variables
```

Every key has an environment equivalent named `ASSISTANT_<SECTION>_<KEY>`. This is
how secrets reach a deployed machine without editing files.

- `config.ini` is **git-ignored**. Never commit it.
- `config.example.ini` is committed and must stay in step. A test loads it.
- Adding a setting means adding it to `config.go`, `config.example.ini`, and
  the table in `README.md`. Unknown keys in the file are a startup error, so
  an omission here breaks the example file's test.

---

## 8. Database

MySQL 8. Local development database and user are created by hand; see
`README.md`.

- Connections must set `parseTime=true`, `loc=UTC` and `time_zone='+00:00'`.
  Without these, `DATETIME` scans as `[]byte` and stored times drift.
- MySQL treats `'user'@'localhost'` and `'user'@'127.0.0.1'` as separate
  accounts. Create both for local development.
- Storage goes behind a repository interface so the engine stays swappable.

Migrations live in `internal/storage/migrations` as numbered `.sql` files and
are applied by `goose` used as a library. `embed.FS` compiles them into the
binary, so deployment stays a single file with no directory of SQL to keep
beside it.

`storage.Migrate` runs at startup when `[database] auto_migrate` is true, which
it is by default. goose records what it has applied, so running it on every
start does nothing when there is nothing to do.

Migrating takes an advisory lock, held on a dedicated connection because MySQL
scopes `GET_LOCK` to the connection that took it. Two processes starting
together would otherwise run the same migration at once, and MySQL does not
roll back DDL: the loser finds a table its own `CREATE` never recorded, and
fails on every start thereafter. This was not hypothetical — parallel test
packages sharing one database hit it.

GORM's `AutoMigrate` is **not** used and is not a substitute. It adds tables
and columns but never drops, renames or transforms, so a schema evolved with it
drifts from what a fresh database produces.

Adding a migration means a new numbered file. Existing files are never edited
once applied anywhere, because goose will not reapply them.

---

## 9. Environment notes

Facts about the owner's machine that have already caused confusion:

- MySQL `root` uses socket authentication. `mysql -u root -p` is refused; use
  `sudo mysql`. **`sudo` requires a password an agent does not have** — so any
  step needing root must be handed to the owner to run.
- The machine also runs a PostgreSQL 12 instance with an unrelated `sasdb`
  database. Leave it alone.
- Machine timezone is IST. MySQL's `time_zone` is `SYSTEM`.
- The `mysql` client reports `@@session.time_zone` as `SYSTEM` even when
  the assistant's own connections are UTC, because the client does not set the
  session variable. This is not a fault.

---

## 10. Current state

Keep this honest. An inaccurate status here is worse than none.

### How the pieces fit

Everything below the API layer is built and tested. `✅` is done, `⬜` is not.

```
        spoken                                                   heard
           |                                                       ^
           v                                                       |
  +----------------------------------------------------------------------+
  |  Android phone     STT --> text              text --> TTS            | ⬜
  +--------+-------------------------------------------------^-----------+
           | POST /v1/chats                  GET .../stream  | SSE
  =========+=================================================+=============
           v                                                 |
  +----------------------+                     +-------------------------+
  |  internal/api     ✅ |                     |  SSE endpoint        ✅ |
  |  /health  /ready     |                     |  pushes each message    |
  |  /v1/chats ...    ✅ |                     +-------------^-----------+
  +----------+-----------+                                   |
             | Submit(chat)                                  |
             v                                               |
  +----------------------------------------------+           |
  |  internal/runner                           ✅ |           |
  |                                              |           |
  |   wait for slot --> Start() --> running      |           |
  |        |                                     |           |
  |        +--> provider.Run(ctx) --> stream     |           |
  |        |         |                           |           |
  |        |         +- update --> store --------+-----------+
  |        |         +- final  --> Complete()    |
  |        |         +- error  --> Fail()        |
  |        |         +- closed --> Cancel/timeout|
  +--------+-------------------------+-----------+
           |                         |
           v                         v
  +---------------------+   +----------------------+
  | internal/provider ✅ |   | internal/chat     ✅ |
  |  Provider interface |   |  Chat + Status       |
  |  Message / Kind     |   |  state machine       |
  |  Stub            ✅ |   |  Repository interface|
  |  Platform AI     ✅ |   +----------+-----------+
  +---------------------+              |
                                       v
                            +-----------------------+
                            | internal/chat/mysql ✅ |
                            |  rows <-> domain      |
                            |  two read paths       |
                            +----------+------------+
                                       v
                            +----------------------+
                            | internal/storage  ✅ |
                            |  GORM + pool         |
                            |  migrations          |
                            +----------+-----------+
                                       v
                               +---------------+
                               |    MySQL   ✅ |
                               |  chats        |
                               |  chat_messages|
                               +---------------+
```

What `cmd/server` wires today:

```
config.LoadFromEnv()  ✅  ->  logging.New()      ✅  ->  storage.Open()  ✅
  ->  storage.Migrate()  ✅  ->  chatmysql.NewRepository()  ✅
  ->  runner.New()       ✅  ->  runner.Recover()           ✅
  ->  api.New()          ✅  ->  serve()                    ✅
```

A prompt submitted over HTTP is stored, answered by a real model through
Platform AI, and streamed back message by message. Verified against the live
service: a question asked over HTTP was answered by Claude in about three
seconds, with the acknowledgement heard immediately. The path a voice client
needs is complete, end to end.

**Built**

- `cmd/server` — chi server, graceful shutdown, `GET /health`, request logging,
  panic recovery
- `internal/logging` — structured logging, context-carried attributes,
  credential redaction, runtime-adjustable level
- `internal/config` — three-layer configuration, validation, secret handling,
  wired into the server
- `internal/storage` — MySQL connection through GORM, pool configuration,
  GORM logging routed into `internal/logging`, opened at startup
- `internal/api` — the HTTP interface, one package per resource under it;
  `api.go` assembles them and is the only place routes are registered
- `internal/chat` — the Chat type and its status state machine. Pure Go; it
  touches neither the database nor HTTP
- `internal/chat/memory` — an in-memory `chat.Repository`, so the runner and
  the API can be tested without MySQL. Real code rather than a test fixture,
  because two packages need it and a copy in each drifts apart
- `internal/provider` — the Provider interface, its message types, and the
  stub implementation. Pure Go; no network
- Migrations for `chats` and `chat_messages`, applied at startup
- `internal/chat/mysql` — the chat repository. The `Repository` interface is
  declared in `internal/chat`, which stays free of GORM; every mapping between
  a domain type and a row happens in the implementation beside it
- `internal/runner` — executes chats: reads a provider's stream, stores each
  transient message, records the result, and handles cancellation, deadlines
  and recovery of chats interrupted by a restart
- The chat API: create, fetch, list, read messages, cancel. `cmd/server` wires
  the repository and runner together, so a prompt submitted over HTTP is
  answered by the stub provider and stored
- `internal/events` — an in-process bus carrying a chat's messages from the
  runner to whoever is listening
- `internal/provider/platformai` — answers using Zoho Platform AI, selected by
  `[provider] name`
- `internal/auth` — token issuing and hashing, bcrypt password hashing, and
  the constant-time comparisons around both
- `internal/api/assist` — `/api/tags` and `/api/chat`, answering Home Assistant
  in the shape its Ollama integration expects. This is how a spoken question
  reaches the server now that the client is gone
- Users, their clients, and their sessions. A client authenticates with
  a bearer token and holds its own active session; chats land there,
  history reaches the provider, and a new prompt supersedes whatever is still
  running in that same session
- `GET /v1/chats/{id}/stream` — server-sent events, delivering each message as
  it is produced. This is the voice path
- `GET /health` (liveness, no dependencies) and `GET /ready` (checks the
  database, 503 when it is unreachable)

**Not built**
- Authentication. Every endpoint is open
- The Android client, tools, memory and the agent loop
- The chat API and the runner that executes chats
- Agent loop, tools, permissions, events
- Authentication
- Any client

**Known loose ends**

- `?wait` still polls the database. It is a convenience for using the API by
  hand; the stream is what a client should use, and could serve `?wait` too.
- The event bus is in-process. A second process would not see another's
  events, and clients would hear nothing from chats it was running.
- Nothing terminates TLS in development. The assistant binds the loopback and
  refuses a public interface in production, and `deployments/` puts Caddy in
  front, but locally it is plain HTTP and must stay on this machine.
- Tokens do not expire. Revocation is the only way to end one, which is the
  agreed trade for a handful of the owner's own clients.
- Platform AI's reassurance interval is fifteen seconds, and answers commonly
  arrive in three to nine, so most chats send only the opening
  acknowledgement. That is fine now, but if answers get slower the interval is
  worth shortening: silence is what to avoid when listening.
- the assistant refuses to start when the database is unreachable. That is deliberate
  for now, but means a database restart takes the server down with it.

---

## 11. Deploying

**The assistant can be looked at, not only talked to.** Owner, 1 October
2026: they want to see the memories, the events, the tool modules with
their names, the vocabulary, and the profile it has written about them.

Three of those had no way to be read. `GET /v1/tools` lists everything
the assistant can do, grouped by domain, marking which tools every
request carries and which are described only on request -- the
deferring mechanism is most of what makes a long tool list affordable
and was invisible everywhere, including to whoever was changing it.
`GET /v1/memories` lists what it has remembered, with uses and when
each was last used: a memory is believed indefinitely and corrected
only by accident, so a wrong one shapes every later answer and this is
the only way to find that out. `GET /v1/events` lists what the devices
reported, newest first by when it happened rather than when it
arrived, with the kinds counted across everything so a filtered view
still knows what else exists.

All three are read-only. A tool is not configuration: turning one off
from a screen would make the assistant's abilities differ from its
code with nothing to say why. Memories are written during a
conversation because that is the only place with the context to judge
what is worth keeping.

**Tasker reports to the server, never through Home Assistant.** Owner,
1 October 2026: "there should be no connection between android tasker
and ha companion, it should directly go to ai server only."

The Home Assistant Companion app was the obvious shortcut -- it is
already installed, already authenticated, and ships about sixty
sensors covering battery, wifi, steps, screen state, the app in the
foreground and location. Four of them are switched on today. It was
offered and declined, and the reason it was declined is the better
reason: Home Assistant is the voice and the hands, not the memory.
Routing what the person does through it would make the mind depend on
the room, and the mind is the thing that is about to move to a cloud
machine while Home Assistant stays at home on a Pi.

So the phone talks to the server directly, and Home Assistant never
sees an event. What the Companion would have given free now needs a
Tasker profile each: that is the price, and it was accepted knowingly.

**Where this is going, decided 1 October 2026.** Three tiers, not two.
The server, MySQL and `tei-embed` move to a cloud machine; Home
Assistant, Piper, Whisper and the Deepgram bridge run on a Raspberry
Pi at home; the wake word stays at the microphone.

`tei-embed` goes with the server and not with Home Assistant. The Go
server is its only caller, so leaving it home would put an internet
round trip inside every memory search -- and it is published for
`linux/amd64` only, with no ARM tag, so it could not run on the Pi in
any case.

The wake word cannot move to the server. It has to hear every second
of audio to catch one phrase, so running it centrally means streaming
the room continuously, all day, and losing the wake word whenever the
network hiccups. Running it at the microphone means only the seconds
after it fires ever leave the device.

Whisper has to come down from `medium-int8` on the Pi: `small` is
already reported as too slow there for real-time voice. That is a real
loss and worth naming -- at `medium` Whisper transcribed "Hey Jarvis"
correctly once in 22 recordings, and `base` will be worse. It is
survivable only because Deepgram is the primary and Whisper is the
outage fallback.

The consequence to design for is that losing the internet currently
costs accuracy -- Deepgram fails, the bridge falls back, Jarvis still
answers -- and after the move it costs everything, because the mind is
on the other side of it. Home Assistant's local intents already cover
time, timers, lights and lists without the server, so the degraded
mode can be "the house still works, Jarvis is away" rather than
silence. That is cheaper to decide before the move than after.

Both directions cross the network: Home Assistant calls the server for
conversation, and the server calls Home Assistant to announce. The
Google OAuth redirect, `ASSISTANT_URL` baked into the web client, and
`[server] allow_public_bind` all change with it, and the Home
Assistant token should be rotated and supplied through the
environment as part of the move.

**The deployed machine is deliberately behind, and is not being touched.**
The owner has parked it: nothing is being deployed there for a while.

So it is still the old install in every respect. It runs the binary from
before the rename, out of `/opt/friday` as `friday.service` under the OS user
`friday`, against a `friday` database. It has none of the work that followed:
no `internal/api/assist`, so no `/api/tags` or `/api/chat`, so Home Assistant
cannot reach it. Voice works against a server running on the owner's laptop
instead.

Two things follow. The migration section in `deployments/README.md` is not
pending work waiting to be finished — it is there for whenever that machine
is next visited. And deploying is not one step: the rename has to happen
alongside it, because the unit, directory, user and database on that box no
longer match anything in this repository.

**the assistant binds the loopback and refuses a public interface in production.**

It speaks plain HTTP and its tokens are bearer credentials, so anyone who can
read one becomes that client. The default address is `127.0.0.1:8080` rather
than `:8080`, because the latter looks like localhost and binds everything —
the mistake is silent, which is why it is refused rather than warned about.
`[server] allow_public_bind` exists for when something else already terminates
TLS, and has to be set deliberately.

**A method the router serves and the cross-origin policy does not list
is a request the browser refuses to make.** `PUT` was missing from
`middleware.Methods` from the day the list was written until the day
somebody tried to save their profile, months later.

Nothing caught it and nothing could. The preflight answered 204 and
refused the method in the same breath, so the server logged a
successful request; the real one never arrived, and the screen said it
could not reach the assistant, which was true and said nothing about
why. It only bites in development, where `flutter run` serves the UI
from its own port -- which is exactly where nobody looks for it.

The list is exported now, and `TestEveryMethodTheRouterServesIsAllowedCrossOrigin`
walks the router against it. That is the only place that can see both.

**Caddy owns the certificate.** It obtains one from Let's Encrypt on the first
request and renews it indefinitely, so there is no certbot and no expiry to
forget. Its configuration sets `flush_interval -1`, without which server-sent
events are buffered and a voice client hears nothing until the end.

**A hostname is required.** Let's Encrypt will not certify a bare address.
DuckDNS gives free subdomains that do not expire.

**The database is on the same machine**, not a managed free tier. Those cap at
around a gigabyte, and every prompt and answer is stored; a local database
also spares a network round trip on the history read that precedes every chat.
The cost is that backups are ours, which `deployments/backup.sh` and its timer
cover: nightly, fourteen days, written under a temporary name so a half-written
dump is never mistaken for a good one, and checked afterwards for the tables it
should contain. A backup that restores nothing is worse than none, because it
is trusted.

Two things that bit while writing it, both worth knowing:

- `mysqldump` reads `INFORMATION_SCHEMA.FILES` unless given
  `--no-tablespaces`, which needs the server-wide `PROCESS` privilege. A
  backup user has no business holding that, so the flag is required rather
  than cosmetic.
- `grep -q` under `set -o pipefail` exits at the first match, kills the
  upstream `zcat` with SIGPIPE, and reports the pipeline as failed. A correct
  backup was declared corrupt by its own verification. Counting reads the
  whole stream and cannot do that.

## A short call is brief, and that is the whole of it

Asked what calls there had been, the answer came back: *"Your mother
called at 10:33 am for nineteen seconds and again at 10:43 am for
twenty-three seconds. Kliffy called at 10:45 am for fourteen
seconds..."* Owner: *"don't need to say exact 19 secs, 23 secs,
those are unwanted."*

Quite right. Handed a 19, a model says nineteen, and four calls
recited to the second is a reading of the call log rather than an
answer about the morning.

Fixed where the number is rendered rather than in the prompt, per
the standing order. A payload field named `seconds` is now shown as
somebody would say it: under a minute is `lasted=brief` and nothing
more, past that the minutes are the fact -- `lasted=7 minutes`. The
exact count never reaches the model, so there is nothing for it to
recite.

**Naming `seconds` here does not break the rule that the event
describes itself.** That rule forbids the server knowing what a
*kind* of event is; nothing here knows what a call is. `seconds` is
a unit, it means the same whoever sent it, and a sender writing that
field has said it is reporting a span.

A side effect worth having: two short calls from the same person now
collapse into one run, because their rendered values match. *"Amma,
brief (2 times, last at 10:43)"* is the shape of the morning, which
is what this tool is for.


## The description cannot list what arrives next

Asked *"do you know what's my battery right now?"* the assistant
answered *"I have no tool that tells me your device battery, sir."*
Twenty-two readings had arrived from that phone the same morning. It
never called the tool.

This is the second time, and the first is in this file already: asked
who he had spoken to, it said it had no tool that reads call logs,
because the description listed places and networks and never people.
Owner: *"remember, whenever a new event type is added, the event tool
must adapt to it."*

Quite right, and the adaptation cannot be a list. Any device may
invent a kind; a description that enumerates them is wrong the day
after it is written, and enumerating is also what teaches the model
that what is *not* listed is not there. So the description now says
the opposite of a list:

- **A question in the present tense is this tool too.** How much is
  left, what it is doing, what it is connected to, whether it is on.
  A device reports a reading whenever it changes, so the newest
  event of a kind *is* how that thing stands now -- ask for the kind
  with a limit of 1. That generalises the music.state rule that was
  already there as a one-off.
- **Never answer from this text that a tool is missing.** "You cannot
  know from this text what is in here. Call this once without a
  kind, read the kinds it lists, and ask again by name."

Measured both ways, as every description change here is. The old
wording scores 73 of 74 and calls nothing at all for *"how much
battery do I have left"*; the new wording 74 of 74. Only one of the
three new cases failed before, which is the usual shape of these: the
wording was not hopeless, it was unreliable, and unreliable is what
produces one flat denial in the middle of a day that worked.


## The phone had to be told what a hundred means

The battery reached a hundred per cent and nothing was said. Two
causes, found together.

The first was the audience bug fixed an hour earlier: `battery.full`
*was* found at 12:28 and allowed, and then refused on the last step
-- `announcement: not something said unasked: warning`. It was
re-found every thirty seconds for five minutes and failed
identically each time.

The second was on the phone. The *Battery Changed* task marks an
event urgent from `If %BATT ~R ^(5|10|15|20|25)$` -- the low
thresholds only. The high end was left to Android's *Battery Full*
broadcast, **which fires once per charge cycle**. It fired at 12:28
and did not fire again when the level touched a hundred at 13:37,
because the phone had never dropped far enough to re-arm it.

So the owner's own example could not have worked: *"if 100 is
reached it announces, I plug out, it becomes 99 in five seconds, I
plug back in and it becomes 100 in ten seconds -- it has to announce
again."* Only the level reading re-fires on that.

**Two edits on the phone, both where the knowledge belongs.** The
regex gained `|100`, so the level reaching a hundred is marked. And
*Battery Full*'s payload went to `"urgent":"false"` -- it still
arrives and is still history, it just no longer insists, because
otherwise one charge produces two announcements eighteen minutes
apart. Nothing in the server learned what a battery is.

Verified end to end by running the task against a real hundred per
cent:

    14:14:23  events taken from a device  stored=1
    14:14:34  something might be worth saying  first="battery.level 100"  because=immediate
    said      "Your phone is fully charged, sir -- you may want to unplug it."

**One thing to watch.** Running the task by hand left its line in
`jarvis-sending.jsonl` undelivered until the next flush -- the move
to the sending file succeeded and the post did not. It recovered on
its own when the flush ran again, which is what the pending check at
the top of *Jarvis Flush* is for, so this is the design working
rather than a fault. Worth remembering when a hand-run event seems
to vanish.


## The direction belongs in the kind

Every family of events splits its direction into the kind:
`wifi.joined`/`wifi.left`, `place.entered`/`place.exited`,
`music.playing`/`music.paused`, `call.made`/`received`/`missed`. Two
did not. `screen.changed` carried `value: "Pixel 7"` with
`locked: true|false`, and `bluetooth.changed` carried
`value: "bike helmet"` with `connected: true|false`.

**Everything here counts on kind *and value*.** So a lock and an
unlock were one label, and so were the helmet going on and coming
off. On 4 October the phone recorded 17 pickups and 25 screen-offs;
the standing description could say he touched his phone 42 times and
could not say which were which. `notice.Toggling` already refuses to
remark on a state, which is right, but the profile's counting had no
way to see past the single label either.

Fixed on the phone, where it belongs:

- `screen.locked` / `screen.unlocked`. The *Screen Changed* task now
  builds `{"kind":"screen.%par1", ...}` and its two callers pass the
  word rather than a boolean. The payload lost its `value`
  altogether -- there is nothing about a screen event that varies, so
  the kind is the whole fact, and `valueOf` seeing nothing gives a
  label that is the bare kind.
- `bluetooth.connected` / `bluetooth.disconnected`, by the same
  shape. The task already mapped a MAC to a friendly name in two
  actions; two more now map `%bt_connected` to a word, and `%par1`
  is `bluetooth.%BTWORD`.

Verified live: `screen.unlocked` arrived. `screen.locked` is
**untested** -- adb cannot put this phone's display to sleep while it
is charging, so no display-off has occurred since the edit.


## Plugged and unplugged are a State, not an Event

Owner picked this and the one above off a survey of what the phone
could report. Worth recording what the survey found, because it is
not what I expected:

**Tasker's Power *event* category holds exactly three things** --
Battery Changed, Battery Full, Battery Overheating -- and all three
were already wired. There is nothing left in it. Charging has to
come from the **State** context (State > Power > Power, source Any),
which brings an enter task and an exit task with it.

So `*Power Any` runs *Power Plugged* on entering and *Power
Unplugged* on leaving, each calling `Jarvis Event` with the kind and
`{"level":"%BATT"}`.

**The level is not the value, deliberately.** Putting `%BATT` in
`value` would make every percentage a different label -- the same
mistake this file records three times already, most recently a call's
length turning one caller into six strangers. The level goes in its
own field, `value` is absent, and the label is the bare kind.

About six rows a day, and it makes the twenty-four battery readings
already arriving mean something: unplugged at 71 again, not on
charge at eleven at night, charged through the night.

**A warning about building this over adb.** Backing out of a profile
whose task is half-made raises *"Delete unfinished profile?"*, and a
blind tap took the OK -- losing the profile and both tasks. A second
attempt bound the profile to *Jarvis Flush* because a tap landed on
the wrong row of a picker. Build the task to completion before
backing out of anything, and read the screen between every tap.


## Announcing a missing tool for something that needs none

Asked to add up a list of numbers, the answer opened:

> *"I have no tool that calculates a sum, sir. I can work through it
> for you."*

and then worked through it. Owner: *"its saying no tool, but why,
its not a proper response."* Three of these in one day -- the same
for a battery reading, and for a question about a song, where it
went further and recited the whole toolbox before declining.

**The persona asked for it.** The honesty block said *"where there
is no tool for what is being asked, say plainly that you cannot do
it and what you can do instead"* -- a good rule for the world, where
a claim to have set a reminder must be backed by a tool that set
one, and a bad one for arithmetic, which needs no tool and never
did. The block never drew the line, so the model drew it nowhere.

Now it does: the rule is about *doing, changing and looking up*,
and adding numbers, what a word means, how to put something, and
thinking a problem through are answered in its own voice with
nothing said about tools. Reciting the toolbox is forbidden
outright -- nobody asked for an inventory.

**The suite can now say what must not be said.** A case may carry
`Never`, fragments the reply must not contain. Everything here
measured which tool was chosen, and here the choice was right both
times and the answer was still poor. `ask` returns the reply text
alongside, and `EVAL_ONLY=text` runs just the matching cases so one
wording can be measured both ways without paying for seventy-seven.

**And it is not reproducible in the harness.** The exact sentence,
with the old wording, with and without the look-first block
production sends, answers cleanly every time. So this change is
*unmeasured*: the suite is 77 of 77 either way, which says only that
nothing broke. Production has something the harness does not -- the
standing profile, recalled memories, the conversation behind the
question -- and until that is reproduced here the fix rests on the
wording being right rather than on a measurement.

**The sum was also wrong**, which nothing above addresses. The
speech came through as twelve numbers including five fifties; the
answer used three of them and gave 475 where the words add to 575.
It neither used what it was given nor said the input was unclear.
That is a separate fault and still open.


## The model is told what the devices report

Owner: *"make the model well aware of the phone device events."*
Asked whether his phone was locked:

> *"I have no tool that reads your phone's lock status, sir. The
> tools available to me cover your calendar, reminders, messages,
> tasks, what you have asked me to remember, and what your devices
> have reported about your activity and location -- calls, networks
> joined, music playing, battery level. None of them includes the
> phone's lock state."*

`screen.locked` and `screen.unlocked` had been wired that morning.
Three of the four kinds it named were guesses.

**This is the cost of not enumerating, and the previous section is
why we do not enumerate:** any device may invent a kind and a list
written into a description is wrong the day after. But a model given
no list makes one up, and then reasons confidently from it.

So the list comes from the data. `event_kinds` is a second tool in
the domain, prefetched, returning one line a kind: the name, how
many, when the last one was. It says outright that this is the whole
of what is reported, so a thing absent from it is genuinely absent
rather than merely unlisted. Nothing in it knows what a battery or a
screen is -- a kind wired on the phone in the morning is in the
prompt that afternoon with no change here.

**It is a second prefetch, and that changes a standing rule.** The
rule was memory alone, because every other domain is large enough
that loading it each turn would cost every turn for the sake of the
few that ask. This is not that: it is one grouped count, twenty-two
lines, shorter than a single day of events, and it is metadata
rather than data -- what *can* be asked, not what happened. The
prefetch mechanism needed no change; the tool sets `Prefetch` and
that is all.

**Not cached.** `Fresh` stays zero, so it is re-read every turn.
Accuracy before latency, and the query is a grouped count.

### What the live listing showed

Two things, for free, the first time it ran against the real table:

- `power.plugged` and `power.unplugged` have both fired -- 14:57 and
  15:10 -- so the charging profile works end to end.
- **`screen.locked` had never arrived**, and the cause was mine.
  Tasker's own run log named it in one line:

      A IfFail   ID53.1   Screen Locked.Perform Task

  The action was being skipped, not failing. *Screen Locked* guards
  its report with `If %ScrOpen eq 1` -- only report a lock if an
  unlock was seen -- and the first, mis-aimed attempt at editing
  that caller had typed the word `locked` into the **condition's**
  left-hand field instead of into Parameter 1. The guard read
  `locked EQ 1`, which is false forever. I then scrolled up, set
  Parameter 1 correctly, and never looked at what I had clobbered on
  the way.

  Restored to `%ScrOpen eq 1` and verified live: Tasker logged
  `A OK ID53.1`, and `screen.locked` reached the table at 15:59.

  **`/sdcard/Tasker/log/runlog.txt` is the thing to read** when a
  profile looks dead. It distinguishes a profile that never fired
  from a task that ran and skipped its action, which no amount of
  staring at the server can.


## The diary speaks first

Owner: *"a movie at 6 is fine a few hours before, maximum one day.
But attending a wedding has to be said a day or two before, because
I need to plan -- buy a gift, iron the shirts. It takes effort. So
the reminder start depends on what action it is."*

That sentence is the whole design. The diary has been read a hundred
and four times in a week and has never once spoken first, and the
reason it could not is that there is no offset that works for both
of those. Fifteen minutes is useless for a wedding; two days is
absurd for a film.

**So the lead time is judged, once, per entry.** A model reads the
entry when it first appears and says how much warning it needs and
why. `internal/coming` keeps that decision, because it does not
change -- what a wedding needs doing before it is the same at nine in
the morning as at ten -- and asking again every five minutes would be
three hundred calls a day to be told the same thing.

**Nothing in the package knows what a wedding or a film is.** The
question put to the model is one anybody could answer about
anything: *how much is there to DO beforehand, not how important it
is*. Something you only turn up to needs hours; something needing a
present bought, clothes got ready, a journey worked out or somebody
else arranged needs days, because that doing has to fit around the
rest of the week. A list of kinds would have been wrong in another
language and silent about the next sort of thing he writes down.

**Several moments, not one.** The two examples are different shapes:
a film earns one word, a wedding two -- days out while there is
still time to buy something, and again on the day when all that is
left is to leave. How many is part of what is judged, so the model
returns a list and the server counts through it.

**Guards, each for a stated reason.**

| | |
| --- | --- |
| `Longest` 7 days | A fortnight's notice is forgotten by the time it matters. |
| `Shortest` 10 min | Below that it is not notice, it is narration. |
| `Every` 5 min | Leads are hours and days; a minute's precision buys nothing and costs a calendar read a minute. |
| `Ahead` 21 days | Longer than the longest lead, or a thing needing a week's warning is first seen six days late. |
| `Shape` | Title plus start. A rename or a move throws the judgement away: "dinner" becoming "dinner with the Raos at their place" is a different amount of doing. |
| `Said` as a count | They are spoken in order, and a count cannot disagree with itself. |
| recorded before speaking | A warning lost is one sentence; a warning repeated every five minutes until the thing starts is the feature being switched off. |

**Audience `Everywhere`**, like a reminder and for the same reason: a
wedding two days out is no use to somebody who is out of the house.
Its own kind, `coming`, because nobody asked for it -- a reminder is
a thing they set, this is the assistant reading the diary and
deciding there is something to do first.

**Silence is kept too.** Most of a diary owes nothing, and that
answer has to be written down or it is paid for again on every tick.

### What the first live pass did

Seven entries in the next three weeks. Six holidays and a birthday
were judged silent. The seventh was his own example, unprompted:

    title="Meesaya Murukku 2 Movie"  before=2h  starts=18:10
    "Meesaya Murukku 2 Movie is in 2 hours, at 6:10 pm.
     Time to leave and let your mum and Muthuselvi know."

One lead, two hours, and a reason it read out of the guest list. The
wedding case is untested until there is a wedding.

### A birthday is not a thing that happens to you

Owner: *"birthdays should be told a few days before to buy a gift."*
The first pass judged one silent, and the fault was in the question,
not the model. Two phrases did it: *"a birthday nobody has to
attend"* sat in the list of things needing no warning, and *"a thing
that happens whether or not they prepare"* is true of a birthday and
of almost everything else.

The question now draws the line the owner actually cares about:

> Not everything in a diary is an appointment. Some entries are a
> date somebody wrote down so that it would not be missed: nothing
> is scheduled, nobody has to go anywhere, and that is exactly why
> the warning is the whole of the point, because on the day itself
> there is no time left to do the thing the date was written down
> for. A day that belongs to another person is nearly always like
> that.
>
> Do not reason that a thing will happen whether or not they prepare
> -- that is true of almost everything, and the question is whether
> they will wish they had.

Still a method: it names no kind, and it covers an anniversary, a
renewal date and a deadline as well as a birthday.

**Changing the question had to re-judge what it had decided**, and
at first it did not: a hundred entries were already settled under
the old words and nothing would ask again. So a fingerprint of the
question is now part of `Shape`. Change the wording and every
judgement made under the old one stops matching and is made afresh,
which is the only behaviour ever right -- a kept answer to a
question nobody asks any more is worse than no answer.

Measured on the real diary, same seven entries, three wordings:

| | first | second | third |
| --- | --- | --- | --- |
| Meesaya Murukku 2 Movie | 2 hours | 2 hours | 2 hours |
| six holidays and festivals | silent | silent | silent |
| Happy birthday! | silent | silent | **3 days** -- *"A present to buy and something to say."* |

Two hours for a film and three days for a birthday, both chosen by
the model, with nothing in the server naming either.


## Answering a warning puts it out

Owner: *"the server adds this announcement in the conversation,
right? If I acknowledge it, it should act accordingly. 'Ok got it'
means no need of that reminder any more, because I'm already on it.
Or 'cancel that' and that reminder should go."*

Quite right, and without it the feature turns on itself: a wedding
warned about three days out says itself again on the day, to
somebody who bought the present the first time they were told.
Saying it twice to a person who has already acted is the whole of
what makes this sort of thing tiresome.

**Which words mean that is the model's to decide.** "Ok got it",
"done", "already bought it", "stop telling me", and the same in any
other language, all land on one tool, `coming_settled`. Nothing in
the server matches a phrase.

**It is keyed on what they called it, not on an identifier.** The
obvious design -- the model passes the Google event id -- cannot
work: the diary listing shows an id only for entries on the
person's own calendar, and the first thing this ever warned about
properly was a birthday from Google Contacts, which has none. So
the tool takes what they said, or nothing at all, and matches
against the handful of warnings actually outstanding:

- Named nothing, it is the one most recently spoken. That is what
  an answer answers.
- Named something, matched by likeness, because what reaches here
  came through speech and "the wedding" has to find "Attending
  Vignesh's wedding". Nothing under four letters matches alone, or
  "wed" would find "Wednesday standup".
- Two things it could mean, it settles neither and says so.
- Nothing spoken about lately, it settles nothing: acting on a
  warning nobody heard is not acting on anything.

`settled_at` survives a re-judgement. Re-judging happens when the
entry is renamed or moved, or when the question put to the model is
reworded, and none of those un-does somebody having said they were
dealing with it.

### Two mistakes worth recording

**The harness, not the model.** Three eval cases failed and I
changed the tool's description twice and the persona once to chase
them. `coming_settled` was never in the eval registry. The model had
been asked to call a tool it was not given, and every conclusion
drawn from those runs was worthless. The persona line added on that
evidence was reverted: measured with the tool actually present, it
does not earn its place.

This is the second time a measurement here has been wrong in the
same way -- the first was reporting a tool-deferral saving that did
not exist, from a registry missing `tool_describe`. **Before
believing an eval failure, check the tool is in the eval's
registry**, which is built separately from the server's.

**A case that needs context cannot be measured without it.** "Ok got
it" with nothing in front of it means nothing, and a model that
called a tool for it would be guessing. The suite now carries
`After`: one assistant turn said just before, so an answer has
something to answer.

**And a third case was simply wrong.** *"I have already bought the
present"* was failing against a warning that said *"a present to buy
and something to wear"*. It is half an acknowledgement: settling the
whole warning on it would silence the shirt as well, and reaching
for the diary instead is not obviously the wrong move. A case whose
right answer is arguable measures nothing, so it was replaced with
*"I have sorted all that out already"*, which passes three times out
of three.

That leaves a real question open rather than closed: **what should
half an acknowledgement do?** Today it does nothing, and the warning
comes again on the day with both things in it. Telling somebody to
buy a present they have bought is the fault this whole section is
about, one step smaller.

The suite is **82 of 82**.


## The mic re-opened in silence

Owner, having asked for something and been cut off mid-sentence:

    16:16:47  "Can you send me the"
    16:16:51  "Your question was cut short, sir. Send you what?"
    16:16:58  "Hey, Jarvis."
    16:17:00  "Sir."

*"I experienced something here. It asked 'send you what?', then I
said hey Jarvis to wake it up thinking it had stopped listening, but
it was already listening."*

The satellite log says the rest of it:

    continue_conversation: '1'   "Your question was cut short, sir. Send you what?"
    Continued conversation started
    continue_conversation: '0'   "Sir."

Three faults in seven seconds, and they are worth separating.

**The mic re-opened with no sound.** A wake word plays
`wakeup_sound`; a continued conversation played nothing. The
`LISTENING` event is emitted, but that drives LEDs and peripheral
clients -- there is nothing to hear. So the only cue that it was his
turn was the absence of one. Fixed in the satellite: the continued
branch now plays the same chime, through the same two paths the wake
word uses, so `listen_during_wake_sound` still decides whether the
mic opens during it or after.

**The wake word became the answer.** Nothing was wrong with hearing
it -- the mic was open and he spoke. What was wrong is what was done
with it: "Sir." is a fine reply to being addressed and a terrible
one to a question still waiting. The question was lost and the thing
he wanted sending was never heard of again.

Now a rule in the persona, which is where a thing that crosses every
tool belongs: *a turn that is nothing but your name is not an answer
to anything. They are checking you are there, which they do when
they cannot tell whether you are still listening, and it happens
most often straight after you have asked them something. Say you
are, and ask your question again in the same breath.* The name comes
from configuration, so this holds when it is not Jarvis.

**And the speech was cut at four words.** *"Can you send me the"* is
where the recogniser stopped, which is a third thing and not yet
looked at. The follow-up question was the right answer to it;
needing to ask was not.


## Being told to slow down is not being told no

The gateway answers a burst of calls with:

    You have made too many requests continuously.
    Please try again after some time. (HTTP 400)

Nothing caught it. The client retries an expired token and a dead
connection, and this is neither, so it came out as a failure and
the person heard *"I am having some difficulty reaching the server,
sir"* about a service that was working and merely busy. Owner:
*"you know it's rate limiting right, then do something for it."*

**It is a 400, not a 429**, with the reason only in words, so the
status alone cannot tell it from a malformed request -- and
retrying a malformed request for ever is exactly what must not
happen. So it is read from the status where there is one and from
the message where there is not, and nothing else is retried.

Retrying is safe for the same reason a dead connection is: nothing
was read, no tool ran, and the model was never reached, so asking
again is asking once.

**Two waits, one second then three.** A turn has thirty seconds
before the caller cuts it and most of that is already spoken for; a
third wait would spend the budget to arrive after nobody is
listening. The wait ends with the turn rather than outlasting it.

**And the suite stopped causing it.** Eighty-three questions as
fast as the machine can send them is what the gateway means by
"continuously". The run now pauses a second and a half between
them, which costs under two minutes on a five-minute suite and
turns a regular interruption into none. A retry inside a test is a
slower test for no information; the pause is cheaper than the
refusal.


## Two search services, because they want opposite things

Owner: *"Let's have both. Firecrawl if I ask a question in
conversation. TinyFish for announcements and research where speed
is not important. In conversation, if Firecrawl doesn't work or
credit is over, use TinyFish as fallback."*

The numbers behind that, independently benchmarked:

| | free allowance | card | latency | accuracy |
| --- | --- | --- | --- | --- |
| **Firecrawl** | 1,000 credits/month = **500 searches** | no | **510 ms** | 95.3% |
| **TinyFish** | **none** -- 30/min, 500/hour, uncounted | no | 2.62 s | 92.0% |
| Brave | $5/month ≈ 1,000 | **yes** | 601 ms | 94.0% |
| Tavily | 1,000 credits/month | no | 1.88 s / **4.29 s** | 87.7% / 93.0% |
| SearXNG | unlimited, self-hosted | no | ~1-3 s | upstream |

**Tavily is what every tutorial recommends and was never a
candidate.** 4.29 s advanced does not survive a turn the caller cuts
at thirty seconds, in a week spent taking the greeting from 7.5 s to
2.8 s. **Brave's free tier is gone** -- it now keeps a card on file,
which happened between the model's training and today and is exactly
why this was looked up rather than recalled.

Sixteen searches a day was the owner's objection to Firecrawl alone,
and it is a fair one. Hence both.

**`search.Pair` is the whole of the fallback.** It asks the quick
one; on any failure it asks the other. Four things it does that a
plain try-then-try would not:

- **A spent allowance is remembered.** 402 is "Insufficient
  credits", and the service says itself it is not retryable. Asking
  it again on every search for the rest of the month is a wasted
  round trip every time, in the one place where the entire point was
  speed. It is left alone for an hour -- not the month its allowance
  actually takes, because the cost of guessing wrong is one wasted
  trip an hour and the cost of not guessing is one per search.
- **The quick one gets a slice, not the turn.** A service that stops
  answering must not spend the budget the one that would have
  answered needed.
- **The turn ending is not the service failing.** Asking the spare
  after the person has gone is a second failure, more slowly.
- **A fallback is logged.** A silent one is how a dead key goes
  unnoticed for a month.

**The patient half has no caller yet, deliberately.** The split is
that loops nobody waits on use TinyFish directly and never spend an
allowance a conversation may need -- but no loop asks the web
anything today, and a function with no caller is a guess about what
the next one will want. When one exists it is `tinyfish.New` and
nothing else.

**Snippets, not pages.** `scrapeOptions` would return page markdown
in the same call, which is tempting and wrong by default: a page
costs a credit each and seconds each, and most questions are
answered by the summary. Reading one should be a thing somebody
chooses.

The tool's description is about the world against their life, and
carries the reason the whole thing exists: *being out of date sounds
exactly like being wrong, and they cannot tell which you are.*
Measured at **88 of 88**, including the two guards that matter --
"what did I do yesterday" still goes to their own events, and
arithmetic still goes nowhere.

### And the benchmark was wrong here, so the order is inverted

Both keys in, nine real searches, same queries through both:

| | firecrawl | tinyfish |
| --- | --- | --- |
| cold, median of four | 3.3 s | **1.95 s** |
| warm, median of five | 1.36 s | **0.96 s** |
| won | 2 of 9 | **7 of 9** |

The published figures were 510 ms against 2.62 s. From this machine
it is the other way round, and not marginally. The benchmark was
almost certainly run from a US datacentre; this is Chennai, and
whatever edge Firecrawl has does not reach here.

**That removes the reason for the split.** Firecrawl was chosen to
be asked first *because it was fast*, and it is not. Worse, it is
the one with the 500-a-month allowance while TinyFish counts
nothing -- so asking it first spends a capped resource in order to
be slower, which loses on both axes at once.

So TinyFish is the quick one and Firecrawl the fallback: for
TinyFish's thirty a minute, and for its being down. The one
argument for the old order survives and is worth recording -- the
same benchmark gives Firecrawl 95.3% against 92.0%, and credits
that expire unused each month cost nothing to spend. That is a
reason to prefer it, but it is not the speed reason it was picked
for.

**The lesson is the one this file keeps relearning.** A published
number is a measurement of somebody else's network. Four searches
would have caught this before the design was written around it.


## A search about now has to say so

Asked what was trending in Tamil today, the search came back in
1.3s with a Pinterest board called *"510 Tamil memes ideas in
2026"*, a WAStickers listing and a Giphy page. Owner: *"why
nothing."*

Two reasons, and only one of them was ours.

**What is trending on social media today is not a web page.** No
index holds it, so an index returns what it does hold: evergreen
SEO. The answer given -- that Instagram and YouTube Shorts would
show it far better -- was the right one and better than inventing
something.

**But the freshness controls were being thrown away.** TinyFish
offers `recency_minutes`, `after_date` and
`domain_type: web|news|research_paper`; this was passing
`domain_type=web` and nothing else. Firecrawl offers the same
through `tbs` and that was ignored too. So a question containing
"today" was ranked exactly like a question about history, and a
page written three years ago outranked one written this morning.

The tool now takes `days` and `news`, and the model decides both --
it is the only thing that knows whether a question is about *now*
or about *how things are*. Firecrawl's `tbs` takes Google's
brackets, so days are rounded **up** to the next one: a filter
tighter than asked for hides things that should have been found,
one looser lets a little extra through.

Measured on the same query:

| without | with `days=1, news=true` |
| --- | --- |
| Trending Now Tamil -- *no date* | CBI arrests DGCA deputy director -- **3 hours ago** |
| Tamil Trending -- *no date* | Gold Rate Today in Tamil Nadu -- **3 hours ago** |
| Trending Tamil, 1.1M reels -- *no date* | Silver Rate Today -- **3 hours ago** |
| Latest Tamil News, News18 -- *no date* | Man attacks nurse over delay -- **9 hours ago** |

Undated evergreen becomes stories from this afternoon. It does not
make the meme question answerable -- memes are not news -- but it
makes every "what happened today" question answerable, and those
are most of what a search is for.


## When to search is a question about volatility, not about knowing

Owner: *"is the AI instructed to use the search tool only when it's
beyond its knowledge, or when it wants the latest info about it?"*

Both, and the description says so in two sentences that contradict
each other:

> **UseWhen:** If you find yourself about to answer from memory
> about the world, search instead -- being out of date sounds
> exactly like being wrong, and they cannot tell which you are.
>
> **Avoid:** Not for arithmetic, for what a word means, or for
> anything you can simply answer -- searching to confirm what you
> already know spends seconds they are waiting through.

One says *about to answer from memory → search*. The other says
*can simply answer → do not*. Taken literally they cover the same
ground and point opposite ways.

**The rule the model is actually applying is neither.** It is not
"do I know this" but "**can what I know have changed**". A capital
city cannot; a chief minister can. Being out of date about the
second is indistinguishable from being wrong about it, and the cost
is not symmetric: a needless search spends a second, a stale answer
is believed.

Measured, all four of these land correctly on the wording as it
stands:

| | |
| --- | --- |
| what is the capital of France | no tool |
| what does laconic mean | no tool |
| who is the chief minister of Tamil Nadu | `web_search` |
| how much is a litre of petrol | `web_search` |

**So the wording is left alone.** It reads badly and it measures
right, and rewriting prose that already works is how a line like
the persona one earlier today gets added on no evidence. What was
missing is not better sentences but a record of where the boundary
is -- so it is four eval cases instead, two either side. The
contract is the cases; the prose is only how it is explained.

The suite is **92 of 92**.


## Reading a page is a second tool, and a dearer one

Searching gives summaries; sometimes the answer is a figure, a
time, a list or the wording of something, and the summary was
abbreviating exactly that. So `web_read` takes addresses a search
gave and returns what the pages say.

**One fallback, not two.** `Pair` now serves both, through a
generic `fall`. The four things it does -- rest a spent service,
give the quick one a slice of the turn rather than all of it, treat
the turn ending as not a failure, and log every fallback -- are
each the result of something that went wrong, and none is worth
writing twice.

**Three pages, six thousand characters each.** Reading is the dear
act: seconds a page, and on Firecrawl a credit a page. Three
sources is already better founded than most answers, and the part
of a page that answers something is almost always near the top. A
page that is cut **says it was cut**, or the model reads half a
sentence as the whole of something.

**An address that no search produced is not fetched.** Anything
without `http://` or `https://` is the model having invented a URL,
and fetching an invented address is a round trip to nowhere.

**One page failing does not lose the others.** Firecrawl reads one
URL per request, so a login wall on the second would otherwise take
the first and third with it; it is carried as trouble beside the
pages that worked. A spent allowance is the exception -- that is
about the account rather than the page, so it stops the lot and
lets the caller fall back.

**And the eval case was wrong three times before the tool was.**
The first put the answer inside the summary -- *"the decision was
taken on Tuesday"* -- and then asked what the article said, which a
model reasonably treats as already answered. Posed so the summary
*cannot* answer it (a link to a timetable, asked for the timings),
it passes three times out of three.

That is the third case in this file whose right answer was
arguable, after "ok got it" with nothing in front of it and "I have
already bought the present" against a warning naming two things.
The pattern is the same each time: **a case that tests a judgement
has to make the judgement forced**, or it measures the model's
guess about an ambiguous situation rather than the thing being
built. A failing case is a question about the case first.

Measured end to end against the real services: BBC Weather read in
**2.74s**, 2,654 characters of text with the navigation stripped.
TinyFish takes all the URLs in one request and allows a hundred and
fifty a minute, so reading three results of a search is one call;
Firecrawl takes one at a time and ten a minute, which is why they
are read in sequence rather than at once.


## A name from a hundred metres away is a guess, and read like a fact

Owner went to Seven Screen's Cinemas at Sky Hub Mall, Kilambakkam.
The stay was recorded as **"Flavors non veg restaurant"**. Asked
why, and then: *"if Flavors non veg restaurant, what is the full
address with that coordinate?"*

Geoapify, at that point, within 150 metres:

    127m  Flavors non veg restaurant    catering.restaurant
    133m  திருவசந்தம்                    catering.restaurant
    166m  (unnamed)                     building,public_transport

Widened to 600 metres and told to include `entertainment` it finds
seven things -- all restaurants and clothes shops. **No cinema, no
mall.** Nominatim at the same point says
`Kilambakkam, Chengalpattu -- highway/residential`, no name at all.
Both are OpenStreetMap underneath, and Sky Hub is a new build
opposite the new bus terminus. The code did exactly what it was
told: the nearest named place within Reach.

**Two faults, and only one needs another map.**

The mall being absent needs a provider with its own commercial
venue data -- Google Places, Foursquare -- or a geofence drawn by
hand, which is free and permanent and the right answer for anywhere
visited often.

**The other fault was ours.** `Reach` is 150 metres, chosen to
match the distance that counts as one cluster of readings, and
those are different questions: one is how much a phone's position
wanders, the other is how close a business must be before somebody
is inside it. At 127 metres you are across a trunk road. The record
said "Flavors non veg restaurant" where it could have said "Grand
Southern Trunk Road" -- vague, but not **wrong**, and wrong is the
expensive one.

**So the name now travels with the two things that can check it.**
`place.stayed` carries `address` and `metres` beside the value:

    {"value":"Flavors non veg restaurant","minutes":170,
     "address":"Grand Southern Trunk Road, Urapakkam, Vandalur - 603210",
     "metres":127}

And `place_visits` reads them: beyond forty metres it says *"the
name is the nearest place 127m away and may not be where they were,
so offer it as a guess"*. Forty, because a shop somebody is inside
is about twenty away. Under that the address is still given and the
name is simply stated -- saying a true thing as a guess is its own
kind of wrong.

A place they named themselves says neither. They drew the fence;
an address and a warning on every line of a day spent at home is
noise.

**It costs a second Geoapify credit on every named stay**, where
before the reverse lookup was only made when no business was found.
A test asserted the old behaviour and failing it was the right
thing to happen. Three thousand credits a month against a handful
of named stays a day leaves ample room, and the credit buys the
only two facts that would have caught this.

**The value field is untouched**, so none of the counting changes:
the standing description still groups on the place rather than on
the particular readings.

### And the stays already written

`deployments/fill-stay-addresses.py`, beside the backup and rename
scripts, because this is an operation rather than a feature. Dry
run by default; `--write` applies it.

Three rules in it, each the reason it is a script and not a
one-line `UPDATE`:

- **It never touches the name.** Everything counts on that field,
  and a backfill that renamed places would quietly rewrite the
  description of somebody's life.
- **It skips places they fenced themselves** -- read from
  `place.entered`/`place.exited` rather than listed, so it stays
  true when another fence is drawn. 271 of the 274 rows were home
  or office, and asking would have spent credits to be told
  something they wrote.
- **One lookup a place, not a row**, grouped on the coordinate
  rounded to four decimals. Two hundred and seventy stays at home
  are one coordinate.

Three places, six credits. And the distances it found are the
argument for the forty-metre line, better than any reasoning:

| place | away | |
| --- | --- | --- |
| Muthu Supermarket | **11 m** | inside it |
| Ammaniammal School | 69 m | borderline, offered as a guess |
| Flavors non veg restaurant | 125 m | plainly a guess |

One of those three was where he actually was.


## What they say back to an unprompted sentence is worth more

Owner: *"whenever it announces or greets, the acknowledgement or
the respective response, if relevant, has to be taken more weight
in profiling, greeting and when talking to me."*

**Nothing was carrying it.** `SaidSince` returns only what the
person said -- the assistant's own turns are excluded on purpose,
or the description would be of the assistant. The cost of that is
that *"ok"* and *"stop telling me that"* arrive as two
unremarkable messages, when one of them is the only direct
evidence there is about whether an unprompted sentence was wanted.

**Why it is worth more is not that it is rare.** Everything else
in the description is the person starting something. This is the
person judging something the assistant *chose* to do, unasked, and
there is no other evidence of that anywhere.

`RepliesSince` pairs each thing said unasked with the next thing
the person said in the same conversation, within five minutes. A
window function, because the question is "the next row in this
conversation" and a join on "the smallest created_at greater than"
re-reads the table once per announcement. How long they took is
kept with it: seconds is a reaction, minutes is a decision.

**One finding from the real transcript changed the design.**
Renaming floods it and says nothing. *"I have called this
conversation Today's alibi"* is spoken in the middle of a turn, so
the next thing the person says is the next thing they were going
to say anyway -- it was **seven of the first ten pairs**, every one
a question with nothing to do with the name. `Volunteered()` is
the audiences table less that one kind, so a new kind is weighed
by default, which is right: a new kind is a new thing said unasked.

What is left is the evidence that was wanted:

| | |
| --- | --- |
| greeting | *"Good evening, sir. You were out a good while"* → **"The movie was quite good."** (14s) |
| warning | *"your phone is fully charged"* → **"Thank you for saying that."** (7s) |
| reminder | *"Good morning, sir."* → **"Good morning."** (249s) |

**It reaches all three places through one change.** The greeting
already reads the standing description, and so does every ordinary
turn, so weighting it in the profile weights it everywhere without
three separate mechanisms.

**And it must stay a description of the person.** The prompt says
so outright -- *"write what it says about the person, not how well
the assistant is doing"* -- because the obvious failure is a
profile that turns into a report card.


## The map, in four questions

Owner's own list: *"how far is the airport, what's near me, how
long will it take to get there, where is this address -- we can
have these."*

Three endpoints answer them, **all on the Geoapify key already
used to name stays**. No new account, no card.

| tool | answers |
| --- | --- |
| `place_find` | where is this, and what is its address |
| `place_near` | what is around them now |
| `place_distance` | how far, and how long in today's traffic |

### Traffic is not optional

One journey across Chennai, measured both ways:

    without traffic   16.1 km, 19 min
    with traffic      18.0 km, 53 min

**Nearly three times out.** A travel time that short is not a worse
answer than none -- it is acted on, and the thing is missed. So
driving always asks for `traffic=approximated`, and the answer says
so: *"the time allows for today's traffic, so it is what it would
take now and not a best case."*

### What makes them worth having is "from here"

A distance between two cities is a thing anybody knows -- asked
once in the whole transcript, answered from memory as *"about 625
kilometres"*, and right. What nothing else can say is how long it
takes **from where they are standing, at this moment**.

So the position comes from the last fix their phone reported, and
is **refused when it is over twenty minutes old**. The phone
reports every five minutes, so older than that means it has been
off or out of signal, and "twenty minutes away" from an hour-old
position is wrong in the one way that matters: it is believed.

### An unknown sort is refused rather than passed through

Geoapify answers an unknown category with an **empty list and no
error**. A typed-through "unicorn" would come back as "nothing near
you", which is a lie that looks like an answer. So the sorts are a
list -- food, coffee, pharmacy, fuel, atm and the rest -- and
anything else is refused with that list. None of accommodation,
sport or tourism appears, for the reason recorded in October: each
poisons the whole call, silently, even paired with one that works.

### What was deliberately not built

Turn-by-turn directions, which are useless read aloud and which the
phone does properly. And nothing was moved off `web_search`: it
found Sky Hub Mall when the map could not, so *what a place is like*
and *when it opens* stay there.

Measured live, first try: the airport found in 2.45s, 19.7 km and
55 minutes from home in 2.28s, an unknown sort refused in
milliseconds. The suite is **99 of 99**, including the two guards
that matter -- "where did I go yesterday" still goes to his own
history, and the distance between two cities still goes nowhere.


## The greeting could not cross-check, because it was never shown the plan

Owner: *"it knows I go to a movie in the calendar, then it knows my
location. When I come back it should cross-check whether I went
there, and ask how was the movie. I can't just hardcode this
situation -- I want a human-like greeting, an actually meaningful
question. It has everything, my every tracking, web search and
maps. It can ask better."*

He is right that it has everything, and it had everything except
the one thing that mattered. `Told.Diary` is documented as *"what
is in the diary between now and the end of tomorrow"*, and the
appointments that had already started were **dropped on the way
in** -- "what has already started is not what is coming". So the
greeting saw where he had been and never what he had meant to do.
It was not failing to reason. It was reasoning over half the
evidence.

`Told.Were` is the other half: what was in the diary between the
assistant last speaking and now. The data was already being
fetched -- the read starts at midnight -- and was being thrown
away four lines later.

**Nothing in the server does the matching**, which was the whole
point of the request. The stays carry their addresses and their
distances as of this morning; the appointments carry their places;
whether "Flavors non veg restaurant, Grand Southern Trunk Road,
Urapakkam" and "Seven Screen's Cinemas, Kilambakkam" are the same
afternoon is a judgement, and the prompt says so: *"the two will
almost never use the same words... judge it as a person would, and
do not require the names to match."*

Measured live against the real model, the real prompt and that
real evening:

> **"Good evening, sir. How was Meesaya Murukku 2?"**

Two times out of two, from a stay recorded under a restaurant's
name 127 metres away.

### What it still gets wrong, and why wording cannot fix it

Given a dentist appointment in Velachery and an evening spent in
Urapakkam, it asks how the dentist went. He was nowhere near it.

The obvious response is to write more caution into the prompt, and
that was tried and **measured worse**:

| wording | film | dentist |
| --- | --- | --- |
| as written | **asked by name, 2/2** | assumes he went |
| "silence is the default", "a plan is not evidence" | *lost the film* -- "was there a change of plan?" | "how did the dentist end up being skipped?" |

The second is worse on both counts: it gave up the case that works
and turned the case that does not into an accusation. Reverted.

**The reason is not caution, it is geography.** The model does not
know whether Velachery is near Urapakkam, and no amount of telling
it to be careful teaches it. What would is the distance, as a fact
-- and that is now a thing this server can compute.

The way to give it is not at the door: geocoding a place costs 2.4
seconds and the greeting has five before it falls back to fixed
text. It belongs in the look-ahead, which already judges every
diary entry hours in advance and keeps the answer. Geocode the
place then, keep the coordinates beside the judgement, and the
greeting gets "the diary said Apollo Clinic, Velachery, and the
nearest he came to it was fourteen kilometres" for nothing.

Still the model's judgement. Just made out of facts rather than
out of a guess about Chennai.


## Three things the greeting could not see, and one it must not invent

Owner, on the greetings proposed for it: *"those are so
inorganic."* Fair -- every one had the same shape: a statement, a
dash, a question tacked on the end. Written down it reads well;
spoken twice a day it is a tic, and he would notice inside a week.
The prompt now names that shape and forbids it, and says to use
the name of the thing rather than describe it: *"how was the film"*
is worse than its title, and *"you were out a good while"* is worse
than either.

Measured, the greetings became **"How was Meesaya Murukku 2,
sir?"** and **"How was dinner, sir?"** -- no preamble, the question
standing on its own.

### Silence

Absence is the one thing nothing else in the prompt can show.
Every other line is something that happened; ninety minutes of
nothing reported is usually the largest thing in the window and
appeared nowhere. `silent()` works the gaps out of the same events
and says what they are: *"a gap is not an absence of anything
happening; it is an absence of anything being reported."* A
battery falling to nothing just before one explains it; where
nothing does, it is told not to guess.

### How sure a place name is

The distance has been in the payload since this morning and
nothing said what it meant. Twenty metres is inside the shop and
may be said plainly; a hundred is across a road, and the name is
whichever thing the map had nearest. At that distance: name the
area, or ask. Measured, the greeting went from naming a restaurant
he had never been in to **"near Urapakkam"**.

### A missed call is told, not asked about -- and I made it invent one

It is the one thing in a greeting that costs him something by
waiting, so it wins over everything else. Written as a plain
statement of fact -- *"one thing here is told rather than asked
about: somebody tried to reach them"* -- the model read it as
evidence that somebody had, and invented a missed call **six times
out of six**, including on an evening with no calls at all.

Rewritten as a condition -- *"if, and only if, something below
shows that... where nothing below shows that, say nothing of the
kind: they will ring somebody back who never called"* -- and it is
clean six times out of six.

A worked instruction, stated unconditionally, is read as a fact
about the input. That is the third time this file records the same
shape of mistake.

## Reasoning is for the things nobody is waiting on

Owner: *"use a high reasoning model for this."* It was already on
the same model as everything else -- `fast_model` is empty, so the
greeting falls through to `claude-sonnet-4-6`. What it did not
have was permission to think: `reasoning_effort` is one global
setting and it is unset.

It is now per purpose, which is the right seam because the two
requirements are opposite. The same three evenings, measured:

| effort | film | dentist in Velachery, evening in Urapakkam | time |
| --- | --- | --- | --- |
| none | by name | *"how did the dentist go?"* | **2.2s** |
| low | by name | *"a long evening near Vandalur"* | 12-17s |
| medium | by name | *"how was Vandalur?"* | 23-27s |
| **high** | by name | **says nothing at all** | **29-46s** |

**Reasoning fixed the case that no wording could.** Told to be
more careful, the model gave up the greeting that worked and
turned the broken one into an accusation. Allowed to think, it
worked out by itself that it could not tell, and stayed quiet.

**And it cannot be switched on.** The budget at the door is five
seconds before the fixed text is said instead, so a reasoned
greeting would be written every time and heard none of the time.

So `Hard` carries the description of the person and not the
greeting. The description runs hourly in the background with
nobody standing anywhere, and finding what keeps being true out of
a week of talk is exactly the kind of judgement this buys.

**What the greeting needs is not permission but a head start.**
It is written when the room sensor sees him. His geofence says he
is home a minute earlier -- the walk from the gate. Begun there,
thirty seconds of reasoning finishes before he is through the
door. That is the next piece, and it is worth more than anything
else on this list.


## The head start does not exist, measured

The plan was to begin the greeting when they are known to be home
and speak it when they reach the room, so that reasoning has time
to finish. The automation looked made for it: it triggers when the
watch is first heard at -85 dBm, then **waits** for -65 before
telling the server, and the comment beside that wait describes
somebody still walking in.

Measured over seven arrivals, how long that wait actually is:

    0.1 s     0.2 s     1.2 s     2.4 s     14.5 s
    997 s     1775 s

Five of seven under fifteen seconds, four of them under three.
**There is no walk from the door**: the laptop is in the room they
arrive into, so the watch crosses both thresholds at once. The two
long ones are not walking either -- they are the watch audible
faintly from elsewhere in the house for twenty minutes before
coming near.

So the wait is not a head start and never was.

**What does fire early is the phone joining wifi**, and it answers
both of the owner's objections: *"I'll have my laptop in my office
too"* and *"what if there is no geofence"*. It is the same
mechanism in both places and needs no fence:

    Zoho-Guest      22 s before an office arrival
    Dhanush_EXT     731 s, 989 s, 1598 s before home ones

But twenty-two seconds is not enough for reasoning and twenty-six
minutes is stale, so it is a head start of wildly unreliable
length. Taking it means composing on a guess about when they will
walk in, and sometimes composing twice.

### And the budget itself

Owner: *"it's ok to wait 6 seconds."* So `Within` is six.

The last six real greetings, from the production log:

    reading_google 1.7-2.5 s     thinking 1.7-4.4 s

Five was leaving almost nothing in hand at the slow end. **Nothing
was actually overrunning** -- I said four in ten were timing out
and that was wrong: the four failures in the log say *"the provider
said nothing"*, a different fault, all on 2 October, and every
attempt since has succeeded. I had read a count of restarts as a
count of attempts.

Then: *"no max 10 secs."* So it is ten.

Ten buys headroom and not reasoning. Low effort was 12.1, 16.7 and
2.1 seconds on those same three evenings -- ten reaches it one time
in three -- and high costs 29 to 46. Thinking cannot be paid for
out of a doorway.

**One stale comment corrected on the way past.** The note beside
`Within` said Home Assistant gives up on the request at ten
seconds. The rest_command says `timeout: 180`, and has for some
time. Nothing was relying on the wrong number, but it was the
reason ten looked like a ceiling rather than a choice.


## The first look opened exactly where the silence ended

A battery at 25 per cent, marked by the phone to be said at once,
was never said. Owner: *"why it didn't say for 25%."*

    01:38:06   server restarted
    01:39:14   the 25 per cent arrived
    01:39:42   server restarted again
    01:44:42   the watcher's first look, reaching back to 01:39:42

**Twenty-six seconds outside the window.** It never became a
candidate, so nothing appears in `remarks` either -- the table that
exists to explain silence could not explain this one, because the
event was never weighed at all.

The cause is two constants that happen to be equal. The watcher
holds off `noticeFirst` (five minutes) after starting, so that a
restart mid-evening does not remark on whatever is arriving at the
time; then it looks back `Behind` (five minutes). The window opens
precisely where the silence ends, with **no margin whatever**, so
anything arriving during the hold is lost and any restart during it
loses more.

The first look now reaches back over the wait as well -- `Waited +
Behind` -- and only the first. Safe to widen because repeating is
prevented elsewhere: the gate weighs each label once per ten
minutes and an immediate one once per arrival, so the worst a wide
first look can do is weigh something already weighed, which it
declines.

**Fifteen restarts today made this likely; one crash makes it
possible.** And the thing it loses is from the one class of
announcement that exists because waiting costs something.


## A warning about a battery is a warning about running out

Owner, on the twenty-five percent announcement: *"it should not say
it when plugged in."*

It had been saying it on the way up. The thresholds are a regex over
`%BATT` -- `^(5|10|15|20|25|100)$` -- and a regex cannot tell which
way the number is travelling. Plug in at twenty-three and the phone
passes twenty-five a minute later; the announcement that means
*find a charger* arrives while holding one.

**Nothing in the server can fix this, and that is the point.** The
server does not know what a battery is: the phone sends
`battery.level` with `urgent` already decided, because the device
describes itself. So the fix is on the phone, in the condition that
decides urgency.

Tasker has no built-in charging variable -- the documented list has
`%BATT` and nothing else about power -- but the state profile built
last week already brackets exactly the interval we need. `*Power
Any` runs *Power Plugged* on entering and *Power Unplugged* on
leaving, so each now also sets a global:

    Power Plugged     Variable Set %CHARGING = 1
    Power Unplugged   Variable Set %CHARGING = 0

and *Battery Changed* action 2 became three conditions:

    %BATT ~R ^(5|10|15|20|25)$   AND
    %CHARGING !~ 1               OR
    %BATT ~ 100

Tasker evaluates left to right with no precedence -- the plain
`And`/`Or`, not the `High Precedence` variants it also offers -- so
this reads `(low AND not charging) OR full`.

**`!~ 1` rather than `~ 0`, deliberately.** An unset or cleared
`%CHARGING` does not match `1`, so the default is to announce. The
failure this guards against is the silent one: a variable that went
missing should cost a redundant warning, never a missed one.

**One hundred keeps its own arm** because it can only be reached
while charging, and it is the one battery announcement that is good
news rather than a warning. It left the regex so that the regex
means one thing -- the thresholds on the way down.

Verified: `A OK ID67.2 Power Plugged.Var Set, %CHARGING=1` in the
run log, and the variable reads `1` in VARS with the cable in. The
suppression itself is only observable the next time the phone
discharges past a threshold, and the hundred arm on the next full
charge.


## The judging half of the unasked remarks is gone

Owner, 5 October 2026, reading the Unasked page -- *"Sir, your phone
has been unlocked three times in the last few minutes"*, said aloud
at 09:47 and held at 09:50:

> *"no need for these kind of unasked announcement. Currently have
> only the immediate and greeting alone."*

`notice` could offer three reasons for speaking unprompted: **Often**
(three of a thing inside fifteen minutes), **Novel** (nothing like it
in four weeks), and **Immediate** (the sender marked it). The first
two are gone. What is left is: a device said this has to be known
now, somebody is in the room, say it.

**The premise was wrong, and the screenshot is the proof.** The
design assumed an unusual event and an event worth interrupting
somebody for were the same thing. They are not, and nothing in this
server can tell which it has -- it deliberately does not know what a
screen or a battery or a call is. Six unlocks in ten minutes is
unusual and is *also* the most ordinary thing a person does with a
phone. Only the sender knows the difference, and the sender already
has a way to say so.

Everything that existed to serve the two judgements went with them:

- `Burst`, `Insistent`, `Candidate.Times`, `Candidate.Before`
- `Toggling`, which existed so that a screen reporting locked and
  unlocked fifty times a day could never be *frequent*
- `measured`, which existed so that a battery at seventy-three per
  cent was not *novel*
- the gate's `Quiet` (twenty minutes between remarks), `Over` (once
  a day per thing), `Spoke`, and the refusal wording that named what
  beat a remark to it
- the half of the `Remark` prompt that invited the model to answer
  `NOTHING`

**801 lines out, 228 in.** `Gate.Allow` lost its candidate argument
along the way: nothing about the thing itself can hold it back any
more, so the only question left is about the room.

### A look got cheaper by four weeks

`Find` took `history` to decide whether a thing was familiar, so
every tick -- **every thirty seconds, for ever** -- read four weeks
of events, capped at 4000 rows. Reading a mark the sender already
wrote needs the last five minutes. The cap came down to 500, and the
only thing that still wants four weeks is the question of *whose*
events to read at all, which is the caller's.

This was not the point of the change and is the largest single
saving in the server.

### What is kept, and why

`Candidate.Immediate` is always true and is still passed rather than
assumed. The gate reads it to know the room is the only rule; a gate
that inferred that from the absence of anything else would start
announcing the day anything else appeared. `Reason` is still a type
with one value, because it is written into every row of the Unasked
page and a column that has only ever held one word is how the next
person knows there was never a second kind.

`conversation.Remark` stays. The kind is in the database and in
transcripts already written; nothing new will carry it.


## The greeting opens with the greeting

A greeting arrived reading, in full:

> *"Kliffy rang while you were out, sir -- came through just before
> one."*

Owner: *"it should say the good morning / afternoon / evening and
welcome backs at the beginning."*

Nothing had ever told it to. The fixed sentences -- *Good evening,
sir.*, *Welcome back, sir.* -- were the **fallback**, said only when
the model failed or ran out of time. When it succeeded, the whole
greeting was whatever it wrote, and whether that began with a
salutation was left to it. Sometimes it did. Often it opened on the
news, which is a sentence somebody shouts across a room, not a
greeting.

**Written by the server and put in front, not asked for.** `Told`
loses `Fallback` and gains `Opening`, which is the same string doing
one job instead of two: the first words, spoken whatever happens.
The model is told it has already been said and asked for what comes
after.

Asking for it would have worked most of the time, and that is the
objection: the failure is silent and nobody reads the greeting
afterwards to catch it. This is the rule about enforcing in code
before enforcing in the prompt, and the prompt line is still there,
because a model that knows the greeting has been said writes a
better second sentence than one that is merely prevented from
repeating it.

**`Welcome back` left the rota.** It used to be a third alternative
in each band, so a greeting had the salutation *or* the welcome and,
once the model was writing them, usually neither. It is a constant
now, said after whichever salutation came up. Safe because nothing
at this door is anything but an arrival: presence sleeps when the
laptop does, so an absence it never saw produces no greeting at all.

**The stripping is the guard.** `greet.Without` takes a salutation
back off the front of the answer when the model writes one anyway --
including "Evening, sir -- ..." with no full stop, and a doubled
"Good evening, sir. Welcome back." The phrases it recognises are
read from the same table the server writes from, not from a list of
English pleasantries invented for the purpose, which would be a
keyword match that fails silently and would one day eat a real first
sentence. For the same reason the table moved into `internal/greet`:
two copies agree until one is edited.

**An empty answer stopped being a warning.** The prompt now says to
answer with nothing where nothing stands out, and most arrivals are
that. The opening is then the whole greeting, which is what it was
built to be.


## Four rules taken from a second opinion, and the rest left

The owner had the whole greeting -- every event kind with its counts
and payload fields, the budgets, the prompt as it stands, the known
defects -- written up and put to another model, and brought back a
three-thousand-word production prompt. Four of its ideas went in.
The rest did not, and why is worth more than the four.

### What went in

**A scale for the judgement.** The prompt already asked the model to
"judge it as a person would" and gave it nothing to judge *on*, which
leaves two moves: say it or do not. The useful third is to ask. So:
a thing recorded outright may be said; several things pointing the
same way with no one of them settling it may be *asked about*; one
ambiguous thing is silence, and the doubt is not read out either.

The line that carries it is the owner's own case -- *"the whole of
the difference is between asking how the film was and telling them
they went to it: the first is still worth saying when it is wrong,
and the second is not."*

**Evidence is not a subject.** Most of what a phone reports is how
you work out the one thing worth saying, not the thing itself. The
test given is whether somebody would ever raise it with a person who
had just walked in; where they would not, it is evidence, and it is
used to reach something and then left out of the saying.

The second opinion stated this well and then listed the kinds it
meant -- unlocks, wifi, bluetooth, battery. The list is exactly what
this file records as wrong three times over, so the principle went in
and the list did not. There is now a test that fails if any event
kind is named in the instructions at all.

**Nothing proves an absence.** The barber is in the diary for nine
and the phone has them at home all morning. The old rule was blunt --
where nothing lines up, say nothing -- and it was blunt because an
earlier attempt at this turned the same case into an accusation.

The distinction that makes it safe is not confidence, it is what the
data *is*: a device reports where it was and says nothing whatever
about where it was not, so a quiet hour is as likely to be a quiet
phone. Never conclude they did not go. But where the record does
cover those hours and plainly has them elsewhere, ask whether they
got to it at all -- lightly, the way somebody asks who has no stake
in the answer.

**A name is worth no more than what handed it to you.** One off a
device may be a nickname, a mishearing or the wrong person; where it
says unknown, it is unknown. Say *somebody*. They will say who. This
is the `Kliffy` case, and the point is that a wrong name is not a
hedge -- it is a different person.

### What did not

**An entire tool-use policy, for a model with no tools.** Four
sections of it, down to a worked example turning "someone tried to
reach you" into a name via a contact lookup that does not exist. The
greeting is one call, no tools, ten seconds; a tool loop is two more
round trips inside a budget a single call already spends up to 4.4
seconds of. Written in, it would have been the third rule in this
file that exists and can never run.

**Three times the length.** Twenty-four numbered sections with §3,
§21 and §24 all saying "one thing only". Prompt size is paid for in
time to first token out of the same ten seconds, and a document that
long asks a model to be thorough when the whole instruction is to be
brief. What went in added about two hundred words.

**A worked answer instead of a rule.** Its section on several
candidates at once resolves one specific combination -- three missed
calls against a reminder -- rather than stating that somebody
actively trying to reach you outranks a machine's schedule. A worked
case teaches the case.

### Not yet measured

All four are beliefs. The barber one is the one to watch, because it
is the nearest neighbour of a change that went wrong earlier the same
day, and the difference between asking and accusing is a matter of
tone that a prompt cannot guarantee. The snapshot set that would
settle it -- a quiet arrival, the film afternoon, the missed calls,
the barber, the empty diary, run before and after -- is the next
piece of work and was deferred to do these first.


## Sampling real arrivals, and the three things it found

The owner asked for a sampling test rather than a set of situations:
take an accumulation of events that actually happened, run the model
on it, and see. `make evals-greeting` picks arrivals out of the four
weeks the phone has been reporting, builds exactly what the server
would have built at that moment, asks the real model, and prints what
came back with the seed so a sample can be run again.

Nothing asserts the words, because a greeting has no right answer.
It asserts the few things that are wrong whatever the words are: a
name nobody reported, a call nobody made, an inference stated as a
fact where there was something to infer, an absence put as an
accusation, a second salutation, a tally read out loud.

### Three of the checks were wrong before the greeting was

Worth recording, because every number the first runs produced was
worthless and none of them looked it:

- Sampling uniformly over four weeks put every arrival in September.
  **The events begin on 1 October.** Eight empty windows measure
  nothing, and the output read as though the model had a problem
  with quiet days.
- A capital at the start of a sentence was read as a name, so
  *Someone*, *Today* and *Looks* were reported as invented people.
- A number was read as a count when it was the hour, and when it was
  the title of a film -- it flagged *"How was Meesaya Murukku 2,
  sir?"*, which is the best greeting in the sample. Narrowed twice:
  a later run read *"five this morning"* as a count because *this*
  ends in an s.

### What it found

**The cross-check works.** On the real evening of 4 October -- diary
saying *Meesaya Murukku 2 Movie* at 18:10, the phone recording a
hundred and seventy minutes at *Flavors non veg restaurant* a
hundred and twenty-seven metres away -- it asked *"How was Meesaya
Murukku 2, sir?"* Twice, independently. That is the thing the owner
asked for, on data neither of us arranged.

**It invented missed calls, three times in twelve.** One window held
four wifi events and nothing else; one held nothing at all, and that
one invented a voicemail as well. This is the failure the prompt
names as the worst it can do.

The cause was not wording. The block saying a missed call outranks
everything was in **every** prompt, so it stood there as a standing
suggestion of what to look for, and a model with nothing to say
filled the slot with the highest value in it. It had already been
made conditional in its wording after inventing a caller six times
out of six; that helped and did not finish it. The condition is in
the code now, where the diary's always was.

**It invented reasons for silence.** *"Did it go flat?"*, *"was the
walk long?"* -- over windows that reported neither. The silence block
was handing it over: it named off, flat and no signal as the usual
reasons and then allowed the model to say whichever seemed plain. A
gap is working material now and never a subject. They know what
their phone was doing better than it does.

**It never says nothing.** Twelve of twelve, and twenty-two of
twenty-two before that. The instruction to return nothing had been
there all along, as one clause inside a paragraph about finding
something, which is not where a model looks for permission to stop.
A block of its own naming what having nothing looks like -- the shape
of the day, the hour, how the phone behaved -- did not move it
either.

Invented calls went from three to nought on the identical sample.
The silence rate did not move at all, and what is left is reaching
rather than inventing: the hour they came in, a plan for tomorrow
evening at breakfast time. Whether that is wrong is the owner's
judgement rather than a defect, and it is still open.

### A page with nothing under it is not put to a model

The last fix of the three, and the cheapest. Where the window, the
diary, the reminders and the chores are all empty there is nothing
to write a greeting from, and the opening was always going to be the
whole of it. Asking anyway spends a call and a doorway's worth of
waiting on a prompt whose only honest answer is silence -- and the
one sampled arrival with an empty window answered it with a missed
call that never happened.


### More of a thing is still that thing

Owner, on the greetings that were left: *"talking about switching
wifi is not a better humany way of response."*

Quite right, and the prompt was licensing it in so many words. It
said that something happening dozens of times a month is the texture
of their life and not news -- and then, in the next clause, that what
is worth remarking on is what is rare **or far more of something
than there usually is**. A phone moves between a router and its
extender seventeen times in a morning. By that second clause it
qualified every time.

So the second clause went. More of it is still it: a count higher
than usual is the same thing more often, and the mechanics of a day
-- what a device did because somebody was somewhere, or moving, or
awake -- are never the subject whatever the number says.

**With one exception, and it is the one that matters.** Anything
with another person at the other end of it. Somebody doing a thing
again and again is somebody; a device doing a thing again and again
is a device. That keeps the case the whole feature was built for --
the same caller, four times -- without naming a kind of event to do
it.

On the same twelve arrivals this was the first run with nothing
flagged at all, and the character of them changed rather than the
count: *"You were on the phone with Alekhya just a few minutes ago,
sir -- is everything all right?"*, *"Were you and Alekhya up late
talking, sir?"*, the film, the plan with Vishnu. The device
mechanics are gone.

Twelve of twelve still spoke. What is left is thin rather than
mechanical -- the hour they came in, how late they were out -- and
whether that is worth saying is still the owner's judgement to give.

### Eleven refusals read as eleven silences

The greeting had been answering "There was a call came in just after
half one, sir" -- four times in five on one window. Owner's own
version of the same greeting was *"You were on a pretty long call
last night. Everything alright?"*: shorter, grammatical, and a thing
a person says. Right judgement, bad speech.

Two lines went in for it: that every sentence must be one, with the
offending construction quoted, and that a length of time is a count
like any other and is no more said than one.

**Then the measurements went to pieces, and the fault was mine.**
Runs began coming back empty -- two in five, then six in six, then
five in five with the lines reverted, and five in five with the
*committed* prompt that had spoken five times out of five an hour
earlier. Three conclusions were drawn from that and all three were
wrong, the last of them being to revert a change that was working.

They were never silences. The provider was rate-limited:

    code="bad_request"
    detail="You have made too many requests continuously."

The harness had always reported it, through `t.Errorf`. But a
failure goes to the test log and an answer goes to stdout, and the
command filtering the output for answers -- `grep -E "   → "` --
dropped every one of them. The signal was there and the filter
removed it.

**A refusal and a silence are the same empty string to everything
downstream, so they have to be told apart loudly and in the same
place.** The harness now prints `THE PROVIDER REFUSED -- not a
silence` where the answer would have gone, with the detail beside
it, and fails hard rather than carrying on.

The same run showed the other half of it: one call reached the
ten-second deadline and returned nothing at all, which in the server
is the fallback path and in a measurement is another empty string
that is not a silence.

With the lines in and the provider answering: three replies, all
grammatical, none reading a duration out loud, and all three about
the right thing.


## An ordinary day gets a section

Owner: *"the profiling should have the daily routine section -- it
should be able to profile my daily activities."*

The material was already being asked for and had nowhere to live.
WHAT IS SO holds one countable line per thing, ordered by how well
established it is, so a day arrives in it shuffled: morning silence
ends between 7:27 and 9:57 on one line, thirteen hours away on
another, a call around eleven at night on a third. WHAT THEY ARE
LIKE had *"and their weeks: what they do most days, where they go
and when"* tacked to the end of a hundred and fifty words about
everything else.

So: **AN ORDINARY DAY**, between the two. The shape of one in the
order it happens, ten lines at most, each an hour or a stretch and
what is usually true of it. Hours as they would be said. Weekdays
and the weekend named apart where the evidence tells them apart,
and said to be indistinguishable where it does not, rather than
averaged into a day nobody has. Every line marked with how sure it
is, and an hour nothing is known about said to be unknown -- which
is worth more than a guess, because this part exists to be **the
thing an unusual day is measured against**.

That is also why it is the greeting's section more than the
profile's. Telling an ordinary evening from an odd one is the whole
of what the greeting does, and until now the baseline for it was
spread across two parts written for other purposes.

The old sentence in WHAT THEY ARE LIKE is gone, so the two do not
compete for the same hundred and fifty words. That part now covers
their weeks -- which days differ and how -- and leaves the day
itself above.

### It could not have been added without raising the cap

The description stood at **3,988 runes of the 4,000** a memory is
allowed. Twelve characters of headroom, which is not a description
that had said what there was to say; it is one that stopped. The
next thing worth adding would have failed `Valid()` and the symptom
is a profile that quietly stops being rewritten -- which is exactly
what happened on 4 October.

Raised by who writes it rather than by what it says.
`memory.MaxBody` stays at 4,000 for everything a person asked to
have kept, because a memory is a fact and anything longer is a
conversation that wanted condensing. The standing description is
the one memory that is a document on purpose, so
`memory.Written()` builds it and `MaxWritten` allows eight
thousand. The flag is not stored: it decides how long the body may
be and nothing else.


## Looking in one place and calling it everywhere

Asked *"do you know anyone called Gunalan?"* it answered:

> *"I have a record that Gunalan's birthday is on 5 February, sir.
> That is all I know about them -- you marked their birthday in your
> calendar and confirmed the spelling... Beyond that, I have no other
> information about who Gunalan is."*

He had rung Gunalan at 19:12 that evening. The event was in the
table while the sentence was being written.

Owner: *"it looked only calendar, the events tool which is the
powerful information tool about me, but it didn't search it. If I
ask something check if it's in my sources -- calendar, task, events,
reminders -- else check its own memory, else do a web search. It
should never say I don't know."*

**The tool was not at fault and neither was its description.**
`event_recent` takes a `contains` and says a person is one of the
things to put in it. What it does not say is that a question about
*who somebody is* belongs there, because the whole description is
framed around what the person has been **doing**. A name is not a
doing, so the question routed to the calendar and the memories and
stopped at the first source that answered.

Two changes, and the division between them is the standing one: a
rule that spans tools goes in the persona, and a rule about when to
reach for one goes on that tool.

**In the persona.** Asked about a person, a place or anything of
theirs, look everywhere that could hold it before deciding what you
have is all there is -- what they asked you to remember, the diary,
what is still to be done, what is due, and everything their devices
reported. *Finding it in one of those is not a reason to stop: the
diary knows a birthday and the devices know they rang the person
this afternoon, and the second is the half worth having.*

**On `event_recent`.** That a question about a person is half
answered here and nowhere else -- somebody they rang, or who rang
them, is in here under their name even when nothing else has heard
of them.

### "Never say I don't know" is not quite the rule

Not knowing is no failure and the persona already insists it be said
plainly. What is false is *"that is all I know"* after one source
out of five. So the line written is that distinction rather than the
instruction as given:

> Not knowing is no failure and is to be said plainly. Saying that
> is all you know, when you have looked in one place out of five, is
> a different thing and it is false.

### And the web is not the fallback for this

`web_search` already says *"not for anything about them"*, which is
right twice over. A search for Gunalan returns nothing about this
Gunalan, and it sends the name of a private contact to a third
party to find that out. The persona now says where the fallback does
apply -- a fact about the world -- and where it does not: *their own
life is never looked up out there.*


## What the evals said, and what they could not say

The suite ran properly for the first time since the morning, with a
deadline long enough to finish.

**Tool choice: 99 of 99.** That is the one the three description
changes of that evening could have broken, and it was 81 of 82 in
the morning. Nothing over-reached.

Four others failed, and only one of them turned out to be worth
anything.

### A test stricter than the rule it tests

`TestItAsksHowAHeardNameIsSpelt` wants every heard name to produce
"how is that spelt?" It failed **before** the persona change as
well as after -- 3 of 5 against 2 of 5 -- so it was not broken that
night. What the change did was shift *which* cases fail, and the
two it moved are both questions about the world: who directed a
film, what somebody is known for.

That is the new line working. Asking how to spell Thondimuthalum
before looking up its director is worse than looking it up, and the
owner's own standing rule says so: **guess outright when reading,
ask only when changing.** Three of the five cases are writes and
should ask; two are reads and should not. The genuine failure,
identical in both runs, is *"remember that Sowmiya is my sister"* --
a write that settled on a name.

So the eval encodes a stricter rule than the one that was stated,
and the fix is to split reads from writes in the eval rather than
to take the line out. Recorded with the usual caution: *the test is
wrong* is the convenient conclusion, and the justification here has
to be the owner's rule, which predates the eval, rather than a
preference.

### And one that could not be measured at all

`TestItDoesNotStateWhatItCannotSee` failed after the change, 4 of 6.
The before-run came back in five seconds with "got no usable
answer", which is the gateway refusing rather than the assertion
failing, after a night of being leaned on. **No baseline, so no
verdict.** It is the one to check first with a rested gateway,
because it is the only one where the change could have made the
assistant less honest rather than more useful.


### A reminder is also a phone alarm

Owner, 6 October 2026: *"whenever a reminder is set in Jarvis, that
reminder should be kept as a phone alarm, and if it could not reach
it should say I could not set the alarm."*

A reminder is a thing the assistant **says**; an alarm is a thing the
**phone does**. The difference is the whole point. At six in the
morning the satellite is a speaker in a room that may be off, the
person is asleep, and the only thing that is going to work is the
phone in their hand. So every `reminder_set` asks the phone for a
real alarm at the same moment, under the same words.

**Both outcomes are said, and neither is silent.** Somebody who
believes an alarm is set and finds at six that it was not has been
worse served than somebody told plainly that it was not.

| state | what the model is told |
|---|---|
| set | an alarm was asked for -- say so in passing, not as a second sentence |
| unreachable | it is not set *yet*, and will keep being tried |
| no address configured | there is no way to reach their phone, so no alarm was set |

The raw reason never reaches the model; it goes to the log, and is
given only if asked.

### Straight to the phone, not through Google

The first build went out through Home Assistant's companion app with
`command_activity`. It worked, and it was measured working. It was
still the wrong route, for a reason the owner spotted: **the
reminder's title goes to Google and back.** "Call Gunalan: the
invoice" is the person's own business, and this project already
treats words leaving the house as a cost -- `notify` is switched off
on those grounds.

The phone is on the tailnet (`100.76.164.70`, Tailscale 1.102.3), so
there is a stable address reachable from any network. The server
POSTs straight to it. Nothing leaves the tailnet and Home Assistant
drops out of the path entirely.

Rejected on the way: an earlier note here claimed Tasker's HTTP
server was unusable because it is local-network-only and DHCP moves
the address. **That was wrong** -- Tailscale answers both -- and it
is recorded because the reasoning, not the conclusion, is what
misled.

### What Tasker can and cannot do, measured

- `Set Alarm` takes **Hours, Minutes, Label, Sound, Vibrate,
  Confirm**. No repeat days.
- `Send Intent` extras take a **Java object**, not a typed literal.
- **`Java Code` takes arbitrary Java.** This is the one that matters,
  and ruling out the repeat before finding it was the second time in
  this feature that an early conclusion was wrong.

So the task is two actions: `Variable Split %http_request_body` on
`|`, then one `Java Code` action that builds the intent itself,
including `putIntegerArrayListExtra` for the days. The phone holds
daily, weekdays and weekly repeats **natively** -- they keep ringing
with this server switched off, which is the whole point of putting
them there.

Monthly is still re-armed by `remind.Loop.rearm` one occurrence at a
time, because an alarm clock has nowhere to put "the fourteenth".
The loop skips every repeat the phone *can* hold, or it would stack
a second identical alarm beside it every morning.

Because the label is pasted into a line of Java and Tasker expands
`%variables` in the field first, the server strips `"`, `\` and
`%` from it as well as `|` and newlines. Any of them would end the
string literal or name a variable, and the alarm would simply not
be set.

### The wire

`POST http://<phone>:1821/a/<secret>` with a body of
`hour|minute|label|days`, where days is `1;2;3` counting Sunday as
1, or `0` for one time only -- a value rather than an empty field,
because a trailing empty one is not reliably one of the pieces.

- **A pipe**, because a reminder title will contain a comma and a
  colon and will not contain a pipe. Any that turn up are replaced,
  not escaped: a label is for reading at six in the morning, not for
  parsing. Newlines go too -- Tasker reads one line.
- **The path is the shared secret.** Tasker's HTTP Request event
  matches it exactly, so authentication costs no extra action. The
  listener binds every interface, so without it anyone on the office
  wifi could set alarms. It lives in `config.ini`.
- **The reply is checked** against `jarvis-alarm-ok`, Tasker's Quick
  Response. Anything can answer an HTTP request; a captive portal's
  cheerful 200 would otherwise read as an alarm that was set.

Tasker side: profile **HTTP Request 1821, POST, /a/...** -> task
**Jarvis Alarm**, two actions -- `Variable Split %http_request_body`
on `|`, then `Set Alarm` with `%http_request_body1/2/3`.

### It keeps trying

Owner, same day: *"if reaching the phone failed, then it has to be
retried a few times, before the actual alarm time."*

A phone is asleep, off the tailnet, or in a lift far more often than
it is broken, and all of those fix themselves in minutes. So
`alarm.Keeper` wraps the sender: the first failure is still
**reported** -- the person is told the alarm is not set yet -- but it
is retried every 2 minutes behind the answer.

Two bounds, both needed. **The clock**: trying stops a minute before
the alarm's own time, because an alarm for a moment that has passed
helps nobody and one set seconds beforehand rings while they are
still reading that it could not be set. **A count** (20): a phone
that answers and *refuses* -- a profile switched off, a changed
secret -- must not be retried until morning.

Keyed by reminder id, so one amended three times is owed one alarm.
Held in memory: a restart loses outstanding retries, which is
accepted rather than given a table, because the window is minutes.

### The alarm was set in the wrong zone

Owner, 6 October 2026, after the first real reminder: *"i think the
time is set wrong in alarm."* It was. A reminder for 7:12 am set an
alarm for 1:42 am.

`remind.New` stores `DueAt` as **UTC**. The spoken answer converted
it -- `spell(stored.DueAt, clock.where())` -- and the alarm did not.
An alarm clock holds a wall clock, so handing it a UTC instant sets
it five and a half hours out.

**The test passed throughout.** It asserted
`p.got.At.Equal(time.Date(..., 6, 5, ..., india))`, and `time.Equal`
compares *instants*: 00:35 UTC and 06:05 IST are the same moment, so
the wrong zone sailed through. Only the hour and the minute ever
reach the phone. The assertion is now on `.Hour()` and `.Minute()`,
which fails loudly on the old code, and the same assertion guards
`remind.Loop.rearm`, which had the conversion right but was untested.

The general shape, worth carrying: **assert what crosses the
boundary.** A test that checks a richer value than the wire carries
will keep passing after the wire starts lying.

### Cancelling and moving a reminder, and what cannot follow

Owner, 6 October 2026: *"if I cancel the reminder, update the
reminder, it should update from the alarm too."*

Measured against the phone, because the answer is not symmetrical:

| | one-off | repeating |
|---|---|---|
| create | yes | yes |
| **remove** | `DISMISS_ALARM` by label deletes it | **impossible** |
| **move** | remove, then create | **impossible** |

Dismissing a repeating alarm skips a single occurrence, and sending
it again makes a **second** alarm rather than changing the first.
Android offers nothing else. So a repeat cannot be taken back from
outside the phone at all.

**The same property both ways.** A repeating alarm survives this
server being switched off -- which is the reason for using one --
and that independence is exactly why nothing here can reach in and
remove it. `alarm.Holds` is the one predicate for both facts.

The owner chose to keep native repeats and be told. So:

- A one-off reminder cancelled or moved takes its alarm with it.
- A repeating one leaves its alarm standing, and the assistant says
  so in a clause -- *delete it in the Clock app* -- rather than
  letting it wake them for something they called off.
- A change the alarm cannot see, such as new spoken words, touches
  the phone not at all.

**Two paths, two secrets.** `/a/<secret>` sets, `/x/<secret>` takes
back. Separate paths rather than a verb in the body because Tasker
matches the path exactly, so it costs no extra action on the phone.

**`alarm.Settle`, and why it exists.** Taking one back and putting
the next on are two starts of the clock's own activity, and back to
back **the second is silently dropped** -- the phone still answers
`jarvis-alarm-ok`, nothing logs a failure, and no alarm appears. A
move lost its alarm entirely until a 1.5 second wait went between
them. The same shape bit once more, setting two alarms in one
breath.

That is the standing weakness here: **the phone answers before it
acts**, so a success is the message having been accepted, not the
alarm existing. Everything above is built knowing it.

### The alarm was beating the voice

Owner, 6 October 2026: *"when the reminder comes, the phone alarm
rings first, but you said time to wake up late."* Two causes, both
real, found in one trace: a one-minute reminder set at **02:33:33**
was spoken at **02:34:36.7**.

**The seconds were being dropped.** An alarm clock holds an hour
and a minute and nothing smaller, so a reminder at 34 past and 33
seconds became an alarm at 34 past -- ringing **33 seconds before**
the thing it was for. `alarm.OnTheMinute` rounds **up** instead, so
the alarm can never precede its reminder. A time already on the
minute is left exactly where it is, or every ordinary reminder
would slip a minute late.

**Saying one is not instant.** The look at the clock, the call to
Home Assistant, Piper, then the satellite: about three and a half
seconds, against a phone alarm that rings exactly on its minute. So
the alarm led the voice every single time and the voice sounded
like an afterthought. `remind.Loop.Lead` (4s) picks a reminder up
slightly before its moment, so the sentence *lands* on the moment
rather than starting there.

Worth keeping straight: the poll was never the problem. It runs
every two seconds. The delay is the speaking, and no amount of
looking more often fixes that -- only starting sooner does.

### The signature

Owner: *"always add a stamp like 'by J.A.R.V.I.S'. If Jarvis is the
name, FRIDAY if FRIDAY."*

`take the tablets -- by Jarvis`, with an em dash. The name comes
from `[assistant] name` and is **never** written down in the server;
the owner's own example settles the styling question, since FRIDAY
stays FRIDAY rather than becoming F.R.I.D.A.Y. Nameless signs
nothing rather than signing "by".

Verified end to end against the phone on 6 October 2026, through
`alarm.Phone` with the real config: label, colon, comma and em dash
all intact. Every test alarm was deleted afterwards and the owner's
own two were left alone.


### Remark is gone, and its six messages are not

The kind the assistant used to raise by its own judgement --
*"your phone has been unlocked three times in the last few
minutes"* -- had been unreachable since the judging was removed on
5 October. The branch that chose it still compiled and could never
run.

Removed from everything that can raise one: the policy table, the
writer's `Remarked`, and the branch in the notice watcher, which
now always says `Immediate` because that is the only thing it can
be.

**The constant stays, listed in `known()` by name.** Six remarks
were really said, in conversations somebody can still scroll back
to, and a stored kind the store does not recognise is a message it
refuses to read -- which is the exact fault the comment above
`known()` already records, where an interruption was rejected for a
morning. A kind that cannot be raised and can be read is the
smallest honest residue.

Relabelling those six as `immediate` was the alternative and is
worse: it would assert a device asked for them, which is false.
They were the assistant's own idea, and that is the thing worth
remembering about them.

### A device that stops talking

Events stopped reaching the server at 11:56 on 6 October 2026 and
nobody noticed until the owner asked at three. A phone that reports
every few minutes said nothing for three hours and the server had
no opinion about it.

**Worth saying because the failure is invisible from both ends.**
The phone was fine and believed it was sending; the server was fine
and had nothing to report on. Only the gap between them was wrong,
and nothing was looking at the gap. `situation.Quiet` looks at it.

**Measured on `ReceivedAt`, not `OccurredAt`.** This is the whole
trick. A phone off the network spools what it sees and delivers the
lot on reconnection, so by the time anybody reads the events they
look perfectly continuous -- the three hours are only visible in
when each one *arrived*.

**Forty-five minutes**, from the data rather than a guess: across
five days the gap between one report and the next is 2m11s at the
median and 12m37s at the ninety-ninth percentile.

**Position readings are now included in the window.** They used to
be omitted as noise, and that number only holds with them counted.
Without them the ninety-ninth percentile is **2h8m**, because a
phone with the screen off and nobody calling genuinely says nothing
for hours. Position is the one thing it never stops reporting, which
makes it the heartbeat. Measuring on the filtered window would have
produced a threshold of hours, or twenty-seven false alarms in five
days at forty-five minutes. The other two checks filter by kind and
never see a fix.

Only devices with at least five reports in the window qualify.
Something that reported once and stopped may have had one thing to
say; only constant talkers have a silence that is evidence.

### What the outage itself turned out to be

Worth recording, because the first diagnosis was wrong. The spool
shrank from 6.8 KB to 583 bytes and that was read as the flush
discarding events it had failed to send. It was not. `Jarvis Flush`
is correctly written: it *moves* the spool to `jarvis-sending.jsonl`,
posts that, and deletes it only `If %http_response_code ~ 200`. The
77 lines were sitting safely in the sending file the whole time.

The actual cause was the office network. From the phone: the
laptop's tailnet address and its LAN address both gave 100 per cent
packet loss, while 1.1.1.1 answered in 80ms. `Zoho-Corp-TLS`
isolates clients, and Tailscale could not get through either --
`active; relay "blr" ... tx 12480 rx 0`, sending and receiving
nothing.

The lesson for next time: a file getting smaller is not evidence
that anything was lost. Read the task before diagnosing it.

### The greeting goes back to a fixed sentence

Owner, 6 October 2026: *"currently the greeting is fully dependent
on AI, let's make it simple as hardcoded string. We will move it to
an algorithmic structure rather than full reliance on the model."*

This turns off the greeting measured earlier the same day from five
of twenty to twenty-four of forty. It is a setting, not a deletion:

    [assistant]
    greeting = plain | written

Plain is the default now. It was never removed from the code --
`compose` has always fallen back to it when there is no writer, so
this switch is the path that was already there and already tested,
not a new one.

**Why keep the written one at all.** Whatever replaces it has to
beat something, and "the model wrote it" is the only measured
baseline there is. Deleting it would leave the algorithmic greeting
with nothing to be compared against, and the evals in
`internal/greet/evals` with nothing to run.

What plain says: a salutation for the band of the day -- *Good
morning, sir* or *Morning, sir*, never the one used last -- then
*Welcome back*, then anything that was held back while they were
out, in words rather than as a count.

The next step is the algorithmic one, which is a third value for
that setting rather than another reversal.

### The server decides which readings matter

Owner, 6 October 2026: *"the urgent shouldn't be coming from the
Tasker, the Tasker just sends the events, the server should decide
at what value it should send event."*

This reverses the rule `internal/notice` was built on, and the
reasoning for the old rule was sound: which battery level is worth
a warning is the person's own preference, and the device that
measured it is the only thing that knows. It lost to a practical
fact. Changing the number meant an adb session editing a Tasker
profile by hand, and **a rule that is painful to change is a rule
that stays wrong**. The phone now only reports what happened.

The cost, stated: the server has to know that a battery has
levels, and a brand new sender cannot be announced without a line
of configuration. That is the trade.

**What is kept is the shape.** A rule names a *label* -- the same
kind-and-value a sender describes itself with, and the same one
the counted half of the profile uses -- so nothing in Go names an
event kind. `config.ini` does:

    at_once = battery.level 25 falling, battery.level 20 falling, ...

One line, always. config.ini has no line continuation, and a
wrapped value is read as a key with no delimiter and stops the
server on the next restart.

**Direction, not a charging check.** A phone charging up through
fifteen per cent is not a phone running out, which the Tasker
profile used to suppress by asking whether the charger was in.
Comparing against the reading before it is the shorter way to the
same answer, and needs no power events in the window.

**Unknown direction says nothing.** The first draft fired when
there was nothing to compare against, which made "falling"
meaningless at the edge of the window -- a phone sitting still at
twenty per cent announced itself every look. Its own test caught
it. That is why `Behind` went from five minutes to thirty: a phone
reports every four to nine minutes, so five minutes often held one
reading and no direction at all.

**What the data said.** Across five days: 26 event kinds arriving,
only `battery.*` ever carried the old flag, and it was true eight
times -- 25, 20, 15 on the way down and 100 on the way up. The
configuration above is exactly that, which is deliberate: this
changed where the decision lives, not what it decides.

### An announcement is published, not delivered

Owner, 6 October 2026, a few hours after the outbox was built:
*"one event announcement model, we will rewrite a new one. An
announcement has to be published to the client, like a pub sub.
Each client is subscribed ... the typed clients receive it, the
voice client also receives it but to say or not depends on the
presence, and presence logic should reside on the client."*

This supersedes the deliverer below, and the reason is worth
keeping because the first version was wrong in a way that looked
right.

**"Was it announced" is not one fact about one person.** The outbox
held a single verdict per announcement, and decided it centrally
from a presence sensor that describes **one room**. But the same
warning can be read on a laptop and missed by a speaker in an empty
room, and both are true at once. A single answer could not say so,
and the server was the wrong place to decide: it knows about a
sensor, and a client knows whether anybody is in front of **it**.

So the state moved to where the knowledge is. An announcement is
published once; every client gets its own copy with its own
verdict. `owed` is the announcement, `owed_clients` is what each
client did about it.

| | |
|---|---|
| typed client | presence is always true -- a screen needs nobody in front of it. Marks its copy said on arrival. |
| voice client | decides from its own trigger, the watch. Says it, or leaves its copy pending. |

**Pending is the default, not a report.** A voice client in an empty
room says nothing and writes nothing, and the announcement is still
waiting when the watch comes back into range. One fewer call, and
nothing is lost if that call never arrives.

`GET /v1/announcements` returns what *this* client owes -- this
client, not this person, which is the whole point. `POST
/v1/announcements/{id}` takes back `said` or `dropped`. The attempt
count is kept by the server rather than sent by the client, because
a client that reports its own count can report anything and the
count is what bounds a thing that fails every time.

**What is left on the server** is the one question a client cannot
answer, because it is about a moment rather than a device: has this
gone past being worth saying at all. A voice client switched off
for a week otherwise comes back to a hundred stale warnings. That
is all the Sweeper does; it never speaks.

Published to nobody is refused loudly rather than written down and
forgotten. An announcement with no audience is a watcher shouting
into a server with no clients, and the silence that follows should
have a reason.

No model call anywhere in this path, by the owner's instruction.
The sentence is settled when the announcement is published.

**The honest state after this change:** situations publish, and
**nothing collects yet**, so they are recorded and not heard. That
is the next step and the one that matters most.

### Announcements have one way out

Owner, 6 October 2026: *"it feels like everything is scattered ...
there should be a framework on how these should be done, like what
if an event is not announced due to any issue -- the server is off
or the phone is off."*

The scatter was real, and uneven. Six watchers decided to speak,
and each **recorded that it had spoken afterwards**. So a failure
between deciding and speaking left nothing behind at all: the
situation, the leave-by, the thing a device said to announce at
once were simply gone, and nothing anywhere knew they had been
owed. Only reminders behaved properly, because they alone had a
lifecycle in a table.

**The line: deciding is not delivering.** *Is this worth saying* is
genuinely different per watcher and stays there -- a diary entry
three weeks out and a battery at fifteen per cent have nothing in
common and never will. *Did it get said, to whom, and what if it
did not* is one job, and is now done once.

So `internal/outbox`. Every announcement is written down **before**
anything is spoken, and one deliverer is the only thing that
speaks. The reminder lifecycle, promoted to cover everything.

**Expiry is fixed when the thing was true,** not when delivery is
attempted. This is the whole point and the easiest thing to get
wrong later: measured from whenever the server came back, nothing
would ever expire, and the answer to being off overnight would be
announcing breakfast at lunchtime.

**Settled rows are kept, not deleted.** "Nothing was announced" and
"six things were owed and every one went stale" look identical from
an empty table, and the second is a fault somebody should be able
to find.

**Presence has one convention here, the existing one.** The first
draft gave the deliverer a two-answer sensor -- away, and whether
that is known. That is a second opinion about the same room:
presence in this house fails closed by the owner's decision, every
fault is away, and `remind.Presence` already says so in one bool.
Reusing it beat inventing a third convention.

The policy table lives beside the audience table in
`internal/conversation`, extended rather than added to, because
that file already argues the case: *one table rather than a
condition in each place that needs to know*.

Done one watcher at a time by the owner's choice. **Situations
first**, because it loses things today and is small. The other five
still speak directly. Each move is its own change, and the outbox
is proved against the real schema rather than only in memory: a
fresh announcement said, one the server was an hour late for
expired with the reason kept.


### A schedule and a sequence of tasks

Owner, 6 October 2026: *"the producers are so rigid, these kind of
rules must be configurable, because in future I'll be having a tool
to set timer and perform a task, like workflow automation."* Then:
*"I will give the AI when to do what, how frequently to do what, so
a timer control I should be able to give, and the what might be an
already available task function or hand it off to AI to do a custom
task. The custom task will be just announce too, as it is also a
task."* And: *"we don't need separate reminders, because it will
also be a schedule timer with a task -- just to talk. To keep an
alarm too will be a task, because not just a single task, a
sequence of tasks can be done."*

The thing being named is that **announcing was the shape of the
system when it is one task among several.** Seven loops in Go, each
watching one thing, each ending in the same place: say something.
Adding an eighth meant writing an eighth loop, and every one of
them was a schedule and a sequence with both written into a
function instead of a row.

The proof was already in the code. `reminder_set` does two things
-- says the words and sets a phone alarm -- which is a two step
sequence that only looks like one verb because somebody typed it
that way. `internal/job` is that same shape, written down.

**Three schedule forms and no more:** `every 5m`, `at 08:00`, `once
2026-10-07 06:40`. A cron expression is unreadable to somebody
dictating it aloud, and dictating it aloud is how these are meant
to be made. Kept as written rather than parsed into columns,
because it is read back to the person who said it and *every five
minutes* is what they said.

**Two kinds of task:** say something, and run one of the tools that
already exist. The verbs were never missing -- there are fifty-two
of them -- and a rule needs a way to point at one, not a new
vocabulary of its own.

**Linear, and that line is held.** No branching, no nesting, no
else. A step may stop the ones after it, which covers *do this only
if the battery is low* without the next question being loops and
variables. Tasker exists and Home Assistant exists, and a third
automation language in this house would be a mistake.

#### A sequence is one turn, not several

The first live probe failed, and failed usefully. A `[task_add]`
step was refused: *task\_add was not called correctly ... this call
never ran, so it says nothing about what exists.* That is the
read-before-write rule working exactly as designed -- and a
sequence built to be `[list, then add]` could never satisfy it,
because each step was arriving as a turn that had read nothing.

So `Doer.Do` takes `already []string`, the runner appends each
tool it ran, and the sequence reaches the tools as what it is: one
turn with several calls in it. Without this, sequences of more than
one step are only decoration, which is the reason they exist.

#### What it refuses to do

**A run more than an hour late does not happen.** The same
judgement the outbox makes, for the same reason: a server that was
off over the moment comes back owing runs, and yesterday's eight
o'clock done at noon is worse than not done at all. The row records
that its moment passed.

**A failed step does not swallow the ones after it.** A phone alarm
that could not be set must not take the words with it, which is the
rule the reminder and its alarm already follow. One run leaves one
record however many of its steps failed.

**A schedule that stops parsing ends the job.** It would otherwise
be read and skipped every two seconds for ever.

**`kind` is carried but never read by the engine.** So that
somebody asking what reminders they have is not told they have
fourteen jobs including a battery check. The listing reads it; the
runner does not.

#### Proved against the real thing

A two step job `[task_lists, task_add]` due six seconds out: both
steps `outcome=ok`, row `done`, no reason recorded. An announce job
on `every 1m`: published to all three clients as pending, spoken,
and rescheduled exactly sixty seconds on with the state left
active.

**Nothing is migrated onto this.** It runs new rules only. The
producers move over one at a time afterwards and **reminders
last**, because reminders are the feature most used and a new
engine is wrong in small ways for a week.


### A gate is a step, not a column

Owner, 6 October 2026: *"now we need gates/checks like run it when
condition passes."*

The shape was already waiting. `Doer.Do` returned a `stop` that
nothing produced, and the package comment promised *a step that
checks and stops*. A check step is what makes that real, and the
runner did not change at all.

**A gate is a step rather than a `when` column on the job.** A
check as the first step *is* a condition on the whole job, and a
check in the middle is a condition on what follows -- which the
column could not express. One concept covers both, and the
alternative was two places that both say *whether*.

**The schedule says when; the gate says whether.** This is the line
that keeps either from growing into the other. It has an immediate
consequence: **day-of-week is not a gate.** "At eight on weekdays"
is a *when*, so it belongs to the schedule grammar, and putting it
in gates would have meant two places answering the same question.
Extending the schedule is its own slice.

#### Three sources, because there are three

A gate reads an event's latest value, how old that reading is, or
presence. Not because three is tidy, but because those are the
things the server holds as **structured values**. The obvious
fourth -- *check what the previous tool call returned* -- cannot
work: tool results are prose written for a model, so there is
nothing to compare. Reaching for it would have meant values passed
between steps, which is variables, which is the language this
engine refuses to become.

	battery.level < 20
	battery.level is not 100
	battery.level older than 2h
	presence is away

One string, kept as written, the same as the schedule and for the
same reason: it is read back to the person who said it.

**Timed by when the server was told,** not by the device's clock.
So `older than 2h` means what it sounds like when a phone has
stopped talking, which is the quiet-device check written as a rule.

#### An unknown reading never passes

Nothing is known, so nothing is proved. The same answer
`notice.worth` gives when it cannot tell which way a value was
moving, and for the same reason: between a sequence that does
nothing and a sequence that fires on an assumption, the first is
the cheaper mistake. A gate on an event kind that has never
arrived stops, every time.

A word where the rule expected a number is the same: not an error,
simply something the gate cannot order.

#### A check that stops is not a failure

It does not count as failed, the job reschedules normally, and
nothing is spoken. But **it is recorded.** `why` was *what went
wrong last time*, and is now *what happened last run* -- the
failure, or which check stopped it. Without that, "why did my rule
not fire" has no answer anywhere, and a gate that silently does
nothing is indistinguishable from a gate that is broken. A real
failure earlier in the sequence keeps the field; it is the more
important thing to have kept.

#### Three smaller things it turned up

**`job.New` never validated its steps.** The doc comment claimed a
rule that cannot work fails in front of whoever wrote it, and only
`Step.Valid` did that, which nothing called on creation. Harmless
while a step was a tool name; with gates, an unreadable condition
would have become a job that silently never passed. Now `New`
checks every step.

**`event.Value` is exported and `notice.valueOf` delegates to it.**
The payload's `value` field is the one convention agreed across
senders, so it belongs to `event` rather than being copied into
each package that reads it. One duplicate removed, not one added.

**One presence client, shared with the reminder loop.** The server
was already building four. A gate asking the room is the same
question the reminder loop asks, and `whereabouts` returning nil
means *nothing can tell*, which the gate reads the way the loop
does rather than inventing a second convention.

#### Proved against the real thing

Four jobs, each a check then a tool call, due eight seconds out:

| condition | passed | what followed |
|---|---|---|
| `battery.level < 90` (it was 68) | yes | the tool ran, row clean |
| `battery.level < 20` | no | nothing after it, reason recorded |
| `nosuch.kind is 1` | no | nothing after it, reason recorded |
| `presence is here` | yes | the tool ran, row clean |

The presence one passed against the real `sensor.owner_presence`,
not a nil fallback.


### A step that writes its own sentence

Owner, 6 October 2026, after seeing that none of the seven talking
producers could move onto the engine: *"do the ai sentence one."*

The blocker was never the schedule. It was that an announce step
says a string fixed when the rule was written, and almost nothing
this assistant says unasked is fixed -- *you tried to reach them
twice*, *leave in twenty minutes*, *the battery has been draining
steadily* are all composed out of whatever was just found. A rule
that could only ever say the same sentence could not replace any
of them.

So a fourth kind of step: **write**. It carries an instruction
rather than words, and the model turns the instruction and what
the sequence just found into one sentence.

#### The model gets no tools of its own

It would have been easy to hand the write step an allow-list and
let the model look things up. It does not get one. **A sequence
says what to look at by putting tool steps in front of the
write**, and the model is given their results and nothing else.

What that buys is that what a rule may reach is visible *in the
rule*. A rule that reads the diary has a diary step in it, which
can be read off the row by somebody asking what this thing is
allowed to see. The allow-list version decides that in the moment,
inside a model call, where nobody can see it. It also avoids a
second tool loop in a server that already has one.

#### A transcript is not variables

This engine refused values passed between steps, and now a step
reads what an earlier step returned. The distinction is real and
worth keeping straight.

What is carried is a **transcript**: tool names and the prose they
returned, appended to and never rewritten. Nothing compares it,
nothing branches on it, and **a gate still cannot read it** -- a
check reads the world, not the sequence. The moment a condition
could test a previous step's output, the engine would need types,
comparison and names for things, which is the language it is
refusing to become.

A transcript is also what the engine already claimed to be. *A
sequence is one turn, not several* was the reason tool names were
carried in the first place; carrying what they said is the same
fact, used by a model instead of by the read-before-write rule.

#### Silence is an answer

A write step may produce nothing, and then nothing is said and the
rest of the sequence does not run. Most looks find nothing -- that
was true of every watcher this replaces -- and a rule obliged to
speak every time it ran would be worse than no rule.

The convention is the existing one: the model is told to answer
`NOTHING`, because a model asked for nothing tends to produce an
apology instead, which would then be spoken aloud.

**`Silence` and `Nothing` now live in `internal/prompt`,** with
`notice` and `coming` aliasing them. Both already had their own
copy of the same word and the same lenient parse; a third was
about to be written. The same move as `event.Value`: the
convention belongs where it is shared from, not in each package
that needs it.

#### Proved against the real model

Two jobs, ten seconds out:

| the rule | what happened |
|---|---|
| `[event_recent battery.level, write "say what the battery has been doing"]` | *"The battery has been draining steadily, losing one percent roughly every ten minutes, and now sits at sixty-six percent, sir."* |
| `[write "only speak if the dishwasher reported a fault", tool event_kinds]` | nothing said, the tool after it did not run, row reads *there was nothing worth saying* |

The first was checked against the rows rather than believed: 66
per cent, and one point every eight to thirteen minutes. The
sentence was right, in persona, and composed from readings no
fixed string could have held.

#### What this does and does not unlock

Situations, the diary, leave-by and immediate's wording were all
blocked on this one thing, and are now blocked only on being
written as rules. Greeting and renaming are not: they do not
happen at a *time* at all, and need an event trigger, which is the
next piece.


### A rule that waits for something to happen

Owner, 6 October 2026, choosing between polling the condition and
acting the moment a reading lands: *"do option b, it is the right
way."*

Until now a rule could only be hung on a clock, and the two
producers that can never move onto a clock are the two that matter
most for an assistant that is supposed to notice things. Walking in
the door does not happen at eight.

So a fourth schedule form, and the odd one out:

	when battery.level < 20

It has no next time. `next_at` stays null, the clock's query asks
for `next_at IS NOT NULL`, and so the runner's two-second look never
sees it. It runs when a reading arrives that makes it true.

#### The crossing, not the condition

A phone at fifteen per cent says so every few minutes. A rule that
ran whenever its condition held would repeat itself all afternoon,
which is the failure the old battery watcher already had and solved
with `falling` and `rising`.

Push makes that idea general and the keywords unnecessary. When a
reading lands, the one before it is read too, and the rule runs only
where the condition was **false then and true now**. `< 20` fires on
the way down because the previous reading was not below twenty; it
does not fire again at eighteen. No direction keyword, and it works
for every comparison rather than only for numbers going one way.

It follows that **a rule never fires on the first reading of a kind
the server has never seen.** Nothing is known, so no crossing is
proved -- the same answer a check gives for the same reason.

#### A trigger is narrower than a check

`Watched` refuses two things a check allows, because a trigger is
set off by an arrival:

- **presence**, which does not arrive here. It is asked of Home
  Assistant, so no reading ever lands to wake the rule.
- **`older than` / `within`**, which could never be true at the
  moment a reading lands, since landing is what makes it new.

Both fail when the rule is written rather than never firing in
silence, which is the harder fault to find.

#### Nothing waits on a rule

`Arrived` puts the kinds on a buffered channel and returns; the
runner's existing loop selects on it beside the ticker, so there is
one place where jobs run and no second goroutine per batch. A phone
handing over its readings is answered either way, which matters more
here than for the stays settled on the same path: a rule can end in
a model call, and a device must never wait on one.

When the channel is full the batch is **dropped and counted**, not
queued. A runner that far behind is better off acting on the next
reading than working through a backlog that has all been overtaken.
Counted rather than logged, because a line per dropped batch is
itself what keeps it behind.

This follows the handler's own precedent. Stays are already worked
out as readings land rather than on a timer, *because a reading is
the only thing that can end a stay*. The same sentence is true of
these rules.

#### Proved against the real thing

A rule `when probe.level < 20`, and four readings posted over HTTP
as a device would:

| reading | fired |
|---|---|
| 50, with nothing before it | no |
| 19 | **yes**, 17ms after it landed |
| 18 | no -- still low, but not a crossing |
| 40 | no |

Afterwards the row was still `active` with a null `next_at`: a
watching rule does not end, it waits for the next crossing.

#### Two things the probe turned up

**A payload's `value` must be a string.** A JSON number fails the
handler's type assertion, is treated as absent, and is rewritten to
`unknown` -- so the first attempt stored three readings of
"unknown" and nothing fired. The phone has always sent strings so
nothing was broken, but a reading that is silently relabelled is a
poor way to find that out.

**The in-memory store was not keeping the contract the real one
keeps.** `Due` matched on `!Next.After(at)`, and a zero time is
before everything, so every watching rule looked due to it while
MySQL's `next_at IS NOT NULL` correctly skipped them. The test
caught it, which is the argument for having written it.


### Rules by voice

Owner, 6 October 2026, choosing it over moving a producer: *"rules
by voice first."*

Until this, the engine was unreachable. Everything it could do had
to be put in by hand with a throwaway Go program, which meant the
schedule, the gate and the write step were all built and none of
them was usable.

`rule_add`, `rule_list`, `rule_cancel`, in the `rule` domain, so a
write reads its own listing first like every other domain here.

#### The arguments are not the row

A step is a structure, and handing the model an array of them would
have been handing it the internals. What it fills in instead is the
shape a rule actually takes when somebody says one out loud: a
**schedule**, optionally a **condition**, optionally things to
**look up**, and then either **say** (words they dictated) or
**ask** (an instruction to work the words out). The sequence is
assembled from those, in a fixed order the model does not choose --
look things up, then check, then speak.

That order is deliberate: a condition is cheap and a lookup is not,
but the check may be about something only a lookup can show, so the
readings go first and the one thing that can stop the rule comes
last before it speaks.

#### A rule may only read

A tool that writes is refused outright. A rule runs unattended and
may run every few minutes; reading is safe to repeat and writing is
not, and nobody is there to notice the first time it goes wrong.
This is checked against the live registry rather than a list here,
so a tool added tomorrow is classified by its own `Writes` flag and
nothing in this package needs to know it exists.

The lookups are capped at three. A rule is a sequence, not a
program, and three is more than any sentence has needed.

#### rule_add is described in full, before any measurement

`internal/tool/deferred.go` normally earns a place on the hot list
with numbers. This one is there on the strength of the two
precedents already recorded in that file: `event_recent` and
`place_visits` were both named-but-not-described, and in both cases
the model answered that it had no tool for the thing while the name
sat in its own catalogue. A standing rule is a kind of doing the
assistant has never had, which is exactly that shape. Its arguments
are also the part that cannot be guessed -- four schedule forms
written exactly, and a condition grammar.

`rule_list` and `rule_cancel` stay deferred. The listing is run for
the model by the read-before-write rule rather than chosen by it,
and cancelling takes one identifier out of what that returned.

#### Proved by speaking to it

Four sentences through `/api/chat`, nothing hand-written:

| said | stored |
|---|---|
| *"every morning at seven thirty, look at my calendar and tell me what is coming up that day"* | `at 07:30` · `[calendar_events, write]` · next run 07:30 tomorrow |
| *"what standing rules do I have set up"* | *"Just the one, sir — the morning calendar briefing..."* |
| *"whenever my battery drops below fifteen, tell me how fast it has been draining"* | `when battery.level < 15` · `[event_recent, write]` |
| *"turn off the morning calendar briefing"* | cancelled |

#### What it chose, and one thing it did not

Asked to *"say something about"* the battery dropping, it chose
**say** with fixed words -- so that rule would read the same
sentence out for ever, which is the thing the write step exists to
avoid. Narrowing the `say` description to warn that the words are
fixed did not change it: the model is honouring "say something"
literally, and it is not wrong to.

Asked instead to *"tell me how fast it has been draining"* it chose
**ask** and added the `event_recent` lookup unprompted. So the
capability is reachable and the phrasing decides. Left alone rather
than forced: a fixed sentence is legitimate -- *"when I get home,
say welcome back"* -- and there is no rule here to enforce, only a
judgement. Worth a measured look if it turns out to matter.


### The first producer moves: Immediate

Owner, 6 October 2026: *"move."*

`internal/notice` was a watcher on a thirty second tick, a table of
thresholds parsed out of `config.ini`, a gate asking whether anybody
was in the room, and a prompt. All of it is now six rows and none of
it is Go.

	when battery.level < 25   only if >= 20   read the readings   write
	when battery.level < 20   only if >= 15   read the readings   write
	when battery.level < 15   only if >= 10   read the readings   write
	when battery.level < 10   only if >= 5    read the readings   write
	when battery.level < 5                    read the readings   write
	when battery.level is 100                                     write

#### What the move fixed on its own

**The old rules matched an exact value.** `battery.level 25 falling`
fired when a reading *was* twenty-five and lower than the last one.
A phone switched off at twenty-six and turned on again at four
reported four, matched nothing, and said nothing -- precisely when
there was most to say. A crossing has no such hole.

**And the crossing replaced the direction keyword.** `falling` and
`rising` existed to stop a phone charging through fifteen per cent
announcing fifteen per cent. A rule firing on the transition from
false to true does that for every comparison without a keyword.

#### What the move created, and the check that fixes it

Five bands and one reading of four means all five conditions become
true at once, which is five sentences where the watcher said one.
The fix is the gate, doing exactly what gates are for: each band is
**bounded below**, so only the lowest band gets past its own check.

Measured on a kind nothing listens to, so that nothing untrue was
said aloud about a battery that was actually at sixty-six per cent.
Thirty, then four, in one step:

	probe band 25   ran, stopped: the check probe.level >= 20 did not pass
	probe band 20   ran, stopped: the check probe.level >= 15 did not pass
	probe band 5    ran, and acted

#### The page had to keep working

`/v1/remarks` exists because of a request: *"I should see in UI when
it triggered, why it didn't speak, if spoken what it spoke."* Its
whole point is that silence has several causes and they look
identical from outside. Deleting the watcher would have frozen it.

A run of a rule turns out to be the same record. `Noted` already
carried a label, an outcome, what was said and why not; a run
carries a name, whether it spoke, the sentence and what stopped it.
So the runner gained a **Journal**, main.go maps a run onto a
`Noted`, and the table, the endpoint and the Flutter client are
untouched. The page now covers every rule rather than one watcher.

**Only a rule that might speak is written down.** The first version
recorded a tool-only rule as having *held* its tongue, which is a
lie: it was never going to say anything. The record is of what the
assistant **considered saying**, and a rule with no announce or
write step considered nothing.

#### One guard turned out to be unnecessary

The watcher waited five minutes after startup before looking, so
that restarting during an evening of changes did not produce a
remark about whatever happened to be arriving. The constant is
gone and nothing replaces it, because a pushed rule cannot have
that fault: it fires on a reading arriving and crossing a line it
was not across before. A restart replays nothing, so there is
nothing to hold back.

#### What was kept out of the bonfire

`notice.Since` -- four weeks, how far back somebody counts as
somebody to speak to -- was never about remarks. Its own comment
said so: *"not used here ... but by whoever runs the watcher."*
Three other loops ask for it. It is now `chat.Active`, next to the
`Talkers` query that consumes it, which is a fact about a person
being around rather than about any watcher.

What is left of `internal/notice` is the record and nothing else,
and its package comment now says that.

#### What this does not prove

The six battery rules were created and read back -- asked what
rules he has, the assistant described all six and their thresholds
correctly -- but they have not fired, because the battery has not
been low. The mechanism is proved on the probe kind and the wiring
is proved by the listing; the first real one will be the first time
the battery actually drops.


### The phone gets a channel of its own

Owner, 6 October 2026: *"the alarm moving with it should not be
bundled with it, it should be configurable, but by default it's
true. And moreover, isolate the process where something from server
to phone is happening. It's a separate process and channel, they
should be decouplable. And since the phone has no snooze, another
copy should be created."*

Three things, and the middle one is the reason the other two were
awkward.

#### Asking is not getting there

A reminder tool reached the phone itself, inside the model's turn.
Three faults came out of that one fact.

**A turn waited on a phone.** Moving a reminder meant a POST, then
`time.Sleep(1500ms)` because the phone's clock drops a second
request that arrives on the heels of the first, then another POST.
Nearly two seconds of a thirty second budget, spent on something
that has nothing to do with answering anybody.

**The retries were in memory.** `alarm.Keeper` held what was still
owed in a map. Restarting the server emptied it without a word --
including alarms a person had already been told would keep being
tried. That is the worst kind of loss: the promise survived and the
thing promised did not.

**And nothing else could ever use the path.** It was a reminder
tool holding an `alarm.Setter`, not a way out of the building.

So `internal/errand`: what the server owes a device, as rows. The
asking writes one and returns. One sender gets them there, in
order, retrying, and a restart changes nothing. Nothing in it knows
what an alarm is -- a kind names what is being asked and the body
carries that kind's arguments, so the next thing the server wants a
device to do is a row rather than another path out.

**The gap is a column, not a sleep.** `after_at` says *not before
this*. Moving an alarm writes the clear at now and the set at now
plus `alarm.Settle`, and the sender respects both. Measured on a
real move: clear ready at 13:08:15.058, set at 13:08:16.558,
exactly the gap apart, with neither in anybody's turn.

#### What the separation nearly lost

The owner's standing rule is that an alarm which was not set is
said outright, *because somebody who believes an alarm is set and
finds it was not is worse off than somebody told*. With the phone
no longer reached in the turn, that sentence has nobody to speak
it: the turn does not know, and cannot.

So the telling moved to where the truth is. The sender gives up
after twenty tries and announces it. Nothing is swallowed; it is
said later, by the only thing that ever knows.

What the turn can still honestly say is that the alarm has been
**asked for**, which is what it now says.

#### Three states that were one

`alarms == nil` and `alarms.On() == false` mean different things
and get different sentences, which the first draft collapsed:

- **no phone configured** -- a fault worth reporting: *there is no
  way to reach their phone from here*.
- **alarms turned off** -- a setting they chose, and silent. Saying
  anything would be the assistant commenting on its own
  configuration.
- **asked for and queued** -- said in passing.

The setting is `[assistant] alarms`, true by default.

#### A second alarm, not a moved one

Putting a reminder off asks for **another** alarm at the new time
rather than moving the first. Nothing here can reach an alarm
already ringing on the phone to postpone it -- there is no snooze
to reach.

The first alarm is left alone deliberately. Putting a reminder off
is usually something done after it has gone off, by which time its
alarm is spent; and where it has not gone off yet, a spare alarm
before the one that matters is a smaller fault than no alarm at the
time that matters.

This also closes a hole that was there before any of this.
`reminder_snooze` was the one reminder tool `All()` never handed
the phone to, so snoozing moved the reminder and left the alarm
where it was -- and a snooze of a repeating reminder created a
whole new one-off with no alarm at all. Found by reading the
wiring, not by anything failing.

`alarm.Keeper` is deleted. It was the in-memory retry queue, and
everything it did the sender now does from a table.

#### Proved end to end

Set, moved and cancelled through `/api/chat`, watching the rows:

| said | errands | state |
|---|---|---|
| *"remind me at 11:55 pm tomorrow"* | `alarm.set` | done |
| *"move it to 11:40"* | `alarm.clear`, then `alarm.set` 1.5s later | both done |
| *"cancel it"* | `alarm.clear` | done |

No stray alarm left on the phone.

#### Not proved, and why not

The snooze path has unit tests and was **not** run against the real
phone. A snoozed reminder would put a second alarm there carrying
the same label, and `Clear` dismisses by label: whether Android
removes one or both is untested, and finding out costs a stray
alarm on somebody's phone. It is a real open question, written down
rather than answered by experiment on a live device.


### A lookup that could not say what it wanted

Owner, 6 October 2026, on a rule set up by voice to report the
battery every minute: *"but the battery is now 56."* It had
announced thirty-four, twice.

Thirty-four was real. It was a reading from the day before.

The rule was `[event_recent, write]`, and the tool step carried
**no arguments**, because `rule_add` had no way to give it any.
`assemble` built `job.Step{Do: Call, Tool: name}` and nothing
else, so every lookup ran on that tool's defaults. For
`event_recent` the default is a week of every kind, so the model
was handed hundreds of rows -- positions, stays, screen locks --
and picked a battery row out of it that was twenty-three hours
old.

The tool could have answered the question exactly. Its own
description says so: *"a limit of 1 with a kind gives how that
thing stands now."* Nothing let the rule ask for that.

So `look_up` stopped being a comma separated list of names and
became a list of `{tool, args}`, with the arguments as JSON the
model writes the same way it writes them when calling the tool
itself. Checked when the rule is written, because the only
symptom of bad arguments is a rule quietly answering from the
wrong row at three in the morning.

Asked again afterwards, the model wrote
`{"kind":"battery.level","limit":1}` without being told to, and
the rule said fifty-six.

#### And it should have said nothing instead

The deeper fault is that the model answered at all. Handed a week
of events with no current battery reading in reach, the honest
answer was silence, and it reached for the nearest number
instead.

The write step is now told that readings carry the time they
arrived, that one from yesterday does not say what is true now,
and that `NOTHING` is the answer when what was asked for is not
there. A wrong number is worse than no sentence: the sentence
nobody hears costs nothing, and the number gets believed.

Worth remembering which half of this was the real defect. The
missing arguments were a gap in what a rule could express. The
confident wrong number was the thing that made it reach the
person, and no amount of argument plumbing would have stopped it
on its own.


### A rule that can decide what to look at

Owner, 6 October 2026, on the write step being given no tools:
*"why is a tool not passed? I think relevant tools must be
passed."*

Right, and the reasoning it overturned was half sound.

What was sound: **what a rule can reach should be readable off
the row.** A rule running unattended every minute with the whole
toolbox is not something anybody can audit by looking at it.

What was wrong: the conclusion. Fixing *what* a rule may reach
does not require fixing *when and with what arguments*, and those
are exactly the parts a rule cannot know in advance. A rule
written on Monday cannot know what Thursday needs.

The cost of getting that wrong had already been paid, an hour
earlier, by a rule that announced a battery reading from the day
before. Its arguments were decided once, when it was written, by
a model guessing at what a question it had not yet been asked
would need.

#### Named, not open

So a write step carries a list of tool names, and those are
passed to the model as real tools. It decides whether to call
them, and with what arguments, against the question actually in
front of it. The row still says what it may reach:

	"anything wrong" (every 30m); works out what to say about
	"say whether anything needs their attention", and may call
	calendar_events

**Reading tools only, checked twice** -- once when the rule is
written, and again when it runs, because a tool can gain a write
between the two and the rule would never know.

**Four rounds, then silence.** A rule still calling tools after
four exchanges has started arguing with itself, and a sentence
built on a conversation that never finished is worse than none.

#### Both ways of looking, kept apart

`look_up` runs tools before the model is asked anything, every
time, and hands over what they said. `tools` lets the model
choose. They are not the same thing and both earn their place:

- `look_up` for a rule that always wants the same thing -- the
  diary every morning. One round trip fewer, and deterministic:
  what it looked at is in the row, not in the model's judgement.
- `tools` for a rule whose need depends on what it finds.

Giving both is sensible: the lookups run first and the tools are
there if more is wanted.

#### Proved against the real model

A rule with **no lookups at all**, one named tool, and
`"Say what the phone battery is at right now"`:

	a job's model called a tool   tool=event_recent outcome=ok
	a job wrote what to say
	→ "Your phone is at 51 percent, sir."

The battery was 51. The model picked the tool, picked the
arguments, and got the current row rather than one from
yesterday -- which is the whole of what the previous design could
not do.


### Two producers that are not moving

Owner, 6 October 2026, told that six of the seven watchers remain
and what each would need: *"rule out renaming, greeting."*

So the engine's scope is four, not six. Written down because the
absence of a thing leaves no trace in the code, and somebody
reading the list of watchers in a year will otherwise work out
for themselves that these two could be rules, and be right, and
be wasting their time.

**Renaming** is not a standing rule and would be worse as one. It
happens inside a turn, when a conversation has enough in it to be
named, and it goes to **the one client that caused it** -- which
is the whole reason the audience table has an `Asked` row at all.
Everything the engine publishes goes to an audience. Moving
renaming would mean teaching rules to target a single client, for
one producer that does not want anything else the engine offers:
no schedule, no condition, no sequence, no sentence worked out
from what was found.

**Greeting** is closer to a rule and still not one. It fires when
somebody walks in, and the engine has two triggers: a clock, and
a reading arriving. Presence is neither -- it is *asked of* Home
Assistant when wanted, and nothing lands at `/v1/events` when a
person appears. Making it a rule means either presence starts
reporting itself as an event, or the engine grows a third kind of
trigger for one caller.

The second of those is the tempting one and the reason to write
this down. A third trigger is a day's work and would leave the
engine with three ways to start a rule where two have been
enough, for a producer that already works and that nobody has
complained about.

**What this leaves in scope:** situations, the diary and leave-by,
all three blocked on the same missing thing -- a rule cannot
remember what it already said -- and reminders, which needs the
schedule grammar widened and an alarm step, and which moves last
by an earlier decision.


### One night at home, written down three hundred times

Found while writing up what data exists, so that somebody could be
asked how profiling ought to work. The answer to that question
barely matters while this is true:

	621 place.stayed rows   =   13 actual stays

The worst single stay -- at home, ending 3 October 11:45 -- is
**319 rows**. Summing their `minutes`, which is the obvious thing
for anything counting where somebody spends their time to do,
gives **4,691 hours of home inside a six day window**. A hundred
and ninety-five days.

#### Why

`Settle` reads a fixed window back -- `Looking`, thirty-six hours
-- and works the stays out again from scratch every run. Its own
comment explains why that is safe:

> *The same readings produce the same stays and the same keys, so
> a stay already written is recognised as a resend and the work is
> wasted rather than wrong.*

True, and only while the stay is wholly inside the window. A
stay's key is made from its **start**. Once the back edge of the
window crawls past that start, the stay is recomputed **cut**: a
later start, a different key, a new row. Five minutes later the
edge has moved another five minutes and it happens again.

So every past stay is rewritten once per settle run for as long as
the window is passing through it, each copy a shorter fragment of
the same hours, every one of them ending at the same minute:

	starts 00:00  mins 325  ends 05:25
	starts 00:05  mins 320  ends 05:25
	starts 00:10  mins 315  ends 05:25
	... thirty-one more ...

Nothing was wrong with any individual row. Each is an honest
account of what the server could see.

#### The fix

A cut stay is **dropped, not mended**. It was written whole while
it was whole, and the fragment in front of us is less true than
the row already recorded. `Whole` takes the stays, the readings
they came from and the moment, and returns the ones that began
where the person stopped rather than where the window ended.

Knowing whether the window cut anything is the only subtle part.
If the oldest reading is well inside the window there is less than
a window of history and nothing was cut -- dropping the first stay
then would lose somebody's first morning, every morning, until
they had been using this for two days.

#### What it costs, stated plainly

A stay longer than `Looking` stops being amended once it outgrows
the window, so an unbroken fortnight at home will read as
thirty-six hours. That is a bounded and visible wrong answer in
place of an unbounded and invisible one, and the alternative --
reaching back into what was already written to recover the real
start -- is more machinery than the fault deserves today.

#### Proved against the running server

Deployed mid-afternoon with 621 rows in the table. Two settle runs
later: still 621, and the open office stay had gone from 495
minutes to 515 **in the same row**. Amended, which is what was
always intended, rather than duplicated.

#### And the fragments already written

Cleared, with the owner's say-so, on the day. Not by the end
minute, which was the obvious grouping and the wrong one: two
copies of the same night ended at 11:44 and 11:45 and fell either
side of every bucket boundary tried. By overlap instead -- a stay
is redundant when another at the same place begins no later and
ends no earlier, within five minutes -- which merged that pair
and everything else in one rule.

	621 rows  ->  12 stays
	609 deleted, dumped first to
	~/.local/share/jarvis-backups/

What four and a half days of someone's life actually looks like,
now that it can be added up:

	home                        6 stays   76.0 h
	office                      3 stays   18.1 h
	Ammaniammal School          1 stay     3.1 h
	Flavors non veg restaurant  1 stay     2.8 h
	Muthu Supermarket           1 stay     0.2 h

A hundred hours in a hundred and eight. Before this it was four
thousand six hundred.


### A sender that could only say one thing, and the two ways it broke

`unpacked` exists because the phone's one working sender can write
only `{"value": one string}`: Tasker's Perform Task passes two
parameters and the snippet has no third. So a reading with
anything besides a name to report puts JSON inside the string, and
the server unfolds it.

A good trade, and it was failing silently on thirty-one events in
six days. Both failures turned a reading into a worse thing than a
missing one: the broken text became the **label**, and a label is
what everything here counts by.

#### Insisting on a value it did not need

`power.plugged` reports a level and nothing else. `unpacked`
refused to unfold it, on the reasoning that *something keyed on a
value is what every reader expects to find*.

True, and already guaranteed one line later. `labelled` runs next
and calls an absent value `unknown` -- which is exactly what
`battery.full` relies on. Insisting on it in both places meant
seventeen events kept the shape

	{"value": "{\"level\":\"54\"}"}

so the level was unreachable by the one convention every reader
uses, and the label was a different piece of nonsense at every
battery percentage. The check is gone and the payload comes out as

	{"level": "54", "value": "unknown"}

#### JSON built by joining strings

The other fourteen are `music.state`, and the cause is in the
data:

	"from":""ramin djawadi ironm" in Search"

Tasker assembles that JSON by concatenation, so a field holding a
quotation mark ends the string early and what arrives is not JSON
at all. It cannot be parsed and it should not be a label: that one
is unique to a single search for a single song, counted for ever
as a thing the person did once.

Kept beside the reading instead of as the reading, under
`unparsed`, which is the treatment `labelled` already gives a
telephone number with no business being a name. Nothing is lost
and nothing unusable becomes a label.

**Narrowed deliberately.** Only text with a brace at *both* ends
is treated as an unfinished object. An existing test says a value
that merely begins with one -- `"{not json"` -- must come through
untouched, and it is right: that is a value, not a broken
structure. The test passed either way, because it only checked
that the payload still parsed; the suffix is what actually keeps
its promise.

**The real fault is on the phone**, which should escape what it
joins. That is a code field edited with a thumb and the only thing
standing between this server and every event it gets, so the
server is made to survive it instead.

#### Proved through the endpoint

	{"value":"{\"level\":\"54\"}"}
	  -> {"level": "54", "value": "unknown"}

	{"value":"{\"value\":\"Geek Music\",\"from\":\"\"q\" in Search\"}"}
	  -> {"value": "unknown", "unparsed": "{\"value\":\"Geek Music\",…"}

#### And the thirty-one already written

Repaired in place on the day, dumped first. The seventeen power
readings got their level back; the fourteen broken ones kept
their text under `unparsed`.

The point of it, in one line -- the labels before and after:

	music.state <fourteen different broken strings>   1 each
	music.state unknown                              14

Fourteen things the person had each done exactly once became one
thing they could not be told about, which is the truth. Underneath
them the real labels sort properly again: Daft Punk 14, Shankar
Mahadevan 9.


### The description had never seen the diary

Of the two ideas worth taking from the outside review of how
profiling should work, one turned out to be already done and the
other was not a prompt change at all.

**Observation against inference is structural here already.** The
description is written in three parts. `WHAT IS SO` takes a count
on every line -- *a line you cannot put a count on does not belong
in this part* -- and `WHAT THEY ARE LIKE` "may say things the
counts only imply, **and it is the only part that may**." Tagging
each line OBSERVATION or INFERENCE would restate the headings and
widen a prompt for nothing, which is the trade this codebase has
measured and lost before.

**Planned against observed could not be added to the prompt,
because the prompt had nothing to compare.** `Builder` is given
what the person said, what their devices reported, what they had
the assistant do, and their memories. No calendar. A description
of somebody's weeks was being written without ever seeing what
they had meant to do with them.

#### A diary is a different kind of fact

So `profile.Diary`, four weeks of entries, in a part of its own
and labelled as plans. The rule matters more than the data:

> An entry says they meant to be somewhere. It is not evidence
> they were, and it never becomes one however ordinary it looks.
> Where a plan and the readings agree, that is worth saying and
> worth saying as agreement. Where a plan has nothing beside it,
> say nothing about it at all rather than assuming it happened or
> assuming it did not.

The pairing is the whole point and neither half could say it
alone. A dentist at four and a stay near a clinic from ten to five
is a sentence; either on its own is not.

An entry they were **invited** to is marked, because somebody
else's plan is a weaker statement of intent than one they made. An
all-day entry is given no hour, because midnight is not when a
birthday happens and inventing one is the description
manufacturing precision.

#### What it found on the first read

Eight entries over four weeks, no calendar unread. Five birthdays
and two public holidays -- which is most of what a diary is for
this person, and goes straight to a thing the prompt already asks
for and could not previously answer: *the people in their life, by
name*.

And one worth the whole change: a film at ten past six on a Sunday
evening, against readings that have them at home from half past
three onwards. The plan and the behaviour disagree. Before this
the description could not have known there was a plan; now it is
told, and told not to decide.

#### Two smaller decisions

**A calendar that will not be read is logged, not swallowed.**
`Everywhere` reports the ones it could not reach, and a diary that
has quietly stopped being readable looks exactly like a person
with nothing on -- the one way this could be wrong rather than
merely thin.

**A failed diary costs the diary, not the description.** The same
trade `rhythm` already makes: the calendar is a network call away
and the rest of this is not.


### Something collects at last

The outbox has been publishing since 6 October and nothing had
ever fetched from it. Every announcement ever written to a client
ended the same way:

	expired   12
	said       0

The sweeper said so twice a minute, in the log, all day: *an
announcement went stale before a client could say it*.

#### The typed half is the simple half

Owner's design, in his own words: *"for a typed client presence is
always true."* A screen needs nobody in front of it, so the web
client collects, shows, and says it said so. The judgement a voice
client has to make -- is anybody in the room -- does not arise.

Polled at twenty seconds, for the reason `refresh` already gives:
the only stream this client opens is for a chat it started itself,
and a second one to keep alive and reconnect costs more than
asking. Nothing here is owed to the second.

**Marked said as it is shown, not as it is dismissed.** A screen
has no later: the sentence is on it from that moment. Waiting for
a click would have the sweeper expire things that were read.

**A failed mark puts it back.** The announcement stays pending and
comes round again, which is the right way to fail: the server
believing something was said when it was not is the failure that
loses it.

#### Where it appears, and why not only the transcript

These reach the transcript too -- `announcement.Writer` has always
written them -- but only the one conversation they landed in. Open
on a different conversation, or on the events page, and you never
see them. Something said unasked is said to the person, not to a
thread, so it belongs where they are looking: a banner above
whatever is open, dismissible, with the tone taken from the kind.

#### The bug that would have made all of this do nothing

The first real collection came back with every sentence correct
and **every identifier empty**. A client cannot report having said
a thing it cannot name.

`Pending` scanned into an anonymous struct with `copyRow`
embedded, and GORM does not look inside an embedded struct unless
it is told to. The joined columns -- kind, say, due, expires --
were named explicitly and arrived; every column of the delivery
itself was silently zero. Flattened, and the select names its
columns rather than asking for `owed_clients.*`.

It had been answering with blank identifiers since the endpoint
was written in the morning. Nothing caught it because nothing
collected, which is exactly the shape of fault a feature nobody
uses accumulates.

#### Proved in the real client

Loaded against the running server, already signed in. Five
announcements that had been pending for hours appeared as
banners -- the backlog nothing had ever fetched -- and all five
went to `said`. A sixth published afterwards appeared alone and
marked itself within the poll.

The five also taught the one piece of design the plan did not
have: stacked, they took half the screen. Three at a time now,
with a line saying how many went before. The cap is what is
shown, not what is answered for -- everything is still marked.


### Not saying it twice

Owner, 6 October 2026: *"do the don't say it twice."* Three of the
four producers still waiting to move were waiting on this and
nothing else. Each would work today and each would repeat itself.

#### The sentence looked like the key, and is not

The first design held the last sentence and refused to say it
again. No key, nothing to identify the occasion: two runs that
mean the same thing say the same words, and two that mean
different things do not. Thursday's warning and Friday's are both
said; Thursday's twice is said once.

It lasted one probe. A rule asked the same question three times,
a minute apart, on data that had not changed:

	The phone was last locked at 22:48 on Tuesday the 6th of October, sir.
	The phone was last locked at twenty-two forty-eight, sir.
	The phone was last locked at 22:48 on Tuesday the 6th of October, sir.

The same news, three wordings. Worse than merely missing the
repeat: the rephrasing in the middle replaced the memory, so the
third -- character for character the first -- looked new as well.

No amount of comparing text fixes that. Whether two sentences are
the same news is a judgement about meaning, and the only thing
that can make it is the thing that wrote them.

#### So it is asked

The write step is told what it said last time and told to answer
`NOTHING` if what it has now is the same news -- *saying the same
thing again in different words is still saying it again*. The
exact-text check stays behind it as a backstop for a model that
judges wrongly and hands back the identical words.

#### Two silences that look alike and are opposite

This is the part the second probe found, and it would have made
the whole thing useless.

A run that says nothing **because a check did not hold** means the
thing has gone away. Forget the sentence: a phone quiet in the
morning has to be reportable quiet again at night.

A run that says nothing **because there is nothing new** means the
thing is still there and has already been said. Keep the sentence.
Forgetting here announces it afresh on the very next run, which is
the repetition the whole column exists to stop.

The first version cleared on both. The second probe showed it
exactly: run one spoke, run two was correctly silent, and the
memory came back empty -- so run three would have spoken again.

#### Proved

Three runs of a rule on data that does not change, before and
after.

	before   spoke  spoke  spoke
	after    spoke  silent silent

#### What else it bought

`Doer.Do` now takes a `Turn` rather than a growing list of
parameters -- whose rule it is, what the earlier steps did, and
what it said last time. Three additions in a day had taken it to
four arguments, and the engine's own words for this were already
*a sequence is one turn*.


### Situations move, and the judging stays in Go

Owner, 6 October 2026: *"move situations."*

The first thing reading the code said was that half of it had
already moved. Situations does not speak; it publishes to the
outbox, and has since the outbox existed. What was left to move
was the two minute loop, the presence gate, the keyed dedupe, and
the calling of three checks.

#### What must not move

The three checks are careful Go. Somebody tried twice and did not
get through; a charger has been in an hour and moved nothing; a
device that reports every few minutes has stopped. Each has a
window, a threshold and eight tests behind it.

A rule could have been written to do this -- give a model the last
three hours of readings and ask whether anything is amiss. It
would have worked, sometimes, and it would have been putting back
exactly what was taken out of the unasked remarks in the first
place: *an unusual event and an event worth interrupting somebody
for are not the same thing, and nothing here can tell which it
has.*

So the looking stayed Go, behind a tool. What moved is **when to
look and what to do about it**, which is all the engine was ever
for.

#### A tool may end a sequence by finding nothing

This is the piece that made it worth doing rather than merely
possible.

A rule is `[situation_find, write]`. If the tool found something
the model says it in its own manner; if it found nothing the
sequence has to stop before the model is asked, or looking every
two minutes costs seven hundred model calls a day to say nothing.

The word for that already existed. A write step answers `NOTHING`
when there is nothing worth saying, so a **tool** that answers
`NOTHING` ends the sequence the same way. No new type, no new
outcome, no column: one shared constant doing in two places what
it already did in one.

Proved on the first run: the tool ran, found nothing, and the
model was never asked.

#### Two things the watcher did that nothing does now

**It held a situation said for six hours**, keyed per check. That
is gone and is not missed -- a rule is told what it said last time
and asked whether what it has is the same news. Which also covers
the case the key could not: the quiet check's sentence says how
long the silence has been, so it was a different sentence every
run and only the key kept it quiet. A judgement about meaning
handles both; a key handled one.

**It asked whether anybody was in the room.** Presence belongs to
the client now. The server publishes a copy per client and each
decides for itself whether anybody heard, which is the whole point
of the outbox and was already true of everything else it carried.

#### What was deleted

`situation.Watcher`, its `Events`, `Say` and `Here`, the `said`
map behind the dedupe, and `watchingFor` in the server. The
constants stayed with a note saying they are no longer read here:
`Every` is the number the rule was made with, and `Again` is a
record of how long six hours seemed when that was the mechanism.

Five tests went with the watcher. Three of them were about the
tool and were rewritten against it; the other two described the
dedupe and the presence gate, and where each of those went is
written into the test file that replaced them, so that nobody
looks for them in the old place.


### The diary moves, and a step that only passes words on

Owner, 6 October 2026: *"do the diary."* The same shape as
situations, and it needed one thing situations did not.

#### A sentence that already exists

Situations needed a model to put its finding into words. The
diary does not. When an entry is first seen it is judged -- how
much warning this particular thing needs, and why -- and that
judgement is a model call made once and stored, often weeks
before the warning is due. `Judged.Sentence` builds the words
from it deterministically.

Handing that to a `write` step would be asking a model to say
again what a model already said, at the cost of a second call and
whatever it lost in the retelling.

So `relay`: say what the tool before it returned, as it stands.
No model. **`write` is for when the words do not exist yet; this
is for when they do.**

That was the one real gap in the engine's vocabulary. A tool could
find something and nothing could simply pass it on.

#### What moved and what did not

The judging is untouched. `coming.Loop` is still what reads ahead,
decides what is new, works out the leads, marks what was said and
forgets what left the diary. The tool builds one per call and
gives it somewhere to put the words instead of a way to say them:

	Say: func(_ context.Context, _, text string) error {
	        said = append(said, text)
	        return nil
	}

Everything else about a pass is the loop's own and unchanged, down
to marking a lead spoken before saying it rather than after.

What moved is the five minute cadence, into a row, and the
delivery: the diary **spoke directly** before this and now
publishes to the outbox like everything else, so it gets per
client copies and presence on the client for nothing.

#### Two warnings at one minute

Rare, and one announcement rather than two interruptions. The
sentences are joined. Situations takes the opposite line -- first
found wins -- because those are three unrelated faults and the
diary's are two parts of the same morning.

#### Proved

`coming_due` ran, found nothing due, and ended the sequence
before the model was asked -- the same economy the situations
move bought, and the reason a rule can look every five minutes.

The relay step was proved separately, since nothing in the diary
was due: a rule of `[event_recent, relay]` published the tool's
words verbatim, recorded them as what it last said, and made no
model call.


### Leave-by moves, and a table that was nearly built

Owner, 6 October 2026: *"do leave by."* The same shape again, and
it caught me about to overbuild.

#### The sentence counts down

Situations and the diary are deduped by the rule now: it is told
what it said last time and asked whether what it has is the same
news. Leave-by cannot be, and the reason is in the words:

	Time to set off for the dentist: it is about 20 minutes away.
	The dentist starts 25 minutes from now and it is about 20 minutes away.

The second form counts down. Twenty five, then twenty, then
fifteen -- a different sentence every five minutes, and nothing
comparing text could tell they are one warning. The watcher
deduped per **appointment**, which the sentence cannot carry.

So this needed keyed state, which the engine does not have, and I
started designing a table for it: a key, a time, a small package,
a migration.

Stopped, because the watcher's memory is a map in the process and
always has been. A tool built once and held keeps that map
exactly as it is. The tool is registered at startup and its
closure outlives every call, so the dedupe is neither better nor
worse than it was this morning -- which is the right answer for a
move, and leaves making it survive a restart as its own decision
with its own reasons.

Coming builds a loop per call because its state is in a table.
Leaving holds one watcher because its state is not. The difference
is written where each is built.

#### What moved

The five minute cadence, into a row. The delivery: it spoke
directly and now publishes like everything else. And the presence
gate, which is gone -- `Here` returns true always now, because
presence belongs to the client and the server keeps no opinion
about rooms.

`Say` became a sink. `Look` returns the sentence as well as
saying it, so the watcher now only returns it and the rule's
relay step publishes it. No model: the words are settled by
`Leaving`, down to the spelling of the duration.

#### Proved

Three rules now run on the engine where three loops used to:

	anything amiss   every 2m   situation_find found nothing
	what is coming   every 5m   coming_due found nothing
	when to set off  every 5m   leaving_due found nothing

Each ends its sequence at the tool, so looking costs no model
call. That is nine hundred and thirty six looks a day between
them and not one model call unless there is something to say.


### The voice client collects, at the door

Owner, 6 October 2026, on the shape of all this: *"when next time
presence is true, the call is made to server and get the pending
announcements."*

The call was already being made. `rest_command.assistant_arrived`
has been posting to `/v1/presence/arrived` since the greeting was
built, triggered by an automation that waits for the watch to
cross -65 dBm and debounces two minutes against a restart being
mistaken for a homecoming. Everything hard about knowing somebody
has walked in was already solved, in Home Assistant, by the thing
that can actually tell.

What the endpoint did not do was look in the outbox. It gathered
held reminders and never-said ones, composed a greeting behind
them, and anything a producer had published while nobody was home
sat pending until the sweeper expired it. Eight of them had,
against this one client, by the time this was written.

#### So the greeting collects

`Welcome` now asks what the **calling client** owes, puts it
behind the greeting with the reminders, and marks it said only
after the speaking succeeded -- the rule the held reminders have
always followed, for the reason written beside them: *said and
not recorded is heard twice; recorded and not said is lost.*

The client is whichever one is asking. The request arrives with
its own token and the copy it owes is its own, which is the whole
point of a copy per client: another client of the same person
still owes it and will say it in its own time.

Nothing is collected when the caller is not a client. The
internal greeter has no token and nothing is owed to it.

#### Why this is not a timer

A screen collects every twenty seconds because it is always in
front of somebody. A room is not. Polling would mean deciding, on
the server, whether anybody is there -- which is the thing the
design exists to avoid, and which Home Assistant answers far
better than anything here could.

So the two clients collect in the two ways that suit them, and
neither needed the server to hold an opinion about rooms.

#### Not proved aloud

The logic is tested through the real route: what was published
while out is said at the door and marked; a failure to read what
is owed costs the greeting that and not the greeting, and marks
nothing.

What has not happened is a live arrival, because by the time this
worked it was past midnight and triggering one would have made
the house talk. The next real arrival proves it, and there is
nothing pending to be wrongly said when it does.

#### Still not collecting

The phone. Tasker holds a client and has six expired copies
against it, having never collected anything. It is not a speaking
client, so nothing is lost today, but a copy is being written for
it every time and nobody reads them.


### The satellite stops wedging on a failed sound

Owner, 7 October 2026: *"do the satellite fix."* The diagnosis was
written when it happened; this is the applying of it, and the
thing standing in the way was not the patch.

#### The compose file had been lying for days

`satellite/docker-compose.yml` mounted `./build/satellite.py`. The
running container was mounting `./patched/satellite.py`. A
container keeps the mounts it was created with, so the wrong path
in the file cost nothing at all -- until somebody recreated it,
which is exactly what applying this patch required.

They had drifted twenty-two lines apart. `./patched` has the chime
that tells the person it is their turn in a continued
conversation, with a comment explaining that without it *"asked
'send you what?', the owner thought it had stopped listening"*.
`./build` is from two days earlier and has none of it. Recreating
against the file as written would have deleted that fix in
silence and left no trace of having done so.

`./patched` is what runs and is newer, so it is canonical and all
three mounts now point at it. Verified before recreating by
diffing each file against the copy inside the running container:
identical, so the recreate changed `libmpv.py` and nothing else.

#### The patch

Upstream fires the finished-playing callback only on a clean end
of file and returns on everything else. For three of the four
reasons that is right -- stop, abort and quit are deliberate, and
firing the callback would claim the speaking finished when it was
interrupted. For `error` it is not: the track will never play, so
nothing will ever fire the callback, so the pipeline never ends,
and every wake word from that moment is refused.

So an error ends the turn exactly as finishing does.

#### Reproduced, which is the part that matters

The original fault was a TTS response that failed to fetch. The
same thing on purpose: announce a media URL that does not exist.

	_on_end_file: reason=4 (is_eof=False, failed=True), state=ERROR
	WARNING: playback failed (reason=4); ending the turn anyway
	_on_end_file: invoking callback
	Playlist finished, invoking done_callback

The entity went back to `idle` rather than sticking at
`responding`. Eleven wake words heard since the recreate and none
refused.

Before the patch the same sequence read *ignoring non-eof event*
and the satellite was deaf for seventy-three minutes.


### The description is written in three calls

Asked what the state of profiling was, and found it had not been
written since nine in the morning. It runs hourly.

	"cannot describe them"  error="profile: asking for the
	   description: I am having some difficulty reaching the server."

That is the persona covering for something more specific:

	platformai: the reply was empty    after=1m7.668429272s

Forty six of those, every one taking between fifty four and sixty
seven seconds. Our own timeout is two minutes and never came into
it: the gateway gives up at about a minute and answers with
nothing. It is not an empty reply, it is a cut-off.

Going back to 29 September, so none of it is new. What is new is
that it used to finish in time and now does not, because the
prompt grows with the history: a week of messages, four weeks of
events, nine hundred tool calls, and since yesterday the diary.

#### Three calls, one per part

The description was always three parts under three fixed
headings, and the instructions for each never mentioned the
others. So they are three calls now. The evidence is the same for
each -- it is the writing that is split, not the reading -- and
each has about a third as much to generate, which is the half
that was running out of time.

The cost is honest: the evidence goes up three times, about
five and a half thousand tokens a call against six for the one.
Input is cheap and was never what the gateway was cutting.

**The parts are written in order and the last is given the
first.** `WHAT THEY ARE LIKE` says in its own instructions that
it is what the first part adds up to, so it is handed what
`WHAT IS SO` just wrote and told it may not contradict it. The
other two are told nothing about each other.

**The headings are written here, not asked for.** Each call is
told to write its part and not the heading. One place decides how
they are spelt, which is what lets them be read back.

#### A part that fails keeps yesterday's

Three calls are three chances to fail, and the gateway also
refuses for being busy -- sixteen of those today. A description
missing a third of itself is worth less than one where a third is
a day old, so a failed part keeps what it said last time and the
others are still rewritten. The parts are about different things
and go stale at different rates anyway.

`Split` reads a description back into its sections to do that,
which is only possible because the headings are written by this
package rather than by a model.

Nothing is kept at all when no part is new. A description rebuilt
entirely out of yesterday is yesterday's, and writing it again
would only move its date.


### Saying the old time twice

Owner, 7 October 2026, with a screenshot of the speaker having
said it:

	Moved to 7:00 am tomorrow, sir. It was at 6:00 am.
	The time was 6:00 am on Wednesday 7 October 2026 before.

Two sentences about the same fact, the second one a machine's.

`Changed` owes the answer the **from** value, on reasoning that
is still right: *the from is what gets dropped, every time, and
it is the half they cannot reconstruct*. `Ensure` appends a
sentence when an owed fact is missing.

What it called missing was wrong. The fact is built by `spell`,
which writes `3:04 pm on Monday 2 January 2006` -- the whole
date, because that is what the tool shows the model. The test was
`strings.Contains`, so the answer had to carry that spelling
exactly. It said "6:00 am", as anybody would, and was judged to
have left the old time out.

So the check reads the substance rather than the spelling: a fact
counts as said when the answer carries the whole of it **or its
leading two words**. A time, a title or a name is distinctive in
its first word or two, and the rest of the spelling is the
model's business.

Two words, not one -- "the" would pass anything -- and not three,
which starts spelling out a date again. A fact of fewer than
three words has no front to lead with and is still matched whole.

**What is still caught.** Dropping the old value altogether,
which is the thing this exists for. And a wrong one: *"it was at
8:00 am"* does not carry "6:00", so the sentence is still added.

Saying it loosely is a worse answer than saying it exactly.
Saying it twice is worse than either, and it is the one of the
three that sounds broken.


### A family answered from one of its kinds

Owner, 7 October 2026, with the exchange:

	Who called me recently?
	You received no calls today, sir. Your most recent received
	call was yesterday -- Alekhya at 9:28 am on Tuesday the 6th.

	I got a call at 09:49AM. Right?
	Yes, sir. You missed a call at 9:49 am this morning from an
	unknown number.

It knew. The stored calls say what it did:

	event_recent {"kind": "call.received", "limit": 10}
	event_recent {"kind": "call.missed",   "limit": 5}

The first question was answered from `call.received` alone. The
second named the thing and it found it at once.

#### The warning was already there

From the `kind` description, written before any of this:

> *Never guess one from the words of the question. A family holds
> the same thing happening several ways and the question names
> only one of them: asked when somebody called, the answer may be
> that they were called instead, and a kind taken from the
> phrasing turns a thing plainly there into nothing at all.*

Nearly this example, in nearly these words, and it did not hold.
Which is the rule about where to put things: **enforce in the
server first; prompt wording is the last resort.** The wording
was already the last resort and had been spent.

#### So the reading says what it left out

A reading of a single kind now comes back with what else of its
family is in the same window, counted:

	The same family also reported this today, and you did not
	ask for it: call.missed 1.

The answer cannot be "no calls today" while that is sitting
underneath it. Nothing is widened and nothing is refused -- the
reading is still exactly the kind that was asked for, and the
listing is unchanged. What is added is the one fact that makes
the omission visible.

Only for a single kind with a family: a prefix has already read
the whole of it, and a kind with no dot has no siblings.

`Nothing` already did a version of this, and only when a
`contains` was given as well and the kind found nothing at all.
Here the kind found something -- yesterday's call -- so it never
looked. Finding something is exactly when this matters, because
that is when an answer sounds complete.

#### The same question afterwards

	Today you missed one call at 9:49 am from an unknown number.
	Yesterday, Alekhya called you at 9:28 am, and you also missed
	two calls -- one unknown and one from Megatron. You called
	Amma twice, once last night and once just after midnight, sir.


### One late night was hiding every hour

Owner, 7 October 2026, asking whether profiling could be an
algorithm rather than a model, and describing what he wanted from
it: *"location: home exit 10am and reaching office 10:20am,
leaving office 7:30pm, badminton 8pm ... that is a pattern found
across days. Screen lock at 1-2am and unlock at 9am can denote my
sleeping time."*

Half of that was already implemented and returning nothing:

	- home:   there on 7 days, arriving at no particular hour,
	          leaving at no particular hour
	- office: there on 3 days, arriving at no particular hour

#### Why it never found one

`aroundWhen` sorted the hours and gave up if the first and last
were more than four apart. A range, over every observation, with
no middle.

The hours somebody actually came home that week:

	2, 10, 11, 21, 21, 21, 21

Four of the seven at nine in the evening. The furthest two are
nineteen hours apart, so the answer was "at no particular hour".
One late night hid four ordinary ones. The office read 11, 11,
11, 17, 18 and went the same way.

The file already knew better. `middle`, thirty lines further
down, takes the median of a set of gaps *"which a single long gap
cannot drag about"*. The same thought had not been applied to the
hours.

#### The densest band, not the range

Every hour is tried as the start of a four hour band and the
fullest one wins. Fewer than half the observations inside it is
not a time of day, it is some of them happening to agree.

**Round, because hours are.** A band may run from eleven at night
to two in the morning, which is exactly where a bedtime lives and
where a range test reads two habits twenty two hours apart.

**Two words, not one.** Two thirds agreeing is "usually around";
half is "often around". Saying "usually" of a coin toss is the
overstating the function exists to prevent, and the old code said
it of anything that passed at all.

#### The same week afterwards

	- home:   there on 7 days, arriving usually around 9pm,
	          leaving usually around 10am, usually staying 12 hours
	- office: there on 3 days, arriving usually around 1pm

Which is the first half of what was asked for, and it was there
all along.

#### And three, not two

Checked against what `visits` actually reads -- the phone's
geofence crossings, not the server's stays -- the first version
overstated. Office arrivals were 19, 11 and 13. Two of the three
fall in one band, which is half of them and passed, and the
middle of that band is one in the afternoon. A confident hour
read off two observations two and a quarter hours apart.

So a band must hold at least three. Two of anything is a
coincidence however large a share of a small number it is, which
is what `changed.Enough` says about the same question elsewhere.
Home keeps its hours -- three of four at nine, four of six near
ten -- and the office honestly has none yet.

#### What is still not there

The chain. This says when he reaches home and when he leaves it,
each on its own; it does not say that leaving home is followed by
reaching the office twenty minutes later, day after day. Pairing
exists but only inside `Soon`, a few minutes wide, so it finds a
screen unlocked and locked again and never a commute.

And the sleep example needs the opposite of `Soon`: the last
thing at night and the first in the morning, hours apart.
`quiet.go` already measures that silence for another purpose.


### Three thousand tokens of thinking and not one word

Owner, 7 October 2026, looking at the Personality panel: *"update
the new profile here based on the logic."* It was showing a
description written on 6 October at 15:11. Nothing had replaced
it in a day and a half.

The log said the same thing every hour:

	profile: asking for "AN ORDINARY DAY":
	I am having some difficulty reaching the server.

Which was wrong in every particular. The server was reached.

#### What the numbers ruled out first

107 failed calls, 62 of them the profile, every one of them
*"the reply was empty"* after between 52 and 76 seconds. Against
that, the calls that worked:

	38,426 tokens in   ->  2.59s
	 6,040 bytes out   ->  3.26s

So neither a long prompt nor a long answer explains a minute of
nothing. Both of the obvious theories were wrong, and the split
into three calls -- made the day before for exactly this failure
-- had been written against the wrong cause.

#### The log could not say why

`finish_reason` was being parsed off the wire and dropped on the
floor. One line of it, and:

	the reply was empty (376 events, finished "maxtokens",
	3000 words written)

The gateway caps a reply at three thousand tokens. Reasoning
comes out of that same three thousand. And one purpose, alone in
the codebase, asks for the most expensive setting there is:

	var Hard = map[environment.Purpose]string{
		environment.PurposeProfile: "high",
	}

The model spent the entire budget thinking and emitted no text at
all. That setting and that failure were added on the same day.

**High and medium are the same failure.** 3000 of 3000 either
way, measured. Which says the dial is not what decides how long
it thinks for -- an open question and room to think is -- so the
room is what was taken away. `low`.

#### Two more things were hiding behind it

**A part that had never been written took the others down.**
`write` falls back to what a section said last time when its call
fails, and returns an error when there is nothing to fall back
on. `AN ORDINARY DAY` was new, so there never was. Two parts
succeeded and were thrown away, every hour, for a day. A
description missing a section is worth far more than none, which
the function's own comment already argued one step short of.

**`Written` was a field with no column.** A memory the server
writes is allowed eight thousand characters and one a person
asked to have kept is allowed four. Which of the two a memory is
was decided by `Memory.Written`, and `memories` had no such
column -- so everything read back out of the table came back as
the person's. The longer limit applied on create and never on
update.

The standing description is created once and rewritten hourly. It
got the long limit exactly once, reached 4,013 characters, and
every rebuild since was refused for length. The comment on
`MaxWritten` describes raising the ceiling for this one memory;
the raise never reached the path that needed it. It also means
**Save on the Personality panel could not have worked** -- 4,013
is already past what a hand-edit is allowed.

#### Afterwards

6,609 characters, all three parts, and the hours from the day
before are in it:

	He is at home on 7 of 7 observed days, arriving usually
	around 9pm and leaving usually around 10am.
	He is at the office on 3 of 7 observed days, with no fixed
	arrival or departure hour.

#### Still wrong, not fixed here

`Update` clears `embed_model` unconditionally, and the API marks
a hand-edited description by writing `byHand` into it. So `Mine`
on the read endpoint can never be true.


### The first second of an announcement

Owner, 7 October 2026: *"I'll be doing something and suddenly
Jarvis is saying the important stuffs fast before I even start to
listen. Is there anything like rules already defined so that I
will be able to understand whatever Jarvis says as an
announcement?"*

There was not, and the one rule that existed pushed the other
way. `aloud`, which every reply is shaped by:

	Be brief and direct: say the answer first, then only the
	detail that matters.

Correct for a reply. Somebody asked, and is waiting. It is
exactly backwards for an announcement, which arrives at somebody
in the middle of something else.

The last ten announcements, with the subject and the number
inside the first six words every time:

	Your phone battery has dropped to twenty-four percent, sir,
	and now would be the time to find a charger.

Miss the opening and what is left is "percent, sir, and now
would be the time to find a charger": a number that has already
gone past.

#### The greeting already solved this

`greet.Opening` puts "Good evening, sir." in front of whatever
the model wrote. That salutation carries nothing, and carrying
nothing is its job -- it is the second somebody spends turning
their head. It is why a greeting is easy to follow and an
announcement is not, and nothing had taken the lesson across.

#### Their own word, and no other

The opening is the persona's form of address and nothing
invented: `persona.Address`, which is "sir" or "boss" or empty.
It is the one word the assistant already says to them, it
carries no information, and a persona that calls them nothing
gets no opening rather than a line of English it would never
say.

Said, not written. The spoken copy gets the opening; what goes
into the conversation does not. A word whose whole purpose is to
be talked over has no business in the transcript.

**Not every kind.** A greeting opens with its own salutation and
the renaming follows an answer somebody was already listening
to; both would be opened twice. So which kinds arrive cold is a
column in the policy table beside the audience, because adding a
kind should be one edit.

#### And the model stops leading with the fact

The lead-in buys about a second. The sentence after it still has
to survive being half-heard, so the write step is told:

	This will be said to somebody who is not listening yet.
	Lead with what is happening in general, then the
	particular. Not "twenty-four percent, sir" but "your
	battery is getting low, sir -- twenty-four percent".

And told that a word is already being said in front of it, so it
does not write an opening of its own -- the same thing the
greeting prompt has always said.

#### What is not done

The chime. `assist_satellite.announce` takes a `preannounce`
flag and it is hardcoded false, with a reason that is true of
the aside it was written for and false of everything that
arrives out of nowhere. Left until somebody has heard whether it
is pleasant or irritating at eleven at night.

And the fifteen second cut is unchanged, so an opening spends
part of a budget that was already fixed.


### Seeing the rules

Owner, 7 October 2026: *"currently I'm having a hardcoded 25, 20,
15, 10, 5, 100 for battery, and like this I'll be adding many
rules. I can't be adding rules manually like this -- there should
be a way for AI to understand how I did those and add these rules
automatically. Before that I should be able to view the standing
rules in UI."*

The second sentence first, and he is right to put it there.
Nothing should write rules on somebody's behalf while the only
way to see the ones that exist is to ask aloud or read the table.

`GET /v1/rules` and a page beside Reminders. Name, schedule, every
step in order, and what happened last time it ran.

**Read only.** Rules are made with `rule_add` and called off with
`rule_cancel`, which check the grammar, the gate and that a tool a
rule names exists and only reads. A second way in is a second
place for all of that, and the first thing it would diverge on is
the thing nobody tested.

**The last run is on the face of it, not behind a tap.** The
ordinary output of most of these is silence, and silence has two
causes that look identical from outside: a rule that ran and found
nothing, and a rule that is not running at all. `Last ran at 17:37
-- coming_due found nothing` tells those apart at a glance.

#### What it showed in its first minute

	when to set off   every 5m0s
	  tool -- leaving_due
	  relay
	  Last ran at 17:37 -- leaving_due: google: the connection
	  has expired and must be granted again: The permission was
	  withdrawn.

The Google link is dead and has been failing every five minutes,
and nothing had said so. It was in `jobs.why` the whole time, in a
column nothing read.

That is the argument for the page in one line, and it is also the
argument for the thing it was asked for next: a rule that fails
quietly is worse the more rules there are, and the answer to
"write me more of these" has to include noticing when one stops
working.

#### And the first version was unreadable

Owner, immediately: *"too much cognitive load."* Fair. Every step
of every rule was on the page at once, seven of the twelve were
battery thresholds, and their steps differ by one number inside a
paragraph of instruction repeated verbatim. Twelve rules filled
more than a screen.

Two lines each now. What it is and when it fires; what came of
the last run. The steps are behind a tap, because they are what
somebody looks at once, to see how a rule was built, and never
again.

**The last run did not collapse with them.** Hiding it would hide
the only sign that a rule has stopped working -- which is the
thing worth noticing and the thing nobody goes looking for.
Shortened to one line and cut with an ellipsis; the whole of it
is in the expansion, where a three-sentence error from somebody
else's service can be read without being most of the page.

And the arguments are words now. `event_recent (kind
battery.level, limit 6)` rather than the JSON, sorted because a
map has no order and a listing that shuffled between refreshes
would be worse than the JSON. `saying` is dropped: it is what the
assistant says while the tool runs, which is about the moment and
not about the rule.

#### Show past

A rule that was called off is not a rule somebody has, and
nothing ever removes one: `battery percentage report` and
`engine status` were two lines to read past on every visit, and
there is no limit on how many accumulate. Standing only, with
the rest behind the same words the reminders use.

**Narrowed in the query, not afterwards.** Filtering what came
back would let a pile of cancelled rules fill the limit and hide
the active ones underneath -- which is the one listing nobody
could do without. So `Mine` takes the states, variadically, so
that `rule_list` asking for all of them did not have to change.

**Its own flag, not the reminders\' one.** Two lists on two
pages, and a toggle on one quietly changing the other is the
sort of thing nobody finds until it confuses them.


### Rules speak in the room and nowhere else

Owner, 7 October 2026, after being shown that no rule can reach
him when he is out: *"currently let it talk only when in room."*

So the reach-and-hold work is not done. A rule publishes as
Immediate -- the room, dropped when nobody is there, worth saying
for five minutes -- and that is the whole of it. Reminders remain
the only thing that follows him out of the house, which is what
they were always for.

What this rules out, and it is worth being plain about it: a rule
whose moment is the moment he leaves cannot work. Setting off
with eighteen percent is the clearest case -- the trigger is the
helmet connecting, which is him walking away from the only thing
that can speak. That rule is not worth building until this
decision changes.

What it leaves: anything whose moment is while he is at the
laptop. The four percent overnight slide is the example -- he was
home, awake, and nothing said so for four hours.

#### How it already waits, which was misread here first

Owner, correcting it: *"it is sent to the client, the client
checks the presence and says the pending, and on greetings it
will say the pending events."*

Which is what is built. A rule publishes to the outbox as well as
speaking, the endpoint hands each client the `away` of the kind
rather than deciding for it, and `announcements.Mark` takes only
said or dropped -- *"a voice client in an empty room says nothing
and writes nothing, and the announcement is still waiting when
the watch comes back into range."* The greeting then reads what
is waiting and says it as part of hello, because a person would
not say hello and then read out a list.

So nothing is spent. The first reading of this -- that a rule
firing while he was out burned its message -- came from reading
the engine and the presence gate and not the outbox, and was
wrong.

The one thing that is true of it: the direct `Speak` beside the
publish does talk to an empty room, which is speech nobody hears
and no loss. That half goes when every client collects, which the
step's own comment in main.go already says.


### And, or, and what time it is

Owner, 7 October 2026: *"time is a trigger, event is a trigger. I
should be able to add like and and or operator -- like if time is
10am and battery is less 25 or location is office."*

Half of that already worked and the half that did not was not
the half it looked like.

**And was always there.** Two check steps in a row are two things
that must both hold, because a step may stop the ones after it.
`battery at 10` has been doing it since it was written.

**Or had nowhere to go.** `ParseGate` reads exactly one source,
one operator, one value.

**And the clock could start a rule but never qualify one.** `at
10:00` is a schedule; there was no way to say *when the battery
drops, but only after ten*. That asymmetry is also why reminders
cannot move onto the engine: no weekdays, no weekly.

#### Where the or goes

Inside one check, never between steps. Steps are the and, a step
is the or, and anything of the shape *this and that and (either
of these)* can be written. No nesting, no parentheses, no
expression language -- the line the engine holds is unmoved, and
it has to be: Tasker exists, Home Assistant exists, and a third
automation language in this house would be a mistake.

	check  hour >= 22 or day is weekend
	check  battery.level < 25

The split happens on the word before anything else is read, so
each part is parsed by the same function one alone would be. It
costs one thing: a value containing "or" cannot be written. No
source or operator contains it, and the values that might are
compared with `is`, where the place can be named differently.

#### The hour, and the day

	hour >= 10      hour < 18
	day is weekday  day is weekend  day is saturday

In the person's own zone, because an hour means nothing anywhere
else. Neither can be a trigger: a time of day does not arrive, so
there is nothing to cross.

The day names are the ones Go prints, which are English. That is
the same bargain cron makes and it does not break the rule about
hardcoded English, which is about matching what somebody said
into a microphone; this is a condition somebody writes down.
`weekday` and `weekend` are there because they are what anybody
actually wants of a clock, and spelling them as five names is
five chances to leave one out.

#### Two bugs the live probe found and the tests did not

**An edit that never landed.** The script rewriting `check` also
touched a struct field, that assertion failed, and the whole
script wrote nothing -- atomically. It still compiled, `make
check` still passed, and `check` was still calling `ParseGate` on
a string with an `or` in it. A rule inserted into the database
and watched is what showed it.

**Two questions that had become one.** `Needs()` returned empty
for anything that could not be a trigger, and the check used it
to decide what to look up. A routine is both: it cannot set a
rule off, and it does have to be fetched. So every condition on
what somebody usually does compared an hour it had never read.
Split into `Needs` -- what to look up -- and `Arrives` -- what
could set a rule off.

Both were found by inserting two rules, one that should pass and
one that should not, and reading `why` afterwards. Neither was
reachable from a unit test, because both were in the wiring.


### Leaving twice is leaving twice

A trigger fired when its condition had *changed* since the
reading before. Right, and the reason it exists: a battery
falling 12, 11, 10, 9 speaks once, at nine, because before that
it was not under ten. Without it a discharging phone says
something every reading until it dies.

Wrong for everything that is not a measurement. This person's own
week:

	Sun 04  place.exited "home"
	Sun 04  place.exited "home"

Out, back, out again. The second was silent, because the last
departure had also been from home and nothing had "changed".
Two named missed calls in a row would have gone the same way.

#### Two sorts of reading, and no list of them

A measurement has a value at every moment. A battery is always at
some percent, so two readings the same really do mean nothing
happened.

A thing that happened has no value in between. There is no state
of "having left"; there are departures, and two of them is two.

The engine had no way to tell them apart, and a list of which
event kinds are which is exactly the thing this codebase refuses
to keep: it is wrong the moment a new device starts reporting.

#### Decided by the value, not the operator

	a number   ->  a line to cross, speak when it is crossed
	a word     ->  a thing that happened, speak each time

Which needs nothing from whoever writes the rule and no knowledge
of any device. Checked against every rule that exists: the six
battery thresholds, `place.exited is home`, `call.missed is not
unknown`, `bluetooth.connected is bike helmet` -- all correct.

**By the value and not the operator, and that is the whole
subtlety.** `battery.level is 100` is written like naming a
thing and is a measurement. Sorted by the operator it would be an
event, and the rule would speak every time the phone reported a
full battery while it sat on the charger.

#### What it costs

A kind that reports the same word over and over would set its
rule off each time. It would still only ever *say* it once -- a
rule remembers its last sentence -- so the cost is work rather
than noise.


### A trigger that waits

Owner, 7 October 2026, on the engine's limitations: *"we need to
fix all, do the wait one."*

Some questions cannot be answered at the moment they are asked.
Whether a phone is actually charging is unknowable when it is
plugged in -- only fifteen minutes later, by whether the level
moved. Whether a missed call mattered is unknowable as it comes
in, only once nobody has rung back.

Both were possible before only by asking every few minutes for
ever: a loop running seven hundred times a day to catch the two
occasions it is about.

	when power.plugged is true then 15m

#### Nothing sleeps

A trigger with a wait does not run when it fires. It sets its own
next time, and the clock that was always there picks it up. So a
rule waiting a fortnight costs exactly what a rule waiting a
minute costs, which is a row.

That also means the delay is kept nowhere new. A watching rule's
`next_at` was always null -- that is what kept the clock from
touching it -- so the one spare field was already there, and
having a time in it now means *set off, and waiting*.

**`Arm` and not `Ran`.** Its own call on the store, because
nothing has happened: a rule that was set off and is waiting has
not run, said anything or failed, and writing it down as a run
would put a line in the record for something nobody did. The
page reads the same field and says "set off -- runs at 20:22",
which is a state it could not otherwise show at all.

**Re-armed if it fires again while waiting.** Plugged in, pulled
out, plugged in again is a fresh fifteen minutes and not the tail
of the old one.

**And the staleness test now reaches it.** `one` skipped that
check for a watching rule, on the grounds that a watching rule
has no time and the reading that set it off arrived a moment ago.
Both halves stop being true the moment one can wait, so the test
is on having a time rather than on being a watching rule.

#### Proved on the running server

	14:50:01  a reading arrives  -> armed for 14:52:01, last_at null
	14:52:02  the clock runs it  -> the check stopped it, as written
	          next_at null again -> waiting for the next trigger

One second late, nothing slept, and it disarmed itself.


### The rules are the examples and the routine is the vocabulary

Owner, 7 October 2026, from the start of all of this: *"I will use
that to write the rules, and further I'll ask the AI to write the
rules by itself with examples of the existing rules, and it should
use the routine too."*

Which needed no new tool, because the thing it needed was already
enforced. `rule_add` writes, and a writing tool refuses to run
until its domain has been listed in the same turn. So anything
put into `rule_list` is in front of whoever is about to write a
rule, every time, without their having to know to ask.

The listing already gave every rule with its exact schedule and
what its steps come to -- ten worked examples in the person's own
house, which is better than any example that could be written
into a description. What it did not give was what else a
condition may read. Now it does:

	- usually.home.leaves -- home leaves around 10am, on 4 of 6 days
	- usually.home.arrives -- home arrives around 9pm, on 3 of 4 days

**Counted, deliberately.** A condition standing on four days out
of six is a different proposition from one standing on twenty,
and whoever is deciding whether to use it should see which.

**And told what they are not.** Nothing about a routine arrives,
so none of them can be what a rule waits for -- said in the
listing because that is where it will be read, rather than left
to be discovered by writing a rule that waits for ever.

**Nothing at all when there is no routine yet.** The first weeks.
A rule written without these is the rule anybody would have
written before there were any, and a paragraph explaining the
absence is worse than silence.


### A rule says how long it matters

Owner, 7 October 2026, shown that the voice client had said none
of the announcements it was ever offered: *"fix the five
minutes."*

	home assistant   expired 18   said 0
	tasker           expired 15   said 0
	chrome-dhanush   expired  1   said 14

Everything a rule says is published as Immediate, which is worth
saying for five minutes. He is out for hours. The copy was swept
long before he walked back in, which makes the earlier correction
in this file -- that the outbox holds it and the greeting says it
-- true of the design and false eighteen times out of eighteen.

#### One number was the wrong fix, and the eighteen say so

	3  the assistant naming a conversation
	6  battery warnings -- 24%, 19%, 14%, 9%, 4%, below 50
	5  probes from an afternoon of testing
	2  missed calls from somebody the phone could not name

Nine of them are worthless hours later. A warning that the
battery was at four percent, read out at the door that evening,
is worse than silence. Two were worth keeping.

Nothing about the kind tells those apart. They are all Immediate.
What knows is whoever wrote the rule, so that is who is asked,
and empty still means the kind decides as it always did.

	when call.missed is not unknown then 30m, kept 6h

#### Where it is kept, and why there

As written, like the schedule beside it, because it is read back
to whoever said it and "6h" is what they said. On the job and not
on the step: a rule's announcements are all the same thing
arriving at the same moment, and a sequence that kept one of them
for the afternoon and binned the next would be two rules.

Carried on the [job.Turn], because the step that publishes never
sees the rule.

`outbox.New` is unchanged and still means the kind's own answer.
`Keeping` is the one that asks, which leaves every caller that
has no opinion -- reminders, the renaming, the greeting -- saying
exactly what it said before.


### Changing a rule instead of writing a second one

A rule could be made and called off and nothing else. So moving a
threshold meant writing a new rule and cancelling the old one, and
the six battery rules are what that looks like after a week:
near-duplicates, because tuning one was more work than adding
another. The owner means to add many more, which makes this the
limitation that compounds.

	rule_update, id and whatever is changing

#### Read back off the steps

A rule's arguments are assembled into a sequence and never stored
as themselves, so changing one means taking the rest back out.
That is exact rather than nearly so: `assemble` writes one step
per thing and nothing else goes in, so `parts` is a matter of
looking rather than guessing.

	job.Call      -> a lookup, with its arguments
	job.Check     -> the condition
	job.Announce  -> the fixed words
	job.Write     -> the question, and what it may call

**Naming one alternative takes the other away.** Words and a
question cannot both be there, so a change that gives `say` drops
`ask` before assembling. Without that, turning a worked-out
sentence into fixed words would be refused for having both --
which is the commonest change anybody would want to make.

**Through `job.New`.** A changed schedule is read by the same
thing that reads a new one, so a rule cannot be edited into a
state it could not have been made in. Tested: an unreadable
schedule, an hour of 99, and a keep of "a while" are all refused
and leave the rule as it was.

**And it forgets what it last said.** The rule is not the rule it
was, so the sentence it was holding back is no longer a repeat of
anything -- and somebody who has just changed a rule wants to
hear whether it works, not silence from a comparison with the old
one.


### A reading against its own past

Owner, 7 October 2026, on the engine's limitations: *"it can't
compare two things."* It could only compare a reading with a
number somebody had written down, so *lower than it was an hour
ago* had nowhere to go -- and that is the whole of knowing
whether a phone on charge is actually charging.

	check battery.level <= 15m ago

#### A value, not an operator

Which is the decision worth recording. Three new operators --
rising, falling, steady -- would have given less and needed rules
of their own. As a value, every comparison already there works on
it unchanged:

	<= 15m ago   has not risen in a quarter of an hour
	>  1h ago    higher than it was
	is 10m ago   has not moved at all
	is not 1h ago  has moved, either way

#### Which reading from back then

The newest one at least that old. Not the oldest held, and
nothing interpolated: what the value actually was then, as near
as anything was reported.

**Nothing that far back means the condition does not hold.** An
unknown reading never passes, which is the answer a gate already
gives about a kind nobody has reported -- and the alternative is
a rule that fires hardest on the day a device starts reporting,
when its past is emptiest.

**Only where there is a past.** The clock, presence and a routine
are answered from the moment or from a number already worked out,
and `older than` asks how old a reading is, which is not a thing
to compare with the past. All refused when the rule is written.

#### Reading deeper, only when asked

The check fetches one reading per kind, and as many as [job.Over]
when any of its conditions compares against the past. Asked of
the conditions rather than of each gate, because the fetch is
once per kind: a condition satisfied by the battery now or by the
battery an hour ago needs the hour either way.

#### Proved on the running server

	readings   15:17 91%   14:53 92%   14:41 93%   14:19 95%

	battery.level <= 2h ago   ->  said it
	battery.level >  2h ago   ->  the check did not pass


### The two rules the day was for

Twenty-four commits of engine and no new rules, so these are the
proof it was worth it. Both were impossible in the morning.

	plugged in and not charging
	  when power.plugged is not nothing then 15m
	    look up   event_recent (kind battery.level, limit 6)
	    only if   battery.level <= 15m ago
	    say       it is plugged in and has not risen

	a call nobody returned
	  when call.missed is not unknown then 30m, kept 6h
	    look up   event_recent (kind call.missed, limit 5)
	    look up   event_recent (kind call.made, limit 10)
	    say       only if one has had no call since

The first needed the wait -- whether a phone on charge is
charging is unknowable as it is plugged in -- and the comparison
with its own past. The second needed the wait and needed keeping
for the afternoon, because he is out when the call comes and the
answer is as true at nine as it was at two.

Added through `rule_add` itself rather than written into the
table, so they were validated and assembled exactly as a dictated
rule is.

#### Two things found by writing them

**`power.plugged` carries `value: "unknown"`.** The charger type
is not resolvable on the phone, so `power.plugged is true` would
never have matched anything. What is wanted is *any* reading of
that kind, and the idiom for it in this grammar is `is not`
against a value that cannot occur. It works and it reads badly,
and the honest fix is a gate that says a reading arrived at all
rather than comparing its value -- not today.

**`rule_add` can only make one check.** The engine ands across
steps, and `assemble` takes a single `when`, so a dictated rule
gets one condition with `or`s inside it and no second check. The
and is reachable only by writing the steps directly. That ruled
out the polling form of the charging rule, which wanted three
conditions, and pushed it onto the trigger -- which is better
anyway, firing once a plug-in instead of six times an hour.


### Whether a thing happened, rather than what it said

	when power.plugged arrives then 15m

Every gate asked about a value. For a measurement that is the
whole question -- what matters about a battery is what it says.
For a thing that happened the value is often beside the point and
sometimes absent: a phone cannot always tell what it has been
plugged into and reports "unknown", so the rule written an hour
earlier had to say `is not nothing` -- a comparison against a
string that could never occur. It worked and it read like a
trick, and every occurrence-shaped kind would have needed the
same trick: a screen unlocked, a call made, a network joined.

#### The word is required

The first version took the kind alone -- `when power.plugged` --
and two existing tests refused it, rightly. `when nonsense` would
then parse, and become a rule that never fired and never said
why. The engine keeps no list of event kinds on purpose, so it
cannot tell a real one from a typo; what it can do is insist that
somebody meant it.

#### And it is never a measurement

It asks nothing about the value, so there is no line to cross and
every arrival is its own occasion. Without that, a second plug-in
would have been silent for the same reason leaving home twice
was.

Mended the rule with `rule_update`, which is the first thing that
tool has been used for and the reason it exists: the rule was
tuned rather than rewritten beside its predecessor.


### A silent rule that logged itself as having spoken

Found while being asked what was still missing, which is the way
these are usually found.

	outcome: said
	said:    "Someone the phone could not name has tried..."
	because: situation_find found nothing

`anything amiss` runs every two minutes, finds nothing, and had
written itself down as having spoken three hundred and five
times -- quoting a sentence from hours earlier. The one page
built to answer *"I should see in UI when it triggered, why it
didn't speak, if spoken what it spoke"* was answering the third
part with a lie.

#### One variable doing two jobs

A step that stops the sequence hands back the sentence this rule
said last time, so the memory that stops it repeating itself
survives a run that found nothing. `settle` needs that. The
record of what was spoken needs the opposite -- nothing -- and
both read the same local.

	spoke      what a step said in this run
	remember   what to keep, which may be from before

A stopping step's sentence becomes the memory and never the
speech. A step that does not stop and hands back a sentence has
just said it, and is both.

**And only where nothing has been said yet in the run.** A
sequence that spoke and was then stopped has to remember what it
just said, not what it said before -- which is the one ordering
that makes this subtle rather than obvious.

Three hundred rows of the old answer are left where they are.
They are a record of what happened, and what happened is that
the server wrote them.


### When a connection dies, say so

The Google permission expired at 11:41. `broken_at` was set, the
account row recorded exactly what Google said, and the server
wrote *"must be granted again"* into the log **109 times**. The
owner found out nine hours later because somebody happened to
look at a page.

A rule finding nothing is the system working. A permission dying
is a capability gone, and every rule standing on it quietly does
nothing until somebody notices.

	check link.google older than 20m

#### A gate source, not another loop

Everything else that watches has been moved onto the engine this
week, so this went the same way rather than becoming the eighth
Go loop. It is a source a condition can read, like presence and
like a routine: read when somebody asks, never arriving, so it
can never be what a rule waits for.

**A namespace and not one source per service.** `link.` so the
next connection needs a resolver in one map and not a word in the
grammar.

**Working or broken, and nothing else.** Why it broke is the
connection's own business and is recorded beside it; a rule
matching on the wording would break when the wording changed.

**And its time is when it broke.** A working connection is always
nought seconds old, so `older than 20m` asks whether it has been
down for twenty minutes -- in a single condition, which matters
because a rule written by voice gets one check. A permission that
fails once and recovers is not worth a sentence.

**No connection configured reads as working.** Nothing is wrong
with a permission nobody asked for, and a rule warning about one
would be wrong on every server that has none.

#### What it said

	Sir, the connection to Google has expired and needs granting
	again. Until it is, nothing can read your diary.

Kept twelve hours, because it is as true in the morning as it was
at ten at night -- which is the first real use of a rule choosing
its own worth.

#### Found while writing it

The comment claimed a rule could ask how long a connection had
been down, and the validation allowed only `is` and `is not`. The
test written from the comment caught it. The comment was right
and the code was narrower, which is the better way round for that
to happen.


### The button that was configured and never built

`settings_url = http://localhost:8000/settings/google` has been in
the config since September. The server has had `/v1/google/account`,
`/authorize` and `/callback` since then, and the callback has always
redirected to that page. Nothing was ever built to land on.

So the only way to grant the permission was by hand -- which
matters more than it sounds, because the client is in Testing on
purpose and Google expires a refresh token after seven days while
it is. This is a weekly errand, and it had no button.

Settings → Google now shows which account, when it last worked,
what Google said when it broke, and **Grant again**.

#### Why the address cannot come from anywhere else

`Begin` keeps the state in the running process's memory. So the
consent URL has to be issued by the same server that will handle
the callback, which rules out a subcommand, a script, or anything
holding the person's token on their behalf. A page talking to its
own server is the only shape that works, and it was the shape
already designed for.

**Same tab, not a new one.** Google sends the browser back to this
screen when it is done; a new tab would leave the answer in one
window and the person looking at another.

**Off the web it offers the address instead.** There is nowhere to
send a browser on Android, so `openUrl` returns false and the
screen shows what to open. Conditional import, like `address_bar`.

#### What the reconnection confirmed

	connected   2026-09-30 11:40:05
	broke       2026-10-07 11:41:59    seven days and one minute
	granted     2026-10-07 16:49:00

And Google was asked rather than the row believed: all seven
scopes came back, which is worth checking because a half-ticked
consent yields a token missing scopes without complaint. This
consent screen had no granular boxes at all.

`when to set off` had been failing every five minutes for twelve
hours and now reads `leaving_due found nothing`, which is the
healthy answer.

**It will die again around 14 October.** The rule added earlier
will say so within half an hour.


### Trying a rule

A rule could be made and then nothing done but wait for the world
to set it off. That is how a rule logging itself wrongly stayed
wrong for a day, and why two rules written this evening could not
be shown to work at all.

**Try**, beside each rule on the page. It runs now and says what
it would have said, having said nothing.

#### The same path, with one half held back

The same steps, through the same code, against the same readings.
A trial down a different path would be testing the path rather
than the rule, so `Turn` carries a flag and the three places that
publish check it. Everything up to the sentence happens; only the
saying of it does not.

**Nothing is written down.** Not the run, not what it said, not
when it next runs. A trial that moved the rule on would be a
trial that changed the thing being tried, and one that remembered
its sentence would silence the real run that followed it.

**But it is told what the rule last said.** So it answers the
question actually being asked -- would this speak, now, as things
stand. A rule holding a sentence it has already said answers
"nothing new to say", which is the truth and is worth seeing.

#### It found a bug in its first use

Trying `a call nobody returned`, written two hours earlier:

	it would say "There are two missed calls that have not been
	followed up, sir -- one from an unsaved number ending 0867,
	and one from a number ending 8376."

Both were strangers. The trigger `call.missed is not unknown`
filters when the rule *fires*; the lookup beside it then hands
the model every missed call including the ones the trigger was
meant to keep out. And it read out telephone digits, which the
rule about a caller with no name exists to prevent.

The gate filters the occasion. It does not filter the evidence,
and nothing had made that distinction visible before.

Mended with `rule_update` -- ignore the unknown ones, never read
a number -- and tried again:

	it would say nothing -- there was nothing new to say

Which is correct: all three of today's missed calls are from
strangers.

#### "On pressing try nothing happens"

Reported the moment it was shipped, and true in effect while the
button worked perfectly. Two things were wrong and neither was
the button.

**Most rules would say nothing**, and "it would say nothing"
rendered as one muted grey line among other muted grey lines --
indistinguishable from the button having done nothing at all. It
is a panel now, bordered, with the verdict in full-weight text.
The commonest answer has to read as an answer.

**And nothing showed while it ran.** A rule with a model in it
takes seconds, during which the page sat still. The link says
"Trying…" and refuses a second press.

**What a tool found is cut to two lines.** A trial of the missed
call rule printed a week of events twice over and filled a
screen. The trial is read to see whether the rule works, not to
read the evidence again -- the same lesson as the first version
of this page, relearned one screen further on.


### One search is a first attempt

Owner, 7 October 2026, on two answers in a row: *"it should do
research until it finds out, always bring a positive message, and
don't say search results."*

	The search suggests he may have a wife, but the results are
	fragmentary and the first one is incomplete.

	The search returned results for Big Boss Telugu 10, not Big
	Boss Tamil Season 10. That is not what you asked for, sir.

Both true, both one search, and `MaxToolHops` is five. It had four
rounds left and stopped.

#### The instruction that caused it

In `web_search`'s own Avoid:

	And not for a second search to check the first: if the answer
	disagrees with itself, say so.

Written to stop it confirming what it had already found. Read as
*one search only*, so a thin result became a report of a thin
result instead of a better query. The two cases are opposites and
the sentence did not separate them: do not search again to be
sure, and do search again when you have not got it yet.

#### And a search is machinery nobody asked about

"The search returned results for Big Boss Telugu" is a sentence
about a tool. The same facts as a sentence about the world are
"Srishti Vyakaranam went from the Telugu version two days ago,
and I have not found the Tamil one". The first tells them to go
and look themselves; the second is an answer with a gap in it.

So: never describe the searching -- not the results, not the
sources, not that the first one was incomplete -- and say what was
found before what is missing, in that order.

#### Afterwards

Same question, same evening:

	3 web_search calls

	Last week Ananthi and Lakshmi Priya were evicted from Bigg
	Boss Tamil Season 10, sir -- that was week three. There was
	also a twist where Lakshmi Priya, Savinya and Javeeth were
	brought back as wildcards.

#### What is not measured

This widens a tool description, and the standing rule here is to
measure that both ways rather than reason about it -- a wider
description buys over-reach. `make evals` has not been run on it.


### Two conditions, said aloud

The engine ands across steps -- a step may stop the ones after
it, so two checks in a row are two things that must both hold --
and `assemble` made exactly one check. So a rule asked for aloud
could have one condition with alternatives inside it and no
second, and what the engine could do had come apart from what
could be asked for.

That is the worst kind of gap, because it changes the work
quietly: the charging rule written this evening wanted three
conditions and became a trigger instead, and the shape was
decided by what could be dictated rather than by what was right.

	when: "hour >= 22 and battery.level < 20"
	  ->  only if hour >= 22, then only if battery.level < 20

#### "and" is the looser of the two

	a and b or c   is   a, and either b or c

Which has to be learned rather than guessed, and is said where a
rule is written. It falls out of the shape -- steps are the and, a
step is the or -- rather than from the usual precedence, and it
buys every form of *this and that and either of these* with no
nesting, no parentheses and no expression language.

It costs what `or` costs: a value containing the word cannot be
written. Nothing in a source or an operator does, and the values
that might are compared with `is`, where the place can be named
differently.

**And `rule_update` reads every check back.** The arguments are
rebuilt from the steps, so reading only the last one would have
dropped the others the first time anybody changed the words.
There is a test for exactly that.

#### What the probe found, by speaking in the room

A probe with three conditions, the third deliberately unreadable:

	why:  that condition could not be read: "99" is not an hour
	said: all three held

**The check failed and the announcement ran anyway.** A step that
errors does not stop the sequence, deliberately -- *a phone alarm
that could not be set must not swallow the words that go with
it*. That is right for an alarm and exactly backwards for a
check, whose whole purpose is to withhold.

So failing was taken for permission. Nothing else in a sequence
can be wrong in this direction: a lookup that fails leaves the
steps after it less to go on, and a check that fails leaves them
unguarded.

A failed check now stops the rule. Found by writing a throwaway
probe badly, which is the second time today that a deliberate
mistake in a test has turned up a real one.


### A clock and a trigger, a rest, and holding off

The last three limitations with nothing behind them but the
asking.

#### Both mouths

	every 30m also when battery.level < 20

A rule came round on a clock or waited for something, never both.
Right for almost everything and wrong for the one shape that
keeps coming up: *tell me when this happens, and look anyway now
and then in case the moment passed while nothing was running.*

"also" and not "or", because they are not alternatives: both are
live and either sets it off. The clock goes first because a
trigger reads strangely in the middle of a sentence.

Two things had to give. `settle` cleared a watching rule's next
time, which is what keeps the clock off it -- now only where it
has no clock of its own. And `Watching` matched schedules
beginning with the word, so a rule beginning "every" would never
have been woken by anything arriving.

**A dangling "also" is refused.** The word is spaced on both
sides so "every 30m also" does not split, and it parsed as a
perfectly good half-hour with a stray word in it. Caught rather
than left to mean something nobody wrote.

#### A rest

	rule_quiet, an identifier, and how long

Cancelling was the only way to stop a rule and it loses the rule:
a week's peace meant remembering how it was written in order to
have it back. Being quiet is not being cancelled -- it returns by
itself and nothing is rewritten.

Read out of the query rather than filtered afterwards, in both
paths: the clock asks every two seconds and a rule put aside
should cost it nothing, and a rule put aside is put aside from
readings arriving as well.

#### Holding off

	cool: "1h"

The only thing stopping a rule repeating itself was the exact
sentence it said last time, so the same news in different words
went through. This catches everything for an hour and knows
nothing about any of it -- blunt where the sentence comparison is
sharp, and they work together.

Only after speaking. A rule that found nothing has nothing to
hold off from.

One column for both, because they are the same thing: a rule that
is not to run until a moment. Somebody sets it, or the rule sets
it on itself.

#### Days, because a week is what people say

`time.ParseDuration` stops at hours, so "7d" was refused and
*quiet for a week* -- the thing that prompted the feature -- could
only be asked for as "168h". `job.Span` reads days and weeks and
hands everything else to Go, and every written length on a rule
goes through it.

#### Proved on the running server

	schedule     every 2m also when battery.level < 100
	next_at      17:44:24   the clock advanced, though it watches too
	said         "the both-mouths probe spoke"
	quiet_until  18:42:24   an hour, set by the rule on itself

	at 17:46, two clock ticks later: last_at still 17:42, one
	announcement in all.


### Checking that a rule was written properly

Owner, 7 October 2026: *"this is an important rule, the AI must
know how to use it well without making mistakes. Is there any way
to check if the AI created or updated a rule properly?"*

Three layers, and the top one did not exist.

**Form** was already checked: an unreadable schedule, an hour of
ninety-nine, a keep of "a while", a tool that writes -- all
refused in front of whoever wrote it, and a change goes through
`job.New` so a rule cannot be edited into a state it could not
have been made in.

**Behaviour** was checkable with Try, but only by going and
pressing it.

**Meaning** was not checked at all.

#### A rule is tried the moment it is written

	Created: "a rule that can never fire"; every 20m; only if
	hour >= 23, then only if hour < 1, then says "..."

	Run just now, it would say nothing: the check hour < 1 did
	not pass.

Both halves come back together, so the model sees in the same
turn that its rule is dead and can say so or mend it. It says
nothing aloud and writes nothing down, so a rule made and
immediately tried is in exactly the state it would have been in
unmade.

The cycle is real -- the rule tools need the runner, the runner
needs the registry, the registry needs the rule tools -- and the
only thing in it that can wait is the runner, because no rule can
be written before the server is listening. So it is named early
and assigned late.

#### And measured

`TestTheModelWritesARuleProperly`, eighteen cases over the rule
tools and the two they are most likely to be confused with.

	17 right, 1 wrong

The one: *"stop telling me about that until Monday"* went to
`reminder_list`. With no prior turn "that" could be a reminder,
and rule_quiet is hours old where reminders are not. A fair
failure, and now a recorded boundary rather than a guess.

#### What the eval does not measure, and why

The first move only. A write refuses to run until its domain has
been listed, so reaching for `rule_list` is as right as reaching
for `rule_add`, and both are accepted: what is measured is
whether the model goes to the rules at all rather than to the
events or the reminders, which is where every failure went.

The rule that comes out is not measured. That needs the round
after the listing, and the gateway refuses one unless it is
handed back the identifier it gave the first call -- which `ask`
throws away. Two evenings of five hundreds came from guessing at
that shape; the fix is to thread the identifier through, and it
is the next thing here.

The first run of this suite is also worth recording for what it
got wrong: four cases failed and two of the four were the test
rather than the model, because the harness measured the first
call and read-before-write makes that the listing. An eval that
is wrong in that direction is worse than none.


### Two rules that use the profile for something

From a list of twenty suggested elsewhere, ten of which were
already running and five of which named tools that do not exist.
These two were new and are the only ones that make the routine
earn its keep.

	still in when you would normally have gone
	  every 15m, quiet for 2h after speaking
	    look up  calendar_events (1 day)
	    look up  event_recent (place.exited, 3)
	    only if  usually.home.leaves older than 90m
	    only if  hour < 18
	    write    say only if they appear to still be in and
	             something today would be missed by staying

	low battery with something on
	  every 10m, quiet for 3h after speaking
	    look up  event_recent (battery.level, 3)
	    look up  calendar_events (1 day)
	    only if  battery.level < 25
	    write    mention the battery only if something still to
	             come today could be affected

Both say nothing on an ordinary day, which is the point. A
departure from a routine is not worth a sentence and a battery
percentage is not worth a sentence; a departure with a
consequence and a battery with somewhere to be are.

#### The advice that was wrong for this server

	every one that can speak should have: check presence is here

It would make a rule silent and lost while he is out. The rule
runs, publishes to the outbox, and the greeting says it when he
is back -- which is what `keep` is for. Checking presence first
means the sentence is never composed at all. It is a fix for a
problem the outbox already solves, by throwing the message away.

And `battery.full arrives` was suggested in place of
`battery.level is 100`. Android fires battery.full once per
charge cycle and only the level reaching 100 re-fires, which is
recorded here from when that rule was built.

#### Tried rather than waited for

	still in ...  -> it would say nothing: the check hour < 18
	                 did not pass
	                 the diary is empty; place.exited today was
	                 10:56 home, 13:47 office, 21:24 office

	low battery -> it would say nothing: the check
	               battery.level < 25 did not pass
	               the battery is at 58

Which also says the first rule would have been right earlier: he
left at 10:56, near the ten the routine worked out, so there was
nothing to say.


### A pause is longest at the start of a sentence

The gap that ends a spoken turn grew from 0.7s to 3.5s the longer
somebody kept talking, but it did not begin growing until two
seconds of speech had gone by. Nine of 125 turns in a day ended
mid-sentence, and every one of them was inside that flat stretch:

	3.8s   Who is the
	3.4s   Jarvis look at the
	2.9s   Whenever I
	2.5s   When was the

Two to four words is well under two seconds, so the owner had
0.7s to think before the turn was taken away -- and then said the
whole thing again. A sentence is paused in longest while its
opening words are being chosen, so the least patient part of the
curve was sitting exactly where the pauses fall.

The floor is 1.2s, the flat stretch 1.0s, and the curve reaches
its ceiling in four rather than seven:

	spoke for      was      is
	  under 1s    0.70    1.20
	        2s    0.70    1.71
	        4s    1.40    2.41
	        7s    2.13    2.99
	       12s    2.83    3.35

The shortest questions pay about half a second more before they
are answered. That is the whole of the cost and the owner took it.

The gate is what made this visible. Before it, room noise read as
speech in 88% of frames and propped the turn open through any
pause, so a floor of 0.7s was never reached. Silencing the quiet
is what let turns end at all, and what exposed a floor that had
always been too tight.

### A turn that noise started has to end somewhere

The same day's logs: 153 windows went to transcription, 30.4
minutes of audio in all. Six of them were 19 of those minutes.

	11:36  258.0s  (nothing)
	12:44  168.7s  Hey,
	12:49  301.1s  (nothing)
	14:31  301.1s  Yeah.

Noise crosses the threshold that starts a command, keeps crossing
the one that sustains it, and the silence countdown is reset
forever. start_timeout_seconds does not help: that budget is for
speech that never begins, and here it began. Nothing ended these
but timeout_seconds, which was 300.

The longest thing the owner has actually said in one turn is 20s,
a dictated rule. The ceiling is 45.


### Something that can hear how a sentence ended

The gap was widened to 1.2s in the morning because a pause at the
start of a sentence read as the end of one. It worked, and it charged
every short question half a second for the privilege. That is the
wrong half of the trade to keep paying once something can tell the two
apart, so the gap is back to 0.7s and the telling-apart is done by a
model.

Smart Turn v3, 8MB of ONNX, reads the waveform rather than the words:
a finished sentence falls in pitch, one being thought through is held
flat. It runs as turn-detector.service on the host, because the thing
wants onnxruntime and a feature extractor and Home Assistant's
container is replaced on every update. Inside the container there are
four lines.

The seam is _speech_to_text_stream in pipeline.py, which already has
the audio and is already async, so the call is awaited rather than
blocking. At the one moment the segmenter says a command has ended --
once per turn, never per chunk -- the last eight seconds are offered
for a second opinion, and keep_going() in vad.py puts back what
reset() cleared if the answer is that the speaking is not over.

It fails open every way it can: no service, a slow one, a bad answer,
and the turn ends on the timer exactly as it did before. About 125ms
when it does answer, which is the cost of asking.

Its accuracy on this voice is **not measured**. Two attempts to
measure it failed, both on the samples rather than the model: phrases
performed into a recorder are not hesitation, and every clip held two
or three attempts with pauses between them. Home Assistant is now
keeping the audio of real turns, and the four cut-offs a day the log
already identifies will label themselves. Until that says otherwise,
this is being tried rather than relied on.


### A rule that follows them out of the room

Phone at four percent and nothing said. The rule had fired, and what
it wrote was the right thing:

	The battery is still dropping, sir -- nine percent now, and if
	you are heading out this morning you will want to charge it
	before you go.

Written 09:20:59, stale 09:26:24, unsaid on both clients. Nobody was
in the room. Which is the one place that sentence is no use: the phone
shows its own percentage, and what the rule adds is the diary, which
matters most to somebody already on their way out.

Every rule announcement fires as `immediate`, and immediate is
`{Asked, Drop, 5m}` -- the room, discarded if nobody is in it. Right
for almost everything it covers and wrong for this one.

So the rule says, not the kind. `anywhere` is the same shape of answer
as `keep` in 00044: a rule knows what its kind cannot. It picks
`reaching` instead of `immediate`, which is `{Everywhere, Hold, 1h}`.

Two things the owner decided along the way. Reminders no longer
travel -- asked directly whether to push them to the phone, no, they
would rather miss one than have every reminder leave the house through
Google. And `notify` is filled in at last, but it now reaches only the
kinds that ask for it, which today is one rule.

Three tests failed on the way through, all of them guarding the
audience table, all of them right to. That table is worth the tests it
has.


### A turn is the next thing in a conversation

Owner, 8 October 2026: *"whenever i say something, it should relate
to the last three to four conversation turns."*

From a morning where a reminder was set for the Jailer booking and the
follow-up came back as "What did you say to the the manual?" -- the
recogniser's best effort at something else entirely. It was answered
by asking what they meant. The turns before it settled what they
meant.

So `following` in the persona, wired into the block that is actually
sent. Two halves, and the second is the one that matters: what reaches
the model has been through a speech recogniser and is misheard more
often than not, so the reading that fits the last few turns beats the
reading that fits the letters. Asking what they meant is for when the
turns genuinely do not settle it, never when they only nearly do.

Prompt wording, which is the last resort here by rule. Reading a turn
against the ones before it cannot be enforced in the server: there is
no check that distinguishes a question that ignored the context from
one that did not need it.

### A reminder with a date and no hour gets the hour it was asked at

Asked directly what a date-only reminder should default to -- "on
October 10" with no time -- the owner said the current time, which is
what it already does.

Written down because it looks like a bug and is not. "Reminder set for
Saturday the 10th at 2:14 pm" from a turn at 2:14 pm reads as the
model reaching for the nearest number to hand, and the next thing
anybody will want to do is pick a sensible hour instead. The owner has
been asked and does not want one.

The correction in that exchange was about the day, not the hour.


### Something that puts the server back

The server was a nohup and a pid file. Every other thing on this
machine -- presence, the turn detector, the satellite's resume watch,
the vocabulary refresh -- is a systemd user service with a restart
policy. The one they all depend on was the exception.

health.go has said so since it was written: it answers *"the two
questions a supervisor and a load balancer ask"*, and is careful to
keep liveness apart from readiness so that a database outage does not
restart a working process. There was no supervisor to ask either.

What makes it matter is the twenty-odd background goroutines -- the
rule engine, reminders, the outbox sweeper, the collectors -- none of
them behind a recover(). A panic in any one takes the process with it,
and the first sign would have been the assistant quietly not
answering. Nothing in 285 recorded starts has panicked, which is the
right time to fix it rather than the wrong one.

deployments/personal-assistant-local.service, beside the production
one it is the counterpart of, and on-failure rather than always for
the same reason production chose it: stopping it on purpose should
stop it. The log keeps the property it was given on purpose: appended rather than truncated, with a line
marking where one run ends and the next begins, because restarting
used to destroy the record of whatever had just gone wrong.

make start, stop, restart and status drive the unit. Not a second way
of running it beside the first: two mechanisms for the same job is
what left the voice patches reporting as neither upstream nor ours
earlier the same day, and once was enough.

status reports restarts rather than uptime. "Up 8 seconds" reads as
healthy and can mean it has died four hundred times.

Tested by killing it with SIGKILL, which is what a panic looks like
from outside. Back in under five seconds, on a new pid, and status
said so. Tested the other way too: make stop stays stopped.

It is not enabled. Everything else here is started by hand -- the
voice stack by ./voice on, presence deliberately not enabled so that
it obeys the same rule -- and a server that came back at login when
nothing else did would be the odd one out. make enable is there for
whenever that is wanted.


### One loop failing should cost one loop

An HTTP handler has had a recover() since the beginning. The loops
that run unattended never did, and there are twenty-odd of them: the
rule engine, reminders, the outbox sweeper, the collectors that fetch
a calendar and a mailbox, the goroutine that streams an answer, the
one that says what is being done while a turn is slow.

A panic in any one took the whole server, and with it voice, every
rule and every reminder -- because one tasklist came back shaped
differently than the day before.

logging.Surviving turns a panic into a logged fault with its stack and
the name of what died. Wrapped in two places and for two reasons: the
body of each ticking loop, so a bad pass costs that pass and the next
tick still happens, and the goroutine itself where there is no loop.

The supervisor is the other half and not a substitute. Coming back
costs every loop its place and whatever turn was in flight; a recover
costs one loop one turn.

Two were left open deliberately. wg.Wait() then close(done) at
shutdown, and the listener goroutine: a panic in either is a bug in
this code rather than bad data from outside, and surviving it would
hide it.

A nil logger falls back to the default handler rather than
re-panicking. Some of the places this is needed -- mail, tasks,
calendar -- take a service client and nothing else, and threading a
logger through them to reach a path that should never run would be the
tail wagging the dog.

### A log that stops growing

57MB and 285 recorded starts, appended since the beginning and never
trimmed. The appending is deliberate and stays: restarting used to
destroy the record of whatever had just gone wrong, which is exactly
when a restart happens.

A user timer rather than /etc/logrotate.d, because everything else on
this machine is user-space and installing it needed root. 50MB, five
kept, compressed.

copytruncate rather than rename-and-recreate. systemd opened the file
with append: and holds the descriptor for the life of the process, so
moving it would leave the server writing to something with no name --
the log appearing to stop while the disk kept filling.

Checked rather than assumed: rotated at 57MB, and a request after it
landed at the start of the new file rather than at offset 57MB. 340
bytes apparent, 4K on disk, so not a sparse file.


### The tree says what the import graph is

Forty-one packages sat flat under internal/, which said nothing about
which of them everything else stands on. The graph was measured first:
chat imported by twenty-nine packages and importing none, prompt by
twenty, event by fourteen, logging by twelve, and at the other end api
importing thirty-three.

So three layers, and they are the graph rather than a filing habit.
base is the floor and may import only base. domain is what the
assistant knows about -- reminders, rules, the diary, the person's
mail -- and stands on base. app is the wiring, and nothing reaches
back into it.

A strict depth-first split does not fit: the real graph is more than
three deep, and mechanically classifying by depth left forty-seven
packages unplaced. The layers are by role, with one rule that is
mechanical and therefore enforceable.

layers_test.go is the enforcement, and it found a violation the moment
it existed: profile imported app/tool to compare against one string
constant. profile imports from base, domain and app and is called by
the api, the memories tool and main -- it is a composer, not a thing
the assistant knows, so it moved to app rather than having a constant
invented to let it stay where it was.

Checked by planting one. base/llm importing domain/remind fails with
"base may only import [base]". The first attempt to plant it did
nothing, because llm.go has a single-line import and the edit looked
for a parenthesised block -- a test that cannot be made to fail has
not been tested.

The second test is for what the first cannot see: a package put
straight into internal/ belongs to no layer and would be checked by
nothing.


### Five jobs in one package, and one interface doing all of them

chat held eight files and one shared unexported helper between them,
which is a package in name only: Chat, Client, User, Conversation and
Model are five independent types that happened to share a directory
because they all touch the same database. Measured against the others
-- runner shares thirty-nine helpers across seven files and is one
mechanism -- chat was the one that would come apart almost for free.

The interface was the real finding. Repository had twenty-six methods
covering all five, so a handler that renamed a conversation was handed
the means to create a user. It is now Chats, Conversations and
Settings where the chat is, Users where the user is, Clients where the
client is, and store.Repository joining them for the two
implementations that really do store all of it.

Why store is its own package: client needs Channel and Model from
chat, so chat cannot import client, so the joined interface cannot
live in either. stored is the same shape of answer for the one helper
all four shared -- the millisecond the database keeps time at belongs
to the columns, not to any of the types written into them.

Splitting files into directories was the thing asked for and is not
what Go does: a directory is a package, so aside.go in its own
directory means thirty-nine unexported helpers in runner become public
API. The package is the unit. What was wrong here was not that chat
had eight files but that it had five responsibilities.

The work was mostly renaming locals. A variable called client shadows
the package called client, and a blanket rewrite turned
chat.ErrRevoked into client.ErrRevoked inside functions where client
was a row from the database. Renaming the local first and moving the
types second is the order that works; twice it was done the other way
and twice the file had to be restored from HEAD.


### The module is called what it is, not where it is

Owner: *"why does github have to do anything here."*

It does not. A Go module path has to be a URL only if somebody is
going to fetch it, and nothing fetches this one: it is a server
binary, nothing imports it, and production is sent a built binary
rather than a module to resolve. The repository does live at that
address, which is why the Documentation lines in the unit files still
say so -- that is where it is, and it was never what it is called.

What it buys is thirty-eight characters off every import line, which
matters more since the layers made the paths longer:

	github.com/DhanushRamesh/personal-assistant/internal/base/store
	personal-assistant/internal/base/store

What it costs is go install from the URL, which was never how this is
built or deployed.


### The floor has its own floor

Owner, on base holding twenty-seven packages: *"they should be
categorised hierarchically, it might lead to circular dependency."*

It cannot. Go rejects an import cycle at compile time, so client
importing chat and chat importing client is a build failure rather
than a thing that creeps in. What was really there was an ordering
nobody could see: twenty-one of the twenty-seven imported nothing,
four imported only those, and two stood on the four.

Grouped by what they are for rather than by how deep they sit, and
checked for a cycle among the groups before anything moved -- there
was none:

	platform   logging config storage auth failure
	world      event events routine heard speech place leaving
	           notice announce alarm errand
	thinking   environment llm prompt embed search
	records    chat user client store stored conversation

platform is the floor of the floor and imports nothing. world and
thinking stand on it. records stands on platform and thinking,
because a conversation is made of prompts.

TestBaseIsOrderedWithin enforces it, and is a different claim from
the one above it: that one says nothing below may reach up, this one
says the four groups keep their order. Checked by planting
platform/logging importing records/chat, which fails with "platform
may only import [platform]".


### Four rules in config that cannot fire

Auditing platform for whether the tests cover what each module is
for. Coverage said 75 to 92 per cent, which hid the thing worth
finding: of the twenty-one validation rules in config, ten had never
been run by a test. Config decides whether the server starts and with
what, and a rule that rejects a bad value but has never been shown to
is a rule taken on trust.

Writing the ten found that four of them are unreachable. value()
treats an empty setting as unset on purpose -- so that
ASSISTANT_SERVER_ADDR= in a shell script falls through rather than
binding nowhere -- and addr, host, user and name all have non-empty
fallbacks. "Must not be empty" is guaranteed before validation sees
it, so the four checks that say so can never run.

The guarantee is the real behaviour and nothing tested it either, so
that is what is tested now, from a file and from the environment. The
four checks are left where they are: they cost nothing and they would
matter the day a fallback becomes empty.

The one rule worth its own test rather than a row in a table is the
one about somebody else getting in: production refuses a public bind
over plain HTTP, because bearer tokens go over it in clear and a
misconfiguration that publishes them is silent.

Two other things came out of it. logging.Repeatedly was dead --
written that morning alongside Surviving, never used, never called --
and is gone. And storage's Fatalf says in its doc that it does not
exit, unlike the logger goose installs by default; nothing checked
that, and the way anybody would have found out is the server
disappearing in the middle of a migration.

	logging  85.5 -> 87.7     config   79.0 -> 82.3
	storage  81.2 -> 83.2     failure  75.0 -> 100


### The half of chat that was never tested

Auditing records. user, client, stored and store had no tests at all
-- they were split out of chat that morning and the tests stayed
behind, having never covered them in the first place.

chat read 63.9 per cent, and which half was missing was the point: the
state machine was tested, because a state machine is the obvious thing
to test, and the conversation resolution was not. That is the half
where a bug is quiet. Nothing crashes; somebody just ends up talking
into the wrong conversation, or a new one each turn, and the history
they were relying on is simply not there.

Writing those tests needed a stub, and the stub would not compile:
EnsureConversation and ActiveConversation took the whole
Conversations interface and use three of its eight methods. So they
take Somewhere now -- list what a person has, make one when there is
none, remember which -- and renaming or deleting a conversation is not
their business to be handed.

Two stale errors turned up in passing, still saying "chat:" after the
move: ErrInvalidUsername and the one about a password hash.

	chat    63.9 -> 98.1      user     none -> 100
	client  none -> 100       stored   none -> 100

conversation was already at 87.8 and was left alone. store is an
interface with no statements in it, and both implementations already
assert they satisfy it at compile time, which is the right check for
an interface.

The user tests were checked by widening the username pattern to accept
anything: ten assertions failed. A test that cannot be made to fail
has not been tested.


### What decides whether anything is said at all

Auditing world and thinking. Most of both was already covered --
speech at 100, events at 97.7, heard at 95.7, alarm at 93.5, llm at
90.9, embed at 90.2 -- so the audit was about which gaps mattered
rather than how many there were.

prompt was 53.8 per cent and is imported by twenty packages, and the
untested half was Nothing(): what decides whether the assistant
speaks. Read too strictly, "NOTHING." is spoken aloud as an answer.
Read too loosely, a real sentence beginning with the word is
swallowed and the person is never told. Both directions are now
written down, including a model that says the word and then explains
itself, which is still declining.

event was 60.9 per cent across two and a half thousand lines, and
reaching.go -- who tried to get hold of them and whether it worked --
had no tests at all. It is deliberately two comparisons rather than a
judgement about language, because a model asked to make it raised a
call that had been returned eight minutes later, on four arrivals out
of four. Now covered, including seconds arriving as a string, which is
how Tasker sends them, and something settled coming back.

Working() decides what is hidden from the model as churn, and a day of
music events is longer than a day of everything else put together. It
matches a family by prefix, which means a single letter matches too.
Nothing sends a kind like that and the behaviour is written down
rather than changed, with the test saying so.

routine's Said() was untested and reaches a person's ears -- "you
usually leave around 10am". Midnight and midday are the two that go
wrong, being 0 and 12.

	prompt   53.8 -> 100      routine  45.5 -> 100
	event    60.9 -> 76.2

notice is an interface with no statements. store was the same and both
are right as they are.


### A mail in Tamil, cut in half a letter

Auditing domain. The low numbers are mostly honest: calendar at 13.5
per cent, mail and tasks at none, are Google clients, and what is
untested in them is the part that calls Google. Covering that needs a
fake service and would measure the fake.

So the audit went after what is logic rather than what is a wrapper,
and found a bug. shorten() cuts a long mail at Longest bytes, which
is right as a budget -- a prompt is counted in bytes -- and wrong as
a cut: three bytes a letter in Tamil, four in an emoji, and the
slice lands inside a character. What comes out is not text. It then
goes into a prompt and from there to whatever is listening. It cuts
on a rune now.

tasks.Pick is the other piece of real logic in either: which list
somebody meant, matched by likeness because what reaches it has been
through a speech recogniser. Covered now, including the case where
two are alike and it must ask rather than choose -- a task added to
the wrong list is not findable by the person who added it.

outbox.Spent and Owing.Stale are the two halves of whether an
announcement is still worth saying, and both were untested. They are
what decided every battery warning that was never heard: written at
09:20, stale at 09:25, said to nobody.

persona.Addressed puts the manner on a sentence the server wrote
rather than the model. Without it the one reply the model had no hand
in is the one that sounds like a different assistant.

	outbox  56.2 -> 59.4      persona  26.3 -> 45.6
	tasks   none -> 6.2       mail     none -> 12.2

The percentages stay low because most of those packages are calls to
somebody else's API, and that is the right shape for them. What
matters is that the logic inside them is covered now.


### The guards on the two things that cannot be taken back

Auditing app. Its own infrastructure is the best covered thing here --
api at 100, health at 100, middleware at 98.3, httpx at 94.7 -- and
the gaps are in handlers and tools, where the numbers are low because
most of each is wiring.

So the audit went after guards rather than percentages, and both of
the ones it found were on something irreversible.

conversation_delete was at 3.8 per cent and has four guards: typed
only, because on voice a misheard sentence is the whole
authorisation; an exact title, which forces a listing first and is the
cheapest proof the model looked before it acted; somebody else's
conversation reported as missing rather than refused, so guessing
identifiers teaches nothing; and read-before-write. None was proven.
The fourth was found by the tests failing on it, which is the best way
to learn a guard is real.

SetModel refuses a model the endpoint does not route, and that guard
exists because choosing one broke voice: the client fails every prompt
afterwards and the failure is at the far end where nothing here can
see it. It was untested. It is now, including the dotted spelling the
gateway itself corrects -- claude-sonnet-5.5 for claude-sonnet-5-5.

The positive half could not be written until the test harness carried
a model catalogue, because with none every model is refused. It builds
one the way the server does now: the routed list joined to what is
known about each, since a vendor the service speaks to is not the set
of models it routes, and a routed model is no use without the context
window that sizes what is sent to it.

	tool/conversations  3.8 -> 22.3      api/clients  43.2 -> 56.8


### A sleeping phone and a dead cable look alike

Owner, told at ten at night that the battery had sat at five per cent
for a quarter of an hour and the cable might be at fault: *"what if
the phone is stale, I did not intend to put the charge."*

He was right. "battery.level <= 15m ago" finds the newest reading at
least that old and compares the latest against it. If nothing has
arrived inside the window, the newest reading is itself older than
the one it is being compared against -- so it is compared with itself,
and a value equals itself. Every phone asleep in a pocket read as a
cable that had stopped charging.

The same shape as the presence sensor that froze and went on reading
as somebody being here. What is knowable is not whether the level
moved but whether anything was reported, and nothing was looking.

So the comparison is refused when nothing has arrived inside the
window, which is the answer the gate already gives about a kind
nobody has reported. A level that is genuinely stuck still says so,
because a phone that is reporting sends the same number again.

It reaches every rule written with "ago", not only this one.


### The phone knows, so stop guessing

Owner, after the stale-reading fix: *"is there any tasker event to
check if a usb cable is connected to the socket, not necessarily
meant to be plugged."*

There is not. Tasker's Power state knows only that a source is
connected; whether current is flowing lives in a kernel file named
differently on every handset, and the feature request for a charging
variable is still open. Every phone event in this server comes from
Tasker, 3792 of them, and every `power.plugged` ever sent carries
`{"level":"95","value":"unknown"}` -- the value slot has never once
been filled, because Tasker had nothing to put in it.

The Home Assistant companion app has had the answer the whole time
and nobody looked: `sensor.pixel_7_battery_state` reads charging,
discharging, full or not_charging, with `sensor.pixel_7_charger_type`
beside it. So this is the one phone fact that comes from Home
Assistant rather than Tasker. `rest_command.assistant_charging` posts
`power.<state>` with the socket as the value and the level alongside,
fired by an automation that refuses unknown, unavailable, and any
trigger that is not a change between two known states -- a server
told charging had stopped because the phone lost signal would be told
a lie in the direction that wakes somebody up.

A new sender is half the work. `situation.NotCharging` matched on
`power.` and treated anything that was not `.plugged` as unplugged,
so the truthful events would have silenced the very check they were
sent to improve. It now takes the newest word whichever device sent
it: told `not_charging`, it says so after two minutes and quotes the
level it was given; told `charging` or `plugged`, it runs the
twenty-minute arithmetic underneath.

The first cut of this had `charging` silence the check, on the
grounds that current going in means the cable is fine. Owner: *"if
state is charging and level has not moved up it should say something
wrong with the charger."* He is right, and it was the wrong way
round. Charging with a level that will not rise is worse evidence
than a bare socket, not better -- the phone is saying current is
arriving and nothing is coming of it, which is a weak charger, a
frayed cable, or the phone spending it as fast as it gets it. Neither
device can see how many amps are going in, so neither earns an
exemption. The only thing the word changes is which fault gets named:
told it is charging, the cable is seated, and sending somebody to
reseat it would send them to the one thing that is not wrong.

Settling is two minutes, not twenty. Android reports `not_charging`
for a few seconds while a cable is seated and again while a hot phone
pauses to cool; waiting the twenty would throw away the whole point
of being told outright.


### The engine is the backbone, so it does not get to have limits

Owner, on being told that counting was worth adding but the join was
a language feature with one customer: *"remember the rule engine is
the backbone and I should have wide capabilities (like writing a code
using predefined templates) cuz in future I'll make the AI to create
rules based on the patterns, so it should not have limitations."*

The argument that killed the join was "who else would use it" --
only `Trying` does, so a grammar word for it is a feature with one
customer. That argument is wrong here, and it is worth writing down
why, because it is the kind of reasoning that looks careful.

It counts the customers who exist. The customer that matters does
not exist yet: an AI that watches what this person actually does and
writes rules for the patterns it finds. That writer cannot ask for a
word the grammar does not have. Every capability left out is not one
rule unwritten, it is a whole shape of noticing that can never be
expressed, by anything, ever -- and nobody will see it missing,
because an unwritable rule leaves no trace.

So the engine is sized for what will write rules, not for what has
written them. Breadth first, and "only one thing needs it" stops
being a reason to leave something out.

What it is not is a licence to invent a programming language in a
database column. The owner said how: *predefined templates*. A
template is a named pattern, written and tested in Go, with blanks a
rule fills in -- counted, grouped, cancelled-by, gone-quiet. The
determinism stays in Go where it can be tested; what moves to the
engine is choosing a pattern and filling it. That is the same split
the engine already has with tools, and it is why the two checks in
`situation` are not a separate engine but the first two templates,
written before there was anywhere to put them.


### The charger moves to where it always belonged

Owner, on the charger check living in `situation`: *"this should not
be in situation, it should be in the rule engine."*

He was right, and the duplication was worse than he knew: it was in
both already. `situation.NotCharging` ran every two minutes over a
three-hour window; the rule `plugged in and not charging` ran fifteen
minutes after a `power.plugged`. Two clocks, two windows, one
question, and an afternoon spent improving one of them without
noticing the other.

It is two rules now, and both are better than the Go was:

	when power.not_charging arrives then 2m
	  check ha.sensor.pixel_7_battery_state is not_charging

	when power.charging arrives then 20m
	  check ha.sensor.pixel_7_battery_state is charging
	  check ha.sensor.pixel_7_battery_level < 95
	  check battery.level <= 20m ago

The two minutes are the settling that was a constant in Go, and the
condition after it is a live reading rather than a timestamp
comparison -- a cable being seated flickers not_charging for a few
seconds, and the rule now waits and asks the phone again instead of
inferring from how old the event was.

What is left in `situation` is the two checks that count and group.
The charger was never that. It was one number against another, which
is the one thing the engine does for nothing.

Both rules speak in the room only. The cable is an arm's length away
or the sentence is no use.


### A lookup that failed is not the same as finding nothing

From a second reading of the engine's specification, by something
with no stake in it: *"missing information should never silently
become permission to speak."*

A failed check already stopped a sequence, and that was learned the
hard way -- a rule with three conditions, the third unreadable, said
its words aloud in the room, because the step meant to withhold
failed and failing was taken for permission. The same mistake was
still sitting one move further on, and the comment in the code names
it without acting on it: *"a lookup that fails leaves the steps after
it less to go on."*

It does worse than that. A check that fails leaves the steps after it
*unguarded*, which is at least visible. A lookup that fails leaves
them *uninformed*, which looks like nothing at all: the write step
still has its instruction, still has a model, and still produces a
sentence -- composed from a picture with a hole in it that nothing in
the sentence will mention. A rule that cannot read the battery does
not say "I could not read the battery". It says something about the
battery.

So a failed read now stops the sequence, and a failed write does not.
The older rule stands and is still right: a reminder whose phone
alarm could not be set must still say its words aloud, because
nothing after it was relying on what it returned. The two are the
same step kind and want opposite things, and what separates them is
`Tool.Writes` -- which has been on every tool all along for a
different reason.

A tool name the registry does not know counts as a read. Nothing is
known about it, so nothing is risked by the quieter answer.


### Counting, searching and grouping are one word

Owner, asked which to build next: *"now do the counted one. I would
expect a mini if-else conditionals, searching, looping like that kind
of thing but easy to build."*

The loop is the part nobody should have to write. Searching through
events, counting them and grouping them are three things to do by
hand and one thing to ask for:

	counted call.missed within 2h >= 2
	counted call.missed is amma within 2h >= 2
	counted call.missed by value within 2h >= 2

`is` narrows, `by value` counts each distinct value and answers with
the largest, `within` is how far back. No `for`, no `if`, no
variables -- and between them they cover nearly everything anybody
would have written a loop for. *Else* is not here on purpose: a rule
that does one thing in one case and another in the other is two
rules with opposite conditions, which can be read off the screen
where an else buried in a row cannot.

The operator is taken from the right-hand end, because the source is
several words and one of them may be `is`. "counted call.missed is
amma within 2h >= 2" has two, and only the last is the comparison.

A count is a number where every other source gives a value, so every
operator already there works on it and counting needed none of its
own. It is asked rather than delivered, like `ha.`, so it cannot be
a trigger -- which also means nobody has to decide what *crossing* a
count would mean.

Nought is known. Everywhere else in this engine nothing known is a
condition that does not hold, and that is right, because nothing was
proved. Here the answer was looked for and found to be none, so
`counted door.opened within 12h is 0` is a rule about a thing not
happening -- the only way this grammar can say that at all.

### What the in-memory store is for

The count is written twice: once in the in-memory store the obvious
way, once as a query. The first is tested and settles what a count
means; the second says the same thing to the database.

Which is how this was caught. `JSON_UNQUOTE` does not inherit the
table's collation: every text column here is utf8mb4_0900_ai_ci and
ignores case, and a value pulled out of JSON comes back utf8mb4_bin
and does not. Live, `is unknown` found six missed calls and `is
UNKNOWN` found none, while the tested implementation found one
either way. A rule would have been right in its test and wrong in
the house, and nothing in the test suite could have seen it --
there is no database behind those tests.

Both sides fold the case now. The lesson is the one the second
implementation exists for: run the query against the real rows
before believing the unit test.


### A clock that can say when not to run

Owner, 8 October 2026: *"I want the time to be in cron style,
because it can cover every kind of pattern."*

`every 30m` and `at 08:00` are what somebody says out loud and are
read back in those words, so they stay. What they could never say is
*when not to*. "at 08:00" is every day of the year; a weekdays-only
rule had to wake every morning and decline on the two it was never
meant for. A condition can stop a rule running but it cannot stop it
waking, and a rule whose next time is a moment it will refuse is a
rule whose next time is a lie.

	cron 0 9 * * 1-5      nine, Monday to Friday
	cron */15 9-17 * * *  every quarter of an hour, nine till five
	cron 0 7 1 * *        the first of the month

Written here rather than taken from a library, for one reason: the
day rule. When both day fields are given, cron runs on a date
matching *either* -- "cron 0 9 1 * mon" is the first of the month
and every Monday, not the Mondays that fall on the first. It
surprises everybody and it is kept, because a pattern copied from
anywhere else has to mean here what it meant there. Inherited from a
dependency it would be true and undocumented; written here it is
true, documented, and the test that proves it fails if anybody
"corrects" it.

The search skips rather than counts: a wrong month jumps to the
first of the next, a wrong day to midnight tomorrow, a wrong hour to
the top of the next. A pattern naming one minute a year is found in
a few hundred steps rather than half a million. Five years and it
gives up, because "cron 0 0 30 2 *" parses perfectly and can never
happen, and the answer to that is a refusal and not a loop.


### Never invent a notation

Owner, on `{"source": "counted call.missed by value within 2h"}` in
a structured rule: *"how will it be evaluated in code? It is more
string and an AI can make a mistake."*

He is right and it was a shortcut. The string is parsed twice --
once by `ParseGate` into a source, and again by `ReadCount` deep in
the resolver at evaluation time -- and it was done that way to avoid
changing the `Readings` interface. In a structured rule it is worse
than untidy: it is a grammar nothing has ever seen, sitting inside a
schema whose entire purpose was that there would be no grammar to
get wrong.

**Never invent a notation. Use a universal one, or use structure.**

The test is not whether a field is a string. It is whether the thing
writing it already knows the notation. `cron 0 9 * * 1-5` stays a
string: every model writes cron reliably, because cron existed
before any of this. `counted call.missed by value within 2h` was
invented one afternoon and nothing has ever seen it. One is a
format; the other is a trap.

Two fields fail the test today and both become structure:

	{"count": {"kind":"call.missed","by":"value","within":"2h"}, "op":">=", "value":"2"}
	{"source": "battery.level", "op": "<=", "ago": "15m"}

The second is the worse of the two, because `"15m ago"` looks like
an ordinary value. Something writing `"value": "15m"` instead gets a
rule that parses, runs, and compares the battery against the number
fifteen.

What this costs of the grammar already written is nothing, only a
change of job. `ReadCount` stops being how a count is stored and
becomes how a dictated one is read -- surface syntax, with the
structure underneath as the truth and the sentence rendered back
from it for reading aloud. Which is the same inversion the whole
rule object is making.


### What a rule did, before changing what a rule is

Owner, having settled the structured rule and asked for the sixteen
live ones to be converted: *"before that -- these rules will get
executed at some point right, we have to collect the execution
logs."*

The right instinct and the right order. Converting sixteen rules at
once with nothing to compare against is changing sixteen things and
having no way to say whether any of them still does what it did.

Something was already written down: one row per run in `remarks`,
with the outcome and the first thing that stopped it. Three things
were missing and all three are about surviving a rewrite.

The name was the only handle. `remarks.label` is `j.Name`, and the
conversion renames rules -- `anything amiss` becomes two -- so the
moment a name changes its history is orphaned. The id is written
down now, and with it the name and the schedule *as they were at the
time*: a row saying "anything amiss, every 2m, declined" stays true
after that rule has become two rules with other names, and the
job_id joins the two halves of its life.

Only the first stopping step was kept, so what had already been done
when a rule stopped was lost. Every step is written now -- what it
was, what came of it, how long it took, and what it decided.

And nothing was timed, so "has this got slower" and "did the
conversion make it cheaper" were both unanswerable.

Its own table and not more columns on `remarks`. That table is
where things the assistant *considered saying* are written, and a
rule that stopped at its first condition never considered saying
anything; squeezing it in made every quiet tick look like a remark
declined. One answers "what did it nearly say", the other "what did
it do".

Sixteen hundred rows a day, kept a month: about fifty thousand
standing, against a third of a million a year unswept. A month is
longer than anybody compares a rule with itself across, and the
sweep is its own loop so that failing to drop old rows cannot stop
the loop that drops old position readings.


### The cheapest question first

The run journal was four minutes old when it paid for itself:

	tool   calendar_events                      ran      1241ms
	tool   event_recent                         ran        12ms
	check  usually.home.leaves older than 90m   stopped     1ms

A Google API call and one and a quarter seconds, to reach an answer
that was available in a millisecond. Every fifteen minutes, ninety-six
times a day. `low battery with something on` had the same shape on a
ten-minute clock, so the two together were spending about two hundred
and forty calendar fetches a day on questions that were nearly always
answered no.

The same engine, the same evening, for contrast:

	the Google link has died        check first, then act      4ms
	still in when you would have    act first, then check   1255ms

Three hundred times apart, and nothing between them but the order the
steps were written in.

A check reads a *source* and never what the step before it returned --
so every check gives the same answer wherever it sits, and a check
written after a lookup is a lookup that did not need to happen. The
steps are reordered now, cheapest question first: the clock and the day
cost no read at all, a source costs one, and anything that leaves the
machine goes last.

It is a fix with a shelf life. In the structured rule the conditions
live in `when` and the steps are only doing, so this cannot be written
wrongly at all. Worth doing today because the saving is real now and
the conversion is not.

All nine rules that have a condition were put through the same
ordering, not the two that were costing the most. Four of the battery
ladder fetched six readings before asking whether the level had
crossed anything, which is twelve milliseconds rather than twelve
hundred -- but the reason to fix them is not the twelve milliseconds.
A rule written after these, copied from whichever one was nearest,
copies the order it finds. Leaving four examples of the wrong shape
in place is how the wrong shape survives the fix.

	low battery with something on   1255ms -> 4ms

Not every rule moved: five were already cheapest-first, which is how
the ordering was found to be a rule rather than a preference.

One thing learned on the way: `jobs.updated_at` is stamped by every
*run*, not by an edit, so it cannot say when a rule was last changed
and is no use as a before-and-after marker. The run journal's own
timestamps are the answer, which is what it is for.


### A rule in two halves

Owner: *"I want the current rule list to be in this when/perform
format, and the rule create tool should be updated."*

The conversion, written as one function that can be run over every
live rule and its answer read before a single row is touched. All
sixteen go through it.

What the shape settles, and why each is not a preference:

The schedule becomes hooks, and several hooks mean *any one of them
fires it* -- which is how two triggers on one rule get written at
all. The old shape parsed one gate and had no room for a second, so
"when the door opens or the window opens" was two rules.

Every check becomes a condition, and `perform` has no check in it.
Not tidiness: a check could only ever read a *source*, never what
the step before it returned, so every check gave the same answer
wherever it sat. One measured 1255ms -> 4ms from being moved ahead
of the fetch it was written after. In this shape it cannot be
written in the wrong place, because there is no wrong place to
write it.

The two invented notations become structure. A count is keys; "15m
ago" is a key of its own. That one was the worse of the two,
because in a value field it looked like an ordinary value and
writing "15m" by mistake gave a rule that parsed, ran, and compared
the battery against the number fifteen.

`is` and `is not` are stored as `==` and `!=`. The words are what
somebody says and what a rule is read back as; the symbols are what
is kept, so a dictated rule and a generated one converge on the
same row.

One bug the conversion found in itself, worth keeping: twenty
minutes came out as `"20"`. Go writes it `20m0s`, and trimming the
tail off that takes the unit with it -- the string ends in `0m` and
so does the number. Each unit is taken on its own now. It would
have made every rule comparing against its own past compare against
a bare number instead, and it was found by reading the output
rather than by a test, which is why the output was read.


### The engine reads one shape while the table holds two

The conversion, wired. Every rule is now a `when` and a `perform`
by the time anything looks at it, whichever shape its row is in.

Three things made it safe to change how sixteen live rules are
evaluated in one go.

The leaves lower back to a `Gate`. Every operator, every source,
the counting and the Home Assistant reading are the ones already
written and tested, and none of them had to learn that a rule now
arrives as a tree. What is new is the walking, not what is at the
bottom of it.

A row with no structured columns is reshaped as it is read. So the
engine knows one shape while the table holds two, the columns could
be backfilled afterwards rather than before, and a rule written
before any of this goes on working whether or not anybody converts
it. `job.New` does the same, so a rule made the old way is in the
new shape from the moment it exists.

And the run journal, written first for exactly this. The conversion
can be compared against what each rule did before it, by id, across
a rewrite that changed both the name and the shape.

What changed in behaviour, which is the part worth stating plainly:

Conditions are answered before anything is done, because every one
of them was a precondition anyway. A rule that does not hold now
fetches nothing at all -- where the old shape ran the lookups
written before the check and then found it had nothing to say.

All the conditions are fetched together. A rule asking three things
about the battery reads it once; each check step used to be
answered on its own and knew nothing of the ones beside it.

The next moment is the soonest clock rather than the first written,
which only matters because a rule may now have more than one.

And `Watching` returns every event hook rather than one, so two
triggers on a single rule exist. The old shape held a schedule, and
a schedule is one sentence with one trigger in it.


### The first check spoke for all of them

`situation_find` ran every check it had and answered with the first
that found something. One rule called it, so for as long as the
first had anything to say, the second could not be reached.

Live, that was not hypothetical. `anything amiss` had said
*"Someone the phone could not name has tried to reach you more than
once"* and was still saying it, which meant the check that notices a
device has stopped reporting had no way to be heard at all. The
phone could have been dead for an hour.

First-wins is right for a person asking whether anything is wrong:
these are interruptions and two at once is one too many. It is wrong
for a rule, because a rule is about one thing. So the checks have
names now, and a rule says which it wants. Nothing named is still
everything, in order, first wins -- which is what the model gets
when somebody asks.

`anything amiss` is two rules: *somebody has been trying* every two
minutes, and *a device has gone quiet* every five. Five because the
silence it looks for is forty-five minutes long and nothing is
gained by asking more often; two for the other because a call that
nobody answered is worth knowing about quickly.

A name nobody has is refused rather than quietly finding nothing. A
rule asking for a shape that does not exist would otherwise never
fire and never say why, which is the failure this codebase has least
patience with.

Both rules were written and both sat still, because they were
inserted by hand with `NOW(3)` for their next moment. Every datetime
column here is UTC -- the stores all write `.UTC()` -- and `NOW(3)`
is the server's own clock, which is five and a half hours ahead. The
rules were perfectly correct and due that evening. Anything written
by hand into this database wants `UTC_TIMESTAMP(3)`, and a rule that
does not run at all is worth checking `next_at` on before anything
cleverer.


### The rule tool speaks the shape it stores

`rule_add` and `rule_update` take `when` and `perform` now, as
objects, and the flat arguments they had -- schedule, say, ask,
when-as-a-sentence, look_up, tools -- are gone. A hundred and eight
lines of the file went with them: `assemble`, `parts`, `lookUpItem`
and four descriptions existed only to turn those parts into a
sequence, and there are no parts any more.

Two things the old arguments could not express at all, and both
were already possible in the engine: two hooks on one rule, and a
group inside a group. A schedule was one sentence with one trigger
in it, and a condition was one string with one level of `or`.

Changing one is whole-half: name `when` and all of it is replaced,
name `perform` and all of that is. There is no merging one tree
with another that anybody could predict, and the rule that used to
be needed -- *naming say takes ask away* -- stops existing, because
a sequence given whole cannot have both.

### An operator that could not be written could now be typed

The structured path let `{"op": "nonsense"}` through, and it was
written to the database and read back as a rule that could never
hold.

Written as a sentence this was impossible: the parser recognised
the operators and refused everything that was not one. Handed over
as a string in a JSON object, nothing looked. That is the whole
class of thing structure buys -- the model cannot typo a grammar --
and the whole class it costs, since a key may hold anything at all.

Every leaf is checked against the nine operators as it lowers to a
gate, which is the only door an unknown one can come through.
Caught by writing the refusals test before believing the happy
path, which is the only reason it was caught at all.

### make evals had been pointing at nothing

Before changing a tool description, the thing to do is measure it
both ways. `make evals` names six packages and `make evals-greeting`
one, and all seven paths were the ones they had before the move into
base, domain and app. They have not run since.

Mended. But it is worth saying what it cost: the rule tool's
description changed shape entirely with no before-measurement to
compare against, because there was nothing to run. And what the
rules eval measures is which tool the model reaches for, not the
arguments it writes -- its own comment says so. So the arguments a
model will now produce for a structured rule are, today, unmeasured.


### A rule is written by something that does nothing else

Owner, 9 October 2026: *"for writing a rule we should have a
separate agent with all the specification clearly written so that no
mistake is made."*

Two things were wrong with the tool writing its own arguments, and
the second is the one that matters.

The grammar had to live in the tool's description, and a description
is paid for on every turn by every conversation. Sources, operators,
values, hooks and the sequence came to some sixty lines sitting in
front of a model being asked what the weather is.

And it could not be measured. The eval that exists asks which tool
the model reaches for -- the thing that used to go wrong -- and says
in its own comment that it does not measure the rule that comes out.
Arguments written mid-turn, among fifty-one other tools, by a model
holding a conversation, are not a thing that can be tested on its
own. Written by something whose whole prompt is the specification,
they are.

`rule_add` takes `asked` now: what they want, in their words, plus
whatever the assistant knows that their words leave out. The writer
is given the grammar, the kinds this person's devices *actually*
report, the tools a rule may use, and their own existing rules as
examples -- because a rule of theirs is about their devices, their
places and their way of saying things.

The part that earns the owner's "no mistake is made" is the loop.
Every refusal in this package is already a sentence somebody could
read, so it is handed straight back: *nothing would ever wake this
rule*, *there is nothing a condition does called "nonsense"*,
*calendar_cancel changes things, and a rule may only look things
up*. A mistake is described in the same words whether a person is
being told or a model is being corrected. Three goes: the answer,
the answer with the mistake pointed out, and one for a bad minute.

One thing the tests found. A writer that answers *"nothing a device
reports says whether the bins are out"* has read the request and
done the thinking; asking it twice more spends two model calls and
the seconds somebody is standing there on an answer that will not
change. A malformed object is a mistake to mend; a refusal is the
end of it, and the two are told apart.

The fast model, and no persona. This one is not speaking to anybody
-- it answers with JSON -- and a manner would only be something else
for it to spend its reply on.

Changing one goes the same way, and is the harder case. The writer
is shown the rule whole and told to answer with the whole of it
back, because the commonest way to ruin a rule is to mend one thing
and drop another. The old tool asked for the half that changed and
kept the other half itself; this asks for all of it, so what is lost
is lost visibly, in an answer somebody can read, rather than quietly
in a merge nobody sees.

The rule being changed is kept out of the examples beside it.
Listed twice it would be read as a rule to copy as well as the rule
to mend.

And a change that is not about what the rule does does not go to the
writer at all. Renaming one, or asking for it to reach them wherever
they are, touches nothing it does -- and a rule rewritten in order
to be renamed is a rule that could come back different.


### The wake word survives what the recogniser cannot

Owner: *"waking happens at a distance but my command is not. Why?"*

The satellite's log says it plainly. Three wake words at 0.995,
0.987 and 0.855, every one of them followed by VAD_START and
VAD_END -- so the microphone heard it and the voice detector heard
it -- and then: *"What is the time?"* correctly, *"We'll be waiting
later tomorrow morning"* for something that was not that at all,
and once `stt-no-text-recognized`.

A fluent, confident sentence built out of mangled phonemes is not a
recogniser mishearing a word. It is a recogniser reconstructing from
mush.

Measured, on the satellite's own processor, with a quiet
far-speech signal at RMS 254:

	                 NS=Max   NS=Off
	AGC 0 (as set)       91      250
	AGC 9               206      563
	AGC 15 / 22 / 31    206      563

Set as it was -- gain off, suppression at maximum -- the thing
threw away nine decibels of the signal before Deepgram saw it. The
microphone heard the command; the processing in front of the
recogniser removed it.

The wake word survives that because `microWakeWord` is a small
binary classifier trained on far-field augmented audio, deciding one
question: was that the phrase. ASR has to resolve every phoneme.
That gap is physics and cannot be closed -- but it was being widened
by three or four times, in the two settings in front of it.

Two things the measurement settled that guessing would not have.
Gain above 9 does nothing at all: 9, 15, 22 and 31 are identical,
because on a signal this quiet the gain is already pinned at its
maximum, so the dial is on or off and nothing between. And
suppression at maximum costs a factor of 2.7 *even with gain on*,
which makes it the larger of the two faults.

Gain 15, suppression off. Both are Home Assistant entities and the
satellite picks them up on the next block -- `Updating WebRTC
settings: Gain=15, NS=0` -- so neither needs it restarted, which
matters because it must not be.

What to watch, because it is the real trade. The wake word reads
the same audio, and suppression at maximum was holding false wakes
down; the owner had already noticed *"sometimes the wake word is
called without calling it."* If they return, the answer is to raise
the wake word's sensitivity from 0.7 and not to put suppression
back: suppression trades false wakes against transcription, which is
the thing that was failing, and sensitivity trades them against
range, which is not.

And the heartbeat will not show any of this. It counts the level
before processing on purpose, so that it reports what the microphone
gave rather than what suppression left of it.

### The side effect was the one I did not predict

Owner, twenty minutes later: *"the continuous conversation is going
to idle so fast."*

It was, and it was mine. Suppression off and gain on, measured over
the turns on either side of the change:

	                                   before   after
	VAD fired within 1.3s of opening      37%      92%
	and then ran longer than 1.0s         71%      38%

The window was not short. It was being spent before anybody spoke:
the mic opens, a second later the detector says speech, seven tenths
later it says silence, and the recogniser is handed a second of
room noise. `speech_seconds` is 0.3 and `silence_seconds` is 0.7, so
one second of amplified hiss is a whole turn.

I had warned about the wrong side effect. Suppression at maximum was
holding something down, and I said it was false wake words; what it
was holding down was the voice detector.

The measurement that should have been made first, speech *and*
noise through each level at gain 15:

	 ns   speech   noise   ratio
	  0      563     214    2.6
	  1      323     108    3.0
	  2      270      61    4.4
	  3      236      39    6.0
	  4      207      28    7.5

Which says the thing the first measurement could not: **gain does
not change the ratio at all.** It scales speech and noise together,
and only suppression changes which is which. So the detector was
never tripping because the ratio got worse -- it was tripping
because the absolute noise went from about twelve to two hundred,
and its threshold is absolute.

What was wanted was speech above two hundred, where the recogniser
works, and noise near forty, where the detector was quiet. Gain 15
with suppression High: speech 236, which is two and a half times
what it was, and noise 39, a fifth of what tripped it.

Off was a step too far, and measuring one signal instead of two is
how I took it.


### A window with no end of its own

Owner: *"when continuous conversation is set, it should wait 5 sec
and if no word comes it should go to idle."*

It never did. The microphone opened after the chime and stayed open
until Home Assistant's own segmenter gave up at forty-five seconds,
so an answer nobody followed left the room being listened to for the
best part of a minute -- and every cough in it was a turn. Part of
what reads as *going idle too fast* was the opposite: it was still
listening, and spending the window on noise.

Five seconds, counted from the moment the mic actually opens rather
than from the answer ending, because the chime is the thing that
says it is their turn and the clock should not run before it.
Cancelled the moment the detector says somebody is speaking, and on
RUN_END, on an error and on a stop, so a timer can never fire into a
turn that is already over.

It sends `start=False`, which is what the stop word sends. Home
Assistant is holding a turn open and waiting on audio, and closing
the microphone without saying so would leave it waiting out its own
forty-five anyway.

`FOLLOW_UP_TIMEOUT` in the environment, like the switch beside it and
for the same reason: the state object is upstream's and this is not,
and adding a field there would make a third patched file of a thing
that needs two.

### The file that is edited is not the file that is mounted

The restart was clean, the satellite came back healthy, and the
change was not running. `build/` is where patchwork rediffs from;
`patched/` is what the container mounts. Editing one and restarting
proves nothing about the other.

There is a comment in the compose file about exactly this, written on
6 October: the mount said `./build` while the running container had
`./patched`, they drifted twenty-two lines apart, and the wrong path
cost nothing until somebody recreated it. The trap caught the next
person to walk past it.

And `restart-satellite` stops and starts, which does not re-read
`.env`. A new environment variable needs `docker compose up -d`.

So the check that matters is not *did it restart* but *is the new
thing in the container*:

	docker exec linux-voice-assistant sh -c \
	  'grep -c _follow_up_seconds /app/linux_voice_assistant/satellite.py; printenv FOLLOW_UP_TIMEOUT'

Nought and nothing the first time. Four and five after
`patched/` was brought level and the container recreated.


### A stranger knocking takes the wake word with it

Owner, for the second or third time: *"why is it not waking? I'm
facing this trouble many times."*

The counters said it outright, and they were built to:

	08:11:52  read=938  listened=938  unattached=0
	08:12:23  Incorrect preamble: 72
	08:12:26  Disconnected from Home Assistant
	08:12:52  read=938  listened=535  unattached=403
	08:13:52  read=938  listened=0    unattached=938
	08:14:52  read=938  listened=0    unattached=938
	08:15:15  Home Assistant reconnects

Two minutes and fifty seconds stone deaf. The microphone was fine --
938 blocks a minute at an ordinary level throughout -- and the
container was healthy, and nothing was visible from the room but a
wake word that did not answer. `unattached` means the wake word loop
was skipped because `state.satellite` was None: no Home Assistant,
no listening.

What happened at 08:12:23, in order: a connection arrives and is
rejected by the websocket server as not valid HTTP; the satellite
begins an ESPHome handshake with it anyway and re-initialises its
entities for it; the thing then sends HTTP, and the API server says
*Incorrect preamble: 72*, which is the H of an HTTP verb; three
seconds later Home Assistant is gone.

Something on the network was probing the laptop's open ports. There
is one connection slot, so the probe overwrote the real one, and
when the probe died the slot was cleared with it.

It was reachable because `HOST` was `0.0.0.0`, and that was set
deliberately: upstream binds to whatever address the interface
happens to have, and a DHCP lease change left Home Assistant dialling
an address that no longer existed. The note in `.env` even says why
0.0.0.0 was chosen -- *"the satellite is also reachable on 127.0.0.1,
which is what Home Assistant uses and which DHCP cannot take away."*

Which is the whole of what was wanted, and 127.0.0.1 is the setting
that says only that. Home Assistant's config entry dials
`127.0.0.1:6053`; DHCP cannot take the loopback away; nothing on the
network can reach it; and the healthcheck reads established
connections locally and does not care. 0.0.0.0 bought the DHCP fix
and an open door nobody asked for.

6055, the peripheral websocket, went the same way a few minutes
later. The same probe reached it first and it cost nothing, because
it only closed its own connection -- but it carries LED and button
events for peripherals nobody here has, nothing on this machine
connects to it, and a port open for no reason is a door somebody
else gets to knock on. Closed second rather than at the same time, so
that if the satellite had broken it would have been clear which
change did it.

Both on the loopback now, and nothing the satellite does reaches
past this machine:

	LISTEN 127.0.0.1:6053
	LISTEN 127.0.0.1:6055


### Nine times, at point eight one two

Owner: *"while listening I say stop, it's not stopping."*

	08:25:05  Continued conversation started
	08:25:06  STT_VAD_START
	08:25:32  Stop activated 0.777, 0.800, 0.629   -- nothing
	08:25:54  Stop activated 0.812 x5, 0.803       -- nothing
	08:26:01  STT_VAD_END
	08:26:04  Stop activated 0.787  -- abandoned, stop.wav

He said it twice while a turn ran for fifty-four seconds. Nine clean
detections, the model certain every time, and the dispatcher threw
all nine away. The third attempt worked because by then the window
had closed and a different path had armed it.

`_arm_stop_word` is called everywhere listening begins -- after a
wake word, after the chime that follows one, after a button -- and
was not called in the one place listening begins without anybody
asking for it. `_tts_finished` discards the stop word when a turn
ends, and the reopened window never put it back.

The docstring on `_arm_stop_word` describes this failure exactly,
including the number: *"The model fires -- Wake word 'Stop'
activated (0.812) appears in the log -- and the dispatcher throws it
away."* Somebody met it, fixed the path they were standing in, and
left the other one. I added the follow-up timer to that same
function an hour before this and did not notice what was missing
beside it.

Worth saying which window this is. A reopened one is the window
most likely to need stopping, because nobody asked for it: the
person who wants it to stop is the person who did not want it open.


### A room the gate cannot close on

Owner, from the office: *"it's listening even though I don't talk."*

It was, and had been for a minute when he said so:

	08:30:10  Continued conversation started
	08:30:11  STT_VAD_START   -- one second after the mic opened
	08:31:10  still listening

Twelve seconds of that room, measured through the satellite's own
processor at the settings it was running:

	raw rms 365, peak 7380; after gain 15 and suppression High, rms 602

	 10ms chunk levels     50th   118     90th    735     99th  2556
	                       75th   297     95th   1155     max   5841

	above the audio gate of 400: 19.9% of chunks

One chunk in five above the gate, and each one holds it open for a
hundred and fifty milliseconds. Twenty per cent loud times fifteen
chunks of hold is a gate that never closes, a detector that never
sees silence, and a turn that cannot end.

Raising the gate does not answer it. Closing on that room takes
about 2600, which is above the owner speaking from across it. The
room is the problem and no threshold separates them.

Which window matters, and this is the part the numbers settle: the
turns he started by saying the wake word ended in one or two
seconds. The two that ran fifty-four and fifty-nine were both
reopened windows -- the ones nobody asked for. A turn somebody began
has somebody present for it, who speaks and then stops. A window
that opens by itself into a room of other people talking has
nothing in it that was ever going to end.

So the reopened window has two ends now, and the second is the one
that matters. `FOLLOW_UP_TIMEOUT` is cancelled when the detector
says somebody started, which is right when it is right. `FOLLOW_UP_MAX`
is cancelled by nothing but the turn ending, because whether the
detector is right is exactly what cannot be relied on here. Fifteen
seconds: longer than any follow-up anybody says, a quarter of what
the room was costing.

Owner chose the cap over switching the reopened window off, which is
the harder of the two and the one that still works at home.


### A switch that belongs in different positions in different rooms

Owner, having taken the cap: *"have the top option home and office."*

Both, then, and chosen by where the laptop is. At home a reopened
window is answered or falls quiet; in an office it opens into other
people's conversations and no threshold tells them apart.

An environment variable could not do it. `CONTINUE_AFTER_EVERY_REPLY`
is read once at startup, and changing it means recreating the
container, which stops the wake word -- for a thing that should
change twice a day.

So it is a switch. Not a new entity class, which would have meant
patching a third file of upstream's: `ThinkingSoundEntity` is already
a switch with a getter, a setter and a name, and the name is a
parameter. A second instance of it is
`switch.…_continue_conversation`, and the only care needed was
matching the existing ones by `object_id` rather than by class, or
each would adopt the other and delete it.

Home Assistant throws it from the network:

	Dhanush, Dhanush_5G, Dhanush_EXT  ->  on
	everything else                   ->  off

Named networks here, where presence deliberately does not name them.
Different question: presence asks *has the laptop moved*, where the
change is the signal and a name would be a lie; this asks *which room
is this*, and the answer is a name. Everything not known to be home
turns it off -- the office, its guest network, a hotspot, a cafe --
because the quiet side is the safe side in a room nobody has
measured.

Thrown on arriving and not only on changing: on Home Assistant
starting, and when the switch comes back from unavailable. The
satellite reads the environment when it starts and the switch is only
for the run, so a restart in the office would otherwise quietly turn
it back on.

### Dead in its own constructor

The switch asks for its value while it is being registered, and
`_after_every` was set two hundred lines further down. The satellite
died in `__init__`, restarted, died again, and the entities went
unavailable in Home Assistant.

Found because the check after a restart is not *did it come back* --
it said `Up 25 seconds (healthy)` throughout -- but whether the log
is clean *since the current start*. `RestartCount` is no use here
either: `restart-satellite` stops and starts rather than letting
Docker do it, so Docker counts nought while the process loops.

	docker inspect linux-voice-assistant --format '{{.State.StartedAt}}'

and read only what came after it.


### The Room selector had been a ghost

Owner, with a screenshot of the voice panel: *"keep the home and
noisy office, and have an option for continuous reply on/off too."*

The panel already had a Room selector, and it read **unavailable**.
Home Assistant was restoring `input_select.voice_profile` from its
registry -- `restored: true`, options still attached -- while nothing
defined it any more. It had been made through the interface, which
puts it in `.storage` where it cannot be read or reviewed, and then
it was lost. So the automation that rewrites the settings for the
room had nothing to read, and had been doing nothing for some time.

Declared in `configuration.yaml` now, beside everything else that is
declared. The dashboard's own comment said it should have been:
*"so it lives in this repository rather than in .storage where it
cannot be read or reviewed."*

The profile grew two settings. Gain, at 15 in both -- it scales
speech and noise together and changes no ratio, so there is nothing
to choose per room. And whether the microphone reopens after a reply,
which is the one that cannot be a threshold at all: at home a
reopened window is answered or falls quiet, and in the office it
opens into other people's conversations.

One of the four it already had was a trap. Quiet room set suppression
`Off`, which was right when it was written at gain 0 and is the
disaster of this morning at gain 15 -- amplified room noise tripping
the voice detector within a second, 92% of the time against 37%.
`Low` now.

And the network chooses the room rather than the settings. An earlier
automation written an hour before this one threw the reopen switch
directly; two things writing the same five entities would have each
undone the other at startup. The profile is where the settings live
and where the reasons for them are written, so the network sets the
profile and stops there.

Three things left behind, all the same shape. An entity in the
registry that nothing defines does not disappear -- it sits there
answering "unavailable", and a panel built on it looks configured and
does nothing. `input_text.last_network` and the hour-old automation
are two more of them, both dead and both harmless, and clearing them
means editing `.storage` with Home Assistant stopped.

The entity id of an automation comes from its `alias`, not its `id`.
Triggering `automation.the_room_is_chosen_by_the_network` -- the id --
returned 200 and ran nothing at all.


### The gate was not stuck open; the patience was growing

Owner: *"suppose in a noisy room, if I talk louder than them, that is
considered -- and when the same loud sound is not heard, it means I
stopped talking."*

A good idea, measured and rejected, and the measuring found the real
mechanism instead.

Eighteen seconds of the office, through the satellite's own
processor, with its shape visible for once:

	sec 0-3    5071 1183 1368  418   somebody talking
	sec 4-8      27   38   40   28   genuinely quiet
	sec 9-16   434 ... 11311 ... 3755   talking again

The room is not constantly loud. It has real silence between bursts,
and its floor is about thirty -- not the five hundred my first floor
tracker settled on, which was the tracker being wrong rather than the
room.

What the gate actually allows, as the longest unbroken silence:

	gate     longest silence   opens on speech
	  400          1.36s            96.4%
	 1000          3.23s            91.9%
	 1500          4.87s            90.4%
	adaptive x8    1.04s            98.4%
	adaptive x30   1.35s            96.4%

So the gate at 400 was never simply stuck open. It allows 1.36
seconds, which is twice what the segmenter needs to end a short
command -- and short commands did end, in one or two seconds, all
day.

The thing it cannot do is end a long one. `_allowed_gap` grows the
silence it asks for the longer a turn has run: 0.70s while the
command is still short, approaching 3.5s with a seven-second time
constant. After ten seconds it wants 2.47s; after twenty, 3.12s. The
office never offers more than 1.36. So a turn that gets past about
eight seconds can never end at all, and runs to Home Assistant's
two-minute cap.

1500 allows 4.87s, which clears the 3.5 ceiling, so a turn can end
however long it has run.

And the adaptive gate loses at every margin, for a reason worth
keeping: the room's quiet sits near thirty and its bursts near ten
thousand, three hundred times apart. Any fixed line between them
separates them. Tracking the floor only makes the line wander, and a
margin large enough to be useful is a fixed line with extra steps.

### Setting a thing to what it already is changes nothing

The recreate that applied the gate restarted the satellite, which
read `CONTINUE_AFTER_EVERY_REPLY=1` again and turned the reopened
window back on -- while the panel showed `Noisy office`, which means
off.

The automation that picks the room from the network had run. It sets
the profile, the profile was already `Noisy office`, and setting an
input_select to the value it already holds is not a state change, so
the automation that applies the settings never fired. Everything
looked right and nothing had been written.

It applies on the satellite returning now, as well as on the profile
changing. The same lesson as the switch itself: a thing that has to
survive a restart must be driven by *arriving*, not only by changing.


### A microphone that can be changed without stopping the satellite

Owner, on earbuds: *"we can have a mode like bluetooth on in Home
Assistant -- a toggle."*

The obstacle was not Bluetooth. The satellite reads one PulseAudio
source for its whole life, and PulseAudio cannot re-point a loaded
module -- so a source built on a device is married to that device.
Changing it meant unloading `module-echo-cancel`, which destroys
`voice_only`, which is twenty-five seconds with no wake word every
time an earbud goes in or comes out. A toggle that costs that is not
a toggle anybody throws.

So nothing is built on the device any more:

	microphone -> module-loopback -> null sink "mic_in" -> mic_in.monitor
	                   the only thing that moves                |
	                                                            v
	                                              module-echo-cancel
	                                                            |
	                                                            v
	                                                      voice_only -> satellite

A null sink in the middle never goes away. Swapping the microphone is
unloading one loopback and loading another, and the monitor of a null
sink does not notice.

Measured under the running satellite, laptop microphone to the
monitor's: 938 blocks a minute throughout, `listened=938`,
`unattached=0`, and the level changing from 433 to 1244. It never
missed a block. One second of quiet while the loopback rebuilds,
against twenty-five for a restart.

Output needed none of this. `AUDIO_OUTPUT_DEVICE` is deliberately
unset so mpv plays to the default sink, and moving the default sink
moves the assistant's voice with it.

`audio-source` owns the chain and `watch-earbuds` carries the toggle
across the container wall, because Home Assistant is in a container
and PulseAudio is not -- the same gap `satellite-resume` closes, in
the same way. The watcher acts only when the toggle and the
microphone disagree: running the swap again costs a second of quiet,
and a watcher paying that every five seconds would be worse than
none.

It does not turn the toggle off when a headset drops out. That would
move the microphone back at every lost second; the setting is held
and the headset waited for.

What none of this fixes is the profile. A headset cannot offer a
microphone and good audio at once, and the satellite holds the
microphone open always -- so earbuds sit in call quality for as long
as they are chosen. The toggle makes switching free. It does not make
wearing them all day pleasant, and that is the thing to decide with
earbuds in rather than from here.


### The microphone matters more than the room

Owner, earbuds in: *"it's working."*

	09:33:13  "What is the time?"
	09:33:20  "What is the date?"
	09:33:29  "What is the status of, b..."
	09:34:01  "Hey, Jarvis. Look again..."

Four in a row, clean, with reopened windows between them. The same
room an hour earlier, through the laptop microphone, gave *"We'll be
waiting later tomorrow morning"* for something that was not that, and
once nothing at all.

The numbers say why. Through the earbuds, after the same gain and
suppression, the median ten-millisecond chunk is **1**. Through the
laptop microphone in that office it is **985**. The noise this whole
day was spent fighting is a far-field problem, and near-field does
not have it.

Which makes most of the day's tuning beside the point while they are
in. The gate at 1500 exists to reject a room; there is no room. The
suppression at High exists to stop the detector hearing furniture;
the detector hears silence. `Noisy office` is solving a problem that
is no longer there.

So the settings should follow the microphone and not only the
network, and one of the six cannot: `AUDIO_GATE_RMS` is an
environment variable rather than an entity, so unlike the rest it
cannot change without recreating the container. Worth an entity only
if the gate turns out to clip quiet speech, which four clean
sentences suggest it does not -- the hundred and fifty milliseconds
of hold carries the soft parts of words.

One thing to watch rather than act on: the stop word fired seven
times in ten minutes at a threshold of 0.75. Some of those were
asked for. If they were not, it is the office profile's number on a
microphone that is in somebody's ear, where nothing else is competing
to sound like "stop".


### Twice sir

Owner, hearing one go out: *"while announcing it is using twice sir
-- like 'Sir, the battery is 25%, sir'."*

Both halves were obeying their own instruction, which is why neither
looked wrong on its own.

The persona says to address the person once in each reply and never
twice, and the model does -- every stored announcement has exactly
one. The opening is added afterwards, by `opened`, for a reason that
is also right: the first second of an announcement is spent getting
somebody's attention rather than carrying anything, so it is given a
word it does not matter to lose. Neither knows about the other.

The opening is the half that stays. A word at the end of a sentence
cannot do the job of getting attention at the start of one, so the
sentence gives its copy up.

Taken out of the middle as well as off the end. It lands there as
often as not -- *"The silence from your phone, sir, has now stretched
to three hours"* -- and that is a second one just as much.

Two things the tests caught that the fix needed and the description
of it did not. The punctuation the address was leaning against:
*"fifty percent, sir."* became *"fifty percent ."*, and a satellite
reads that gap aloud as a pause. And the capital: taking *"Sir,"* off
the front of *"Sir, the battery is low"* leaves a sentence that has to
start somewhere.

That capital changed an older test, which had asserted *"Sir. your
battery is low."* -- the old behaviour written down rather than the
right one.


### One gate, two microphones

Owner: *"sometimes it's listening, but I talk some words and it goes
idle."*

	10:05:08  STT_START                   no VAD_START  -> 13s -> "Hello"
	10:05:22  STT_START  -> nothing said in 5.0s, window closed
	10:05:32  STT_START                   no VAD_START  -> 13s -> "Hello?"
	10:05:46  STT_START  -> STT_VAD_START -> 2s -> "What is t..."
	10:05:58  STT_START  -> nothing said in 5.0s, window closed

The detector was not firing. Where it did, the turn was two seconds
and correct; where it did not, Deepgram scraped a "Hello" out of
thirteen seconds of nothing, or the five-second timer closed the
window with somebody talking into it. That timer was doing its job
and looked like the fault.

The gate. 1500 was measured this morning on the office through the
laptop microphone, where it is right: that room has to be rejected
and a turn has to be able to end. Through earbuds the room is not
there -- the median ten-millisecond chunk is 1 -- so 1500 sits in
the middle of the speech rather than above the noise. A tenth of
chunks cleared it and the rest were replaced with silence before
anything could hear them.

300 for now, which is above an earbud's floor and far below anything
said into one.

But the number is wrong whichever way it is set, because it is one
number for two microphones, and that is the thing to fix. Every
other audio setting is an entity and follows the room; the gate is
read once from the environment at startup, so it can follow nothing
and changing it costs a recreate. It wants to be a number entity
like the gain beside it, and then the profile can set it and the
microphone can choose it.


### Earbuds are not a room

Owner, with them in: *"I say 'Hey Jarvis' and it's not waking.
Remember when I use earbuds I speak softly only."*

It was waking -- fourteen times in twenty minutes -- just not when
asked. The settings said why:

	Room                   Noisy office    chosen by the office wifi
	wake must be this sure 0.8
	noise suppression      High

Every one of those was raised to resist a room. A threshold is how
certain the model must be before it acts, and 0.8 is deliberately
hard to reach so that a room full of talking does not set it off. An
inch from somebody's mouth nothing is competing, and 0.8 asked a
softly spoken "Hey Jarvis" for a confidence it could not reach.
Suppression at High was taking the quiet parts of quietly spoken
words out of a signal that had no noise in it to justify the cutting.

So the profile is chosen by the microphone when there is one worth
choosing by, and by the network otherwise. Earbuds beat whichever
room he is standing in, because they are not one: no echo, nothing
else talking, and a person who speaks softly into them.

(*"Nothing else talking"* survived one day. See "An earbud is not a
directional microphone" below: it is true of a room's hum and false
of a person standing in it.)

	wake 0.5, stop 0.7, suppression Low, relaxed, gain 15, window reopens

Relaxed rather than aggressive for the same reason the office needs
the opposite. Aggressive exists to find a gap in a room that never
has one; an earbud has nothing but gaps, and aggressive would cut
somebody off between words.

The thing that keeps coming back: these settings were all chosen
against a room, and every one of them is wrong when the microphone
is not in the room. The only one that still cannot follow is the
gate, because it is read once from the environment at startup.


### A sequence is not a clock

Owner: *"the time is wrong whenever the AI says it."*

Every clock in the house was right -- the laptop, the Home Assistant
container, its timezone, MySQL, the server's own configured zone --
and the line the model is given was right too, checked by rendering
it: *"The time where the person is: 3:48 pm on Friday 9 October 2026
(IST+05:30)."*

The conversation is what was wrong.

	13:57 IST   "The time is 1:40 pm"    seventeen minutes slow
	14:46 IST   "The time is 1:41 pm"
	14:58 IST   "The time is 1:42 pm"
	15:05 IST   "The time is 1:43 pm. That is what I was told."
	15:42 IST   "The time is 1:47 pm"

Four minutes of reported time across two hours of real time -- one
minute per question asked, not per minute passed. One answer went
out seventeen minutes slow and every answer after it copied the one
before and added a minute. A chat of six hundred and thirty-eight
messages, with its own wrong answers nearer the end of the window
than the clock at the top of it, and the model continued the
sequence it could see rather than reading the line it was given.

Asked directly whether it was sure, it said *"that is what I was
told"*, and separately *"I have not been given a tool to check the
actual current time"* -- believing both, and wrong about both.

The line says more now: that it is the only clock, that times said
earlier in the conversation were right when they were said and are
stale, and that one is never to be carried forward by adding to it.
It had been left to be inferred that a clock beats a pattern, and in
a window full of the pattern it was not inferred.

What this does not fix is the chat it happened in, where the
sequence is still visible. A new conversation starts clean.


### Two sounds three milliseconds apart

Owner: *"when it goes idle I should get a sound"*, and then *"when
listening I say stop, it goes idle, but I don't get sound."*

Two faults, and the second had been there longer.

The idle sound played after the fifteen-second cap and not after the
five-second wait. Both ended through `_tts_finished`, which plays it
past two guards written for a turn that *spoke* -- and a window
nobody answered is not one of those. A window closing is its own
end and marks itself now, with a moment's guard so the other path
does not mark it twice.

The stop was worse and more interesting:

	10:31:38.858  Asked Home Assistant to abandon the pipeline
	10:31:38.858  stop.wav
	10:31:38.861  idle.wav

Three milliseconds, and the second cut the first. `stop()` already
knew this could happen -- its own comment says *"two sounds one
after the other say less than either alone"* -- and guards it with a
flag held while it runs. But the RUN_END that Home Assistant sends
in answer arrives after `stop()` has returned and cleared the flag.
A flag cannot cover a race that outlives the function holding it.
A moment can, so the stop records one.

### The rate that should have been free

Owner: *"in bluetooth there is a delay in wake word listening and
stop to idle latency."*

Measured, the chain was converting every chunk twice for nothing:
the earbuds give 16000 mono, the null sink held 44100 stereo, and
echo-cancel took it back to 32000 mono. Matching the sink to the
rate at both ends looked free.

It cost thirty milliseconds. `voice_only` went from six to
thirty-six, measured settled and streaming, three readings apart.
PulseAudio sizes these buffers in frames, so the same buffer at a
lower rate is more time -- and the resampling it saved was the cheap
kind.

Put back the same hour. What was kept is the loopback's own buffer,
twenty milliseconds to ten, which is small and real. The headset's
own thirty-eight cannot be argued with and is most of what is left.

Worth writing down because it looked like free waste and was not.
The measurement that settled it is the same one that should have
preceded it.


### An earbud is not a directional microphone

Owner, mid-sentence, the problem in it: *"I speak to the mic and I
stopped, but my colleague speaking near me -- my mic is listening,
why? That too I'm in earbuds."*

Fourteen seconds captured from the earbud microphone while it was
happening, run through the Earbuds profile's own settings -- gain 15,
suppression Low -- the way the satellite would see it:

	50th percentile of 10ms chunks       359
	75th                                1093
	90th                                2781
	95th                                5337
	max                                14138

	above the gate of 300              54.1%
	above 1500                         19.8%

Fifty-four per cent is not leakage. That is a second voice arriving
at full strength, and the peaks are as loud as speech gets.

It overturns the reason the earbuds were reached for. The section
above is right that they are not a room -- when nobody speaks the
median chunk is **1**, against 985 on the laptop microphone, which is
a two-order-of-magnitude rejection of fan, hum and air conditioning.
But that is isolation from *noise*, and a colleague is not noise. An
HFP earbud microphone is a single omnidirectional capsule. It is
nearer the owner's mouth than anybody else's and that is the whole of
its advantage: a few decibels, not a different signal.

Which closes the door the gate was standing in. The owner's voice and
the neighbour's land in the same range, so there is no number that
admits one and refuses the other. 300 was lowered to 300 precisely so
quiet speech into an earbud would pass; it lets a colleague past for
the same reason. Raising it back to 1500 would reject both.

So the turn is given an end that does not depend on hearing one --
the same answer as the reopened window, extended to the half that was
exempted. `TURN_MAX=30`: longer than any command measured here, a
quarter of Home Assistant's own two minutes.

The exemption was reasoned and wrong. It said a turn started by
saying the wake word is a turn the person is present for, and so
needs no cap. Presence is not the variable. What holds a window open
is somebody *else* talking, and that is indifferent to whether the
owner meant to open it.

What would actually separate two voices is a microphone that can tell
where one is coming from -- a bone-conduction pickup, or an array
doing beamforming. Earbuds at this price have one capsule and no
array. It is not a setting that is missing.

### A tone nobody can hear is not a quiet tone

Owner: *"sometimes when it goes idle I don't get the sound."*

The first move was to look for the ending that forgot to play it.
There isn't one. Six hours of the satellite's log, every turn walked
from `RUN_START` to the end:

	turns that ended                     46
	ended with idle.wav                  30
	ended with stop.wav                  16
	ended silently                        0

Sixteen of those are a "stop", which has its own sound and
deliberately suppresses the idle one. Every single ending made a
noise. Then the file itself:

	idle.wav      55 ms   -24.5 dBFS   392 Hz
	stop.wav     180 ms   -16.7 dBFS   660 -> 440 Hz

A quarter of the amplitude and a third of the length. Both were
deliberate -- it marks an ending, and a chime after every turn would
be worse than silence -- and both went too far.

The frequency was the part that was not about volume at all. 392 Hz
was chosen to sit *below* the stop sound's lowest note so the two
could not be confused. A laptop speaker barely reproduces 392 Hz; a
small driver rolls off well above it, and an earbud is not much
better. The tone was being attenuated by the hardware before
loudness entered into it.

Now C5 at the stop sound's own amplitude, held 150 ms. What keeps
the three sounds apart was never the level: one steady tone, two
falling tones, a rising wake chime. Shape does that work.

The general lesson, which is the reason this is written down: when
something intermittently *is not perceived*, establish whether it
happened before looking for why it did not. Here the code was right
and had been right the whole time, and half an hour went into
proving it. The measurement that mattered took one command on the
file.

`satellite/sounds/` is a bind mount and mpv opens the file on each
play, so a new sound is live the moment it is written. No restart.

## 12. Maintaining this file

When the owner states a preference, makes a decision, or corrects something,
**record it here** in the same turn. That is the point of the file: a fresh
agent on a different machine should be able to read it and behave consistently
with every session that came before.

Record the reasoning, not just the conclusion. A decision whose rationale is
lost gets reversed by the next person who thinks they know better.

Delete entries that become wrong. A stale instruction is followed just as
faithfully as a correct one.

## 13. Maintaining the map

[`FEATURES.md`](FEATURES.md) is the map of what exists: the ways in,
the tools, the six watchers that speak unasked, the quiet daily work,
and the limits that are known.

**It is changed in the same turn as the thing it describes.** A tool
added or removed, a watcher wired in, a flow rerouted, a limit found
or fixed -- the map moves with it. This file says *why*; that one says
*what*, and a map that lies about what exists is worse than no map at
all, because it is believed.

Two habits keep it honest. Take the tool list from the server's own
`tools registered` line rather than from grep, which picks up
fixtures out of test files. And count rather than estimate: the
first draft of the map said 51 tools when there were 52.
