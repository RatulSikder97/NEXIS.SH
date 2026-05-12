"use client";

import { Suspense, useState } from "react";
import type { Route } from "next";
import { useRouter, useSearchParams } from "next/navigation";

import { Button } from "@/components/ui/Button";
import { auth, AuthError } from "@/lib/auth";

export default function SignInPage() {
  // `useSearchParams()` opts the page out of static prerendering, so Next 16
  // requires it to live inside a Suspense boundary. The wrapper renders the
  // static chrome; the inner form reads ?next= once the client takes over.
  return (
    <Suspense fallback={<SignInChrome />}>
      <SignInForm />
    </Suspense>
  );
}

function SignInChrome() {
  return (
    <>
      <h1 className="text-2xl font-semibold mb-2 text-[var(--color-foreground)]">
        Sign in to NEXIS
      </h1>
      <p className="text-sm text-[var(--color-muted-foreground)] mb-6">
        Welcome back. Use your email and password to continue.
      </p>
    </>
  );
}

function SignInForm() {
  const router = useRouter();
  const searchParams = useSearchParams();
  // The ?next= param is user-controlled and arbitrary; cast through Route
  // because typedRoutes can't prove a runtime string is one of the known
  // routes. We restrict to same-origin paths starting with "/" (and reject
  // protocol-relative "//evil.example") to avoid open-redirects.
  const rawNext = searchParams.get("next") ?? "/dashboard";
  const next = (rawNext.startsWith("/") && !rawNext.startsWith("//")
    ? rawNext
    : "/dashboard") as Route;

  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [mfaCode, setMfaCode] = useState("");
  const [mfaRequired, setMfaRequired] = useState(false);
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setErr("");
    setBusy(true);
    try {
      await auth.login({
        email,
        password,
        ...(mfaRequired && mfaCode ? { mfa_code: mfaCode } : {}),
      });
      router.push(next);
    } catch (e) {
      if (e instanceof AuthError) {
        // The control-plane returns 400 with an "mfa required" style message
        // when MFA is enrolled but no code was supplied. Surface the second
        // field and let the user retry.
        if (e.status === 400 && e.message.toLowerCase().includes("mfa")) {
          setMfaRequired(true);
          setErr("");
        } else {
          setErr(e.message);
        }
      } else {
        setErr("Something went wrong.");
      }
    } finally {
      setBusy(false);
    }
  }

  return (
    <>
      <SignInChrome />
      <form onSubmit={onSubmit} className="space-y-4">
        <Field
          label="Email"
          id="email"
          type="email"
          required
          value={email}
          onChange={setEmail}
        />
        <Field
          label="Password"
          id="password"
          type="password"
          required
          value={password}
          onChange={setPassword}
        />
        {mfaRequired && (
          <Field
            label="MFA code"
            id="mfa_code"
            inputMode="numeric"
            pattern="[0-9]{6}"
            required
            value={mfaCode}
            onChange={setMfaCode}
          />
        )}
        {err && (
          <p
            className="text-sm text-[var(--color-destructive)]"
            role="alert"
          >
            {err}
          </p>
        )}
        <Button type="submit" disabled={busy} className="w-full">
          {busy ? "Signing in…" : "Sign in"}
        </Button>
      </form>
      <p className="mt-4 text-sm text-[var(--color-muted-foreground)] text-center">
        Don&apos;t have an account?{" "}
        <a
          href="/sign-up"
          className="text-[var(--color-primary)] hover:underline"
        >
          Sign up
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
  inputMode,
  pattern,
  ...rest
}: {
  label: string;
  id: string;
  type?: string;
  value: string;
  onChange: (v: string) => void;
  required?: boolean;
  minLength?: number;
  inputMode?: "numeric" | "text";
  pattern?: string;
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
        inputMode={inputMode}
        pattern={pattern}
        onChange={(e) => onChange(e.target.value)}
        className="w-full rounded-md border border-[var(--color-border)] bg-[var(--color-background)] px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-[var(--color-ring)]"
        {...rest}
      />
    </div>
  );
}
