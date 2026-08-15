# syntax=docker/dockerfile:1.7
FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/seed-pgvector ./cmd/seed-pgvector
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/eval ./cmd/eval
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/seed-neo4j ./cmd/seed-neo4j

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=builder /out/server /app/server
COPY --from=builder /out/seed-pgvector /app/seed-pgvector
COPY --from=builder /out/eval /app/eval
COPY --from=builder /out/seed-neo4j /app/seed-neo4j
# Scenario incidents read at runtime by handler.loadFixtureIncident and the
# eval runner. Without them the demo/eval paths start a pipeline with no
# `incident` payload and every agent reasons over a placeholder, which is what
# made deployed runs look stubbed.
COPY --from=builder /src/fixtures /app/fixtures
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/app/server"]
