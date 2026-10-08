-- +goose Up
-- When a reminder's alarm was asked of the phone, so it is asked once.
--
-- An alarm holds an hour and a minute and no date. Asked for two days
-- ahead it rings at the next time that clock face comes round, which
-- is tomorrow, and the reminder it belongs to is still correctly on
-- the day after.
--
-- Owner, 8 October 2026, asking a third time why the Jailer alarm
-- could not be moved off tomorrow: nothing was wrong with it that
-- could be moved. The reminder was right, the phone was right, and
-- the alarm could not express the difference.
--
-- So an alarm is asked for only once the reminder is inside a day,
-- and a sweep asks for the ones that have come into range since the
-- last look. This column is what stops it asking twice: an alarm
-- asked for and asked for again is two alarms on the phone, and the
-- second cannot be told from the first.
--
-- Null for everything already written down, which is right. Those
-- were asked for at the time under the old rule; the sweep will not
-- touch them, because what it looks for is a reminder inside a day
-- that has never been asked for at all.

-- +goose StatementBegin
ALTER TABLE reminders
    ADD COLUMN alarm_asked_at DATETIME(6) NULL DEFAULT NULL;
-- +goose StatementEnd

-- +goose Down
ALTER TABLE reminders DROP COLUMN alarm_asked_at;
