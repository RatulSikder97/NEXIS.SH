"use client";

// Phase 3.5 — Project recovery-policy editor.
//
// Form for the 7 policy fields:
//
//   * auto_merge_low_severity         — toggle
//   * auto_merge_medium_severity      — toggle
//   * medium_countdown_seconds        — number input (10–600)
//   * kill_switch_enabled             — toggle
//   * approver_user_ids               — chip multi-input
//   * max_concurrent_recoveries       — number input (1–10)
//   * rollback_on_slo_breach          — toggle
//
// PATCH semantics: we call `projects.updatePolicy(id, policy)` which PUTs
// the whole block at once (replace, not merge). Save returns the updated
// project; we don't need to refresh because the form mirrors the state.
//
// Toast pattern: we don't have a global toast bus today, so we mirror the
// inline banner pattern used elsewhere in the console (settings/api-keys
// + integrations) — a transient success/error banner under the header.

import * as React from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import type { Route } from "next";
import {
  ArrowLeft,
  CheckCircle2,
  Loader2,
  Plus,
  Save,
  X,
  Zap,
} from "lucide-react";

import { Button } from "@/components/ui/Button";
import { cn } from "@/lib/utils";
import { projects, type RecoveryPolicy } from "@/lib/projects";

const COUNTDOWN_MIN = 10;
const COUNTDOWN_MAX = 600;
const CONCURRENCY_MIN = 1;
const CONCURRENCY_MAX = 10;

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
  onChange: (next: boolean) => void;
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

function NumberRow({
  label,
  description,
  value,
  onChange,
  min,
  max,
  suffix,
  disabled,
}: {
  label: string;
  description: string;
  value: number;
  onChange: (next: number) => void;
  min: number;
  max: number;
  suffix?: string;
  disabled?: boolean;
}) {
  return (
    <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-4">
      <div className="flex flex-col gap-1">
        <label
          htmlFor={label}
          className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]"
        >
          {label}
        </label>
        <p className="text-xs text-[var(--color-muted-foreground)]">
          {description}
        </p>
      </div>
      <div className="mt-3 flex items-center gap-2">
        <input
          id={label}
          type="number"
          min={min}
          max={max}
          value={Number.isFinite(value) ? value : min}
          onChange={(e) => {
            const n = Number.parseInt(e.target.value, 10);
            if (Number.isNaN(n)) return;
            onChange(Math.max(min, Math.min(max, n)));
          }}
          disabled={disabled}
          className="w-32 rounded-md border border-[var(--color-border)] bg-[var(--color-background)] px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-ring)]"
        />
        {suffix && (
          <span className="text-xs text-[var(--color-muted-foreground)]">
            {suffix}
          </span>
        )}
        <span className="ml-auto text-[10px] uppercase tracking-widest text-[var(--color-muted-foreground)]">
          Range {min}–{max}
        </span>
      </div>
    </div>
  );
}

function ApproverChips({
  ids,
  onChange,
  disabled,
}: {
  ids: string[];
  onChange: (next: string[]) => void;
  disabled?: boolean;
}) {
  const [draft, setDraft] = React.useState("");

  function commit(value: string) {
    const trimmed = value.trim().replace(/,$/, "");
    if (!trimmed) return;
    if (ids.includes(trimmed)) {
      setDraft("");
      return;
    }
    onChange([...ids, trimmed]);
    setDraft("");
  }

  function onKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
    if (e.key === "Enter" || e.key === ",") {
      e.preventDefault();
      commit(draft);
    } else if (e.key === "Backspace" && draft === "" && ids.length > 0) {
      onChange(ids.slice(0, -1));
    }
  }

  function remove(id: string) {
    onChange(ids.filter((v) => v !== id));
  }

  return (
    <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-4">
      <label
        htmlFor="approvers"
        className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]"
      >
        Approver user IDs
      </label>
      <p className="mt-1 text-xs text-[var(--color-muted-foreground)]">
        Users who can sign off on medium and high severity recoveries. Paste a
        user ID and press Enter. Leave empty to fall back to the org default
        routing.
      </p>
      <div className="mt-3 flex flex-wrap items-center gap-2 rounded-md border border-[var(--color-border)] bg-[var(--color-background)] px-2 py-1.5">
        {ids.map((id) => (
          <span
            key={id}
            className="inline-flex items-center gap-1 rounded-full bg-[var(--color-muted)] px-2 py-0.5 text-xs font-mono text-[var(--color-foreground)]"
          >
            {id}
            <button
              type="button"
              aria-label={`Remove ${id}`}
              onClick={() => remove(id)}
              disabled={disabled}
              className="rounded text-[var(--color-muted-foreground)] hover:text-[var(--color-foreground)]"
            >
              <X className="h-3 w-3" />
            </button>
          </span>
        ))}
        <input
          id="approvers"
          type="text"
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={onKeyDown}
          onBlur={() => commit(draft)}
          disabled={disabled}
          placeholder={ids.length === 0 ? "Paste user ID and press Enter" : ""}
          className="flex-1 min-w-[10rem] border-0 bg-transparent px-2 py-1 text-sm outline-none placeholder:text-[var(--color-muted-foreground)]"
        />
        <Button
          type="button"
          size="sm"
          variant="ghost"
          onClick={() => commit(draft)}
          disabled={disabled || !draft.trim()}
        >
          <Plus className="h-4 w-4" />
          Add
        </Button>
      </div>
    </div>
  );
}

export function ProjectSettingsClient({
  projectId,
  projectName,
  initialPolicy,
  policyEndpointMissing,
}: {
  projectId: string;
  projectName: string;
  initialPolicy: RecoveryPolicy;
  policyEndpointMissing: boolean;
}) {
  const router = useRouter();
  const [policy, setPolicy] = React.useState<RecoveryPolicy>(initialPolicy);
  const [saving, setSaving] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  const [success, setSuccess] = React.useState<string | null>(null);

  // Auto-dismiss the success toast after 4s so the form doesn't carry stale
  // confirmation text once the user moves on.
  React.useEffect(() => {
    if (!success) return;
    const id = window.setTimeout(() => setSuccess(null), 4_000);
    return () => window.clearTimeout(id);
  }, [success]);

  function patch<K extends keyof RecoveryPolicy>(
    key: K,
    value: RecoveryPolicy[K],
  ) {
    setPolicy((p) => ({ ...p, [key]: value }));
  }

  async function onSave(e: React.FormEvent) {
    e.preventDefault();
    setSaving(true);
    setError(null);
    setSuccess(null);
    try {
      await projects.updatePolicy(projectId, policy);
      setSuccess("Recovery policy saved.");
      router.refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to save policy");
    } finally {
      setSaving(false);
    }
  }

  return (
    <form onSubmit={onSave} className="space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <Button asChild variant="ghost" size="sm">
            <Link href={`/console/projects/${projectId}` as Route}>
              <ArrowLeft className="h-4 w-4" />
              Back to project
            </Link>
          </Button>
          <h1 className="mt-2 text-2xl font-semibold">Recovery policy</h1>
          <p className="mt-1 text-sm text-[var(--color-muted-foreground)]">
            {projectName} · changes apply to all future recovery runs.
          </p>
        </div>
        <Button type="submit" disabled={saving}>
          {saving ? (
            <Loader2 className="h-4 w-4 animate-spin" />
          ) : (
            <Save className="h-4 w-4" />
          )}
          Save policy
        </Button>
      </div>

      {policy.kill_switch_enabled && (
        <div
          role="alert"
          className="flex items-start gap-3 rounded-md border border-red-500/30 bg-red-500/10 p-4 text-sm text-red-700 dark:text-red-300"
        >
          <Zap className="mt-0.5 h-4 w-4 shrink-0" />
          <p>
            Kill switch is ENGAGED. Auto-recovery is disabled for this project
            until the toggle is cleared below.
          </p>
        </div>
      )}

      {policyEndpointMissing && (
        <div
          role="status"
          className="rounded-md border border-amber-500/30 bg-amber-500/10 px-4 py-3 text-sm text-amber-700 dark:text-amber-300"
        >
          The recovery-policy API is still rolling out. Saving will queue your
          changes against the project until the dedicated endpoint lands.
        </div>
      )}

      {error && (
        <div
          role="alert"
          className="rounded-md border border-red-500/30 bg-red-500/10 px-4 py-3 text-sm text-red-700 dark:text-red-300"
        >
          {error}
        </div>
      )}

      {success && (
        <div
          role="status"
          className="flex items-center gap-2 rounded-md border border-emerald-500/30 bg-emerald-500/10 px-4 py-3 text-sm text-emerald-700 dark:text-emerald-300"
        >
          <CheckCircle2 className="h-4 w-4" />
          {success}
        </div>
      )}

      <section className="space-y-3">
        <h2 className="text-[10px] font-semibold uppercase tracking-widest text-[var(--color-muted-foreground)]">
          Auto-merge thresholds
        </h2>
        <ToggleRow
          label="Auto-merge low severity"
          description="Land low-severity PRs without waiting for an operator."
          checked={policy.auto_merge_low_severity}
          onChange={(v) => patch("auto_merge_low_severity", v)}
          disabled={saving}
        />
        <ToggleRow
          label="Auto-merge medium severity"
          description="Land medium-severity PRs after the countdown below elapses."
          checked={policy.auto_merge_medium_severity}
          onChange={(v) => patch("auto_merge_medium_severity", v)}
          disabled={saving}
        />
        <NumberRow
          label="Medium countdown"
          description="Cooldown applied to medium-severity auto-merges so an operator can cancel."
          value={policy.medium_countdown_seconds}
          onChange={(v) => patch("medium_countdown_seconds", v)}
          min={COUNTDOWN_MIN}
          max={COUNTDOWN_MAX}
          suffix="seconds"
          disabled={saving}
        />
      </section>

      <section className="space-y-3">
        <h2 className="text-[10px] font-semibold uppercase tracking-widest text-[var(--color-muted-foreground)]">
          Concurrency & rollback
        </h2>
        <NumberRow
          label="Max concurrent recoveries"
          description="Caps in-flight recovery pipelines on this project."
          value={policy.max_concurrent_recoveries}
          onChange={(v) => patch("max_concurrent_recoveries", v)}
          min={CONCURRENCY_MIN}
          max={CONCURRENCY_MAX}
          suffix="runs"
          disabled={saving}
        />
        <ToggleRow
          label="Rollback on SLO breach"
          description="Revert the deploy automatically if the post-merge SLO probe trips."
          checked={policy.rollback_on_slo_breach}
          onChange={(v) => patch("rollback_on_slo_breach", v)}
          disabled={saving}
        />
      </section>

      <section className="space-y-3">
        <h2 className="text-[10px] font-semibold uppercase tracking-widest text-[var(--color-muted-foreground)]">
          Approvals
        </h2>
        <ApproverChips
          ids={policy.approver_user_ids}
          onChange={(v) => patch("approver_user_ids", v)}
          disabled={saving}
        />
      </section>

      <section className="space-y-3">
        <h2 className="text-[10px] font-semibold uppercase tracking-widest text-[var(--color-muted-foreground)]">
          Kill switch
        </h2>
        <ToggleRow
          label="Engage kill switch"
          description="Pauses every auto-recovery on this project. Use during incidents where automation must stand down."
          checked={policy.kill_switch_enabled}
          onChange={(v) => patch("kill_switch_enabled", v)}
          disabled={saving}
          tone="danger"
        />
      </section>

      <div className="flex justify-end gap-2 pt-2">
        <Button asChild variant="ghost" type="button" disabled={saving}>
          <Link href={`/console/projects/${projectId}` as Route}>Cancel</Link>
        </Button>
        <Button type="submit" disabled={saving}>
          {saving ? (
            <Loader2 className="h-4 w-4 animate-spin" />
          ) : (
            <Save className="h-4 w-4" />
          )}
          Save policy
        </Button>
      </div>
    </form>
  );
}
