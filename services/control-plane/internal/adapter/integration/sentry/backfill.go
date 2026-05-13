// Package sentry — issue backfill.
//
// backfill.go fills the gap between Sentry's webhook fan-out and our incident
// store. Webhooks can drop on the Sentry side (rate limiting, outage), and we
// need at-least-once delivery for incidents. The strategy:
//
//  1. Every 5 minutes (see cron.go) the provider lists "issues seen since
//     T-5min" for every connected tenant.
//  2. Each returned fingerprint is checked against an in-memory LRU (last
//     1000 fingerprints). Already-seen → skip; new → emit to the sink.
//  3. The LRU is shared with HandleWebhook so an issue delivered via webhook
//     is not double-emitted by the next cron tick.
//
// Idempotency contract: BackfillRecent is safe to call concurrently per
// tenant; the LRU is mutex-guarded. Running BackfillRecent twice with the
// same `since` returns 0 on the second call.
package sentry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// BackfillRecent fetches issues seen since `since` for the principal's
// tenant, dedupes against the in-memory fingerprint LRU, and emits new
// fingerprints to the IncidentSink. Returns the number of new emits.
//
// Idempotent — running twice over the same window emits zero on the
// second call thanks to the LRU. A restart of the process clears the LRU,
// which is bounded by the IncidentSink's own (org_id, source_event_id)
// uniqueness constraint downstream.
func (p *Provider) BackfillRecent(ctx context.Context, princ domain.Principal, since time.Time) (int, error) {
	if princ.OrgID == "" {
		return 0, errors.New("sentry: backfill: empty OrgID")
	}
	client, err := p.getClient()
	if err != nil {
		return 0, err
	}

	_, enc, err := p.repo.Get(ctx, princ.OrgID, domain.IntegrationSentry)
	if err != nil {
		return 0, fmt.Errorf("sentry: backfill: load connection: %w", err)
	}
	if enc == nil {
		return 0, errors.New("sentry: backfill: no connection")
	}
	blob, err := p.decryptBlob(ctx, enc)
	if err != nil {
		return 0, fmt.Errorf("sentry: backfill: decrypt: %w", err)
	}
	if blob.AuthToken == "" || blob.OrgSlug == "" || blob.ProjectSlug == "" {
		// Legacy connection — backfill is impossible without REST creds.
		// Skip cleanly so the cron does not log spam.
		return 0, nil
	}

	issues, err := client.ListRecentIssues(ctx, blob.AuthToken, blob.OrgSlug, blob.ProjectSlug, since)
	if err != nil {
		return 0, fmt.Errorf("sentry: backfill: list issues: %w", err)
	}

	emitted := 0
	for _, issue := range issues {
		if !p.dedupe.AddIfAbsent(issue.Fingerprint) {
			continue
		}
		payload := issueToPayload(issue)
		raw := domain.RawIncident{
			Source:        "sentry",
			SourceEventID: issue.Fingerprint,
			Title:         issue.Title,
			Level:         issue.Level,
			Service:       blob.ProjectSlug,
			Environment:   issueEnvironment(issue),
			Payload:       payload,
		}
		if err := p.sink.Insert(ctx, princ.OrgID, raw); err != nil {
			// Re-add to ensure the next tick retries (AddIfAbsent already
			// inserted; explicitly drop so the next call can retry).
			p.dedupe.Drop(issue.Fingerprint)
			return emitted, fmt.Errorf("sentry: backfill: sink insert: %w", err)
		}
		emitted++
	}
	return emitted, nil
}

// issueToPayload serialises an Issue into a plain map[string]any so the
// sink can JSON-encode it for incidents_raw.payload. Going through
// json.Marshal/Unmarshal keeps the shape stable rather than reflecting
// struct field names.
func issueToPayload(i Issue) map[string]any {
	raw, _ := json.Marshal(i)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out
}

// issueEnvironment pulls an "environment" hint from the Sentry issue
// metadata map when present. Sentry exposes environment per-event rather
// than per-issue, so this is best-effort and defaults to empty.
func issueEnvironment(i Issue) string {
	if i.Metadata == nil {
		return ""
	}
	if env, ok := i.Metadata["environment"].(string); ok {
		return env
	}
	return ""
}
