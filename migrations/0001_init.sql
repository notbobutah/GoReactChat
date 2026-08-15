-- lumi-go chat schema.
--
-- Deliberately its own schema rather than reusing lumi-neo's `assist.*` tables:
-- both services can run side by side without a bug in one writer corrupting the
-- other's live data. Column shape mirrors assist.assist_conversations closely
-- enough that a later backfill is a straight INSERT ... SELECT.

CREATE SCHEMA IF NOT EXISTS lumi;

-- gen_random_uuid() lives in pgcrypto on older servers; built in from PG 13.
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS lumi.conversations (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         text        NOT NULL,
    workspace_id    text        NOT NULL,
    project_id      text,
    title           text,
    status          text        NOT NULL DEFAULT 'active',
    persona_mode    boolean     NOT NULL DEFAULT false,
    message_count   integer     NOT NULL DEFAULT 0,
    last_message_at timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

-- The left rail reads (user, workspace) ordered by recency; this index is the
-- one that query plans against.
CREATE INDEX IF NOT EXISTS conversations_scope_recent_idx
    ON lumi.conversations (user_id, workspace_id, last_message_at DESC NULLS LAST);

CREATE INDEX IF NOT EXISTS conversations_project_idx
    ON lumi.conversations (workspace_id, project_id)
    WHERE project_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS lumi.messages (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id uuid        NOT NULL REFERENCES lumi.conversations (id) ON DELETE CASCADE,
    role            text        NOT NULL CHECK (role IN ('user', 'assistant')),
    content         text        NOT NULL,
    -- Canvas blocks emitted during the turn, so a reload reproduces the layout.
    metadata        jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS messages_conversation_idx
    ON lumi.messages (conversation_id, created_at);
