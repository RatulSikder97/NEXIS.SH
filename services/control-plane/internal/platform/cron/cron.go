// Package cron is a minimal ticker dispatcher. Each Job has a name, an
// interval, and a Run func. Start launches one goroutine per Job that fires
// Run on the interval until ctx is cancelled. Errors are logged but do not
// stop the loop — transient DB hiccups should not silently disable a cron.
package cron

import (
	"context"
	"log/slog"
	"time"
)

// Job is one scheduled task. Run receives the same ctx Start was called with;
// if the ctx is cancelled mid-Run the job is responsible for cooperative
// shutdown.
type Job struct {
	Name     string
	Interval time.Duration
	Run      func(ctx context.Context) error
}

// Start launches one goroutine per Job. Returns immediately; jobs run until
// ctx is cancelled. Intentionally no return value — there is no graceful
// shutdown signal to wait on; the main server's shutdown ctx propagates
// cancellation.
func Start(ctx context.Context, logger *slog.Logger, jobs []Job) {
	for _, j := range jobs {
		j := j
		if j.Interval <= 0 {
			logger.Warn("cron: skipping job with non-positive interval", "name", j.Name, "interval", j.Interval)
			continue
		}
		go func() {
			t := time.NewTicker(j.Interval)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					if err := j.Run(ctx); err != nil {
						logger.Error("cron job", "name", j.Name, "err", err)
					}
				}
			}
		}()
	}
}
