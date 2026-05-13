package agents

import (
	"context"
	"fmt"
	"strings"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// RetrievalClient builds the "Relevant code" markdown block prepended to
// every L1 agent's user prompt. It's tolerant of missing Store / RepoSHA —
// returns "" so the agent prompt still runs unconditioned.
type RetrievalClient struct {
	Store      domain.RetrievalStore
	Embed      domain.EmbeddingProvider
	EmbedModel string
	K          int
}

// ContextFor embeds the query, runs TopK, and renders the resulting chunks
// as a markdown block. Returns ("", nil, nil) when the store/embed is unset
// or zero chunks are returned.
func (r *RetrievalClient) ContextFor(ctx context.Context, orgID, repoSHA, query string) (string, []domain.Chunk, error) {
	if r == nil || r.Store == nil || r.Embed == nil || strings.TrimSpace(query) == "" {
		return "", nil, nil
	}
	if r.K <= 0 {
		r.K = 5
	}
	vecs, err := r.Embed.Embed(ctx, r.EmbedModel, []string{query})
	if err != nil || len(vecs) == 0 {
		return "", nil, err
	}
	chunks, err := r.Store.TopK(ctx, orgID, repoSHA, vecs[0], r.K)
	if err != nil {
		return "", nil, err
	}
	if len(chunks) == 0 {
		return "", nil, nil
	}

	var b strings.Builder
	b.WriteString("## Relevant code (top ")
	b.WriteString(fmt.Sprint(len(chunks)))
	b.WriteString(" chunks by cosine similarity)\n\n")
	for _, c := range chunks {
		fmt.Fprintf(&b, "### `%s` L%d–L%d (sim=%.2f)\n```\n%s\n```\n\n",
			c.FilePath, c.ChunkStart, c.ChunkEnd, c.Similarity, c.Content)
	}
	return b.String(), chunks, nil
}
