// /api/system-status — thin server-side proxy that forwards the public
// /status board's poll request to the control-plane's /v1/system-status
// endpoint. Lets the client live in the browser without needing a CORS
// preflight against the control-plane host.
import { NextResponse } from "next/server";

const INTERNAL_API =
  process.env.API_URL_INTERNAL ??
  process.env.NEXT_PUBLIC_API_URL ??
  "http://localhost:8080";

export async function GET() {
  try {
    const r = await fetch(`${INTERNAL_API}/v1/system-status`, {
      cache: "no-store",
    });
    if (!r.ok) {
      return NextResponse.json(
        { error: "control plane unreachable" },
        { status: 502 },
      );
    }
    const body = await r.json();
    return NextResponse.json(body);
  } catch {
    return NextResponse.json(
      { error: "control plane unreachable" },
      { status: 502 },
    );
  }
}
