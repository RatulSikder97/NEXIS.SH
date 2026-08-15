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

// FilesFor renders the exact indexed contents of the named files as a
// markdown block, with real line numbers.
//
// ContextFor answers "what code looks relevant"; this answers "what does the
// file I am about to patch actually say". A unified diff has to reproduce
// context lines byte-for-byte, so the agent needs the second question
// answered or it invents plausible surroundings — imports that aren't there,
// helpers that don't exist — and `git apply` rejects the result.
//
// Returns "" when the store is unset, no paths are given, or none of them are
// indexed; the caller's prompt then falls back to similarity context alone.
func (r *RetrievalClient) FilesFor(ctx context.Context, orgID, repoSHA string, paths []string) (string, error) {
	if r == nil || r.Store == nil || len(paths) == 0 || strings.TrimSpace(repoSHA) == "" {
		return "", nil
	}
	chunks, err := r.Store.FileChunks(ctx, orgID, repoSHA, paths)
	if err != nil || len(chunks) == 0 {
		return "", err
	}

	byFile := map[string][]domain.Chunk{}
	order := []string{}
	for _, c := range chunks {
		if _, seen := byFile[c.FilePath]; !seen {
			order = append(order, c.FilePath)
		}
		byFile[c.FilePath] = append(byFile[c.FilePath], c)
	}

	var b strings.Builder
	b.WriteString("## Current file contents — the diff MUST match these lines exactly\n\n")
	for _, path := range order {
		fmt.Fprintf(&b, "### `%s`\n```\n", path)
		for _, c := range byFile[path] {
			line := c.ChunkStart
			for _, text := range strings.Split(c.Content, "\n") {
				fmt.Fprintf(&b, "%d: %s\n", line, text)
				line++
			}
		}
		b.WriteString("```\n\n")
	}
	return b.String(), nil
}

// FileTexts returns the indexed text of the named files as path → content,
// reassembled in line order. This is the raw material behind FilesFor: the
// caller needs the map (not just the rendered block) when it has to diff the
// agent's rewritten file against the original.
func (r *RetrievalClient) FileTexts(ctx context.Context, orgID, repoSHA string, paths []string) (map[string]string, error) {
	if r == nil || r.Store == nil || len(paths) == 0 || strings.TrimSpace(repoSHA) == "" {
		return nil, nil
	}
	chunks, err := r.Store.FileChunks(ctx, orgID, repoSHA, paths)
	if err != nil || len(chunks) == 0 {
		return nil, err
	}
	out := map[string]string{}
	for _, c := range chunks {
		if prev, ok := out[c.FilePath]; ok {
			out[c.FilePath] = prev + "\n" + c.Content
			continue
		}
		out[c.FilePath] = c.Content
	}
	return out, nil
}
