// Package usecase implements the gitops service's open-pr flow. The
// orchestration steps are:
//
//   1. Look up the GitHub App installation for the org → build a Client.
//   2. Resolve the base branch tip via GetRef.
//   3. Parse the unified-diff patch; for each touched file fetch the
//      baseline blob, apply the hunks, and CreateBlob the new contents.
//   4. CreateTree with the new blob entries pointing at the base commit's tree.
//   5. CreateCommit with the message + parent = base SHA.
//   6. CreateRef refs/heads/<branch_name> pointing at the new commit.
//   7. PullRequests.Create from <branch_name> → base.
//   8. Audit row "gitops.pr_opened".
//
// All steps share one context.Context so a request-level timeout aborts the
// in-flight GitHub call cleanly.
package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	gh "github.com/nexis-eco/nexis/services/gitops/internal/adapter/github"
	"github.com/nexis-eco/nexis/services/gitops/internal/domain"
)

// ClientFactory returns a per-org Client (production: ghinstallation; tests:
// stubbed). Splitting the factory out of OpenPRUsecase lets tests register
// a fake without touching the ghinstallation layer.
type ClientFactory interface {
	ForOrg(ctx context.Context, orgID string) (gh.Client, error)
}

// AuditAppender is the audit-row sink. The HMAC chain is computed inside
// the repo; the usecase only supplies the row content.
type AuditAppender interface {
	AppendPROpened(ctx context.Context, orgID, actor, target string, metadata map[string]any) (string, error)
}

// OpenPRUsecase orchestrates the PR-open flow. Construct once at boot; the
// underlying Client + repo handles are reused across requests.
type OpenPRUsecase struct {
	clients ClientFactory
	audit   AuditAppender
}

// NewOpenPRUsecase wires the dependencies. Both must be non-nil.
func NewOpenPRUsecase(c ClientFactory, a AuditAppender) *OpenPRUsecase {
	return &OpenPRUsecase{clients: c, audit: a}
}

// Run executes the full open-pr flow described in the package doc.
//
// Validation rules:
//
//   - org_id, repo ("owner/name"), branch_name, patch_diff are required.
//   - branch_base defaults to "main".
//   - commit_message defaults to pr_title (which must be non-empty).
//
// Errors are surfaced verbatim. The transport layer maps:
//
//   - errBadDiff           → 400
//   - "installation not connected" → 412 (caller must connect GitHub first)
//   - everything else      → 500
func (u *OpenPRUsecase) Run(ctx context.Context, in domain.PROpenRequest) (domain.PROpenResponse, error) {
	if in.OrgID == "" {
		return domain.PROpenResponse{}, errors.New("gitops: org_id required")
	}
	if in.Repo == "" {
		return domain.PROpenResponse{}, errors.New("gitops: repo required")
	}
	if in.BranchName == "" {
		return domain.PROpenResponse{}, errors.New("gitops: branch_name required")
	}
	if in.PatchDiff == "" {
		return domain.PROpenResponse{}, errors.New("gitops: patch_diff required")
	}
	if in.PRTitle == "" {
		return domain.PROpenResponse{}, errors.New("gitops: pr_title required")
	}
	owner, name, ok := splitRepo(in.Repo)
	if !ok {
		return domain.PROpenResponse{}, errors.New("gitops: repo must be owner/name")
	}
	base := in.BranchBase
	if base == "" {
		base = "main"
	}
	commitMsg := in.CommitMsg
	if commitMsg == "" {
		commitMsg = in.PRTitle
	}

	client, err := u.clients.ForOrg(ctx, in.OrgID)
	if err != nil {
		return domain.PROpenResponse{}, err
	}

	// 1. base branch tip.
	baseRef, err := client.GetRef(ctx, owner, name, "refs/heads/"+base)
	if err != nil {
		return domain.PROpenResponse{}, fmt.Errorf("get base ref %s: %w", base, err)
	}

	// 2. parse + apply diff. Fetch baselines lazily — only the files the
	// patch touches are downloaded.
	fetch := func(path string) (string, error) {
		blob, err := client.GetFile(ctx, owner, name, path, baseRef.SHA)
		if err != nil {
			return "", err
		}
		return string(blob.Body), nil
	}
	files, err := parseUnifiedDiff(in.PatchDiff, fetch)
	if err != nil {
		return domain.PROpenResponse{}, err
	}

	// 3. CreateBlob for each new file body.
	entries := make([]gh.TreeEntry, 0, len(files))
	for _, f := range files {
		sha, err := client.CreateBlob(ctx, owner, name, []byte(f.NewBody))
		if err != nil {
			return domain.PROpenResponse{}, fmt.Errorf("create blob %s: %w", f.Path, err)
		}
		entries = append(entries, gh.TreeEntry{
			Path:    f.Path,
			Mode:    "100644",
			Type:    "blob",
			BlobSHA: sha,
		})
	}

	// 4. CreateTree on top of base commit's tree.
	treeSHA, err := client.CreateTree(ctx, owner, name, baseRef.SHA, entries)
	if err != nil {
		return domain.PROpenResponse{}, fmt.Errorf("create tree: %w", err)
	}

	// 5. CreateCommit.
	commit, err := client.CreateCommit(ctx, owner, name, commitMsg, treeSHA, []string{baseRef.SHA})
	if err != nil {
		return domain.PROpenResponse{}, fmt.Errorf("create commit: %w", err)
	}

	// 6. CreateRef refs/heads/<branch_name>.
	if _, err := client.CreateRef(ctx, owner, name, "refs/heads/"+in.BranchName, commit.SHA); err != nil {
		return domain.PROpenResponse{}, fmt.Errorf("create ref %s: %w", in.BranchName, err)
	}

	// 7. OpenPR.
	pr, err := client.OpenPR(ctx, owner, name, in.PRTitle, in.PRBody, in.BranchName, base)
	if err != nil {
		return domain.PROpenResponse{}, fmt.Errorf("open pr: %w", err)
	}

	openedAt := time.Now().UTC().Truncate(time.Microsecond)

	// 8. Audit row. Failure here is logged + best-effort: the PR is open
	// regardless. We surface the audit error to the caller anyway because
	// "PR opened but no audit trail" is a security-relevant state.
	if u.audit != nil {
		_, err = u.audit.AppendPROpened(ctx, in.OrgID, in.WorkflowRunID, fmt.Sprintf("%s#%d", in.Repo, pr.Number), map[string]any{
			"repo":            in.Repo,
			"branch":          in.BranchName,
			"branch_base":     base,
			"head_sha":        commit.SHA,
			"pr_number":       pr.Number,
			"pr_url":          pr.HTMLURL,
			"workflow_run_id": in.WorkflowRunID,
			"workspace_id":    in.WorkspaceID,
		})
		if err != nil {
			return domain.PROpenResponse{}, fmt.Errorf("audit append: %w", err)
		}
	}

	return domain.PROpenResponse{
		PRNumber: pr.Number,
		PRURL:    pr.HTMLURL,
		Branch:   in.BranchName,
		HeadSHA:  commit.SHA,
		OpenedAt: openedAt,
	}, nil
}

// splitRepo parses "owner/name" into its parts. Returns (owner, name, true)
// on success, ("", "", false) on any other shape.
func splitRepo(repo string) (string, string, bool) {
	idx := strings.Index(repo, "/")
	if idx <= 0 || idx == len(repo)-1 {
		return "", "", false
	}
	owner := repo[:idx]
	name := repo[idx+1:]
	if strings.Contains(name, "/") {
		return "", "", false
	}
	return owner, name, true
}
