// Package gitclone shallow-clones a GitHub repo into a caller-owned temp
// dir using a short-lived installation token for auth.
//
// Secrets policy (mirrors internal/adapter/integration/github/provider.go in
// the control-plane): the raw token NEVER appears in returned errors, logs,
// or captured git output — every string that could embed the clone URL runs
// through Redact first. Callers that want a forensic trail log
// TokenSHA256(token), never the token itself.
package gitclone

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// redactedPlaceholder replaces the raw token in any surfaced string.
const redactedPlaceholder = "[redacted]"

// repoRe validates the "owner/name" form so a hostile repo string can't
// smuggle extra URL segments or CLI flags into the git invocation.
var repoRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// refRe validates branch names + commit SHAs: no whitespace, no leading '-'
// (which git would parse as a flag).
var refRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)

// Options are the inputs for one clone. Dir must be an existing empty
// directory owned by the caller (the caller also removes it).
type Options struct {
	Repo      string // "owner/name"
	Branch    string
	CommitSHA string // optional; empty = branch HEAD
	Token     string // optional; empty = anonymous https (public repos)
	Dir       string
}

// Clone shallow-clones the branch HEAD (the common case) and, when a
// CommitSHA is supplied that differs from HEAD, fetches + checks out that
// SHA. Returns the resolved HEAD SHA and the combined (token-redacted) git
// output for diagnostics.
func Clone(ctx context.Context, opts Options) (headSHA string, output string, err error) {
	if !repoRe.MatchString(opts.Repo) {
		return "", "", fmt.Errorf("invalid repo %q: want owner/name", opts.Repo)
	}
	if !refRe.MatchString(opts.Branch) {
		return "", "", fmt.Errorf("invalid branch %q", opts.Branch)
	}
	if opts.CommitSHA != "" && !refRe.MatchString(opts.CommitSHA) {
		return "", "", fmt.Errorf("invalid commit sha %q", opts.CommitSHA)
	}

	url := CloneURL(opts.Repo, opts.Token)
	var log strings.Builder

	out, err := runGit(ctx, opts.Token, "clone", "--depth", "1", "--branch", opts.Branch, "--single-branch", url, opts.Dir)
	log.WriteString(out)
	if err != nil {
		return "", log.String(), fmt.Errorf("git clone %s@%s: %w", opts.Repo, opts.Branch, err)
	}

	headSHA, out, err = revParseHead(ctx, opts.Dir, opts.Token)
	log.WriteString(out)
	if err != nil {
		return "", log.String(), err
	}

	if opts.CommitSHA != "" && !strings.HasPrefix(strings.ToLower(headSHA), strings.ToLower(opts.CommitSHA)) {
		// Requested SHA is not the branch HEAD — fetch it explicitly.
		// GitHub allows fetching reachable SHAs directly; if the direct
		// fetch fails (e.g. short SHA), deepen the branch history and
		// retry the checkout against that.
		out, ferr := runGit(ctx, opts.Token, "-C", opts.Dir, "fetch", "--depth", "1", "origin", opts.CommitSHA)
		log.WriteString(out)
		if ferr != nil {
			out, ferr = runGit(ctx, opts.Token, "-C", opts.Dir, "fetch", "--depth", "100", "origin", opts.Branch)
			log.WriteString(out)
			if ferr != nil {
				return "", log.String(), fmt.Errorf("git fetch %s: %w", opts.CommitSHA, ferr)
			}
		}
		out, err = runGit(ctx, opts.Token, "-C", opts.Dir, "checkout", "--detach", opts.CommitSHA)
		log.WriteString(out)
		if err != nil {
			return "", log.String(), fmt.Errorf("git checkout %s: %w", opts.CommitSHA, err)
		}
		headSHA, out, err = revParseHead(ctx, opts.Dir, opts.Token)
		log.WriteString(out)
		if err != nil {
			return "", log.String(), err
		}
	}
	return headSHA, log.String(), nil
}

// CloneURL builds the https clone URL. With a token it uses the GitHub App
// installation-token convention (x-access-token basic auth); without one it
// returns the plain public URL.
func CloneURL(repo, token string) string {
	if token == "" {
		return "https://github.com/" + repo + ".git"
	}
	return "https://x-access-token:" + token + "@github.com/" + repo + ".git"
}

// Redact strips the raw token from s. Safe to call with an empty token.
func Redact(s, token string) string {
	if token == "" {
		return s
	}
	return strings.ReplaceAll(s, token, redactedPlaceholder)
}

// TokenSHA256 is the only representation of the token that may be logged —
// same forensic convention as the control-plane's github provider
// (metadata.last_token_sha256).
func TokenSHA256(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// runGit executes git with combined output capture; both the output and any
// error string are token-redacted before they leave this function.
func runGit(ctx context.Context, token string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = append(cmd.Environ(), "GIT_TERMINAL_PROMPT=0")
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	out := Redact(buf.String(), token)
	if err != nil {
		return out, fmt.Errorf("%s", Redact(err.Error(), token))
	}
	return out, nil
}

func revParseHead(ctx context.Context, dir, token string) (sha, output string, err error) {
	out, err := runGit(ctx, token, "-C", dir, "rev-parse", "HEAD")
	if err != nil {
		return "", out, fmt.Errorf("git rev-parse HEAD: %w", err)
	}
	return strings.TrimSpace(out), "", nil
}
