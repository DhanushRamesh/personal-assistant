-- +goose Up
-- A rule as a structure rather than as sentences to be parsed.
--
-- Owner, 9 October 2026: *"a rule object should be like {"when":
-- ..., "perform": ...}"*, and on a count written as a string inside
-- it: *"it's more string and an AI can make a mistake."*
--
-- The old shape kept the same grammar in two places -- a trigger in
-- the schedule, conditions in check steps -- and stored both as text
-- to be parsed on every pass. This stores the structure and renders
-- the text, which is the way round that suits whatever will be
-- writing most of these: a model can fill in keys, and cannot typo a
-- grammar that is not there.
--
-- Null is a rule that has not been converted. The old columns stay
-- and stay authoritative for those, so the engine reads one shape
-- while the rows arrive in two -- and a rule written before today
-- goes on working whether or not anybody converts it.
ALTER TABLE jobs
    ADD COLUMN `when` JSON NULL,
    ADD COLUMN perform JSON NULL;

-- +goose Down
ALTER TABLE jobs DROP COLUMN `when`, DROP COLUMN perform;
