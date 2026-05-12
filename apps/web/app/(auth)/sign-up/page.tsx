"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";

import { Button } from "@/components/ui/Button";
import { auth, AuthError } from "@/lib/auth";

export default function SignUpPage() {
  const router = useRouter();
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
      await auth.signup({ email, password, org_name: orgName });
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
