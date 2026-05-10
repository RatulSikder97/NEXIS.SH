FROM golang:1.22-alpine AS builder

ARG SERVICE
WORKDIR /src

COPY backend/go.mod backend/go.sum ./
COPY backend/internal ./internal
COPY backend/cmd ./cmd

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/service ./cmd/${SERVICE}

FROM alpine:3.20 AS runtime

RUN addgroup -S nexis && adduser -S nexis -G nexis && \
    mkdir -p /data && chown -R nexis:nexis /data && \
    apk add --no-cache ca-certificates wget

WORKDIR /app
COPY --from=builder /out/service /app/service

USER nexis
EXPOSE 8080

ENTRYPOINT ["/app/service"]
