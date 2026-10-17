-- +goose Up
-- What the person had in front of them in the client when they asked: the
-- view, the active space, the focused terminal and its last lines, as the
-- client wrote it, with secrets already hidden there.
--
-- Kept on the chat, as the channel and model are, so a chat recovered
-- after a restart is answered as it was asked. Cut to 8 KB before it is
-- stored, so TEXT holds it. Named where_context because WHERE is a word
-- MySQL keeps for itself. Null when the client said nothing, and for every
-- chat before this.
ALTER TABLE chats
    ADD COLUMN where_context TEXT NULL AFTER prompt;

-- +goose Down
ALTER TABLE chats
    DROP COLUMN where_context;
