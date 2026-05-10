import type { Metadata, Viewport } from "next";
import { Inter, JetBrains_Mono } from "next/font/google";

import { MotionProvider } from "@/components/motion/MotionProvider";
import { JsonLd } from "@/components/seo/JsonLd";
import { getSiteUrl } from "@/lib/site";

import "./globals.css";

const inter = Inter({
  subsets: ["latin"],
  weight: ["400", "500"],
  variable: "--font-inter",
  display: "swap",
});

const jetBrainsMono = JetBrains_Mono({
  subsets: ["latin"],
  weight: ["400"],
  variable: "--font-jetbrains-mono",
  display: "swap",
});

const siteUrl = getSiteUrl();

const defaultDescription =
  "Autonomous engineering for CI/CD: Nexis detects pipeline failures, traces root cause, validates patches in isolation, and queues fixes for approval—built for platform and SRE teams.";

export const viewport: Viewport = {
  themeColor: "#0a1529",
};

export const metadata: Metadata = {
  metadataBase: new URL(siteUrl),
  title: {
    default: "Nexis — Autonomous engineering for CI/CD & production pipelines",
    template: "%s | Nexis",
  },
  applicationName: "Nexis",
  description: defaultDescription,
  keywords: [
    "Nexis",
    "autonomous engineering",
    "CI/CD",
    "pipeline remediation",
    "incident response",
    "self-healing infrastructure",
    "SRE",
    "platform engineering",
    "automated patching",
    "MTTR",
    "root cause analysis",
    "shadow deployment",
  ],
  authors: [{ name: "Nexis" }],
  creator: "Nexis",
  alternates: {
    canonical: "/",
  },
  robots: {
    index: true,
    follow: true,
    googleBot: {
      index: true,
      follow: true,
      "max-video-preview": -1,
      "max-image-preview": "large",
      "max-snippet": -1,
    },
  },
  openGraph: {
    type: "website",
    locale: "en_US",
    url: siteUrl,
    siteName: "Nexis",
    title: "Nexis — Autonomous engineering for broken pipelines",
    description:
      "Multi-agent fault recovery: detect anomalies, trace dependency graphs, validate patches in isolation, and ship with engineer-in-the-loop approval.",
    images: [
      {
        url: "/logo.svg",
        width: 540,
        height: 140,
        alt: "Nexis — nexis.sh autonomous engineering platform",
      },
    ],
  },
  twitter: {
    card: "summary_large_image",
    title: "Nexis — Autonomous engineering for broken pipelines",
    description: defaultDescription,
    images: ["/logo.svg"],
  },
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html
      lang="en"
      className={`${inter.variable} ${jetBrainsMono.variable} h-full antialiased`}
    >
      <body className="min-h-full flex flex-col bg-background text-text-primary font-sans">
        <JsonLd />
        <MotionProvider>{children}</MotionProvider>
      </body>
    </html>
  );
}
