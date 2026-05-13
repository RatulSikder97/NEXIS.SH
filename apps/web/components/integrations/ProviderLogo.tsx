"use client";

// ProviderLogo — official-ish brand marks rendered as inline SVG. Inline
// keeps them theme-aware (each path's fill is controlled by tailwind classes
// or the brand-monochrome treatment) and avoids any network fetch on the
// /console/integrations grid.
//
// All glyphs sit on a 24×24 canvas. Pass `className` to control size + tint.

import * as React from "react";

type ProviderLogoProps = {
  provider:
    | "github"
    | "sentry"
    | "argocd"
    | "slack"
    | "datadog"
    | "pagerduty";
  className?: string;
};

const GitHub = (
  <svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" aria-hidden>
    <path
      fill="currentColor"
      d="M12 .5C5.65.5.5 5.65.5 12c0 5.08 3.29 9.39 7.86 10.91.58.1.79-.25.79-.56v-2.18c-3.2.7-3.87-1.36-3.87-1.36-.52-1.33-1.28-1.68-1.28-1.68-1.04-.71.08-.7.08-.7 1.15.08 1.76 1.18 1.76 1.18 1.03 1.76 2.7 1.25 3.36.96.1-.74.4-1.25.73-1.54-2.55-.29-5.24-1.28-5.24-5.69 0-1.26.45-2.29 1.18-3.1-.12-.29-.51-1.46.11-3.05 0 0 .97-.31 3.18 1.18a11.04 11.04 0 0 1 5.78 0c2.21-1.49 3.18-1.18 3.18-1.18.62 1.59.23 2.76.11 3.05.73.81 1.18 1.84 1.18 3.1 0 4.43-2.69 5.4-5.26 5.68.41.36.78 1.06.78 2.13v3.16c0 .31.21.67.8.55C20.21 21.39 23.5 17.08 23.5 12 23.5 5.65 18.35.5 12 .5z"
    />
  </svg>
);

const Sentry = (
  <svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" aria-hidden>
    <path
      fill="#362D59"
      d="M13.85 1.71a2.13 2.13 0 0 0-3.7 0L7.78 5.85a13.36 13.36 0 0 1 7.96 11.51h-2.27a11.1 11.1 0 0 0-6.82-9.55l-2.39 4.14a6.36 6.36 0 0 1 3.94 5.41H5.78a4.1 4.1 0 0 0-2.62-3.45L1.32 17.3a2.13 2.13 0 0 0 1.84 3.2h6.46c-.04-.36-.06-.73-.06-1.1A8.93 8.93 0 0 0 4.92 11.7l1.13-1.95a11.18 11.18 0 0 1 5.91 9.66c0 .37-.02.74-.05 1.1h2.27c.03-.36.05-.73.05-1.1a13.45 13.45 0 0 0-7.05-11.83l1.13-1.95a15.7 15.7 0 0 1 8.18 13.78c0 .37-.02.74-.04 1.1h3.45a2.13 2.13 0 0 0 1.84-3.2L13.85 1.7z"
    />
  </svg>
);

const ArgoCD = (
  <svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" aria-hidden>
    <path
      fill="#EF7B4D"
      d="M12 2.2c-5.4 0-9.8 4.4-9.8 9.8s4.4 9.8 9.8 9.8 9.8-4.4 9.8-9.8S17.4 2.2 12 2.2zm0 17.6c-4.3 0-7.8-3.5-7.8-7.8S7.7 4.2 12 4.2s7.8 3.5 7.8 7.8-3.5 7.8-7.8 7.8z"
    />
    <path
      fill="#EF7B4D"
      d="M12 6.4l-4.5 7.8h2.6L12 11l1.9 3.2h2.6L12 6.4zm-3 9l1 1.8h4l1-1.8H9z"
    />
  </svg>
);

const Slack = (
  <svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" aria-hidden>
    <path fill="#E01E5A" d="M5.04 14.84a2.04 2.04 0 1 1 0-4.08h2.04v2.04a2.04 2.04 0 0 1-2.04 2.04zm1.02 0a2.04 2.04 0 0 1 4.08 0v5.1a2.04 2.04 0 0 1-4.08 0v-5.1z" />
    <path fill="#36C5F0" d="M9.16 5.04a2.04 2.04 0 1 1 4.08 0v2.04H11.2a2.04 2.04 0 0 1-2.04-2.04zm0 1.02a2.04 2.04 0 0 1 0 4.08h-5.1a2.04 2.04 0 0 1 0-4.08h5.1z" />
    <path fill="#2EB67D" d="M18.96 9.16a2.04 2.04 0 1 1 0 4.08h-2.04V11.2a2.04 2.04 0 0 1 2.04-2.04zm-1.02 0a2.04 2.04 0 0 1-4.08 0v-5.1a2.04 2.04 0 0 1 4.08 0v5.1z" />
    <path fill="#ECB22E" d="M14.84 18.96a2.04 2.04 0 1 1-4.08 0v-2.04h2.04a2.04 2.04 0 0 1 2.04 2.04zm0-1.02a2.04 2.04 0 0 1 0-4.08h5.1a2.04 2.04 0 0 1 0 4.08h-5.1z" />
  </svg>
);

const Datadog = (
  <svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" aria-hidden>
    <path
      fill="#632CA6"
      d="M21.7 14.93l-1.83.04-.34-3.86-2.94 4.3 1.16.61-1.65 2.47a8.99 8.99 0 0 1-1.13-1.79l-1.74.39c.13-.69.34-1.78.34-1.78l1.4-.25-1.36-2.13 1.96-.66 1.42 1.42-.46-3.21 3.83-1.17.36 2 1.43-.55-.13 3.42-.31.74zm-9.18-3.36c-.46.34-.84.7-1.13 1.05l-2.55-1.05.13-1.43c1.13.41 2.46 1.07 3.55 1.43zm-3.46 4.27c-.4.78-.5 1.86-.35 2.74l-2.96-.43.13-2.04 3.18-.27zm5.86-9.36c.93-.27 1.9-.13 2.7.4-.86 0-1.74.27-2.5.7-.07-.4-.13-.7-.2-1.1zM4.4 18.96l1.9.27c-.04.34-.04.61 0 .9l-1.96-.27c.04-.3.04-.61.06-.9zM5.3 14.4l1.96.34c-.07.7-.07 1.43-.04 2.07l-2.04-.34c0-.69.04-1.38.13-2.07zm.7-4.27l1.9.5c-.21.65-.34 1.43-.5 2.13l-1.96-.5c.13-.78.34-1.43.56-2.13zm1.65-3.93l1.74 1.07c-.43.5-.78 1.05-1.13 1.65l-1.79-.87c.34-.7.78-1.27 1.18-1.85zm9.85-2.5l-.86 1.43c-.61-.13-1.27-.27-1.96-.27l.13-1.43c.86-.07 1.85-.07 2.7.27zm-7.79.97c.78-.4 1.65-.7 2.5-.87l-.07 1.43c-.78.13-1.5.3-2.2.54l-.23-1.1z"
    />
  </svg>
);

const PagerDuty = (
  <svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" aria-hidden>
    <path
      fill="#06AC38"
      d="M16.34 2H4v15.41h3.75v-3.13h8.59a6.14 6.14 0 0 0 0-12.28zm-.27 8.86H7.75V5.42h8.32a2.72 2.72 0 0 1 0 5.44zM4 19.13h3.75V22H4v-2.87z"
    />
  </svg>
);

const LOGOS: Record<ProviderLogoProps["provider"], React.ReactNode> = {
  github: GitHub,
  sentry: Sentry,
  argocd: ArgoCD,
  slack: Slack,
  datadog: Datadog,
  pagerduty: PagerDuty,
};

export function ProviderLogo({ provider, className }: ProviderLogoProps) {
  return <span className={className}>{LOGOS[provider]}</span>;
}
