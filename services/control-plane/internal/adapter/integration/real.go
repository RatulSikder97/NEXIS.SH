// real.go — wires real-API HTTP clients onto each adapter. Lives inside the
// integration package so it can import the `internal/httpx` package (which
// cmd/server can't touch due to Go's internal-package boundary). Each
// adapter degrades to stub mode when its config is empty.
package integration

import (
	"context"
	"strings"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration/argocd"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration/datadog"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration/github"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration/internal/httpx"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration/pagerduty"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration/sentry"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration/slack"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// RealConfig bundles every credential/URL needed to flip an adapter from
// stub-mode to real-API-mode. Any zero-valued field disables that adapter's
// real path.
type RealConfig struct {
	AppBaseURL string

	GitHubAppID            int64
	GitHubAppPrivateKeyPEM []byte
	GitHubAppSlug          string
	GitHubWebhookSecret    []byte

	SentryBaseURL           string
	SentryListConnectedOrgs func(ctx context.Context) ([]string, error)

	SlackClientID       string
	SlackClientSecret   []byte
	SlackAppRedirectURI string

	PagerDutyWebhookSecrets [][]byte

	IncidentSink domain.IncidentSink
}

// EnableRealAPIs flips each adapter to real-API mode where credentials allow.
// Returns a stop func for the Sentry backfill cron (nil if not started).
func (r *Registry) EnableRealAPIs(ctx context.Context, cfg RealConfig) func() {
	hx := httpx.New(nil, httpx.Config{})

	// GitHub — needs App ID + PEM. Webhook-only mode otherwise.
	if cfg.GitHubAppID != 0 && len(cfg.GitHubAppPrivateKeyPEM) > 0 {
		ghClient, gErr := github.NewClient(github.AppCreds{
			AppID:         cfg.GitHubAppID,
			PrivateKeyPEM: cfg.GitHubAppPrivateKeyPEM,
		}, hx)
		if gErr != nil {
			// Fall through to stub mode; admin sees this in the connection's last_error.
			ghClient = nil
		}
		r.GitHub.SetConfig(github.Config{
			AppID:         cfg.GitHubAppID,
			PrivateKeyPEM: cfg.GitHubAppPrivateKeyPEM,
			AppSlug:       cfg.GitHubAppSlug,
			WebhookSecret: cfg.GitHubWebhookSecret,
			AppBaseURL:    cfg.AppBaseURL,
		}, ghClient)
		if cfg.IncidentSink != nil {
			r.GitHub.SetIncidentSink(cfg.IncidentSink)
		}
	}

	// Sentry — always wire the real client; Connect probes the user's token
	// at call time, no startup creds needed.
	seClient := sentry.NewClient(cfg.SentryBaseURL, hx)
	r.Sentry.SetClient(seClient, sentry.Config{BaseURL: cfg.SentryBaseURL})

	var stopSentryCron func()
	if cfg.SentryListConnectedOrgs != nil {
		stopSentryCron = r.Sentry.StartBackfillCron(ctx, func() ([]string, error) {
			return cfg.SentryListConnectedOrgs(ctx)
		})
	}

	// ArgoCD — token+server come via Connect; just wire the client builder.
	r.ArgoCD.SetClientBuilder(func(base, token string) *argocd.Client {
		return argocd.NewClient(base, token, hx)
	})

	// Slack — needs OAuth client creds. Falls back to webhook-only mode.
	if cfg.SlackClientID != "" && len(cfg.SlackClientSecret) > 0 {
		slClient := slack.NewClient(hx)
		redirect := cfg.SlackAppRedirectURI
		if redirect == "" {
			redirect = strings.TrimRight(cfg.AppBaseURL, "/") + "/v1/integrations/slack/callback"
		}
		slProv := slack.NewWithOAuth(r.repo, r.kv, slClient, slack.OAuthConfig{
			ClientID:     cfg.SlackClientID,
			ClientSecret: string(cfg.SlackClientSecret),
			RedirectURI:  redirect,
		})
		r.Slack = slProv
		r.providers[domain.IntegrationSlack] = slProv
	}

	// Datadog — site set per-tenant on Connect; client wired with default site.
	ddClient := datadog.NewClient("datadoghq.com", hx)
	r.Datadog.SetClient(ddClient)

	// PagerDuty — same shape: token per-tenant; webhook secret list optional.
	pdClient := pagerduty.NewClient(hx)
	r.PagerDuty.SetClient(pdClient)
	if len(cfg.PagerDutyWebhookSecrets) > 0 {
		r.PagerDuty.SetWebhookSecrets(cfg.PagerDutyWebhookSecrets)
	}

	return stopSentryCron
}
