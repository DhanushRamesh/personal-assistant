-- +goose Up
-- What is left to do about something, once part of it has been done.
--
-- A warning carries the doing it is about: "a present to buy and
-- something to wear". Answer half of that -- "I've bought the
-- present" -- and the warning nearer the time still says both,
-- which is the same fault as saying it twice, one step smaller.
--
-- So an acknowledgement can be partial. The model says what is
-- still to do, in the person's own words, and the warnings not yet
-- given say that instead of what was first judged. The server never
-- reads either string; it only keeps the newer one.
--
-- Null for almost every row, which is the ordinary case: either
-- nothing was acknowledged, or all of it was and settled_at carries
-- that instead.
ALTER TABLE coming
    -- What is still to do, overriding the reason on the warnings not
    -- yet given. Survives a re-judgement for the same reason
    -- settled_at does: neither a rename nor a reworded question
    -- un-does somebody having already done part of it.
    ADD COLUMN still VARCHAR(255) NULL AFTER settled_at;

-- +goose Down
ALTER TABLE coming DROP COLUMN still;
