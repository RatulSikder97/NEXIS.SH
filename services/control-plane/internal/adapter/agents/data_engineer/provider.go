// Package data_engineer implements the L1 DataEngineer agent — proposes
// forward + reverse SQL migrations when the patch requires schema change,
// or returns an empty migrations array otherwise.
package data_engineer

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

func (p *Provider) Name() domain.AgentName { return domain.AgentNameDataEngineer }

func (p *Provider) Run(ctx context.Context, in domain.AgentInput) (domain.AgentOutput, error) {
	start := time.Now()
	inc := in.Incident
	if inc == nil {
		inc = &domain.IncidentPayload{Title: "Unspecified incident"}
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
		"ArchitectPlanJSON": archPlanJSON,
		"BackendSummary":    backendSummary,
	}); err != nil {
		return domain.AgentOutput{}, err
	}

	res, err := p.LLM.Invoke(ctx, agents.InvokeRequest{
		Agent:             domain.AgentNameDataEngineer,
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
