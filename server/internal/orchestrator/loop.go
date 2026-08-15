package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// DefaultMaxToolRounds caps runaway tool loops.
const DefaultMaxToolRounds = 8

// MinThinkingPreambleChars is the shortest recall preamble worth preserving as
// a thinking chip. A stray "Sure." is noise; anything longer is a real
// "thinking through the intent" signal the user benefits from seeing.
const MinThinkingPreambleChars = 20

// LoopOptions configures one run of the streaming tool-use loop.
type LoopOptions struct {
	Client      StreamingClient
	RateLimiter RateLimiter
	Scope       RateKey // UserID + WorkspaceID; Tier is set from Tier below
	Tier        Tier
	ModelConfig ModelConfig

	Messages []Message
	System   string
	Tools    []ToolDef

	MaxTokens     int64
	MaxToolRounds int
}

// Emit receives each event the loop yields. Returning an error aborts the run
// (the surface's transport hung up).
type Emit func(Event) error

// RunStreamingLoop drives one turn:
//
//	intent → plan → deliver-first → dispatch tool rounds → stream
//
// Per round: check the rate limit, stream from the model accumulating text and
// tool_use blocks, then either finish (no tool calls) or execute the tools and
// loop. If any executed tool was flagged Recall, a discard_buffer is emitted so
// the preamble text is dropped before the canonical answer streams.
func RunStreamingLoop(ctx context.Context, opts LoopOptions, emit Emit) error {
	cfg := opts.ModelConfig
	if cfg.Strong == "" && cfg.Fast == "" {
		cfg = DefaultModels
	}
	model := cfg.Resolve(opts.Tier)

	maxRounds := opts.MaxToolRounds
	if maxRounds <= 0 {
		maxRounds = DefaultMaxToolRounds
	}

	toolsByName := make(map[string]ToolDef, len(opts.Tools))
	for _, t := range opts.Tools {
		toolsByName[t.Name] = t
	}

	// The growing history we feed the model each round.
	messages := append([]Message(nil), opts.Messages...)

	for round := 0; round < maxRounds; round++ {
		// 1. Rate-limit gate.
		key := RateKey{UserID: opts.Scope.UserID, WorkspaceID: opts.Scope.WorkspaceID, Tier: opts.Tier}
		if d := opts.RateLimiter.Check(key); !d.Allowed {
			hint := ""
			if d.RetryAfter > 0 {
				hint = fmt.Sprintf(" (retry after %s)", d.RetryAfter.Round(time.Millisecond))
			}
			return emit(Event{
				Type:    EventError,
				Code:    CodeRateLimited,
				Message: fmt.Sprintf("rate limit exceeded for tier %s%s", opts.Tier, hint),
			})
		}

		// 2-3. Stream, accumulating assistant text and tool calls.
		var assistantText strings.Builder
		var preambleStart time.Time
		toolUses := make([]*accumulatingToolUse, 0, 2)
		stopReason := "end_turn"

		stream, err := opts.Client.Stream(ctx, StreamRequest{
			Model:     model,
			System:    opts.System,
			Messages:  messages,
			Tools:     opts.Tools,
			MaxTokens: opts.MaxTokens,
		})
		if err != nil {
			return emit(Event{Type: EventError, Code: CodeModelError, Message: err.Error()})
		}

		streamErr := func() error {
			defer stream.Close()
			for stream.Next() {
				ev := stream.Current()
				switch ev.Type {
				case StreamText:
					if preambleStart.IsZero() {
						preambleStart = time.Now()
					}
					assistantText.WriteString(ev.Delta)
					if err := emit(Event{Type: EventTextDelta, Text: ev.Delta}); err != nil {
						return err
					}
				case StreamToolUseStart:
					toolUses = append(toolUses, &accumulatingToolUse{ID: ev.ID, Name: ev.Name})
				case StreamToolInputDelta:
					for _, tu := range toolUses {
						if tu.ID == ev.ID {
							tu.InputJSON.WriteString(ev.Partial)
							break
						}
					}
				case StreamToolUseStop:
					// Nothing to do — the partial JSON is parsed below.
				case StreamMessageEnd:
					if ev.StopReason != "" {
						stopReason = ev.StopReason
					}
				}
			}
			return stream.Err()
		}()
		if streamErr != nil {
			msg := streamErr.Error()
			if msg == "" {
				msg = "model stream failed"
			}
			return emit(Event{Type: EventError, Code: CodeModelError, Message: msg})
		}

		// 4a. No tool calls — the turn is done.
		if len(toolUses) == 0 {
			return emit(Event{Type: EventMessageEnd, StopReason: stopReason})
		}

		// 4b. Execute tools.
		toolResults := make([]ContentBlock, 0, len(toolUses))
		recallCalled := false
		var firstRecall *accumulatingToolUse

		for _, tu := range toolUses {
			input, err := tu.parsedInput()
			if err != nil {
				return emit(Event{
					Type:    EventError,
					Code:    CodeModelError,
					Message: fmt.Sprintf("tool %s: invalid input JSON: %v", tu.Name, err),
				})
			}

			if err := emit(Event{Type: EventToolUse, ToolUseID: tu.ID, ToolName: tu.Name, Input: input}); err != nil {
				return err
			}

			tool, ok := toolsByName[tu.Name]
			if !ok {
				msg := fmt.Sprintf("tool %s: not registered", tu.Name)
				if err := emit(Event{Type: EventToolResult, ToolUseID: tu.ID, Result: msg, IsError: true}); err != nil {
					return err
				}
				toolResults = append(toolResults, ContentBlock{
					Type: BlockToolResult, ToolUseID: tu.ID, Content: msg, IsError: true,
				})
				continue
			}

			if tool.Recall {
				recallCalled = true
				if firstRecall == nil {
					firstRecall = tu
				}
			}

			result, err := tool.Execute(ctx, input)
			if err != nil {
				msg := err.Error()
				if msg == "" {
					msg = "tool execution failed"
				}
				if emitErr := emit(Event{Type: EventToolResult, ToolUseID: tu.ID, Result: msg, IsError: true}); emitErr != nil {
					return emitErr
				}
				toolResults = append(toolResults, ContentBlock{
					Type: BlockToolResult, ToolUseID: tu.ID, Content: msg, IsError: true,
				})
				continue
			}

			if err := emit(Event{Type: EventToolResult, ToolUseID: tu.ID, Result: result}); err != nil {
				return err
			}
			toolResults = append(toolResults, ContentBlock{
				Type: BlockToolResult, ToolUseID: tu.ID, Content: result,
			})
		}

		// 5. Recall guard rail — drop the preamble before the next round streams.
		if recallCalled && assistantText.Len() > 0 {
			preamble := strings.TrimSpace(assistantText.String())
			if len(preamble) >= MinThinkingPreambleChars && firstRecall != nil {
				var dur time.Duration
				if !preambleStart.IsZero() {
					dur = time.Since(preambleStart)
				}
				block := buildThinkingBlock(preamble, dur, firstRecall.Name, firstRecall.ID)
				if err := emit(Event{Type: EventThinking, Block: block}); err != nil {
					return err
				}
			}
			if err := emit(Event{Type: EventDiscardBuffer}); err != nil {
				return err
			}
		}

		// 6. Append the assistant turn + tool results, then loop.
		assistantBlocks := make([]ContentBlock, 0, len(toolUses)+1)
		if assistantText.Len() > 0 {
			assistantBlocks = append(assistantBlocks, ContentBlock{Type: BlockText, Text: assistantText.String()})
		}
		for _, tu := range toolUses {
			input, _ := tu.parsedInput() // already validated above
			assistantBlocks = append(assistantBlocks, ContentBlock{
				Type: BlockToolUse, ID: tu.ID, Name: tu.Name, Input: input,
			})
		}
		messages = append(messages,
			Message{Role: RoleAssistant, Content: assistantBlocks},
			Message{Role: RoleUser, Content: toolResults},
		)
	}

	return emit(Event{
		Type:    EventError,
		Code:    CodeModelError,
		Message: fmt.Sprintf("max tool rounds reached (%d)", maxRounds),
	})
}

type accumulatingToolUse struct {
	ID        string
	Name      string
	InputJSON strings.Builder
}

func (t *accumulatingToolUse) parsedInput() (json.RawMessage, error) {
	raw := strings.TrimSpace(t.InputJSON.String())
	if raw == "" {
		return json.RawMessage(`{}`), nil
	}
	if !json.Valid([]byte(raw)) {
		return nil, fmt.Errorf("invalid JSON: %s", raw)
	}
	return json.RawMessage(raw), nil
}

var recallPrefix = regexp.MustCompile(`^(lookup|list|read|check|get)_`)

// HumanizeRecallTool turns a recall tool name into a short human purpose
// ("lookup_persona" → "checking your personas"). The raw tool name is never
// surfaced to a user.
func HumanizeRecallTool(name string) string {
	stripped := strings.TrimSpace(strings.ReplaceAll(recallPrefix.ReplaceAllString(name, ""), "_", " "))
	if stripped == "" {
		return "checking your workspace"
	}
	if !strings.HasSuffix(stripped, "s") {
		stripped += "s"
	}
	return "checking your " + stripped
}

func buildThinkingBlock(text string, dur time.Duration, toolName, toolUseID string) *CanvasBlock {
	return &CanvasBlock{
		Kind:     "thinking",
		ID:       "thinking:" + toolUseID,
		Rendered: text,
		Data: map[string]any{
			"text":       text,
			"durationMs": dur.Milliseconds(),
			"toolContext": map[string]any{
				"toolName":    toolName,
				"toolPurpose": HumanizeRecallTool(toolName),
			},
		},
	}
}
