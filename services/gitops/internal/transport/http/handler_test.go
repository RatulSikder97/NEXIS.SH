package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	gh "github.com/nexis-eco/nexis/services/gitops/internal/adapter/github"
	"github.com/nexis-eco/nexis/services/gitops/internal/domain"
	httpsrv "github.com/nexis-eco/nexis/services/gitops/internal/transport/http"
	"github.com/nexis-eco/nexis/services/gitops/internal/usecase"
)

// stubClient is a minimal gh.Client implementation that lets the handler
// test run through the usecase end-to-end without depending on go-github.
type stubClient struct {
	mu        sync.Mutex
	base      gh.Reference
	files     map[string]gh.FileBlob
	commitSHA string
	pr        gh.PR
}

func (s *stubClient) GetRef(_ context.Context, _, _, _ string) (gh.Reference, error) {
	return s.base, nil
}
func (s *stubClient) GetFile(_ context.Context, _, _, path, _ string) (gh.FileBlob, error) {
	if b, ok := s.files[path]; ok {
		return b, nil
	}
	return gh.FileBlob{}, errors.New("not found")
}
func (s *stubClient) CreateBlob(_ context.Context, _, _ string, body []byte) (string, error) {
	return "blob-" + sumNibble(string(body)), nil
}
func (s *stubClient) CreateTree(_ context.Context, _, _, _ string, _ []gh.TreeEntry) (string, error) {
	return "tree", nil
}
func (s *stubClient) CreateCommit(_ context.Context, _, _, _, _ string, _ []string) (gh.Commit, error) {
	return gh.Commit{SHA: s.commitSHA, Message: "msg"}, nil
}
func (s *stubClient) CreateRef(_ context.Context, _, _, ref, sha string) (gh.Reference, error) {
	return gh.Reference{Name: ref, SHA: sha}, nil
}
func (s *stubClient) OpenPR(_ context.Context, _, _, _, _, _, _ string) (gh.PR, error) {
	return s.pr, nil
}

func sumNibble(s string) string {
	var n uint64
	for _, c := range s {
		n = (n*31 + uint64(c)) % 0xFFFFFFFF
	}
	out := []byte("00000000")
	for i := 0; i < 8; i++ {
		out[i] = "0123456789abcdef"[n&0xf]
		n >>= 4
	}
	return string(out)
}

type stubFactory struct{ c gh.Client }

func (s *stubFactory) ForOrg(_ context.Context, _ string) (gh.Client, error) { return s.c, nil }

type stubAudit struct{ called bool }

func (s *stubAudit) AppendPROpened(_ context.Context, _, _, _ string, _ map[string]any) (string, error) {
	s.called = true
	return "audit-1", nil
}

func buildHandler(t *testing.T, token string) (http.Handler, *stubAudit) {
	t.Helper()
	c := &stubClient{
		base:      gh.Reference{SHA: "base"},
		files:     map[string]gh.FileBlob{"foo.txt": {Path: "foo.txt", Body: []byte("hello\n")}},
		commitSHA: "commit",
		pr:        gh.PR{Number: 7, HTMLURL: "https://github.com/acme/repo/pull/7"},
	}
	audit := &stubAudit{}
	uc := usecase.NewOpenPRUsecase(&stubFactory{c: c}, audit)
	return httpsrv.New(httpsrv.Deps{OpenPR: uc, AuthToken: token}), audit
}

func TestHandler_Healthz(t *testing.T) {
	h, _ := buildHandler(t, "secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/healthz", nil))
	require.Equal(t, 200, rr.Code)
	require.Contains(t, rr.Body.String(), "ok")
}

func TestHandler_OpenPR_HappyPath_ReturnsCreated(t *testing.T) {
	h, audit := buildHandler(t, "secret")
	diff := strings.Join([]string{
		"diff --git a/foo.txt b/foo.txt",
		"--- a/foo.txt",
		"+++ b/foo.txt",
		"@@ -1 +1,2 @@",
		" hello",
		"+world",
		"",
	}, "\n")
	body, _ := json.Marshal(domain.PROpenRequest{
		OrgID: "org-1", WorkspaceID: "ws-1", WorkflowRunID: "run-1",
		Repo: "acme/repo", BranchBase: "main", BranchName: "topic",
		CommitMsg: "fix", PatchDiff: diff,
		PRTitle: "PR title", PRBody: "PR body",
	})
	req := httptest.NewRequest("POST", "/v1/gitops/open-pr", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	require.Equal(t, http.StatusCreated, rr.Code, "body=%s", rr.Body.String())
	var resp domain.PROpenResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	require.Equal(t, 7, resp.PRNumber)
	require.Equal(t, "https://github.com/acme/repo/pull/7", resp.PRURL)
	require.True(t, audit.called)
}

func TestHandler_OpenPR_RejectsMissingBearer(t *testing.T) {
	h, _ := buildHandler(t, "secret")
	req := httptest.NewRequest("POST", "/v1/gitops/open-pr", strings.NewReader("{}"))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	require.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestHandler_OpenPR_RejectsWrongBearer(t *testing.T) {
	h, _ := buildHandler(t, "secret")
	req := httptest.NewRequest("POST", "/v1/gitops/open-pr", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer wrong")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	require.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestHandler_OpenPR_RejectsBadJSON(t *testing.T) {
	h, _ := buildHandler(t, "secret")
	req := httptest.NewRequest("POST", "/v1/gitops/open-pr", strings.NewReader("{"))
	req.Header.Set("Authorization", "Bearer secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	require.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestHandler_OpenPR_RejectsMissingFields(t *testing.T) {
	h, _ := buildHandler(t, "secret")
	body, _ := json.Marshal(domain.PROpenRequest{OrgID: "o"})
	req := httptest.NewRequest("POST", "/v1/gitops/open-pr", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	require.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestHandler_OpenPR_RejectsBadDiff(t *testing.T) {
	h, _ := buildHandler(t, "secret")
	body, _ := json.Marshal(domain.PROpenRequest{
		OrgID: "o", Repo: "a/b", BranchName: "x", PRTitle: "t",
		PatchDiff: "diff --git a/img.png b/img.png\nBinary files differ\n",
	})
	req := httptest.NewRequest("POST", "/v1/gitops/open-pr", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	require.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestHandler_ConfigWithEmptyToken_Returns500(t *testing.T) {
	h, _ := buildHandler(t, "")
	req := httptest.NewRequest("POST", "/v1/gitops/open-pr", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer whatever")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	require.Equal(t, http.StatusInternalServerError, rr.Code)
}
