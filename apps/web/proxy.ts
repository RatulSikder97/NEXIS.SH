// Next.js 16 Proxy (formerly Middleware) — gates /dashboard/* on the
// presence of the nexis_session cookie. If the cookie is missing the request
// is redirected to /sign-in with a ?next= parameter so we can bounce the user
// back after login.
//
// The control-plane is the source of truth for session validity. Here we only
// check that *some* cookie is present; the dashboard server component does a
// real /v1/me round-trip and redirects to /sign-in if the cookie is rejected.
import { NextResponse } from "next/server";
import type { NextRequest } from "next/server";

export function proxy(req: NextRequest) {
  const session = req.cookies.get("nexis_session");
  const path = req.nextUrl.pathname;
  if (!session) {
    const url = req.nextUrl.clone();
    url.pathname = "/sign-in";
    url.searchParams.set("next", path);
    return NextResponse.redirect(url);
  }
  return NextResponse.next();
}

export const config = {
  matcher: ["/dashboard/:path*"],
};
