// Phase 8 — Nextra docs _app.tsx.
//
// Nextra v3 uses the pages router and discovers its layout via the global
// _app component. This file forwards the rest of the props through so the
// Nextra theme can wrap them with its own layout shell.

import type { AppProps } from "next/app";

export default function App({ Component, pageProps }: AppProps) {
  return <Component {...pageProps} />;
}
