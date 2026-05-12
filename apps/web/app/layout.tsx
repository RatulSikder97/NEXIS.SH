import type { Metadata, Viewport } from "next";
import { Inter, JetBrains_Mono } from "next/font/google";
import { cookies } from "next/headers";

import { MotionProvider } from "@/components/motion/MotionProvider";
import { JsonLd } from "@/components/seo/JsonLd";
import { ThemeProvider } from "@/components/ThemeProvider";
import { getSiteUrl } from "@/lib/site";

import "./globals.css";

// Phase 3 Stage 8 — SSR theme hydration.
//
// When an authenticated user lands on any page we ask the control-plane for
// their stored theme preference. The value is forwarded into next-themes via
// defaultTheme + a matching `class` on <html> so the first paint already
// matches their choice — without this the page flashes in the light palette
// before next-themes' useEffect catches up.
//
// Unauthenticated visitors (and any fetch failure) fall through to "light",
// which is the existing default. We intentionally do not look at the
// prefers-color-scheme media query server-side: it's only available in the
// browser and any guess would still produce a flash.
const API_URL =
  process.env.API_URL_INTERNAL ??
  process.env.NEXT_PUBLIC_API_URL ??
  "http://localhost:8080";

async function loadServerTheme(): Promise<"light" | "dark" | "system"> {
  try {
    const c = await cookies();
    const session = c.get("nexis_session");
    if (!session) return "light";
    const r = await fetch(`${API_URL}/v1/me/preferences`, {
      headers: { cookie: `nexis_session=${session.value}` },
      cache: "no-store",
    });
    if (!r.ok) return "light";
    const p = (await r.json()) as Record<string, unknown>;
    const t = p["theme"];
    if (t === "light" || t === "dark" || t === "system") return t;
    return "light";
  } catch {
    return "light";
  }
}

const inter = Inter({
  subsets: ["latin"],
  weight: ["400", "500", "600", "700"],
  variable: "--font-inter",
  display: "swap",
});

const jetBrainsMono = JetBrains_Mono({
  subsets: ["latin"],
  weight: ["400", "500"],
  variable: "--font-jetbrains-mono",
  display: "swap",
});

const siteUrl = getSiteUrl();
const defaultDescription =
  "Nine AI agents. One engineering team that ships fixes while you sleep. NEXIS detects pipeline failures, traces root cause, validates patches in isolation, and routes fixes through engineer approval.";

export const viewport: Viewport = {
  themeColor: [
    { media: "(prefers-color-scheme: light)", color: "#ffffff" },
    { media: "(prefers-color-scheme: dark)",  color: "#0A0E1A" },
  ],
};

export const metadata: Metadata = {
  metadataBase: new URL(siteUrl),
  title: { default: "NEXIS — Autonomous engineering, supervised by you.", template: "%s | NEXIS" },
  applicationName: "NEXIS",
  description: defaultDescription,
  alternates: { canonical: "/" },
  robots: { index: true, follow: true },
  openGraph: {
    type: "website", locale: "en_US", url: siteUrl, siteName: "NEXIS",
    title: "NEXIS — Autonomous engineering, supervised by you.",
    description: defaultDescription,
    images: [{ url: "/logo-light.svg", width: 540, height: 140, alt: "NEXIS" }],
  },
  twitter: { card: "summary_large_image", title: "NEXIS", description: defaultDescription, images: ["/logo-light.svg"] },
};

export default async function RootLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  const theme = await loadServerTheme();
  // For "system" we don't apply a class server-side — next-themes adds the
  // correct one on hydration based on the prefers-color-scheme media query.
  const htmlClass = `${inter.variable} ${jetBrainsMono.variable} h-full antialiased${
    theme === "dark" ? " dark" : ""
  }`;
  return (
    <html lang="en" className={htmlClass} suppressHydrationWarning>
      <body className="min-h-full flex flex-col font-sans">
        <ThemeProvider defaultTheme={theme}>
          <JsonLd />
          <MotionProvider>{children}</MotionProvider>
        </ThemeProvider>
      </body>
    </html>
  );
}
