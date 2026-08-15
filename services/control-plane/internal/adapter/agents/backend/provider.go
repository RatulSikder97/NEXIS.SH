// Package backend implements the L1 Backend agent — generates a unified
// diff that implements the Architect's plan.
package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"text/template"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/agents"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

type Provider struct {
	LLM       *agents.LLMClient
	Retrieval *agents.RetrievalClient
	Model     string
}

func New(llm *agents.LLMClient, retrieval *agents.RetrievalClient, model string) *Provider {
	return &Provider{LLM: llm, Retrieval: retrieval, Model: model}
}

func (p *Provider) Name() domain.AgentName { return domain.AgentNameBackend }

func (p *Provider) Run(ctx context.Context, in domain.AgentInput) (domain.AgentOutput, error) {
	start := time.Now()
	inc := in.Incident
	if inc == nil {
		inc = &domain.IncidentPayload{Title: "Unspecified incident"}
	}

	archPlanJSON := "{}"
	if in.PriorOutputs != nil {
		if a, ok := in.PriorOutputs["architect"]; ok {
			if b, err := json.MarshalIndent(a, "", "  "); err == nil {
				archPlanJSON = string(b)
			}
		}
	}

	query := inc.Title + " " + inc.Stacktrace + " " + archPlanJSON
	rctx, _, _ := p.Retrieval.ContextFor(ctx, in.OrgID, in.RepoSHA, query)

	// Exact contents of the files the Architect committed to touching. With
	// them we can run in rewrite mode — the agent returns whole files and the
	// diff is computed here — which removes the "diff does not apply" class
	// of failure entirely. Without them we fall back to asking for a diff.
	originals, _ := p.Retrieval.FileTexts(ctx, in.OrgID, in.RepoSHA, affectedFiles(in.PriorOutputs))
	fctx := renderFileBlock(originals)
	rewriteMode := len(originals) > 0

	var buf bytes.Buffer
	if err := template.Must(template.New("u").Parse(UserTemplate)).Execute(&buf, map[string]any{
		"Title":             inc.Title,
		"Service":           inc.Service,
		"Environment":       inc.Environment,
		"Stacktrace":        inc.Stacktrace,
		"ArchitectPlanJSON": archPlanJSON,
		"RetrievalContext":  rctx,
		"FileContext":       fctx,
	}); err != nil {
		return domain.AgentOutput{}, err
	}

	system := SystemPrompt
	if rewriteMode {
		system = SystemPromptRewrite
	}

	res, err := p.LLM.Invoke(ctx, agents.InvokeRequest{
		Agent:             domain.AgentNameBackend,
		Model:             p.Model,
		System:            system,
		User:              buf.String(),
		MaxTokens:         4000,
		JSONResponse:      false, // free-form prose + diff
		CacheSystem:       true,
		WorkflowRunID:     in.WorkflowRunID,
		OrgID:             in.OrgID,
		EstimatedTokensIn: 5000,
		Validate: func(content string) (map[string]any, error) {
			if rewriteMode {
				diff, files, summary, rerr := diffFromRewrite(content, originals)
				if rerr != nil {
					return nil, rerr
				}
				if summary == "" {
					summary = SummaryFromContent(content)
				}
				return map[string]any{
					"patch_diff":    diff,
					"files_changed": files,
					"summary":       summary,
				}, nil
			}
			diff, files, err := ExtractDiff(content)
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"patch_diff":    diff,
				"files_changed": files,
				"summary":       SummaryFromContent(content),
			}, nil
		},
	})
	if err != nil {
		return domain.AgentOutput{
			Success: false, Content: res.Content,
			SystemPrompt: system, UserPrompt: buf.String(),
		}, err
	}

	return domain.AgentOutput{
		Success:       true,
		Content:       res.Content,
		Structured:    res.Structured,
		TokensIn:      res.TokensIn,
		TokensOut:     res.TokensOut,
		CachedTokens:  res.CachedTokens,
		CostCents:     res.CostCents,
		DurationMs:    time.Since(start).Milliseconds(),
		Model:         res.Model,
		Provider:      res.Provider,
		SchemaRetries: res.SchemaRetries,
		SystemPrompt:  system,
		UserPrompt:    buf.String(),
	}, nil
}

// affectedFiles pulls the Architect's `affected_files` contract out of the
// prior-output map. That list is already enforced downstream — a diff that
// strays outside it forces HIGH severity — so it is exactly the set of files
// the Backend agent should be looking at while writing the patch.
func affectedFiles(prior map[string]any) []string {
	if prior == nil {
		return nil
	}
	arch, ok := prior["architect"].(map[string]any)
	if !ok {
		return nil
	}
	raw, ok := arch["affected_files"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

// renderFileBlock formats path → content as the "current file contents" block,
// with real line numbers so hunk headers and context lines have something
// exact to be checked against.
func renderFileBlock(files map[string]string) string {
	if len(files) == 0 {
		return ""
	}
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var b strings.Builder
	b.WriteString("## Current file contents — your rewrite must start from these exact lines\n\n")
	for _, path := range paths {
		fmt.Fprintf(&b, "### `%s`\n```\n", path)
		for i, line := range strings.Split(files[path], "\n") {
			fmt.Fprintf(&b, "%d: %s\n", i+1, line)
		}
		b.WriteString("```\n\n")
	}
	return b.String()
}
