package chat

import (
	"context"
	"strings"
	"unicode"

	"github.com/expona-ai/lumi-go/server/internal/orchestrator"
)

// maxTitleChars bounds both the generated and the fallback title, so the rail
// row never has to truncate mid-render.
const maxTitleChars = 48

const titleSystemPrompt = `Write a short title for a conversation that starts with the user message below.
Rules: 2-6 words, no quotes, no trailing punctuation, no preamble. Reply with the title only.`

// TitleGenerator names a new conversation from its first user message, using
// the fast tier so it never adds latency to the turn it runs alongside.
type TitleGenerator struct {
	client      orchestrator.StreamingClient
	rateLimiter orchestrator.RateLimiter
	modelConfig orchestrator.ModelConfig
}

func NewTitleGenerator(c orchestrator.StreamingClient, rl orchestrator.RateLimiter, mc orchestrator.ModelConfig) *TitleGenerator {
	return &TitleGenerator{client: c, rateLimiter: rl, modelConfig: mc}
}

// Generate returns a model-written title, or "" when the model produced
// nothing usable. Callers fall back to FallbackTitle — a title is a courtesy,
// never a turn gate.
func (g *TitleGenerator) Generate(ctx context.Context, firstMessage string) (string, error) {
	stream, err := g.client.Stream(ctx, orchestrator.StreamRequest{
		Model:     g.modelConfig.Resolve(orchestrator.TierFast),
		System:    titleSystemPrompt,
		Messages:  []orchestrator.Message{orchestrator.UserText(firstMessage)},
		MaxTokens: 64,
	})
	if err != nil {
		return "", err
	}
	defer stream.Close()

	var b strings.Builder
	for stream.Next() {
		if ev := stream.Current(); ev.Type == orchestrator.StreamText {
			b.WriteString(ev.Delta)
		}
	}
	if err := stream.Err(); err != nil {
		return "", err
	}
	return cleanTitle(b.String()), nil
}

func cleanTitle(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, `"'`)
	s = strings.TrimRight(s, ".!?,;:")
	if s == "" {
		return ""
	}
	return truncateOnWord(s, maxTitleChars)
}

// FallbackTitle is the always-succeeds path: the first message trimmed to a
// rail-sized label. Used when no generator is wired, or when generation fails.
func FallbackTitle(firstMessage string) string {
	s := strings.Join(strings.Fields(firstMessage), " ")
	if s == "" {
		return "New conversation"
	}
	return truncateOnWord(s, maxTitleChars)
}

// truncateOnWord cuts at the last word boundary within max, appending an
// ellipsis so the reader can tell the label was shortened.
func truncateOnWord(s string, max int) string {
	if len([]rune(s)) <= max {
		return s
	}
	runes := []rune(s)[:max]
	cut := len(runes)
	for i := len(runes) - 1; i > 0; i-- {
		if unicode.IsSpace(runes[i]) {
			cut = i
			break
		}
	}
	return strings.TrimRight(string(runes[:cut]), " ") + "…"
}
