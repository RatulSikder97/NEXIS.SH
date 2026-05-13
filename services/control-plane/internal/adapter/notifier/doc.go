// Package notifier hosts per-channel domain.Notifier implementations (slack,
// email, console) and the multi-fanout wrapper used by the Approval Gate.
// The full implementations land in Phase 6 Stages 7-8; this doc.go is a
// placeholder so the .arch.yaml component declaration resolves cleanly before
// those shards run.
package notifier
