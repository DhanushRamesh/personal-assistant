-- +goose Up
-- The proper nouns somebody has said, for speech recognition to expect.
--
-- Not a memory, though it is written the same way and from the same
-- messages. A memory is for the model to read; this is for faster-whisper
-- and Deepgram, which never see a prompt. Putting it in the memories table
-- would mean a list of fifty names either sitting in every system prompt or
-- turning up as a search result whenever somebody is asked about.
--
-- Not the settings table either: that holds one short value per name and its
-- column is VARCHAR(255), which a list of names outgrows immediately.
--
-- One row per person, replaced wholesale each night, because the names
-- somebody uses are the names in their own conversations. What the
-- microphone is finally told is the union of every row: it cannot tell who
-- is speaking, so it has to expect all of them.
CREATE TABLE vocabularies (
    user_id    CHAR(30)    NOT NULL,
    terms      TEXT        NOT NULL,
    written_at DATETIME(3) NOT NULL,
    PRIMARY KEY (user_id),
    CONSTRAINT fk_vocabularies_user FOREIGN KEY (user_id)
        REFERENCES users (id) ON DELETE CASCADE
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

-- +goose Down
DROP TABLE vocabularies;
