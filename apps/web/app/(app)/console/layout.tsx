// Phase 3 — Console shell layout (server component).
//
// Authoritative auth gate: every page under /console/* must pass through
// here. The proxy.ts middleware is a cheap cookie-presence check that
// keeps unauthenticated traffic off our servers, but it can't validate
// the JWT. This layout calls /v1/me with the cookie; an invalid / expired
// / revoked session yields a redirect to /sign-in regardless of what the
// individual page does.
//
// Two-column layout: fixed Sidebar (240px) + main column with sticky
// Topbar. The main gutter is a static `ml-60` because the Sidebar
// persists its collapsed/expanded state client-side and we accept the
// fixed gutter to avoid hydration flicker — same compromise as Stage 6.

import { cookies } from "next/headers";
import { redirect } from "next/navigation";
import * as React from "react";

import { Sidebar } from "@/components/console/Sidebar";
import { Topbar } from "@/components/console/Topbar";

const API =
  process.env.API_URL_INTERNAL ??
  process.env.NEXT_PUBLIC_API_URL ??
  "http://localhost:8080";

export default async function ConsoleLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const c = await cookies();
  const session = c.get("nexis_session");
  if (!session) redirect("/sign-in");

  const res = await fetch(`${API}/v1/me`, {
    headers: { cookie: `nexis_session=${session.value}` },
    cache: "no-store",
  });
  if (!res.ok) redirect("/sign-in");
  const me = (await res.json()) as { user: { email: string } };

  return (
    <div className="min-h-screen bg-[var(--color-background)] text-[var(--color-foreground)]">
      <Sidebar />
      <div className="ml-60">
        <Topbar userEmail={me.user.email} />
        <main className="mx-auto max-w-[1440px] px-6 py-6">{children}</main>
      </div>
    </div>
  );
}
