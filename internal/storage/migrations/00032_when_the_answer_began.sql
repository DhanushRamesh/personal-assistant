-- +goose Up
-- When the first piece of the answer went out.
--
-- A turn used to be described by one number: how long it took. That
-- number stopped meaning anything when the answer began being spoken as
-- it was written. The person stops waiting when the speaker starts, and
-- the turn runs on for seconds afterwards writing the rest of a sentence
-- they are already hearing. A turn that "took 7.2 seconds" was a 5.9
-- second wait.
--
-- A column rather than a number worked out from the messages, because
-- nothing in the messages records it: the answer is written down once,
-- whole, when it is finished. The moment it started existing is not
-- recoverable afterwards from anything we keep.
--
-- Null for every turn before this and for every turn that streams
-- nothing -- a failure, or a short answer on a channel with nobody
-- listening. Null means not measured, never zero.
ALTER TABLE chats
    ADD COLUMN answer_began_at DATETIME(3) NULL AFTER started_at;

-- +goose Down
ALTER TABLE chats DROP COLUMN answer_began_at;
