-- +goose Up
-- A person's standing permission to reach their own Google account.
--
-- One row per person: a second Google account would need a way to say
-- which is meant, and there is no call for that yet.
--
-- The refresh token is the whole of it. Everything else is either for
-- showing on a screen or for noticing that something has quietly
-- stopped: a refresh token dies for reasons invisible from here -- the
-- password changed, six months passed unused, it was revoked from a
-- settings page -- and without somewhere to record that, every tool
-- that needs it fails separately and says nothing useful.
--
-- subject is Google's own permanent identifier. An address can change
-- hands and cannot be compared on; this can, which is what makes
-- granting a different account by mistake something that can be caught.
CREATE TABLE google_accounts (
    user_id       CHAR(30)     NOT NULL,
    email         VARCHAR(320) NOT NULL,
    subject       VARCHAR(255) NOT NULL,
    refresh_token TEXT         NOT NULL,
    scopes        TEXT         NOT NULL,
    connected_at  DATETIME(3)  NOT NULL,
    refreshed_at  DATETIME(3)  NULL,
    broken_at     DATETIME(3)  NULL,
    broken        TEXT         NULL,

    PRIMARY KEY (user_id),
    CONSTRAINT fk_google_accounts_user
        FOREIGN KEY (user_id) REFERENCES users (id)
        ON DELETE CASCADE
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4;

-- +goose Down
DROP TABLE google_accounts;
