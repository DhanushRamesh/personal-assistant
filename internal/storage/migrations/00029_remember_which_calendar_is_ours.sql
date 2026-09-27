-- +goose Up
-- Which calendar the assistant made for itself.
--
-- Remembered rather than discovered, because the permission that makes
-- this safe is also what stops it looking. calendar.app.created allows
-- making a calendar and doing anything to that one, and nothing else --
-- including CalendarList.list, which returns 403. There is no way to
-- ask Google "which of these is mine"; the only way to know is to have
-- written it down when it was made.
--
-- Null until one has been made. A calendar the person deletes by hand
-- is noticed as a 404 when it is next used, and a new one is made and
-- recorded in its place.
ALTER TABLE google_accounts
    ADD COLUMN calendar_id VARCHAR(255) NULL AFTER scopes;

-- +goose Down
ALTER TABLE google_accounts
    DROP COLUMN calendar_id;
