// Next.js 16 Proxy — gates /dashboard/* (legacy) and /console/* on the
// nexis_session cookie. Two cheap checks before any server work runs:
//
// 1. Cookie present. No cookie → redirect to /sign-in.
// 2. Cookie *looks* like a JWT (three base64 segments separated by dots)
//    and isn't already past its `exp`. A garbage value like
//    `nexis_session=foo` is rejected without a round-trip.
//
// Cryptographic validation (signature) happens in the console layout's
// /v1/me call — that's the authoritative gate. The proxy is a fast
// rejection path so we don't render the shell for obvious garbage.
import { NextResponse } from "next/server";
import type { NextRequest } from "next/server";

function looksLikeValidJWT(value: string): boolean {
  const parts = value.split(".");
  if (parts.length !== 3) return false;
  try {
    // payload is base64url; pad and decode just enough to read exp.
    const padded = parts[1].replace(/-/g, "+").replace(/_/g, "/");
    const json = atob(padded + "===".slice(0, (4 - (padded.length % 4)) % 4));
    const claims = JSON.parse(json) as { exp?: number };
    if (typeof claims.exp === "number" && claims.exp * 1000 < Date.now()) {
      return false;
    }
    return true;
  } catch {
    return false;
  }
}

export function proxy(req: NextRequest) {
  const session = req.cookies.get("nexis_session");
  const path = req.nextUrl.pathname;

  if (!session || !looksLikeValidJWT(session.value)) {
    const url = req.nextUrl.clone();
    url.pathname = "/sign-in";
    url.searchParams.set("next", path);
    const res = NextResponse.redirect(url);
    if (session && !looksLikeValidJWT(session.value)) {
      // Clear the bogus cookie so the user isn't stuck in a loop.
      res.cookies.delete("nexis_session");
    }
    return res;
  }
  return NextResponse.next();
}

export const config = {
  matcher: ["/dashboard/:path*", "/console/:path*"],
};
