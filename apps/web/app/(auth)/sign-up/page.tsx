"use client";

import { Suspense, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { CheckCircle2 } from "lucide-react";

import { Button } from "@/components/ui/Button";
import { auth, AuthError } from "@/lib/auth";

// Phase 7 — Cloud cutover. When NEXT_PUBLIC_AUTH_PROVIDER=workos we hand the
// signup flow off to the WorkOS hosted UI (AuthKit); JIT provisioning on the
// backend mints the org + entitlements row. Otherwise the Phase 2 password
// form is preserved.
//
// Phase 8 — Public signup gated by an invite code. Both the WorkOS and
// password paths read `?invite=<code>` from the URL and thread it through
// to the backend so the control-plane can atomically consume it. We don't
// gate the rendering on the code's presence — that's a server-side
// concern handled in proxy.ts / the backend agent's work.
const AUTH_PROVIDER = process.env.NEXT_PUBLIC_AUTH_PROVIDER ?? "local";
const AUTH_PROVIDER_URL = process.env.NEXT_PUBLIC_AUTH_PROVIDER_URL ?? "";

function generateCsrfState(): string {
  if (typeof window === "undefined") return "";
  const bytes = new Uint8Array(16);
  window.crypto.getRandomValues(bytes);
  return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
}

function setOAuthStateCookie(state: string): void {
  const maxAge = 60 * 5;
  const secure = typeof window !== "undefined" && window.location.protocol === "https:";
  document.cookie = [
    `nexis_oauth_state=${state}`,
    "Path=/",
    `Max-Age=${maxAge}`,
    "SameSite=Lax",
    ...(secure ? ["Secure"] : []),
  ].join("; ");
}

export default function SignUpPage() {
  // useSearchParams() inside the child components requires a Suspense
  // boundary at the page level so Next 16 can statically prerender the
  // shell and stream the query-dependent banner on the client. The
  // fallback is the same form chrome with the banner slot empty.
  return (
    <Suspense fallback={<SignUpShell />}>
      {AUTH_PROVIDER === "workos" ? <WorkOSSignUp /> : <PasswordSignUp />}
    </Suspense>
  );
}

function SignUpShell() {
  return (
    <>
      <h1 className="text-2xl font-semibold mb-2 text-[var(--color-foreground)]">
        Create your NEXIS account
      </h1>
      <p className="text-sm text-[var(--color-muted-foreground)] mb-6">
        Loading…
      </p>
    </>
  );
}

function InviteBanner({ invite }: { invite: string }) {
  if (!invite) return null;
  return (
    <div
      role="status"
      className="mb-6 flex items-center gap-2 rounded-md border border-emerald-500/30 bg-emerald-500/10 px-3 py-2 text-sm text-emerald-700 dark:text-emerald-300"
    >
      <CheckCircle2 className="h-4 w-4 shrink-0" />
      <span>
        Invite code{" "}
        <span className="font-mono">{invite.slice(0, 6)}…</span> applied
      </span>
    </div>
  );
}

function WorkOSSignUp() {
  const searchParams = useSearchParams();
  const invite = searchParams?.get("invite") ?? "";
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");

  function onContinue() {
    if (!AUTH_PROVIDER_URL) {
      setErr("WorkOS provider URL is not configured.");
      return;
    }
    setBusy(true);
    try {
      const state = generateCsrfState();
      setOAuthStateCookie(state);
      const origin = window.location.origin;
      // Phase 8 — round-trip the invite through WorkOS by appending it to
      // the redirect_uri. The control-plane callback handler reads
      // ?invite=<code> off the redirect to consume it in the same
      // transaction as user provisioning.
      const callbackUrl = new URL(`${origin}/v1/auth/workos/callback`);
      if (invite) callbackUrl.searchParams.set("invite", invite);
      const redirectUri = callbackUrl.toString();
      const url = new URL(`${AUTH_PROVIDER_URL.replace(/\/+$/, "")}/sign-in`);
      url.searchParams.set("redirect_uri", redirectUri);
      url.searchParams.set("state", state);
      url.searchParams.set("intent", "sign-up");
      window.location.assign(url.toString());
    } catch (e) {
      setBusy(false);
      setErr(e instanceof Error ? e.message : "Failed to start sign-up.");
    }
  }

  return (
    <>
      <h1 className="text-2xl font-semibold mb-2 text-[var(--color-foreground)]">
        Create your NEXIS account
      </h1>
      <p className="text-sm text-[var(--color-muted-foreground)] mb-6">
        Continue with NEXIS&apos;s hosted onboarding. We&apos;ll provision your
        org automatically.
      </p>
      <InviteBanner invite={invite} />
      {err && (
        <p
          className="text-sm text-[var(--color-destructive)] mb-4"
          role="alert"
        >
          {err}
        </p>
      )}
      <Button
        type="button"
        disabled={busy}
        onClick={onContinue}
        className="w-full"
      >
        {busy ? "Redirecting…" : "Continue with NEXIS"}
      </Button>
      <p className="mt-4 text-sm text-[var(--color-muted-foreground)] text-center">
        Already have an account?{" "}
        <a
          href="/sign-in"
          className="text-[var(--color-primary)] hover:underline"
        >
          Sign in
        </a>
      </p>
    </>
  );
}

function PasswordSignUp() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const invite = searchParams?.get("invite") ?? "";
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [orgName, setOrgName] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setErr("");
    setBusy(true);
    try {
      if (invite.length > 0) {
        // Phase 8 — invite-code path. POST to /v1/auth/signup with the
        // code on the query string so the backend can consume it
        // atomically under `SELECT ... FOR UPDATE`.
        const API =
          process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";
        const r = await fetch(
          `${API}/v1/auth/signup?invite=${encodeURIComponent(invite)}`,
          {
            method: "POST",
            credentials: "include",
            headers: { "content-type": "application/json" },
            body: JSON.stringify({
              email,
              password,
              org_name: orgName,
            }),
          },
        );
        if (!r.ok) {
          const body = (await r.json().catch(() => ({}))) as {
            error?: unknown;
          };
          const msg =
            body && typeof body === "object" && "error" in body
              ? String(body.error)
              : r.statusText;
          throw new AuthError(msg, r.status);
        }
      } else {
        await auth.signup({ email, password, org_name: orgName });
      }
      router.push("/dashboard");
    } catch (e) {
      if (e instanceof AuthError) setErr(e.message);
      else setErr("Something went wrong.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <>
      <h1 className="text-2xl font-semibold mb-2 text-[var(--color-foreground)]">
        Create your NEXIS account
      </h1>
      <p className="text-sm text-[var(--color-muted-foreground)] mb-6">
        Start with a personal org. Invite teammates later.
      </p>
      <InviteBanner invite={invite} />
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
          minLength={8}
          value={password}
          onChange={setPassword}
        />
        <Field
          label="Org name"
          id="org_name"
          required
          value={orgName}
          onChange={setOrgName}
        />
        {err && (
          <p
            className="text-sm text-[var(--color-destructive)]"
            role="alert"
          >
            {err}
          </p>
        )}
        <Button type="submit" disabled={busy} className="w-full">
          {busy ? "Creating…" : "Sign up"}
        </Button>
      </form>
      <p className="mt-4 text-sm text-[var(--color-muted-foreground)] text-center">
        Already have an account?{" "}
        <a
          href="/sign-in"
          className="text-[var(--color-primary)] hover:underline"
        >
          Sign in
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
