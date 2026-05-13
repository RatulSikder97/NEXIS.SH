// Package devops implements the L1 DevOps agent — emits ArgoCD + GH Actions
// YAML strings for the demo deployment surface. Phase 6 wires the GitOps
// service to actually open PRs; Phase 5 only schema-validates the YAML.
package devops

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

func (p *Provider) Name() domain.AgentName { return domain.AgentNameDevOps }

func (p *Provider) Run(ctx context.Context, in domain.AgentInput) (domain.AgentOutput, error) {
	start := time.Now()
	inc := in.Incident
	if inc == nil {
		inc = &domain.IncidentPayload{Title: "Unspecified incident", Service: "nexis-fixture", Environment: "production"}
	}

	archPlanJSON := "{}"
	backendSummary := ""
	if in.PriorOutputs != nil {
		if a, ok := in.PriorOutputs["architect"]; ok {
			if b, err := json.MarshalIndent(a, "", "  "); err == nil {
				archPlanJSON = string(b)
			}
		}
		if b, ok := in.PriorOutputs["backend"].(map[string]any); ok {
			if s, ok := b["summary"].(string); ok {
				backendSummary = s
			}
		}
	}

	var buf bytes.Buffer
	if err := template.Must(template.New("u").Parse(UserTemplate)).Execute(&buf, map[string]any{
		"Title":             inc.Title,
		"Service":           inc.Service,
		"Environment":       inc.Environment,
		"ArchitectPlanJSON": archPlanJSON,
		"BackendSummary":    backendSummary,
	}); err != nil {
		return domain.AgentOutput{}, err
	}

	res, err := p.LLM.Invoke(ctx, agents.InvokeRequest{
		Agent:             domain.AgentNameDevOps,
		Model:             p.Model,
		System:            SystemPrompt,
		User:              buf.String(),
		MaxTokens:         1500,
		JSONResponse:      true,
		CacheSystem:       true,
		WorkflowRunID:     in.WorkflowRunID,
		OrgID:             in.OrgID,
		EstimatedTokensIn: 2500,
		Validate: func(content string) (map[string]any, error) {
			return agents.Validate(SchemaJSON, content)
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
