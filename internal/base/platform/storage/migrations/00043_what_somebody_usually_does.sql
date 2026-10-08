-- +goose Up
-- What somebody usually does, as something a condition can read.
--
-- Owner, 7 October 2026: *"I want the routine to be in a conditional
-- way and able to visualise. The reason I want it conditional is I
-- will use that to write the rules, and further I'll ask the AI to
-- write the rules by itself with examples of the existing rules, and
-- it should use the routine too."*
--
-- The standing description already says it -- "Returns home around
-- nine in the evening" -- in prose a model can read and a gate
-- cannot. This is the same answer as a number.
--
-- The counted half of the description is recomputed from the events
-- every rebuild and therefore needs no store. That argument holds
-- for a paragraph written once an hour and fails for a condition
-- read every two minutes, which is the whole reason this table
-- exists and the reason it holds nothing else.
--
-- A handful of rows a person: one per place per end of a visit.
-- Rewritten whole each time the description is, so it cannot drift
-- from what the description says and a place somebody stopped going
-- to stops answering conditions.
CREATE TABLE routines (
    id      CHAR(30)    NOT NULL,
    user_id CHAR(30)    NOT NULL,

    -- What it is about, by the name they gave it on their own
    -- device: home, office. Never a name the server chose.
    subject VARCHAR(100) NOT NULL,
    -- arrives or leaves.
    what    VARCHAR(16)  NOT NULL,

    -- The hour, 0 to 23, in their own zone. An hour and not a
    -- minute: it is the middle of a three hour band, so a minute
    -- would be precision the evidence does not have.
    hour    TINYINT      NOT NULL,

    -- How many days agreed, out of how many it was seen at all.
    -- Both, because together they are the whole of what makes this
    -- arguable: 4 of 7 says something that 4 does not.
    days    SMALLINT     NOT NULL,
    seen    SMALLINT     NOT NULL,

    -- When it was worked out.
    at         DATETIME(3) NOT NULL,
    created_at DATETIME(3) NOT NULL,
    updated_at DATETIME(3) NOT NULL,

    PRIMARY KEY (id),
    -- One row per thing. A second row for the same place and end
    -- would be two answers to one condition.
    UNIQUE KEY idx_routines_one (user_id, subject, what),
    CONSTRAINT fk_routines_user FOREIGN KEY (user_id)
        REFERENCES users (id) ON DELETE CASCADE
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

-- +goose Down
DROP TABLE routines;
