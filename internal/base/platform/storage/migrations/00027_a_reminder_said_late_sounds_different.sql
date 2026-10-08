-- +goose Up
-- What a reminder says when it is already too late to act on it.
--
-- "Time to take your tablets" heard at noon sounds like now. The server
-- can say when it was due -- that is arithmetic -- but it cannot turn
-- "take your tablets" into "you should have taken your tablets", which
-- is grammar. So the model writes the past form when the reminder is
-- set, and the server puts the hour in at the time.
--
-- Null for every reminder made before this and for any the model leaves
-- out, and those fall back to prefixing the body with when it was due.
ALTER TABLE reminders
    ADD COLUMN said_late VARCHAR(500) NULL AFTER body;

-- +goose Down
ALTER TABLE reminders
    DROP COLUMN said_late;
