"use client";

// Phase 3 Stage 6 — Command palette.
//
// Wraps `cmdk`'s Command.Dialog and listens globally for ⌘K / Ctrl+K to
// toggle. Five static items navigate to the primary console routes; future
// stages will register additional commands. The component is fully self-
// contained so the Topbar can render <CommandPalette /> without props.

import * as React from "react";
import { useRouter } from "next/navigation";
import { Command } from "cmdk";
import {
  AlertTriangle,
  CheckSquare,
  FileText,
  Home,
  Plug,
  type LucideIcon,
} from "lucide-react";

import { cn } from "@/lib/utils";

type PaletteItem = {
  label: string;
  hint?: string;
  icon: LucideIcon;
  href: string;
};

const ITEMS: PaletteItem[] = [
  { label: "Go to Home", hint: "Console overview", icon: Home, href: "/console" },
  {
    label: "Go to Incidents",
    hint: "Incident timeline",
    icon: AlertTriangle,
    href: "/console/incidents",
  },
  {
    label: "Go to Approvals",
    hint: "Pending agent actions",
    icon: CheckSquare,
    href: "/console/approvals",
  },
  { label: "Go to Audit", hint: "Audit log", icon: FileText, href: "/console/audit" },
  {
    label: "Go to Integrations",
    hint: "GitHub, Sentry, ArgoCD",
    icon: Plug,
    href: "/console/integrations",
  },
];

export function CommandPalette() {
  const router = useRouter();
  const [open, setOpen] = React.useState(false);

  React.useEffect(() => {
    function onKey(e: KeyboardEvent) {
      const isToggle = (e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k";
      if (isToggle) {
        e.preventDefault();
        setOpen((o) => !o);
      }
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  function go(href: string) {
    setOpen(false);
    // typedRoutes accepts Route, our href is a known string; cast via unknown.
    router.push(href as unknown as never);
  }

  return (
    <Command.Dialog
      open={open}
      onOpenChange={setOpen}
      label="Command palette"
      className={cn(
        "fixed left-1/2 top-1/4 z-50 w-[min(560px,90vw)] -translate-x-1/2 overflow-hidden rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] shadow-2xl",
      )}
      overlayClassName="fixed inset-0 z-40 bg-black/40 backdrop-blur-sm"
    >
      <Command.Input
        placeholder="Type a command or search…"
        className="w-full border-b border-[var(--color-border)] bg-transparent px-4 py-3 text-sm text-[var(--color-foreground)] placeholder:text-[var(--color-muted-foreground)] focus:outline-none"
      />
      <Command.List className="max-h-80 overflow-y-auto p-2">
        <Command.Empty className="px-3 py-6 text-center text-sm text-[var(--color-muted-foreground)]">
          No results.
        </Command.Empty>
        <Command.Group heading="Navigate" className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)] [&_[cmdk-group-heading]]:px-3 [&_[cmdk-group-heading]]:py-2">
          {ITEMS.map((item) => {
            const Icon = item.icon;
            return (
              <Command.Item
                key={item.href}
                value={`${item.label} ${item.hint ?? ""}`}
                onSelect={() => go(item.href)}
                className="flex cursor-pointer items-center gap-3 rounded-md px-3 py-2 text-sm text-[var(--color-foreground)] aria-selected:bg-[var(--color-muted)]"
              >
                <Icon className="h-4 w-4 text-[var(--color-muted-foreground)]" />
                <span className="flex-1">{item.label}</span>
                {item.hint && (
                  <span className="text-xs text-[var(--color-muted-foreground)]">
                    {item.hint}
                  </span>
                )}
              </Command.Item>
            );
          })}
        </Command.Group>
      </Command.List>
    </Command.Dialog>
  );
}
