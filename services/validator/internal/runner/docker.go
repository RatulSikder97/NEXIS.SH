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
	"os/exec"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Runner is the sandbox surface the HTTP handler depends on.
type Runner interface {
	Run(ctx context.Context, in RunRequest) (RunResult, error)
}

// RunRequest carries the optional Image + TimeoutMs overrides + the required
// RepoSHA + PatchDiff. PatchDiff is piped into the sandbox on stdin and
// written to a tmpfs file there before pytest runs.
type RunRequest struct {
	RepoSHA   string
	PatchDiff string
	Image     string // optional override; falls back to defaultImage
	TimeoutMs int    // optional; default 60s
}

// Sentinels the sandbox shell prints so the Go side can tell "no patch",
// "patch applied" and "patch rejected" apart from a single stdout stream.
const (
	patchAppliedMarker = "__NEXIS_PATCH_APPLIED__"
	patchFailedMarker  = "__NEXIS_PATCH_FAILED__"
	patchEmptyMarker   = "__NEXIS_PATCH_EMPTY__"
)

// RunResult is the JSON shape both the handler and the control-plane client
// consume. snake_case tags mirror the wire format.
type RunResult struct {
	TestsPassed bool    `json:"tests_passed"`
	TestCount   int     `json:"test_count"`
	FailCount   int     `json:"fail_count"`
	Coverage    float64 `json:"coverage"`
	DurationMs  int64   `json:"duration_ms"`
	Logs        string  `json:"logs"`

	// PatchApplied reports whether `git apply` accepted a non-empty patch.
	// It exists because the old command swallowed apply failures with
	// `|| true`, so pytest ran against the UNPATCHED tree and the caller was
	// told tests_passed=true — a green result that said nothing about the
	// patch. A patch that does not apply is now a failed validation.
	PatchApplied bool   `json:"patch_applied"`
	PatchError   string `json:"patch_error,omitempty"`
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

// Run spawns a sandboxed container, pipes the patch to it on stdin, and
// parses the pytest-json-report payload from stdout. The container is
// guaranteed to be cleaned up by `--rm`, and nothing is written to this
// service's own filesystem.
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

	// The patch is piped in on stdin, NOT bind-mounted.
	//
	// This service runs in a container and drives the HOST's Docker daemon
	// through the mounted socket, so `-v /tmp/x/patch.diff:...` is resolved
	// by the daemon against the HOST filesystem. The file written here lives
	// inside this container; the host has no such path, and Docker's
	// bind-mount behaviour is to create an empty DIRECTORY in its place.
	// git duly reported "failed to read patch: Is a directory" — meaning
	// every validation had been running against an UNPATCHED tree while
	// reporting a green suite.
	//
	// Piping needs no shared filesystem, so it is correct both here and in a
	// bare-metal deployment.
	//
	// git apply also rejects a patch whose last line has no newline
	// ("corrupt patch at line N"), which model-authored diffs routinely
	// lack, so that is normalised here as well.
	patchBody := in.PatchDiff
	if patchBody != "" && !strings.HasSuffix(patchBody, "\n") {
		patchBody += "\n"
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
		"-i", // the patch arrives on stdin
		"-e", "CI=true",
		"--label", "nexis.validator.run-id="+runID,
		img,
		"/bin/sh", "-c",
		// An empty patch is legitimate (validation of the baseline suite);
		// a non-empty patch that will not apply is not, so its exit status
		// is captured and printed as a sentinel the Go side parses. pytest
		// still runs either way, because the failure logs are more useful
		// than a bare "did not apply".
		"cat > /workspace/patch.diff; cp -r /app/. /workspace/ 2>/dev/null; cd /workspace && "+
			"if test -s patch.diff; then "+
			"  if git apply --verbose patch.diff 2>&1; then echo '"+patchAppliedMarker+"'; "+
			"  else echo '"+patchFailedMarker+"'; fi; "+
			"else echo '"+patchEmptyMarker+"'; fi; "+
			"pytest --json-report --json-report-file=/tmp/report.json -q ; cat /tmp/report.json 2>/dev/null",
	)
	var out, stderr bytes.Buffer
	cmd.Stdin = strings.NewReader(patchBody)
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
	hadPatch := strings.TrimSpace(in.PatchDiff) != ""
	res.PatchApplied, res.PatchError = applyStatus(out.String(), hadPatch)

	if report := extractReport(out.Bytes()); report != nil {
		res.TestCount = report.Summary.Total
		res.FailCount = report.Summary.Failed
		res.TestsPassed = report.Summary.Failed == 0 && report.Summary.Total > 0
	}
	// A green suite over an unpatched tree is not a validated patch.
	if hadPatch && !res.PatchApplied {
		res.TestsPassed = false
		if res.PatchError == "" {
			res.PatchError = "patch did not apply"
		}
	}
	if runErr != nil {
		return res, fmt.Errorf("docker run: %w", runErr)
	}
	return res, nil
}

// applyStatus reads the sandbox's patch markers out of stdout.
//
// Split out of Run so the decision table is testable without Docker — this
// is the logic that decides whether a green pytest run is evidence about the
// patch or merely about the baseline tree.
func applyStatus(stdout string, hadPatch bool) (applied bool, errMsg string) {
	switch {
	case strings.Contains(stdout, patchFailedMarker):
		// Carry git's own line back — "corrupt patch at line 28" and
		// "does not match index" need completely different fixes, and
		// without the reason the caller is left guessing.
		if reason := gitApplyReason(stdout); reason != "" {
			return false, "git apply rejected the patch: " + reason
		}
		return false, "git apply rejected the patch"
	case strings.Contains(stdout, patchAppliedMarker):
		return true, ""
	case strings.Contains(stdout, patchEmptyMarker):
		// The sandbox saw an empty patch file. That is only consistent with
		// a caller who sent no patch; if one was sent, something dropped it.
		if hadPatch {
			return false, "patch was supplied but the sandbox saw an empty patch file"
		}
		return true, ""
	default:
		// No marker at all — the shell died before reaching the apply step.
		if hadPatch {
			return false, "patch application status unknown — sandbox produced no marker"
		}
		return true, ""
	}
}

// gitApplyReason pulls git's own diagnostic out of the sandbox stdout. git
// apply prints "error: ..." / "fatal: ..." lines before we echo the failure
// marker, so the last such line before the marker is the reason.
func gitApplyReason(stdout string) string {
	cut := strings.Index(stdout, patchFailedMarker)
	if cut < 0 {
		cut = len(stdout)
	}
	var last string
	for _, line := range strings.Split(stdout[:cut], "\n") {
		l := strings.TrimSpace(line)
		if strings.HasPrefix(l, "error:") || strings.HasPrefix(l, "fatal:") {
			last = l
		}
	}
	return last
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
