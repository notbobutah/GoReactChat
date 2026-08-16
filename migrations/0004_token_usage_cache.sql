-- Prompt-caching components of a model call.
--
-- With caching on, the provider reports cached prefix tokens separately and
-- EXCLUDES them from input_tokens. Without these columns the existing sum would
-- silently stop counting the bulk of a cached prompt, and the spend cap would
-- quietly become a much weaker cap than it reads as — the failure mode being
-- that nothing looks wrong.
--
-- Stored raw. The budget counts a billable equivalent (a cached read is charged
-- at a tenth, writing the cache at a premium), and keeping the components makes
-- that figure checkable rather than asserted.
ALTER TABLE lumi.token_usage
    ADD COLUMN IF NOT EXISTS cache_creation_input_tokens bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS cache_read_input_tokens     bigint NOT NULL DEFAULT 0;
