package repo

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/db"
)

// EvalRepo persists eval_runs + eval_transcripts. Dual-pool the way
// WorkflowRepo + TokenLedgerRepo are: app pool for RLS-scoped HTTP reads
// (request tx via db.FromCtx) and admin pool for runner-side writes that
// happen outside any HTTP request (the eval CLI + the in-process runner
// goroutine kicked off by POST /v1/workspaces/{ws}/eval).
type EvalRepo struct {
	pool      *pgxpool.Pool // nexis_app — RLS-aware, used inside request tx
	adminPool *pgxpool.Pool // nexis superuser — bypasses RLS for runner writes
}

// NewEvalRepo constructs an EvalRepo. adminPool may be nil — runner writes
// then fall back to pool, which works when RLS is not yet enabled (early
// dev / unit tests) but breaks once 0013 + 0014 policies are applied.
func NewEvalRepo(pool, adminPool *pgxpool.Pool) *EvalRepo {
	if adminPool == nil {
		adminPool = pool
	}
	return &EvalRepo{pool: pool, adminPool: adminPool}
}

// CreateRun inserts the eval_runs row in "queued" state and assigns the
// per-provider statuses to queued. Called by the runner before any LLM
// dispatch so the CLI / UI can see the row immediately.
func (r *EvalRepo) CreateRun(ctx context.Context, run *domain.EvalRun) error {
	if run.StartedAt.IsZero() {
		run.StartedAt = time.Now().UTC().Truncate(time.Microsecond)
	}
	if run.Status == "" {
		run.Status = "queued"
	}
	if run.OpenAIStatus == "" {
		run.OpenAIStatus = domain.EvalRunStatusQueued
	}
	if run.OllamaStatus == "" {
		run.OllamaStatus = domain.EvalRunStatusQueued
	}
	return r.adminPool.QueryRow(ctx, `
        INSERT INTO eval_runs (
            org_id, incident_label, status, providers,
            openai_status, ollama_status,
            openai_tokens_in, openai_tokens_out,
            ollama_tokens_in, ollama_tokens_out,
            openai_cost_cents, ollama_cost_cents,
            duration_openai_ms, duration_ollama_ms,
            started_at, error
        ) VALUES (
            $1, $2, $3, $4,
            $5, $6,
            $7, $8,
            $9, $10,
            $11, $12,
            $13, $14,
            $15, NULLIF($16, '')
        )
        RETURNING id`,
		run.OrgID, run.IncidentLabel, run.Status, run.Providers,
		string(run.OpenAIStatus), string(run.OllamaStatus),
		run.OpenAITokensIn, run.OpenAITokensOut,
		run.OllamaTokensIn, run.OllamaTokensOut,
		run.OpenAICostCentsExact, run.OllamaCostCentsExact,
		run.DurationOpenAIMs, run.DurationOllamaMs,
		run.StartedAt, run.Error,
	).Scan(&run.ID)
}

// UpdateProviderStatus flips one provider leg's status independently of the
// other. Idempotent: re-running with the same state is a no-op write.
func (r *EvalRepo) UpdateProviderStatus(ctx context.Context, runID, provider string, status domain.EvalRunStatus) error {
	col := ""
	switch provider {
	case "openai":
		col = "openai_status"
	case "ollama":
		col = "ollama_status"
	default:
		return errors.New("eval_repo: unknown provider " + provider)
	}
	_, err := r.adminPool.Exec(ctx,
		`UPDATE eval_runs SET `+col+`=$2 WHERE id=$1`,
		runID, string(status))
	return err
}

// AccumulateProviderTotals folds one transcript's tokens + cost + duration
// into the per-provider running totals on the eval_runs row. Called after
// each agent completes (or fails) so the list view shows live progress.
func (r *EvalRepo) AccumulateProviderTotals(
	ctx context.Context,
	runID, provider string,
	tokensIn, tokensOut int,
	costCents float64,
	durationMs int64,
) error {
	var inCol, outCol, costCol, durCol string
	switch provider {
	case "openai":
		inCol, outCol, costCol, durCol = "openai_tokens_in", "openai_tokens_out", "openai_cost_cents", "duration_openai_ms"
	case "ollama":
		inCol, outCol, costCol, durCol = "ollama_tokens_in", "ollama_tokens_out", "ollama_cost_cents", "duration_ollama_ms"
	default:
		return errors.New("eval_repo: unknown provider " + provider)
	}
	_, err := r.adminPool.Exec(ctx, `
        UPDATE eval_runs SET
            `+inCol+` = COALESCE(`+inCol+`, 0) + $2,
            `+outCol+` = COALESCE(`+outCol+`, 0) + $3,
            `+costCol+` = COALESCE(`+costCol+`, 0) + $4,
            `+durCol+` = COALESCE(`+durCol+`, 0) + $5
        WHERE id=$1`,
		runID, tokensIn, tokensOut, costCents, durationMs)
	return err
}

// CompleteRun stamps the terminal state on the eval_runs row. completed=true
// means the runner has finished both providers (or failed and decided to
// give up); the legacy single `status` column is set accordingly so any
// non-eval tooling reading the 0011 shape still sees a sensible value.
func (r *EvalRepo) CompleteRun(ctx context.Context, runID, runErr string) error {
	now := time.Now().UTC().Truncate(time.Microsecond)
	status := "completed"
	if runErr != "" {
		status = "failed"
	}
	_, err := r.adminPool.Exec(ctx, `
        UPDATE eval_runs SET
            status=$2,
            completed_at=$3,
            error=NULLIF($4, '')
        WHERE id=$1`,
		runID, status, now, runErr)
	return err
}

// AppendTranscript persists one (provider × agent) row. JSON-encoded
// input/output payloads land in the jsonb columns so future SQL queries can
// index into them without re-parsing.
func (r *EvalRepo) AppendTranscript(ctx context.Context, t *domain.EvalTranscript) error {
	if t.StartedAt.IsZero() {
		t.StartedAt = time.Now().UTC().Truncate(time.Microsecond)
	}
	finished := nullTime(t.FinishedAt)
	inputJSON, _ := json.Marshal(safeMap(t.InputJSON))
	outputJSON, _ := json.Marshal(safeMap(t.OutputJSON))
	return r.adminPool.QueryRow(ctx, `
        INSERT INTO eval_transcripts (
            org_id, eval_run_id, provider, agent, model,
            input_json, output_json,
            success, schema_valid,
            tokens_in, tokens_out, cached_tokens,
            cost_cents, duration_ms,
            started_at, finished_at
        ) VALUES (
            $1, $2, $3, $4, $5,
            $6::jsonb, $7::jsonb,
            $8, $9,
            $10, $11, $12,
            $13, $14,
            $15, $16
        )
        ON CONFLICT (eval_run_id, provider, agent) DO UPDATE SET
            model        = EXCLUDED.model,
            input_json   = EXCLUDED.input_json,
            output_json  = EXCLUDED.output_json,
            success      = EXCLUDED.success,
            schema_valid = EXCLUDED.schema_valid,
            tokens_in    = EXCLUDED.tokens_in,
            tokens_out   = EXCLUDED.tokens_out,
            cached_tokens= EXCLUDED.cached_tokens,
            cost_cents   = EXCLUDED.cost_cents,
            duration_ms  = EXCLUDED.duration_ms,
            started_at   = EXCLUDED.started_at,
            finished_at  = EXCLUDED.finished_at
        RETURNING id`,
		t.OrgID, t.EvalRunID, t.Provider, string(t.Agent), t.Model,
		inputJSON, outputJSON,
		t.Success, t.SchemaValid,
		t.TokensIn, t.TokensOut, t.CachedTokens,
		t.CostCents, t.DurationMs,
		t.StartedAt, finished,
	).Scan(&t.ID)
}

// Get loads one eval_run + its transcripts. p.OrgID is a defence-in-depth
// filter on top of RLS — calling with a foreign org returns ErrNotFound
// even if the policy is misconfigured. Routes through db.FromCtx so HTTP
// callers benefit from the per-request RLS tx; the eval CLI sets
// SessionID="" so the call falls through to adminPool which bypasses RLS
// (no principal in ctx at CLI time).
func (r *EvalRepo) Get(ctx context.Context, p domain.Principal, runID string) (domain.EvalRun, []domain.EvalTranscript, error) {
	// Heuristic: an HTTP-bound caller always has a SessionID (set by the
	// auth middleware). The CLI path leaves it empty, so we hit the admin
	// pool directly to bypass RLS. The WHERE org_id filter remains the
	// authoritative check in both modes.
	useTx := p.SessionID != ""
	run, err := r.fetchRun(ctx, useTx, p.OrgID, runID)
	if err != nil {
		return domain.EvalRun{}, nil, err
	}
	transcripts, err := r.fetchTranscripts(ctx, useTx, p.OrgID, runID)
	if err != nil {
		return run, nil, err
	}
	return run, transcripts, nil
}

// List returns the eval_runs rows for one org, newest first. limit is
// capped at 200; offset implements cursor-less pagination.
func (r *EvalRepo) List(ctx context.Context, p domain.Principal, limit, offset int) ([]domain.EvalRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := db.FromCtx(ctx, r.pool)
	rows, err := q.Query(ctx, `
        SELECT id::text, org_id::text, incident_label, status,
               providers,
               COALESCE(openai_status, ''), COALESCE(ollama_status, ''),
               COALESCE(openai_tokens_in, 0),  COALESCE(openai_tokens_out, 0),
               COALESCE(ollama_tokens_in, 0),  COALESCE(ollama_tokens_out, 0),
               COALESCE(openai_cost_cents, 0)::float8,
               COALESCE(ollama_cost_cents, 0)::float8,
               COALESCE(duration_openai_ms, 0), COALESCE(duration_ollama_ms, 0),
               started_at, completed_at,
               COALESCE(error, '')
        FROM eval_runs
        WHERE org_id=$1
        ORDER BY started_at DESC
        LIMIT $2 OFFSET $3`, p.OrgID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.EvalRun{}
	for rows.Next() {
		run, scanErr := scanEvalRun(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

// fetchRun runs the single-row SELECT. When useTx is true the request-tx
// is resolved via db.FromCtx so RLS pins org_id; when false the call
// hits adminPool directly (CLI path).
func (r *EvalRepo) fetchRun(ctx context.Context, useTx bool, orgID, runID string) (domain.EvalRun, error) {
	row := r.queryRow(ctx, useTx, `
        SELECT id::text, org_id::text, incident_label, status,
               providers,
               COALESCE(openai_status, ''), COALESCE(ollama_status, ''),
               COALESCE(openai_tokens_in, 0),  COALESCE(openai_tokens_out, 0),
               COALESCE(ollama_tokens_in, 0),  COALESCE(ollama_tokens_out, 0),
               COALESCE(openai_cost_cents, 0)::float8,
               COALESCE(ollama_cost_cents, 0)::float8,
               COALESCE(duration_openai_ms, 0), COALESCE(duration_ollama_ms, 0),
               started_at, completed_at,
               COALESCE(error, '')
        FROM eval_runs
        WHERE org_id=$1 AND id=$2`, orgID, runID)
	run, err := scanEvalRun(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return run, domain.ErrNotFound
	}
	return run, err
}

func (r *EvalRepo) fetchTranscripts(ctx context.Context, useTx bool, orgID, runID string) ([]domain.EvalTranscript, error) {
	var rows pgx.Rows
	var err error
	if useTx {
		q := db.FromCtx(ctx, r.pool)
		rows, err = q.Query(ctx, transcriptsSQL, orgID, runID)
	} else {
		rows, err = r.adminPool.Query(ctx, transcriptsSQL, orgID, runID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.EvalTranscript{}
	for rows.Next() {
		var t domain.EvalTranscript
		var agent string
		var inputJSON, outputJSON []byte
		var finished *time.Time
		if scanErr := rows.Scan(
			&t.ID, &t.EvalRunID, &t.OrgID, &t.Provider, &agent, &t.Model,
			&inputJSON, &outputJSON,
			&t.Success, &t.SchemaValid,
			&t.TokensIn, &t.TokensOut, &t.CachedTokens,
			&t.CostCents, &t.DurationMs,
			&t.StartedAt, &finished,
		); scanErr != nil {
			return nil, scanErr
		}
		t.Agent = domain.AgentName(agent)
		if finished != nil {
			t.FinishedAt = *finished
		}
		_ = json.Unmarshal(inputJSON, &t.InputJSON)
		_ = json.Unmarshal(outputJSON, &t.OutputJSON)
		out = append(out, t)
	}
	return out, rows.Err()
}

const transcriptsSQL = `
SELECT id::text, eval_run_id::text, org_id::text, provider, agent, model,
       COALESCE(input_json, 'null'::jsonb),
       COALESCE(output_json, 'null'::jsonb),
       success, schema_valid,
       tokens_in, tokens_out, cached_tokens,
       cost_cents::float8, duration_ms,
       started_at, finished_at
FROM eval_transcripts
WHERE org_id=$1 AND eval_run_id=$2
ORDER BY started_at, provider, agent`

// queryRow runs the SELECT through the RLS tx when useTx is true,
// otherwise hits adminPool directly.
func (r *EvalRepo) queryRow(ctx context.Context, useTx bool, sql string, args ...any) pgx.Row {
	if useTx {
		q := db.FromCtx(ctx, r.pool)
		return q.QueryRow(ctx, sql, args...)
	}
	return r.adminPool.QueryRow(ctx, sql, args...)
}

// rowScanner is the common shape pgx.Row + pgx.Rows expose. We accept both
// so scanEvalRun can serve single-row and list paths from one body.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanEvalRun(row rowScanner) (domain.EvalRun, error) {
	var run domain.EvalRun
	var providers []string
	var openaiStatus, ollamaStatus string
	var completed *time.Time
	if err := row.Scan(
		&run.ID, &run.OrgID, &run.IncidentLabel, &run.Status,
		&providers,
		&openaiStatus, &ollamaStatus,
		&run.OpenAITokensIn, &run.OpenAITokensOut,
		&run.OllamaTokensIn, &run.OllamaTokensOut,
		&run.OpenAICostCentsExact, &run.OllamaCostCentsExact,
		&run.DurationOpenAIMs, &run.DurationOllamaMs,
		&run.StartedAt, &completed,
		&run.Error,
	); err != nil {
		return run, err
	}
	run.Providers = providers
	if openaiStatus != "" {
		run.OpenAIStatus = domain.EvalRunStatus(openaiStatus)
	}
	if ollamaStatus != "" {
		run.OllamaStatus = domain.EvalRunStatus(ollamaStatus)
	}
	if completed != nil {
		run.CompletedAt = completed
	}
	return run, nil
}

// safeMap returns m or an empty map. Empty map round-trips through JSON as
// `{}` rather than `null`, which keeps the eval_transcripts.input_json /
// output_json columns non-NULL and queryable by Postgres' JSON operators
// without coalescing every read.
func safeMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}
