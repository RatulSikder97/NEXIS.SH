// Package sentry — backfill cron.
//
// cron.go owns the goroutine that fans BackfillRecent across every
// connected tenant on a 5-minute cadence. The controller (cmd/server/main.go)
// constructs the closure that enumerates connected orgs — usually via the
// IntegrationsRepo.ConnectedSentryOrgs admin-pool query.
package sentry

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// defaultBackfillInterval is the cron tick cadence. Exposed so tests can
// override via the optional second arg of StartBackfillCron without
// reaching into time.NewTicker directly.
const defaultBackfillInterval = 5 * time.Minute

// defaultBackfillWindow is the look-back window each tick passes to
// BackfillRecent. Matches the tick cadence so overlapping windows give
// us at-least-once even if a tick is delayed by GC / IO.
const defaultBackfillWindow = 5 * time.Minute

// StartBackfillCron launches the background goroutine that drives the
// backfill loop. listOrgs is invoked on each tick and must return the org
// ids that have a connected Sentry integration (the controller usually
// passes IntegrationsRepo.ConnectedSentryOrgs verbatim).
//
// The returned stop function cancels the loop and blocks until the
// goroutine exits — call from a SIGTERM handler or test cleanup.
//
// Calls BackfillRecent for every org in parallel via a tiny pool. The
// per-tenant call already handles its own decrypt + REST + dedupe; the
// cron just orchestrates and logs.
func (p *Provider) StartBackfillCron(ctx context.Context, listOrgs func() ([]string, error)) func() {
	return p.startBackfillCronWith(ctx, listOrgs, defaultBackfillInterval, defaultBackfillWindow)
}

// startBackfillCronWith is the test seam — the real entry point hard-codes
// the production cadence.
func (p *Provider) startBackfillCronWith(ctx context.Context, listOrgs func() ([]string, error), interval, window time.Duration) func() {
	if interval <= 0 {
		interval = defaultBackfillInterval
	}
	if window <= 0 {
		window = defaultBackfillWindow
	}

	innerCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})

	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		log := slog.Default().With(slog.String("component", "sentry-backfill"))

		for {
			select {
			case <-innerCtx.Done():
				return
			case <-ticker.C:
				p.runBackfillTick(innerCtx, listOrgs, window, log)
			}
		}
	}()

	return func() {
		cancel()
		<-done
	}
}

// runBackfillTick fans BackfillRecent across every connected org. Each org
// gets its own goroutine so a slow tenant does not stall the others; the
// outer wait group ensures the tick does not exit until every goroutine
// returns.
func (p *Provider) runBackfillTick(ctx context.Context, listOrgs func() ([]string, error), window time.Duration, log *slog.Logger) {
	orgs, err := listOrgs()
	if err != nil {
		log.WarnContext(ctx, "list connected orgs failed", slog.String("err", err.Error()))
		return
	}
	if len(orgs) == 0 {
		return
	}

	since := nowFn().Add(-window)

	var wg sync.WaitGroup
	for _, orgID := range orgs {
		orgID := orgID // capture for goroutine
		wg.Add(1)
		go func() {
			defer wg.Done()
			princ := domain.Principal{OrgID: orgID}
			emitted, err := p.BackfillRecent(ctx, princ, since)
			if err != nil {
				log.WarnContext(ctx, "backfill failed",
					slog.String("org_id", orgID),
					slog.String("err", err.Error()))
				return
			}
			if emitted > 0 {
				log.InfoContext(ctx, "backfill emitted",
					slog.String("org_id", orgID),
					slog.Int("count", emitted))
			}
		}()
	}
	wg.Wait()
}

// defaultNow / nowFn — package-level injection seam. Default to wall clock.
// Tests in this package overwrite nowFn so they can drive the cron forward
// without sleeping for 5 minutes per tick.
func defaultNow() time.Time { return time.Now() }
