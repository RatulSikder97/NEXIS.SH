// Validator sandbox surface — recent sandbox runs + queue depth + success rate.
//
// Server-side seed: GET /v1/validator/runs?limit=20. Endpoint may not exist
// yet; we degrade gracefully to an empty/coming-soon state via the client
// island so the operator still gets a coherent page.

import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import { ValidatorClient, type ValidatorRunRow } from "./client";

const API =
  process.env.API_URL_INTERNAL ??
  process.env.NEXT_PUBLIC_API_URL ??
  "http://localhost:8080";

type ValidatorListResp = {
  runs: ValidatorRunRow[];
  queue_depth?: number;
  in_flight?: number;
  success_rate_24h?: number;
  avg_duration_ms?: number;
};

export default async function ValidatorPage() {
  const c = await cookies();
  const session = c.get("nexis_session");
  if (!session) redirect("/sign-in");
  const cookieHeader = `nexis_session=${session.value}`;

  let initial: ValidatorListResp | null = null;
  try {
    const r = await fetch(`${API}/v1/validator/runs?limit=20`, {
      headers: { cookie: cookieHeader },
      cache: "no-store",
    });
    if (r.ok) {
      initial = (await r.json()) as ValidatorListResp;
    }
  } catch {
    // ignore — client will render the "endpoint coming soon" state
  }

  return <ValidatorClient initial={initial} />;
}
