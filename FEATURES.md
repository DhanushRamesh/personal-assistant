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
    SAT["Voice<br/>satellite in the room"]
    PHONE["Phone<br/>Tasker"]
    WEB["Web client<br/>and HTTP API"]

    HA["Home Assistant<br/>wake word, speech to text, text to speech"]
    EVENTS["/v1/events<br/>what the phone noticed"]

    CORE["The core<br/>persona, memory, tool loop"]
    MODEL["Model<br/>PlatformAI"]
    DB[("MySQL<br/>reminders, memories, events,<br/>conversations, places, profile")]

    SAT --> HA --> CORE
    WEB --> CORE
    PHONE --> EVENTS --> DB
    PHONE -. "presence" .-> HA

    CORE <--> MODEL
    CORE <--> DB
    CORE --> TOOLS["52 tools<br/>see section 2"]

    CORE -- "an answer" --> HA
    UNASKED["Speaking unasked<br/>see section 3"] --> HA
    UNASKED --> PUSH["The phone<br/>alarms over the tailnet"]
    DB --> UNASKED
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
    subgraph W["Watchers"]
        direction TB
        RL["Reminders<br/>says one when its time comes"]
        GR["The greeting<br/>written at the door from<br/>what happened while they were out"]
        CO["The diary<br/>speaks first about what is<br/>coming that needs doing"]
        LV["Leave by<br/>when to set off, with<br/>a battery caution if low"]
        SIT["Situations<br/>repeated missed calls,<br/>charging but not rising"]
        NOT["Immediate<br/>an event marked urgent,<br/>the moment it lands"]
    end

    W --> PRES{"Are they<br/>in the room?"}
    PRES -- "away" --> HOLD["Held back,<br/>or not said at all"]
    PRES -- "here" --> SPEAK["Said aloud<br/>through the satellite"]

    RL --> ALARM["And a real alarm<br/>on the phone"]
```

**The rule underneath all of it:** nothing is said unless something
asked for it to be said. There is no chatter watcher, by decision —
only the six above.

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

- The phone answers before it acts, so a successful call is not
  proof the alarm exists.
- A repeating phone alarm cannot be cancelled or moved from here.
- `place.entered home` never fires, and some stay durations are wrong.
- The satellite goes deaf after the laptop suspends.
