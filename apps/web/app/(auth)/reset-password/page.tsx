"use client";

// Phase 9 — reset-password landing page. The emailed link points here with
// ?token=<plaintext>. We collect a new password (with confirmation), POST to
// /v1/auth/password-reset/confirm, and bounce to /sign-in?reset=success —
// the sign-in page renders the success banner. The backend revokes every
// session on success, so there is no cookie to carry forward.

import { Suspense, useState } from "react";
import type { Route } from "next";
import { useRouter, useSearchParams } from "next/navigation";

import { Button } from "@/components/ui/Button";
import { auth, AuthError } from "@/lib/auth";

export default function ResetPasswordPage() {
  // useSearchParams() opts the page out of static prerendering, so Next 16
  // requires it inside a Suspense boundary — same pattern as sign-in.
  return (
    <Suspense fallback={<ResetChrome />}>
      <ResetPasswordForm />
    </Suspense>
  );
}

function ResetChrome() {
  return (
    <>
      <h1 className="text-2xl font-semibold mb-2 text-[var(--color-foreground)]">
        Choose a new password
      </h1>
      <p className="text-sm text-[var(--color-muted-foreground)] mb-6">
        Almost there — pick a new password for your account.
      </p>
    </>
  );
}

function ResetPasswordForm() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const token = searchParams.get("token") ?? "";

  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setErr("");
    if (password !== confirm) {
      setErr("Passwords do not match.");
      return;
    }
    setBusy(true);
    try {
      await auth.confirmPasswordReset(token, password);
      router.push("/sign-in?reset=success" as Route);
    } catch (e) {
      if (e instanceof AuthError) setErr(e.message);
      else setErr("Something went wrong.");
    } finally {
      setBusy(false);
    }
  }

  if (!token) {
    return (
      <>
        <ResetChrome />
        <p className="text-sm text-[var(--color-destructive)]" role="alert">
          This reset link is missing its token. Request a new one from the
          forgot-password page.
        </p>
        <p className="mt-4 text-sm text-[var(--color-muted-foreground)] text-center">
          <a
            href="/forgot-password"
            className="text-[var(--color-primary)] hover:underline"
          >
            Request a new link
          </a>
        </p>
      </>
    );
  }

  return (
    <>
      <ResetChrome />
      <form onSubmit={onSubmit} className="space-y-4">
        <Field
          label="New password"
          id="new_password"
          type="password"
          required
          minLength={8}
          value={password}
          onChange={setPassword}
        />
        <Field
          label="Confirm new password"
          id="confirm_password"
          type="password"
          required
          minLength={8}
          value={confirm}
          onChange={setConfirm}
        />
        {err && (
          <p className="text-sm text-[var(--color-destructive)]" role="alert">
            {err}
          </p>
        )}
        <Button type="submit" disabled={busy} className="w-full">
          {busy ? "Updating…" : "Update password"}
        </Button>
      </form>
      <p className="mt-4 text-sm text-[var(--color-muted-foreground)] text-center">
        Link expired?{" "}
        <a
          href="/forgot-password"
          className="text-[var(--color-primary)] hover:underline"
        >
          Request a new one
        </a>
      </p>
    </>
  );
}

function Field({
  label,
  id,
  type = "text",
  value,
  onChange,
  ...rest
}: {
  label: string;
  id: string;
  type?: string;
  value: string;
  onChange: (v: string) => void;
  required?: boolean;
  minLength?: number;
}) {
  return (
    <div>
      <label
        htmlFor={id}
        className="block text-sm font-medium mb-1 text-[var(--color-foreground)]"
      >
        {label}
      </label>
      <input
        id={id}
        type={type}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="w-full rounded-md border border-[var(--color-border)] bg-[var(--color-background)] px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-[var(--color-ring)]"
        {...rest}
      />
    </div>
  );
}
