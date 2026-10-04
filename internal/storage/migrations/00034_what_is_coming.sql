-- +goose Up
-- What was decided about each entry in the diary, and how much of it
-- has been said.
--
-- The diary is read constantly and never speaks first. Giving it a
-- voice needs a judgement that no offset can make: the owner's own
-- words are that a film is fine a few hours out, while a wedding has
-- to be said days before, because there is a present to buy and a
-- shirt to iron. So the lead time depends on how much doing there is
-- beforehand, which a model reads out of the entry.
--
-- Kept rather than recomputed. The judgement does not change between
-- one tick and the next, and asking again every five minutes would
-- be three hundred model calls a day to be told the same thing --
-- most often that nothing is owed at all, which is why the silent
-- answers are kept too.
--
-- One row per entry in the next three weeks, perhaps a hundred at a
-- time, dropped as entries pass or leave the diary. The table never
-- grows.
CREATE TABLE coming (
    id       CHAR(30)     NOT NULL,
    user_id  CHAR(30)     NOT NULL,
    -- Google's identifier for the entry.
    event_id VARCHAR(255) NOT NULL,
    -- What the entry was when it was judged: the title and the start.
    -- A changed shape throws the judgement away and asks again,
    -- because "dinner" becoming "dinner with the Raos at their place"
    -- is a different amount of doing.
    shape    VARCHAR(400) NOT NULL,
    -- Enough to say the sentence without reading the diary again.
    title    VARCHAR(400) NOT NULL,
    starts   DATETIME(3)  NOT NULL,
    -- The moments worth a word, longest warning first, as the model
    -- gave them: how long before, and what there is to do by then.
    -- JSON because the number of them is part of what is judged -- a
    -- film earns one, a wedding two -- and a column cannot be a
    -- variable number of anything.
    leads    JSON         NOT NULL,
    -- How many have been spoken. A count rather than a set, because
    -- they are said in order and a count cannot disagree with itself.
    said     TINYINT      NOT NULL DEFAULT 0,
    -- When the judgement was made.
    at       DATETIME(3)  NOT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY idx_coming_event (user_id, event_id),
    KEY idx_coming_starts (user_id, starts),
    CONSTRAINT fk_coming_user FOREIGN KEY (user_id)
        REFERENCES users (id) ON DELETE CASCADE
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

-- +goose Down
DROP TABLE coming;
