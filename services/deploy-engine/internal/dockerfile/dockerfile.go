// Package dockerfile generates a runnable Dockerfile for repos that don't
// ship one, and parses EXPOSE out of repo-provided Dockerfiles so the deploy
// engine knows which container port to publish.
//
// Generation is intentionally conservative: pick the overwhelmingly common
// default for each stack and fail loudly for anything unrecognized — an
// unrecognized-stack failure becomes an incident the Backend agent can
// resolve by writing a real Dockerfile.
package dockerfile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/nexis-eco/nexis/services/deploy-engine/internal/detect"
)

// Default container ports per stack. These match the EXPOSE line each
// generator emits.
const (
	PortNode   = 3000
	PortPython = 8000
	PortGo     = 8080
	PortStatic = 80
	// PortFallback is used when a repo-provided Dockerfile has no EXPOSE
	// line and the stack gives no better hint.
	PortFallback = 8080
)

// ErrUnknownStack is wrapped by Generate when no stack marker matched. The
// deploy engine surfaces it verbatim in the 422 response so the upstream
// incident carries a specific, actionable message.
var ErrUnknownStack = fmt.Errorf(
	"no Dockerfile and no recognized stack marker (package.json/requirements.txt/go.mod/index.html) at repo root")

// Generate produces Dockerfile content + the container port it EXPOSEs for
// the given stack. Returns ErrUnknownStack for detect.StackUnknown.
func Generate(dir string, stack detect.Stack) (content string, containerPort int, err error) {
	switch stack {
	case detect.StackNode:
		c, e := generateNode(dir)
		return c, PortNode, e
	case detect.StackPython:
		c, e := generatePython(dir)
		return c, PortPython, e
	case detect.StackGo:
		return generateGo(), PortGo, nil
	case detect.StackStatic:
		return generateStatic(), PortStatic, nil
	default:
		return "", 0, ErrUnknownStack
	}
}

// DefaultPort maps a stack to the container port its generated Dockerfile
// would EXPOSE. Used as the EXPOSE-less fallback hint for repo-provided
// Dockerfiles.
func DefaultPort(stack detect.Stack) int {
	switch stack {
	case detect.StackNode:
		return PortNode
	case detect.StackPython:
		return PortPython
	case detect.StackGo:
		return PortGo
	case detect.StackStatic:
		return PortStatic
	default:
		return PortFallback
	}
}

// exposeRe matches the first port of an EXPOSE instruction, tolerating a
// protocol suffix ("EXPOSE 8080/tcp") and multiple ports on one line.
var exposeRe = regexp.MustCompile(`(?mi)^\s*EXPOSE\s+(\d+)`)

// ExposedPort extracts the first EXPOSEd port from Dockerfile content,
// returning fallback when none is present or parseable.
func ExposedPort(dockerfileContent string, fallback int) int {
	m := exposeRe.FindStringSubmatch(dockerfileContent)
	if m == nil {
		return fallback
	}
	p, err := strconv.Atoi(m[1])
	if err != nil || p <= 0 || p > 65535 {
		return fallback
	}
	return p
}

// packageJSON is the subset of package.json the node generator reads.
type packageJSON struct {
	Main    string            `json:"main"`
	Scripts map[string]string `json:"scripts"`
}

func generateNode(dir string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return "", fmt.Errorf("read package.json: %w", err)
	}
	var pkg packageJSON
	if err := json.Unmarshal(raw, &pkg); err != nil {
		return "", fmt.Errorf("parse package.json: %w", err)
	}

	var b strings.Builder
	b.WriteString("FROM node:20-alpine\n")
	b.WriteString("WORKDIR /app\n")
	if fileExists(dir, "package-lock.json") {
		b.WriteString("COPY package.json package-lock.json ./\n")
		b.WriteString("RUN npm ci\n")
	} else {
		b.WriteString("COPY package.json ./\n")
		b.WriteString("RUN npm install\n")
	}
	b.WriteString("COPY . .\n")
	if _, ok := pkg.Scripts["build"]; ok {
		b.WriteString("RUN npm run build\n")
	}
	fmt.Fprintf(&b, "EXPOSE %d\n", PortNode)
	if _, ok := pkg.Scripts["start"]; ok {
		b.WriteString(`CMD ["npm", "start"]` + "\n")
	} else {
		entry := pkg.Main
		if entry == "" {
			entry = "index.js"
		}
		fmt.Fprintf(&b, "CMD [\"node\", %q]\n", entry)
	}
	return b.String(), nil
}

func generatePython(dir string) (string, error) {
	var b strings.Builder
	b.WriteString("FROM python:3.12-slim\n")
	b.WriteString("WORKDIR /app\n")
	if fileExists(dir, "requirements.txt") {
		b.WriteString("COPY requirements.txt ./\n")
		b.WriteString("RUN pip install --no-cache-dir -r requirements.txt\n")
		b.WriteString("COPY . .\n")
	} else {
		// pyproject.toml — install the project itself.
		b.WriteString("COPY . .\n")
		b.WriteString("RUN pip install --no-cache-dir .\n")
	}
	fmt.Fprintf(&b, "EXPOSE %d\n", PortPython)
	b.WriteString(pythonCmd(dir))
	return b.String(), nil
}

// pythonCmd picks the most specific run command detectable from the repo:
// Django's manage.py wins, then uvicorn for FastAPI imports in the
// entrypoint, then a plain `python <entrypoint>` dev-server fallback.
func pythonCmd(dir string) string {
	if fileExists(dir, "manage.py") {
		return fmt.Sprintf(`CMD ["python", "manage.py", "runserver", "0.0.0.0:%d"]`+"\n", PortPython)
	}
	entry := ""
	for _, cand := range []string{"app.py", "main.py", "wsgi.py"} {
		if fileExists(dir, cand) {
			entry = cand
			break
		}
	}
	if entry == "" {
		// No recognizable entrypoint — default to main.py so the container
		// fails with an explicit "can't open file" in its log rather than
		// this generator guessing silently.
		entry = "main.py"
	}
	if raw, err := os.ReadFile(filepath.Join(dir, entry)); err == nil {
		src := string(raw)
		if strings.Contains(src, "fastapi") || strings.Contains(src, "FastAPI") {
			mod := strings.TrimSuffix(entry, ".py")
			return fmt.Sprintf(`CMD ["uvicorn", "%s:app", "--host", "0.0.0.0", "--port", "%d"]`+"\n", mod, PortPython)
		}
	}
	return fmt.Sprintf(`CMD ["python", %q]`+"\n", entry)
}

func generateGo() string {
	return fmt.Sprintf(`FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY . .
RUN go mod download && CGO_ENABLED=0 go build -o /out/app .

FROM alpine:latest
RUN apk add --no-cache ca-certificates
COPY --from=builder /out/app /app
EXPOSE %d
CMD ["/app"]
`, PortGo)
}

func generateStatic() string {
	return fmt.Sprintf(`FROM nginx:alpine
COPY . /usr/share/nginx/html
EXPOSE %d
`, PortStatic)
}

func fileExists(dir, name string) bool {
	info, err := os.Stat(filepath.Join(dir, name))
	return err == nil && !info.IsDir()
}
