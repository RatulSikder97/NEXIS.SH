package retrieval

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWalk_SmallFile(t *testing.T) {
	dir := t.TempDir()
	var body strings.Builder
	for i := 0; i < 12; i++ {
		fmt.Fprintf(&body, "line %d\n", i)
	}
	if err := os.WriteFile(filepath.Join(dir, "small.py"), []byte(body.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "skip.py"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pnpm-lock.yaml"), []byte("ignored"), 0o644); err != nil {
		t.Fatal(err)
	}

	chunks, err := Walk(dir, "org", "sha")
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 {
		t.Fatalf("want 1 chunk, got %d", len(chunks))
	}
	c := chunks[0]
	if c.OrgID != "org" || c.RepoSHA != "sha" {
		t.Errorf("metadata wrong: %+v", c)
	}
	if c.FilePath != "small.py" {
		t.Errorf("path=%q", c.FilePath)
	}
	if c.ChunkStart != 1 {
		t.Errorf("start=%d", c.ChunkStart)
	}
	if c.ChunkEnd < 12 {
		t.Errorf("end=%d want>=12", c.ChunkEnd)
	}
}

func TestWalk_BinaryFileSkipped(t *testing.T) {
	dir := t.TempDir()
	body := append([]byte{0xff, 0x00, 0xab}, []byte("more binary garbage")...)
	if err := os.WriteFile(filepath.Join(dir, "bin.dat"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	chunks, err := Walk(dir, "org", "sha")
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 0 {
		t.Fatalf("expected zero chunks for binary file, got %d", len(chunks))
	}
}

func TestWalk_HiddenDirSkipped(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".hidden"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".hidden", "x.py"), []byte("print('x')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	chunks, err := Walk(dir, "org", "sha")
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 0 {
		t.Fatalf("hidden dir should be skipped, got %d chunks", len(chunks))
	}
}
