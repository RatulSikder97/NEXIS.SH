/** Canonical site origin for metadata, sitemap, and JSON-LD. */
export function getSiteUrl() {
  const raw = process.env.NEXT_PUBLIC_SITE_URL?.trim();
  if (raw) return raw.replace(/\/$/, "");
  return "https://nexis.sh";
}
