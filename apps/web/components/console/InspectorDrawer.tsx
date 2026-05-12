"use client";

// Phase 3 Stage 6 — Inspector drawer.
//
// A right-anchored slide-over built on Radix Dialog. Used by future stages
// (Audit row inspector, Incident detail, etc.) — for this stage the
// component exists and imports cleanly but is not yet mounted anywhere.
// Width is fixed at 480px; the consumer controls open state.

import * as React from "react";
import * as Dialog from "@radix-ui/react-dialog";
import { X } from "lucide-react";

import { cn } from "@/lib/utils";

export interface InspectorDrawerProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: React.ReactNode;
  description?: React.ReactNode;
  children?: React.ReactNode;
  footer?: React.ReactNode;
}

export function InspectorDrawer({
  open,
  onOpenChange,
  title,
  description,
  children,
  footer,
}: InspectorDrawerProps) {
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay
          className={cn(
            "fixed inset-0 z-40 bg-black/40 backdrop-blur-sm",
            "data-[state=open]:animate-in data-[state=open]:fade-in",
            "data-[state=closed]:animate-out data-[state=closed]:fade-out",
          )}
        />
        <Dialog.Content
          className={cn(
            "fixed right-0 top-0 z-50 flex h-full w-[480px] max-w-[100vw] flex-col border-l border-[var(--color-border)] bg-[var(--color-card)] shadow-xl outline-none",
            "data-[state=open]:animate-in data-[state=open]:slide-in-from-right",
            "data-[state=closed]:animate-out data-[state=closed]:slide-out-to-right",
          )}
        >
          <div className="flex items-start justify-between gap-4 border-b border-[var(--color-border)] px-5 py-4">
            <div className="min-w-0 flex-1">
              <Dialog.Title className="text-base font-semibold text-[var(--color-foreground)]">
                {title}
              </Dialog.Title>
              {description && (
                <Dialog.Description className="mt-1 text-sm text-[var(--color-muted-foreground)]">
                  {description}
                </Dialog.Description>
              )}
            </div>
            <Dialog.Close asChild>
              <button
                type="button"
                aria-label="Close"
                className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-[var(--color-muted-foreground)] hover:bg-[var(--color-muted)] hover:text-[var(--color-foreground)]"
              >
                <X className="h-4 w-4" />
              </button>
            </Dialog.Close>
          </div>
          <div className="flex-1 overflow-y-auto px-5 py-4">{children}</div>
          {footer && (
            <div className="border-t border-[var(--color-border)] px-5 py-4">
              {footer}
            </div>
          )}
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
