-- +goose Up
-- What the assistant owes somebody to say, and how it ended.
--
-- Six watchers decide to speak, and each used to deliver its own
-- announcement and record that it had spoken afterwards. A failure
-- between deciding and speaking therefore left nothing behind: the
-- situation, the leave-by, the thing a device said to announce at
-- once were gone, and nothing anywhere knew they had been owed.
-- Only reminders behaved properly, because they alone had a
-- lifecycle in a table. This is that lifecycle, made the one way
-- anything gets said.
--
-- Written before anything is spoken, which is the whole point. A
-- record written afterwards cannot describe a failure, and the
-- failure is what the owner asked about: the server off over the
-- moment, the phone off, nobody in the room.
--
-- Rows are kept once they are settled rather than deleted. "Nothing
-- was announced" and "six things were owed and every one went
-- stale" look identical from an empty table, and the second is a
-- fault somebody should be able to find. Perhaps fifty rows a day,
-- so twenty thousand a year: small, but not nothing, and the daily
-- forgetting that drops old position readings is where these should
-- be dropped too once there is a reason to.
CREATE TABLE owed (
    id      CHAR(30)     NOT NULL,
    user_id CHAR(30)     NOT NULL,
    -- Which kind of announcement. Decides the audience, whether an
    -- empty room holds it or drops it, how long it stays worth
    -- saying and how often it may be tried -- all from one table in
    -- the code, so that adding a kind is one edit.
    kind    VARCHAR(32)  NOT NULL,
    -- What to say, settled before it is stored. A restart does not
    -- have to reconstruct the sentence, and a model that wrote one
    -- is not asked for it twice.
    say     VARCHAR(2000) NOT NULL,

    -- When it became worth saying.
    due     DATETIME(3)  NOT NULL,
    -- When it stops being worth saying, fixed at the moment the
    -- thing was true rather than worked out at delivery. Measured
    -- from whenever the server came back it would never expire at
    -- all, which is precisely the case this table exists for.
    expires DATETIME(3)  NOT NULL,

    -- owed, said, held, expired or dropped.
    state   VARCHAR(16)  NOT NULL,
    -- How many attempts have been made. The clock bounds the
    -- ordinary case; this bounds a thing that refuses every time.
    tries   TINYINT      NOT NULL DEFAULT 0,
    -- What went wrong, for somebody reading back why they never
    -- heard it. Empty when nothing did.
    why     VARCHAR(500) NOT NULL DEFAULT '',

    -- When it was last moved on.
    at      DATETIME(3)  NOT NULL,
    PRIMARY KEY (id),
    -- What the deliverer reads on every pass: this person's
    -- unfinished ones, oldest first.
    KEY idx_owed_outstanding (user_id, state, due),
    CONSTRAINT fk_owed_user FOREIGN KEY (user_id)
        REFERENCES users (id) ON DELETE CASCADE
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

-- +goose Down
DROP TABLE owed;
