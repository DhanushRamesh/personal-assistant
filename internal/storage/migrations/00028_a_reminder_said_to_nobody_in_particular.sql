-- +goose Up
-- When a reminder was said without anything being able to confirm that
-- somebody was there to hear it.
--
-- Presence is fail-safe: anything short of a confident "they are
-- elsewhere" speaks, because speaking to an empty room is the cheaper
-- mistake. But that rule cannot tell "I know they are here" from "I have
-- no idea", and it treats both as here.
--
-- On 27 September 2026 it had no idea for twelve minutes -- Home
-- Assistant restarted while the watch was out of range, so the sensor
-- behind the flag did not exist and the flag held its last value. A
-- reminder was spoken into an empty room, recorded as said, and was
-- therefore not news when the owner walked back in. Nothing was lost by
-- the letter of the record and the reminder was lost in fact.
--
-- Null means it was said with somebody confirmed there, which is the
-- ordinary case. Set means it was said into a silence nobody could
-- vouch for, and it is worth raising at the door. Cleared by
-- mentioned_at, the same as a miss, so it is raised once.
ALTER TABLE reminders
    ADD COLUMN unwitnessed_at DATETIME(3) NULL AFTER mentioned_at;

-- +goose Down
ALTER TABLE reminders
    DROP COLUMN unwitnessed_at;
