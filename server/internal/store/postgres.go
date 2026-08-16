package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/expona-ai/lumi-go/server/internal/newsagent"
	"github.com/expona-ai/lumi-go/server/internal/orchestrator"
)

// PostgresStore is the production Store. Every statement carries the scope
// triple in its WHERE clause, so a cross-tenant id reads as absent and writes
// affect zero rows.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// Pool limits. The defaults are tuned for a database on the other side of a
// LAN that is always awake; this one is neither.
//
// Neon compute scales to zero and its PgBouncer recycles connections, so a
// pooled connection that has sat idle can be dead while still looking usable.
// pgx retries, which is why nothing has visibly broken — but the failure that
// does surface is a first-query error after a quiet period, which is the
// hardest kind to reproduce. Bounding idle time and total lifetime replaces
// that with a reconnect nobody notices.
const (
	// maxConns is deliberately small. The API runs one replica by design (see
	// deploy/api.yaml), traffic is a handful of visitors, and every query here
	// is a short indexed read or a single insert. A large pool would only hold
	// idle connections open against a database billed for being awake.
	maxConns = 8
	// minConns is zero on purpose, against the instinct to keep one warm. The
	// pool's health check restores the pool to MinConns, so a non-zero minimum
	// combined with the idle timeout below would close a connection and
	// immediately re-dial it, forever — a heartbeat that would keep a
	// scale-to-zero compute awake and bill for the privilege of idling. The
	// price of zero is one cold dial on the first message after a quiet
	// period, paid by a visitor who is already waiting on a model call.
	minConns = 0
	// maxConnIdleTime is shorter than the intervals that matter here — nobody
	// is watching this page continuously — so idle connections are released
	// rather than held across the gaps.
	maxConnIdleTime = 5 * time.Minute
	// maxConnLifetime caps how long a connection can be reused even while busy,
	// so a pooler-side recycle is never discovered mid-query.
	maxConnLifetime = 30 * time.Minute
	// healthCheckPeriod is how often the pool prunes what the two limits above
	// have made stale.
	healthCheckPeriod = time.Minute
)

// NewPostgresStore dials the database and verifies connectivity before
// returning — a bad DATABASE_URL fails at boot, not on the first chat turn.
func NewPostgresStore(ctx context.Context, dsn string) (*PostgresStore, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	cfg.MaxConns = maxConns
	cfg.MinConns = minConns
	cfg.MaxConnIdleTime = maxConnIdleTime
	cfg.MaxConnLifetime = maxConnLifetime
	cfg.HealthCheckPeriod = healthCheckPeriod

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &PostgresStore{pool: pool}, nil
}

func (s *PostgresStore) Close() { s.pool.Close() }

const conversationColumns = `id, COALESCE(title, ''), status, COALESCE(project_id, ''), persona_mode,
	message_count, last_message_at, created_at, updated_at`

func scanConversation(row pgx.Row) (*Conversation, error) {
	var c Conversation
	var lastMessageAt *time.Time
	if err := row.Scan(&c.ID, &c.Title, &c.Status, &c.ProjectID, &c.PersonaMode,
		&c.MessageCount, &lastMessageAt, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, err
	}
	if lastMessageAt != nil {
		c.LastMessageAt = *lastMessageAt
	}
	return &c, nil
}

func (s *PostgresStore) GetConversation(ctx context.Context, id string, scope Scope) (*Conversation, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT `+conversationColumns+`
		FROM lumi.conversations
		WHERE id = $1 AND user_id = $2 AND workspace_id = $3
	`, id, scope.UserID, scope.WorkspaceID)

	conv, err := scanConversation(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get conversation: %w", err)
	}
	return conv, nil
}

// CreateConversation inserts, or returns the existing row when the id is
// already present in this scope. The ON CONFLICT DO NOTHING + scoped re-select
// keeps a double-send idempotent; a conflicting id owned by another tenant
// falls through to ErrNotFound rather than leaking its existence.
func (s *PostgresStore) CreateConversation(ctx context.Context, id string, scope Scope, opts CreateOptions) (*Conversation, error) {
	var projectID *string
	if opts.ProjectID != "" {
		projectID = &opts.ProjectID
	}

	if _, err := s.pool.Exec(ctx, `
		INSERT INTO lumi.conversations (id, user_id, workspace_id, project_id)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (id) DO NOTHING
	`, id, scope.UserID, scope.WorkspaceID, projectID); err != nil {
		return nil, fmt.Errorf("create conversation: %w", err)
	}

	conv, err := s.GetConversation(ctx, id, scope)
	if err != nil {
		return nil, err
	}
	if conv == nil {
		return nil, ErrNotFound
	}
	return conv, nil
}

func (s *PostgresStore) ListConversations(ctx context.Context, scope Scope, limit int, projectID string) ([]Conversation, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var project *string
	if projectID != "" {
		project = &projectID
	}

	rows, err := s.pool.Query(ctx, `
		SELECT `+conversationColumns+`
		FROM lumi.conversations
		WHERE user_id = $1 AND workspace_id = $2
		  AND status = 'active'
		  AND ($3::text IS NULL OR project_id = $3)
		ORDER BY COALESCE(last_message_at, created_at) DESC
		LIMIT $4
	`, scope.UserID, scope.WorkspaceID, project, limit)
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}
	defer rows.Close()

	out := make([]Conversation, 0, limit)
	for rows.Next() {
		conv, err := scanConversation(rows)
		if err != nil {
			return nil, fmt.Errorf("scan conversation: %w", err)
		}
		out = append(out, *conv)
	}
	return out, rows.Err()
}

// messageMetadata is the jsonb payload shape. Only `blocks` is used today;
// keeping it a struct means adding a field is additive on read.
type messageMetadata struct {
	Blocks []Block `json:"blocks,omitempty"`
}

func (s *PostgresStore) GetMessages(ctx context.Context, conversationID string, scope Scope) ([]Message, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT m.id, m.conversation_id, m.role, m.content, m.metadata, m.created_at
		FROM lumi.messages m
		JOIN lumi.conversations c ON c.id = m.conversation_id
		WHERE m.conversation_id = $1 AND c.user_id = $2 AND c.workspace_id = $3
		ORDER BY m.created_at ASC, m.id ASC
	`, conversationID, scope.UserID, scope.WorkspaceID)
	if err != nil {
		return nil, fmt.Errorf("get messages: %w", err)
	}
	defer rows.Close()

	out := []Message{}
	for rows.Next() {
		var m Message
		var raw []byte
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Role, &m.Content, &raw, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		if len(raw) > 0 {
			var meta messageMetadata
			// A malformed metadata blob must not break history replay — the
			// text is the load-bearing half.
			if err := json.Unmarshal(raw, &meta); err == nil {
				m.Blocks = meta.Blocks
			}
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// AppendMessage writes the message and bumps the conversation counters in one
// transaction, so message_count never drifts from the actual row count.
func (s *PostgresStore) AppendMessage(ctx context.Context, conversationID string, msg NewMessage, scope Scope) (*Message, error) {
	metaBytes, err := json.Marshal(messageMetadata{Blocks: msg.Blocks})
	if err != nil {
		return nil, fmt.Errorf("marshal message metadata: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// The scoped UPDATE is the authorization check: zero rows means the
	// conversation is absent or foreign, and we never touch the messages table.
	var updatedAt time.Time
	err = tx.QueryRow(ctx, `
		UPDATE lumi.conversations
		SET message_count = message_count + 1,
		    last_message_at = now(),
		    updated_at = now()
		WHERE id = $1 AND user_id = $2 AND workspace_id = $3
		RETURNING updated_at
	`, conversationID, scope.UserID, scope.WorkspaceID).Scan(&updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("bump conversation: %w", err)
	}

	var m Message
	if err := tx.QueryRow(ctx, `
		INSERT INTO lumi.messages (conversation_id, role, content, metadata)
		VALUES ($1, $2, $3, $4)
		RETURNING id, conversation_id, role, content, created_at
	`, conversationID, msg.Role, msg.Content, metaBytes).
		Scan(&m.ID, &m.ConversationID, &m.Role, &m.Content, &m.CreatedAt); err != nil {
		return nil, fmt.Errorf("insert message: %w", err)
	}
	m.Blocks = msg.Blocks

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit append: %w", err)
	}
	return &m, nil
}

func (s *PostgresStore) UpdateConversation(ctx context.Context, id string, patch ConversationPatch, scope Scope) (*Conversation, error) {
	row := s.pool.QueryRow(ctx, `
		UPDATE lumi.conversations
		SET title        = COALESCE($4, title),
		    status       = COALESCE($5, status),
		    project_id   = COALESCE($6, project_id),
		    persona_mode = COALESCE($7, persona_mode),
		    updated_at   = now()
		WHERE id = $1 AND user_id = $2 AND workspace_id = $3
		RETURNING `+conversationColumns+`
	`, id, scope.UserID, scope.WorkspaceID, patch.Title, patch.Status, patch.ProjectID, patch.PersonaMode)

	conv, err := scanConversation(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("update conversation: %w", err)
	}
	return conv, nil
}

func (s *PostgresStore) DeleteConversation(ctx context.Context, id string, scope Scope) (int, error) {
	// Messages cascade from the conversation delete; count them first so the
	// caller can report what was removed.
	var count int
	if err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*)::int
		FROM lumi.messages m
		JOIN lumi.conversations c ON c.id = m.conversation_id
		WHERE m.conversation_id = $1 AND c.user_id = $2 AND c.workspace_id = $3
	`, id, scope.UserID, scope.WorkspaceID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count messages: %w", err)
	}

	tag, err := s.pool.Exec(ctx, `
		DELETE FROM lumi.conversations
		WHERE id = $1 AND user_id = $2 AND workspace_id = $3
	`, id, scope.UserID, scope.WorkspaceID)
	if err != nil {
		return 0, fmt.Errorf("delete conversation: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return 0, ErrNotFound
	}
	return count, nil
}

// --- token usage -------------------------------------------------------------
// Backs the service-wide token budget (internal/budget). Kept on the store
// because it is persistence, not policy: the budget package decides what the
// numbers mean.

// TotalTokens returns the billable-equivalent total recorded so far. Called
// once at boot to restore the budget.
//
// The sum is computed from the stored components rather than read from a
// running total, so the multipliers live in exactly one place (the SQL mirrors
// orchestrator.Usage.BillableInputTokens) and a historical row can never carry
// a stale precomputed figure.
func (s *PostgresStore) TotalTokens(ctx context.Context) (int64, error) {
	var total int64
	if err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(
			input_tokens
			+ output_tokens
			+ (cache_creation_input_tokens * 1.25)
			+ (cache_read_input_tokens * 0.10)
		), 0)::bigint FROM lumi.token_usage
	`).Scan(&total); err != nil {
		return 0, fmt.Errorf("sum token usage: %w", err)
	}
	return total, nil
}

// RecordTokens appends the usage of one model call, cache components included.
func (s *PostgresStore) RecordTokens(ctx context.Context, u orchestrator.Usage) error {
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO lumi.token_usage
			(input_tokens, output_tokens, cache_creation_input_tokens, cache_read_input_tokens)
		VALUES ($1, $2, $3, $4)
	`, u.InputTokens, u.OutputTokens, u.CacheCreationInputTokens, u.CacheReadInputTokens); err != nil {
		return fmt.Errorf("insert token usage: %w", err)
	}
	return nil
}

// --- news digest --------------------------------------------------------------
// Backs the news watcher (internal/newswatch). Persisted for one reason: an
// agent that costs money per run must not rescan every time a pod restarts.

// LoadDigest returns the stored digest, or nil when none has been saved.
func (s *PostgresStore) LoadDigest(ctx context.Context) (*newsagent.Digest, error) {
	var (
		d     newsagent.Digest
		items []byte
	)
	err := s.pool.QueryRow(ctx, `
		SELECT digest_id, generated_at, tool_calls, total_tokens, items
		FROM lumi.news_digest WHERE id = 1
	`).Scan(&d.ID, &d.GeneratedAt, &d.ToolCalls, &d.TotalTokens, &items)
	if errors.Is(err, pgx.ErrNoRows) {
		// Never scanned. Not an error — the watcher starts cold and scans on
		// the first subscriber.
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load news digest: %w", err)
	}
	if err := json.Unmarshal(items, &d.Items); err != nil {
		return nil, fmt.Errorf("decode news digest items: %w", err)
	}
	// Ids are derived from the URL rather than stored, so a change to how they
	// are computed cannot leave stale ones in the database.
	for i := range d.Items {
		d.Items[i].ID = newsagent.ItemID(d.Items[i].URL)
	}
	return &d, nil
}

// SaveDigest replaces the stored digest.
func (s *PostgresStore) SaveDigest(ctx context.Context, d *newsagent.Digest) error {
	if d == nil {
		return nil
	}
	items, err := json.Marshal(d.Items)
	if err != nil {
		return fmt.Errorf("encode news digest items: %w", err)
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO lumi.news_digest (id, digest_id, generated_at, tool_calls, total_tokens, items, updated_at)
		VALUES (1, $1, $2, $3, $4, $5, now())
		ON CONFLICT (id) DO UPDATE SET
			digest_id    = EXCLUDED.digest_id,
			generated_at = EXCLUDED.generated_at,
			tool_calls   = EXCLUDED.tool_calls,
			total_tokens = EXCLUDED.total_tokens,
			items        = EXCLUDED.items,
			updated_at   = now()
	`, d.ID, d.GeneratedAt, d.ToolCalls, d.TotalTokens, items); err != nil {
		return fmt.Errorf("save news digest: %w", err)
	}
	return nil
}
