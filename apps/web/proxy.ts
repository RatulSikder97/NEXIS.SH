// Next.js 16 Proxy — central auth gate for protected and auth-only pages.
//
// Two route classes:
//
//   PROTECTED  — require a valid session. Redirect to /sign-in if missing
//                or malformed/expired. Includes /console/*, /dashboard/*
//                (legacy redirect), /mfa (TOTP enroll calls protected API).
//
//   AUTH-ONLY  — sign-in / sign-up. If the user already has a valid
//                session they should bounce to /console — re-rendering
//                the sign-in form for someone already logged in is
//                always wrong.
//
// Public surfaces (landing, /verify, /invites/[token], /api/waitlist)
// are not in either matcher and pass through unchanged.
//
// JWT validation here is structural only — three segments + an `exp`
// that hasn't passed. Cryptographic signature checking happens in the
// console layout's /v1/me round-trip, which is the authoritative gate.
// Defense in depth: the proxy is fast and catches obvious garbage; the
// layout catches forged-but-well-formed tokens.

import { NextResponse } from "next/server";
import type { NextRequest } from "next/server";

const PROTECTED = ["/console", "/dashboard", "/mfa"];
const AUTH_ONLY = ["/sign-in", "/sign-up"];

function isProtected(path: string): boolean {
  return PROTECTED.some((p) => path === p || path.startsWith(p + "/"));
}

function isAuthOnly(path: string): boolean {
  return AUTH_ONLY.some((p) => path === p || path.startsWith(p + "/"));
}

function looksLikeValidJWT(value: string): boolean {
  const parts = value.split(".");
  if (parts.length !== 3) return false;
  try {
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
  const path = req.nextUrl.pathname;
  const session = req.cookies.get("nexis_session");
  const hasValidSession = !!session && looksLikeValidJWT(session.value);

  if (isProtected(path)) {
    if (hasValidSession) return NextResponse.next();
    const url = req.nextUrl.clone();
    url.pathname = "/sign-in";
    url.searchParams.set("next", path);
    const res = NextResponse.redirect(url);
    if (session && !looksLikeValidJWT(session.value)) {
      // Clear bogus / expired cookie so the user isn't stuck in a loop.
      res.cookies.delete("nexis_session");
    }
    return res;
  }

  if (isAuthOnly(path) && hasValidSession) {
    const url = req.nextUrl.clone();
    url.pathname = "/console";
    url.search = "";
    return NextResponse.redirect(url);
  }

  return NextResponse.next();
}

export const config = {
  // Matcher must list every path class above. The proxy function does the
  // real classification — the matcher just narrows the scope.
  matcher: [
    "/dashboard/:path*",
    "/console/:path*",
    "/mfa",
    "/mfa/:path*",
    "/sign-in",
    "/sign-up",
  ],
};
