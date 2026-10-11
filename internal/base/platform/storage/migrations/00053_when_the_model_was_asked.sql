-- +goose Up
-- Every request made to the model for an answer: when it went out, when
-- the first thing came back, when it ended and how.
--
-- The messages a turn writes record what the model asked for, and when
-- that was written down, but not when the request was sent or how long
-- the model took to reply. A timeline that shows only the tools leaves
-- the gaps between them unexplained, and the gaps are usually the model.
--
-- JSON rather than a table of its own, as recalled is: it is read with
-- the chat, a handful of entries a turn, and never queried on its own.
-- Null for every turn before this.
ALTER TABLE chats
    ADD COLUMN calls JSON NULL AFTER finished_at;

-- +goose Down
ALTER TABLE chats DROP COLUMN calls;
