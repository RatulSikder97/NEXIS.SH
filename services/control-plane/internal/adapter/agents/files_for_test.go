package agents

// FilesFor is what stops the Backend agent inventing the code around the line
// it is patching. These tests pin the two properties that matter: the block
// carries the file's real text with real line numbers, and every degraded
// input returns "" rather than a half-truth the model would fill in.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

type fileStore struct {
	chunks []domain.Chunk
	err    error
	asked  []string
}

func (s *fileStore) Insert(_ context.Context, _ []domain.Chunk) error { return nil }

func (s *fileStore) TopK(_ context.Context, _, _ string, _ []float32, _ int) ([]domain.Chunk, error) {
	return nil, nil
}

func (s *fileStore) FileChunks(_ context.Context, _, _ string, paths []string) ([]domain.Chunk, error) {
	s.asked = paths
	if s.err != nil {
		return nil, s.err
	}
	want := map[string]bool{}
	for _, p := range paths {
		want[p] = true
	}
	out := []domain.Chunk{}
	for _, c := range s.chunks {
		if want[c.FilePath] {
			out = append(out, c)
		}
	}
	return out, nil
}

func TestFilesFor_RendersExactTextWithRealLineNumbers(t *testing.T) {
	store := &fileStore{chunks: []domain.Chunk{
		{FilePath: "src/pricing.py", ChunkStart: 12, ChunkEnd: 14,
			Content: "def unit_price(line_total, quantity):\n    return line_total / quantity"},
		{FilePath: "src/pricing.py", ChunkStart: 20, ChunkEnd: 21,
			Content: "def apply_discount(total, percent):"},
	}}
	r := &RetrievalClient{Store: store}

	out, err := r.FilesFor(context.Background(), "org", "sha", []string{"src/pricing.py"})
	if err != nil {
		t.Fatalf("FilesFor: %v", err)
	}
	for _, want := range []string{
		"src/pricing.py",
		"12: def unit_price(line_total, quantity):",
		"13:     return line_total / quantity",
		"20: def apply_discount(total, percent):",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("block missing %q\n---\n%s", want, out)
		}
	}
	// The instruction is the point of the block — without it the model treats
	// the listing as background reading.
	if !strings.Contains(out, "MUST match these lines exactly") {
		t.Error("block does not tell the agent the lines are binding")
	}
	if len(store.asked) != 1 || store.asked[0] != "src/pricing.py" {
		t.Errorf("store asked for %v", store.asked)
	}
}

func TestFilesFor_DegradesToEmptyString(t *testing.T) {
	chunks := []domain.Chunk{{FilePath: "a.py", ChunkStart: 1, Content: "x = 1"}}
	cases := map[string]struct {
		client *RetrievalClient
		repo   string
		paths  []string
	}{
		"nil store":        {&RetrievalClient{}, "sha", []string{"a.py"}},
		"no paths":         {&RetrievalClient{Store: &fileStore{chunks: chunks}}, "sha", nil},
		"blank repo sha":   {&RetrievalClient{Store: &fileStore{chunks: chunks}}, "  ", []string{"a.py"}},
		"unindexed path":   {&RetrievalClient{Store: &fileStore{chunks: chunks}}, "sha", []string{"other.py"}},
		"store errors out": {&RetrievalClient{Store: &fileStore{err: errors.New("boom")}}, "sha", []string{"a.py"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			out, _ := tc.client.FilesFor(context.Background(), "org", tc.repo, tc.paths)
			if out != "" {
				t.Errorf("expected an empty block, got:\n%s", out)
			}
		})
	}
}

func TestFilesFor_NilClientIsSafe(t *testing.T) {
	var r *RetrievalClient
	if out, err := r.FilesFor(context.Background(), "org", "sha", []string{"a.py"}); out != "" || err != nil {
		t.Fatalf("nil client returned (%q, %v)", out, err)
	}
}
