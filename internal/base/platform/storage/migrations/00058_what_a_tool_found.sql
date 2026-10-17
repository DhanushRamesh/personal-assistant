-- +goose Up
-- What a tool found, kept out of the conversation.
--
-- Owner, 17 October 2026: *"every time one single large prompt goes ..
-- that is why the tokens explode"*. Measured: tool results are 51% of
-- everything stored in conversations -- 581,023 characters against
-- 366,876 of speech -- and a conversation carries the last 60,000
-- bytes of itself on every turn. One `event_recent` came back with
-- "300 events today, as 298 entries once repeats are grouped", a
-- battery climbing 77 to 100 a line at a time, and that wall then rode
-- along in the history for the rest of the conversation.
--
-- So a result that is too big to be worth repeating is written here,
-- and the conversation keeps one line saying what it was and how to
-- read it again. Nothing is destroyed, which is what separates this
-- from summarising: the assistant can always come back for the whole
-- thing.
--
-- Scoped to the conversation it was found in, and kept as long as that
-- conversation is. Asked for after that, reading it says plainly that
-- it has gone rather than returning nothing, so the assistant knows to
-- ask the tool again instead of answering from silence.
CREATE TABLE found (
    id              CHAR(30)     NOT NULL,
    user_id         CHAR(30)     NOT NULL,
    conversation_id CHAR(31)     NOT NULL,
    -- The tool that found it, and the arguments it was given, so that
    -- asking the same thing twice can be recognised.
    tool            VARCHAR(64)  NOT NULL,
    arguments       TEXT         NULL,
    -- What the conversation keeps: one line, already worded.
    head            VARCHAR(500) NOT NULL,
    -- What it keeps instead of: the result as the tool wrote it.
    body            MEDIUMTEXT   NOT NULL,
    -- For saying how much was put aside without measuring it again.
    body_bytes      INT          NOT NULL DEFAULT 0,
    -- How many times the assistant came back for the whole thing,
    -- which is the measure of whether the head was good enough.
    -- Not "reads": MySQL keeps that one for READS SQL DATA, and the
    -- migration then fails at the next start rather than here.
    rereads         INT          NOT NULL DEFAULT 0,
    at              DATETIME(3)  NOT NULL,
    PRIMARY KEY (id),
    KEY idx_found_conversation (conversation_id, at),
    CONSTRAINT fk_found_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
    CONSTRAINT fk_found_conversation FOREIGN KEY (conversation_id) REFERENCES conversations (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- +goose Down
DROP TABLE found;
