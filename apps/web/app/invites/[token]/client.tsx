"use client";

// Phase 3 Stage 10 — Invite claim flow.
//
// Three states:
//   * loading — initial fetch of /v1/invites/{token}.
//   * landed — show org name + role + inviter; collect a password.
//   * error — token invalid/expired/already-claimed.
//
// On successful claim, the control-plane sets the nexis_session cookie and
// returns 200. We then router.push("/console") to land the user on their
// new home. We use a plain anchor on success because typedRoutes is happy
// with the static "/console" route.

import * as React from "react";
import { Loader2 } from "lucide-react";
import { useRouter } from "next/navigation";

import { Button } from "@/components/ui/Button";
import { invites, type InviteInfo } from "@/lib/invites";

export function InviteClaimClient({ token }: { token: string }) {
  const router = useRouter();
  const [info, setInfo] = React.useState<InviteInfo | null>(null);
  const [loading, setLoading] = React.useState(true);
  const [loadError, setLoadError] = React.useState<string | null>(null);

  const [password, setPassword] = React.useState("");
  const [confirm, setConfirm] = React.useState("");
  const [submitting, setSubmitting] = React.useState(false);
  const [submitError, setSubmitError] = React.useState<string | null>(null);

  React.useEffect(() => {
    let cancelled = false;
    (async () => {
      setLoading(true);
      setLoadError(null);
      try {
        const r = await invites.get(token);
        if (!cancelled) setInfo(r);
      } catch (err) {
        if (!cancelled) {
          setLoadError(err instanceof Error ? err.message : "Invalid invite");
        }
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [token]);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setSubmitError(null);
    if (password.length < 8) {
      setSubmitError("Password must be at least 8 characters.");
      return;
    }
    if (password !== confirm) {
      setSubmitError("Passwords do not match.");
      return;
    }
    setSubmitting(true);
    try {
      await invites.claim(token, password);
      router.push("/console");
    } catch (err) {
      setSubmitError(err instanceof Error ? err.message : "Claim failed");
      setSubmitting(false);
    }
  }

  return (
    <main className="min-h-screen flex items-center justify-center bg-[var(--color-muted)] px-4 py-12">
      <div className="w-full max-w-md">
        <div className="rounded-xl border border-[var(--color-border)] bg-[var(--color-card)] p-8 shadow-sm">
          <h1 className="mb-2 text-2xl font-semibold text-[var(--color-foreground)]">
            Accept your invite
          </h1>

          {loading && (
            <p className="mt-4 inline-flex items-center gap-2 text-sm text-[var(--color-muted-foreground)]">
              <Loader2 className="h-4 w-4 animate-spin" /> Loading invite…
            </p>
          )}

          {loadError && (
            <div className="mt-4 space-y-3">
              <div
                role="alert"
                className="rounded-md border border-red-500/30 bg-red-500/10 px-4 py-3 text-sm text-red-700 dark:text-red-300"
              >
                {loadError}
              </div>
              <p className="text-sm text-[var(--color-muted-foreground)]">
                The invite may have expired or already been claimed. Ask the
                person who invited you to resend it.
              </p>
            </div>
          )}

          {info && !loadError && (
            <>
              <p className="mb-6 text-sm text-[var(--color-muted-foreground)]">
                You&apos;ve been invited to join{" "}
                <span className="font-medium text-[var(--color-foreground)]">
                  {info.org.name}
                </span>{" "}
                as <span className="font-medium uppercase">{info.role}</span>{" "}
                by <span className="font-medium">{info.inviter_email}</span>.
              </p>
              <form onSubmit={submit} className="space-y-4">
                <div className="space-y-1">
                  <label
                    htmlFor="password"
                    className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]"
                  >
                    Password
                  </label>
                  <input
                    id="password"
                    type="password"
                    required
                    autoComplete="new-password"
                    minLength={8}
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    className="w-full rounded-md border border-[var(--color-border)] bg-[var(--color-background)] px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-ring)]"
                  />
                </div>
                <div className="space-y-1">
                  <label
                    htmlFor="confirm"
                    className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]"
                  >
                    Confirm password
                  </label>
                  <input
                    id="confirm"
                    type="password"
                    required
                    autoComplete="new-password"
                    minLength={8}
                    value={confirm}
                    onChange={(e) => setConfirm(e.target.value)}
                    className="w-full rounded-md border border-[var(--color-border)] bg-[var(--color-background)] px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-ring)]"
                  />
                </div>
                {submitError && (
                  <p className="text-sm text-red-500" role="alert">
                    {submitError}
                  </p>
                )}
                <Button
                  type="submit"
                  className="w-full"
                  disabled={submitting || !password || !confirm}
                >
                  {submitting && <Loader2 className="h-4 w-4 animate-spin" />}
                  Accept &amp; continue
                </Button>
              </form>
            </>
          )}
        </div>
        <p className="mt-4 text-center text-xs text-[var(--color-muted-foreground)]">
          Already have an account?{" "}
          <a href="/sign-in" className="font-medium underline">
            Sign in
          </a>
        </p>
      </div>
    </main>
  );
}
