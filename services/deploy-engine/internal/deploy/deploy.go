// Package deploy orchestrates the preview-deployment pipeline:
// clone -> detect/generate Dockerfile -> docker build -> docker run ->
// HTTP health poll. It shells out to the docker CLI exactly like the
// validator's runner does, so the same socket-mount deployment model works.
//
// WARNING: like services/validator, this service drives the host docker
// socket. Never expose it outside the dev compose network.
package deploy

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nexis-eco/nexis/services/deploy-engine/internal/detect"
	"github.com/nexis-eco/nexis/services/deploy-engine/internal/dockerfile"
	"github.com/nexis-eco/nexis/services/deploy-engine/internal/gitclone"
)

// Deployer is the surface the HTTP handler depends on, mirroring how the
// validator handler depends on runner.Runner.
type Deployer interface {
	// Deploy runs the full pipeline. A non-nil error means an internal
	// failure (500 path); pipeline failures (bad clone, failed build,
	// unhealthy container) come back as err == nil with
	// Result.Status == StatusFailed.
	Deploy(ctx context.Context, in Request) (Result, error)
	// Stop force-removes the named container.
	Stop(ctx context.Context, containerName string) error
}

// Statuses appearing in the wire-format "status" field.
const (
	StatusRunning = "running"
	StatusFailed  = "failed"
	StatusStopped = "stopped"
)

// Request carries one /v1/deploy invocation. DeploymentID is minted by the
// control-plane and drives container/image naming.
type Request struct {
	DeploymentID string
	ProjectID    string
	OrgID        string
	Repo         string // "owner/name"
	Branch       string
	CommitSHA    string // optional; empty = branch HEAD
	GitHubToken  string // optional; empty = anonymous https (public repos)
	TimeoutMs    int
}

// Result is the wire-format response body for both the 200 and 422 paths.
// URL + Port are pointers so the failure shape serializes them as null per
// the API contract.
type Result struct {
	DeploymentID     string    `json:"deployment_id"`
	Status           string    `json:"status"`
	URL              *string   `json:"url"`
	Port             *int      `json:"port"`
	ImageTag         string    `json:"image_tag"`
	DockerfileSource string    `json:"dockerfile_source"`
	DetectedStack    string    `json:"detected_stack"`
	Error            string    `json:"error,omitempty"`
	BuildLog         string    `json:"build_log"`
	ContainerLog     string    `json:"container_log"`
	StartedAt        time.Time `json:"started_at"`
	FinishedAt       time.Time `json:"finished_at"`
}

const (
	defaultTimeout = 120 * time.Second
	// buildBudgetFraction caps how much of the request budget the docker
	// build may consume, leaving the remainder for run + health polling.
	buildBudgetFraction = 0.7
	healthPollInterval  = 500 * time.Millisecond
	// containerNameShortLen chars of the deployment UUID go into the
	// container name — the first UUID segment, no trailing hyphen.
	containerNameShortLen = 8
	imageTagShortSHALen   = 12
)

// Engine is the docker-CLI implementation of Deployer.
type Engine struct {
	logger *slog.Logger
	// healthHost is the hostname health polls dial. "localhost" when the
	// engine runs natively; "host.docker.internal" when it runs inside
	// compose with the docker socket mounted (published ports land on the
	// host, not in this container's netns).
	healthHost string
}

// NewEngine constructs an Engine. healthHost falls back to "localhost" when
// empty.
func NewEngine(healthHost string, logger *slog.Logger) *Engine {
	if healthHost == "" {
		healthHost = "localhost"
	}
	return &Engine{logger: logger, healthHost: healthHost}
}

var _ Deployer = (*Engine)(nil)

// ContainerName derives the docker container name for a deployment id.
// Exported so the handler can resolve names for stop calls on tracked
// deployments.
func ContainerName(deploymentID string) string {
	short := deploymentID
	if len(short) > containerNameShortLen {
		short = short[:containerNameShortLen]
	}
	return "nexis-preview-" + short
}

// Deploy implements the pipeline. See Deployer for the error contract.
func (e *Engine) Deploy(ctx context.Context, in Request) (Result, error) {
	started := time.Now()
	timeout := defaultTimeout
	if in.TimeoutMs > 0 {
		timeout = time.Duration(in.TimeoutMs) * time.Millisecond
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	res := Result{
		DeploymentID:  in.DeploymentID,
		Status:        StatusFailed,
		DetectedStack: string(detect.StackUnknown),
		StartedAt:     started.UTC(),
	}
	fail := func(msg string) (Result, error) {
		res.Error = msg
		res.FinishedAt = time.Now().UTC()
		e.logger.Warn("deploy failed", "deployment_id", in.DeploymentID, "repo", in.Repo, "err", msg)
		return res, nil
	}

	// ---- 1. Clone -------------------------------------------------------
	tmpDir, err := os.MkdirTemp("", "nexis-deploy-")
	if err != nil {
		return res, fmt.Errorf("mkdir temp: %w", err)
	}
	defer os.RemoveAll(tmpDir)
	workDir := filepath.Join(tmpDir, "src")

	if in.GitHubToken != "" {
		// Forensics convention: only ever log the token's sha256, never
		// the token (see internal/adapter/integration/github/provider.go
		// on the control-plane side).
		e.logger.Info("cloning with installation token",
			"deployment_id", in.DeploymentID, "repo", in.Repo,
			"token_sha256", gitclone.TokenSHA256(in.GitHubToken))
	}
	headSHA, cloneOut, err := gitclone.Clone(cctx, gitclone.Options{
		Repo:      in.Repo,
		Branch:    in.Branch,
		CommitSHA: in.CommitSHA,
		Token:     in.GitHubToken,
		Dir:       workDir,
	})
	if err != nil {
		res.BuildLog = cloneOut
		return fail("clone failed: " + err.Error())
	}

	// ---- 2. Dockerfile: repo-provided or generated ----------------------
	stack := detect.Detect(workDir)
	res.DetectedStack = string(stack)
	var containerPort int
	if detect.HasRootDockerfile(workDir) {
		res.DockerfileSource = "repo"
		raw, err := os.ReadFile(filepath.Join(workDir, "Dockerfile"))
		if err != nil {
			return res, fmt.Errorf("read repo Dockerfile: %w", err)
		}
		containerPort = dockerfile.ExposedPort(string(raw), dockerfile.DefaultPort(stack))
	} else {
		res.DockerfileSource = "generated"
		content, port, err := dockerfile.Generate(workDir, stack)
		if err != nil {
			return fail(err.Error())
		}
		if err := os.WriteFile(filepath.Join(workDir, "Dockerfile"), []byte(content), 0o644); err != nil {
			return res, fmt.Errorf("write generated Dockerfile: %w", err)
		}
		containerPort = port
	}

	// ---- 3. Build -------------------------------------------------------
	shortSHA := headSHA
	if len(shortSHA) > imageTagShortSHALen {
		shortSHA = shortSHA[:imageTagShortSHALen]
	}
	res.ImageTag = fmt.Sprintf("nexis-preview-%s:%s", in.ProjectID, shortSHA)

	buildBudget := time.Duration(buildBudgetFraction * float64(time.Until(deadlineOf(cctx))))
	bctx, bcancel := context.WithTimeout(cctx, buildBudget)
	buildOut, err := runDocker(bctx, in.GitHubToken, "build", "-t", res.ImageTag, workDir)
	bcancel()
	res.BuildLog = buildOut
	if err != nil {
		return fail("docker build failed: " + err.Error())
	}

	// ---- 4. Run ---------------------------------------------------------
	hostPort, err := freeHostPort()
	if err != nil {
		return res, err
	}
	name := ContainerName(in.DeploymentID)
	// Best-effort removal of a stale container from a previous attempt at
	// the same deployment id; ignore "no such container".
	_, _ = runDocker(cctx, "", "rm", "-f", name)

	runOut, err := runDocker(cctx, in.GitHubToken, "run", "-d",
		"--name", name,
		"--memory=512m",
		"--cpus=1.0",
		"--pids-limit=256",
		"--security-opt=no-new-privileges",
		"--label", "nexis.deploy.id="+in.DeploymentID,
		// Heroku-style apps bind $PORT; exporting the detected container
		// port keeps them listening where we publish. Apps that ignore
		// PORT are unaffected.
		"-e", fmt.Sprintf("PORT=%d", containerPort),
		"-p", fmt.Sprintf("%d:%d", hostPort, containerPort),
		res.ImageTag,
	)
	if err != nil {
		res.ContainerLog = runOut
		return fail("docker run failed: " + err.Error())
	}
	containerID := strings.TrimSpace(runOut)

	// ---- 5. Health poll -------------------------------------------------
	if err := e.pollHealthy(cctx, hostPort); err != nil {
		logs, _ := runDocker(context.WithoutCancel(cctx), "", "logs", name)
		res.ContainerLog = logs
		_, _ = runDocker(context.WithoutCancel(cctx), "", "rm", "-f", name)
		return fail("container never became healthy: " + err.Error())
	}
	if logs, err := runDocker(cctx, "", "logs", name); err == nil {
		res.ContainerLog = logs
	}

	url := fmt.Sprintf("http://localhost:%d", hostPort)
	res.Status = StatusRunning
	res.URL = &url
	res.Port = &hostPort
	res.Error = ""
	res.FinishedAt = time.Now().UTC()
	e.logger.Info("deploy running",
		"deployment_id", in.DeploymentID, "repo", in.Repo, "container", containerID,
		"image", res.ImageTag, "url", url, "stack", res.DetectedStack, "dockerfile", res.DockerfileSource)
	return res, nil
}

// Stop force-removes the container. "No such container" is treated as
// success — the goal state (container gone) already holds.
func (e *Engine) Stop(ctx context.Context, containerName string) error {
	out, err := runDocker(ctx, "", "rm", "-f", containerName)
	if err != nil && !strings.Contains(strings.ToLower(out), "no such container") {
		return fmt.Errorf("docker rm -f %s: %w: %s", containerName, err, out)
	}
	return nil
}

// pollHealthy GETs the app root every healthPollInterval until any HTTP
// response with status 200-499 arrives (a 404 still proves the process is
// alive and listening) or the context budget runs out.
func (e *Engine) pollHealthy(ctx context.Context, hostPort int) error {
	client := &http.Client{Timeout: 2 * time.Second}
	url := fmt.Sprintf("http://%s:%d/", e.healthHost, hostPort)
	var lastErr error = fmt.Errorf("no poll attempts completed")
	ticker := time.NewTicker(healthPollInterval)
	defer ticker.Stop()
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode <= 499 {
				return nil
			}
			lastErr = fmt.Errorf("app answered with http %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("health poll timed out: last error: %w", lastErr)
		case <-ticker.C:
		}
	}
}

// runDocker shells out to the docker CLI with combined output capture.
// Output + error strings are token-redacted defensively — a build log can
// echo anything the repo's Dockerfile prints.
func runDocker(ctx context.Context, token string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	out := gitclone.Redact(buf.String(), token)
	if err != nil {
		return out, fmt.Errorf("%s", gitclone.Redact(err.Error(), token))
	}
	return out, nil
}

// deadlineOf returns the context deadline, falling back to now+default when
// absent (cannot happen for Deploy's cctx, but keeps the math total).
func deadlineOf(ctx context.Context) time.Time {
	if d, ok := ctx.Deadline(); ok {
		return d
	}
	return time.Now().Add(defaultTimeout)
}
