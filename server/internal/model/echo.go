package model

import (
	"context"
	"strings"
	"time"

	"github.com/expona-ai/lumi-go/server/internal/orchestrator"
)

// EchoClient is an offline StreamingClient: it streams a canned reply back one
// word at a time. It exists so the full path — auth, store, loop, Connect
// streaming, React rendering — can be exercised without an ANTHROPIC_API_KEY,
// and so tests never make a network call. Select it with MODEL_CLIENT=echo.
type EchoClient struct {
	// Delay between tokens. Non-zero makes the browser-side streaming visible
	// during development.
	Delay time.Duration
}

var _ orchestrator.StreamingClient = (*EchoClient)(nil)

func (c *EchoClient) Stream(_ context.Context, req orchestrator.StreamRequest) (orchestrator.Stream, error) {
	last := ""
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role != orchestrator.RoleUser {
			continue
		}
		for _, b := range req.Messages[i].Content {
			if b.Type == orchestrator.BlockText {
				last = b.Text
			}
		}
		if last != "" {
			break
		}
	}

	reply := "You said: " + last + ". This is the echo model — set ANTHROPIC_API_KEY and MODEL_CLIENT=anthropic for real responses."
	return &echoStream{words: strings.SplitAfter(reply, " "), delay: c.Delay}, nil
}

type echoStream struct {
	words   []string
	i       int
	delay   time.Duration
	current orchestrator.StreamEvent
	ended   bool
}

func (s *echoStream) Next() bool {
	if s.i < len(s.words) {
		if s.delay > 0 {
			time.Sleep(s.delay)
		}
		s.current = orchestrator.StreamEvent{Type: orchestrator.StreamText, Delta: s.words[s.i]}
		s.i++
		return true
	}
	if !s.ended {
		s.ended = true
		s.current = orchestrator.StreamEvent{Type: orchestrator.StreamMessageEnd, StopReason: "end_turn"}
		return true
	}
	return false
}

func (s *echoStream) Current() orchestrator.StreamEvent { return s.current }
func (s *echoStream) Err() error                        { return nil }
func (s *echoStream) Close() error                      { return nil }
