package domain

import "context"

// Chunk is the unit of retrieval. The pgvector index is on embedding; the
// repo writes chunk_start + chunk_end + content alongside.
type Chunk struct {
	OrgID      string
	RepoSHA    string
	FilePath   string
	ChunkStart int    // 1-indexed
	ChunkEnd   int    // inclusive end line
	Content    string // exact slice (whitespace preserved)
	Embedding  []float32
	Similarity float32 // populated by TopK; 1.0 = perfect match
}

// RetrievalStore is the port the agents layer (+ seed CLI) depend on.
// Implementations live in internal/adapter/retrieval/pgvector.go.
type RetrievalStore interface {
	Insert(ctx context.Context, batch []Chunk) error
	TopK(ctx context.Context, orgID, repoSHA string, query []float32, k int) ([]Chunk, error)
}

// EmbeddingProvider is implemented by both the OpenAI and Ollama LLM
// adapters. The factory composes one alongside the LLMProvider; the agents
// layer + seed CLI consume it.
type EmbeddingProvider interface {
	Name() string
	Embed(ctx context.Context, model string, texts []string) ([][]float32, error)
	EmbeddingDims() int
}
