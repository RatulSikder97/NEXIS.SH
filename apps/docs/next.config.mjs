// Phase 8 — Nextra docs sub-app.
//
// Nextra v3 wraps the Next.js config to enable MDX, the docs theme, and the
// built-in flexsearch index. The docs site is intentionally simple: it
// renders a flat set of MDX pages from `apps/docs/pages/` plus `_meta.json`
// for navigation order. No i18n, no custom search backend; Phase 9+ owns
// that.
//
// We pin Next 14 + React 18 in apps/docs/package.json so this sub-app can
// run independently of the web app's Next 16 + React 19. The pnpm
// workspace isolates node_modules per package.

import nextra from "nextra";

const withNextra = nextra({
  theme: "nextra-theme-docs",
  themeConfig: "./theme.config.tsx",
  defaultShowCopyCode: true,
  staticImage: true,
  latex: false,
});

/** @type {import("next").NextConfig} */
const config = {
  reactStrictMode: true,
  images: { unoptimized: true },
};

export default withNextra(config);
