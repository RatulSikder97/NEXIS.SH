// Real-integrations Wave 1 — declarative manifest for the Configure dialog.
//
// Each entry tells the generic <ConfigureDialog/> how to render itself:
//   * `flow:"oauth"` → the dialog is a single "Continue with {label}" button
//     that bounces the browser to `oauth_start_url`. The control-plane owns
//     the consent screen + callback.
//   * `flow:"form"`  → the dialog renders one input per `fields[]` entry and
//     POSTs `{config: <all fields>}` to /v1/integrations/{provider}/connect.
//
// Wire format is snake_case across the boundary — every `name` here matches
// the JSON key the control-plane expects in the config payload, and every
// `provider` matches `domain.IntegrationProvider` in the Go control-plane.

export type IntegrationProvider =
  | "github"
  | "sentry"
  | "argocd"
  | "slack"
  | "datadog"
  | "pagerduty";

export type ConfigureField = {
  // Form field name → also the JSON key emitted in the config payload.
  name: string;
  label: string;
  type: "text" | "password" | "url" | "select";
  placeholder?: string;
  required: boolean;
  // Optional inline help rendered under the field; appears in muted text.
  help?: string;
  // Default value for the field; pre-fills the input on first render.
  default?: string;
  // Required when `type === "select"`. Renders as <option/> entries.
  options?: { value: string; label: string }[];
};

export type IntegrationManifest = {
  provider: IntegrationProvider;
  label: string;
  // "oauth" = redirect to provider for consent.
  // "form"  = render `fields[]` inline and POST to /connect.
  flow: "oauth" | "form";
  // Required when `flow === "oauth"`. Browser navigates here on Continue.
  oauth_start_url?: string;
  // Required when `flow === "form"`. Rendered in the order listed.
  fields?: ConfigureField[];
  // Link surfaced in the dialog footer for self-service docs.
  docs_url: string;
  description: string;
};

export const INTEGRATION_MANIFESTS: Record<IntegrationProvider, IntegrationManifest> = {
  github: {
    provider: "github",
    label: "GitHub",
    flow: "oauth",
    oauth_start_url: "/v1/integrations/github/install",
    docs_url: "/docs/integrations#github",
    description:
      "Install the NEXIS GitHub App so we can open pull requests and read repository metadata when proposing fixes.",
  },

  sentry: {
    provider: "sentry",
    label: "Sentry",
    flow: "form",
    docs_url: "/docs/integrations#sentry",
    description:
      "Connect your Sentry project so NEXIS can ingest incidents via REST API and propose fixes.",
    fields: [
      {
        name: "organization_slug",
        label: "Organization slug",
        type: "text",
        required: true,
        placeholder: "acme-inc",
      },
      {
        name: "project_slug",
        label: "Project slug",
        type: "text",
        required: true,
        placeholder: "backend-api",
      },
      {
        name: "auth_token",
        label: "Auth token",
        type: "password",
        required: true,
        help: "Settings → Auth Tokens → org:read + project:read scopes",
      },
    ],
  },

  argocd: {
    provider: "argocd",
    label: "ArgoCD",
    flow: "form",
    docs_url: "/docs/integrations#argocd",
    description:
      "Connect your ArgoCD server so NEXIS can sync deployments and roll back after a validated incident.",
    fields: [
      {
        name: "server_url",
        label: "Server URL",
        type: "url",
        required: true,
        placeholder: "https://argocd.example.com",
      },
      {
        name: "auth_token",
        label: "Auth token",
        type: "password",
        required: true,
      },
      {
        name: "project",
        label: "Project",
        type: "text",
        required: true,
        default: "default",
        placeholder: "default",
      },
      {
        name: "app_name",
        label: "Application name",
        type: "text",
        required: true,
        placeholder: "my-app",
      },
    ],
  },

  slack: {
    provider: "slack",
    label: "Slack",
    flow: "oauth",
    oauth_start_url: "/v1/integrations/slack/install",
    docs_url: "/docs/integrations#slack",
    description:
      "Install the NEXIS Slack app so we can post approval requests to a channel and DM the approver for high-severity incidents.",
  },

  datadog: {
    provider: "datadog",
    label: "Datadog",
    flow: "form",
    docs_url: "/docs/integrations#datadog",
    description:
      "Connect Datadog for metric-driven anomaly detection. Alerts feed Sentinel alongside Sentry events.",
    fields: [
      {
        name: "api_key",
        label: "API key",
        type: "password",
        required: true,
      },
      {
        name: "app_key",
        label: "Application key",
        type: "password",
        required: true,
      },
      {
        name: "site",
        label: "Datadog site",
        type: "select",
        required: true,
        default: "datadoghq.com",
        options: [
          { value: "datadoghq.com", label: "US1 (datadoghq.com)" },
          { value: "datadoghq.eu", label: "EU1 (datadoghq.eu)" },
          { value: "us3.datadoghq.com", label: "US3 (us3.datadoghq.com)" },
          { value: "us5.datadoghq.com", label: "US5 (us5.datadoghq.com)" },
          { value: "ddog-gov.com", label: "US1-FED (ddog-gov.com)" },
        ],
      },
    ],
  },

  pagerduty: {
    provider: "pagerduty",
    label: "PagerDuty",
    flow: "form",
    docs_url: "/docs/integrations#pagerduty",
    description:
      "Connect PagerDuty so NEXIS can query on-call schedules and trigger escalation when Sentinel detects something the customer hasn't seen.",
    fields: [
      {
        name: "api_token",
        label: "API token",
        type: "password",
        required: true,
        help: "REST API V2 token from Profile → User Settings",
      },
      {
        name: "service_id",
        label: "Service ID",
        type: "text",
        required: true,
        placeholder: "PXXXXXX",
      },
    ],
  },
};
