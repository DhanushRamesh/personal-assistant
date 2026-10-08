-- +goose Up
-- What the server owes a device, kept until the device has it.
--
-- Owner, 6 October 2026: *"isolate the process where something from
-- server to phone is happening. It's a separate process and channel,
-- they should be decouplable."*
--
-- Until now a reminder tool reached the phone itself, inside the
-- model's turn: a POST over the tailnet, a 1.5 second sleep between
-- taking an alarm back and putting the next one on, and a retry queue
-- held in memory. Three faults in that, and the owner named the one
-- underneath them.
--
-- A reminder waited on a phone. The turn has thirty seconds and
-- nearly two of them went on a sleep that has nothing to do with
-- answering anybody.
--
-- And the retries were in memory, so restarting the server lost every
-- alarm still owed, silently, including the ones a person had already
-- been told would keep being tried.
--
-- So the two halves are separated. Setting a reminder writes down
-- what the phone should end up with; one sender gets it there, in
-- order, retrying, and survives a restart. Turning the sender off
-- stops alarms and changes nothing else.
--
-- Not alarm-specific on purpose. The kind says what is being asked
-- and the body carries whatever that kind needs, so the next thing
-- the server wants a device to do is a row rather than another path
-- out of the building.
CREATE TABLE errands (
    id      CHAR(30)    NOT NULL,
    user_id CHAR(30)    NOT NULL,
    -- What is being asked: alarm.set, alarm.clear.
    kind    VARCHAR(32) NOT NULL,
    -- What it concerns, so that later news about the same thing can
    -- cancel what is still waiting. A reminder's identifier, in
    -- practice: an alarm still queued for a time that has since
    -- moved should never arrive.
    about   VARCHAR(100) NOT NULL DEFAULT '',
    -- What to do, as the kind's own arguments.
    body    JSON        NOT NULL,

    -- Not before this. How ordering is kept without the sender
    -- sleeping: taking an alarm back and putting the next one on are
    -- two starts of the phone's clock activity, and back to back the
    -- second is dropped in silence. The gap is a column now rather
    -- than a sleep inside somebody's turn.
    after_at   DATETIME(3) NOT NULL,
    -- When it stops being worth doing. An alarm for a moment that
    -- has passed helps nobody.
    expires_at DATETIME(3) NOT NULL,

    -- waiting, done, expired, superseded or given up.
    state   VARCHAR(16) NOT NULL DEFAULT 'waiting',
    tries   TINYINT     NOT NULL DEFAULT 0,
    why     VARCHAR(500) NOT NULL DEFAULT '',

    created_at DATETIME(3) NOT NULL,
    updated_at DATETIME(3) NOT NULL,
    PRIMARY KEY (id),
    -- What the sender asks: what is owed and ready.
    KEY idx_errands_due (state, after_at),
    -- And what a change asks: what is still waiting about this thing.
    KEY idx_errands_about (user_id, about, state),
    CONSTRAINT fk_errands_user FOREIGN KEY (user_id)
        REFERENCES users (id) ON DELETE CASCADE
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

-- +goose Down
DROP TABLE errands;
