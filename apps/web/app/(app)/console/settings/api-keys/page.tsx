// Phase 3 Stage 8 — Settings/API Keys.
//
// Server component shell: fetches the current list of keys then hands off to
// the APIKeysClient client component. Creation + revocation happen client-
// side (the plaintext on create needs to be surfaced in a dialog before being
// discarded, which is awkward in a pure server component).

import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import { APIKeysClient } from "./client";
import type { APIKey } from "@/lib/apikeys";

const API =
  process.env.API_URL_INTERNAL ??
  process.env.NEXT_PUBLIC_API_URL ??
  "http://localhost:8080";

export default async function APIKeysPage() {
  const c = await cookies();
  const session = c.get("nexis_session");
  if (!session) redirect("/sign-in");
  const r = await fetch(`${API}/v1/apikeys`, {
    headers: { cookie: `nexis_session=${session.value}` },
    cache: "no-store",
  });
  if (!r.ok && r.status !== 401) {
    // 401 → fall through to /sign-in below; other failures degrade to empty.
  }
  if (r.status === 401) redirect("/sign-in");
  const initial: APIKey[] = r.ok ? ((await r.json()) as APIKey[]) : [];

  return <APIKeysClient initial={initial} />;
}
