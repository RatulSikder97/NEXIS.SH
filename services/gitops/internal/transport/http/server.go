// Package http hosts the gitops service's HTTP surface. The full router
// + middleware wiring lives in handler.go — this file is kept as a
// placeholder so the package's go-doc top comment is colocated with the
// existing module surface from before Phase 6.
//
// See handler.go for:
//   - Deps struct + New() router constructor.
//   - bearerAuth middleware (GITOPS_AUTH_TOKEN bearer check).
//   - POST /v1/gitops/open-pr handler (delegates to usecase.OpenPRUsecase).
//
// The /healthz route on this package is unauthenticated and always returns
// 200 — used by kubelet readiness/liveness probes.
package http
