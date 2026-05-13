// Package backend implements the L1 Backend agent — generates a unified
// diff that implements the Architect's plan.
package backend

import (
	"bytes"
	"context"
	"encoding/json"
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

	var buf bytes.Buffer
	if err := template.Must(template.New("u").Parse(UserTemplate)).Execute(&buf, map[string]any{
		"Title":             inc.Title,
		"Service":           inc.Service,
		"Environment":       inc.Environment,
		"Stacktrace":        inc.Stacktrace,
		"ArchitectPlanJSON": archPlanJSON,
		"RetrievalContext":  rctx,
	}); err != nil {
		return domain.AgentOutput{}, err
	}

	res, err := p.LLM.Invoke(ctx, agents.InvokeRequest{
		Agent:             domain.AgentNameBackend,
		Model:             p.Model,
		System:            SystemPrompt,
		User:              buf.String(),
		MaxTokens:         4000,
		JSONResponse:      false, // free-form prose + diff
		CacheSystem:       true,
		WorkflowRunID:     in.WorkflowRunID,
		OrgID:             in.OrgID,
		EstimatedTokensIn: 5000,
		Validate: func(content string) (map[string]any, error) {
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
			SystemPrompt: SystemPrompt, UserPrompt: buf.String(),
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
		SystemPrompt:  SystemPrompt,
		UserPrompt:    buf.String(),
	}, nil
}
