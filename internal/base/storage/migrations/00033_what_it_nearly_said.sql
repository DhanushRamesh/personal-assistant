-- +goose Up
-- Every time the assistant considered saying something unasked, and
-- what came of it.
--
-- The feature's ordinary output is silence, and silence has four
-- causes: nothing arrived, nothing was unusual, the moment was
-- refused, or the model decided it was not worth saying. From the
-- outside all four look identical, and so does a watcher that has
-- stopped running. The owner asked to see it: when it triggered, why
-- it did not speak, and what it said when it did.
--
-- Only the considered ones. A tick that finds nothing writes nothing,
-- or this would be two and a half thousand rows a day saying that
-- nothing happened. What is recorded is the interesting minority: a
-- candidate was found, and here is what was done about it.
--
-- Perhaps fifty rows a day at the gate's current settings, most of
-- them refusals. Twenty thousand a year, which is nothing, and the
-- table is read newest-first by one person.
CREATE TABLE remarks (
    id      CHAR(30)    NOT NULL,
    user_id CHAR(30)    NOT NULL,
    -- When the thing it was about happened, not when this row was
    -- written: a remark held back and the event that prompted it are
    -- the same moment to the person reading this back.
    at      DATETIME(3) NOT NULL,
    -- The thing, as its own sender described it. Kind and value, the
    -- same shape the counted evidence uses.
    label   VARCHAR(160) NOT NULL,
    -- What made it worth looking at: often, novel.
    reason  VARCHAR(20)  NOT NULL,
    -- What came of it: said, held, declined, failed.
    outcome VARCHAR(20)  NOT NULL,
    -- What was spoken, when anything was. Null otherwise, which is
    -- most rows and is the point of keeping them.
    said    TEXT         NULL,
    -- Why it came to nothing, in the server's own words, for the rows
    -- where outcome alone does not say enough.
    because VARCHAR(255) NOT NULL DEFAULT '',
    PRIMARY KEY (id),
    KEY idx_remarks_when (user_id, at),
    CONSTRAINT fk_remarks_user FOREIGN KEY (user_id)
        REFERENCES users (id) ON DELETE CASCADE
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

-- +goose Down
DROP TABLE remarks;
