-- +goose Up
-- Every request sent to a model, with what it cost.
--
-- Owner, 17 October 2026: *"whatever AI call is made .. we will be
-- getting input output token usage .. mark it and show in settings
-- page"*. The gateway says how many tokens each call took and that was
-- only logged.
--
-- One row per attempt, not per answer: a call retried after a dead
-- connection or a limiter is two rows, because both were sent. Written
-- at the one place every call goes through, so nothing that asks a
-- model can be missed.
--
-- feature is who asked -- chat, title, rule_writer and the rest -- and
-- not the request's purpose, which several callers share. detail is
-- anything narrower: the rule's name, "tools", "started early".
--
-- user_id may be null for a call nothing above it could attribute,
-- which is kept as feature "other" rather than not kept. The chat and
-- the conversation are not foreign keys: a conversation deleted keeps
-- what it cost, it only stops having a title.
--
-- Kept ninety days, forgotten by the daily pass.
CREATE TABLE model_calls (
    id              CHAR(30)    NOT NULL,
    user_id         CHAR(30)    NULL,
    feature         VARCHAR(40) NOT NULL,
    detail          VARCHAR(80) NULL,
    vendor          VARCHAR(40) NOT NULL DEFAULT '',
    model           VARCHAR(80) NOT NULL DEFAULT '',
    input_tokens    INT         NOT NULL DEFAULT 0,
    output_tokens   INT         NOT NULL DEFAULT 0,
    duration_ms     INT         NOT NULL DEFAULT 0,
    -- answered, tools, failed, stopped or unused.
    ended           VARCHAR(20) NOT NULL,
    -- Thirty-one, as chat_ and conv_ are a character longer than the
    -- rest.
    chat_id         CHAR(31)    NULL,
    conversation_id CHAR(31)    NULL,
    at              DATETIME(3) NOT NULL,
    PRIMARY KEY (id),
    KEY idx_model_calls_user (user_id, at),
    -- For the daily forgetting, which reads by time alone.
    KEY idx_model_calls_at (at),
    CONSTRAINT fk_model_calls_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- +goose Down
DROP TABLE model_calls;
