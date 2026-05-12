"use client";

// Phase 3 Stage 8 — API Keys client.
//
// Three flows: list (provided by parent), create (dialog → POST → show
// plaintext once), revoke (DELETE). The plaintext is held only in component
// state for the duration of the post-create dialog; once the user dismisses
// it we drop the value and rely on the masked prefix going forward.
//
// Scopes for Phase 3 are limited to "read". When Phase 4 introduces additional
// scopes the SCOPES array below grows; the dialog renders a checkbox per row.

import * as React from "react";
import * as Dialog from "@radix-ui/react-dialog";
import { Copy, Loader2, X } from "lucide-react";
import { useRouter } from "next/navigation";

import { Button } from "@/components/ui/Button";
import { apikeys, type APIKey, type APIKeyCreated } from "@/lib/apikeys";

const SCOPES = [{ id: "read", label: "Read", description: "Read-only access." }];

function formatDate(iso: string): string {
  if (!iso) return "—";
  try {
    return new Date(iso).toLocaleString(undefined, {
      month: "short",
      day: "numeric",
      year: "numeric",
    });
  } catch {
    return iso;
  }
}

function CopyButton({ value, label }: { value: string; label: string }) {
  const [copied, setCopied] = React.useState(false);
  async function copy() {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1500);
    } catch {
      // clipboard may be unavailable on insecure contexts; value is still
      // displayed for manual copy.
    }
  }
  return (
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
  );
}

export function APIKeysClient({ initial }: { initial: APIKey[] }) {
  const router = useRouter();
  const [createOpen, setCreateOpen] = React.useState(false);
  const [name, setName] = React.useState("");
  const [selectedScopes, setSelectedScopes] = React.useState<string[]>(["read"]);
  const [pending, setPending] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  const [revealed, setRevealed] = React.useState<APIKeyCreated | null>(null);
  const [revoking, setRevoking] = React.useState<string | null>(null);

  function toggleScope(id: string) {
    setSelectedScopes((curr) =>
      curr.includes(id) ? curr.filter((s) => s !== id) : [...curr, id],
    );
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setPending(true);
    setError(null);
    try {
      const created = await apikeys.create(name, selectedScopes);
      setRevealed(created);
      setCreateOpen(false);
      setName("");
      setSelectedScopes(["read"]);
      router.refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to create key");
    } finally {
      setPending(false);
    }
  }

  async function revoke(id: string) {
    setRevoking(id);
    setError(null);
    try {
      await apikeys.revoke(id);
      router.refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to revoke key");
    } finally {
      setRevoking(null);
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold">API Keys</h1>
          <p className="mt-1 text-sm text-[var(--color-muted-foreground)]">
            Use these from CI or scripts to call the NEXIS API on behalf of
            your account.
          </p>
        </div>
        <Button type="button" onClick={() => setCreateOpen(true)}>
          Create API key
        </Button>
      </div>

      {error && (
        <div
          role="alert"
          className="rounded-md border border-red-500/30 bg-red-500/10 px-4 py-3 text-sm text-red-700 dark:text-red-300"
        >
          {error}
        </div>
      )}

      <div className="overflow-hidden rounded-lg border border-[var(--color-border)] bg-[var(--color-card)]">
        {initial.length === 0 ? (
          <p className="px-4 py-8 text-center text-sm text-[var(--color-muted-foreground)]">
            No API keys yet. Create one to get started.
          </p>
        ) : (
          <table className="w-full text-sm">
            <thead className="bg-[var(--color-muted)]/40 text-left text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
              <tr>
                <th className="px-4 py-2 font-medium">Name</th>
                <th className="px-4 py-2 font-medium">Prefix</th>
                <th className="px-4 py-2 font-medium">Scopes</th>
                <th className="px-4 py-2 font-medium">Created</th>
                <th className="px-4 py-2 font-medium">Last used</th>
                <th className="px-4 py-2 font-medium" aria-hidden></th>
              </tr>
            </thead>
            <tbody>
              {initial.map((k) => (
                <tr
                  key={k.id}
                  className="border-t border-[var(--color-border)]"
                >
                  <td className="px-4 py-3">{k.name}</td>
                  <td className="px-4 py-3 font-mono text-xs">{k.prefix}…</td>
                  <td className="px-4 py-3 text-xs text-[var(--color-muted-foreground)]">
                    {k.scopes.join(", ") || "—"}
                  </td>
                  <td className="px-4 py-3 text-[var(--color-muted-foreground)]">
                    {formatDate(k.created_at)}
                  </td>
                  <td className="px-4 py-3 text-[var(--color-muted-foreground)]">
                    {k.last_used_at ? formatDate(k.last_used_at) : "Never"}
                  </td>
                  <td className="px-4 py-3 text-right">
                    <Button
                      size="sm"
                      variant="outline"
                      type="button"
                      disabled={revoking === k.id}
                      onClick={() => revoke(k.id)}
                    >
                      {revoking === k.id && (
                        <Loader2 className="h-4 w-4 animate-spin" />
                      )}
                      Revoke
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      <Dialog.Root open={createOpen} onOpenChange={setCreateOpen}>
        <Dialog.Portal>
          <Dialog.Overlay className="fixed inset-0 z-40 bg-black/40 backdrop-blur-sm" />
          <Dialog.Content className="fixed left-1/2 top-1/2 z-50 w-[min(480px,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-6 shadow-xl outline-none">
            <div className="flex items-start justify-between gap-4 pb-4">
              <div>
                <Dialog.Title className="text-lg font-semibold">
                  Create API key
                </Dialog.Title>
                <Dialog.Description className="mt-1 text-xs text-[var(--color-muted-foreground)]">
                  The plaintext key will be shown once on creation. Copy it
                  immediately — NEXIS cannot recover it later.
                </Dialog.Description>
              </div>
              <Dialog.Close asChild>
                <button
                  type="button"
                  aria-label="Close"
                  className="inline-flex h-8 w-8 items-center justify-center rounded-md text-[var(--color-muted-foreground)] hover:bg-[var(--color-muted)] hover:text-[var(--color-foreground)]"
                >
                  <X className="h-4 w-4" />
                </button>
              </Dialog.Close>
            </div>
            <form onSubmit={submit} className="space-y-4">
              <div className="space-y-1">
                <label
                  htmlFor="key-name"
                  className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]"
                >
                  Name
                </label>
                <input
                  id="key-name"
                  type="text"
                  required
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder="ci-pipeline"
                  className="w-full rounded-md border border-[var(--color-border)] bg-[var(--color-background)] px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-ring)]"
                />
              </div>
              <div className="space-y-2">
                <p className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
                  Scopes
                </p>
                {SCOPES.map((s) => (
                  <label
                    key={s.id}
                    className="flex items-start gap-3 rounded-md border border-[var(--color-border)] bg-[var(--color-background)] p-3 text-sm"
                  >
                    <input
                      type="checkbox"
                      checked={selectedScopes.includes(s.id)}
                      onChange={() => toggleScope(s.id)}
                      className="mt-0.5"
                    />
                    <div>
                      <p className="font-medium">{s.label}</p>
                      <p className="text-xs text-[var(--color-muted-foreground)]">
                        {s.description}
                      </p>
                    </div>
                  </label>
                ))}
              </div>
              {error && (
                <p className="text-sm text-red-500" role="alert">
                  {error}
                </p>
              )}
              <div className="flex justify-end gap-2 pt-2">
                <Button
                  variant="ghost"
                  type="button"
                  onClick={() => setCreateOpen(false)}
                  disabled={pending}
                >
                  Cancel
                </Button>
                <Button
                  type="submit"
                  disabled={pending || !name || selectedScopes.length === 0}
                >
                  {pending && <Loader2 className="h-4 w-4 animate-spin" />}
                  Create
                </Button>
              </div>
            </form>
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>

      <Dialog.Root
        open={revealed !== null}
        onOpenChange={(open) => {
          if (!open) setRevealed(null);
        }}
      >
        <Dialog.Portal>
          <Dialog.Overlay className="fixed inset-0 z-40 bg-black/40 backdrop-blur-sm" />
          <Dialog.Content className="fixed left-1/2 top-1/2 z-50 w-[min(540px,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-6 shadow-xl outline-none">
            <div className="flex items-start justify-between gap-4 pb-4">
              <div>
                <Dialog.Title className="text-lg font-semibold">
                  API key created
                </Dialog.Title>
                <Dialog.Description className="mt-1 text-xs text-[var(--color-muted-foreground)]">
                  Copy the key now. It will not be shown again.
                </Dialog.Description>
              </div>
              <Dialog.Close asChild>
                <button
                  type="button"
                  aria-label="Close"
                  className="inline-flex h-8 w-8 items-center justify-center rounded-md text-[var(--color-muted-foreground)] hover:bg-[var(--color-muted)] hover:text-[var(--color-foreground)]"
                >
                  <X className="h-4 w-4" />
                </button>
              </Dialog.Close>
            </div>
            {revealed && (
              <div className="space-y-4">
                <div className="space-y-1">
                  <p className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
                    Plaintext (shown once)
                  </p>
                  <div className="flex items-center gap-2 rounded-md border border-[var(--color-border)] bg-[var(--color-muted)]/30 p-2">
                    <code className="flex-1 truncate font-mono text-xs">
                      {revealed.plaintext_once}
                    </code>
                    <CopyButton
                      value={revealed.plaintext_once}
                      label="API key"
                    />
                  </div>
                </div>
                <p className="text-[11px] text-[var(--color-muted-foreground)]">
                  Store this in a password manager or your CI secret store
                  immediately.
                </p>
                <div className="flex justify-end">
                  <Button type="button" onClick={() => setRevealed(null)}>
                    Done
                  </Button>
                </div>
              </div>
            )}
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>
    </div>
  );
}
