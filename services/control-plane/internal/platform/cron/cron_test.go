package cron

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

// TestStart_FiresJobsOnInterval — a job with a 50ms interval should run
// at least twice within a 200ms window.
func TestStart_FiresJobsOnInterval(t *testing.T) {
	var fires atomic.Int32
	jobs := []Job{
		{
			Name:     "test-job",
			Interval: 50 * time.Millisecond,
			Run: func(_ context.Context) error {
				fires.Add(1)
				return nil
			},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	Start(ctx, logger, jobs)
	<-ctx.Done()

	got := fires.Load()
	if got < 2 {
		t.Fatalf("expected at least 2 fires, got %d", got)
	}
}

// TestStart_SkipsNonPositiveInterval — a job with interval <= 0 must be
// silently skipped (logged + no goroutine).
func TestStart_SkipsNonPositiveInterval(t *testing.T) {
	var fires atomic.Int32
	jobs := []Job{
		{
			Name:     "zero-interval",
			Interval: 0,
			Run: func(_ context.Context) error {
				fires.Add(1)
				return nil
			},
		},
		{
			Name:     "negative-interval",
			Interval: -1 * time.Millisecond,
			Run: func(_ context.Context) error {
				fires.Add(1)
				return nil
			},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	Start(ctx, logger, jobs)
	<-ctx.Done()

	if got := fires.Load(); got != 0 {
		t.Fatalf("non-positive interval job must NOT fire, got %d fires", got)
	}
}

// TestStart_JobErrorContinues — a job that returns an error on every fire
// keeps firing on subsequent ticks (we don't kill the loop on error).
func TestStart_JobErrorContinues(t *testing.T) {
	var fires atomic.Int32
	jobs := []Job{
		{
			Name:     "always-errors",
			Interval: 30 * time.Millisecond,
			Run: func(_ context.Context) error {
				fires.Add(1)
				return errors.New("transient")
			},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	Start(ctx, logger, jobs)
	<-ctx.Done()

	if got := fires.Load(); got < 2 {
		t.Fatalf("expected >=2 fires despite errors, got %d", got)
	}
}

// TestStart_CtxCancellationStopsAllJobs — once the parent ctx is cancelled,
// no further fires happen on any job.
func TestStart_CtxCancellationStopsAllJobs(t *testing.T) {
	var fires atomic.Int32
	jobs := []Job{
		{
			Name:     "test-job",
			Interval: 30 * time.Millisecond,
			Run: func(_ context.Context) error {
				fires.Add(1)
				return nil
			},
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	Start(ctx, logger, jobs)
	time.Sleep(100 * time.Millisecond)
	cancel()

	beforeFires := fires.Load()
	time.Sleep(150 * time.Millisecond)
	afterFires := fires.Load()
	if afterFires != beforeFires {
		t.Fatalf("jobs continued firing after cancellation: %d -> %d", beforeFires, afterFires)
	}
}
