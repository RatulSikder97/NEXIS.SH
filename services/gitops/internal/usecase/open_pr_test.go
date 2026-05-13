package usecase_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	gh "github.com/nexis-eco/nexis/services/gitops/internal/adapter/github"
	"github.com/nexis-eco/nexis/services/gitops/internal/domain"
	"github.com/nexis-eco/nexis/services/gitops/internal/usecase"
)

// fakeClient records every GitHub call + lets each test set canned
// responses. Concurrency-safe (the usecase serialises calls within a
// single request, but we lock anyway since the test runner may run with
// -race).
type fakeClient struct {
	mu sync.Mutex

	baseRef     gh.Reference
	files       map[string]gh.FileBlob // (path) → blob
	blobsByBody map[string]string      // body → sha (idempotent)
	treeSHA     string
	commitSHA   string
	pr          gh.PR

	calls       []string
	getRefErr   error
	openPRErr   error
}

func (f *fakeClient) GetRef(_ context.Context, owner, repo, ref string) (gh.Reference, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "GetRef:"+owner+"/"+repo+":"+ref)
	if f.getRefErr != nil {
		return gh.Reference{}, f.getRefErr
	}
	return f.baseRef, nil
}

func (f *fakeClient) GetFile(_ context.Context, _, _, path, _ string) (gh.FileBlob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "GetFile:"+path)
	blob, ok := f.files[path]
	if !ok {
		return gh.FileBlob{}, errors.New("not found")
	}
	return blob, nil
}

func (f *fakeClient) CreateBlob(_ context.Context, _, _ string, body []byte) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "CreateBlob:"+string(body))
	if sha, ok := f.blobsByBody[string(body)]; ok {
		return sha, nil
	}
	sha := "blob-" + sha8(string(body))
	if f.blobsByBody == nil {
		f.blobsByBody = map[string]string{}
	}
	f.blobsByBody[string(body)] = sha
	return sha, nil
}

func (f *fakeClient) CreateTree(_ context.Context, _, _ string, baseTreeSHA string, entries []gh.TreeEntry) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	parts := []string{"CreateTree:" + baseTreeSHA}
	for _, e := range entries {
		parts = append(parts, e.Path+"="+e.BlobSHA)
	}
	f.calls = append(f.calls, strings.Join(parts, "|"))
	return f.treeSHA, nil
}

func (f *fakeClient) CreateCommit(_ context.Context, _, _, message, treeSHA string, parentSHAs []string) (gh.Commit, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "CreateCommit:"+message+":"+treeSHA+":"+strings.Join(parentSHAs, ","))
	return gh.Commit{SHA: f.commitSHA, Message: message}, nil
}

func (f *fakeClient) CreateRef(_ context.Context, _, _, ref, sha string) (gh.Reference, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "CreateRef:"+ref+":"+sha)
	return gh.Reference{Name: ref, SHA: sha}, nil
}

func (f *fakeClient) OpenPR(_ context.Context, owner, repo, title, body, head, base string) (gh.PR, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "OpenPR:"+owner+"/"+repo+":"+head+"→"+base+":"+title)
	if f.openPRErr != nil {
		return gh.PR{}, f.openPRErr
	}
	return f.pr, nil
}

func sha8(s string) string {
	// quick test-only hash: sum of bytes mod 100000007.
	var n uint64
	for _, c := range s {
		n = (n*31 + uint64(c)) % 100000007
	}
	out := make([]byte, 8)
	for i := 0; i < 8; i++ {
		out[i] = "0123456789abcdef"[n&0xf]
		n >>= 4
	}
	return string(out)
}

type fakeFactory struct {
	c   gh.Client
	err error
}

func (f *fakeFactory) ForOrg(_ context.Context, _ string) (gh.Client, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.c, nil
}

type recordingAudit struct {
	mu   sync.Mutex
	rows []auditRow
	err  error
}

type auditRow struct {
	orgID    string
	actor    string
	target   string
	metadata map[string]any
}

func (a *recordingAudit) AppendPROpened(_ context.Context, orgID, actor, target string, metadata map[string]any) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.err != nil {
		return "", a.err
	}
	a.rows = append(a.rows, auditRow{orgID: orgID, actor: actor, target: target, metadata: metadata})
	return "audit-1", nil
}

// ---- happy path ------------------------------------------------------------

func TestOpenPR_HappyPath(t *testing.T) {
	diff := strings.Join([]string{
		"diff --git a/foo.txt b/foo.txt",
		"--- a/foo.txt",
		"+++ b/foo.txt",
		"@@ -1,2 +1,3 @@",
		" line1",
		" line2",
		"+line3",
		"",
	}, "\n")

	fc := &fakeClient{
		baseRef:   gh.Reference{Name: "refs/heads/main", SHA: "base-sha"},
		files:     map[string]gh.FileBlob{"foo.txt": {Path: "foo.txt", SHA: "old-blob", Body: []byte("line1\nline2\n")}},
		treeSHA:   "tree-sha-1",
		commitSHA: "commit-sha-1",
		pr:        gh.PR{Number: 42, HTMLURL: "https://github.com/acme/repo/pull/42"},
	}
	audit := &recordingAudit{}
	uc := usecase.NewOpenPRUsecase(&fakeFactory{c: fc}, audit)

	resp, err := uc.Run(context.Background(), domain.PROpenRequest{
		OrgID:         "org-1",
		WorkspaceID:   "ws-1",
		WorkflowRunID: "run-1",
		Repo:          "acme/repo",
		BranchBase:    "main",
		BranchName:    "nexis/fix-abc",
		CommitMsg:     "fix: foo",
		PatchDiff:     diff,
		PRTitle:       "Recover from foo",
		PRBody:        "auto generated",
	})
	require.NoError(t, err)
	require.Equal(t, 42, resp.PRNumber)
	require.Equal(t, "https://github.com/acme/repo/pull/42", resp.PRURL)
	require.Equal(t, "nexis/fix-abc", resp.Branch)
	require.Equal(t, "commit-sha-1", resp.HeadSHA)
	require.False(t, resp.OpenedAt.IsZero())

	// Call ordering must match the spec: GetRef → GetFile → CreateBlob →
	// CreateTree → CreateCommit → CreateRef → OpenPR.
	require.Len(t, fc.calls, 7)
	require.Contains(t, fc.calls[0], "GetRef:acme/repo:refs/heads/main")
	require.Contains(t, fc.calls[1], "GetFile:foo.txt")
	require.Contains(t, fc.calls[2], "CreateBlob:line1\nline2\nline3\n")
	require.Contains(t, fc.calls[3], "CreateTree:base-sha|foo.txt=")
	require.Contains(t, fc.calls[4], "CreateCommit:fix: foo:tree-sha-1:base-sha")
	require.Contains(t, fc.calls[5], "CreateRef:refs/heads/nexis/fix-abc:commit-sha-1")
	require.Contains(t, fc.calls[6], "OpenPR:acme/repo:nexis/fix-abc→main:Recover from foo")

	// Audit row landed.
	require.Len(t, audit.rows, 1)
	require.Equal(t, "org-1", audit.rows[0].orgID)
	require.Equal(t, "acme/repo#42", audit.rows[0].target)
	require.Equal(t, 42, audit.rows[0].metadata["pr_number"])
	require.Equal(t, "commit-sha-1", audit.rows[0].metadata["head_sha"])
}

func TestOpenPR_DefaultsBranchBaseToMain(t *testing.T) {
	diff := "diff --git a/foo.txt b/foo.txt\nnew file mode 100644\n--- /dev/null\n+++ b/foo.txt\n@@ -0,0 +1 @@\n+x\n"
	fc := &fakeClient{
		baseRef:   gh.Reference{SHA: "base-sha"},
		treeSHA:   "tree",
		commitSHA: "commit",
		pr:        gh.PR{Number: 1, HTMLURL: "u"},
	}
	uc := usecase.NewOpenPRUsecase(&fakeFactory{c: fc}, &recordingAudit{})
	_, err := uc.Run(context.Background(), domain.PROpenRequest{
		OrgID: "org-1", Repo: "a/b", BranchName: "topic", PRTitle: "t", PatchDiff: diff,
	})
	require.NoError(t, err)
	// First call must request refs/heads/main.
	require.Contains(t, fc.calls[0], "refs/heads/main")
}

func TestOpenPR_DefaultsCommitMsgToTitle(t *testing.T) {
	diff := "diff --git a/foo.txt b/foo.txt\nnew file mode 100644\n--- /dev/null\n+++ b/foo.txt\n@@ -0,0 +1 @@\n+x\n"
	fc := &fakeClient{
		baseRef: gh.Reference{SHA: "base"}, treeSHA: "t", commitSHA: "c",
		pr: gh.PR{Number: 1, HTMLURL: "u"},
	}
	uc := usecase.NewOpenPRUsecase(&fakeFactory{c: fc}, &recordingAudit{})
	_, err := uc.Run(context.Background(), domain.PROpenRequest{
		OrgID: "org-1", Repo: "a/b", BranchName: "x", PRTitle: "the title", PatchDiff: diff,
	})
	require.NoError(t, err)
	// CreateCommit call carries "the title".
	var sawCommit string
	for _, c := range fc.calls {
		if strings.HasPrefix(c, "CreateCommit:") {
			sawCommit = c
		}
	}
	require.Contains(t, sawCommit, "the title")
}

// ---- validation -----------------------------------------------------------

func TestOpenPR_RejectsMissingOrgID(t *testing.T) {
	uc := usecase.NewOpenPRUsecase(&fakeFactory{c: &fakeClient{}}, &recordingAudit{})
	_, err := uc.Run(context.Background(), domain.PROpenRequest{
		Repo: "a/b", BranchName: "x", PatchDiff: "diff", PRTitle: "t",
	})
	require.ErrorContains(t, err, "org_id required")
}

func TestOpenPR_RejectsBadRepoShape(t *testing.T) {
	uc := usecase.NewOpenPRUsecase(&fakeFactory{c: &fakeClient{}}, &recordingAudit{})
	_, err := uc.Run(context.Background(), domain.PROpenRequest{
		OrgID: "o", Repo: "not-a-slash", BranchName: "x", PatchDiff: "d", PRTitle: "t",
	})
	require.ErrorContains(t, err, "owner/name")
}

func TestOpenPR_PropagatesFactoryError(t *testing.T) {
	uc := usecase.NewOpenPRUsecase(&fakeFactory{err: errors.New("installation not connected")}, &recordingAudit{})
	_, err := uc.Run(context.Background(), domain.PROpenRequest{
		OrgID: "o", Repo: "a/b", BranchName: "x",
		PatchDiff: "diff --git a/x b/x\n@@ -0,0 +1 @@\n+x",
		PRTitle: "t",
	})
	require.ErrorContains(t, err, "installation not connected")
}

// ---- audit failures bubble up ---------------------------------------------

func TestOpenPR_AuditFailureSurfaces(t *testing.T) {
	diff := "diff --git a/foo.txt b/foo.txt\nnew file mode 100644\n--- /dev/null\n+++ b/foo.txt\n@@ -0,0 +1 @@\n+x\n"
	fc := &fakeClient{
		baseRef: gh.Reference{SHA: "base"}, treeSHA: "t", commitSHA: "c",
		pr: gh.PR{Number: 1, HTMLURL: "u"},
	}
	audit := &recordingAudit{err: errors.New("db down")}
	uc := usecase.NewOpenPRUsecase(&fakeFactory{c: fc}, audit)
	_, err := uc.Run(context.Background(), domain.PROpenRequest{
		OrgID: "org-1", Repo: "a/b", BranchName: "x", PRTitle: "t", PatchDiff: diff,
	})
	require.ErrorContains(t, err, "audit append")
}
