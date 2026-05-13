.PHONY: build test run-dev install lint

# Phase 6 image tag — Phase 7 promotes to nexis/causal-inference:1.0.0.
IMAGE := nexis/causal-inference:dev

# Build the local container image. Use `make build` from CI + docker-compose
# `build:` targets reference services/causal-inference directly.
build:
	docker build -t $(IMAGE) .

# Install runtime + dev deps into the active python env. Used for local
# pytest runs without spinning up the container.
install:
	pip install -r requirements.txt

# Pytest entrypoint. Pytest picks up pyproject.toml's pythonpath + testpaths.
test:
	pytest src/

# Local dev server — equivalent to the container CMD.
run-dev:
	PYTHONPATH=src uvicorn app:app --reload --host 0.0.0.0 --port 8090

lint:
	python -m compileall -q src
