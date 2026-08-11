// Package main is the eval CLI: replays a fixture incident through both
// providers (openai + ollama) sequentially, persists transcripts + ledger
// rows, and prints a summary.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/audit"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/repo"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
	"github.com/nexis-eco/nexis/services/control-plane/internal/usecase"
)

// Exit codes per spec Appendix E.
const (
	exitOK             = 0
	exitSchemaMismatch = 2
	exitBudgetExceeded = 3
	exitInfraFailure   = 4
)

func main() {
	var orgID, label, providers, scenario string
	flag.StringVar(&orgID, "org-id", os.Getenv("SEED_ORG_ID"), "organization id")
	flag.StringVar(&orgID, "org", orgID, "alias for --org-id")
	flag.StringVar(&label, "label", "demo-null-pointer", "fixture incident label")
	flag.StringVar(&scenario, "scenario", "", "alias for --label")
	flag.StringVar(&providers, "providers", "openai,ollama", "comma-separated provider override list")
	flag.Parse()
	if scenario != "" {
		label = scenario
	}
	if orgID == "" {
		fmt.Fprintln(os.Stderr, "--org-id required (or SEED_ORG_ID env)")
		os.Exit(2)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg := config.Load()
	if cfg.DatabaseURL == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL not set")
		os.Exit(exitInfraFailure)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	adminPool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "admin pool: %v\n", err)
		os.Exit(exitInfraFailure)
	}
	defer adminPool.Close()

	appPool := adminPool
	if cfg.DatabaseURLApp != "" {
		ap, err := pgxpool.New(ctx, cfg.DatabaseURLApp)
		if err == nil {
			appPool = ap
			defer ap.Close()
		}
	}

	evalRepo := repo.NewEvalRepo(appPool, adminPool)
	ledger := repo.NewTokenLedgerRepo(appPool, adminPool, repo.TokenLedgerConfig{
		AllowedTokensIn:  cfg.TokenBudgetTokensIn,
		AllowedTokensOut: cfg.TokenBudgetTokensOut,
		PeriodDays:       cfg.TokenBudgetPeriodDays,
	})
	workflowRepo := repo.NewWorkflowRepo(appPool, adminPool)

	var auditWriter domain.AuditWriter
	if cfg.AuditSecret != "" {
		auditWriter = audit.New([]byte(cfg.AuditSecret), adminPool)
	}

	providerList := strings.Split(providers, ",")
	for i, p := range providerList {
		providerList[i] = strings.TrimSpace(p)
	}

	runner := &usecase.EvalRunner{
		Cfg:          cfg,
		AppPool:      appPool,
		AdminPool:    adminPool,
		EvalRepo:     evalRepo,
		LedgerRepo:   ledger,
		WorkflowRepo: workflowRepo,
		AuditWriter:  auditWriter,
		Logger:       logger,
		Providers:    providerList,
	}

	p := domain.Principal{OrgID: orgID, Role: domain.RoleOwner}
	runID, err := runner.RunSync(ctx, p, label)
	if err != nil {
		fmt.Fprintf(os.Stderr, "eval failed: %v\n", err)
		os.Exit(classifyExit(err))
	}

	run, transcripts, err := evalRepo.Get(ctx, p, runID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "get run: %v\n", err)
		os.Exit(exitInfraFailure)
	}
	printSummary(run, transcripts)

	// Decide exit code based on schema validity.
	anyMismatch := false
	for _, t := range transcripts {
		if !t.SchemaValid {
			anyMismatch = true
			break
		}
	}
	if anyMismatch {
		os.Exit(exitSchemaMismatch)
	}
}

func printSummary(run domain.EvalRun, transcripts []domain.EvalTranscript) {
	fmt.Printf("eval_run_id=%s started_at=%s\n", run.ID, run.StartedAt.Format(time.RFC3339))
	if run.CompletedAt != nil {
		fmt.Printf("  duration=%s\n", run.CompletedAt.Sub(run.StartedAt))
	}
	for _, prov := range run.Providers {
		validCount, total := 0, 0
		var tokensIn, tokensOut int
		var cost float64
		for _, t := range transcripts {
			if t.Provider != prov {
				continue
			}
			total++
			if t.SchemaValid {
				validCount++
			}
			tokensIn += t.TokensIn
			tokensOut += t.TokensOut
			cost += t.CostCents
		}
		fmt.Printf("  %s:  tokens=%d/%d  cost=$%.4f  schema_valid=%d/%d\n",
			prov, tokensIn, tokensOut, cost/100, validCount, total)
	}
	if run.Error != "" {
		fmt.Printf("  error: %s\n", run.Error)
	}
}

func classifyExit(err error) int {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "budget"):
		return exitBudgetExceeded
	case strings.Contains(msg, "schema"):
		return exitSchemaMismatch
	default:
		return exitInfraFailure
	}
}
