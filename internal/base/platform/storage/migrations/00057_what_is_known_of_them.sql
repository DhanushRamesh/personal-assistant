-- +goose Up
-- Everything known of the person, kept on disk and embedded, so what bears
-- on a moment is found by meaning rather than everything pushed into every
-- prompt (owner, 17 October 2026: *"everything should be recorded on disk
-- somewhere and the embedding model should bring my personality"*).
--
-- A source is where it came from -- their description, a memory, a space's
-- notes, Claude Code's conversations, a file -- and ref which one there.
-- Each is kept in chunks; a source's text is replaced whole when it
-- changes, its hash telling whether it has. One vector a chunk a model, as
-- message_vectors keeps them.
CREATE TABLE knowledge (
    user_id     CHAR(30)     NOT NULL,
    source      VARCHAR(20)  NOT NULL,
    ref         VARCHAR(255) NOT NULL,
    chunk       INT          NOT NULL,
    text        TEXT         NOT NULL,
    embedding   MEDIUMBLOB   NOT NULL,
    embed_model VARCHAR(128) NOT NULL,
    hash        CHAR(64)     NOT NULL,
    at          DATETIME(3)  NOT NULL,
    PRIMARY KEY (user_id, source, ref, chunk, embed_model),
    KEY idx_knowledge_search (user_id, embed_model)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

-- +goose Down
DROP TABLE knowledge;
