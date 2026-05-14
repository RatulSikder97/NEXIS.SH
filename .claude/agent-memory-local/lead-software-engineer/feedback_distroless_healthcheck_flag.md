---
name: Distroless services use --healthcheck flag pattern
description: For Go services on distroless images, in-container HEALTHCHECKs go through a --healthcheck flag on the main binary that dials /healthz — never add curl/wget to the runtime image.
type: feedback
---

Add an in-container HEALTHCHECK to a distroless Go service by giving its main binary a `--healthcheck` subcommand that dials `http://127.0.0.1:$PORT/healthz` over a short timeout and exits 0/1. Then in compose: `healthcheck: { test: ["CMD", "/app/server", "--healthcheck"], ... }`.

**Why:** `gcr.io/distroless/static-debian12:nonroot` has no shell, no curl, no wget — any `CMD-SHELL` or `CMD ["curl", ...]` healthcheck silently fails. Adding curl bloats the runtime image and breaks the distroless threat-model promise. The same binary already speaks HTTP, so reusing it as the probe costs only a few kB of code and zero extra dependencies. This is the pattern documented in the 2026-05-14 DevOps audit (F-12, F-22) and shipped in Wave 2 for the control-plane.

**How to apply:**
- Implement using `flag.NewFlagSet(...).Parse(os.Args[1:])` so the flag short-circuits the boot path before `slog`/config init runs (a healthcheck spin should be ~milliseconds, not boot the whole app).
- Use `http.Client{Timeout: 3 * time.Second}` and read the body with `io.Copy(io.Discard, ...)` to avoid leaking connections under repeated probes.
- For services whose source you cannot edit (e.g. an out-of-scope main.go), leave the healthcheck off and document why — do not regress to curl-in-distroless or to no probe.
