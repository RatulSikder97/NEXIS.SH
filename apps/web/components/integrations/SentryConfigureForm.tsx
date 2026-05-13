"use client";

// Phase 3 Stage 7 — Sentry Configure form.
//
// Three fields:
//   * dsn — Sentry project DSN (visible).
//   * webhook_secret — auto-generated on mount, copy-only (the user must
//     paste it into Sentry's webhook config so HMACs match).
//   * webhook_url — derived from the public API URL + org id. Surfaced
//     AFTER the secret is saved so the user has both values to paste.
//
// The secret is generated once on mount and kept in a ref so re-renders
// don't rotate it. If the user closes the dialog before submitting we
// throw the secret away — the next open generates a fresh one. We never
// echo a previously-saved secret back to the client (the control-plane
// stores it AES-GCM-encrypted via KeyVault and does not return plaintext).

import * as React from "react";
import { Copy, Loader2 } from "lucide-react";
import { useRouter } from "next/navigation";

import { Button } from "@/components/ui/Button";
import { generateWebhookSecret, integrations, type Integration } from "@/lib/integrations";

const PUBLIC_API =
  process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

function CopyableField({
  label,
  value,
  monospace,
}: {
  label: string;
  value: string;
  monospace?: boolean;
}) {
  const [copied, setCopied] = React.useState(false);
  async function copy() {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1500);
    } catch {
      // navigator.clipboard can be unavailable on insecure contexts; the
      // text is still visible so users can copy manually.
    }
  }
  return (
    <div className="space-y-1">
      <p className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
        {label}
      </p>
      <div className="flex items-center gap-2 rounded-md border border-[var(--color-border)] bg-[var(--color-muted)]/30 p-2">
        <code
          className={
            "flex-1 truncate text-xs " +
            (monospace ? "font-mono" : "")
          }
        >
          {value}
        </code>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          onClick={copy}
          aria-label={`Copy ${label}`}
        >
          <Copy className="h-4 w-4" />
          {copied ? "Copied" : "Copy"}
        </Button>
      </div>
    </div>
  );
}

export function SentryConfigureForm({
  integration,
  orgId,
  onClose,
}: {
  integration?: Integration;
  orgId: string;
  onClose: () => void;
}) {
  const router = useRouter();
  const [dsn, setDsn] = React.useState("");
  // Generate the webhook secret exactly once per mount. useState with a
  // lazy initializer is the React-blessed way to compute a value at mount
  // without re-running on every render; useRef would also work but writing
  // to ref.current during render trips React 19's ref-strictness rule.
  const [secret] = React.useState<string>(() => generateWebhookSecret());

  const webhookUrl = `${PUBLIC_API}/v1/webhooks/sentry/${orgId}`;

  const [pending, setPending] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  const [saved, setSaved] = React.useState(false);

  const connected = integration?.status === "connected";

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setPending(true);
    setError(null);
    try {
      await integrations.connectRaw("sentry", { dsn, webhook_secret: secret });
      setSaved(true);
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
      await integrations.disconnect("sentry");
      onClose();
      router.refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Disconnect failed");
    } finally {
      setPending(false);
    }
  }

  function done() {
    onClose();
    router.refresh();
  }

  if (connected && !saved) {
    // Already-connected state: we don't surface the existing secret (the
    // server never returns it). Operators who need to rotate it should
    // disconnect + reconnect.
    return (
      <div className="space-y-4">
        <p className="text-sm text-[var(--color-muted-foreground)]">
          Sentry is connected. Incidents posted to your webhook URL will be
          ingested by NEXIS.
        </p>
        <CopyableField label="Webhook URL" value={webhookUrl} monospace />
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

  if (saved) {
    return (
      <div className="space-y-4">
        <div className="rounded-md border border-emerald-500/30 bg-emerald-500/10 p-3 text-sm text-emerald-700 dark:text-emerald-300">
          Connection saved. Add the webhook URL + secret to your Sentry
          project to start receiving events.
        </div>
        <CopyableField label="Webhook URL" value={webhookUrl} monospace />
        <CopyableField label="Webhook Secret" value={secret} monospace />
        <p className="text-xs text-[var(--color-muted-foreground)]">
          In Sentry: Settings → Integrations → Webhooks → Add Webhook. Paste
          the URL and set the secret to the value above. This secret is shown
          only once.
        </p>
        <div className="flex justify-end pt-2">
          <Button type="button" onClick={done}>
            Done
          </Button>
        </div>
      </div>
    );
  }

  return (
    <form onSubmit={submit} className="space-y-4">
      <p className="text-sm text-[var(--color-muted-foreground)]">
        Connect your Sentry project so NEXIS can ingest incidents and propose
        fixes.
      </p>
      <div className="space-y-1">
        <label
          htmlFor="sentry-dsn"
          className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]"
        >
          DSN
        </label>
        <input
          id="sentry-dsn"
          name="dsn"
          type="text"
          required
          value={dsn}
          onChange={(e) => setDsn(e.target.value)}
          placeholder="https://<key>@<host>/<project_id>"
          className="w-full rounded-md border border-[var(--color-border)] bg-[var(--color-background)] px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-ring)]"
        />
      </div>
      <CopyableField label="Webhook Secret (generated)" value={secret} monospace />
      <p className="text-[11px] text-[var(--color-muted-foreground)]">
        The secret is generated for you. Copy it now — once saved, NEXIS will
        store it encrypted and will not show it again.
      </p>
      {error && (
        <p className="text-sm text-red-500" role="alert">
          {error}
        </p>
      )}
      <div className="flex justify-end gap-2 pt-2">
        <Button variant="ghost" type="button" onClick={onClose} disabled={pending}>
          Cancel
        </Button>
        <Button type="submit" disabled={pending || !dsn}>
          {pending && <Loader2 className="h-4 w-4 animate-spin" />}
          Connect
        </Button>
      </div>
    </form>
  );
}
