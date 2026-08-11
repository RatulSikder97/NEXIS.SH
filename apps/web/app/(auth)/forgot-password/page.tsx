"use client";

// Phase 9 — forgot-password entry point. Collects an email and POSTs to
// /v1/auth/password-reset/request. The endpoint always answers 202 for a
// well-formed email (unknown addresses included), so on success we show the
// same "check your inbox" state regardless — no account-existence oracle.

import { useState } from "react";
import { MailCheck } from "lucide-react";

import { Button } from "@/components/ui/Button";
import { auth, AuthError } from "@/lib/auth";

export default function ForgotPasswordPage() {
  const [email, setEmail] = useState("");
  const [sent, setSent] = useState(false);
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setErr("");
    setBusy(true);
    try {
      await auth.requestPasswordReset(email);
      setSent(true);
    } catch (e) {
      if (e instanceof AuthError) setErr(e.message);
      else setErr("Something went wrong.");
    } finally {
      setBusy(false);
    }
  }

  if (sent) {
    return (
      <>
        <h1 className="text-2xl font-semibold mb-2 text-[var(--color-foreground)]">
          Check your inbox
        </h1>
        <div
          role="status"
          className="mb-6 flex items-start gap-2 rounded-md border border-emerald-500/30 bg-emerald-500/10 px-3 py-2 text-sm text-emerald-700 dark:text-emerald-300"
        >
          <MailCheck className="mt-0.5 h-4 w-4 shrink-0" />
          <span>
            If an account exists for{" "}
            <span className="font-medium">{email}</span>, we&apos;ve sent a
            password-reset link. It expires in 30 minutes.
          </span>
        </div>
        <p className="text-sm text-[var(--color-muted-foreground)] text-center">
          Remembered it after all?{" "}
          <a
            href="/sign-in"
            className="text-[var(--color-primary)] hover:underline"
          >
            Back to sign in
          </a>
        </p>
      </>
    );
  }

  return (
    <>
      <h1 className="text-2xl font-semibold mb-2 text-[var(--color-foreground)]">
        Reset your password
      </h1>
      <p className="text-sm text-[var(--color-muted-foreground)] mb-6">
        Enter the email you signed up with and we&apos;ll send you a reset link.
      </p>
      <form onSubmit={onSubmit} className="space-y-4">
        <div>
          <label
            htmlFor="email"
            className="block text-sm font-medium mb-1 text-[var(--color-foreground)]"
          >
            Email
          </label>
          <input
            id="email"
            type="email"
            required
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            className="w-full rounded-md border border-[var(--color-border)] bg-[var(--color-background)] px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-[var(--color-ring)]"
          />
        </div>
        {err && (
          <p className="text-sm text-[var(--color-destructive)]" role="alert">
            {err}
          </p>
        )}
        <Button type="submit" disabled={busy} className="w-full">
          {busy ? "Sending…" : "Send reset link"}
        </Button>
      </form>
      <p className="mt-4 text-sm text-[var(--color-muted-foreground)] text-center">
        Remembered it?{" "}
        <a
          href="/sign-in"
          className="text-[var(--color-primary)] hover:underline"
        >
          Back to sign in
        </a>
      </p>
    </>
  );
}
