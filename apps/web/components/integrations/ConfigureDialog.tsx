"use client";

// Real-integrations Wave 1 — generic, manifest-driven Configure dialog.
//
// Reads an `IntegrationManifest` (see lib/integrations-config.ts) and
// renders one of two branches:
//
//   * flow === "oauth"  → A description + a "Continue with {label}" button
//     that bounces the browser to `manifest.oauth_start_url`. The
//     control-plane owns the consent screen + callback; the dialog has no
//     state of its own beyond a disabled flag while the navigation is in
//     flight.
//
//   * flow === "form"   → One input per `manifest.fields[]` entry. Submit
//     POSTs `{config: {...}}` to /v1/integrations/{provider}/connect via
//     the SDK in lib/integrations.ts. A 2xx fires `onSuccess` and closes;
//     a non-2xx surfaces an inline error banner above the form.
//
// We deliberately do not handle a "connected" branch here — the existing
// per-provider forms (GitHubConfigureForm/SentryConfigureForm/...) still
// own the manage-and-disconnect path until Wave 2 lands the live backend.
// This dialog is wired in as the Configure CTA's first surface; once the
// backend lands the parent can decide whether to keep the legacy form for
// the connected path or fold it into this dialog.

import * as React from "react";
import * as Dialog from "@radix-ui/react-dialog";
import { ExternalLink, Loader2, X } from "lucide-react";

import { Button } from "@/components/ui/Button";
import { integrations } from "@/lib/integrations";
import type {
  ConfigureField,
  IntegrationManifest,
} from "@/lib/integrations-config";
import { cn } from "@/lib/utils";

type Props = {
  manifest: IntegrationManifest;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  // Fired after a successful connect (form flow) so the parent can
  // router.refresh() the integrations list. OAuth flow never fires this —
  // the redirect/callback round-trip is responsible for the refresh.
  onSuccess?: () => void;
};

const API = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

// initialValues seeds every form input from the manifest's `default` field
// so React inputs stay controlled from first paint (a `value={undefined}`
// input would flip to uncontrolled and warn).
function initialValues(fields: ConfigureField[] | undefined): Record<string, string> {
  const v: Record<string, string> = {};
  for (const f of fields ?? []) {
    v[f.name] = f.default ?? "";
  }
  return v;
}

function FieldInput({
  field,
  value,
  onChange,
  inputRef,
}: {
  field: ConfigureField;
  value: string;
  onChange: (next: string) => void;
  inputRef?: React.Ref<HTMLInputElement | HTMLSelectElement>;
}) {
  const id = `cfg-${field.name}`;
  const common =
    "w-full rounded-md border border-[var(--color-border)] bg-[var(--color-background)] px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-ring)]";

  return (
    <div className="space-y-1">
      <label
        htmlFor={id}
        className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]"
      >
        {field.label}
        {field.required && <span className="ml-0.5 text-red-500">*</span>}
      </label>

      {field.type === "select" ? (
        <select
          id={id}
          name={field.name}
          required={field.required}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          ref={inputRef as React.Ref<HTMLSelectElement>}
          className={common}
        >
          {(field.options ?? []).map((opt) => (
            <option key={opt.value} value={opt.value}>
              {opt.label}
            </option>
          ))}
        </select>
      ) : (
        <input
          id={id}
          name={field.name}
          type={field.type === "url" ? "url" : field.type === "password" ? "password" : "text"}
          required={field.required}
          autoComplete={field.type === "password" ? "off" : undefined}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          placeholder={field.placeholder}
          ref={inputRef as React.Ref<HTMLInputElement>}
          className={common}
        />
      )}

      {field.help && (
        <p className="text-[11px] text-[var(--color-muted-foreground)]">
          {field.help}
        </p>
      )}
    </div>
  );
}

export function ConfigureDialog({ manifest, open, onOpenChange, onSuccess }: Props) {
  const isOAuth = manifest.flow === "oauth";

  // Hooks must run regardless of branch. We hold a single values map keyed
  // by field name. For the OAuth branch this stays empty.
  const [values, setValues] = React.useState<Record<string, string>>(() =>
    initialValues(manifest.fields),
  );
  const [pending, setPending] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);

  // Refs used by Dialog.Content's onOpenAutoFocus to redirect initial focus
  // away from the Close button (which Radix would otherwise pick because it
  // is the first focusable child in source order). Per WCAG 2.4.3, initial
  // focus on a destructive control is hostile UX — focus the primary action
  // / first form field instead.
  const firstFieldRef = React.useRef<HTMLInputElement | HTMLSelectElement | null>(null);
  const primaryActionRef = React.useRef<HTMLButtonElement | null>(null);

  // Reset state when the manifest swaps. We use the React-blessed "adjust
  // state during render by comparing to previous prop" pattern instead of an
  // effect — React 19's react-hooks/set-state-in-effect rule rightly flags
  // effect-driven resets as cascading renders, and the docs explicitly
  // recommend this idiom for "reset some state when a prop changes":
  //   https://react.dev/learn/you-might-not-need-an-effect
  const [prevProvider, setPrevProvider] = React.useState(manifest.provider);
  if (prevProvider !== manifest.provider) {
    setPrevProvider(manifest.provider);
    setValues(initialValues(manifest.fields));
    setError(null);
    setPending(false);
  }

  function setField(name: string, next: string) {
    setValues((prev) => ({ ...prev, [name]: next }));
  }

  function startOAuth() {
    if (!manifest.oauth_start_url) return;
    setPending(true);
    // Manifests carry server-relative paths (e.g. "/v1/integrations/...").
    // Prepend the public API URL when the URL isn't already absolute so the
    // browser navigates to the control-plane, not the Next.js app.
    const url = /^https?:\/\//i.test(manifest.oauth_start_url)
      ? manifest.oauth_start_url
      : `${API}${manifest.oauth_start_url}`;
    window.location.assign(url);
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setPending(true);
    setError(null);
    try {
      await integrations.connect(manifest.provider, values);
      onSuccess?.();
      onOpenChange(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Connect failed");
    } finally {
      setPending(false);
    }
  }

  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-40 bg-black/40 backdrop-blur-sm data-[state=open]:animate-in data-[state=open]:fade-in data-[state=closed]:animate-out data-[state=closed]:fade-out" />
        <Dialog.Content
          onOpenAutoFocus={(event) => {
            // Redirect Radix's default "focus first tabbable" (which is the
            // Close button) to the first form field (form flow) or the
            // primary CTA (OAuth flow). Falls back to the default if neither
            // ref is mounted yet.
            const target = isOAuth ? primaryActionRef.current : firstFieldRef.current;
            if (target) {
              event.preventDefault();
              target.focus();
            }
          }}
          className="fixed left-1/2 top-1/2 z-50 w-[min(560px,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-6 shadow-xl outline-none data-[state=open]:animate-in data-[state=open]:fade-in data-[state=open]:zoom-in-95 data-[state=closed]:animate-out data-[state=closed]:fade-out"
        >
          <div className="flex items-start justify-between gap-4 pb-4">
            <div>
              <Dialog.Title className="text-lg font-semibold">
                Configure {manifest.label}
              </Dialog.Title>
              <Dialog.Description className="mt-1 text-xs text-[var(--color-muted-foreground)]">
                Credentials are stored encrypted (AES-GCM).
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

          {isOAuth ? (
            <div className="space-y-4">
              <p className="text-sm text-[var(--color-muted-foreground)]">
                {manifest.description}
              </p>
              {error && (
                <div
                  role="alert"
                  className="rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-sm text-red-700 dark:text-red-300"
                >
                  {error}
                </div>
              )}
              <div className="flex items-center justify-between gap-3 pt-2">
                <a
                  href={manifest.docs_url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="inline-flex items-center gap-1 text-xs text-[var(--color-muted-foreground)] hover:text-[var(--color-foreground)]"
                >
                  Docs <ExternalLink className="h-3 w-3" />
                </a>
                <div className="flex gap-2">
                  <Button
                    variant="ghost"
                    type="button"
                    onClick={() => onOpenChange(false)}
                    disabled={pending}
                  >
                    Cancel
                  </Button>
                  <Button
                    type="button"
                    onClick={startOAuth}
                    disabled={pending}
                    ref={primaryActionRef}
                  >
                    {pending && <Loader2 className="h-4 w-4 animate-spin" />}
                    Continue with {manifest.label}
                  </Button>
                </div>
              </div>
            </div>
          ) : (
            <form onSubmit={submit} className="space-y-4">
              <p className="text-sm text-[var(--color-muted-foreground)]">
                {manifest.description}
              </p>

              {error && (
                <div
                  role="alert"
                  className={cn(
                    "rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-sm text-red-700 dark:text-red-300",
                  )}
                >
                  {error}
                </div>
              )}

              <div className="space-y-3">
                {(manifest.fields ?? []).map((f, idx) => (
                  <FieldInput
                    key={f.name}
                    field={f}
                    value={values[f.name] ?? ""}
                    onChange={(v) => setField(f.name, v)}
                    inputRef={idx === 0 ? firstFieldRef : undefined}
                  />
                ))}
              </div>

              <div className="flex items-center justify-between gap-3 pt-2">
                <a
                  href={manifest.docs_url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="inline-flex items-center gap-1 text-xs text-[var(--color-muted-foreground)] hover:text-[var(--color-foreground)]"
                >
                  Docs <ExternalLink className="h-3 w-3" />
                </a>
                <div className="flex gap-2">
                  <Button
                    variant="ghost"
                    type="button"
                    onClick={() => onOpenChange(false)}
                    disabled={pending}
                  >
                    Cancel
                  </Button>
                  <Button type="submit" disabled={pending}>
                    {pending && <Loader2 className="h-4 w-4 animate-spin" />}
                    Connect
                  </Button>
                </div>
              </div>
            </form>
          )}
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
