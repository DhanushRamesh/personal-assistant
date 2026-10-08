-- +goose Up
-- Whether what a rule says is worth following them out of the room.
--
-- Owner, 8 October 2026, phone at four percent and nothing said:
-- *"battery is 4% its not announced..why"*
--
-- The rule had fired and written a good sentence -- "the battery is
-- still dropping, sir, nine percent now, and if you are heading out
-- this morning you will want to charge it before you go" -- and it
-- went stale five minutes later, unsaid, on both clients. Nobody was
-- in the room.
--
-- Which is the one place that warning is no use. The phone shows its
-- own percentage; what the rule adds is the diary, and the diary
-- matters most to somebody already on their way out. The kind it
-- fires as is "immediate", and immediate means the room, decided for
-- good reasons that hold for almost everything else it covers.
--
-- So it is the rule that says, not the kind -- the same answer as
-- keep in 00044, to the same shape of question. Off unless asked
-- for: saying it anywhere means the words leaving the house through
-- a push service, which nothing else here does.

-- +goose StatementBegin
ALTER TABLE jobs
    ADD COLUMN anywhere TINYINT(1) NOT NULL DEFAULT 0;
-- +goose StatementEnd

-- +goose Down
ALTER TABLE jobs DROP COLUMN anywhere;
