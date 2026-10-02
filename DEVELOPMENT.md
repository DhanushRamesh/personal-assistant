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

## Present, away, and the third answer

The second of the three presence pieces, 29 September 2026.

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

Still to come: three states instead of two, so a fault reads as
`unknown` rather than `away`; and the wifi SSID, so leaving with the
laptop is visible.

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

## 12. Maintaining this file

When the owner states a preference, makes a decision, or corrects something,
**record it here** in the same turn. That is the point of the file: a fresh
agent on a different machine should be able to read it and behave consistently
with every session that came before.

Record the reasoning, not just the conclusion. A decision whose rationale is
lost gets reversed by the next person who thinks they know better.

Delete entries that become wrong. A stale instruction is followed just as
faithfully as a correct one.
