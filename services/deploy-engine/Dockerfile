# syntax=docker/dockerfile:1.7
#
# WARNING: do not deploy this container in production.
#
# The deploy-engine mounts the host docker socket so it can `docker build`
# preview images and `docker run` preview containers for POST /v1/deploy.
# It also shells out to git for shallow clones, so git + ca-certificates
# ship in the runtime image. Like services/validator, this image MUST stay
# inside the dev compose network and never be exposed beyond localhost.

# -------- Go build stage ----------------------------------------------------
FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# -------- Final image -------------------------------------------------------
# alpine + docker CLI + git: the engine only needs the CLI surface
# (`docker build` / `docker run` / `docker rm`) against the mounted socket —
# version drift with the host daemon is tolerated. curl backs the
# compose-level healthcheck, mirroring the validator image.
FROM alpine:3.20
WORKDIR /app
RUN apk add --no-cache docker-cli git curl ca-certificates

COPY --from=builder /out/server /app/server

EXPOSE 8091
ENTRYPOINT ["/app/server"]
