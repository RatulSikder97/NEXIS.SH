"use client";

// Phase 3.5 — Connect Project wizard.
//
// 5-step linear flow. Each step has a Next + Back button (except step 1's
// Back is disabled, and step 5's primary CTA is "Create project").
//
//   1. Basics              — name, environment, description, owner
//   2. Connect GitHub      — installation repos picker (or freeform owner/repo)
//   3. Connect Sentry      — org_slug + project_slug picker (or two text inputs)
//   4. Connect monitoring  — ArgoCD, PagerDuty, Datadog, Slack (all optional)
//   5. Recovery policy     — toggles + countdowns + kill switch + approvers
//
// On submit, POST /v1/workspaces/{ws}/projects, then router.push to the new
// project's detail page. Server-side validation errors render inline below
// the relevant step header.
//
// FE-first: the GitHub/Sentry/Slack picker endpoints (`/v1/integrations/
// github/repos`, `/v1/integrations/sentry/projects`, `/v1/integrations/
// slack/channels`) may 404 today — when they do, the picker degrades to a
// plain text input so the user can still type the selector by hand.

import * as React from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import type { Route } from "next";
import {
  AlertCircle,
  ArrowLeft,
  ArrowRight,
  CheckCircle2,
  ChevronsRight,
  Loader2,
  Plus,
  SkipForward,
  Sparkles,
  X,
} from "lucide-react";

import { Button } from "@/components/ui/Button";
import { cn } from "@/lib/utils";
import {
  DEFAULT_POLICY,
  projects,
  type CreateProjectInput,
  type ProjectEnvironment,
  type ProjectSelectors,
  type RecoveryPolicy,
} from "@/lib/projects";
import type { Integration } from "@/lib/integrations";
import type { MeResp } from "@/lib/auth";

const API = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

const STEPS = [
  { id: "basics", label: "Basics" },
  { id: "github", label: "GitHub" },
  { id: "sentry", label: "Sentry" },
  { id: "monitoring", label: "Monitoring" },
  { id: "policy", label: "Policy" },
] as const;
type StepId = (typeof STEPS)[number]["id"];

// GitHubRepo / SentryProject / SlackChannel are the shapes we expect from
// the integration discovery endpoints. We keep these loose because the
// endpoints may not be live yet — every consumer falls back to freeform
// inputs when the discovery returns null.
type GitHubRepo = {
  full_name: string;
  default_branch?: string;
  installation_id?: number;
};

type SentryProject = {
  organization_slug: string;
  project_slug: string;
  name?: string;
};

type SlackChannel = {
  id: string;
  name: string;
};

// genSlug derives a URL-safe slug from the user-typed name. The server
// will validate + normalise; this is just a friendly preview for the
// user so they can see what the slug will look like.
function genSlug(name: string): string {
  return name
    .toLowerCase()
    .trim()
    .replace(/[^a-z0-9-]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 60);
}

function Label({ children }: { children: React.ReactNode }) {
  return (
    <p className="text-[10px] font-semibold uppercase tracking-widest text-[var(--color-muted-foreground)]">
      {children}
    </p>
  );
}

function TextField({
  id,
  label,
  value,
  onChange,
  placeholder,
  hint,
  required,
  disabled,
}: {
  id: string;
  label: string;
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
  hint?: string;
  required?: boolean;
  disabled?: boolean;
}) {
  return (
    <div className="space-y-1.5">
      <label htmlFor={id} className="block">
        <Label>{label}</Label>
      </label>
      <input
        id={id}
        type="text"
        value={value}
        required={required}
        disabled={disabled}
        placeholder={placeholder}
        onChange={(e) => onChange(e.target.value)}
        className="w-full rounded-md border border-[var(--color-border)] bg-[var(--color-background)] px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-ring)]"
      />
      {hint && (
        <p className="text-xs text-[var(--color-muted-foreground)]">{hint}</p>
      )}
    </div>
  );
}

function Textarea({
  id,
  label,
  value,
  onChange,
  placeholder,
  hint,
  disabled,
}: {
  id: string;
  label: string;
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
  hint?: string;
  disabled?: boolean;
}) {
  return (
    <div className="space-y-1.5">
      <label htmlFor={id} className="block">
        <Label>{label}</Label>
      </label>
      <textarea
        id={id}
        value={value}
        disabled={disabled}
        placeholder={placeholder}
        onChange={(e) => onChange(e.target.value)}
        rows={3}
        className="w-full rounded-md border border-[var(--color-border)] bg-[var(--color-background)] px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-ring)]"
      />
      {hint && (
        <p className="text-xs text-[var(--color-muted-foreground)]">{hint}</p>
      )}
    </div>
  );
}

function EnvRadio({
  value,
  onChange,
  disabled,
}: {
  value: ProjectEnvironment;
  onChange: (v: ProjectEnvironment) => void;
  disabled?: boolean;
}) {
  const opts: Array<{ v: ProjectEnvironment; label: string; hint: string }> = [
    { v: "dev", label: "Dev", hint: "Engineering sandbox" },
    { v: "staging", label: "Staging", hint: "Pre-production" },
    { v: "prod", label: "Prod", hint: "Production traffic" },
  ];
  return (
    <div className="space-y-1.5">
      <Label>Environment</Label>
      <div className="grid grid-cols-1 gap-2 md:grid-cols-3">
        {opts.map((o) => {
          const active = value === o.v;
          return (
            <button
              type="button"
              key={o.v}
              onClick={() => onChange(o.v)}
              disabled={disabled}
              aria-pressed={active}
              className={cn(
                "rounded-lg border bg-[var(--color-card)] p-3 text-left transition-colors",
                active
                  ? "border-[var(--color-primary)] ring-1 ring-[var(--color-primary)]"
                  : "border-[var(--color-border)] hover:border-[var(--color-primary)]/40",
              )}
            >
              <p className="text-sm font-medium text-[var(--color-foreground)]">
                {o.label}
              </p>
              <p className="text-xs text-[var(--color-muted-foreground)]">
                {o.hint}
              </p>
            </button>
          );
        })}
      </div>
    </div>
  );
}

function ChipMultiInput({
  ids,
  onChange,
  placeholder,
  disabled,
}: {
  ids: string[];
  onChange: (v: string[]) => void;
  placeholder?: string;
  disabled?: boolean;
}) {
  const [draft, setDraft] = React.useState("");
  function commit(v: string) {
    const t = v.trim().replace(/,$/, "");
    if (!t || ids.includes(t)) {
      setDraft("");
      return;
    }
    onChange([...ids, t]);
    setDraft("");
  }
  return (
    <div className="flex flex-wrap items-center gap-2 rounded-md border border-[var(--color-border)] bg-[var(--color-background)] px-2 py-1.5">
      {ids.map((id) => (
        <span
          key={id}
          className="inline-flex items-center gap-1 rounded-full bg-[var(--color-muted)] px-2 py-0.5 font-mono text-xs"
        >
          {id}
          <button
            type="button"
            aria-label={`Remove ${id}`}
            onClick={() => onChange(ids.filter((v) => v !== id))}
            disabled={disabled}
            className="text-[var(--color-muted-foreground)] hover:text-[var(--color-foreground)]"
          >
            <X className="h-3 w-3" />
          </button>
        </span>
      ))}
      <input
        type="text"
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === ",") {
            e.preventDefault();
            commit(draft);
          } else if (e.key === "Backspace" && draft === "" && ids.length > 0) {
            onChange(ids.slice(0, -1));
          }
        }}
        onBlur={() => commit(draft)}
        placeholder={placeholder ?? "Add and press Enter"}
        disabled={disabled}
        className="flex-1 min-w-[8rem] border-0 bg-transparent px-1 py-0.5 text-sm outline-none placeholder:text-[var(--color-muted-foreground)]"
      />
      <Button
        type="button"
        size="sm"
        variant="ghost"
        onClick={() => commit(draft)}
        disabled={disabled || !draft.trim()}
      >
        <Plus className="h-4 w-4" />
      </Button>
    </div>
  );
}

// useDiscoveryEndpoint is the shared "try the endpoint, degrade to null
// when it 404s" pattern used by the GitHub / Sentry / Slack picker steps.
// The hook returns the rows (or null when the endpoint is missing) and a
// loading flag so the picker can show a spinner the first time.
//
// Implementation note: React 19's set-state-in-effect rule forbids
// synchronous setState calls in an effect body. We therefore keep the
// state map keyed by `url` and only mutate it from inside the async
// callback (a "subscribe for updates from an external system" pattern).
type DiscoveryEntry<T> = { rows: T[] | null; loading: boolean };

function useDiscoveryEndpoint<T>(
  url: string | null,
): DiscoveryEntry<T> {
  const [state, setState] = React.useState<Record<string, DiscoveryEntry<T>>>(
    {},
  );
  React.useEffect(() => {
    if (!url) return;
    // Already loaded — don't re-fire if the same url is still mounted.
    if (state[url] && !state[url].loading) return;
    let cancelled = false;
    // Defer the "loading=true" pulse to the next microtask so we don't
    // setState synchronously inside the effect body (which React 19 flags
    // as a cascading render).
    void Promise.resolve().then(() => {
      if (cancelled) return;
      setState((s) => ({
        ...s,
        [url]: { rows: null, loading: true },
      }));
    });
    void (async () => {
      try {
        const r = await fetch(url, {
          credentials: "include",
          cache: "no-store",
        });
        if (cancelled) return;
        if (!r.ok) {
          setState((s) => ({ ...s, [url]: { rows: null, loading: false } }));
          return;
        }
        const body = (await r.json()) as unknown;
        if (cancelled) return;
        setState((s) => ({
          ...s,
          [url]: {
            rows: Array.isArray(body) ? (body as T[]) : [],
            loading: false,
          },
        }));
      } catch {
        if (!cancelled) {
          setState((s) => ({
            ...s,
            [url]: { rows: null, loading: false },
          }));
        }
      }
    })();
    return () => {
      cancelled = true;
    };
    // We intentionally only refetch when the URL changes; rerunning when
    // `state` flips would cause a request storm.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [url]);
  if (!url) return { rows: null, loading: false };
  return state[url] ?? { rows: null, loading: true };
}

function StepHeader({
  current,
  total,
  title,
  description,
}: {
  current: number;
  total: number;
  title: string;
  description: string;
}) {
  return (
    <div className="space-y-2">
      <p className="text-[10px] font-semibold uppercase tracking-widest text-[var(--color-muted-foreground)]">
        Step {current} of {total}
      </p>
      <h1 className="text-2xl font-semibold text-[var(--color-foreground)]">
        {title}
      </h1>
      <p className="max-w-2xl text-sm text-[var(--color-muted-foreground)]">
        {description}
      </p>
    </div>
  );
}

function StepProgress({ stepIndex }: { stepIndex: number }) {
  return (
    <ol className="flex items-center gap-1 rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-1">
      {STEPS.map((s, i) => {
        const active = i === stepIndex;
        const done = i < stepIndex;
        return (
          <li
            key={s.id}
            aria-current={active ? "step" : undefined}
            className={cn(
              "flex-1 rounded-md px-3 py-2 text-center text-xs font-medium",
              active
                ? "bg-[var(--color-muted)] text-[var(--color-foreground)]"
                : done
                  ? "text-[var(--color-primary)]"
                  : "text-[var(--color-muted-foreground)]",
            )}
          >
            <span className="mr-2 inline-flex h-4 w-4 items-center justify-center rounded-full bg-[var(--color-muted)] text-[10px]">
              {done ? <CheckCircle2 className="h-3 w-3" /> : i + 1}
            </span>
            {s.label}
          </li>
        );
      })}
    </ol>
  );
}

type FormState = {
  name: string;
  description: string;
  environment: ProjectEnvironment;
  owner_user_id: string;
  selectors: ProjectSelectors;
  policy: RecoveryPolicy;
};

export function ProjectConnectWizard({
  workspaceId,
  workspaceName,
  me,
  integrations,
}: {
  workspaceId: string;
  workspaceName: string;
  me: MeResp;
  integrations: Integration[];
}) {
  const router = useRouter();
  const [stepIndex, setStepIndex] = React.useState<number>(0);
  const [submitting, setSubmitting] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);

  const githubConnected = integrations.some(
    (i) => i.provider === "github" && i.status === "connected",
  );
  const sentryConnected = integrations.some(
    (i) => i.provider === "sentry" && i.status === "connected",
  );
  // Slack/argocd/etc don't enforce a `connected` gate in this wizard —
  // the inputs are always available so the user can paste selectors even
  // when the org-wide integration hasn't been configured yet (the project
  // selector is independently useful).

  const [form, setForm] = React.useState<FormState>({
    name: "",
    description: "",
    environment: "dev",
    owner_user_id: me.user.id,
    selectors: {},
    policy: DEFAULT_POLICY,
  });

  function step(): StepId {
    return STEPS[stepIndex].id;
  }

  function patchForm<K extends keyof FormState>(key: K, value: FormState[K]) {
    setForm((f) => ({ ...f, [key]: value }));
  }
  function patchSelectors(next: Partial<ProjectSelectors>) {
    setForm((f) => ({ ...f, selectors: { ...f.selectors, ...next } }));
  }
  function patchPolicy<K extends keyof RecoveryPolicy>(
    key: K,
    value: RecoveryPolicy[K],
  ) {
    setForm((f) => ({ ...f, policy: { ...f.policy, [key]: value } }));
  }

  function canAdvance(): boolean {
    if (step() === "basics") {
      return form.name.trim().length > 0;
    }
    return true;
  }

  function next() {
    if (!canAdvance()) return;
    setError(null);
    setStepIndex((i) => Math.min(STEPS.length - 1, i + 1));
  }
  function back() {
    setError(null);
    setStepIndex((i) => Math.max(0, i - 1));
  }
  function skip() {
    setError(null);
    setStepIndex((i) => Math.min(STEPS.length - 1, i + 1));
  }

  async function onSubmit() {
    setSubmitting(true);
    setError(null);
    try {
      const input: CreateProjectInput = {
        name: form.name.trim(),
        slug: genSlug(form.name),
        description: form.description.trim(),
        environment: form.environment,
        owner_user_id: form.owner_user_id || undefined,
        selectors: form.selectors,
        recovery_policy: form.policy,
      };
      const created = await projects.create(workspaceId, input);
      router.push(`/console/projects/${created.id}` as Route);
    } catch (err) {
      setError(
        err instanceof Error
          ? err.message
          : "Failed to create project. Please retry.",
      );
      setSubmitting(false);
    }
  }

  // ------- Step renderers -------
  const githubReposQ = useDiscoveryEndpoint<GitHubRepo>(
    step() === "github" && githubConnected
      ? `${API}/v1/integrations/github/repos`
      : null,
  );
  const sentryProjectsQ = useDiscoveryEndpoint<SentryProject>(
    step() === "sentry" && sentryConnected
      ? `${API}/v1/integrations/sentry/projects`
      : null,
  );
  const slackChannelsQ = useDiscoveryEndpoint<SlackChannel>(
    step() === "monitoring"
      ? `${API}/v1/integrations/slack/channels`
      : null,
  );

  function renderBasics() {
    return (
      <div className="space-y-5">
        <TextField
          id="proj-name"
          label="Name"
          value={form.name}
          onChange={(v) => patchForm("name", v)}
          placeholder="checkout-service"
          required
          disabled={submitting}
        />
        <p className="text-xs text-[var(--color-muted-foreground)]">
          Slug preview: <span className="font-mono">{genSlug(form.name) || "—"}</span>
        </p>
        <EnvRadio
          value={form.environment}
          onChange={(v) => patchForm("environment", v)}
          disabled={submitting}
        />
        <Textarea
          id="proj-desc"
          label="Description"
          value={form.description}
          onChange={(v) => patchForm("description", v)}
          placeholder="What this service does, who owns it, and how to reach the on-call."
          disabled={submitting}
        />
        <TextField
          id="proj-owner"
          label="Owner user ID"
          value={form.owner_user_id}
          onChange={(v) => patchForm("owner_user_id", v)}
          placeholder={me.user.id}
          hint={`Defaults to you (${me.user.email}).`}
          disabled={submitting}
        />
      </div>
    );
  }

  function renderGithub() {
    if (!githubConnected) {
      return (
        <div className="space-y-4">
          <div
            role="status"
            className="flex items-start gap-3 rounded-md border border-amber-500/30 bg-amber-500/10 p-4 text-sm text-amber-700 dark:text-amber-300"
          >
            <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
            <div>
              <p className="font-semibold">GitHub integration not connected.</p>
              <p className="text-xs">
                Connect GitHub from the Integrations page, then return here.
                You can also enter a repo manually below.
              </p>
            </div>
          </div>
          <TextField
            id="gh-repo"
            label="Repository"
            value={form.selectors.github_repo ?? ""}
            onChange={(v) => patchSelectors({ github_repo: v })}
            placeholder="acme-co/checkout"
            hint="Format: owner/repo. Required for PR generation."
            disabled={submitting}
          />
          <TextField
            id="gh-branch"
            label="Default branch"
            value={form.selectors.github_default_branch ?? ""}
            onChange={(v) => patchSelectors({ github_default_branch: v })}
            placeholder="main"
            disabled={submitting}
          />
          <div>
            <Button asChild variant="outline" size="sm">
              <Link href={"/console/integrations" as Route}>
                Open Integrations
                <ChevronsRight className="h-4 w-4" />
              </Link>
            </Button>
          </div>
        </div>
      );
    }
    const { rows, loading } = githubReposQ;
    if (loading) {
      return (
        <div className="flex items-center gap-2 text-sm text-[var(--color-muted-foreground)]">
          <Loader2 className="h-4 w-4 animate-spin" />
          Loading repositories from your installation…
        </div>
      );
    }
    if (rows === null || rows.length === 0) {
      return (
        <div className="space-y-4">
          <p className="text-sm text-[var(--color-muted-foreground)]">
            We couldn&apos;t fetch the repository list for your installation.
            Enter the repo manually below.
          </p>
          <TextField
            id="gh-repo"
            label="Repository"
            value={form.selectors.github_repo ?? ""}
            onChange={(v) => patchSelectors({ github_repo: v })}
            placeholder="acme-co/checkout"
            hint="Format: owner/repo."
            disabled={submitting}
          />
          <TextField
            id="gh-branch"
            label="Default branch"
            value={form.selectors.github_default_branch ?? ""}
            onChange={(v) => patchSelectors({ github_default_branch: v })}
            placeholder="main"
            disabled={submitting}
          />
        </div>
      );
    }
    return (
      <div className="space-y-3">
        <Label>Pick a repository</Label>
        <ul className="max-h-[18rem] space-y-1.5 overflow-y-auto rounded-md border border-[var(--color-border)] bg-[var(--color-background)] p-2">
          {rows.map((r) => {
            const active = form.selectors.github_repo === r.full_name;
            return (
              <li key={r.full_name}>
                <button
                  type="button"
                  onClick={() =>
                    patchSelectors({
                      github_repo: r.full_name,
                      github_default_branch: r.default_branch,
                      github_installation_id: r.installation_id,
                    })
                  }
                  disabled={submitting}
                  aria-pressed={active}
                  className={cn(
                    "flex w-full items-center justify-between rounded-md px-3 py-2 text-left text-sm transition-colors",
                    active
                      ? "bg-[var(--color-primary)]/10 ring-1 ring-[var(--color-primary)]"
                      : "hover:bg-[var(--color-muted)]/50",
                  )}
                >
                  <span className="font-mono">{r.full_name}</span>
                  {r.default_branch && (
                    <span className="font-mono text-xs text-[var(--color-muted-foreground)]">
                      {r.default_branch}
                    </span>
                  )}
                </button>
              </li>
            );
          })}
        </ul>
      </div>
    );
  }

  function renderSentry() {
    if (!sentryConnected) {
      return (
        <div className="space-y-4">
          <div
            role="status"
            className="flex items-start gap-3 rounded-md border border-amber-500/30 bg-amber-500/10 p-4 text-sm text-amber-700 dark:text-amber-300"
          >
            <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
            <p>
              Sentry integration not connected. You can paste the slugs manually
              below or skip this step.
            </p>
          </div>
          <TextField
            id="sentry-org"
            label="Sentry org slug"
            value={form.selectors.sentry_organization_slug ?? ""}
            onChange={(v) => patchSelectors({ sentry_organization_slug: v })}
            placeholder="acme"
            disabled={submitting}
          />
          <TextField
            id="sentry-project"
            label="Sentry project slug"
            value={form.selectors.sentry_project_slug ?? ""}
            onChange={(v) => patchSelectors({ sentry_project_slug: v })}
            placeholder="checkout"
            disabled={submitting}
          />
        </div>
      );
    }
    const { rows, loading } = sentryProjectsQ;
    if (loading) {
      return (
        <div className="flex items-center gap-2 text-sm text-[var(--color-muted-foreground)]">
          <Loader2 className="h-4 w-4 animate-spin" />
          Loading Sentry projects…
        </div>
      );
    }
    if (rows === null || rows.length === 0) {
      return (
        <div className="space-y-4">
          <p className="text-sm text-[var(--color-muted-foreground)]">
            Project discovery isn&apos;t available — paste the slugs manually.
          </p>
          <TextField
            id="sentry-org"
            label="Sentry org slug"
            value={form.selectors.sentry_organization_slug ?? ""}
            onChange={(v) => patchSelectors({ sentry_organization_slug: v })}
            placeholder="acme"
            disabled={submitting}
          />
          <TextField
            id="sentry-project"
            label="Sentry project slug"
            value={form.selectors.sentry_project_slug ?? ""}
            onChange={(v) => patchSelectors({ sentry_project_slug: v })}
            placeholder="checkout"
            disabled={submitting}
          />
        </div>
      );
    }
    return (
      <div className="space-y-3">
        <Label>Pick a Sentry project</Label>
        <ul className="max-h-[18rem] space-y-1.5 overflow-y-auto rounded-md border border-[var(--color-border)] bg-[var(--color-background)] p-2">
          {rows.map((r) => {
            const active =
              form.selectors.sentry_organization_slug === r.organization_slug &&
              form.selectors.sentry_project_slug === r.project_slug;
            return (
              <li key={`${r.organization_slug}/${r.project_slug}`}>
                <button
                  type="button"
                  onClick={() =>
                    patchSelectors({
                      sentry_organization_slug: r.organization_slug,
                      sentry_project_slug: r.project_slug,
                    })
                  }
                  disabled={submitting}
                  aria-pressed={active}
                  className={cn(
                    "flex w-full items-center justify-between rounded-md px-3 py-2 text-left text-sm transition-colors",
                    active
                      ? "bg-[var(--color-primary)]/10 ring-1 ring-[var(--color-primary)]"
                      : "hover:bg-[var(--color-muted)]/50",
                  )}
                >
                  <span className="font-mono">
                    {r.organization_slug}/{r.project_slug}
                  </span>
                  {r.name && (
                    <span className="text-xs text-[var(--color-muted-foreground)]">
                      {r.name}
                    </span>
                  )}
                </button>
              </li>
            );
          })}
        </ul>
      </div>
    );
  }

  function renderMonitoring() {
    const { rows: slackRows, loading: slackLoading } = slackChannelsQ;
    return (
      <div className="space-y-6">
        <section className="space-y-3">
          <Label>ArgoCD</Label>
          <TextField
            id="argocd-server"
            label="Server URL"
            value={form.selectors.argocd_server_url ?? ""}
            onChange={(v) => patchSelectors({ argocd_server_url: v })}
            placeholder="https://argocd.acme.io"
            disabled={submitting}
          />
          <TextField
            id="argocd-app"
            label="App name"
            value={form.selectors.argocd_app_name ?? ""}
            onChange={(v) => patchSelectors({ argocd_app_name: v })}
            placeholder="checkout-prod"
            disabled={submitting}
          />
          <TextField
            id="argocd-project"
            label="Project"
            value={form.selectors.argocd_project ?? ""}
            onChange={(v) => patchSelectors({ argocd_project: v })}
            placeholder="default"
            disabled={submitting}
          />
        </section>

        <section className="space-y-3">
          <Label>PagerDuty</Label>
          <TextField
            id="pd-service"
            label="Service ID"
            value={form.selectors.pagerduty_service_id ?? ""}
            onChange={(v) => patchSelectors({ pagerduty_service_id: v })}
            placeholder="P1AB2CD"
            disabled={submitting}
          />
          <TextField
            id="pd-escalation"
            label="Escalation policy ID"
            value={form.selectors.pagerduty_escalation_policy_id ?? ""}
            onChange={(v) =>
              patchSelectors({ pagerduty_escalation_policy_id: v })
            }
            placeholder="P3XY4Z"
            disabled={submitting}
          />
        </section>

        <section className="space-y-3">
          <Label>Datadog</Label>
          <TextField
            id="dd-service"
            label="Service tag"
            value={form.selectors.datadog_service_tag ?? ""}
            onChange={(v) => patchSelectors({ datadog_service_tag: v })}
            placeholder="service:checkout"
            disabled={submitting}
          />
          <TextField
            id="dd-env"
            label="Env tag"
            value={form.selectors.datadog_env_tag ?? ""}
            onChange={(v) => patchSelectors({ datadog_env_tag: v })}
            placeholder="env:prod"
            disabled={submitting}
          />
        </section>

        <section className="space-y-3">
          <Label>Slack</Label>
          {slackLoading ? (
            <div className="flex items-center gap-2 text-sm text-[var(--color-muted-foreground)]">
              <Loader2 className="h-4 w-4 animate-spin" />
              Loading channels…
            </div>
          ) : slackRows === null || slackRows.length === 0 ? (
            <TextField
              id="slack-channel"
              label="Channel ID"
              value={form.selectors.slack_channel_id ?? ""}
              onChange={(v) => patchSelectors({ slack_channel_id: v })}
              placeholder="C01234ABCDE"
              hint="Channel discovery is unavailable — paste the channel ID."
              disabled={submitting}
            />
          ) : (
            <SlackChannelAutocomplete
              channels={slackRows}
              value={form.selectors.slack_channel_id ?? ""}
              onChange={(v) => patchSelectors({ slack_channel_id: v })}
              disabled={submitting}
            />
          )}
        </section>
      </div>
    );
  }

  function renderPolicy() {
    return (
      <div className="space-y-4">
        <ToggleRow
          label="Auto-merge low severity"
          description="Land low-severity PRs without operator review."
          checked={form.policy.auto_merge_low_severity}
          onChange={(v) => patchPolicy("auto_merge_low_severity", v)}
          disabled={submitting}
        />
        <ToggleRow
          label="Auto-merge medium severity"
          description="Land medium-severity PRs after the countdown elapses."
          checked={form.policy.auto_merge_medium_severity}
          onChange={(v) => patchPolicy("auto_merge_medium_severity", v)}
          disabled={submitting}
        />
        <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-4">
          <label htmlFor="countdown" className="block">
            <Label>Medium countdown (seconds)</Label>
          </label>
          <input
            id="countdown"
            type="number"
            min={10}
            max={600}
            value={form.policy.medium_countdown_seconds}
            disabled={submitting}
            onChange={(e) => {
              const n = Number.parseInt(e.target.value, 10);
              if (!Number.isNaN(n)) {
                patchPolicy(
                  "medium_countdown_seconds",
                  Math.max(10, Math.min(600, n)),
                );
              }
            }}
            className="mt-2 w-32 rounded-md border border-[var(--color-border)] bg-[var(--color-background)] px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-ring)]"
          />
        </div>
        <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-4">
          <label htmlFor="concurrency" className="block">
            <Label>Max concurrent recoveries</Label>
          </label>
          <input
            id="concurrency"
            type="number"
            min={1}
            max={10}
            value={form.policy.max_concurrent_recoveries}
            disabled={submitting}
            onChange={(e) => {
              const n = Number.parseInt(e.target.value, 10);
              if (!Number.isNaN(n)) {
                patchPolicy(
                  "max_concurrent_recoveries",
                  Math.max(1, Math.min(10, n)),
                );
              }
            }}
            className="mt-2 w-32 rounded-md border border-[var(--color-border)] bg-[var(--color-background)] px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-ring)]"
          />
        </div>
        <ToggleRow
          label="Rollback on SLO breach"
          description="Auto-revert deploys when post-merge SLO probes fail."
          checked={form.policy.rollback_on_slo_breach}
          onChange={(v) => patchPolicy("rollback_on_slo_breach", v)}
          disabled={submitting}
        />
        <ToggleRow
          label="Engage kill switch on create"
          description="Start the project with auto-recovery disabled. Useful for shadow-mode rollouts."
          checked={form.policy.kill_switch_enabled}
          onChange={(v) => patchPolicy("kill_switch_enabled", v)}
          disabled={submitting}
          tone="danger"
        />
        <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-4">
          <Label>Approver user IDs</Label>
          <p className="mt-1 text-xs text-[var(--color-muted-foreground)]">
            Leave empty to use the org default routing.
          </p>
          <div className="mt-3">
            <ChipMultiInput
              ids={form.policy.approver_user_ids}
              onChange={(v) => patchPolicy("approver_user_ids", v)}
              placeholder="Paste user ID and press Enter"
              disabled={submitting}
            />
          </div>
        </div>
      </div>
    );
  }

  const isLast = stepIndex === STEPS.length - 1;
  const isFirst = stepIndex === 0;

  function renderActiveStep(): React.ReactNode {
    switch (step()) {
      case "basics":
        return renderBasics();
      case "github":
        return renderGithub();
      case "sentry":
        return renderSentry();
      case "monitoring":
        return renderMonitoring();
      case "policy":
        return renderPolicy();
    }
  }

  function stepTitle(): { title: string; description: string } {
    switch (step()) {
      case "basics":
        return {
          title: "Project basics",
          description: `Tell us about the service inside ${workspaceName}.`,
        };
      case "github":
        return {
          title: "Connect GitHub",
          description:
            "Pick the repository that PRs should land in. Required for code-change recoveries; skip if you only want monitoring + alerts.",
        };
      case "sentry":
        return {
          title: "Connect Sentry",
          description:
            "Wire the Sentry project so error events route into recovery pipelines.",
        };
      case "monitoring":
        return {
          title: "Monitoring & deploys",
          description:
            "Optional integrations: ArgoCD for rollback, PagerDuty for paging, Datadog for SLOs, Slack for notifications.",
        };
      case "policy":
        return {
          title: "Recovery policy",
          description:
            "Set the auto-merge thresholds, countdowns, and approvers. Conservative defaults are applied — adjust as needed.",
        };
    }
  }

  const { title, description } = stepTitle();

  return (
    <div className="space-y-6">
      <Button asChild variant="ghost" size="sm">
        <Link href={"/console/projects" as Route}>
          <ArrowLeft className="h-4 w-4" />
          Back to projects
        </Link>
      </Button>

      <StepProgress stepIndex={stepIndex} />

      <StepHeader
        current={stepIndex + 1}
        total={STEPS.length}
        title={title}
        description={description}
      />

      {error && (
        <div
          role="alert"
          className="flex items-start gap-2 rounded-md border border-red-500/30 bg-red-500/10 p-4 text-sm text-red-700 dark:text-red-300"
        >
          <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
          <span>{error}</span>
        </div>
      )}

      <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-5">
        {renderActiveStep()}
      </div>

      <div className="flex items-center justify-between gap-2 pt-2">
        <Button
          type="button"
          variant="ghost"
          onClick={back}
          disabled={isFirst || submitting}
        >
          <ArrowLeft className="h-4 w-4" />
          Back
        </Button>
        <div className="flex items-center gap-2">
          {!isLast && step() !== "basics" && (
            <Button
              type="button"
              variant="ghost"
              onClick={skip}
              disabled={submitting}
            >
              <SkipForward className="h-4 w-4" />
              Skip
            </Button>
          )}
          {!isLast ? (
            <Button
              type="button"
              onClick={next}
              disabled={!canAdvance() || submitting}
            >
              Next
              <ArrowRight className="h-4 w-4" />
            </Button>
          ) : (
            <Button type="button" onClick={onSubmit} disabled={submitting}>
              {submitting ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                <Sparkles className="h-4 w-4" />
              )}
              Create project
            </Button>
          )}
        </div>
      </div>
    </div>
  );
}

// ------- Internal helpers used by the wizard but kept inline so the
// surface stays self-contained. ToggleRow + SlackChannelAutocomplete
// don't live in their own files because they're only needed here.

function ToggleRow({
  label,
  description,
  checked,
  onChange,
  disabled,
  tone,
}: {
  label: string;
  description: string;
  checked: boolean;
  onChange: (v: boolean) => void;
  disabled?: boolean;
  tone?: "danger";
}) {
  return (
    <label
      className={cn(
        "flex cursor-pointer items-start justify-between gap-4 rounded-lg border bg-[var(--color-card)] p-4",
        tone === "danger" && checked
          ? "border-red-500/40"
          : "border-[var(--color-border)]",
        disabled && "cursor-not-allowed opacity-60",
      )}
    >
      <div>
        <p className="text-sm font-medium text-[var(--color-foreground)]">
          {label}
        </p>
        <p className="mt-1 max-w-xl text-xs text-[var(--color-muted-foreground)]">
          {description}
        </p>
      </div>
      <span
        className={cn(
          "relative mt-0.5 inline-flex h-5 w-9 shrink-0 items-center rounded-full transition-colors",
          checked
            ? tone === "danger"
              ? "bg-red-500"
              : "bg-[var(--color-primary)]"
            : "bg-[var(--color-muted)]",
        )}
      >
        <span
          className={cn(
            "inline-block h-4 w-4 transform rounded-full bg-white shadow transition-transform",
            checked ? "translate-x-4" : "translate-x-0.5",
          )}
        />
        <input
          type="checkbox"
          className="sr-only"
          checked={checked}
          onChange={(e) => onChange(e.target.checked)}
          disabled={disabled}
          aria-label={label}
        />
      </span>
    </label>
  );
}

function SlackChannelAutocomplete({
  channels,
  value,
  onChange,
  disabled,
}: {
  channels: SlackChannel[];
  value: string;
  onChange: (v: string) => void;
  disabled?: boolean;
}) {
  const [query, setQuery] = React.useState<string>(() => {
    const match = channels.find((c) => c.id === value);
    return match ? `#${match.name}` : value;
  });

  const filtered = React.useMemo(() => {
    const q = query.trim().toLowerCase().replace(/^#/, "");
    if (!q) return channels.slice(0, 20);
    return channels
      .filter((c) => c.name.toLowerCase().includes(q))
      .slice(0, 20);
  }, [channels, query]);

  return (
    <div className="space-y-2">
      <input
        type="text"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        disabled={disabled}
        placeholder="Search channels…"
        className="w-full rounded-md border border-[var(--color-border)] bg-[var(--color-background)] px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-ring)]"
      />
      <ul className="max-h-[14rem] space-y-1 overflow-y-auto rounded-md border border-[var(--color-border)] bg-[var(--color-background)] p-2">
        {filtered.map((c) => {
          const active = c.id === value;
          return (
            <li key={c.id}>
              <button
                type="button"
                onClick={() => {
                  onChange(c.id);
                  setQuery(`#${c.name}`);
                }}
                disabled={disabled}
                aria-pressed={active}
                className={cn(
                  "flex w-full items-center justify-between rounded-md px-3 py-1.5 text-left text-sm",
                  active
                    ? "bg-[var(--color-primary)]/10 ring-1 ring-[var(--color-primary)]"
                    : "hover:bg-[var(--color-muted)]/50",
                )}
              >
                <span># {c.name}</span>
                <span className="font-mono text-[10px] text-[var(--color-muted-foreground)]">
                  {c.id}
                </span>
              </button>
            </li>
          );
        })}
        {filtered.length === 0 && (
          <li className="px-3 py-1.5 text-xs text-[var(--color-muted-foreground)]">
            No channels match.
          </li>
        )}
      </ul>
    </div>
  );
}
