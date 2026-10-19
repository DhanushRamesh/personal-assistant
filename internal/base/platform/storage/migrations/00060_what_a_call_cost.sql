-- +goose Up
-- What each model call cost, beside how many tokens it took.
--
-- Owner, 20 October 2026, on being shown that a turn is 65,755 tokens
-- in: *"in the client it shows the token usage per call, I think we
-- can update there"*. The usage dashboard said "tokens only: the
-- server keeps no prices", because none were known.
--
-- They were being sent all along. The gateway returns
-- zoho_total_credits and zoho_available_credits beside ai_stats, and
-- the client dropped them on the floor with the cache counts.
--
-- Credits rather than money: the gateway prices a call in its own
-- unit, the rate differs by model and by whether a token was cached,
-- and nothing here knows what a credit is worth. Taken as given.
--
-- available is the balance after this call, so the newest row is what
-- is left. Kept per row rather than once, since nothing else records
-- it and a row is the only place the figure was seen.
ALTER TABLE model_calls
    ADD COLUMN cache_read_tokens  INT           NOT NULL DEFAULT 0 AFTER output_tokens,
    ADD COLUMN cache_write_tokens INT           NOT NULL DEFAULT 0 AFTER cache_read_tokens,
    ADD COLUMN credits            DECIMAL(14,4) NOT NULL DEFAULT 0 AFTER cache_write_tokens,
    ADD COLUMN credits_left       DECIMAL(16,4) NOT NULL DEFAULT 0 AFTER credits;

-- +goose Down
ALTER TABLE model_calls
    DROP COLUMN cache_read_tokens,
    DROP COLUMN cache_write_tokens,
    DROP COLUMN credits,
    DROP COLUMN credits_left;
