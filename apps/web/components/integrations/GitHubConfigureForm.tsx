"use client";

// Phase 3 Stage 7 — GitHub Configure form.
//
// Sits inside a Radix Dialog. Two states:
//
//   * Disconnected → renders an "Install GitHub App (mock)" button that
//     bounces the browser to the control-plane's dev-only
//     /v1/integrations/github/mock_install endpoint, which 302s back to
//     /console/integrations?installed=github with the integration row
//     persisted.
//   * Connected → shows the last 6 chars of installation_id + a Disconnect
//     button. Disconnect calls DELETE and then triggers a router.refresh()
//     in the parent.
//
// The parent is responsible for closing the dialog and refreshing the
// server component after a successful disconnect.

import * as React from "react";
import { Loader2 } from "lucide-react";
import { useRouter } from "next/navigation";

import { Button } from "@/components/ui/Button";
import { integrations, type Integration } from "@/lib/integrations";

export function GitHubConfigureForm({
  integration,
  onClose,
}: {
  integration?: Integration;
  onClose: () => void;
}) {
  const router = useRouter();
  const [pending, setPending] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);

  const connected = integration?.status === "connected";
  // Last 6 chars of the installation id make a compact, recognisable
  // fingerprint without leaking the full value into the DOM/screenshots.
  const installSuffix = integration?.installation_id
    ? integration.installation_id.slice(-6)
    : null;

  async function disconnect() {
    setPending(true);
    setError(null);
    try {
      await integrations.disconnect("github");
      onClose();
      router.refresh();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Disconnect failed");
    } finally {
      setPending(false);
    }
  }

  function install() {
    integrations.mockInstallGithub();
  }

  return (
    <div className="space-y-4">
      <p className="text-sm text-[var(--color-muted-foreground)]">
        Connect your GitHub organization so NEXIS can open pull requests and
        read repository metadata when proposing fixes.
      </p>

      {connected ? (
        <div className="space-y-3">
          <div className="rounded-md border border-[var(--color-border)] bg-[var(--color-muted)]/30 p-3">
            <p className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
              Installation
            </p>
            <p className="mt-1 font-mono text-sm">…{installSuffix}</p>
          </div>
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
      ) : (
        <div className="space-y-3">
          <p className="text-xs text-[var(--color-muted-foreground)]">
            Dev environments use a mock install that bypasses the GitHub App
            consent screen. Production will redirect to GitHub for real
            installation.
          </p>
          {error && (
            <p className="text-sm text-red-500" role="alert">
              {error}
            </p>
          )}
          <div className="flex justify-end gap-2 pt-2">
            <Button variant="ghost" type="button" onClick={onClose}>
              Cancel
            </Button>
            <Button type="button" onClick={install}>
              Install GitHub App (mock)
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}
