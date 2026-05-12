"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";

import { Button } from "@/components/ui/Button";
import { auth, AuthError } from "@/lib/auth";

export default function MFAPage() {
  const router = useRouter();
  const [qr, setQR] = useState<string | null>(null);
  const [code, setCode] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    auth
      .mfaEnroll()
      .then((res) => setQR(res.qr_data_url))
      .catch((e) =>
        setErr(e instanceof AuthError ? e.message : "enroll failed"),
      );
  }, []);

  async function verify(e: React.FormEvent) {
    e.preventDefault();
    setErr("");
    setBusy(true);
    try {
      await auth.mfaVerify(code);
      router.push("/dashboard");
    } catch (e) {
      setErr(e instanceof AuthError ? e.message : "verify failed");
    } finally {
      setBusy(false);
    }
  }

  return (
    <>
      <h1 className="text-2xl font-semibold mb-2 text-[var(--color-foreground)]">
        Enable two-factor
      </h1>
      <p className="text-sm text-[var(--color-muted-foreground)] mb-6">
        Scan the QR with your authenticator app, then enter the 6-digit code.
      </p>
      {qr && (
        // Next/Image rejects data: URLs without configuring a loader; the QR
        // is a small inline image so a plain <img> is fine here.
        // eslint-disable-next-line @next/next/no-img-element
        <img
          src={qr}
          alt="QR code"
          className="mx-auto mb-6"
          data-testid="mfa-qr"
        />
      )}
      <form onSubmit={verify} className="space-y-4">
        <div>
          <label
            htmlFor="code"
            className="block text-sm font-medium mb-1 text-[var(--color-foreground)]"
          >
            MFA code
          </label>
          <input
            id="code"
            inputMode="numeric"
            pattern="[0-9]{6}"
            value={code}
            onChange={(e) => setCode(e.target.value)}
            className="w-full rounded-md border border-[var(--color-border)] bg-[var(--color-background)] px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-[var(--color-ring)]"
          />
        </div>
        {err && (
          <p
            className="text-sm text-[var(--color-destructive)]"
            role="alert"
          >
            {err}
          </p>
        )}
        <Button
          type="submit"
          disabled={busy || code.length !== 6}
          className="w-full"
        >
          Verify
        </Button>
      </form>
    </>
  );
}
