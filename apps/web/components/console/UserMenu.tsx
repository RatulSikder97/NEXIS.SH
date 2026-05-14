"use client";

// Phase 3 — User menu pinned to the right of the Topbar.
// Shows the signed-in user's email and a dropdown with quick links +
// Log out. Logout calls the control-plane to revoke the session, clears
// the cookie via the response Set-Cookie, then bounces to /sign-in.

import * as React from "react";
import Link from "next/link";
import * as DropdownMenu from "@radix-ui/react-dropdown-menu";
import { ChevronDown, LogOut, Settings, User } from "lucide-react";

import { auth } from "@/lib/auth";

export function UserMenu({ email }: { email: string }) {
  const [busy, setBusy] = React.useState(false);

  const initial = email.charAt(0).toUpperCase();
  const localPart = email.split("@")[0];

  async function logout() {
    setBusy(true);
    try {
      await auth.logout();
    } catch {
      // best effort — even if the revoke fails, redirect to /sign-in.
    } finally {
      // Hard navigate so the proxy re-evaluates with the cleared cookie.
      window.location.href = "/sign-in";
    }
  }

  return (
    <DropdownMenu.Root>
      <DropdownMenu.Trigger asChild>
        <button
          type="button"
          aria-label="Account menu"
          className="inline-flex items-center gap-2 rounded-md border border-[var(--color-border)] bg-[var(--color-card)] px-2 py-1.5 text-sm text-[var(--color-foreground)] hover:bg-[var(--color-muted)]"
        >
          <span
            aria-hidden
            className="flex h-6 w-6 items-center justify-center rounded-full bg-[var(--color-primary)] text-[10px] font-semibold text-[var(--color-primary-foreground)]"
          >
            {initial}
          </span>
          <span className="hidden max-w-[140px] truncate sm:inline">{localPart}</span>
          <ChevronDown className="h-3.5 w-3.5 text-[var(--color-muted-foreground)]" />
        </button>
      </DropdownMenu.Trigger>
      <DropdownMenu.Portal>
        <DropdownMenu.Content
          align="end"
          sideOffset={6}
          className="z-50 min-w-[220px] rounded-md border border-[var(--color-border)] bg-[var(--color-card)] p-1 shadow-md"
        >
          <div className="px-2 py-2 text-xs text-[var(--color-muted-foreground)]">
            Signed in as
            <div className="mt-0.5 truncate text-sm text-[var(--color-foreground)]">{email}</div>
          </div>
          <DropdownMenu.Separator className="my-1 h-px bg-[var(--color-border)]" />
          <DropdownMenu.Item asChild>
            <Link
              href="/console/settings/profile"
              className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-sm outline-none hover:bg-[var(--color-muted)] focus:bg-[var(--color-muted)]"
            >
              <User className="h-4 w-4" />
              Profile
            </Link>
          </DropdownMenu.Item>
          <DropdownMenu.Item asChild>
            <Link
              href="/console/settings/preferences"
              className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-sm outline-none hover:bg-[var(--color-muted)] focus:bg-[var(--color-muted)]"
            >
              <Settings className="h-4 w-4" />
              Settings
            </Link>
          </DropdownMenu.Item>
          <DropdownMenu.Separator className="my-1 h-px bg-[var(--color-border)]" />
          <DropdownMenu.Item
            disabled={busy}
            onSelect={(e) => {
              e.preventDefault();
              void logout();
            }}
            className="flex w-full cursor-pointer items-center gap-2 rounded px-2 py-1.5 text-sm text-[var(--color-destructive)] outline-none hover:bg-[var(--color-muted)] focus:bg-[var(--color-muted)] data-[disabled]:opacity-60"
          >
            <LogOut className="h-4 w-4" />
            {busy ? "Logging out…" : "Log out"}
          </DropdownMenu.Item>
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  );
}
