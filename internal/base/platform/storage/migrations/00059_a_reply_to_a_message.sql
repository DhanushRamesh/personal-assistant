-- +goose Up
-- Numbered 59: 57 and 58 were run on 17 October by work since taken back, and goose counts them as applied.
-- The message of the assistant's a chat replies to, when the person chose
-- one to reply to (owner, 19 October 2026: "add a reply to a message
-- option, where a reply is specifically for that message, and the server is
-- also told that it is for that message"). Its words as they were shown,
-- cut to 2 KB. Kept on the chat so the conversation shows what each reply
-- answered after a restart, and a chat recovered is answered as it was
-- asked. Null for a chat that replies to nothing chosen, and for every chat
-- before this.
ALTER TABLE chats
    ADD COLUMN reply_to TEXT NULL AFTER where_context;

-- +goose Down
ALTER TABLE chats
    DROP COLUMN reply_to;
