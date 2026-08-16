// Package newsagent is a research agent that never runs a loop here.
//
// The usual shape for this is a local agent framework: you own a loop that
// calls a model, reads the tool calls it asks for, executes searches, feeds the
// results back, and repeats until the model stops. That loop is most of the
// code, and all of the operational risk — retries, partial state, runaway
// iteration.
//
// xAI's Agent Tools move that loop server-side. One request declares which
// tools the model may use; the model then searches, reads, reasons and iterates
// on xAI's infrastructure and returns a finished answer. A single scan here
// routinely runs a dozen or more web searches. None of them are dispatched by
// this process, and there is no new service to deploy — it is one HTTP call
// that happens to take a minute.
//
// What is left for us is the part that is genuinely ours: what to ask for, what
// shape the answer must take, and what it is allowed to cost.
package newsagent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultModel is the model with server-side agent tools.
const DefaultModel = "grok-4.6"

// DefaultEndpoint is the Responses API. The tool loop is a property of this
// endpoint, not of the chat-completions one.
const DefaultEndpoint = "https://api.x.ai/v1/responses"

// Topics the watcher covers. The enum in the response schema is built from this
// list, so adding one here is the only edit needed.
var Topics = []string{"go", "grpc", "protobuf"}

// Item is one piece of news.
type Item struct {
	ID        string `json:"-"`
	Topic     string `json:"topic"`
	Headline  string `json:"headline"`
	Summary   string `json:"summary"`
	URL       string `json:"url"`
	Source    string `json:"source"`
	Published string `json:"published"`
}

// Digest is one completed scan.
type Digest struct {
	ID          string
	GeneratedAt time.Time
	Items       []Item
	// ToolCalls is how many server-side tools the model invoked. This is the
	// number that actually drives cost — the token count barely moves next to
	// it — so it is carried out of the package rather than logged and dropped.
	ToolCalls   int
	TotalTokens int64
	// CostTicks is the provider's own `cost_in_usd_ticks`. Deliberately not
	// converted to dollars: the tick scale is not documented, and a number
	// labelled USD that is wrong by an order of magnitude is worse than a raw
	// number labelled as raw.
	CostTicks int64
}

// Agent performs scans. Safe for concurrent use.
type Agent struct {
	APIKey   string
	Model    string
	Endpoint string
	// MaxTurns caps how many rounds of tool use the server-side loop may run.
	// This is the only lever we have on the cost of a single scan, since we do
	// not dispatch the searches and cannot stop them one by one.
	MaxTurns int
	// Window is how far back to look.
	Window time.Duration
	// MaxItems caps the digest size.
	MaxItems int

	HTTP *http.Client
}

// New returns an Agent with workable defaults.
func New(apiKey string) *Agent {
	return &Agent{
		APIKey:   apiKey,
		Model:    DefaultModel,
		Endpoint: DefaultEndpoint,
		MaxTurns: 12,
		Window:   30 * 24 * time.Hour,
		MaxItems: 6,
		// A scan takes about a minute of wall clock, nearly all of it spent in
		// searches on the far side. The timeout has to accommodate that; the
		// caller's context is what actually cancels a scan.
		HTTP: &http.Client{Timeout: 5 * time.Minute},
	}
}

// --- wire types -------------------------------------------------------------

type request struct {
	Model     string         `json:"model"`
	Input     []inputMessage `json:"input"`
	Tools     []tool         `json:"tools"`
	MaxTurns  int            `json:"max_turns,omitempty"`
	Reasoning *reasoning     `json:"reasoning,omitempty"`
	Text      *textFormat    `json:"text,omitempty"`
}

type inputMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type tool struct {
	Type string `json:"type"`
}

type reasoning struct {
	Effort string `json:"effort"`
}

type textFormat struct {
	Format jsonSchemaFormat `json:"format"`
}

type jsonSchemaFormat struct {
	Type   string         `json:"type"`
	Name   string         `json:"name"`
	Strict bool           `json:"strict"`
	Schema map[string]any `json:"schema"`
}

type response struct {
	Status string `json:"status"`
	Error  any    `json:"error"`
	Output []struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
	Usage struct {
		TotalTokens            int64 `json:"total_tokens"`
		NumServerSideToolsUsed int   `json:"num_server_side_tools_used"`
		CostInUSDTicks         int64 `json:"cost_in_usd_ticks"`
	} `json:"usage"`
}

// --- scanning ---------------------------------------------------------------

// Scan runs one research pass. It blocks for as long as the server-side loop
// takes, which is typically around a minute — call it from a background worker,
// never from a request handler.
func (a *Agent) Scan(ctx context.Context) (*Digest, error) {
	if strings.TrimSpace(a.APIKey) == "" {
		return nil, fmt.Errorf("newsagent: no API key")
	}

	body, err := json.Marshal(request{
		Model: a.model(),
		Input: []inputMessage{{Role: "user", Content: a.prompt()}},
		// web_search only. x_search is available and would add community
		// chatter, but every tool call is billed and a release announcement is
		// the kind of news this watcher is for — the signal is on release
		// pages and blogs, not in replies.
		Tools:    []tool{{Type: "web_search"}},
		MaxTurns: a.MaxTurns,
		// Low effort. The work here is retrieval, not reasoning: a default-
		// effort run spent thousands of tokens deliberating about a question
		// whose answer is "read the release notes".
		Reasoning: &reasoning{Effort: "low"},
		Text:      &textFormat{Format: a.schema()},
	})
	if err != nil {
		return nil, fmt.Errorf("newsagent: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.endpoint(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.APIKey)

	resp, err := a.http().Do(req)
	if err != nil {
		return nil, fmt.Errorf("newsagent: request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("newsagent: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("newsagent: %s: %s", resp.Status, snippet(raw))
	}

	var r response
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("newsagent: decode response: %w", err)
	}
	if r.Error != nil {
		return nil, fmt.Errorf("newsagent: api error: %v", r.Error)
	}

	text := finalMessage(&r)
	if text == "" {
		return nil, fmt.Errorf("newsagent: response carried no message (status %q)", r.Status)
	}

	var payload struct {
		Items []Item `json:"items"`
	}
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		return nil, fmt.Errorf("newsagent: decode digest: %w", err)
	}

	items := clean(payload.Items, a.maxItems())
	if len(items) == 0 {
		return nil, fmt.Errorf("newsagent: scan produced no usable items")
	}

	now := time.Now().UTC()
	return &Digest{
		ID:          digestID(items, now),
		GeneratedAt: now,
		Items:       items,
		ToolCalls:   r.Usage.NumServerSideToolsUsed,
		TotalTokens: r.Usage.TotalTokens,
		CostTicks:   r.Usage.CostInUSDTicks,
	}, nil
}

// finalMessage pulls the text out of the last message item. The output array
// interleaves reasoning steps and tool calls — a full transcript of the loop we
// did not have to write — and the answer is the message at the end.
func finalMessage(r *response) string {
	for i := len(r.Output) - 1; i >= 0; i-- {
		if r.Output[i].Type != "message" {
			continue
		}
		for _, c := range r.Output[i].Content {
			if strings.TrimSpace(c.Text) != "" {
				return c.Text
			}
		}
	}
	return ""
}

// clean drops items the model returned without a usable source, deduplicates by
// URL, and assigns stable ids.
//
// The URL check is not defensive tidying. An item with no link is an item a
// reader cannot verify, and an unverifiable claim presented as news is the one
// failure mode of a research agent that actually matters.
func clean(in []Item, max int) []Item {
	seen := make(map[string]bool, len(in))
	out := make([]Item, 0, len(in))
	for _, it := range in {
		it.URL = strings.TrimSpace(it.URL)
		it.Headline = strings.TrimSpace(it.Headline)
		if it.URL == "" || it.Headline == "" {
			continue
		}
		if !strings.HasPrefix(it.URL, "http://") && !strings.HasPrefix(it.URL, "https://") {
			continue
		}
		if seen[it.URL] {
			continue
		}
		seen[it.URL] = true

		it.Topic = normalizeTopic(it.Topic)
		it.ID = ItemID(it.URL)
		out = append(out, it)
		if len(out) >= max {
			break
		}
	}
	return out
}

func normalizeTopic(t string) string {
	t = strings.ToLower(strings.TrimSpace(t))
	for _, known := range Topics {
		if t == known {
			return t
		}
	}
	return "go"
}

// ItemID is derived from the URL so the same story keeps its identity across
// scans — which is what lets a client tell a genuinely new item from one it has
// already shown. Exported so a persisted digest can recompute ids on load
// rather than storing them, keeping this the only definition.
func ItemID(url string) string {
	sum := sha256.Sum256([]byte(url))
	return hex.EncodeToString(sum[:8])
}

func digestID(items []Item, at time.Time) string {
	h := sha256.New()
	for _, it := range items {
		_, _ = io.WriteString(h, it.URL+"\n")
	}
	_, _ = io.WriteString(h, at.Format(time.RFC3339))
	return hex.EncodeToString(h.Sum(nil)[:8])
}

func snippet(b []byte) string {
	const max = 300
	s := strings.TrimSpace(string(b))
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}

func (a *Agent) model() string {
	if a.Model != "" {
		return a.Model
	}
	return DefaultModel
}

func (a *Agent) endpoint() string {
	if a.Endpoint != "" {
		return a.Endpoint
	}
	return DefaultEndpoint
}

func (a *Agent) maxItems() int {
	if a.MaxItems > 0 {
		return a.MaxItems
	}
	return 6
}

func (a *Agent) http() *http.Client {
	if a.HTTP != nil {
		return a.HTTP
	}
	return http.DefaultClient
}
