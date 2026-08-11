// Package detect classifies a checked-out repository root into one of the
// stacks the deploy-engine knows how to containerize. Detection is purely
// marker-file based — no file contents are parsed here (the dockerfile
// package reads contents when it needs entrypoint detail).
package detect

import (
	"os"
	"path/filepath"
)

// Stack is the detected application stack. Values are wire-format strings —
// they appear verbatim in the /v1/deploy response's "detected_stack" field.
type Stack string

const (
	StackNode    Stack = "node"
	StackPython  Stack = "python"
	StackGo      Stack = "go"
	StackStatic  Stack = "static"
	StackUnknown Stack = "unknown"
)

// Detect inspects the repo root and returns the stack. Priority order:
// node > python > go > static. A repo with both package.json and index.html
// is a node app whose build likely emits the HTML.
func Detect(dir string) Stack {
	switch {
	case fileExists(dir, "package.json"):
		return StackNode
	case fileExists(dir, "requirements.txt"), fileExists(dir, "pyproject.toml"):
		return StackPython
	case fileExists(dir, "go.mod"):
		return StackGo
	case fileExists(dir, "index.html"):
		return StackStatic
	default:
		return StackUnknown
	}
}

// HasRootDockerfile reports whether the repo ships its own Dockerfile at the
// root. When true the deploy-engine uses it as-is (dockerfile_source="repo").
func HasRootDockerfile(dir string) bool {
	return fileExists(dir, "Dockerfile")
}

func fileExists(dir, name string) bool {
	info, err := os.Stat(filepath.Join(dir, name))
	return err == nil && !info.IsDir()
}
