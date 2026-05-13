// Package github wraps the go-github v60 client behind a narrow interface
// the OpenPRUsecase depends on. Wrapping (rather than depending on go-github
// directly) means the usecase can be unit-tested without mocking out the
// entire GitHub SDK surface.
//
// The package owns two concerns:
//
//   1. ClientBuilder — given an org id, return a *gh.Client signed with the
//      org's GitHub App installation. Uses ghinstallation/v2 for the JWT
//      handshake.
//   2. Client interface — the narrow set of GitHub operations OpenPR needs
//      (read ref, create commit, create branch, open PR). Tests inject a
//      fake implementation; production wires NewClient.
package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/bradleyfalzon/ghinstallation/v2"
	gh "github.com/google/go-github/v60/github"
)

// IntegrationsReader is the dependency the builder needs to map an org id
// to a GitHub installation id. The control-plane stores this row in the
// integrations table — see internal/adapter/repo/integrations_repo.go.
type IntegrationsReader interface {
	InstallationID(ctx context.Context, orgID string) (int64, error)
}

// Commit represents a created git commit on GitHub. Only the fields the
// usecase needs are surfaced; tests can construct values directly.
type Commit struct {
	SHA     string
	Message string
}

// Reference is a thin wrapper around a git ref (branch tip).
type Reference struct {
	Name string // refs/heads/main, refs/heads/nexis/fix-abc, …
	SHA  string
}

// PR represents an opened pull request.
type PR struct {
	Number  int
	HTMLURL string
}

// FileBlob is the content of an existing file fetched via GetContents.
type FileBlob struct {
	Path string
	SHA  string
	Body []byte
}

// Client is the operations surface OpenPR depends on. Each method maps 1:1
// to a single GitHub REST call. Implementations:
//
//   - ghClient (this package, production) wraps go-github.
//   - fakeClient (open_pr_test.go) records calls in memory.
type Client interface {
	GetRef(ctx context.Context, owner, repo, ref string) (Reference, error)
	GetFile(ctx context.Context, owner, repo, path, ref string) (FileBlob, error)
	CreateBlob(ctx context.Context, owner, repo string, body []byte) (string, error)
	CreateTree(ctx context.Context, owner, repo, baseTreeSHA string, entries []TreeEntry) (string, error)
	CreateCommit(ctx context.Context, owner, repo, message, treeSHA string, parentSHAs []string) (Commit, error)
	CreateRef(ctx context.Context, owner, repo, ref, sha string) (Reference, error)
	OpenPR(ctx context.Context, owner, repo, title, body, head, base string) (PR, error)
}

// TreeEntry is one item written to a git tree. Mode "100644" = regular file.
type TreeEntry struct {
	Path    string
	Mode    string // "100644" | "100755" | "040000"
	Type    string // "blob" | "tree"
	BlobSHA string // for blob entries
}

// ClientBuilder constructs an installation-authenticated *gh.Client per org.
// Reads the private key once at construction so per-org transport setup is
// just a JWT mint round-trip.
type ClientBuilder struct {
	appID            int64
	privateKeyPEM    []byte
	integrationsRepo IntegrationsReader
}

// NewBuilder reads the private key file and validates the appID. Returns
// an error when the key file is missing or appID is 0.
func NewBuilder(appID int64, keyPath string, repo IntegrationsReader) (*ClientBuilder, error) {
	if appID == 0 {
		return nil, errors.New("github: app id is zero")
	}
	if keyPath == "" {
		return nil, errors.New("github: private key path empty")
	}
	if repo == nil {
		return nil, errors.New("github: integrations reader nil")
	}
	key, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("read private key %s: %w", keyPath, err)
	}
	return &ClientBuilder{appID: appID, privateKeyPEM: key, integrationsRepo: repo}, nil
}

// ForOrg returns a go-github client authenticated as the GitHub App
// installation associated with orgID. The installation id is read fresh
// every call so org-level token rotation lands on the next request.
func (b *ClientBuilder) ForOrg(ctx context.Context, orgID string) (*gh.Client, error) {
	installID, err := b.integrationsRepo.InstallationID(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("installation lookup: %w", err)
	}
	if installID == 0 {
		return nil, errors.New("github installation not connected")
	}
	tr, err := ghinstallation.New(http.DefaultTransport, b.appID, installID, b.privateKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("ghinstallation: %w", err)
	}
	return gh.NewClient(&http.Client{Transport: tr}), nil
}

// NewClient wraps a *gh.Client in a Client. Use this for production wiring;
// tests use the fakeClient in open_pr_test.go.
func NewClient(c *gh.Client) Client {
	return &ghClient{c: c}
}

// ghClient is the go-github-backed Client implementation.
type ghClient struct {
	c *gh.Client
}

func (g *ghClient) GetRef(ctx context.Context, owner, repo, ref string) (Reference, error) {
	r, _, err := g.c.Git.GetRef(ctx, owner, repo, ref)
	if err != nil {
		return Reference{}, err
	}
	return Reference{Name: r.GetRef(), SHA: r.GetObject().GetSHA()}, nil
}

func (g *ghClient) GetFile(ctx context.Context, owner, repo, path, ref string) (FileBlob, error) {
	file, _, _, err := g.c.Repositories.GetContents(ctx, owner, repo, path, &gh.RepositoryContentGetOptions{Ref: ref})
	if err != nil {
		return FileBlob{}, err
	}
	if file == nil {
		return FileBlob{}, fmt.Errorf("github: %s/%s/%s not a file", owner, repo, path)
	}
	body, err := file.GetContent()
	if err != nil {
		return FileBlob{}, err
	}
	return FileBlob{Path: file.GetPath(), SHA: file.GetSHA(), Body: []byte(body)}, nil
}

func (g *ghClient) CreateBlob(ctx context.Context, owner, repo string, body []byte) (string, error) {
	enc := "utf-8"
	content := string(body)
	blob, _, err := g.c.Git.CreateBlob(ctx, owner, repo, &gh.Blob{
		Content:  &content,
		Encoding: &enc,
	})
	if err != nil {
		return "", err
	}
	return blob.GetSHA(), nil
}

func (g *ghClient) CreateTree(ctx context.Context, owner, repo, baseTreeSHA string, entries []TreeEntry) (string, error) {
	te := make([]*gh.TreeEntry, 0, len(entries))
	for _, e := range entries {
		path := e.Path
		mode := e.Mode
		typ := e.Type
		sha := e.BlobSHA
		te = append(te, &gh.TreeEntry{
			Path: &path,
			Mode: &mode,
			Type: &typ,
			SHA:  &sha,
		})
	}
	tree, _, err := g.c.Git.CreateTree(ctx, owner, repo, baseTreeSHA, te)
	if err != nil {
		return "", err
	}
	return tree.GetSHA(), nil
}

func (g *ghClient) CreateCommit(ctx context.Context, owner, repo, message, treeSHA string, parentSHAs []string) (Commit, error) {
	parents := make([]*gh.Commit, 0, len(parentSHAs))
	for _, p := range parentSHAs {
		sha := p
		parents = append(parents, &gh.Commit{SHA: &sha})
	}
	msg := message
	tree := treeSHA
	commit, _, err := g.c.Git.CreateCommit(ctx, owner, repo, &gh.Commit{
		Message: &msg,
		Tree:    &gh.Tree{SHA: &tree},
		Parents: parents,
	}, nil)
	if err != nil {
		return Commit{}, err
	}
	return Commit{SHA: commit.GetSHA(), Message: commit.GetMessage()}, nil
}

func (g *ghClient) CreateRef(ctx context.Context, owner, repo, ref, sha string) (Reference, error) {
	refName := ref
	objSHA := sha
	r, _, err := g.c.Git.CreateRef(ctx, owner, repo, &gh.Reference{
		Ref:    &refName,
		Object: &gh.GitObject{SHA: &objSHA},
	})
	if err != nil {
		return Reference{}, err
	}
	return Reference{Name: r.GetRef(), SHA: r.GetObject().GetSHA()}, nil
}

func (g *ghClient) OpenPR(ctx context.Context, owner, repo, title, body, head, base string) (PR, error) {
	t := title
	b := body
	h := head
	ba := base
	pr, _, err := g.c.PullRequests.Create(ctx, owner, repo, &gh.NewPullRequest{
		Title: &t,
		Body:  &b,
		Head:  &h,
		Base:  &ba,
	})
	if err != nil {
		return PR{}, err
	}
	return PR{Number: pr.GetNumber(), HTMLURL: pr.GetHTMLURL()}, nil
}
