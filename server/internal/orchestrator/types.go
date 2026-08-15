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
}

// StreamRequest is one model call.
type StreamRequest struct {
	Model     string
	System    string
	Messages  []Message
	Tools     []ToolDef
	MaxTokens int64
}

// Stream is an in-flight model response. Iterate with Next/Current, then check
// Err. Close releases the underlying HTTP response.
type Stream interface {
	Next() bool
	Current() StreamEvent
	Err() error
	Close() error
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
