// Package model implements orchestrator.StreamingClient against a provider.
//
// AnthropicClient is the production client; EchoClient is the offline
// stand-in used by tests and by `MODEL_CLIENT=echo`, so the whole chat path can
// be exercised without an API key.
package model

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/ssestream"

	"github.com/expona-ai/lumi-go/server/internal/orchestrator"
)

// DefaultMaxTokens is the per-turn output cap when a caller doesn't set one.
// The turn always streams, so this can be generous without risking an HTTP
// timeout — and on Claude Opus 5 thinking is on by default and shares this
// budget with the visible response, so leave it headroom.
const DefaultMaxTokens int64 = 16000

// AnthropicClient adapts the Anthropic Go SDK to the orchestrator seam.
type AnthropicClient struct {
	client anthropic.Client
	// Effort tunes thinking depth and overall token spend. Interactive chat
	// defaults to medium: on Claude Opus 5 the lower levels are unusually
	// strong, and high/xhigh buy depth the average chat turn doesn't need at a
	// real cost in time-to-first-token.
	effort anthropic.OutputConfigEffort
}

func NewAnthropicClient(apiKey, effort string) *AnthropicClient {
	opts := []option.RequestOption{}
	if apiKey != "" {
		opts = append(opts, option.WithAPIKey(apiKey))
	}
	return &AnthropicClient{
		client: anthropic.NewClient(opts...),
		effort: normalizeEffort(effort),
	}
}

func normalizeEffort(e string) anthropic.OutputConfigEffort {
	switch anthropic.OutputConfigEffort(e) {
	case anthropic.OutputConfigEffortLow,
		anthropic.OutputConfigEffortMedium,
		anthropic.OutputConfigEffortHigh,
		anthropic.OutputConfigEffortXhigh,
		anthropic.OutputConfigEffortMax:
		return anthropic.OutputConfigEffort(e)
	default:
		return anthropic.OutputConfigEffortMedium
	}
}

var _ orchestrator.StreamingClient = (*AnthropicClient)(nil)

func (c *AnthropicClient) Stream(ctx context.Context, req orchestrator.StreamRequest) (orchestrator.Stream, error) {
	messages, err := toAnthropicMessages(req.Messages)
	if err != nil {
		return nil, err
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = DefaultMaxTokens
	}

	params := anthropic.MessageNewParams{
		Model:        anthropic.Model(req.Model),
		MaxTokens:    maxTokens,
		Messages:     messages,
		OutputConfig: anthropic.OutputConfigParam{Effort: c.effort},
	}
	if req.System != "" {
		// One cache breakpoint, on the system block. A breakpoint caches
		// everything up to and including itself — tools and system prompt —
		// which here is the whole stable prefix: the résumé, the job
		// description and the project documentation are inlined and identical
		// on every single turn, and were being reprocessed every time.
		//
		// Only the conversation that follows differs, so this is the largest
		// possible prefix that is safe to cache.
		block := anthropic.TextBlockParam{Text: req.System}
		if req.CacheSystem {
			block.CacheControl = anthropic.NewCacheControlEphemeralParam()
		}
		params.System = []anthropic.TextBlockParam{block}
	}
	if len(req.Tools) > 0 {
		params.Tools = toAnthropicTools(req.Tools)
	}

	return newAnthropicStream(c.client.Messages.NewStreaming(ctx, params)), nil
}

func toAnthropicMessages(msgs []orchestrator.Message) ([]anthropic.MessageParam, error) {
	out := make([]anthropic.MessageParam, 0, len(msgs))
	for _, m := range msgs {
		blocks := make([]anthropic.ContentBlockParamUnion, 0, len(m.Content))
		for _, b := range m.Content {
			switch b.Type {
			case orchestrator.BlockText:
				if b.Text == "" {
					continue
				}
				blocks = append(blocks, anthropic.NewTextBlock(b.Text))
			case orchestrator.BlockToolUse:
				var input any = map[string]any{}
				if len(b.Input) > 0 {
					if err := json.Unmarshal(b.Input, &input); err != nil {
						return nil, fmt.Errorf("tool_use %s: %w", b.Name, err)
					}
				}
				blocks = append(blocks, anthropic.ContentBlockParamUnion{
					OfToolUse: &anthropic.ToolUseBlockParam{ID: b.ID, Name: b.Name, Input: input},
				})
			case orchestrator.BlockToolResult:
				blocks = append(blocks, anthropic.NewToolResultBlock(b.ToolUseID, b.Content, b.IsError))
			}
		}
		if len(blocks) == 0 {
			continue
		}
		role := anthropic.MessageParamRoleUser
		if m.Role == orchestrator.RoleAssistant {
			role = anthropic.MessageParamRoleAssistant
		}
		out = append(out, anthropic.MessageParam{Role: role, Content: blocks})
	}
	return out, nil
}

func toAnthropicTools(tools []orchestrator.ToolDef) []anthropic.ToolUnionParam {
	out := make([]anthropic.ToolUnionParam, 0, len(tools))
	for _, t := range tools {
		props := t.Properties
		if props == nil {
			props = map[string]any{}
		}
		tool := anthropic.ToolParam{
			Name:        t.Name,
			Description: anthropic.String(t.Description),
			InputSchema: anthropic.ToolInputSchemaParam{Properties: props, Required: t.Required},
		}
		out = append(out, anthropic.ToolUnionParam{OfTool: &tool})
	}
	return out
}

// anthropicStream normalizes the SDK's raw event union into the small set the
// orchestrator consumes. Events we don't model (message_start, text block
// stops, thinking deltas) are skipped rather than surfaced.
type anthropicStream struct {
	stream  *ssestream.Stream[anthropic.MessageStreamEventUnion]
	current orchestrator.StreamEvent
	// input_json_delta identifies its block by index, not by tool-use id, so we
	// keep the mapping from the content_block_start that opened it.
	toolIDByIndex map[int64]string
}

func newAnthropicStream(s *ssestream.Stream[anthropic.MessageStreamEventUnion]) *anthropicStream {
	return &anthropicStream{stream: s, toolIDByIndex: map[int64]string{}}
}

func (s *anthropicStream) Next() bool {
	for s.stream.Next() {
		ev := s.stream.Current()
		switch v := ev.AsAny().(type) {
		case anthropic.ContentBlockStartEvent:
			if v.ContentBlock.Type == "tool_use" {
				s.toolIDByIndex[v.Index] = v.ContentBlock.ID
				s.current = orchestrator.StreamEvent{
					Type: orchestrator.StreamToolUseStart,
					ID:   v.ContentBlock.ID,
					Name: v.ContentBlock.Name,
				}
				return true
			}
		case anthropic.ContentBlockDeltaEvent:
			switch d := v.Delta.AsAny().(type) {
			case anthropic.TextDelta:
				s.current = orchestrator.StreamEvent{Type: orchestrator.StreamText, Delta: d.Text}
				return true
			case anthropic.InputJSONDelta:
				s.current = orchestrator.StreamEvent{
					Type:    orchestrator.StreamToolInputDelta,
					ID:      s.toolIDByIndex[v.Index],
					Partial: d.PartialJSON,
				}
				return true
			}
		case anthropic.ContentBlockStopEvent:
			if id, ok := s.toolIDByIndex[v.Index]; ok {
				s.current = orchestrator.StreamEvent{Type: orchestrator.StreamToolUseStop, ID: id}
				return true
			}
		case anthropic.MessageDeltaEvent:
			// Usage here is cumulative for the whole message, so this single
			// event carries the full cost of the call.
			s.current = orchestrator.StreamEvent{
				Type:       orchestrator.StreamMessageEnd,
				StopReason: string(v.Delta.StopReason),
				Usage: orchestrator.Usage{
					InputTokens:              v.Usage.InputTokens,
					OutputTokens:             v.Usage.OutputTokens,
					CacheCreationInputTokens: v.Usage.CacheCreationInputTokens,
					CacheReadInputTokens:     v.Usage.CacheReadInputTokens,
				},
			}
			return true
		}
	}
	return false
}

func (s *anthropicStream) Current() orchestrator.StreamEvent { return s.current }
func (s *anthropicStream) Err() error                        { return s.stream.Err() }
func (s *anthropicStream) Close() error                      { return s.stream.Close() }
