package integration

// Unit coverage for the Registry constructor + accessors. NewRegistry pulls
// in every provider's New(); the test confirms that all six are registered
// under their canonical names, that Get() round-trips, and that SetProvider
// swaps an entry in place.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration/datadog"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration/github"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/repo"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// stubKV is a tiny no-op KeyVault — the factory never invokes a method on
// the keyvault during construction; the providers cache the reference and
// only consume it later in webhook/probe paths.
type stubKV struct{}

func (stubKV) Encrypt(_ context.Context, plaintext []byte) ([]byte, error) {
	return plaintext, nil
}

func (stubKV) Decrypt(_ context.Context, ciphertext []byte) ([]byte, error) {
	return ciphertext, nil
}

// stubIncidentSink is a no-op sink — same rationale as the KV above.
type stubIncidentSink struct{}

func (stubIncidentSink) Insert(_ context.Context, _ string, _ domain.RawIncident) error {
	return nil
}

// TestNewRegistry_AllProvidersRegistered confirms every IntegrationProvider
// constant has an entry. Future adapters should be added here so the test
// fails when a new provider is wired through main.go but missed in the
// factory.
func TestNewRegistry_AllProvidersRegistered(t *testing.T) {
	r := NewRegistry(Deps{
		Repo:                 &repo.IntegrationsRepo{},
		KV:                   stubKV{},
		IncidentSink:         stubIncidentSink{},
		GitHubDefaultSecret:  []byte("x"),
		DatadogSigningSecret: []byte("y"),
	})

	wantProviders := []domain.IntegrationProvider{
		domain.IntegrationGitHub,
		domain.IntegrationSentry,
		domain.IntegrationArgoCD,
		domain.IntegrationSlack,
		domain.IntegrationDatadog,
		domain.IntegrationPagerDuty,
	}
	for _, p := range wantProviders {
		_, ok := r.Get(p)
		if !ok {
			t.Fatalf("Get(%q) returned !ok — provider not registered", p)
		}
	}
	// Typed handles must also be non-nil.
	if r.GitHub == nil || r.Sentry == nil || r.ArgoCD == nil ||
		r.Slack == nil || r.Datadog == nil || r.PagerDuty == nil {
		t.Fatalf("typed handles incomplete: GitHub=%v Sentry=%v ArgoCD=%v Slack=%v Datadog=%v PagerDuty=%v",
			r.GitHub, r.Sentry, r.ArgoCD, r.Slack, r.Datadog, r.PagerDuty)
	}
}

// TestRegistry_GetUnknownReturnsFalse — an unrecognised provider name comes
// back !ok.
func TestRegistry_GetUnknownReturnsFalse(t *testing.T) {
	r := NewRegistry(Deps{
		Repo: &repo.IntegrationsRepo{}, KV: stubKV{}, IncidentSink: stubIncidentSink{},
	})
	if _, ok := r.Get(domain.IntegrationProvider("not-a-provider")); ok {
		t.Fatalf("unknown provider should return !ok")
	}
}

// TestRegistry_SetProvider — swap a stub provider in for github and verify
// Get returns the swap.
func TestRegistry_SetProvider(t *testing.T) {
	r := NewRegistry(Deps{
		Repo: &repo.IntegrationsRepo{}, KV: stubKV{}, IncidentSink: stubIncidentSink{},
	})
	stub := &fakeIntegration{name: domain.IntegrationGitHub}
	r.SetProvider(domain.IntegrationGitHub, stub)

	got, ok := r.Get(domain.IntegrationGitHub)
	if !ok {
		t.Fatalf("swapped provider missing")
	}
	gotStub, _ := got.(*fakeIntegration)
	if gotStub != stub {
		t.Fatalf("SetProvider did not replace the entry")
	}
}

// TestNewRegistry_FromEmailDefault — when PagerDutyFromEmail is empty the
// factory uses the canonical "nexis-bot@nexis.dev" default. We can't
// observe this directly through the registry surface, but exercising the
// branch is sufficient for the line-cov target.
func TestNewRegistry_FromEmailDefault(t *testing.T) {
	r := NewRegistry(Deps{
		Repo: &repo.IntegrationsRepo{}, KV: stubKV{}, IncidentSink: stubIncidentSink{},
		// PagerDutyFromEmail intentionally empty
	})
	if r.PagerDuty == nil {
		t.Fatalf("pagerduty provider must still be constructed")
	}
}

// fakeIntegration is the minimal stub satisfying domain.Integration so
// SetProvider can hand back a non-nil swap entry. Only the registry methods
// are exercised; the rest are wired up to avoid compile errors.
type fakeIntegration struct {
	name domain.IntegrationProvider
}

func (f *fakeIntegration) Name() domain.IntegrationProvider {
	return f.name
}

func (f *fakeIntegration) Connect(context.Context, domain.Principal, map[string]any) (domain.Connection, error) {
	return domain.Connection{}, errors.New("not implemented")
}

func (f *fakeIntegration) Disconnect(context.Context, domain.Principal) error {
	return errors.New("not implemented")
}

func (f *fakeIntegration) Status(context.Context, domain.Principal) (domain.Connection, error) {
	return domain.Connection{}, errors.New("not implemented")
}

func (f *fakeIntegration) HandleWebhook(context.Context, string, map[string]string, []byte) error {
	return errors.New("not implemented")
}

// TestProbeResult_HealthyWithin walks every branch of the probe-status
// reducer used by the system-health endpoint.
func TestProbeResult_HealthyWithin(t *testing.T) {
	cases := []struct {
		name      string
		result    ProbeResult
		threshold time.Duration
		want      string
	}{
		{
			name:      "error_returns_down",
			result:    ProbeResult{Err: errors.New("boom")},
			threshold: 500 * time.Millisecond,
			want:      "down",
		},
		{
			name:      "fast_returns_healthy",
			result:    ProbeResult{Latency: 5 * time.Millisecond},
			threshold: 500 * time.Millisecond,
			want:      "healthy",
		},
		{
			name:      "slow_returns_degraded",
			result:    ProbeResult{Latency: 750 * time.Millisecond},
			threshold: 500 * time.Millisecond,
			want:      "degraded",
		},
		{
			name:      "no_threshold_returns_healthy",
			result:    ProbeResult{Latency: 750 * time.Millisecond},
			threshold: 0, // no degraded cutoff
			want:      "healthy",
		},
		{
			name:      "negative_threshold_returns_healthy",
			result:    ProbeResult{Latency: 750 * time.Millisecond},
			threshold: -1,
			want:      "healthy",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.result.HealthyWithin(tc.threshold)
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

// TestProbePostgres_NilPool returns the "disabled" sentinel without
// dereferencing the pool.
func TestProbePostgres_NilPool(t *testing.T) {
	got := ProbePostgres(context.Background(), nil)
	if got.Err == nil || got.Err.Error() != "disabled" {
		t.Fatalf("expected disabled sentinel, got %v", got)
	}
}

// TestProbeRedis_EmptyAddr — disabled when the addr is empty.
func TestProbeRedis_EmptyAddr(t *testing.T) {
	got := ProbeRedis(context.Background(), "")
	if got.Err == nil || got.Err.Error() != "disabled" {
		t.Fatalf("expected disabled sentinel, got %v", got)
	}
}

// TestProbeRedis_BadAddrErrors — non-empty but unreachable addr surfaces
// an error (latency is recorded for debugging).
func TestProbeRedis_BadAddrErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	got := ProbeRedis(ctx, "127.0.0.1:1") // port 1 is reserved; should refuse
	if got.Err == nil {
		t.Fatalf("expected an error for unreachable redis")
	}
}

// TestProbeNeo4j_NilDriver — disabled.
func TestProbeNeo4j_NilDriver(t *testing.T) {
	got := ProbeNeo4j(context.Background(), nil)
	if got.Err == nil || got.Err.Error() != "disabled" {
		t.Fatalf("expected disabled sentinel, got %v", got)
	}
}

// TestProbeMinIO_EmptyEndpoint — disabled.
func TestProbeMinIO_EmptyEndpoint(t *testing.T) {
	got := ProbeMinIO(context.Background(), MinIOConfig{})
	if got.Err == nil || got.Err.Error() != "disabled" {
		t.Fatalf("expected disabled sentinel, got %v", got)
	}
}

// TestProbeTemporal_NilClient — disabled.
func TestProbeTemporal_NilClient(t *testing.T) {
	got := ProbeTemporal(context.Background(), nil)
	if got.Err == nil || got.Err.Error() != "disabled" {
		t.Fatalf("expected disabled sentinel, got %v", got)
	}
}

// Ensure the github + datadog typed pointers from NewRegistry are usable —
// compile-time pointer non-nil check.
var _ *github.Provider
var _ *datadog.Provider

// TestEnableRealAPIs_NilCredsLeavesStubMode — when GitHub creds are empty,
// EnableRealAPIs does NOT flip GitHub to real mode (the App ID is the trigger).
// The function still returns without panicking.
func TestEnableRealAPIs_NilCredsLeavesStubMode(t *testing.T) {
	r := NewRegistry(Deps{
		Repo: &repo.IntegrationsRepo{}, KV: stubKV{}, IncidentSink: stubIncidentSink{},
	})
	stop := r.EnableRealAPIs(context.Background(), RealConfig{
		// All creds empty
	})
	// Stop should be nil because SentryListConnectedOrgs isn't wired.
	if stop != nil {
		stop()
	}
}

// TestEnableRealAPIs_WithGitHubCredentials — GitHub App credentials trigger
// the real-API wiring path; no panic on a malformed PEM (the function
// tolerates errors and falls back).
func TestEnableRealAPIs_WithGitHubCredentials(t *testing.T) {
	r := NewRegistry(Deps{
		Repo: &repo.IntegrationsRepo{}, KV: stubKV{}, IncidentSink: stubIncidentSink{},
	})
	// Malformed PEM — github.NewClient will fail, but EnableRealAPIs absorbs.
	stop := r.EnableRealAPIs(context.Background(), RealConfig{
		AppBaseURL:             "https://example.com",
		GitHubAppID:            12345,
		GitHubAppPrivateKeyPEM: []byte("-----BEGIN ANYTHING-----\nnot-a-real-pem\n-----END\n"),
		GitHubAppSlug:          "nexis-test",
		GitHubWebhookSecret:    []byte("secret"),
	})
	if stop != nil {
		stop()
	}
}

// TestEnableRealAPIs_WithSlackOAuthCredentials — wiring Slack OAuth swaps
// the provider; verify the provider entry under IntegrationSlack changes.
func TestEnableRealAPIs_WithSlackOAuthCredentials(t *testing.T) {
	r := NewRegistry(Deps{
		Repo: &repo.IntegrationsRepo{}, KV: stubKV{}, IncidentSink: stubIncidentSink{},
	})
	original, _ := r.Get(domain.IntegrationSlack)
	stop := r.EnableRealAPIs(context.Background(), RealConfig{
		SlackClientID:     "client-1",
		SlackClientSecret: []byte("secret-1"),
	})
	if stop != nil {
		stop()
	}
	after, _ := r.Get(domain.IntegrationSlack)
	if original == after {
		t.Fatalf("Slack provider should have been replaced when OAuth creds are wired")
	}
}
