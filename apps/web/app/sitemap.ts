import type { MetadataRoute } from "next";

import { getSiteUrl } from "@/lib/site";

const ROUTES = [
  "",
  "/product",
  "/agents",
  "/integrations",
  "/pricing",
  "/docs",
  "/changelog",
  "/status",
  "/about",
  "/careers",
  "/contact",
  "/privacy",
  "/terms",
  "/security",
];

export default function sitemap(): MetadataRoute.Sitemap {
  const base = getSiteUrl();
  const lastModified = new Date();
  return ROUTES.map((path) => ({
    url: `${base}${path}`,
    lastModified,
    changeFrequency: path === "" ? "weekly" : "monthly",
    priority: path === "" ? 1 : 0.7,
  }));
}
