-- +goose Up
-- How long what a rule says stays worth saying.
--
-- Owner, 7 October 2026, shown that eighteen announcements in a row
-- had expired before he was ever in the room to hear them: *"fix the
-- five minutes."*
--
-- One number was the wrong fix and the eighteen say so. Six were
-- battery warnings and three were the assistant naming a
-- conversation; a warning that the battery was at four percent, read
-- out at the door that evening, is worse than silence. Two were
-- missed calls from somebody the phone could not name, and those
-- were worth keeping for hours.
--
-- Nothing about the kind tells those apart -- they are all the same
-- kind. Whoever wrote the rule knows, so that is who is asked.
--
-- As written, like the schedule beside it, because it is read back
-- to the person who said it and "6h" is what they said. Empty is the
-- kind's own answer, which is what every rule written before today
-- meant.
ALTER TABLE jobs
    ADD COLUMN keep VARCHAR(32) NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE jobs DROP COLUMN keep;
