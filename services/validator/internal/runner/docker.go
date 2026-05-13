// Package runner abstracts the sandbox a /v1/validate call delegates to.
// Phase 4 ships the Docker-spawn implementation; Phase 7 will add a Modal
// alternative behind the same Runner interface.
package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

// Runner is the sandbox surface the HTTP handler depends on.
type Runner interface {
	Run(ctx context.Context, in RunRequest) (RunResult, error)
}

// RunRequest carries the optional Image + TimeoutMs overrides + the required
// RepoSHA + PatchDiff. PatchDiff is written to a tmpfs file inside the
// sandbox before pytest runs.
type RunRequest struct {
	RepoSHA   string
	PatchDiff string
	Image     string // optional override; falls back to defaultImage
	TimeoutMs int    // optional; default 60s
}

// RunResult is the JSON shape both the handler and the control-plane client
// consume. snake_case tags mirror the wire format.
type RunResult struct {
	TestsPassed bool    `json:"tests_passed"`
	TestCount   int     `json:"test_count"`
	FailCount   int     `json:"fail_count"`
	Coverage    float64 `json:"coverage"`
	DurationMs  int64   `json:"duration_ms"`
	Logs        string  `json:"logs"`
}

// Docker is the production Runner for Phase 4. It shells out to `docker run`
// with a hardened flag set: --rm + --network=none + --read-only + tmpfs +
// cap-drop=ALL + no-new-privileges. Phase 7 trades this for Modal.
type Docker struct {
	defaultImage string
	logger       *slog.Logger
}

// NewDocker constructs a Docker runner. image is the fully-qualified tag of
// the fixture image (e.g. "nexis/validator-fixture:latest").
func NewDocker(image string, logger *slog.Logger) *Docker {
	return &Docker{defaultImage: image, logger: logger}
}

// Run writes the patch to a tmpfs file, spawns a sandboxed container, and
// parses the pytest-json-report payload from stdout. The container is
// guaranteed to be cleaned up by `--rm`; the tmpfs directory is removed on
// return.
func (d *Docker) Run(ctx context.Context, in RunRequest) (RunResult, error) {
	img := d.defaultImage
	if in.Image != "" {
		img = in.Image
	}
	timeout := 60 * time.Second
	if in.TimeoutMs > 0 {
		timeout = time.Duration(in.TimeoutMs) * time.Millisecond
	}

	runID := uuid.NewString()
	tmpDir := filepath.Join(os.TempDir(), "nexis-validate-"+runID)
	if err := os.MkdirAll(tmpDir, 0o700); err != nil {
		return RunResult{}, err
	}
	defer os.RemoveAll(tmpDir)
	patchPath := filepath.Join(tmpDir, "patch.diff")
	if err := os.WriteFile(patchPath, []byte(in.PatchDiff), 0o600); err != nil {
		return RunResult{}, err
	}

	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	started := time.Now()

	// The container starts from a read-only rootfs but we mount tmpfs at
	// /tmp + /workspace so pytest can read/write fixtures. The shell command
	// inside applies the optional patch then runs pytest with
	// --json-report.
	cmd := exec.CommandContext(cctx, "docker", "run",
		"--rm",
		"--network=none",
		"--read-only",
		"--tmpfs", "/tmp:rw,nosuid,size=64m",
		"--tmpfs", "/workspace:rw,nosuid,size=128m",
		"--memory=512m",
		"--cpus=1.0",
		"--pids-limit=128",
		"--security-opt=no-new-privileges",
		"--cap-drop=ALL",
		"-v", patchPath+":/workspace/patch.diff:ro",
		"-e", "CI=true",
		"--label", "nexis.validator.run-id="+runID,
		img,
		"/bin/sh", "-c",
		// git apply tolerates an empty patch; pytest-json-report writes the
		// report. We `cat` it at the end so stdout carries the JSON.
		"cp -r /app/. /workspace/ 2>/dev/null; cd /workspace && (test -s patch.diff && git apply patch.diff || true) && pytest --json-report --json-report-file=/tmp/report.json -q ; cat /tmp/report.json 2>/dev/null",
	)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	dur := time.Since(started).Milliseconds()

	res := RunResult{
		Logs:       out.String() + "\n--- stderr ---\n" + stderr.String(),
		DurationMs: dur,
	}
	// pytest writes the JSON report after the test summary, so we scan from
	// the end backwards for the first { to find it.
	if report := extractReport(out.Bytes()); report != nil {
		res.TestCount = report.Summary.Total
		res.FailCount = report.Summary.Failed
		res.TestsPassed = report.Summary.Failed == 0 && report.Summary.Total > 0
	}
	if runErr != nil {
		return res, fmt.Errorf("docker run: %w", runErr)
	}
	return res, nil
}

// pytestReport is the subset of pytest-json-report we read.
type pytestReport struct {
	Summary struct {
		Passed int `json:"passed"`
		Failed int `json:"failed"`
		Total  int `json:"total"`
	} `json:"summary"`
}

// extractReport finds the first JSON object in stdout whose `summary` is
// decodable. Returns nil when no report is present (sandbox failed to
// produce one). We scan top-down rather than bottom-up because the fixture
// image's `cat /tmp/report.json` always lands at the end of stdout — but
// being defensive lets a flaky pytest invocation degrade gracefully.
func extractReport(out []byte) *pytestReport {
	for i := 0; i < len(out); i++ {
		if out[i] != '{' {
			continue
		}
		dec := json.NewDecoder(bytes.NewReader(out[i:]))
		var rep pytestReport
		if err := dec.Decode(&rep); err == nil && rep.Summary.Total >= 0 {
			// Only accept the report if `summary` is present; otherwise we
			// might be decoding an unrelated `{}` from pytest's traceback.
			if rep.Summary.Total > 0 || rep.Summary.Failed > 0 || rep.Summary.Passed > 0 {
				return &rep
			}
		}
	}
	return nil
}
