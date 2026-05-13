# syntax=docker/dockerfile:1.7
#
# WARNING: do not deploy this container in production.
#
# Phase 4 baseline: the validator mounts the host docker socket so it can
# spawn isolated `--rm --network=none --read-only` sandbox containers for
# each pytest run.
#
# Phase 6 additions (dev-only):
#   * Python 3.12 + `hypothesis` + `pytest` so the in-container Hypothesis
#     sidecar (POST /v1/validate/property) can shell out to property
#     tests against the patched fixture.
#   * The fixture source tree is copied to /srv/fixture so the sidecar
#     can prepend it to PYTHONPATH.
#   * The Phase 6 Hypothesis property suite is copied to
#     /srv/hypothesis-tests for the sidecar to pick up.
#
# Phase 7 swaps both runners (Docker + Hypothesis) for Modal. Until then
# this image MUST stay inside the dev compose network and never be
# exposed beyond localhost.

# -------- Go build stage ----------------------------------------------------
FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# -------- Final image -------------------------------------------------------
# Phase 6 swaps the docker:27-cli base for python:3.12-slim because the
# Hypothesis sidecar needs CPython. The docker CLI is installed on top so
# the Phase 4 Docker runner can still shell out to `docker run`.
FROM python:3.12-slim
WORKDIR /app

# Install the docker CLI + curl (used by container healthchecks). We pin
# nothing here because the runner only needs the CLI surface
# (`docker run --rm ...`) — version drift is tolerated.
RUN apt-get update \
    && apt-get install -y --no-install-recommends docker.io curl ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Phase 6: hypothesis + pytest for the in-container property-test sidecar.
# Pinned to a known-good range — hypothesis 6.99 ships the deadline + max
# examples API the property suite depends on.
RUN pip install --no-cache-dir 'hypothesis>=6.99,<7' 'pytest>=8.0,<9'

# Go server binary.
COPY --from=builder /out/server /app/server

# Hypothesis sidecar source — sidecar.py + runners/. We keep this under
# /sidecar so PYTHONPATH stays minimal.
COPY hypothesis-sidecar/ /sidecar/

# Phase 6 property suite + patched fixture source. The sidecar prepends
# /srv/fixture to PYTHONPATH per request; HYPOTHESIS_TESTS_DIR points
# pytest at the property files.
COPY fixtures/hypothesis/ /srv/hypothesis-tests/
COPY fixtures/src/ /srv/fixture/

ENV PYTHONPATH=/sidecar \
    HYPOTHESIS_SOCKET=/tmp/hypothesis.sock \
    HYPOTHESIS_REPO_PATH=/srv/fixture \
    HYPOTHESIS_TESTS_DIR=/srv/hypothesis-tests \
    HYPOTHESIS_MAX_RUNTIME_SECONDS=30 \
    HYPOTHESIS_MAX_EXAMPLES=20 \
    PYTHONUNBUFFERED=1

# Entrypoint launches the Hypothesis sidecar in the background, then
# execs the Go server in the foreground. The sidecar's lifetime is tied
# to the container so `docker compose down` kills both.
RUN printf '#!/bin/sh\nset -e\npython /sidecar/sidecar.py &\nexec /app/server\n' > /entrypoint.sh \
    && chmod +x /entrypoint.sh

EXPOSE 8081
ENTRYPOINT ["/entrypoint.sh"]
