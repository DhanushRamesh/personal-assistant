-- +goose Up
-- A schedule, and the sequence of tasks it runs.
--
-- Owner, 6 October 2026: *"I will give the AI when to do what, how
-- frequently to do what, so a timer control I should be able to
-- give, and the what might be an already available task function or
-- hand it off to AI to do a custom task. The custom task will be
-- just announce too, as it is also a task."*
--
-- And: *"we don't need separate reminders, because it will also be
-- a schedule timer with a task -- just to talk. To keep an alarm
-- too will be a task, because not just a single task, a sequence of
-- tasks can be done."*
--
-- What this is for is seven loops in Go, each watching one thing and
-- each ending in the same place: say something. Announcing stops
-- being the shape of the system and becomes one task among several,
-- which is what it always was. The proof was already in the code --
-- reminder_set says the words and sets a phone alarm, which is a two
-- step sequence with the steps written into a function.
--
-- Nothing is migrated onto this yet. It runs new rules only, and the
-- producers move over one at a time afterwards, reminders last,
-- because reminders are the feature most used and a new engine is
-- wrong in small ways for a week.
--
-- A handful of rows, read every two seconds by one index. It never
-- grows on its own: a row appears when somebody asks for one.
CREATE TABLE jobs (
    id       CHAR(30)     NOT NULL,
    user_id  CHAR(30)     NOT NULL,
    -- What to call it in a listing.
    name     VARCHAR(200) NOT NULL DEFAULT '',
    -- What it is in the person's own words -- reminder, rule.
    --
    -- So that somebody asking what reminders they have is not told
    -- they have fourteen jobs including a battery check. The engine
    -- never reads this; the listing does.
    kind     VARCHAR(32)  NOT NULL DEFAULT 'rule',

    -- When to run, as it was written: "every 5m", "at 08:00",
    -- "once 2026-10-07 06:40". Three forms and no more -- a cron
    -- expression is unreadable to somebody dictating it aloud,
    -- which is how these are meant to be made.
    --
    -- Kept as written rather than parsed into columns, because it
    -- is read back to the person who said it and "every 5m" is
    -- what they said.
    schedule VARCHAR(100) NOT NULL,
    -- When it next runs. The one thing the runner reads.
    next_at  DATETIME(3)  NULL,

    -- What to do, in order. JSON because the number of steps is
    -- part of what is being described -- a reminder has two, a
    -- check has one -- and a column cannot be a variable number of
    -- anything.
    --
    -- Linear. No branching, no nesting, no else: a step may stop
    -- the ones after it and that is the whole of the conditional.
    -- Tasker exists and Home Assistant exists, and a third
    -- automation language in this house would be a mistake.
    steps    JSON         NOT NULL,

    -- active, done or cancelled.
    state    VARCHAR(16)  NOT NULL DEFAULT 'active',
    -- When it last ran, and what went wrong if anything did.
    last_at  DATETIME(3)  NULL,
    why      VARCHAR(500) NOT NULL DEFAULT '',

    created_at DATETIME(3) NOT NULL,
    updated_at DATETIME(3) NOT NULL,
    PRIMARY KEY (id),
    -- What the runner asks every two seconds: what is due.
    KEY idx_jobs_due (state, next_at),
    -- And what a listing asks: this person's, newest first.
    KEY idx_jobs_user (user_id, kind, created_at),
    CONSTRAINT fk_jobs_user FOREIGN KEY (user_id)
        REFERENCES users (id) ON DELETE CASCADE
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

-- +goose Down
DROP TABLE jobs;
