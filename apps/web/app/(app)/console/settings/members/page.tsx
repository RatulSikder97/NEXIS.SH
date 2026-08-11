// Phase 3 Stage 8 + Phase 9 — Settings/Members & Roles.
//
// Server component shell: loads /v1/me, then (for owner|admin) the pending
// invites, the real member list, and the intelligent role recommendations in
// parallel. The members + role-recommendations endpoints are Phase 9 — when
// either fetch fails (route not mounted yet, or 403) we degrade to null and
// the client hides that surface rather than blocking the whole page.

import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import { MembersClient } from "./client";
import type { MeResp } from "@/lib/auth";
import type { PendingInvite } from "@/lib/invites";
import type { OrgMember, RoleRecommendation } from "@/lib/roleRecommendations";

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
  let members: OrgMember[] | null = null;
  let recommendations: RoleRecommendation[] | null = null;
  let canManage = me.role === "owner" || me.role === "admin";
  if (canManage) {
    const opts = {
      headers: { cookie: cookieHeader },
      cache: "no-store",
    } as const;
    const [inviteR, membersR, recsR] = await Promise.all([
      fetch(`${API}/v1/orgs/${me.org.id}/invites`, opts),
      fetch(`${API}/v1/orgs/${me.org.id}/members`, opts),
      fetch(`${API}/v1/orgs/${me.org.id}/role-recommendations`, opts),
    ]);
    if (inviteR.ok) {
      pending = (await inviteR.json()) as PendingInvite[];
    } else if (inviteR.status === 403) {
      canManage = false;
    }
    // Phase 9 surfaces — soft-fail to null (hidden) when unavailable.
    if (membersR.ok) {
      members = (await membersR.json()) as OrgMember[];
    }
    if (recsR.ok) {
      recommendations = (await recsR.json()) as RoleRecommendation[];
    }
  }

  return (
    <MembersClient
      me={me}
      pending={pending}
      members={members}
      recommendations={recommendations}
      canIssue={me.role === "owner"}
      canManage={canManage}
    />
  );
}
