package dockerfile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nexis-eco/nexis/services/deploy-engine/internal/detect"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGenerate_NodeWithLockfileBuildAndStart(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{"scripts":{"build":"tsc","start":"node dist/index.js"}}`)
	writeFile(t, dir, "package-lock.json", `{}`)

	content, port, err := Generate(dir, detect.StackNode)
	if err != nil {
		t.Fatal(err)
	}
	if port != PortNode {
		t.Fatalf("port = %d, want %d", port, PortNode)
	}
	for _, want := range []string{
		"FROM node:20-alpine",
		"COPY package.json package-lock.json ./",
		"RUN npm ci",
		"RUN npm run build",
		"EXPOSE 3000",
		`CMD ["npm", "start"]`,
	} {
		if !strings.Contains(content, want) {
			t.Errorf("missing %q in:\n%s", want, content)
		}
	}
}

func TestGenerate_NodeNoLockfileNoStart(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{"main":"server.js"}`)

	content, _, err := Generate(dir, detect.StackNode)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"RUN npm install", `CMD ["node", "server.js"]`} {
		if !strings.Contains(content, want) {
			t.Errorf("missing %q in:\n%s", want, content)
		}
	}
	if strings.Contains(content, "npm ci") {
		t.Error("npm ci must not appear without a lockfile")
	}
	if strings.Contains(content, "npm run build") {
		t.Error("build step must not appear without a build script")
	}
}

func TestGenerate_NodeNoMainFallsBackToIndexJS(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{}`)
	content, _, err := Generate(dir, detect.StackNode)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content, `CMD ["node", "index.js"]`) {
		t.Errorf("missing index.js fallback CMD in:\n%s", content)
	}
}

func TestGenerate_NodeBadPackageJSON(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{not json`)
	if _, _, err := Generate(dir, detect.StackNode); err == nil {
		t.Fatal("expected error for malformed package.json")
	}
}

func TestGenerate_PythonFastAPI(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "requirements.txt", "fastapi\nuvicorn\n")
	writeFile(t, dir, "main.py", "from fastapi import FastAPI\napp = FastAPI()\n")

	content, port, err := Generate(dir, detect.StackPython)
	if err != nil {
		t.Fatal(err)
	}
	if port != PortPython {
		t.Fatalf("port = %d, want %d", port, PortPython)
	}
	for _, want := range []string{
		"FROM python:3.12-slim",
		"RUN pip install --no-cache-dir -r requirements.txt",
		"EXPOSE 8000",
		`CMD ["uvicorn", "main:app", "--host", "0.0.0.0", "--port", "8000"]`,
	} {
		if !strings.Contains(content, want) {
			t.Errorf("missing %q in:\n%s", want, content)
		}
	}
}

func TestGenerate_PythonFlaskFallsBackToPlainPython(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "requirements.txt", "flask\n")
	writeFile(t, dir, "app.py", "from flask import Flask\napp = Flask(__name__)\napp.run(host='0.0.0.0')\n")

	content, _, err := Generate(dir, detect.StackPython)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content, `CMD ["python", "app.py"]`) {
		t.Errorf("missing flask dev-server CMD in:\n%s", content)
	}
}

func TestGenerate_PythonDjango(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "requirements.txt", "django\n")
	writeFile(t, dir, "manage.py", "#!/usr/bin/env python\n")

	content, _, err := Generate(dir, detect.StackPython)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content, `CMD ["python", "manage.py", "runserver", "0.0.0.0:8000"]`) {
		t.Errorf("missing django runserver CMD in:\n%s", content)
	}
}

func TestGenerate_PythonPyproject(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pyproject.toml", "[project]\nname = \"x\"\n")
	writeFile(t, dir, "main.py", "print('hi')\n")

	content, _, err := Generate(dir, detect.StackPython)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"RUN pip install --no-cache-dir .", `CMD ["python", "main.py"]`} {
		if !strings.Contains(content, want) {
			t.Errorf("missing %q in:\n%s", want, content)
		}
	}
	if strings.Contains(content, "requirements.txt") {
		t.Error("requirements.txt path must not appear for pyproject repos")
	}
}

func TestGenerate_Go(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/x\n\ngo 1.25\n")

	content, port, err := Generate(dir, detect.StackGo)
	if err != nil {
		t.Fatal(err)
	}
	if port != PortGo {
		t.Fatalf("port = %d, want %d", port, PortGo)
	}
	for _, want := range []string{
		"FROM golang:1.25-alpine AS builder",
		"go build -o /out/app .",
		"FROM alpine:latest",
		"EXPOSE 8080",
		`CMD ["/app"]`,
	} {
		if !strings.Contains(content, want) {
			t.Errorf("missing %q in:\n%s", want, content)
		}
	}
}

func TestGenerate_Static(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "index.html", "<h1>hi</h1>")

	content, port, err := Generate(dir, detect.StackStatic)
	if err != nil {
		t.Fatal(err)
	}
	if port != PortStatic {
		t.Fatalf("port = %d, want %d", port, PortStatic)
	}
	for _, want := range []string{"FROM nginx:alpine", "COPY . /usr/share/nginx/html", "EXPOSE 80"} {
		if !strings.Contains(content, want) {
			t.Errorf("missing %q in:\n%s", want, content)
		}
	}
}

func TestGenerate_Unknown(t *testing.T) {
	_, _, err := Generate(t.TempDir(), detect.StackUnknown)
	if !errors.Is(err, ErrUnknownStack) {
		t.Fatalf("err = %v, want ErrUnknownStack", err)
	}
	if !strings.Contains(err.Error(), "package.json/requirements.txt/go.mod/index.html") {
		t.Fatalf("error must name the markers it looked for, got: %v", err)
	}
}

func TestExposedPort(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    int
	}{
		{"plain", "FROM x\nEXPOSE 5000\n", 5000},
		{"protocol suffix", "EXPOSE 5000/tcp\n", 5000},
		{"multiple ports takes first", "EXPOSE 80 443\n", 80},
		{"multiple expose lines takes first", "EXPOSE 3000\nEXPOSE 9000\n", 3000},
		{"indented", "  EXPOSE 4000\n", 4000},
		{"lowercase", "expose 7000\n", 7000},
		{"none", "FROM alpine\nCMD [\"/app\"]\n", PortFallback},
		{"commented only line ignored", "# EXPOSE 1234\n", PortFallback},
		{"out of range", "EXPOSE 99999\n", PortFallback},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExposedPort(tc.content, PortFallback); got != tc.want {
				t.Fatalf("ExposedPort() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestDefaultPort(t *testing.T) {
	if DefaultPort(detect.StackNode) != PortNode ||
		DefaultPort(detect.StackPython) != PortPython ||
		DefaultPort(detect.StackGo) != PortGo ||
		DefaultPort(detect.StackStatic) != PortStatic ||
		DefaultPort(detect.StackUnknown) != PortFallback {
		t.Fatal("DefaultPort mapping wrong")
	}
}
