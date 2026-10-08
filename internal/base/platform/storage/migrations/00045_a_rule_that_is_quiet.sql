-- +goose Up
-- When a rule is to stay quiet until.
--
-- Two things wanted the same column and neither is worth one of its
-- own.
--
-- A person putting a rule aside for a week: *"can't pause"* was the
-- limitation, and cancelling is the only thing that existed, which
-- loses the rule. Being quiet is not being cancelled -- it comes
-- back by itself, and nobody has to remember how it was written.
--
-- And a rule holding off after it has spoken. The only thing
-- stopping a rule repeating itself was the exact sentence it said
-- last time, so the same news in different words went through. A
-- rule that has just said something can be quiet for an hour, which
-- is the blunt instrument the sentence comparison is the sharp one
-- for; the two work together and neither replaces the other.
--
-- Null is a rule that runs. A time in the past is the same thing:
-- nothing sweeps this, because a comparison against now costs
-- nothing and a sweeper is one more thing to go wrong at three in
-- the morning.
ALTER TABLE jobs
    ADD COLUMN quiet_until DATETIME(3) NULL;

-- How long to stay quiet after speaking, as it was written.
--
-- As written, like the schedule and keep beside it, because it is
-- read back to the person who said it and "1h" is what they said.
-- Empty is a rule that may speak as often as it has something to
-- say, which is every rule written before today.
ALTER TABLE jobs
    ADD COLUMN cool VARCHAR(32) NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE jobs DROP COLUMN quiet_until;
ALTER TABLE jobs DROP COLUMN cool;
