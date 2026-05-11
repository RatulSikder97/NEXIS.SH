"use client";

import type { ReactNode } from "react";
import { useEffect, useMemo, useRef, useState } from "react";

import { usePrefersReducedMotion } from "@/lib/usePrefersReducedMotion";

export type TerminalBlinkProps = {
  title?: string;
  lines: string[];
  intervalMs?: number;
  maxLines?: number;
};

/** `[hh:mm:ss] agent › message` → timestamp + agent + body with light syntax color */
function LogLine({ text }: { text: string }) {
  const m = text.match(/^(\[[\d:]+\])\s+([^›]+?)(\s*›\s*)(.*)$/);
  if (!m) {
    return <span className="text-text-secondary">{colorizeMessage(text)}</span>;
  }
  const [, ts, agent, sep, rest] = m;
  return (
    <span className="leading-relaxed">
      <span className="text-text-muted">{ts}</span>
      <span className="text-primary/90"> {agent.trim()} </span>
      <span className="text-text-muted">{sep}</span>
      {colorizeMessage(rest)}
    </span>
  );
}

function colorizeMessage(msg: string): ReactNode {
  if (!msg) return null;

  const re =
    /(2,847 property tests passed|0 failures|patch approved|anomaly detected|schema drift|ready —|deploying to shadow|generating candidate|traversing dependency|✓)/gi;

  const nodes: ReactNode[] = [];
  let last = 0;
  let m: RegExpExecArray | null;
  const r = new RegExp(re.source, re.flags);
  while ((m = r.exec(msg))) {
    if (m.index > last) {
      nodes.push(
        <span key={`t-${last}`} className="text-text-secondary/95">
          {msg.slice(last, m.index)}
        </span>,
      );
    }
    const hit = m[0];
    const lower = hit.toLowerCase();
    const isSuccess =
      lower.includes("passed") ||
      lower.includes("0 failures") ||
      lower.includes("approved") ||
      hit === "✓";
    const isRisk = lower.includes("anomaly") || lower.includes("schema drift");
    nodes.push(
      <span
        key={`h-${m.index}`}
        className={
          isSuccess
            ? "font-medium text-primary"
            : isRisk
              ? "font-medium text-amber"
              : "font-medium text-accent-alt"
        }
      >
        {hit}
      </span>,
    );
    last = m.index + hit.length;
  }
  if (last < msg.length) {
    nodes.push(
      <span key={`t-${last}`} className="text-text-secondary/95">
        {msg.slice(last)}
      </span>,
    );
  }

  return nodes.length ? <>{nodes}</> : <span className="text-text-secondary/95">{msg}</span>;
}

export function TerminalBlink({
  title = "terminal",
  lines,
  intervalMs = 800,
  maxLines,
}: TerminalBlinkProps) {
  const reduced = usePrefersReducedMotion();
  const effectiveMaxLines = maxLines ?? Math.max(6, lines.length);
  const hasLines = lines.length > 0;

  const scrollRef = useRef<HTMLDivElement | null>(null);
  const [logLines, setLogLines] = useState<string[]>(() =>
    hasLines ? [lines[0]] : [],
  );
  const [cursorVisible, setCursorVisible] = useState(true);
  const nextIndex = useRef<number>(hasLines ? 1 : 0);

  const cursorChar = useMemo(() => "▍", []);

  useEffect(() => {
    if (!hasLines) return;

    nextIndex.current = 1 % lines.length;

    const id = window.setInterval(() => {
      const idx = nextIndex.current;
      nextIndex.current = (nextIndex.current + 1) % lines.length;

      setLogLines((prev) => {
        const next = [...prev, lines[idx]];
        return next.length > effectiveMaxLines
          ? next.slice(next.length - effectiveMaxLines)
          : next;
      });
    }, intervalMs);

    return () => window.clearInterval(id);
  }, [effectiveMaxLines, hasLines, intervalMs, lines]);

  useEffect(() => {
    const id = window.setInterval(() => {
      setCursorVisible((v) => !v);
    }, 500);
    return () => window.clearInterval(id);
  }, []);

  useEffect(() => {
    if (!scrollRef.current) return;
    scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
  }, [logLines]);

  return (
    <div
      className={[
        "overflow-hidden rounded-[12px] border border-border bg-surface",
        "shadow-[0_0_0_1px_rgba(59,130,246,0.12),0_20px_48px_-24px_rgba(0,0,0,0.55)]",
        !reduced && "terminal-chrome-glow",
      ]
        .filter(Boolean)
        .join(" ")}
    >
      <div className="flex items-center gap-2 border-b border-border/80 bg-gradient-to-b from-surface to-background/40 px-4 py-3">
        <span className="h-3 w-3 rounded-full bg-red-signal shadow-[0_0_8px_rgba(239,68,68,0.45)]" />
        <span className="h-3 w-3 rounded-full bg-amber-signal shadow-[0_0_8px_rgba(96,165,250,0.35)]" />
        <span className="h-3 w-3 rounded-full bg-green-signal shadow-[0_0_8px_rgba(59,130,246,0.45)]" />
        <span className="ml-2 font-mono text-[11px] tracking-tight text-accent-alt">
          {title}
        </span>
      </div>

      <div
        ref={scrollRef}
        className="terminal-log-scroll max-h-[260px] overflow-y-auto bg-gradient-to-b from-background/30 via-surface to-surface px-4 py-4 font-mono text-[11px] leading-[1.65]"
      >
        {logLines.map((line, idx) => {
          const isLast = idx === logLines.length - 1;
          return (
            <div
              key={`line-${idx}`}
              className={[
                "border-l-2 border-transparent py-0.5 pl-2",
                isLast && !reduced ? "border-primary/50 bg-primary/[0.06]" : "",
              ]
                .filter(Boolean)
                .join(" ")}
            >
              <LogLine text={line} />
              {isLast ? (
                <span className={cursorVisible ? "opacity-100" : "opacity-0"}>
                  <span className="text-primary">{cursorChar}</span>
                </span>
              ) : null}
            </div>
          );
        })}
      </div>
    </div>
  );
}
