// Phase 8 — Nextra theme config.
//
// Minimal brand pass: NEXIS wordmark in the navbar, link out to the GitHub
// repo, simple footer. Search is on by default via Nextra's flexsearch
// (small static index — perfectly fine for the ~25-page docs set).
//
// We intentionally don't wire i18n, customised head meta, or analytics in
// Phase 8. Phase 9+ revisits those.

import type { DocsThemeConfig } from "nextra-theme-docs";

const config: DocsThemeConfig = {
  logo: <span className="font-semibold tracking-tight">NEXIS</span>,
  project: {
    link: "https://github.com/nexis-eco/nexis",
  },
  docsRepositoryBase:
    "https://github.com/nexis-eco/nexis/tree/main/apps/docs",
  footer: {
    content: (
      <span>
        MIT {new Date().getFullYear()} © NEXIS ·{" "}
        <a
          href="https://status.nexis.dev"
          target="_blank"
          rel="noreferrer"
          className="underline"
        >
          Status
        </a>
      </span>
    ),
  },
  head: (
    <>
      <meta name="viewport" content="width=device-width, initial-scale=1.0" />
      <meta
        name="description"
        content="NEXIS — autonomous incident recovery. Documentation."
      />
      <meta property="og:title" content="NEXIS docs" />
    </>
  ),
  // Nextra's flexsearch ships with the build; we just turn the placeholder
  // copy into something less generic.
  search: {
    placeholder: "Search docs…",
  },
  // Disable the "Edit on GitHub" link for non-public-repo paths until we
  // open-source. Phase 9+ flips this on when the GitHub org goes public.
  editLink: { component: null },
  feedback: { content: null },
  darkMode: true,
};

export default config;
