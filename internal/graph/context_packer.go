package graph

import (
	"fmt"
	"strings"

	"github.com/pkoukk/tiktoken-go"
	"github.com/truongvinh/biograph/internal/config"
)

// ContextPacker assembles a token-bounded context string from activated nodes.
type ContextPacker struct {
	cfg     *config.Config
	encoder *tiktoken.Tiktoken
}

func NewContextPacker(cfg *config.Config) *ContextPacker {
	enc, err := tiktoken.GetEncoding("cl100k_base")
	if err != nil {
		enc = nil // fallback to char-based estimation
	}
	return &ContextPacker{cfg: cfg, encoder: enc}
}

// Pack assembles context from activated nodes within the token budget.
// Nodes are already sorted by energy descending — highest priority first.
func (p *ContextPacker) Pack(nodes []ActivatedNode) string {
	budget := p.cfg.Activation.MaxContextTokens
	if budget <= 0 {
		budget = 6000
	}

	var sb strings.Builder
	tokensUsed := 0

	for _, node := range nodes {
		entry := p.formatEntry(node)
		entryTokens := p.countTokens(entry)

		if tokensUsed+entryTokens > budget {
			break
		}

		sb.WriteString(entry)
		sb.WriteString("\n---\n")
		tokensUsed += entryTokens
	}

	return sb.String()
}

func (p *ContextPacker) formatEntry(node ActivatedNode) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "**%s** [%s | %s] (relevance: %.2f)\n", node.DisplayName, node.Category, node.Course, node.Energy)
	fmt.Fprintf(&sb, "%s\n", node.Definition)

	if len(node.RawLatex) > 0 {
		sb.WriteString("Equations:\n")
		for _, eq := range node.RawLatex {
			fmt.Fprintf(&sb, "  %s\n", eq)
		}
	}

	if len(node.Sources) > 0 {
		refs := make([]string, 0, len(node.Sources))
		for _, s := range node.Sources {
			refs = append(refs, fmt.Sprintf("%s p.%d", s.PDF, s.Page))
		}
		fmt.Fprintf(&sb, "Sources: %s\n", strings.Join(refs, ", "))
	}

	return sb.String()
}

func (p *ContextPacker) countTokens(s string) int {
	if p.encoder != nil {
		return len(p.encoder.Encode(s, nil, nil))
	}
	// Rough fallback: 4 chars ≈ 1 token
	return len(s) / 4
}
