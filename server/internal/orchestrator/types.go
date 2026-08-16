// Package orchestrator holds the seam between the model client and a surface:
// the conversation types the loop feeds the model, the streaming client
// interface, and the closed set of events the loop yields.
//
// Ported from lumi-neo/src/orchestrator/{types,models,streaming-loop}.ts.
package orchestrator

import (
	"context"
	"encoding/json"
)

// --- conversation content ---------------------------------------------------

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// BlockType discriminates a ContentBlock.
type BlockType string

const (
	BlockText       BlockType = "text"
	BlockToolUse    BlockType = "tool_use"
	BlockToolResult BlockType = "tool_result"
)

// ContentBlock is one block on a message. Which fields are meaningful depends
// on Type — text blocks use Text, tool_use blocks use ID/Name/Input, and
// tool_result blocks use ToolUseID/Content/IsError.
type ContentBlock struct {
	Type BlockType

	Text string

	ID    string
	Name  string
	Input json.RawMessage

	ToolUseID string
	Content   string
	IsError   bool
}

// Message is one turn of conversation history as the model sees it.
type Message struct {
	Role    Role
	Content []ContentBlock
}

// UserText builds a plain user turn.
func UserText(text string) Message {
	return Message{Role: RoleUser, Content: []ContentBlock{{Type: BlockText, Text: text}}}
}

// AssistantText builds a plain assistant turn.
func AssistantText(text string) Message {
	return Message{Role: RoleAssistant, Content: []ContentBlock{{Type: BlockText, Text: text}}}
}

// --- tools ------------------------------------------------------------------

// ToolDef is a capability the loop can call mid-stream.
//
// Recall flags a silent context loader (memory lookup, history fetch, doc
// read). Text streamed in the same round as a recall tool is treated as
// preamble and dropped via a discard_buffer event once the tool returns —
// otherwise the user sees the "let me check…" prelude AND the answer that
// restates it.
//
// Writes flags a tool that mutates a persistent store. The save-without-write
// guard reads it: an assistant claiming "saved!" with no write-flagged tool run
// is a fabrication.
type ToolDef struct {
	Name        string
	Description string
	// JSON Schema properties for the tool input, plus the required field names.
	Properties map[string]any
	Required   []string

	Recall bool
	Writes bool

	Execute func(ctx context.Context, input json.RawMessage) (string, error)
}

// --- model client seam ------------------------------------------------------

// StreamEventType discriminates a raw event from the model client.
type StreamEventType string

const (
	StreamText           StreamEventType = "text"
	StreamToolUseStart   StreamEventType = "tool_use_start"
	StreamToolInputDelta StreamEventType = "tool_input_delta"
	StreamToolUseStop    StreamEventType = "tool_use_stop"
	StreamMessageEnd     StreamEventType = "message_end"
)

// StreamEvent is the normalized shape the loop consumes, independent of which
// provider SDK produced it.
type StreamEvent struct {
	Type StreamEventType

	Delta string // text delta

	ID      string // tool_use id
	Name    string // tool name
	Partial string // partial tool input JSON

	StopReason string
	// Token usage for the completed call, on StreamMessageEnd. Cumulative for
	// the whole message, which is what the provider reports.
	Usage Usage
}

// Usage is what one model call consumed.
//
// The cache fields are not decoration. With prompt caching on, the provider
// reports cached prefix tokens SEPARATELY and excludes them from InputTokens —
// so anything that adds only InputTokens and OutputTokens silently stops
// counting the bulk of a cached prompt, and a spend cap quietly becomes a much
// weaker cap than it reads as.
type Usage struct {
	InputTokens  int64
	OutputTokens int64
	// CacheCreationInputTokens were written to the cache by this call.
	CacheCreationInputTokens int64
	// CacheReadInputTokens were served from the cache instead of reprocessed.
	CacheReadInputTokens int64
}

// Billing multipliers, relative to a base input token. These are the ratios
// Anthropic documents for prompt caching — a cached read is a tenth of the
// price, and writing the cache costs a premium over processing the same tokens
// once. They are ratios rather than prices, so they do not go stale when
// per-model pricing changes.
const (
	CacheWrite5mMultiplier = 1.25
	CacheReadMultiplier    = 0.10
)

// BillableInputTokens converts a cached call into the number of uncached input
// tokens it cost the equivalent of.
//
// The budget counts this rather than the raw sum, so that caching actually buys
// more conversation instead of merely making the meter read lower. Counting raw
// tokens would leave the cap exactly as tight as before while the real spend
// fell — technically safe, but it would waste the saving the cache exists to
// produce.
func (u Usage) BillableInputTokens() int64 {
	return u.InputTokens +
		int64(float64(u.CacheCreationInputTokens)*CacheWrite5mMultiplier) +
		int64(float64(u.CacheReadInputTokens)*CacheReadMultiplier)
}

// TotalRawTokens is every token the provider processed, cached or not. Used for
// reporting, never for the cap: it is the honest measure of work done, and the
// billable figure is the honest measure of what it cost.
func (u Usage) TotalRawTokens() int64 {
	return u.InputTokens + u.OutputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens
}

// StreamRequest is one model call.
type StreamRequest struct {
	Model     string
	System    string
	Messages  []Message
	Tools     []ToolDef
	MaxTokens int64
	// CacheSystem asks the provider to cache the tools+system prefix. Off by
	// default so a client that does not support caching is never handed a
	// request it cannot honour.
	CacheSystem bool
}

// Stream is an in-flight model response. Iterate with Next/Current, then check
// Err. Close releases the underlying HTTP response.
type Stream interface {
	Next() bool
	Current() StreamEvent
	Err() error
	Close() error
}

// TokenBudget caps total model spend across the whole service. The loop checks
// it before each call and reports usage after. Nil disables the cap.
type TokenBudget interface {
	Allow(ctx context.Context) error
	Record(ctx context.Context, u Usage)
}

// StreamingClient is the provider seam. Production wires the Anthropic client;
// tests wire a scripted fake.
type StreamingClient interface {
	Stream(ctx context.Context, req StreamRequest) (Stream, error)
}

// --- events the loop yields -------------------------------------------------

type EventType string

const (
	EventTextDelta     EventType = "text_delta"
	EventToolUse       EventType = "tool_use"
	EventToolResult    EventType = "tool_result"
	EventDiscardBuffer EventType = "discard_buffer"
	EventThinking      EventType = "thinking"
	EventMessageEnd    EventType = "message_end"
	EventError         EventType = "error"
)

// Error codes on EventError. Closed set — the surface routes on these.
const (
	CodeRateLimited    = "rate_limited"
	CodeToolError      = "tool_error"
	CodeModelError     = "model_error"
	CodeGuardViolation = "guard_violation"
	// CodeBudgetExhausted means the service-wide token cap is spent. Distinct
	// from rate limiting: waiting will not help.
	CodeBudgetExhausted = "budget_exhausted"
)

// CanvasBlock is a renderable affordance produced mid-turn (a thinking chip, a
// brief, a picker). Data carries the block payload as free-form JSON so a new
// block kind lands without touching the wire contract; Rendered is the
// canonical markdown every surface shows.
type CanvasBlock struct {
	Kind     string         `json:"kind"`
	ID       string         `json:"id"`
	Rendered string         `json:"rendered"`
	Data     map[string]any `json:"data,omitempty"`
}

// Event is what the loop yields. The set is closed; adding a variant means
// updating the surface translators in lockstep.
type Event struct {
	Type EventType

	Text string // text_delta

	ToolUseID string          // tool_use / tool_result
	ToolName  string          // tool_use
	Input     json.RawMessage // tool_use
	Result    string          // tool_result
	IsError   bool            // tool_result

	Block *CanvasBlock // thinking (and, later, guard_violation)

	StopReason string // message_end

	Code    string // error
	Message string // error
}
