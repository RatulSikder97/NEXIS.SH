// Phase 3 Stage 8 — Settings/Members & Roles.
//
// Server component shell: loads /v1/me + /v1/orgs/{id}/invites in parallel.
// The current Phase 3 control-plane does not yet expose a members list
// endpoint — GET /v1/orgs/{id}/members lands in Phase 4. For now we show
// the caller as the sole "you" row plus the pending-invites table.
//
// If the invites fetch returns 403 (caller is a plain member, not owner|admin)
// we degrade to a hidden invites section rather than blocking the whole page.

import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import { MembersClient } from "./client";
import type { MeResp } from "@/lib/auth";
import type { PendingInvite } from "@/lib/invites";

const API =
  process.env.API_URL_INTERNAL ??
  process.env.NEXT_PUBLIC_API_URL ??
  "http://localhost:8080";

export default async function MembersPage() {
  const c = await cookies();
  const session = c.get("nexis_session");
  if (!session) redirect("/sign-in");

  const cookieHeader = `nexis_session=${session.value}`;
  const meR = await fetch(`${API}/v1/me`, {
    headers: { cookie: cookieHeader },
    cache: "no-store",
  });
  if (!meR.ok) redirect("/sign-in");
  const me = (await meR.json()) as MeResp;

  // Only owners + admins can list invites. Members get an empty list
  // through a soft 403 — pass an empty array down.
  let pending: PendingInvite[] = [];
  let canManage = me.role === "owner" || me.role === "admin";
  if (canManage) {
    const inviteR = await fetch(
      `${API}/v1/orgs/${me.org.id}/invites`,
      {
        headers: { cookie: cookieHeader },
        cache: "no-store",
      },
    );
    if (inviteR.ok) {
      pending = (await inviteR.json()) as PendingInvite[];
    } else if (inviteR.status === 403) {
      canManage = false;
    }
  }

  return (
    <MembersClient
      me={me}
      pending={pending}
      canIssue={me.role === "owner"}
      canManage={canManage}
    />
  );
}
