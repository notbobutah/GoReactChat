-- Cumulative model token usage, so the service-wide budget survives a restart.
--
-- Append-only: one row per model call rather than a single mutable counter.
-- Costs a row per call and buys an audit trail — which turn spent what, and
-- when — for a table that will hold thousands of rows, not billions.

CREATE TABLE IF NOT EXISTS lumi.token_usage (
    id            bigserial PRIMARY KEY,
    input_tokens  bigint      NOT NULL CHECK (input_tokens >= 0),
    output_tokens bigint      NOT NULL CHECK (output_tokens >= 0),
    created_at    timestamptz NOT NULL DEFAULT now()
);

-- The only read is SUM(input + output) over the whole table at boot.
CREATE INDEX IF NOT EXISTS token_usage_created_idx ON lumi.token_usage (created_at);
