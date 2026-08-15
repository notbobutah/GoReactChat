// Package store owns conversation + message persistence.
//
// Every method takes a Scope and is tenant-scoped in SQL: a conversation
// belonging to another (user, workspace) reads as absent and writes nothing.
// The scope always comes from the verified auth context, never from a
// client-supplied field.
package store

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound is returned when a conversation does not exist in the given
// scope — whether because it was never created or because it belongs to
// another tenant. The two cases are deliberately indistinguishable to callers.
var ErrNotFound = errors.New("conversation not found")

// Scope is the verified tenant identity for a request.
type Scope struct {
	UserID      string
	WorkspaceID string
}

// Conversation is the header row backing a chat thread.
type Conversation struct {
	ID            string
	Title         string
	Status        string // "active" | "archived"
	ProjectID     string
	PersonaMode   bool
	MessageCount  int
	LastMessageAt time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Block is a canvas block persisted alongside an assistant message, so a
// resumed conversation reproduces the layout the user originally saw.
type Block struct {
	Kind     string         `json:"kind"`
	ID       string         `json:"id"`
	Rendered string         `json:"rendered"`
	Data     map[string]any `json:"data,omitempty"`
}

// Message is one persisted conversation message.
type Message struct {
	ID             string
	ConversationID string
	Role           string // "user" | "assistant"
	Content        string
	Blocks         []Block
	CreatedAt      time.Time
}

// NewMessage is the write shape for AppendMessage.
type NewMessage struct {
	Role    string
	Content string
	Blocks  []Block
}

// CreateOptions carries the fields only settable at creation time.
type CreateOptions struct {
	// Stamped onto the row on create only. An existing conversation keeps its
	// own project id — only an explicit move changes it.
	ProjectID string
}

// ConversationPatch is a sparse update. A nil field is left untouched.
type ConversationPatch struct {
	Title       *string
	Status      *string
	ProjectID   *string
	PersonaMode *bool
}

// Store is the persistence seam. Postgres backs production; the in-memory
// implementation backs tests and `STORE=memory` local runs.
type Store interface {
	// GetConversation returns nil (no error) when the conversation does not
	// exist in scope.
	GetConversation(ctx context.Context, id string, scope Scope) (*Conversation, error)
	// CreateConversation is idempotent: a second call for the same id in the
	// same scope returns the existing row rather than erroring.
	CreateConversation(ctx context.Context, id string, scope Scope, opts CreateOptions) (*Conversation, error)
	ListConversations(ctx context.Context, scope Scope, limit int, projectID string) ([]Conversation, error)
	GetMessages(ctx context.Context, conversationID string, scope Scope) ([]Message, error)
	AppendMessage(ctx context.Context, conversationID string, msg NewMessage, scope Scope) (*Message, error)
	UpdateConversation(ctx context.Context, id string, patch ConversationPatch, scope Scope) (*Conversation, error)
	// DeleteConversation hard-deletes the conversation and its messages,
	// returning the number of messages removed.
	DeleteConversation(ctx context.Context, id string, scope Scope) (int, error)
	Close()
}
