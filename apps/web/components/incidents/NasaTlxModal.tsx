"use client";

// Phase 8 — NASA-TLX subjective survey modal.
//
// Six 21-point sliders (0–20 each) on the canonical NASA-TLX axes plus an
// optional notes textarea. Submission POSTs to /v1/nasa-tlx; skipping
// closes the modal and we don't re-prompt (the parent gates on
// `successful_recoveries_count === 3`).
//
// UX detail: the Performance slider is labelled "Perfect → Failure" per
// the NASA TLX manual, the others are "Very Low → Very High". The slider
// values themselves are still 0..20; the inversion is purely cosmetic.

import * as React from "react";
import * as Dialog from "@radix-ui/react-dialog";
import { Loader2, X } from "lucide-react";

import { Button } from "@/components/ui/Button";
import { nasaTlx, type NasaTlxSubmission } from "@/lib/nasa-tlx";

type Axis = {
  key: keyof Omit<NasaTlxSubmission, "notes" | "recovery_run_id">;
  label: string;
  prompt: string;
  // For Performance the visual endpoints are swapped.
  lowLabel: string;
  highLabel: string;
};

// Wording taken verbatim from the NASA TLX manual (Appendix E of the
// Phase 8 spec). Don't paraphrase — the academic survey is only valid
// against the canonical wording.
const AXES: Axis[] = [
  {
    key: "mental_demand",
    label: "Mental Demand",
    prompt: "How mentally demanding was the task?",
    lowLabel: "Very Low",
    highLabel: "Very High",
  },
  {
    key: "physical_demand",
    label: "Physical Demand",
    prompt: "How physically demanding was the task?",
    lowLabel: "Very Low",
    highLabel: "Very High",
  },
  {
    key: "temporal_demand",
    label: "Temporal Demand",
    prompt: "How hurried or rushed was the pace of the task?",
    lowLabel: "Very Low",
    highLabel: "Very High",
  },
  {
    key: "performance",
    label: "Performance",
    prompt:
      "How successful were you in accomplishing what you were asked to do?",
    lowLabel: "Perfect",
    highLabel: "Failure",
  },
  {
    key: "effort",
    label: "Effort",
    prompt:
      "How hard did you have to work to accomplish your level of performance?",
    lowLabel: "Very Low",
    highLabel: "Very High",
  },
  {
    key: "frustration",
    label: "Frustration",
    prompt:
      "How insecure, discouraged, irritated, stressed, and annoyed were you?",
    lowLabel: "Very Low",
    highLabel: "Very High",
  },
];

export function NasaTlxModal({
  open,
  onOpenChange,
  recoveryRunId,
}: {
  open: boolean;
  onOpenChange: (next: boolean) => void;
  recoveryRunId?: string;
}) {
  // The six sliders share one piece of state. Default each to 10 (mid-
  // point) so the modal looks unbiased on open.
  const [scores, setScores] = React.useState<
    Record<Axis["key"], number>
  >({
    mental_demand: 10,
    physical_demand: 10,
    temporal_demand: 10,
    performance: 10,
    effort: 10,
    frustration: 10,
  });
  const [notes, setNotes] = React.useState("");
  const [submitting, setSubmitting] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);

  // First slider receives initial focus on open (via Dialog.Content's
  // onOpenAutoFocus). Without this Radix would land focus on the Close
  // button — the first tabbable child in source order — which is hostile
  // UX for a survey modal asking the operator to answer six questions.
  const firstSliderRef = React.useRef<HTMLInputElement | null>(null);

  // Reset state every time the modal re-opens. A user who skips and then
  // somehow opens it again (e.g. via Help) shouldn't see their last
  // partial entry. React-19's react-hooks/set-state-in-effect rule
  // forbids the effect-driven reset pattern, so we use the React-blessed
  // "adjust state during render by comparing to previous prop" idiom:
  //   https://react.dev/learn/you-might-not-need-an-effect
  const [prevOpen, setPrevOpen] = React.useState(open);
  if (open !== prevOpen) {
    setPrevOpen(open);
    if (open) {
      setScores({
        mental_demand: 10,
        physical_demand: 10,
        temporal_demand: 10,
        performance: 10,
        effort: 10,
        frustration: 10,
      });
      setNotes("");
      setSubmitting(false);
      setError(null);
    }
  }

  async function submit() {
    setSubmitting(true);
    setError(null);
    try {
      await nasaTlx.submit({
        ...scores,
        notes: notes.trim() || undefined,
        recovery_run_id: recoveryRunId,
      });
      onOpenChange(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to submit");
      setSubmitting(false);
    }
  }

  function skip() {
    if (submitting) return;
    onOpenChange(false);
  }

  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-40 bg-black/40 backdrop-blur-sm" />
        <Dialog.Content
          onOpenAutoFocus={(event) => {
            // Redirect Radix's "focus first tabbable" away from the Close
            // button to the first slider — the actual primary input on the
            // survey. WCAG 2.4.3.
            if (firstSliderRef.current) {
              event.preventDefault();
              firstSliderRef.current.focus();
            }
          }}
          className="fixed left-1/2 top-1/2 z-50 max-h-[90vh] w-[min(640px,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 overflow-y-auto rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-6 shadow-xl outline-none"
        >
          <div className="flex items-start justify-between gap-4 pb-4">
            <div>
              <Dialog.Title className="text-lg font-semibold">
                Tell us how that went
              </Dialog.Title>
              <Dialog.Description className="mt-1 text-xs text-[var(--color-muted-foreground)]">
                You&apos;ve just completed your third successful recovery. Six
                quick sliders — takes a minute, helps shape the product.
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

          <div className="space-y-5">
            {AXES.map((axis, idx) => (
              <Slider
                key={axis.key}
                axis={axis}
                value={scores[axis.key]}
                onChange={(v) =>
                  setScores((prev) => ({ ...prev, [axis.key]: v }))
                }
                inputRef={idx === 0 ? firstSliderRef : undefined}
              />
            ))}

            <div className="space-y-1">
              <label
                htmlFor="tlx-notes"
                className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]"
              >
                Notes (optional)
              </label>
              <textarea
                id="tlx-notes"
                rows={3}
                value={notes}
                onChange={(e) => setNotes(e.target.value)}
                placeholder="Anything specific about this recovery you want us to know?"
                className="w-full resize-y rounded-md border border-[var(--color-border)] bg-[var(--color-background)] px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-ring)]"
              />
            </div>

            {error && (
              <div
                role="alert"
                className="rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-sm text-red-700 dark:text-red-300"
              >
                {error}
              </div>
            )}

            <div className="flex justify-end gap-2 pt-2">
              <Button
                type="button"
                variant="ghost"
                onClick={skip}
                disabled={submitting}
              >
                Skip
              </Button>
              <Button type="button" onClick={submit} disabled={submitting}>
                {submitting && <Loader2 className="h-4 w-4 animate-spin" />}
                Submit
              </Button>
            </div>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

function Slider({
  axis,
  value,
  onChange,
  inputRef,
}: {
  axis: Axis;
  value: number;
  onChange: (v: number) => void;
  inputRef?: React.Ref<HTMLInputElement>;
}) {
  const inputId = `tlx-${axis.key}`;
  return (
    <div className="space-y-2">
      <div className="flex items-baseline justify-between gap-3">
        <label
          htmlFor={inputId}
          className="text-sm font-medium text-[var(--color-foreground)]"
        >
          {axis.label}
        </label>
        <span
          aria-hidden
          className="font-mono text-xs text-[var(--color-muted-foreground)]"
        >
          {value}/20
        </span>
      </div>
      <p className="text-xs text-[var(--color-muted-foreground)]">
        {axis.prompt}
      </p>
      <input
        id={inputId}
        ref={inputRef}
        type="range"
        min={0}
        max={20}
        step={1}
        value={value}
        onChange={(e) => onChange(Number(e.target.value))}
        aria-valuemin={0}
        aria-valuemax={20}
        aria-valuenow={value}
        className="w-full accent-[var(--color-primary)]"
      />
      <div className="flex justify-between text-[10px] uppercase tracking-widest text-[var(--color-muted-foreground)]">
        <span>{axis.lowLabel}</span>
        <span>{axis.highLabel}</span>
      </div>
    </div>
  );
}
