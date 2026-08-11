"use client";

// Phase 9 — Sessions client.
//
// Renders the caller's active sessions (devices) with a Revoke button per
// row. The current session's row shows a badge instead of the button —
// revoking yourself is just a logout the hard way, and the sign-out control
// already exists in the topbar. Revocation DELETEs /v1/me/sessions/{id} and
// refreshes the server-fetched list.

import * as React from "react";
import { Loader2, MonitorSmartphone } from "lucide-react";
import { useRouter } from "next/navigation";

import { Button } from "@/components/ui/Button";
import { EmptyState } from "@/components/empty-state/EmptyState";
import { auth, type SessionInfo } from "@/lib/auth";

function formatDate(iso: string): string {
  if (!iso) return "—";
  try {
    return new Date(iso).toLocaleString(undefined, {
      month: "short",
      day: "numeric",
      year: "numeric",
      hour: "numeric",
      minute: "2-digit",
    });
  } catch {
    return iso;
  }
}

// describeAgent compresses a raw User-Agent string into something scannable.
// Best-effort — unknown agents fall back to the raw (truncated) string.
function describeAgent(ua: string): string {
  if (!ua) return "Unknown device";
  if (/edg\//i.test(ua)) return "Edge";
  if (/firefox\//i.test(ua)) return "Firefox";
  if (/chrome\//i.test(ua)) return "Chrome";
  if (/safari\//i.test(ua) && /version\//i.test(ua)) return "Safari";
  if (/curl\//i.test(ua)) return "curl";
  return ua.length > 48 ? `${ua.slice(0, 48)}…` : ua;
}

export function SessionsClient({ initial }: { initial: SessionInfo[] }) {
  const router = useRouter();
  const [error, setError] = React.useState<string | null>(null);
  const [revoking, setRevoking] = React.useState<string | null>(null);

  async function revoke(id: string) {
    setRevoking(id);
    setError(null);
    try {
      await auth.revokeSession(id);
      router.refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to revoke session");
    } finally {
      setRevoking(null);
    }
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold">Sessions</h1>
        <p className="mt-1 text-sm text-[var(--color-muted-foreground)]">
          Devices currently signed in to your account. Revoke anything you
          don&apos;t recognise — that device is signed out immediately.
        </p>
      </div>

      {error && (
        <div
          role="alert"
          className="rounded-md border border-red-500/30 bg-red-500/10 px-4 py-3 text-sm text-red-700 dark:text-red-300"
        >
          {error}
        </div>
      )}

      {initial.length === 0 ? (
        <EmptyState
          icon={MonitorSmartphone}
          title="No active sessions"
          description="Sessions appear here when you sign in from a browser or device."
        />
      ) : (
        <div className="overflow-hidden rounded-lg border border-[var(--color-border)] bg-[var(--color-card)]">
          <div className="overflow-x-auto">
            <table className="w-full min-w-[720px] text-sm">
              <thead className="bg-[var(--color-muted)]/40 text-left text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
                <tr>
                  <th className="px-4 py-2 font-medium">Device</th>
                  <th className="px-4 py-2 font-medium">IP</th>
                  <th className="px-4 py-2 font-medium">Signed in</th>
                  <th className="px-4 py-2 font-medium">Last active</th>
                  <th className="px-4 py-2 font-medium" aria-hidden></th>
                </tr>
              </thead>
              <tbody>
                {initial.map((s) => (
                  <tr
                    key={s.id}
                    className="border-t border-[var(--color-border)]"
                  >
                    <td className="px-4 py-3">
                      <span title={s.user_agent || undefined}>
                        {describeAgent(s.user_agent)}
                      </span>
                      {s.is_current && (
                        <span className="ml-2 rounded-full border border-emerald-500/30 bg-emerald-500/10 px-2 py-0.5 text-[11px] text-emerald-700 dark:text-emerald-300">
                          This device
                        </span>
                      )}
                    </td>
                    <td className="px-4 py-3 font-mono text-xs text-[var(--color-muted-foreground)]">
                      {s.ip || "—"}
                    </td>
                    <td className="px-4 py-3 text-[var(--color-muted-foreground)]">
                      {formatDate(s.created_at)}
                    </td>
                    <td className="px-4 py-3 text-[var(--color-muted-foreground)]">
                      {s.last_seen_at ? formatDate(s.last_seen_at) : "—"}
                    </td>
                    <td className="px-4 py-3 text-right">
                      {!s.is_current && (
                        <Button
                          size="sm"
                          variant="outline"
                          type="button"
                          disabled={revoking === s.id}
                          onClick={() => revoke(s.id)}
                        >
                          {revoking === s.id && (
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
        </div>
      )}
    </div>
  );
}
