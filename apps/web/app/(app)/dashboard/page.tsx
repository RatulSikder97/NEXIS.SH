import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import type { MeResp } from "@/lib/auth";

const API = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

export default async function DashboardPage() {
  const cookieStore = await cookies();
  const session = cookieStore.get("nexis_session");
  if (!session) redirect("/sign-in");

  const r = await fetch(`${API}/v1/me`, {
    headers: { cookie: `nexis_session=${session.value}` },
    cache: "no-store",
  });
  if (!r.ok) redirect("/sign-in");
  const me = (await r.json()) as MeResp;

  return (
    <div className="space-y-6">
      <div>
        <p className="text-sm uppercase tracking-widest text-[var(--color-muted-foreground)]">
          DASHBOARD
        </p>
        <h1 className="text-3xl font-semibold mt-1 text-[var(--color-foreground)]">
          Welcome, {me.user.email}
        </h1>
      </div>
      <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-6">
        <h2 className="font-medium mb-3 text-[var(--color-foreground)]">
          Organization
        </h2>
        <dl className="grid grid-cols-3 gap-4 text-sm">
          <div>
            <dt className="text-[var(--color-muted-foreground)]">Name</dt>
            <dd className="font-medium text-[var(--color-foreground)]">
              {me.org.name}
            </dd>
          </div>
          <div>
            <dt className="text-[var(--color-muted-foreground)]">Slug</dt>
            <dd className="font-medium font-mono text-[var(--color-foreground)]">
              {me.org.slug}
            </dd>
          </div>
          <div>
            <dt className="text-[var(--color-muted-foreground)]">Role</dt>
            <dd className="font-medium uppercase text-[var(--color-foreground)]">
              {me.role}
            </dd>
          </div>
        </dl>
      </div>
      <span className="inline-flex items-center gap-2 rounded-full bg-[var(--color-success)]/15 text-[var(--color-success)] px-3 py-1 text-xs font-medium">
        ✓ Phase 2 complete
      </span>
    </div>
  );
}
