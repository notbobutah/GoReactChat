-- The last news digest, so a restart does not trigger a fresh scan.
--
-- Single row by construction: the digest is a cache of the current state, not a
-- history, and keeping only the latest is what makes "have we scanned recently"
-- a lookup rather than an aggregate. The CHECK is what enforces that — without
-- it, a second row would silently make the restart guard non-deterministic.
CREATE TABLE IF NOT EXISTS lumi.news_digest (
    id           smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    digest_id    text        NOT NULL,
    generated_at timestamptz NOT NULL,
    tool_calls   integer     NOT NULL DEFAULT 0,
    total_tokens bigint      NOT NULL DEFAULT 0,
    -- The items as the agent returned them. JSONB rather than a child table:
    -- nothing queries inside a digest, it is read and written whole, and a
    -- schema migration for a field the agent adds would be pure ceremony.
    items        jsonb       NOT NULL,
    updated_at   timestamptz NOT NULL DEFAULT now()
);
