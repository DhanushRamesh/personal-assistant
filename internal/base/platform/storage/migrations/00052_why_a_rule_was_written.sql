-- +goose Up
-- Why a rule exists, kept apart from what it did last time it ran.
--
-- Owner, asking to tell his own rules from the ones written for him:
-- *"there should be a marker -- which rule is created by AI for its
-- purpose, and which is created by me."*
--
-- The marker was already there, in `kind`. What was not there was the
-- other half: a rule nobody asked for has to be able to say what it
-- is for, or the only thing to do with it is delete it.
--
-- That reason was being written into `why`, which `Ran` overwrites on
-- every single run with the outcome of that run -- a failed lookup, a
-- check that withheld, nothing at all. So the reason a rule was
-- written survived exactly until the first time it worked. Twenty-five
-- rules on the system and none of them could say why it existed.
--
-- Two different facts in one column: why it exists, which never
-- changes, and what happened last time, which changes every run. They
-- are separate now.
--
-- Empty for every rule the person asked for. They know why.
ALTER TABLE jobs
    ADD COLUMN because VARCHAR(500) NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE jobs DROP COLUMN because;
