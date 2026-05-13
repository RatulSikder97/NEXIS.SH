package retrieval

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	pgv "github.com/nexis-eco/nexis/services/control-plane/internal/platform/pgvector"
)

// Store implements domain.RetrievalStore on top of pgvector. The seed
// script uses the admin pool (no RLS principal); request-bound TopK uses
// the same admin pool (the LLM activity is system-owned and passes orgID
// explicitly), but org_id is always filtered in the WHERE clause.
type Store struct {
	pool *pgxpool.Pool
}

// New constructs a Store. pool is the admin pool — the activity layer does
// not run inside an RLS request tx.
func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Insert writes a batch of Chunks. Wrapped in a tx so a partial failure
// rolls back.
func (s *Store) Insert(ctx context.Context, batch []domain.Chunk) error {
	if len(batch) == 0 {
		return nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	for _, c := range batch {
		if len(c.Embedding) == 0 {
			return fmt.Errorf("chunk %s L%d has empty embedding", c.FilePath, c.ChunkStart)
		}
		_, err := tx.Exec(ctx, `
            INSERT INTO code_embeddings
              (org_id, repo_sha, file_path, chunk_start, chunk_end, content, embedding)
            VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			c.OrgID, c.RepoSHA, c.FilePath, c.ChunkStart, c.ChunkEnd, c.Content,
			pgv.FromSlice(c.Embedding))
		if err != nil {
			return fmt.Errorf("insert chunk %s L%d: %w", c.FilePath, c.ChunkStart, err)
		}
	}
	return tx.Commit(ctx)
}

// TopK returns the K most similar Chunks for the given query vector within
// (orgID, repoSHA). Uses cosine distance via the pgvector `<=>` operator and
// converts to similarity = 1 - distance.
func (s *Store) TopK(ctx context.Context, orgID, repoSHA string, query []float32, k int) ([]domain.Chunk, error) {
	if k <= 0 {
		k = 5
	}
	rows, err := s.pool.Query(ctx, `
        SELECT file_path, chunk_start, chunk_end, content,
               1 - (embedding <=> $1::vector) AS similarity
        FROM code_embeddings
        WHERE org_id=$2 AND repo_sha=$3
        ORDER BY embedding <=> $1::vector
        LIMIT $4`,
		pgv.FromSlice(query), orgID, repoSHA, k)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Chunk{}
	for rows.Next() {
		c := domain.Chunk{OrgID: orgID, RepoSHA: repoSHA}
		if err := rows.Scan(&c.FilePath, &c.ChunkStart, &c.ChunkEnd, &c.Content, &c.Similarity); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
