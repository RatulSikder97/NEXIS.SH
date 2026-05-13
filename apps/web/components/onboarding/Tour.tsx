"use client";

// Phase 8 — In-app guided tour (Shepherd.js).
//
// First console visit triggers a 6-step dismissible tour. Persisted via
// `users.preferences.tour_completed=true|false`. Lazy-loaded — Shepherd
// (~30 KB gzip) only ships once per user, and only on first visit.
//
// Mounting contract:
//   <Tour /> is rendered by the console layout when
//   `me.preferences.tour_completed` is falsy. The component:
//     1. Waits for the next animation frame so the DOM is settled.
//     2. Dynamically imports shepherd.js + its CSS.
//     3. Builds a 6-step tour, attaches it to data-tour anchors.
//     4. On complete/cancel: PATCH /v1/me/preferences { tour_completed: true }
//        + tear down the tour instance + remove its DOM.
//
// The component renders nothing visible itself — shepherd.js paints
// overlays / popovers directly into <body> on its own schedule.

import * as React from "react";

import { preferences } from "@/lib/preferences";

// Step config kept module-scoped so re-renders don't rebuild the closures.
// `attachTo.element` is a CSS selector evaluated when the step is shown.
type StepCfg = {
  id: string;
  title: string;
  text: string;
  element?: string;
  position?: "top" | "bottom" | "left" | "right" | "auto";
};

const STEPS: StepCfg[] = [
  {
    id: "welcome",
    title: "Welcome to NEXIS",
    text:
      "Quick six-step tour of the console. Use the buttons below to step through, or hit Esc to dismiss.",
    element: undefined,
  },
  {
    id: "topbar",
    title: "Workspace + Search",
    text:
      "The top bar shows your active workspace and a global search (⌘K) that jumps to any incident or setting.",
    element: 'header[class*="sticky"]',
    position: "bottom",
  },
  {
    id: "sidebar-incidents",
    title: "Incidents",
    text:
      "Every recovery-pipeline run lives here. Click into a row to watch the agent timeline live.",
    element: 'a[href="/console/incidents"]',
    position: "right",
  },
  {
    id: "sidebar-integrations",
    title: "Integrations",
    text:
      "Wire up GitHub, Sentry, and ArgoCD. NEXIS uses these to ingest incidents and ship fixes.",
    element: 'a[href="/console/integrations"]',
    position: "right",
  },
  {
    id: "sidebar-approvals",
    title: "Approvals",
    text:
      "High-severity fixes pause for a human signal here. The badge counts how many are waiting on you.",
    element: 'a[href="/console/approvals"]',
    position: "right",
  },
  {
    id: "live-demo",
    title: "Run a synthetic incident",
    text:
      "Open Live Demo to fire the deterministic fixture from anywhere in the console — the loop runs end-to-end in under a minute.",
    element: 'a[href="/console/live-demo"]',
    position: "right",
  },
];

// Anchor lookup tolerates a missing element (e.g. sidebar collapsed,
// element not yet mounted) by attaching to body with auto positioning so
// the step still renders as a centred modal.
function attachFor(step: StepCfg) {
  if (!step.element) return undefined;
  return { element: step.element, on: step.position ?? "auto" };
}

export default function Tour() {
  // Strict-mode safe: keep the running tour instance on a ref so a second
  // mount (React 19 dev double-invoke) doesn't spawn two tours.
  const startedRef = React.useRef(false);

  React.useEffect(() => {
    if (startedRef.current) return;
    startedRef.current = true;

    let cancelled = false;
    // tour kept in closure scope so the cleanup function can tear it down
    // without a second module-scope variable.
    type TourLike = {
      addStep: (s: unknown) => void;
      start: () => void;
      complete: () => void;
      cancel: () => void;
      on: (ev: string, cb: () => void) => void;
    };
    let tour: TourLike | null = null;

    async function markCompleted() {
      try {
        await preferences.patch({ tour_completed: true });
      } catch {
        // Non-fatal: the user can still close the tour. They'll just see
        // it again on next mount, which is annoying but not broken.
      }
    }

    async function boot() {
      try {
        // Shepherd.js + its stylesheet are both lazy-imported so we don't
        // pay the ~30 KB cost on returning users (who never mount Tour).
        const [{ default: Shepherd }] = await Promise.all([
          import("shepherd.js"),
          import("shepherd.js/dist/css/shepherd.css"),
        ]);
        if (cancelled) return;

        tour = new Shepherd.Tour({
          useModalOverlay: true,
          defaultStepOptions: {
            cancelIcon: { enabled: true },
            scrollTo: { behavior: "smooth", block: "center" },
            classes: "nx-shepherd-step",
          },
        }) as unknown as TourLike;

        for (let i = 0; i < STEPS.length; i++) {
          const s = STEPS[i];
          const isLast = i === STEPS.length - 1;
          const isFirst = i === 0;
          tour.addStep({
            id: s.id,
            title: s.title,
            text: s.text,
            attachTo: attachFor(s),
            buttons: [
              ...(isFirst
                ? []
                : [
                    {
                      text: "Back",
                      action: () => {
                        tour?.cancel();
                      },
                      classes: "nx-shepherd-button-secondary",
                      secondary: true,
                    },
                  ]),
              {
                text: isLast ? "Done" : "Next",
                action: () => {
                  if (isLast) tour?.complete();
                  else {
                    // Shepherd's button action receives the tour as `this`;
                    // calling next() on the closure-bound instance works.
                    (tour as unknown as { next: () => void }).next();
                  }
                },
                classes: "nx-shepherd-button-primary",
              },
            ],
          });
        }

        tour.on("complete", () => {
          void markCompleted();
        });
        tour.on("cancel", () => {
          void markCompleted();
        });

        tour.start();
      } catch {
        // shepherd.js failed to load — bail silently. Users can still
        // restart the tour from Help once that surface lands.
      }
    }

    // Defer one frame so the surrounding layout has mounted its anchors.
    const raf =
      typeof window !== "undefined"
        ? window.requestAnimationFrame(() => {
            void boot();
          })
        : 0;

    return () => {
      cancelled = true;
      if (raf) window.cancelAnimationFrame(raf);
      try {
        tour?.cancel();
      } catch {
        /* ignore */
      }
    };
  }, []);

  return null;
}
