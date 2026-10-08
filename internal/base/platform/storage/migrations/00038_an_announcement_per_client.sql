-- +goose Up
-- An announcement is published once and delivered to each client
-- separately.
--
-- Owner, 6 October 2026: *"a announcement has to be published to the
-- client, like a pub sub. Each client is subscribed to the
-- announcement ... the typed clients receive it, the voice client
-- also receives it but to say or not depends on the presence, and
-- presence logic should reside on the client. So for a typed client
-- presence is always true; for a voice client presence depends on
-- the trigger, my watch."*
--
-- What this fixes is an assumption that was wrong from the start:
-- that "was it announced" is one fact about one person. It is not.
-- The same warning can be read on a laptop and missed by a speaker
-- in an empty room, and until now the server held a single verdict
-- for both -- and decided it centrally, from a sensor that describes
-- one room and says nothing about where the laptop is.
--
-- So the state moves to where the knowledge is. The announcement
-- says what there is to say; the row below says what each client did
-- about it. A client that could not say it leaves its own row
-- pending and picks it up when it can, without affecting anybody
-- else's copy.
ALTER TABLE owed
    DROP COLUMN state,
    DROP COLUMN tries,
    DROP COLUMN why;

-- What each client did with one announcement.
--
-- One row per client per announcement, written when the announcement
-- is published rather than when it is delivered. A row that does not
-- exist cannot be asked "why did nobody hear this", which is the
-- whole reason the outbox exists.
--
-- Perhaps three clients and fifty announcements a day, so a hundred
-- and fifty rows a day and under sixty thousand a year. Dropped
-- alongside the announcements they belong to, by the daily
-- forgetting, once there is a reason to.
CREATE TABLE owed_clients (
    id        CHAR(30)    NOT NULL,
    owed_id   CHAR(30)    NOT NULL,
    client_id CHAR(30)    NOT NULL,
    -- pending until the client says otherwise, then said, or
    -- dropped when the client could not and will not.
    --
    -- Pending is the default rather than something the client has
    -- to report. A voice client in an empty room says nothing and
    -- writes nothing, and the announcement is still waiting for it
    -- when the watch comes back into range.
    state     VARCHAR(16) NOT NULL DEFAULT 'pending',
    -- How many times this client has tried.
    tries     TINYINT     NOT NULL DEFAULT 0,
    -- What the client said went wrong, in its own words. Empty when
    -- nothing did.
    why       VARCHAR(500) NOT NULL DEFAULT '',
    at        DATETIME(3) NOT NULL,
    PRIMARY KEY (id),
    -- A client asking what it still owes somebody, oldest first.
    KEY idx_owed_clients_pending (client_id, state),
    -- One row per client per announcement, enforced rather than
    -- trusted: a republished announcement must not give a client
    -- two copies of the same sentence.
    UNIQUE KEY idx_owed_clients_once (owed_id, client_id),
    CONSTRAINT fk_owed_clients_owed FOREIGN KEY (owed_id)
        REFERENCES owed (id) ON DELETE CASCADE,
    CONSTRAINT fk_owed_clients_client FOREIGN KEY (client_id)
        REFERENCES clients (id) ON DELETE CASCADE
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

-- +goose Down
DROP TABLE owed_clients;

ALTER TABLE owed
    ADD COLUMN state VARCHAR(16) NOT NULL,
    ADD COLUMN tries TINYINT NOT NULL DEFAULT 0,
    ADD COLUMN why VARCHAR(500) NOT NULL DEFAULT '';
