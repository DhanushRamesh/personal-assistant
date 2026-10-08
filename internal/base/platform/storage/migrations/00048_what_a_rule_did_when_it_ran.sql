-- +goose Up
-- Every run of every rule, step by step.
--
-- Owner, before converting sixteen live rules to a new shape:
-- *"these rules will get executed at some point right, we have to
-- collect the execution logs."* Converting without them would be
-- changing sixteen things at once and having no way to say whether
-- any of them still does what it did.
--
-- Not the remarks table, which is where things the assistant
-- considered saying are written. A rule that stopped at its first
-- condition never considered saying anything, and squeezing it in
-- there made every quiet tick look like a remark declined. That
-- table answers "what did it nearly say"; this one answers "what
-- did it do".
--
-- The name and the schedule are copied in as they were at the
-- moment, not left to be read off the rule. The whole point of
-- this table is to survive the rule being rewritten: a row saying
-- "anything amiss, every 2m, declined" is still true after that
-- rule has become two rules with other names, and a job_id joins
-- the two halves of its life.
CREATE TABLE runs (
    id          CHAR(30)     NOT NULL,
    user_id     CHAR(30)     NOT NULL,
    job_id      CHAR(30)     NOT NULL,
    -- As the rule was called and written at the time.
    name        VARCHAR(200) NOT NULL DEFAULT '',
    schedule    VARCHAR(100) NOT NULL DEFAULT '',
    at          DATETIME(3)  NOT NULL,
    -- How long the whole sequence took, so that a rule which got
    -- slower can be found rather than noticed.
    took_ms     INT          NOT NULL DEFAULT 0,
    outcome     VARCHAR(16)  NOT NULL,
    why         VARCHAR(500) NOT NULL DEFAULT '',
    said        TEXT,
    -- One entry per step: what it was, what it did, how long, and
    -- what it decided. The column that makes "why did the new one
    -- behave differently" answerable at all.
    steps       JSON         NOT NULL,
    PRIMARY KEY (id),
    KEY idx_runs_job (job_id, at),
    KEY idx_runs_when (user_id, at),
    CONSTRAINT fk_runs_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- +goose Down
DROP TABLE runs;
