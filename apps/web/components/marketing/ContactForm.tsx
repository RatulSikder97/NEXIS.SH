"use client";

import { useState } from "react";
import { CheckCircle2, Loader2 } from "lucide-react";

import { Button } from "@/components/ui/Button";

type Status = "idle" | "sending" | "ok" | "error";

export function ContactForm() {
  const [status, setStatus] = useState<Status>("idle");
  const [error, setError] = useState<string | null>(null);

  async function onSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    setStatus("sending");
    setError(null);

    const form = e.currentTarget;
    const data = new FormData(form);
    const payload = {
      name: String(data.get("name") ?? ""),
      email: String(data.get("email") ?? ""),
      company: String(data.get("company") ?? ""),
      message: String(data.get("message") ?? ""),
    };

    try {
      const r = await fetch("/api/contact", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify(payload),
      });
      if (!r.ok) {
        const body = (await r.json().catch(() => ({ error: r.statusText }))) as { error?: string };
        throw new Error(body.error ?? "Failed to send");
      }
      setStatus("ok");
      form.reset();
    } catch (err) {
      setStatus("error");
      setError(err instanceof Error ? err.message : "Failed to send");
    }
  }

  const inputBase =
    "w-full rounded-md border border-[var(--color-border)] bg-[var(--color-card)] px-3 py-2 text-[14px] text-[var(--color-foreground)] placeholder:text-[var(--color-muted-foreground)] focus:outline-none focus:ring-2 focus:ring-[var(--color-primary)]/40";

  return (
    <form
      onSubmit={onSubmit}
      className="space-y-4 rounded-[14px] border border-[var(--color-border)] bg-[var(--color-card)] p-6 shadow-sm"
    >
      <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
        <label className="block">
          <span className="text-[12px] font-medium text-[var(--color-foreground)]">Name</span>
          <input
            name="name"
            type="text"
            required
            placeholder="Jane Smith"
            className={`mt-1 ${inputBase}`}
          />
        </label>
        <label className="block">
          <span className="text-[12px] font-medium text-[var(--color-foreground)]">Email</span>
          <input
            name="email"
            type="email"
            required
            placeholder="jane@acme.dev"
            className={`mt-1 ${inputBase}`}
          />
        </label>
      </div>
      <label className="block">
        <span className="text-[12px] font-medium text-[var(--color-foreground)]">Company</span>
        <input
          name="company"
          type="text"
          placeholder="Acme Inc."
          className={`mt-1 ${inputBase}`}
        />
      </label>
      <label className="block">
        <span className="text-[12px] font-medium text-[var(--color-foreground)]">Message</span>
        <textarea
          name="message"
          required
          rows={6}
          placeholder="What can we help with?"
          className={`mt-1 ${inputBase} font-sans`}
        />
      </label>
      <div className="flex items-center justify-between gap-3">
        <Button type="submit" disabled={status === "sending"}>
          {status === "sending" ? (
            <>
              <Loader2 className="h-4 w-4 animate-spin" /> Sending…
            </>
          ) : (
            "Send message"
          )}
        </Button>
        {status === "ok" ? (
          <span
            role="status"
            className="inline-flex items-center gap-2 text-[13px] text-[var(--color-success)]"
          >
            <CheckCircle2 className="h-4 w-4" /> Thanks — we&apos;ll be in touch.
          </span>
        ) : null}
        {status === "error" ? (
          <span role="alert" className="text-[13px] text-[var(--color-destructive)]">
            {error ?? "Something went wrong. Please email hello@nexis.dev."}
          </span>
        ) : null}
      </div>
    </form>
  );
}
