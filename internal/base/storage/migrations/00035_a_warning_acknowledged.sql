-- +goose Up
-- When a warning was last said, and when the person said they were
-- on top of it.
--
-- Owner: *"if I acknowledge it, it should act accordingly. 'Ok got
-- it' means no need of that reminder any more, because I'm already
-- on it. Or 'cancel that' and that reminder should go."*
--
-- Without this a warning given three days out says itself again on
-- the day, to somebody who bought the present the first time they
-- were told. Saying it twice to a person who has already acted is
-- the whole of what makes this sort of feature tiresome.
--
-- settled_at is deliberately not touched when a judgement is made
-- again. Re-judging happens when the entry is renamed or moved, or
-- when the question put to the model is reworded, and none of those
-- un-does somebody having said they were dealing with it.
ALTER TABLE coming
    -- When the last warning about this was spoken. Null until one is,
    -- which is most rows. Used to know which warning "cancel that"
    -- is about when nothing was named.
    ADD COLUMN spoke_at   DATETIME(3) NULL AFTER said,
    -- When they said they were on top of it. Null until they do.
    ADD COLUMN settled_at DATETIME(3) NULL AFTER spoke_at;

-- +goose Down
ALTER TABLE coming DROP COLUMN settled_at, DROP COLUMN spoke_at;
