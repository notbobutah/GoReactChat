package store

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// MemoryStore is a process-local Store. It exists so the chat loop can be run
// and tested without Postgres (`STORE=memory`); it is not a production path —
// everything is lost on restart.
type MemoryStore struct {
	mu    sync.RWMutex
	convs map[string]*conversationRecord
	now   func() time.Time
}

type conversationRecord struct {
	conv     Conversation
	scope    Scope
	messages []Message
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{convs: make(map[string]*conversationRecord), now: time.Now}
}

func (s *MemoryStore) Close() {}

// get resolves a conversation in scope. Callers hold the lock.
func (s *MemoryStore) get(id string, scope Scope) *conversationRecord {
	rec, ok := s.convs[id]
	if !ok || rec.scope != scope {
		return nil
	}
	return rec
}

func (s *MemoryStore) GetConversation(_ context.Context, id string, scope Scope) (*Conversation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec := s.get(id, scope)
	if rec == nil {
		return nil, nil
	}
	conv := rec.conv
	return &conv, nil
}

func (s *MemoryStore) CreateConversation(_ context.Context, id string, scope Scope, opts CreateOptions) (*Conversation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if rec := s.get(id, scope); rec != nil {
		conv := rec.conv
		return &conv, nil
	}
	if _, taken := s.convs[id]; taken {
		// Same id in a foreign scope — indistinguishable from absent.
		return nil, ErrNotFound
	}

	now := s.now()
	rec := &conversationRecord{
		scope: scope,
		conv: Conversation{
			ID:        id,
			Status:    "active",
			ProjectID: opts.ProjectID,
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
	s.convs[id] = rec
	conv := rec.conv
	return &conv, nil
}

func (s *MemoryStore) ListConversations(_ context.Context, scope Scope, limit int, projectID string) ([]Conversation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Conversation, 0, len(s.convs))
	for _, rec := range s.convs {
		if rec.scope != scope || rec.conv.Status != "active" {
			continue
		}
		if projectID != "" && rec.conv.ProjectID != projectID {
			continue
		}
		out = append(out, rec.conv)
	}
	sort.Slice(out, func(i, j int) bool {
		return sortKey(out[i]).After(sortKey(out[j]))
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func sortKey(c Conversation) time.Time {
	if !c.LastMessageAt.IsZero() {
		return c.LastMessageAt
	}
	return c.CreatedAt
}

func (s *MemoryStore) GetMessages(_ context.Context, conversationID string, scope Scope) ([]Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec := s.get(conversationID, scope)
	if rec == nil {
		return nil, ErrNotFound
	}
	return append([]Message(nil), rec.messages...), nil
}

func (s *MemoryStore) AppendMessage(_ context.Context, conversationID string, msg NewMessage, scope Scope) (*Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec := s.get(conversationID, scope)
	if rec == nil {
		return nil, ErrNotFound
	}

	now := s.now()
	m := Message{
		ID:             uuid.NewString(),
		ConversationID: conversationID,
		Role:           msg.Role,
		Content:        msg.Content,
		Blocks:         msg.Blocks,
		CreatedAt:      now,
	}
	rec.messages = append(rec.messages, m)
	rec.conv.MessageCount++
	rec.conv.LastMessageAt = now
	rec.conv.UpdatedAt = now
	return &m, nil
}

func (s *MemoryStore) UpdateConversation(_ context.Context, id string, patch ConversationPatch, scope Scope) (*Conversation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec := s.get(id, scope)
	if rec == nil {
		return nil, ErrNotFound
	}
	if patch.Title != nil {
		rec.conv.Title = *patch.Title
	}
	if patch.Status != nil {
		rec.conv.Status = *patch.Status
	}
	if patch.ProjectID != nil {
		rec.conv.ProjectID = *patch.ProjectID
	}
	if patch.PersonaMode != nil {
		rec.conv.PersonaMode = *patch.PersonaMode
	}
	rec.conv.UpdatedAt = s.now()
	conv := rec.conv
	return &conv, nil
}

func (s *MemoryStore) DeleteConversation(_ context.Context, id string, scope Scope) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec := s.get(id, scope)
	if rec == nil {
		return 0, ErrNotFound
	}
	n := len(rec.messages)
	delete(s.convs, id)
	return n, nil
}
