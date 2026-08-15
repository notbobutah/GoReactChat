// Package chat is one active conversation on one surface: it owns the turn —
// persist the user message, run the guarded streaming loop, translate
// orchestrator events into surface events, persist the assistant message.
//
// Ported from lumi-neo/src/surface/session.ts.
package chat

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/expona-ai/lumi-go/server/internal/orchestrator"
	"github.com/expona-ai/lumi-go/server/internal/store"
)

// EventType is the closed set a surface renders.
type EventType string

const (
	EventToken              EventType = "token"
	EventBlock              EventType = "block"
	EventDiscardBuffer      EventType = "discard_buffer"
	EventDone               EventType = "done"
	EventError              EventType = "error"
	EventConversationTitled EventType = "conversation_titled"
)

// Event is what a turn yields to its consumer. Which fields are meaningful
// depends on Type.
type Event struct {
	Type EventType

	Content  string // token
	Rendered string // token / block — the canonical markdown

	Block *orchestrator.CanvasBlock // block

	Code    string // error
	Message string // error

	ConversationID string // conversation_titled
	Title          string // conversation_titled
}

// Emit receives each surface event. An error aborts the turn — the client hung
// up, so there is nobody left to stream to.
type Emit func(Event) error

// Deps are the collaborators a Session needs. Everything here is an interface
// so a test can drive a full turn with fakes.
type Deps struct {
	Store       store.Store
	Client      orchestrator.StreamingClient
	RateLimiter orchestrator.RateLimiter
	ModelConfig orchestrator.ModelConfig
	// Optional. Absent → conversations keep the trimmed-first-message fallback
	// title rather than a generated one.
	TitleGenerator *TitleGenerator
	// Extra tools beyond the chat baseline. Capability tools land here.
	Tools []orchestrator.ToolDef
	// Overrides the default system prompt when set.
	SystemPrompt string
	Logger       *slog.Logger
}

// Session runs turns for one (scope, conversation) pair.
type Session struct {
	deps Deps
}

func NewSession(deps Deps) *Session {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	return &Session{deps: deps}
}

// SendInput is one inbound user turn.
type SendInput struct {
	ConversationID string
	Text           string
	// Applied only when this send creates the conversation row.
	ProjectID string
}

const defaultSystemPrompt = `You are Lumi, an AI intelligence layer for the Expona marketing platform.
Deliver something usable in seconds. Find what is missing. Cite evidence.

Do not narrate funnel numbers without a backing report or evidence block.
Do not claim something was saved unless a write actually happened.`

// Send runs one turn. Side effects, in order:
//
//	appends the user message (so it survives even if the model call fails)
//	kicks off auto-titling for a first message on an untitled conversation
//	streams the guarded loop, translating each event for the surface
//	persists the assistant text (skipped on error — a blocked or partial
//	  response must not enter canonical history)
func (s *Session) Send(ctx context.Context, scope store.Scope, in SendInput, emit Emit) error {
	st := s.deps.Store

	conv, err := st.GetConversation(ctx, in.ConversationID, scope)
	if err != nil {
		return fmt.Errorf("load conversation: %w", err)
	}
	if conv == nil {
		// Create-on-first-message: the panel mints a conversation id per mount,
		// so the first send arrives before any row exists. Created under the
		// VERIFIED scope — never a client-supplied tenant field.
		conv, err = st.CreateConversation(ctx, in.ConversationID, scope, store.CreateOptions{ProjectID: in.ProjectID})
		if err != nil {
			return fmt.Errorf("create conversation: %w", err)
		}
	}

	// Snapshot BEFORE appending — the append below bumps MessageCount, so
	// checking after would always read >= 1.
	untitledFirstMessage := conv.MessageCount == 0 && strings.TrimSpace(conv.Title) == ""

	if _, err := st.AppendMessage(ctx, in.ConversationID, store.NewMessage{
		Role: "user", Content: in.Text,
	}, scope); err != nil {
		return fmt.Errorf("append user message: %w", err)
	}

	// Auto-title in parallel with the turn. The fallback always succeeds, so
	// the row never stays blank even if the model call fails.
	var titleCh chan string
	if untitledFirstMessage {
		titleCh = make(chan string, 1)
		go func() {
			title := s.resolveTitle(ctx, in.Text)
			if _, err := st.UpdateConversation(ctx, in.ConversationID,
				store.ConversationPatch{Title: &title}, scope); err != nil {
				// The title still reaches the open client, so the rail updates
				// live; a later turn re-reads the untitled row and retries.
				// Never fail the turn over a title.
				s.deps.Logger.Warn("conversation title persist failed",
					"conversation_id", in.ConversationID, "error", err)
			}
			titleCh <- title
		}()
	}

	history, err := st.GetMessages(ctx, in.ConversationID, scope)
	if err != nil {
		return fmt.Errorf("load history: %w", err)
	}

	system := s.deps.SystemPrompt
	if system == "" {
		system = defaultSystemPrompt
	}

	var assistantText strings.Builder
	var blocks []store.Block
	errored := false

	loopErr := orchestrator.RunStreamingLoop(ctx, orchestrator.LoopOptions{
		Client:      s.deps.Client,
		RateLimiter: s.deps.RateLimiter,
		Scope:       orchestrator.RateKey{UserID: scope.UserID, WorkspaceID: scope.WorkspaceID},
		Tier:        orchestrator.TierStrong,
		ModelConfig: s.deps.ModelConfig,
		Messages:    toOrchestratorMessages(history),
		System:      system,
		Tools:       s.deps.Tools,
	}, func(ev orchestrator.Event) error {
		switch ev.Type {
		case orchestrator.EventTextDelta:
			assistantText.WriteString(ev.Text)
			return emit(Event{Type: EventToken, Content: ev.Text, Rendered: ev.Text})

		case orchestrator.EventDiscardBuffer:
			// The recall guard rail: the preamble already left the wire, so the
			// surface drops it and we drop it from what gets persisted too.
			assistantText.Reset()
			return emit(Event{Type: EventDiscardBuffer})

		case orchestrator.EventThinking:
			// Side-channel block: persisted alongside the assistant text so it
			// survives a reload, and shown live as a chip.
			if ev.Block != nil {
				blocks = append(blocks, store.Block{
					Kind: ev.Block.Kind, ID: ev.Block.ID, Rendered: ev.Block.Rendered, Data: ev.Block.Data,
				})
			}
			return emit(Event{Type: EventBlock, Block: ev.Block, Rendered: blockRendered(ev.Block)})

		case orchestrator.EventMessageEnd:
			// Apply the title just before the turn closes, so the rail row
			// updates with the same paint.
			if titleCh != nil {
				select {
				case title := <-titleCh:
					if err := emit(Event{
						Type: EventConversationTitled, ConversationID: in.ConversationID, Title: title,
					}); err != nil {
						return err
					}
				case <-ctx.Done():
				}
			}
			return emit(Event{Type: EventDone})

		case orchestrator.EventError:
			errored = true
			return emit(Event{Type: EventError, Code: ev.Code, Message: ev.Message})

		default:
			// tool_use / tool_result are silent at the surface — the blocks they
			// produce are the visible side effect.
			return nil
		}
	})
	if loopErr != nil {
		return loopErr
	}

	if errored || assistantText.Len() == 0 {
		return nil
	}

	if _, err := st.AppendMessage(ctx, in.ConversationID, store.NewMessage{
		Role:    "assistant",
		Content: assistantText.String(),
		Blocks:  blocks,
	}, scope); err != nil {
		// The user already saw the response; failing the RPC now would be
		// misleading. Log loudly — this is a real durability gap.
		s.deps.Logger.Error("assistant message persist failed",
			"conversation_id", in.ConversationID, "error", err)
	}
	return nil
}

func (s *Session) resolveTitle(ctx context.Context, firstMessage string) string {
	if s.deps.TitleGenerator != nil {
		if title, err := s.deps.TitleGenerator.Generate(ctx, firstMessage); err == nil && title != "" {
			return title
		} else if err != nil && !errors.Is(err, context.Canceled) {
			s.deps.Logger.Warn("title generation failed", "error", err)
		}
	}
	return FallbackTitle(firstMessage)
}

func blockRendered(b *orchestrator.CanvasBlock) string {
	if b == nil {
		return ""
	}
	return b.Rendered
}

// toOrchestratorMessages projects persisted history into the shape the model
// sees. Persisted canvas blocks are display state, not model context, so they
// are deliberately dropped here.
func toOrchestratorMessages(msgs []store.Message) []orchestrator.Message {
	out := make([]orchestrator.Message, 0, len(msgs))
	for _, m := range msgs {
		role := orchestrator.RoleUser
		if m.Role == "assistant" {
			role = orchestrator.RoleAssistant
		}
		if strings.TrimSpace(m.Content) == "" {
			continue
		}
		out = append(out, orchestrator.Message{
			Role:    role,
			Content: []orchestrator.ContentBlock{{Type: orchestrator.BlockText, Text: m.Content}},
		})
	}
	return out
}
