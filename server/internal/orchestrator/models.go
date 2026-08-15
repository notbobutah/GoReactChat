package orchestrator

// Tier selects which model a run uses. `strong` is the main reasoning tier for
// an interactive surface; `fast` backs the auxiliary helpers (titling, memory
// extraction) that must not add latency to the turn.
type Tier string

const (
	TierStrong Tier = "strong"
	TierFast   Tier = "fast"
)

// ModelConfig maps tiers to concrete model ids. Overridable from env so a
// deployment can pin a model without a rebuild.
type ModelConfig struct {
	Strong string
	Fast   string
}

// DefaultModels — Claude Opus 5 for the main loop, Haiku 4.5 for the fast tier.
var DefaultModels = ModelConfig{
	Strong: "claude-opus-5",
	Fast:   "claude-haiku-4-5",
}

// Resolve returns the model id for a tier, falling back to the strong model
// for an unknown tier rather than sending an empty model to the API.
func (c ModelConfig) Resolve(t Tier) string {
	switch t {
	case TierFast:
		if c.Fast != "" {
			return c.Fast
		}
	case TierStrong:
		if c.Strong != "" {
			return c.Strong
		}
	}
	if c.Strong != "" {
		return c.Strong
	}
	return DefaultModels.Strong
}
