package gitclone

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCloneURL(t *testing.T) {
	if got := CloneURL("owner/name", ""); got != "https://github.com/owner/name.git" {
		t.Fatalf("public URL = %q", got)
	}
	if got := CloneURL("owner/name", "ghs_secret"); got != "https://x-access-token:ghs_secret@github.com/owner/name.git" {
		t.Fatalf("token URL = %q", got)
	}
}

func TestRedact(t *testing.T) {
	tok := "ghs_supersecret123"
	in := "fatal: unable to access 'https://x-access-token:" + tok + "@github.com/o/r.git'"
	out := Redact(in, tok)
	if strings.Contains(out, tok) {
		t.Fatalf("token leaked: %q", out)
	}
	if !strings.Contains(out, "[redacted]") {
		t.Fatalf("placeholder missing: %q", out)
	}
	// Empty token must be a no-op, not a corruptor.
	if got := Redact("hello", ""); got != "hello" {
		t.Fatalf("empty-token redact mangled input: %q", got)
	}
}

func TestTokenSHA256(t *testing.T) {
	// Deterministic + never equal to the input.
	h := TokenSHA256("ghs_x")
	if len(h) != 64 || h == "ghs_x" {
		t.Fatalf("unexpected hash %q", h)
	}
	if h != TokenSHA256("ghs_x") {
		t.Fatal("hash not deterministic")
	}
}

func TestClone_RejectsHostileInputs(t *testing.T) {
	ctx := context.Background()
	cases := []Options{
		{Repo: "owner", Branch: "main"},                                    // no slash
		{Repo: "owner/name/extra", Branch: "main"},                         // extra segment
		{Repo: "owner/name@evil.com/x", Branch: "main"},                    // URL smuggling
		{Repo: "owner/name", Branch: "-c core.sshCommand=x"},               // flag injection
		{Repo: "owner/name", Branch: "main", CommitSHA: "--upload-pack=x"}, // flag injection
	}
	for _, opts := range cases {
		opts.Dir = t.TempDir()
		if _, _, err := Clone(ctx, opts); err == nil {
			t.Fatalf("expected validation error for %+v", opts)
		}
	}
}

// TestClone_TokenNeverInErrorOrOutput exercises the real git binary against a
// guaranteed-nonexistent repo and asserts the token cannot leak through the
// error path (the URL with the embedded token is exactly what git prints on
// failure).
func TestClone_TokenNeverInErrorOrOutput(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	tok := "ghs_leaktest_token_value"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, out, err := Clone(ctx, Options{
		// A repo name that cannot exist: fails fast whether online (404
		// from github.com) or offline (DNS error). Either way git prints
		// the clone URL — which embeds the token — into stderr.
		Repo:   "nexis-eco-does-not-exist-4242/nope",
		Branch: "main",
		Token:  tok,
		Dir:    filepath.Join(t.TempDir(), "clone"),
	})
	if err == nil {
		t.Skip("clone unexpectedly succeeded; cannot test failure redaction")
	}
	if strings.Contains(err.Error(), tok) {
		t.Fatalf("token leaked in error: %v", err)
	}
	if strings.Contains(out, tok) {
		t.Fatalf("token leaked in output: %s", out)
	}
}

// TestClone_LocalRepo does a real end-to-end clone of a file:// repo built on
// the fly, proving the branch-HEAD path and rev-parse plumbing work without
// network access. file:// URLs support --depth (unlike plain-path local
// clones), but CloneURL always targets github.com — so this drives runGit
// directly through a thin wrapper of the same options minus URL building.
func TestClone_LocalRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	src := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = src
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	if err := os.WriteFile(filepath.Join(src, "index.html"), []byte("<h1>hi</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "init")

	dst := filepath.Join(t.TempDir(), "clone")
	ctx := context.Background()
	out, err := runGit(ctx, "", "clone", "--depth", "1", "--branch", "main", "file://"+src, dst)
	if err != nil {
		t.Fatalf("local clone: %v\n%s", err, out)
	}
	sha, _, err := revParseHead(ctx, dst, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(sha) != 40 {
		t.Fatalf("rev-parse HEAD = %q, want 40-char sha", sha)
	}
}
