-- +goose Up
-- Whether the server wrote a memory for itself.
--
-- The flag existed in Go and nowhere else. A memory the server writes
-- is allowed eight thousand characters and one a person asked to have
-- kept is allowed four, and which of the two a memory is was decided
-- by a field that no column backed: every memory read out of this
-- table came back as the person's, so the longer limit only ever
-- applied to a memory being created and never to one being rewritten.
--
-- The standing description is rewritten hourly and is the only memory
-- the server writes, so the longer limit applied to it exactly once,
-- on the first run, and never again. It had reached 4,013 characters
-- that way and every rebuild since has been refused for length --
-- including, had anyone tried, saving an edit of it by hand.
--
-- One boolean on a table of a few hundred rows.
ALTER TABLE memories
    ADD COLUMN written TINYINT(1) NOT NULL DEFAULT 0;

-- The one that exists. Named by its subject because that is what
-- identifies it in the server too -- profile.Subject -- and there is
-- no other mark on the row to find it by.
UPDATE memories
   SET written = 1
 WHERE subject = 'What I have noticed about them';

-- +goose Down
ALTER TABLE memories DROP COLUMN written;
