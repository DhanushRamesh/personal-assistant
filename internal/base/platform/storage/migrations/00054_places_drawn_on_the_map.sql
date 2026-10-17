-- +goose Up
-- The places a person draws for themselves: a name, a centre and a
-- radius, kept here rather than as geofences in Tasker.
--
-- Owner, 16 October 2026: *"I can plot a point and set the radius and
-- name that as location .. it should be added as geofence and not from
-- Tasker"*. Until now a place existed only as the name a Tasker
-- crossing gave the stays inside its window; nothing kept where it was.
--
-- Who is inside where is not kept here. The latest place.entered or
-- place.exited the server wrote under a place's name is its state, so
-- the record and the state cannot disagree.
CREATE TABLE places (
    id       CHAR(30)    NOT NULL,
    user_id  CHAR(30)    NOT NULL,
    -- What a crossing reports, and what the stays inside are called.
    -- Unique per person whatever its case, by the collation.
    name     VARCHAR(60) NOT NULL,
    lat      DOUBLE      NOT NULL,
    lng      DOUBLE      NOT NULL,
    -- 50 m to 2 km.
    radius_m INT         NOT NULL,
    created_at DATETIME(3) NOT NULL,
    updated_at DATETIME(3) NOT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uq_places_name (user_id, name),
    CONSTRAINT fk_places_user FOREIGN KEY (user_id)
        REFERENCES users (id) ON DELETE CASCADE
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

-- Whose Home and Office have been made from their stays already.
--
-- Once per person, and remembered: a Home deleted on purpose must not
-- come back the next time the places are listed.
CREATE TABLE places_seeded (
    user_id   CHAR(30)    NOT NULL,
    seeded_at DATETIME(3) NOT NULL,
    PRIMARY KEY (user_id),
    CONSTRAINT fk_places_seeded_user FOREIGN KEY (user_id)
        REFERENCES users (id) ON DELETE CASCADE
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

-- +goose Down
DROP TABLE places_seeded;
DROP TABLE places;
