# syntax=docker/dockerfile:1.7
#
# WARNING: do not deploy this container in production.
# The validator mounts the host docker socket so it can spawn isolated
# `--rm --network=none --read-only` sandbox containers for each pytest run.
# Phase 7 swaps this for Modal — until then this image MUST stay inside the
# dev compose network and never be exposed beyond localhost.

FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# Final image needs the docker CLI so the runner can shell out to `docker run`.
# We use the standard docker-cli image as a tag-and-go layer source.
FROM docker:27-cli
WORKDIR /app
COPY --from=builder /out/server /app/server
EXPOSE 8081
ENTRYPOINT ["/app/server"]
