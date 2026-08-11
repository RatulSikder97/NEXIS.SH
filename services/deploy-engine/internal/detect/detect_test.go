package detect

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDetect(t *testing.T) {
	cases := []struct {
		name    string
		markers []string
		want    Stack
	}{
		{"node", []string{"package.json"}, StackNode},
		{"python requirements", []string{"requirements.txt"}, StackPython},
		{"python pyproject", []string{"pyproject.toml"}, StackPython},
		{"go", []string{"go.mod"}, StackGo},
		{"static", []string{"index.html"}, StackStatic},
		{"empty repo", nil, StackUnknown},
		{"readme only", []string{"README.md"}, StackUnknown},
		{"node beats static", []string{"package.json", "index.html"}, StackNode},
		{"node beats python", []string{"package.json", "requirements.txt"}, StackNode},
		{"python beats go", []string{"requirements.txt", "go.mod"}, StackPython},
		{"go beats static", []string{"go.mod", "index.html"}, StackGo},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, m := range tc.markers {
				writeFile(t, dir, m, "x")
			}
			if got := Detect(dir); got != tc.want {
				t.Fatalf("Detect() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDetect_DirectoryMarkerIgnored(t *testing.T) {
	// A DIRECTORY named package.json must not count as a marker file.
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "package.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := Detect(dir); got != StackUnknown {
		t.Fatalf("Detect() = %q, want unknown for dir marker", got)
	}
}

func TestHasRootDockerfile(t *testing.T) {
	dir := t.TempDir()
	if HasRootDockerfile(dir) {
		t.Fatal("empty dir should have no Dockerfile")
	}
	writeFile(t, dir, "Dockerfile", "FROM alpine")
	if !HasRootDockerfile(dir) {
		t.Fatal("Dockerfile present but not detected")
	}
}
