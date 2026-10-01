-- +goose Up
-- What happened to the person, as their own devices saw it.
--
-- The point of this table is that the assistant stops waiting to be asked.
-- It cannot notice that somebody got home late, or has not moved all
-- afternoon, or missed the same call three times, unless something writes
-- those down as they happen.
--
-- Written by the phone directly and not through Home Assistant. Home
-- Assistant is the voice and the hands; routing what the person does
-- through it would tie the memory to the room, and the memory is about to
-- live on a different machine from the room.
--
-- kind is a dotted string rather than a foreign key to a table of kinds,
-- and payload is JSON rather than columns. A new thing worth noticing --
-- a train ticket scanned, a medicine taken -- must cost a profile on the
-- phone and nothing here. The moment adding one needs a migration, the
-- person stops adding them.
--
-- Two clocks, because they disagree and the difference is information.
-- occurred_at is the phone's: when it happened to the person. received_at
-- is ours: when we found out. The phone spends hours unable to reach
-- anything -- underground, abroad, asleep -- and then arrives with a day
-- in one batch. With one timestamp that day collapses into the minute it
-- was uploaded, and "you were out late" becomes unanswerable.
--
-- dedupe_key is unique per person because the phone retries. A duplicate
-- is not a cosmetic problem for an assistant that volunteers things: it is
-- being told the same thing twice.
CREATE TABLE events (
    id          CHAR(30)    NOT NULL,
    user_id     CHAR(30)    NOT NULL,
    -- Which kind of thing reported this: tasker, watch, laptop.
    source      VARCHAR(40) NOT NULL,
    -- Which particular one, when there is more than one of a kind.
    -- Empty while there is only a phone, and a column rather than a
    -- promise to add it later, because backfilling which device a year
    -- of rows came from is not possible.
    device      VARCHAR(60) NOT NULL DEFAULT '',
    kind        VARCHAR(60) NOT NULL,
    occurred_at DATETIME(3) NOT NULL,
    received_at DATETIME(3) NOT NULL,
    payload     JSON        NOT NULL,
    dedupe_key  VARCHAR(80) NOT NULL,
    PRIMARY KEY (id),
    -- Per person, not global: two people's phones may count from one.
    UNIQUE KEY uq_events_dedupe (user_id, dedupe_key),
    -- "What happened today."
    KEY idx_events_when (user_id, occurred_at),
    -- "When did I last go to the gym."
    KEY idx_events_kind (user_id, kind, occurred_at),
    CONSTRAINT fk_events_user FOREIGN KEY (user_id)
        REFERENCES users (id) ON DELETE CASCADE
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

-- +goose Down
DROP TABLE events;
