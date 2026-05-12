// Phase 3 Stage 8 — Settings/Organization.
//
// Read-only org card: name + slug + caller's role. The control-plane has no
// org-rename endpoint yet (Phase 4 will add /v1/orgs/{id} PATCH); for Phase
// 3 we surface the values verbatim from /v1/me.

import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import type { MeResp } from "@/lib/auth";

const API =
  process.env.API_URL_INTERNAL ??
  process.env.NEXT_PUBLIC_API_URL ??
  "http://localhost:8080";

function Field({
  label,
  value,
  mono,
}: {
  label: string;
  value: string;
  mono?: boolean;
}) {
  return (
    <div>
      <p className="text-xs uppercase tracking-widest text-[var(--color-muted-foreground)]">
        {label}
      </p>
      <p className={"mt-1 text-sm " + (mono ? "font-mono" : "")}>{value}</p>
    </div>
  );
}

export default async function OrganizationPage() {
  const c = await cookies();
  const session = c.get("nexis_session");
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
        <h1 className="text-2xl font-semibold">Organization</h1>
        <p className="mt-1 text-sm text-[var(--color-muted-foreground)]">
          The tenant your account belongs to. Renaming + transfers coming soon.
        </p>
      </div>
      <div className="space-y-4 rounded-lg border border-[var(--color-border)] bg-[var(--color-card)] p-6">
        <Field label="Name" value={me.org.name} />
        <Field label="Slug" value={me.org.slug} mono />
        <Field label="Organization ID" value={me.org.id} mono />
        <Field label="Your role" value={me.role} />
      </div>
    </div>
  );
}
