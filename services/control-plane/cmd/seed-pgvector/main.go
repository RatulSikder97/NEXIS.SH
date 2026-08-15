// Package main seeds code_embeddings from services/validator/fixtures/. Run
// via: docker compose run --rm control-plane /app/seed-pgvector --org-id=<id>
// Re-running is safe per (org, repo_sha) — the script does an explicit DELETE
// of that pair first, then INSERTs the fresh set.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/llm"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/retrieval"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
)

const defaultFixtureRoot = "services/validator/fixtures"

func main() {
	var orgID, fixtureOverride, repoSHAOverride string
	flag.StringVar(&orgID, "org-id", os.Getenv("SEED_ORG_ID"), "organization id to seed under")
	flag.StringVar(&orgID, "org", orgID, "alias for --org-id")
	flag.StringVar(&fixtureOverride, "fixture-dir", "", "directory to walk (defaults to services/validator/fixtures)")
	flag.StringVar(&fixtureOverride, "repo", fixtureOverride, "alias for --fixture-dir")
	// Retrieval reads WHERE org_id=$2 AND repo_sha=$3, so the seeded tag has
	// to equal the repo_sha the pipeline runs under. The git-HEAD default
	// changes on every commit and never matches the demo path's stable
	// "fixture-seed-001", which left agents with zero code context.
	flag.StringVar(&repoSHAOverride, "repo-sha", os.Getenv("SEED_REPO_SHA"),
		"repo_sha tag to seed under (defaults to fixture-<git HEAD>)")
	flag.Parse()
	if orgID == "" {
		fmt.Fprintln(os.Stderr, "--org-id required (or SEED_ORG_ID env)")
		os.Exit(2)
	}
	if fixtureOverride == "" {
		fixtureOverride = defaultFixtureRoot
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg := config.Load()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if cfg.DatabaseURL == "" {
		logger.Error("DATABASE_URL must be set")
		os.Exit(1)
	}
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL) // admin URL — bypasses RLS for seed
	if err != nil {
		logger.Error("pool", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	providers, err := llm.NewProviders(cfg, logger)
	if err != nil {
		logger.Error("providers", "err", err)
		os.Exit(1)
	}

	repoSHA := repoSHAOverride
	if repoSHA == "" {
		repoSHA = "fixture-" + repoSHA_()
	}
	chunks, err := retrieval.Walk(fixtureOverride, orgID, repoSHA)
	if err != nil {
		logger.Error("walk", "err", err)
		os.Exit(1)
	}
	logger.Info("walk complete", "chunks", len(chunks), "repo_sha", repoSHA)
	if len(chunks) == 0 {
		logger.Warn("no chunks to seed — fixture dir is empty or all files skipped")
		return
	}

	// Idempotent: wipe any prior rows for this (org, repo_sha) before insert.
	if _, err := pool.Exec(ctx, `DELETE FROM code_embeddings WHERE org_id=$1 AND repo_sha=$2`, orgID, repoSHA); err != nil {
		logger.Error("delete", "err", err)
		os.Exit(1)
	}

	store := retrieval.New(pool)
	embedModel := cfg.OpenAIEmbedModel
	if providers.Embedding.Name() == "ollama" {
		embedModel = cfg.OllamaEmbedModel
	}

	for i := 0; i < len(chunks); i += 64 {
		end := i + 64
		if end > len(chunks) {
			end = len(chunks)
		}
		batch := chunks[i:end]
		texts := make([]string, len(batch))
		for j, c := range batch {
			texts[j] = c.Content
		}
		vecs, err := providers.Embedding.Embed(ctx, embedModel, texts)
		if err != nil {
			logger.Error("embed", "err", err, "batch_start", i)
			os.Exit(1)
		}
		for j := range batch {
			batch[j].Embedding = vecs[j]
		}
		if err := store.Insert(ctx, batch); err != nil {
			logger.Error("insert", "err", err, "batch_start", i)
			os.Exit(1)
		}
	}

	// Build the ivfflat index (idempotent). Must run AFTER data exists so
	// the centroids are computed correctly.
	if _, err := pool.Exec(ctx, `
        CREATE INDEX IF NOT EXISTS code_embeddings_embedding_ivfflat
        ON code_embeddings USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100)`); err != nil {
		logger.Error("index", "err", err)
		os.Exit(1)
	}

	fmt.Printf("seeded %d chunks for org=%s repo_sha=%s\n", len(chunks), orgID, repoSHA)
}

func repoSHA_() string {
	out, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}
