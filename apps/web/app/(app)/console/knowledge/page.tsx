// Knowledge base surface — pgvector index status per workspace.
//
// Server side: try /v1/knowledge/status (may 404). Degrade to empty/coming
// soon if not available.

import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import { KnowledgeClient, type KnowledgeWorkspace } from "./client";

const API =
  process.env.API_URL_INTERNAL ??
  process.env.NEXT_PUBLIC_API_URL ??
  "http://localhost:8080";

type KnowledgeResp = {
  workspaces: KnowledgeWorkspace[];
  total_chunks?: number;
};

export default async function KnowledgePage() {
  const c = await cookies();
  const session = c.get("nexis_session");
  if (!session) redirect("/sign-in");
  const cookieHeader = `nexis_session=${session.value}`;

  let initial: KnowledgeResp | null = null;
  try {
    const r = await fetch(`${API}/v1/knowledge/status`, {
      headers: { cookie: cookieHeader },
      cache: "no-store",
    });
    if (r.ok) {
      initial = (await r.json()) as KnowledgeResp;
    }
  } catch {
    // ignore
  }

  return <KnowledgeClient initial={initial} />;
}
