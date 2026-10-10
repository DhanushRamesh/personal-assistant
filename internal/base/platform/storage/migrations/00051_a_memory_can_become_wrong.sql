-- +goose Up
-- When a memory stopped being true, and what replaced it.
--
-- Owner, on the one thing the description and the rules had and the
-- memories did not: *"old facts should be able to expire."*
--
-- Eighteen memories, none of which could ever become wrong. A fact
-- kept in September is read into every turn in October with exactly
-- the same weight, and there was no way for a later one to say it had
-- taken over. Four of them are about his music and two claim to name
-- a favourite song.
--
-- It is the same hole as a rule that cannot be retired and a sentence
-- that outlived the occasion it was about, and it is the worst of the
-- three: a wrong memory is read into every single answer.
--
-- Set rather than deleted. A memory that stopped being true is a
-- thing that happened, and asked why he no longer believes something
-- the answer is in the row. Null is a memory that still stands,
-- which is nearly all of them nearly always.
ALTER TABLE memories
    ADD COLUMN stale_at DATETIME(3) NULL,
    ADD COLUMN stale_why VARCHAR(500) NOT NULL DEFAULT '';

CREATE INDEX idx_memories_standing ON memories (user_id, stale_at);

-- +goose Down
DROP INDEX idx_memories_standing ON memories;
ALTER TABLE memories DROP COLUMN stale_at, DROP COLUMN stale_why;
