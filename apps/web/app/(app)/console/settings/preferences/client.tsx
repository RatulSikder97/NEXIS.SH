"use client";

// Phase 3 Stage 8 — Preferences client.
//
// Three radios: Light / Dark / System. Selecting one:
//   1. Updates the visual theme immediately via next-themes setTheme().
//   2. PATCHes /v1/me/preferences { theme } so the choice survives a logout.
//
// The "saved" indicator is a transient ✓ that auto-clears after 1.5s. We
// fetch the persisted value on mount to seed the radio state — if it has
// drifted from next-themes' localStorage value we reconcile by trusting the
// server value (since other devices may have set it).

import * as React from "react";
import { useTheme } from "next-themes";
import { CheckCircle2, Loader2 } from "lucide-react";

import { preferences } from "@/lib/preferences";

type ThemeChoice = "light" | "dark" | "system";

const CHOICES: { id: ThemeChoice; label: string; description: string }[] = [
  {
    id: "light",
    label: "Light",
    description: "Always use the light palette.",
  },
  {
    id: "dark",
    label: "Dark",
    description: "Always use the dark palette.",
  },
  {
    id: "system",
    label: "System",
    description: "Match your operating system preference.",
  },
];

export function PreferencesClient() {
  const { theme, setTheme, resolvedTheme } = useTheme();
  const [pending, setPending] = React.useState(false);
  const [saved, setSaved] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);

  // `theme` is undefined during SSR + first paint (next-themes documents
  // this — its provider hydrates async via useEffect). We use that signal
  // to suppress radio-checked rendering until hydration completes, which
  // sidesteps a server/client mismatch warning.
  const mounted = theme !== undefined;

  // Hydrate the radio from the server's persisted preferences on first mount.
  // We deliberately ignore the cached next-themes value — if the user changed
  // it on another device, the server is authoritative.
  React.useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const p = await preferences.get();
        if (cancelled) return;
        if (
          typeof p.theme === "string" &&
          (p.theme === "light" || p.theme === "dark" || p.theme === "system")
        ) {
          // Writes to an external system (next-themes' internal store), so
          // this is exactly what useEffect is for — not a cascading render.
          setTheme(p.theme);
        }
      } catch {
        // Silently ignore — the user can still set the theme; failure to
        // load just means we don't pre-seed. We don't surface the error
        // because it's likely transient or the user hasn't set anything.
      }
    })();
    return () => {
      cancelled = true;
    };
    // setTheme is stable across renders per next-themes; intentionally omit
    // to avoid running on every render.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function pick(choice: ThemeChoice) {
    setTheme(choice);
    setPending(true);
    setError(null);
    try {
      await preferences.patch({ theme: choice });
      setSaved(true);
      window.setTimeout(() => setSaved(false), 1500);
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "Failed to save preferences",
      );
    } finally {
      setPending(false);
    }
  }

  // Until mounted, render a stable placeholder so SSR and the first client
  // paint don't disagree on the radio's `checked` attribute (the persisted
  // theme is only known on the client).
  const current = mounted ? (theme as ThemeChoice | undefined) : undefined;

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold">Preferences</h1>
        <p className="mt-1 text-sm text-[var(--color-muted-foreground)]">
          Personalise how the console looks on this account.
        </p>
      </div>

      <section
        aria-labelledby="theme-heading"
        className="space-y-3 rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-6"
      >
        <div className="flex items-center justify-between gap-4">
          <h2 id="theme-heading" className="text-base font-semibold">
            Theme
          </h2>
          <div
            className="text-xs text-[var(--color-muted-foreground)]"
            aria-live="polite"
          >
            {pending && (
              <span className="inline-flex items-center gap-1.5">
                <Loader2 className="h-3.5 w-3.5 animate-spin" /> Saving…
              </span>
            )}
            {saved && !pending && (
              <span className="inline-flex items-center gap-1.5 text-emerald-600 dark:text-emerald-400">
                <CheckCircle2 className="h-3.5 w-3.5" /> Saved
              </span>
            )}
          </div>
        </div>

        {resolvedTheme && mounted && (
          <p className="text-xs text-[var(--color-muted-foreground)]">
            Currently rendering: <span className="font-mono">{resolvedTheme}</span>
          </p>
        )}

        <div className="grid gap-3 sm:grid-cols-3">
          {CHOICES.map((c) => {
            const checked = current === c.id;
            return (
              <label
                key={c.id}
                className={
                  "flex cursor-pointer items-start gap-3 rounded-md border p-3 text-sm transition-colors " +
                  (checked
                    ? "border-[var(--color-primary)] bg-[var(--color-primary)]/5"
                    : "border-[var(--color-border)] hover:bg-[var(--color-muted)]/40")
                }
              >
                <input
                  type="radio"
                  name="theme"
                  value={c.id}
                  checked={checked}
                  onChange={() => pick(c.id)}
                  className="mt-0.5"
                />
                <div>
                  <p className="font-medium">{c.label}</p>
                  <p className="text-xs text-[var(--color-muted-foreground)]">
                    {c.description}
                  </p>
                </div>
              </label>
            );
          })}
        </div>

        {error && (
          <p className="text-sm text-red-500" role="alert">
            {error}
          </p>
        )}
      </section>
    </div>
  );
}
