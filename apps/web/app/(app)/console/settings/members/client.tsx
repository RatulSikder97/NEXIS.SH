"use client";

// Phase 3 Stage 8 + Phase 9 — Members & Roles client.
//
// Renders the member table (real list when the Phase 9 endpoint is mounted,
// current-user fallback row otherwise), the intelligent role-recommendation
// banner (owner|admin: accept/dismiss with the analyser's plain-language
// rationale, plus an on-demand "Run analysis" trigger), and the
// pending-invites table. Owners see an "Invite member" button that opens a
// Radix Dialog with email + role inputs; admins can revoke pending invites
// but not issue new ones (mirrors the control-plane RBAC matrix).

import * as React from "react";
import * as Dialog from "@radix-ui/react-dialog";
import { Loader2, X } from "lucide-react";
import { useRouter } from "next/navigation";

import { Button } from "@/components/ui/Button";
import { invites, type PendingInvite } from "@/lib/invites";
import {
  roleRecommendations,
  type OrgMember,
  type RoleRecommendation,
} from "@/lib/roleRecommendations";
import type { MeResp } from "@/lib/auth";

function formatExpiry(iso: string): string {
  try {
    return new Date(iso).toLocaleString(undefined, {
      month: "short",
      day: "numeric",
      year: "numeric",
      hour: "2-digit",
      minute: "2-digit",
    });
  } catch {
    return iso;
  }
}

function formatLastLogin(iso?: string): string {
  if (!iso) return "Never logged in";
  try {
    return `Last login ${new Date(iso).toLocaleDateString(undefined, {
      month: "short",
      day: "numeric",
      year: "numeric",
    })}`;
  } catch {
    return iso;
  }
}

export function MembersClient({
  me,
  pending,
  members,
  recommendations,
  canIssue,
  canManage,
}: {
  me: MeResp;
  pending: PendingInvite[];
  members: OrgMember[] | null;
  recommendations: RoleRecommendation[] | null;
  canIssue: boolean;
  canManage: boolean;
}) {
  const router = useRouter();
  const [dialogOpen, setDialogOpen] = React.useState(false);
  const [email, setEmail] = React.useState("");
  const [role, setRole] = React.useState<"admin" | "member">("member");
  const [pendingSubmit, setPendingSubmit] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  const [lastIssued, setLastIssued] = React.useState<string | null>(null);
  const [revoking, setRevoking] = React.useState<string | null>(null);
  const [deciding, setDeciding] = React.useState<string | null>(null);
  const [analysing, setAnalysing] = React.useState(false);
  const [analysisNote, setAnalysisNote] = React.useState<string | null>(null);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setPendingSubmit(true);
    setError(null);
    try {
      const r = await invites.issue(me.org.id, email, role);
      setLastIssued(r.token_prefix);
      setDialogOpen(false);
      setEmail("");
      setRole("member");
      router.refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to invite");
    } finally {
      setPendingSubmit(false);
    }
  }

  async function revoke(tokenHash: string) {
    setRevoking(tokenHash);
    setError(null);
    try {
      await invites.revoke(me.org.id, tokenHash);
      router.refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to revoke");
    } finally {
      setRevoking(null);
    }
  }

  async function decide(recId: string, action: "accept" | "dismiss") {
    setDeciding(recId);
    setError(null);
    setAnalysisNote(null);
    try {
      await roleRecommendations.decide(me.org.id, recId, action);
      router.refresh();
    } catch (err) {
      setError(
        err instanceof Error
          ? err.message
          : `Failed to ${action} recommendation`,
      );
    } finally {
      setDeciding(null);
    }
  }

  async function runAnalysis() {
    setAnalysing(true);
    setError(null);
    setAnalysisNote(null);
    try {
      const r = await roleRecommendations.refresh(me.org.id);
      setAnalysisNote(
        r.created === 0
          ? "Analysis complete — no new recommendations."
          : `Analysis complete — ${r.created} new recommendation${r.created === 1 ? "" : "s"}.`,
      );
      router.refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to run analysis");
    } finally {
      setAnalysing(false);
    }
  }

  return (
    <div className="space-y-8">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold">Members &amp; Roles</h1>
          <p className="mt-1 text-sm text-[var(--color-muted-foreground)]">
            People with access to this organization.
          </p>
        </div>
        {canIssue && (
          <Button type="button" onClick={() => setDialogOpen(true)}>
            Invite member
          </Button>
        )}
      </div>

      {lastIssued && (
        <div className="rounded-md border border-emerald-500/30 bg-emerald-500/10 px-4 py-3 text-sm text-emerald-700 dark:text-emerald-300">
          Invite sent. Token prefix:{" "}
          <span className="font-mono">{lastIssued}…</span>
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

      {analysisNote && (
        <div className="rounded-md border border-emerald-500/30 bg-emerald-500/10 px-4 py-3 text-sm text-emerald-700 dark:text-emerald-300">
          {analysisNote}
        </div>
      )}

      {canManage && recommendations !== null && (
        <section className="space-y-3">
          <div className="flex items-center justify-between gap-4">
            <h2 className="text-sm font-medium uppercase tracking-widest text-[var(--color-muted-foreground)]">
              Role recommendations
            </h2>
            <Button
              size="sm"
              variant="outline"
              type="button"
              disabled={analysing}
              onClick={runAnalysis}
            >
              {analysing && <Loader2 className="h-4 w-4 animate-spin" />}
              Run analysis
            </Button>
          </div>
          <div className="overflow-hidden rounded-lg border border-[var(--color-border)] bg-[var(--color-card)]">
            {recommendations.length === 0 ? (
              <p className="px-4 py-6 text-center text-sm text-[var(--color-muted-foreground)]">
                No pending recommendations. The analyser reviews member activity
                and login patterns — run it to check for role adjustments.
              </p>
            ) : (
              <ul className="divide-y divide-[var(--color-border)]">
                {recommendations.map((rec) => (
                  <li
                    key={rec.id}
                    className="flex flex-col gap-3 px-4 py-4 sm:flex-row sm:items-start sm:justify-between"
                  >
                    <div className="min-w-0 space-y-1">
                      <p className="text-sm font-medium">
                        {rec.email}
                        <span className="ml-2 text-xs uppercase text-[var(--color-muted-foreground)]">
                          {rec.current_role} → {rec.recommended_role}
                        </span>
                      </p>
                      <p className="text-sm text-[var(--color-muted-foreground)]">
                        {rec.rationale}
                      </p>
                    </div>
                    <div className="flex shrink-0 gap-2">
                      <Button
                        size="sm"
                        type="button"
                        disabled={deciding === rec.id}
                        onClick={() => decide(rec.id, "accept")}
                      >
                        {deciding === rec.id && (
                          <Loader2 className="h-4 w-4 animate-spin" />
                        )}
                        Accept
                      </Button>
                      <Button
                        size="sm"
                        variant="outline"
                        type="button"
                        disabled={deciding === rec.id}
                        onClick={() => decide(rec.id, "dismiss")}
                      >
                        Dismiss
                      </Button>
                    </div>
                  </li>
                ))}
              </ul>
            )}
          </div>
        </section>
      )}

      <section className="space-y-3">
        <h2 className="text-sm font-medium uppercase tracking-widest text-[var(--color-muted-foreground)]">
          Members
        </h2>
        <div className="overflow-hidden rounded-lg border border-[var(--color-border)] bg-[var(--color-card)]">
          <div className="overflow-x-auto">
            <table className="w-full min-w-[480px] text-sm">
              <thead className="bg-[var(--color-muted)]/40 text-left text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
                <tr>
                  <th className="px-4 py-2 font-medium">Email</th>
                  <th className="px-4 py-2 font-medium">Role</th>
                  <th className="px-4 py-2 font-medium" aria-hidden></th>
                </tr>
              </thead>
              <tbody>
                {members !== null && members.length > 0 ? (
                  members.map((m) => (
                    <tr
                      key={m.user_id}
                      className="border-t border-[var(--color-border)]"
                    >
                      <td className="px-4 py-3">{m.email}</td>
                      <td className="px-4 py-3 uppercase text-[var(--color-muted-foreground)]">
                        {m.role}
                      </td>
                      <td className="px-4 py-3 text-right text-xs text-[var(--color-muted-foreground)]">
                        {m.user_id === me.user.id
                          ? "You"
                          : formatLastLogin(m.last_login_at)}
                      </td>
                    </tr>
                  ))
                ) : (
                  <tr className="border-t border-[var(--color-border)]">
                    <td className="px-4 py-3">{me.user.email}</td>
                    <td className="px-4 py-3 uppercase text-[var(--color-muted-foreground)]">
                      {me.role}
                    </td>
                    <td className="px-4 py-3 text-right text-xs text-[var(--color-muted-foreground)]">
                      You
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </div>
        {members === null && (
          <p className="text-[11px] text-[var(--color-muted-foreground)]">
            Showing only your own membership — the full member list requires
            owner or admin access.
          </p>
        )}
      </section>

      {canManage && (
        <section className="space-y-3">
          <h2 className="text-sm font-medium uppercase tracking-widest text-[var(--color-muted-foreground)]">
            Pending invites
          </h2>
          <div className="overflow-hidden rounded-lg border border-[var(--color-border)] bg-[var(--color-card)]">
            {pending.length === 0 ? (
              <p className="px-4 py-6 text-center text-sm text-[var(--color-muted-foreground)]">
                No pending invites.
              </p>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full min-w-[560px] text-sm">
                  <thead className="bg-[var(--color-muted)]/40 text-left text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
                    <tr>
                      <th className="px-4 py-2 font-medium">Email</th>
                      <th className="px-4 py-2 font-medium">Role</th>
                      <th className="px-4 py-2 font-medium">Expires</th>
                      <th className="px-4 py-2 font-medium" aria-hidden></th>
                    </tr>
                  </thead>
                  <tbody>
                    {pending.map((p) => (
                      <tr
                        key={p.token_hash}
                        className="border-t border-[var(--color-border)]"
                      >
                        <td className="px-4 py-3">{p.email}</td>
                        <td className="px-4 py-3 uppercase text-[var(--color-muted-foreground)]">
                          {p.role}
                        </td>
                        <td className="px-4 py-3 text-[var(--color-muted-foreground)]">
                          {formatExpiry(p.expires_at)}
                        </td>
                        <td className="px-4 py-3 text-right">
                          {canIssue && (
                            <Button
                              size="sm"
                              variant="outline"
                              type="button"
                              disabled={revoking === p.token_hash}
                              onClick={() => revoke(p.token_hash)}
                            >
                              {revoking === p.token_hash && (
                                <Loader2 className="h-4 w-4 animate-spin" />
                              )}
                              Revoke
                            </Button>
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        </section>
      )}

      <Dialog.Root open={dialogOpen} onOpenChange={setDialogOpen}>
        <Dialog.Portal>
          <Dialog.Overlay className="fixed inset-0 z-40 bg-black/40 backdrop-blur-sm" />
          <Dialog.Content className="fixed left-1/2 top-1/2 z-50 w-[min(480px,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-6 shadow-xl outline-none">
            <div className="flex items-start justify-between gap-4 pb-4">
              <div>
                <Dialog.Title className="text-lg font-semibold">
                  Invite member
                </Dialog.Title>
                <Dialog.Description className="mt-1 text-xs text-[var(--color-muted-foreground)]">
                  We&apos;ll email them a one-time link to set a password.
                </Dialog.Description>
              </div>
              <Dialog.Close asChild>
                <button
                  type="button"
                  aria-label="Close"
                  className="inline-flex h-8 w-8 items-center justify-center rounded-md text-[var(--color-muted-foreground)] hover:bg-[var(--color-muted)] hover:text-[var(--color-foreground)]"
                >
                  <X className="h-4 w-4" />
                </button>
              </Dialog.Close>
            </div>
            <form onSubmit={submit} className="space-y-4">
              <div className="space-y-1">
                <label
                  htmlFor="invite-email"
                  className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]"
                >
                  Email
                </label>
                <input
                  id="invite-email"
                  type="email"
                  required
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  className="w-full rounded-md border border-[var(--color-border)] bg-[var(--color-background)] px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-ring)]"
                />
              </div>
              <div className="space-y-1">
                <label
                  htmlFor="invite-role"
                  className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]"
                >
                  Role
                </label>
                <select
                  id="invite-role"
                  value={role}
                  onChange={(e) =>
                    setRole(e.target.value as "admin" | "member")
                  }
                  className="w-full rounded-md border border-[var(--color-border)] bg-[var(--color-background)] px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-ring)]"
                >
                  <option value="member">Member</option>
                  <option value="admin">Admin</option>
                </select>
              </div>
              {error && (
                <p className="text-sm text-red-500" role="alert">
                  {error}
                </p>
              )}
              <div className="flex justify-end gap-2 pt-2">
                <Button
                  variant="ghost"
                  type="button"
                  onClick={() => setDialogOpen(false)}
                  disabled={pendingSubmit}
                >
                  Cancel
                </Button>
                <Button type="submit" disabled={pendingSubmit || !email}>
                  {pendingSubmit && (
                    <Loader2 className="h-4 w-4 animate-spin" />
                  )}
                  Send invite
                </Button>
              </div>
            </form>
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>
    </div>
  );
}
