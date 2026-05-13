// Package domain holds the gitops service's pure data shapes. No
// implementation dependencies live here — adapter and usecase packages
// import these types but domain never imports them back.
package domain

import "time"

// RepoRef is the GitHub repository coordinate the open-pr endpoint targets.
// Owner + Name + BranchBase are required; defaults to "main" when empty.
type RepoRef struct {
	Owner         string `json:"owner"`
	Name          string `json:"name"`
	DefaultBranch string `json:"default_branch"`
}

// PROpenRequest is the wire shape of POST /v1/gitops/open-pr. The body
// carries everything needed to (a) authenticate the GitHub App installation
// for the org, (b) build a commit from the unified-diff patch, and (c)
// open a PR back to the default branch.
//
// Field names use snake_case JSON tags per project convention.
type PROpenRequest struct {
	OrgID         string `json:"org_id"`
	WorkspaceID   string `json:"workspace_id"`
	WorkflowRunID string `json:"workflow_run_id"`

	// Flat repo coords — the request body inlines these rather than
	// nesting under a "repo" key so the user-facing curl examples in the
	// runbook stay short.
	Repo         string `json:"repo"`         // "owner/name"
	BranchBase   string `json:"branch_base"`  // base branch — default "main"
	BranchName   string `json:"branch_name"`  // new branch
	CommitMsg    string `json:"commit_message"`
	PatchDiff    string `json:"patch_diff"`
	PRTitle      string `json:"pr_title"`
	PRBody       string `json:"pr_body"`
}

// PROpenResponse is the body of a successful 201. PRURL is the GitHub
// HTMLURL (web link); HeadSHA + Branch are echoed so the control-plane can
// pin the activity payload to a deterministic commit pointer.
type PROpenResponse struct {
	PRNumber int       `json:"pr_number"`
	PRURL    string    `json:"pr_url"`
	Branch   string    `json:"branch"`
	HeadSHA  string    `json:"head_sha"`
	OpenedAt time.Time `json:"opened_at"`
}
