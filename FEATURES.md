# What Jarvis does

A map of the whole thing, kept current. If you are looking for *why*
something is the way it is, that is [`DEVELOPMENT.md`](DEVELOPMENT.md);
this is only *what exists and how it connects*.

**Keep it current.** A feature added, removed or rewired is changed
here in the same turn, the way a decision is recorded in
DEVELOPMENT.md. A map that lies is worse than no map.

---

## 1. The shape of it

Everything funnels into one core. The voice and the web client are
the same brain with a different mouth, which is why a thing fixed
for one is fixed for both.

```mermaid
flowchart TB
    subgraph IN["Ways in"]
        direction LR
        SAT["Voice<br/>satellite in the room"]
        WEB["Web client<br/>and HTTP API"]
        PHONE["Phone<br/>Tasker"]
    end

    HA["Home Assistant<br/>wake word, speech to text,<br/>text to speech"]
    EVENTS["/v1/events<br/>what the phone noticed"]

    SAT --> HA
    PHONE --> EVENTS
    PHONE -. "presence" .-> HA
    HA --> CORE
    WEB --> CORE

    CORE["The core<br/>persona, memory, tool loop"]
    MODEL["Model<br/>PlatformAI"]
    TOOLS["52 tools<br/>section 2"]
    DB[("MySQL<br/>reminders, memories, events,<br/>conversations, places, profile")]

    CORE <--> MODEL
    CORE --> TOOLS
    CORE <--> DB
    EVENTS --> DB
    CORE -- "an answer" --> HA

    UNASKED["Speaking unasked<br/>section 3"]
    ALARMS["The phone<br/>alarms over the tailnet"]

    DB --> UNASKED
    UNASKED --> HA
    UNASKED --> ALARMS
```

---

## 2. What it can do when asked

52 tools, grouped by what they are about. The model picks; nothing
here is keyword matched.

```mermaid
flowchart LR
    CORE(("The core"))

    CORE --- REM["Reminders<br/>set, list, recent,<br/>update, snooze, cancel"]
    CORE --- CAL["Calendar<br/>events, add, update, cancel,<br/>rename, free, calendars"]
    CORE --- TASK["Tasks<br/>add, done, remove, update, move,<br/>find, clear, list, lists"]
    CORE --- MEM["Memory<br/>remember, list, search,<br/>update, forget"]
    CORE --- EV["Events<br/>recent, kinds"]
    CORE --- PLACE["Places<br/>find, near, distance, visits"]
    CORE --- MAIL["Mail<br/>inbox, unread, read,<br/>search, thread"]
    CORE --- WEB["Web<br/>search, read"]
    CORE --- CONV["Conversations<br/>new, list, find, switch,<br/>rename, archive, delete"]
    CORE --- MISC["Other<br/>coming_settled, tool_describe"]
```

**Where to look first.** Anything about this person's own life is
answered from the calendar, tasks, reminders, events or memory —
never the web, which would send a private name to a stranger. The
web is for the world, not for him.

---

## 3. What it says without being asked

Each of these is its own watcher, and each decides on its own
whether there is anything worth saying. Silence is a valid answer
everywhere here.

```mermaid
flowchart TB
    subgraph L["The logic -- is there something to say?"]
        direction LR
        SI["Situations"]
        RN["Renaming"]
        RM["Reminders"]
        GR["Greeting"]
        CO["Diary"]
        LV["Leave by"]
        IM["Immediate"]
    end

    SI --> EV
    RN --> EV
    RM -.-> OLD
    GR -.-> OLD
    CO -.-> OLD
    LV -.-> OLD
    IM -.-> OLD
    RN -.-> OLD

    EV["One event object<br/>who it is for · the sentence · when it became true<br/>how long it stays worth saying · which client caused it"]
    EV --> PUB{"Publisher<br/>audience looked up from the kind"}

    PUB -- "Asked" --> ONE["Only the client that caused it"]
    PUB -- "Everywhere" --> ALL["Every client of that person"]
    PUB -- "Asked with no cause" --> NO["Refused<br/>never broadcast on a guess"]

    ONE --> DB
    ALL --> DB
    DB[("owed + owed_clients<br/>one copy per client, pending")]

    DB --> GET["GET /v1/announcements<br/>what THIS client owes"]
    GET -.-> NOBODY["nothing collects yet"]
    GET --> CL{"The client decides<br/>presence lives here"}

    CL -- "typed: a screen needs nobody" --> MARK["POST /v1/announcements/id<br/>said"]
    CL -- "voice: somebody is there" --> SAY["Say it aloud, then said"]
    CL -- "voice: empty room" --> KEEP["Write nothing<br/>the copy stays pending"]
    KEEP --> GET

    MARK --> DB
    SAY --> DB
    DB --> SW["Sweeper<br/>what went stale is marked expired"]

    OLD["Spoken directly to the satellite<br/>the old path, still in place so nothing went quiet"]
```

**The rule underneath all of it:** nothing is said unless something
asked for it to be said. There is no chatter watcher, by decision —
only the six above.

**The logic and the event object are different things.** Greeting,
renaming, reminders, situations, leave-by and the diary are *logic*:
each watches something different and answers one question, *is there
something to say*. What each produces is the same **event object**,
and everything after that is one path that does not care which logic
made it. The name on it -- reminder, renamed, immediate -- is
provenance, not a species.

**Published once, delivered per client.** An announcement goes into
the outbox before anything is said, and every client of that person
gets its own copy with its own verdict. *Was it announced* is not one
fact about one person: the same warning can be read on a laptop and
missed by a speaker in an empty room, and a single answer could not
say so.

**Presence lives on the client, not the server.** A screen needs
nobody in front of it, so a typed client marks its copy said the
moment it arrives. A voice client knows its own room -- and only it
can -- so it says the announcement or leaves its copy pending and
collects it when the watch comes back. The server keeps no opinion
about rooms.

The server answers the one question a client cannot, because it is
about a moment rather than a device: **has this gone past being
worth saying at all.** Without that, a voice client switched off for
a week comes back to a hundred stale warnings.

| kind | nobody there | worth saying for | tries |
|---|---|---|---|
| reminder | held | 1 hour | 5 |
| coming | said anyway, it reaches the phone | 6 hours | 5 |
| immediate | dropped, a device meant *now* | 5 minutes | 3 |
| presence | dropped | 20 seconds | 1 |
| renamed | said anyway, never aloud | 1 hour | 1 |

`GET /v1/announcements` is what a client collects; `POST
/v1/announcements/{id}` is how it says what it did.

Only **situations** publishes this way so far, and **no client
collects yet** -- so situations are recorded and not heard. The other
five watchers still speak the old way. Both halves are being moved
over one at a time.

---

## 4. A reminder, end to end

The piece with the most moving parts, so it gets its own picture.

```mermaid
flowchart TB
    SET["They ask for a reminder"] --> STORE[("Stored as UTC")]
    STORE --> MK{"Does it repeat?"}

    MK -- "daily, weekdays, weekly" --> NATIVE["Alarm set with its days<br/>the phone holds the repeat itself"]
    MK -- "monthly, or once" --> ONEOFF["Alarm set for<br/>the next occurrence only"]

    NATIVE --> PH["Phone<br/>rounded up to the minute,<br/>signed 'by Jarvis'"]
    ONEOFF --> PH
    PH --> OK{"Did the<br/>phone take it?"}
    OK -- "yes" --> SAID["Said in passing"]
    OK -- "no" --> KEEP["Told plainly, and retried<br/>every 2 min until its time"]

    STORE --> DUE["Its time comes"]
    DUE --> LEAD["Picked up 4s early,<br/>so the voice lands on the moment"]
    LEAD --> HERE{"In the room?"}
    HERE -- "yes" --> SAY["Said aloud"]
    HERE -- "no, and one-off" --> HELD["Held until they return"]
    DUE --> REARM["Monthly only:<br/>next alarm armed"]
```

**Cancelling or moving one** takes its alarm with it — unless the
alarm repeats, which Android gives no way to remove from outside.
Then Jarvis says so rather than leaving it to wake them for
something called off.

---

## 5. What it does quietly

Nothing here is ever heard. It is the work that makes the rest
possible.

| every | what |
|---|---|
| a day | **Profile** — a standing description of the person, in prose, from what they said and did |
| a day | **Vocabulary** — the proper nouns they actually say, fed to both speech engines |
| a day | **Forgetting** — raw position readings older than a week |
| always | **Events** — the phone reports what it noticed: battery, calls, apps, music, screen, places |
| always | **Places** — raw fixes settled into arrivals, stays and departures |

---

## 6. What is known to be missing

Kept here so it is not rediscovered.

- No client collects its announcements yet, so what situations
  publish is written down and never said. The next step, and the
  one that matters most.
- Five of the six watchers still speak directly rather than
  publishing.
- `remark` is a kind nothing can raise any more: the branch that
  chose it is unreachable since unasked remarks were removed.
- The phone answers before it acts, so a successful call is not
  proof the alarm exists.
- A repeating phone alarm cannot be cancelled or moved from here.
- `place.entered home` never fires, and some stay durations are wrong.
- The satellite goes deaf after the laptop suspends.
