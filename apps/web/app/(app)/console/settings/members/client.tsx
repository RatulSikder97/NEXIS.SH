"use client";

// Phase 3 Stage 8 — Members & Roles client.
//
// Renders the current-user row (the only "member" surfaced by the Phase 3
// API — a real list endpoint lands in Phase 4) and the pending-invites
// table. Owners see an "Invite member" button that opens a Radix Dialog
// with email + role inputs; admins can revoke pending invites but not
// issue new ones (mirrors the control-plane RBAC matrix).

import * as React from "react";
import * as Dialog from "@radix-ui/react-dialog";
import { Loader2, X } from "lucide-react";
import { useRouter } from "next/navigation";

import { Button } from "@/components/ui/Button";
import { invites, type PendingInvite } from "@/lib/invites";
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

export function MembersClient({
  me,
  pending,
  canIssue,
  canManage,
}: {
  me: MeResp;
  pending: PendingInvite[];
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
          Invite sent. Token prefix: <span className="font-mono">{lastIssued}…</span>
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

      <section className="space-y-3">
        <h2 className="text-sm font-medium uppercase tracking-widest text-[var(--color-muted-foreground)]">
          Members
        </h2>
        <div className="overflow-hidden rounded-lg border border-[var(--color-border)] bg-[var(--color-card)]">
          <table className="w-full text-sm">
            <thead className="bg-[var(--color-muted)]/40 text-left text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
              <tr>
                <th className="px-4 py-2 font-medium">Email</th>
                <th className="px-4 py-2 font-medium">Role</th>
                <th className="px-4 py-2 font-medium" aria-hidden></th>
              </tr>
            </thead>
            <tbody>
              <tr className="border-t border-[var(--color-border)]">
                <td className="px-4 py-3">{me.user.email}</td>
                <td className="px-4 py-3 uppercase text-[var(--color-muted-foreground)]">
                  {me.role}
                </td>
                <td className="px-4 py-3 text-right text-xs text-[var(--color-muted-foreground)]">
                  You
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <p className="text-[11px] text-[var(--color-muted-foreground)]">
          Full member list lands in Phase 4. Until then, invite acceptance is
          how members join.
        </p>
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
              <table className="w-full text-sm">
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
                  onChange={(e) => setRole(e.target.value as "admin" | "member")}
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
                  {pendingSubmit && <Loader2 className="h-4 w-4 animate-spin" />}
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
