package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore is the production Store. Every statement carries the scope
// triple in its WHERE clause, so a cross-tenant id reads as absent and writes
// affect zero rows.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore dials the database and verifies connectivity before
// returning — a bad DATABASE_URL fails at boot, not on the first chat turn.
func NewPostgresStore(ctx context.Context, dsn string) (*PostgresStore, error) {
	pool, err := pgxpool.New(ctx, dsn)
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
