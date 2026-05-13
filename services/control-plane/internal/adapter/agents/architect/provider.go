// Package architect implements the L1 Architect agent — produces the
// JSON-schema-validated remediation plan that downstream agents consume.
package architect

import (
	"bytes"
	"context"
	"text/template"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/agents"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// Provider is the concrete L1 Architect implementation. Wired in cmd/server/
// main.go and registered with agents.Registry under domain.AgentNameArchitect.
type Provider struct {
	LLM       *agents.LLMClient
	Retrieval *agents.RetrievalClient
	Model     string // resolved per-provider by main.go
}

func New(llm *agents.LLMClient, retrieval *agents.RetrievalClient, model string) *Provider {
	return &Provider{LLM: llm, Retrieval: retrieval, Model: model}
}

func (p *Provider) Name() domain.AgentName { return domain.AgentNameArchitect }

func (p *Provider) Run(ctx context.Context, in domain.AgentInput) (domain.AgentOutput, error) {
	start := time.Now()
	inc := in.Incident
	if inc == nil {
		inc = &domain.IncidentPayload{Title: "Unspecified incident"}
	}

	query := inc.Title + " " + inc.Stacktrace
	rctx, _, _ := p.Retrieval.ContextFor(ctx, in.OrgID, in.RepoSHA, query)

	var buf bytes.Buffer
	if err := template.Must(template.New("u").Parse(UserTemplate)).Execute(&buf, map[string]any{
		"Title":            inc.Title,
		"Service":          inc.Service,
		"Environment":      inc.Environment,
		"Stacktrace":       inc.Stacktrace,
		"Logs":             inc.Logs,
		"RetrievalContext": rctx,
	}); err != nil {
		return domain.AgentOutput{}, err
	}

	res, err := p.LLM.Invoke(ctx, agents.InvokeRequest{
		Agent:             domain.AgentNameArchitect,
		Model:             p.Model,
		System:            SystemPrompt,
		User:              buf.String(),
		MaxTokens:         1500,
		JSONResponse:      true,
		CacheSystem:       true,
		WorkflowRunID:     in.WorkflowRunID,
		OrgID:             in.OrgID,
		EstimatedTokensIn: 3000,
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
