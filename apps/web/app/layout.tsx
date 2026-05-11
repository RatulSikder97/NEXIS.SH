import type { Metadata, Viewport } from "next";
import { Inter, JetBrains_Mono } from "next/font/google";

import { MotionProvider } from "@/components/motion/MotionProvider";
import { JsonLd } from "@/components/seo/JsonLd";
import { ThemeProvider } from "@/components/ThemeProvider";
import { getSiteUrl } from "@/lib/site";

import "./globals.css";

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

export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="en" className={`${inter.variable} ${jetBrainsMono.variable} h-full antialiased`} suppressHydrationWarning>
      <body className="min-h-full flex flex-col font-sans">
        <ThemeProvider>
          <JsonLd />
          <MotionProvider>{children}</MotionProvider>
        </ThemeProvider>
      </body>
    </html>
  );
}
