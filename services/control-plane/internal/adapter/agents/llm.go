package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// LLMClient wraps a domain.LLMProvider + TokenLedger + AuditWriter so every
// agent gets uniform budget enforcement, ledger persistence, and audit row
// emission for free. Each agent constructs one of these (typically shared
// across all five via NewLLMClient at boot) and calls Invoke() once per LLM
// round-trip. Schema retries are managed here based on SchemaRetryMax.
type LLMClient struct {
	Provider       domain.LLMProvider
	Embedding      domain.EmbeddingProvider
	Ledger         domain.TokenLedger
	Audit          domain.AuditWriter
	Logger         *slog.Logger
	SchemaRetryMax int
}

// InvokeRequest packages one LLM call. Validate runs against the model
// output and returns ErrAgentSchemaMismatch on bad shape; LLMClient retries
// up to SchemaRetryMax+1 attempts.
type InvokeRequest struct {
	Agent             domain.AgentName
	Model             string
	System            string
	User              string
	MaxTokens         int
	JSONResponse      bool
	CacheSystem       bool
	WorkflowRunID     string
	OrgID             string
	EstimatedTokensIn int
	Validate          func(content string) (map[string]any, error)
}

// InvokeResult is the consolidated per-call telemetry the agents fold into
// their AgentOutput.
type InvokeResult struct {
	Content       string
	Structured    map[string]any
	TokensIn      int
	TokensOut     int
	CachedTokens  int
	CostCents     float64
	DurationMs    int64
	Model         string
	Provider      string
	SchemaRetries int
}

// Invoke runs one LLM call with optional schema retry. Returns
// ErrBudgetExceeded (wrapped) when the pre-flight budget check fails, or
// ErrAgentSchemaMismatch (wrapped) when retries are exhausted.
func (c *LLMClient) Invoke(ctx context.Context, req InvokeRequest) (InvokeResult, error) {
	// Pre-flight budget. The agent passes a rough upper bound; the ledger
	// only checks input tokens (output tokens are unknown until completion).
	if c.Ledger != nil {
		if err := c.Ledger.CheckBudget(ctx, req.OrgID, req.EstimatedTokensIn); err != nil {
			_ = c.Ledger.Record(ctx, domain.TokenLedgerEntry{
				OrgID: req.OrgID, WorkflowRunID: req.WorkflowRunID,
				Agent: req.Agent, Model: req.Model, Provider: c.Provider.Name(),
				Status: "budget_exceeded",
			})
			c.auditInvoked(ctx, req, domain.CompletionResponse{Model: req.Model}, "budget_exceeded", 0)
			return InvokeResult{}, fmt.Errorf("%w: agent=%s", domain.ErrBudgetExceeded, req.Agent)
		}
	}

	var lastErr error
	for attempt := 0; attempt <= c.SchemaRetryMax; attempt++ {
		resp, err := c.Provider.Complete(ctx, domain.CompletionRequest{
			Model:        req.Model,
			System:       req.System,
			Prompt:       req.User,
			MaxTokens:    req.MaxTokens,
			JSONResponse: req.JSONResponse,
			CacheSystem:  req.CacheSystem,
		})
		if err != nil {
			if c.Ledger != nil {
				_ = c.Ledger.Record(ctx, domain.TokenLedgerEntry{
					OrgID: req.OrgID, WorkflowRunID: req.WorkflowRunID,
					Agent: req.Agent, Model: req.Model, Provider: c.Provider.Name(),
					Status: "provider_error",
				})
			}
			c.auditInvoked(ctx, req, resp, "provider_error", attempt)
			return InvokeResult{}, fmt.Errorf("provider call failed: %w", err)
		}

		if req.Validate != nil {
			structured, vErr := req.Validate(resp.Content)
			if vErr == nil {
				if c.Ledger != nil {
					_ = c.Ledger.Record(ctx, domain.TokenLedgerEntry{
						OrgID: req.OrgID, WorkflowRunID: req.WorkflowRunID,
						Agent: req.Agent, Model: resp.Model, Provider: c.Provider.Name(),
						TokensIn: resp.InputTokens, TokensOut: resp.OutputTokens,
						CachedTokens: resp.CachedTokens, CostCents: resp.CostCents,
						DurationMs: int(resp.DurationMs), Status: "succeeded",
					})
				}
				c.auditInvoked(ctx, req, resp, "succeeded", attempt)
				return InvokeResult{
					Content: resp.Content, Structured: structured,
					TokensIn: resp.InputTokens, TokensOut: resp.OutputTokens,
					CachedTokens: resp.CachedTokens, CostCents: resp.CostCents,
					DurationMs: resp.DurationMs, Model: resp.Model, Provider: c.Provider.Name(),
					SchemaRetries: attempt,
				}, nil
			}
			lastErr = vErr
			if c.Ledger != nil {
				_ = c.Ledger.Record(ctx, domain.TokenLedgerEntry{
					OrgID: req.OrgID, WorkflowRunID: req.WorkflowRunID,
					Agent: req.Agent, Model: resp.Model, Provider: c.Provider.Name(),
					TokensIn: resp.InputTokens, TokensOut: resp.OutputTokens,
					CachedTokens: resp.CachedTokens, CostCents: resp.CostCents,
					DurationMs: int(resp.DurationMs), Status: "schema_mismatch",
				})
			}
			c.auditInvoked(ctx, req, resp, "schema_mismatch", attempt)
			if c.Logger != nil {
				c.Logger.Warn("agent schema mismatch",
					"agent", req.Agent, "attempt", attempt+1, "err", vErr)
			}
			continue
		}
		// No validator → return raw content unchanged.
		if c.Ledger != nil {
			_ = c.Ledger.Record(ctx, domain.TokenLedgerEntry{
				OrgID: req.OrgID, WorkflowRunID: req.WorkflowRunID,
				Agent: req.Agent, Model: resp.Model, Provider: c.Provider.Name(),
				TokensIn: resp.InputTokens, TokensOut: resp.OutputTokens,
				CachedTokens: resp.CachedTokens, CostCents: resp.CostCents,
				DurationMs: int(resp.DurationMs), Status: "succeeded",
			})
		}
		c.auditInvoked(ctx, req, resp, "succeeded", attempt)
		return InvokeResult{
			Content: resp.Content, TokensIn: resp.InputTokens, TokensOut: resp.OutputTokens,
			CachedTokens: resp.CachedTokens, CostCents: resp.CostCents,
			DurationMs: resp.DurationMs, Model: resp.Model, Provider: c.Provider.Name(),
		}, nil
	}
	return InvokeResult{}, fmt.Errorf("%w: agent=%s attempts=%d last=%v",
		domain.ErrAgentSchemaMismatch, req.Agent, c.SchemaRetryMax+1, lastErr)
}

func (c *LLMClient) auditInvoked(ctx context.Context, req InvokeRequest, resp domain.CompletionResponse, status string, retries int) {
	if c.Audit == nil {
		return
	}
	_ = c.Audit.Write(ctx, domain.Principal{OrgID: req.OrgID}, "agent.invoked", req.WorkflowRunID, map[string]any{
		"agent":          string(req.Agent),
		"model":          resp.Model,
		"provider":       c.Provider.Name(),
		"tokens_in":      resp.InputTokens,
		"tokens_out":     resp.OutputTokens,
		"cached_tokens":  resp.CachedTokens,
		"cost_cents":     resp.CostCents,
		"duration_ms":    resp.DurationMs,
		"status":         status,
		"schema_retries": retries,
	})
}

// DecodeJSONOrError is the default validator each agent plugs in via
// Validate. Returns ErrAgentSchemaMismatch when the body is not parseable
// JSON. Per-agent schema.go layers a stricter validator on top.
func DecodeJSONOrError(content string) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal([]byte(content), &m); err != nil {
		return nil, errors.Join(domain.ErrAgentSchemaMismatch, err)
	}
	return m, nil
}
