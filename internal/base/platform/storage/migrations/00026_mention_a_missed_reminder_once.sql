-- +goose Up
-- When a missed reminder was mentioned.
--
-- A reminder whose time passed while nothing could say it is marked missed
-- and then nothing happens: it is never spoken, it was not on the settings
-- screen, and no later conversation brought it up. It vanished.
--
-- Null means it has not been mentioned yet. It is set once, so the person
-- is told a thing was missed on the next turn and not on every turn after.
ALTER TABLE reminders
    ADD COLUMN mentioned_at DATETIME(3) NULL AFTER last_fired_at;

-- +goose Down
ALTER TABLE reminders
    DROP COLUMN mentioned_at;
