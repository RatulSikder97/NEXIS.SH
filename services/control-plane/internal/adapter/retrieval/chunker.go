// Package retrieval contains the pgvector-backed RetrievalStore + the
// file→chunk walker used by the seed CLI.
package retrieval

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

const (
	chunkLines   = 50
	chunkOverlap = 5
	maxFileBytes = 256 * 1024
)

var skipDirs = map[string]bool{
	".git": true, "__pycache__": true, "node_modules": true,
	".venv": true, "dist": true, "build": true, "target": true,
	".pytest_cache": true,
}

var lockfiles = map[string]bool{
	"pnpm-lock.yaml": true, "package-lock.json": true,
	"poetry.lock": true, "Cargo.lock": true, "go.sum": true,
}

// Walk yields one Chunk per ~50-line window (5-line overlap) of every file
// under root. Binary files, lockfiles, hidden + skip-listed dirs, and files
// larger than maxFileBytes are excluded. Chunks have Embedding empty —
// callers fill it via EmbeddingProvider.Embed before Insert.
func Walk(root string, orgID, repoSHA string) ([]domain.Chunk, error) {
	var out []domain.Chunk
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if skipDirs[name] {
				return fs.SkipDir
			}
			if strings.HasPrefix(name, ".") && path != root {
				return fs.SkipDir
			}
			return nil
		}
		if lockfiles[d.Name()] {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() > maxFileBytes {
			return nil
		}

		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sniff := body
		if len(sniff) > 512 {
			sniff = sniff[:512]
		}
		if bytes.IndexByte(sniff, 0) >= 0 {
			return nil // binary
		}

		relPath, _ := filepath.Rel(root, path)
		relPath = filepath.ToSlash(relPath)
		lines := strings.Split(string(body), "\n")

		step := chunkLines - chunkOverlap
		if step <= 0 {
			step = chunkLines
		}
		for start := 0; start < len(lines); start += step {
			end := start + chunkLines
			if end > len(lines) {
				end = len(lines)
			}
			content := strings.Join(lines[start:end], "\n")
			if strings.TrimSpace(content) == "" {
				if end == len(lines) {
					break
				}
				continue
			}
			out = append(out, domain.Chunk{
				OrgID:      orgID,
				RepoSHA:    repoSHA,
				FilePath:   relPath,
				ChunkStart: start + 1,
				ChunkEnd:   end,
				Content:    content,
			})
			if end == len(lines) {
				break
			}
		}
		return nil
	})
	return out, err
}
