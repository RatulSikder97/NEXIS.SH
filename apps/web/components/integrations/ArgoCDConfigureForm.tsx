"use client";

// Phase 3 Stage 7 — ArgoCD Configure form.
//
// Two fields: server_url + token. The token is sent to the control-plane
// which encrypts it via KeyVault before persistence; the plaintext never
// leaves this form. On success we close the dialog and trigger a
// router.refresh() so the parent server component re-fetches the
// integrations list.

import * as React from "react";
import { Loader2 } from "lucide-react";
import { useRouter } from "next/navigation";

import { Button } from "@/components/ui/Button";
import { integrations, type Integration } from "@/lib/integrations";

export function ArgoCDConfigureForm({
  integration,
  onClose,
}: {
  integration?: Integration;
  onClose: () => void;
}) {
  const router = useRouter();
  const [serverUrl, setServerUrl] = React.useState("");
  const [token, setToken] = React.useState("");
  const [pending, setPending] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);

  const connected = integration?.status === "connected";

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setPending(true);
    setError(null);
    try {
      await integrations.connect("argocd", {
        server_url: serverUrl,
        token,
      });
      onClose();
      router.refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Connect failed");
    } finally {
      setPending(false);
    }
  }

  async function disconnect() {
    setPending(true);
    setError(null);
    try {
      await integrations.disconnect("argocd");
      onClose();
      router.refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Disconnect failed");
    } finally {
      setPending(false);
    }
  }

  if (connected) {
    return (
      <div className="space-y-4">
        <p className="text-sm text-[var(--color-muted-foreground)]">
          ArgoCD is connected. NEXIS can roll back deployments via this
          server. To rotate the token, disconnect and reconnect.
        </p>
        {error && (
          <p className="text-sm text-red-500" role="alert">
            {error}
          </p>
        )}
        <div className="flex justify-end gap-2 pt-2">
          <Button variant="ghost" type="button" onClick={onClose} disabled={pending}>
            Close
          </Button>
          <Button
            variant="outline"
            type="button"
            onClick={disconnect}
            disabled={pending}
          >
            {pending && <Loader2 className="h-4 w-4 animate-spin" />}
            Disconnect
          </Button>
        </div>
      </div>
    );
  }

  return (
    <form onSubmit={submit} className="space-y-4">
      <p className="text-sm text-[var(--color-muted-foreground)]">
        Connect your ArgoCD server so NEXIS can roll back deployments after a
        validated incident.
      </p>
      <div className="space-y-1">
        <label
          htmlFor="argo-server"
          className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]"
        >
          Server URL
        </label>
        <input
          id="argo-server"
          name="server_url"
          type="url"
          required
          value={serverUrl}
          onChange={(e) => setServerUrl(e.target.value)}
          placeholder="https://argocd.example.com"
          className="w-full rounded-md border border-[var(--color-border)] bg-[var(--color-background)] px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-ring)]"
        />
      </div>
      <div className="space-y-1">
        <label
          htmlFor="argo-token"
          className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]"
        >
          API Token
        </label>
        <input
          id="argo-token"
          name="token"
          type="password"
          required
          autoComplete="off"
          value={token}
          onChange={(e) => setToken(e.target.value)}
          placeholder="argocd.token.…"
          className="w-full rounded-md border border-[var(--color-border)] bg-[var(--color-background)] px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-ring)]"
        />
        <p className="text-[11px] text-[var(--color-muted-foreground)]">
          Stored encrypted (AES-GCM). NEXIS will never display this token again.
        </p>
      </div>
      {error && (
        <p className="text-sm text-red-500" role="alert">
          {error}
        </p>
      )}
      <div className="flex justify-end gap-2 pt-2">
        <Button variant="ghost" type="button" onClick={onClose} disabled={pending}>
          Cancel
        </Button>
        <Button type="submit" disabled={pending || !serverUrl || !token}>
          {pending && <Loader2 className="h-4 w-4 animate-spin" />}
          Connect
        </Button>
      </div>
    </form>
  );
}
