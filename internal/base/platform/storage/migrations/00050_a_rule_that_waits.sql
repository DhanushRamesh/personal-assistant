-- +goose Up
-- Where a rule got to, so it can be told to carry on.
--
-- Owner, on `perform` being a function: *"it should have the
-- function's basics, like if, else, conditionals, loop, sleep."*
-- Of those, waiting is the one that needs nothing else to mean
-- something -- "say it, give it five minutes, look again" is a
-- whole thought, where a branch is not until a condition can read
-- what the step before it returned.
--
-- Not a sleep. The runner is one pass over every rule that is due,
-- on one goroutine, so a rule that slept for five minutes would
-- stop every other rule for five minutes. It stops instead, and
-- this is how it knows where to start again: the index of the act
-- to run next, with next_at moved to the moment it should.
--
-- Zero is a rule that is not part-way through anything, which is
-- almost all of them almost all of the time.
ALTER TABLE jobs
    ADD COLUMN resume_at_step INT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE jobs DROP COLUMN resume_at_step;
