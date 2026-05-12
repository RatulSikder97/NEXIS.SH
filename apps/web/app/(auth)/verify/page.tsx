import type { Route } from "next";
import { redirect } from "next/navigation";

const API = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

// Magic-link verify page.
//
// The control-plane endpoint at /v1/auth/verify is the source of truth: it
// validates the token, sets the nexis_session cookie on its 302 response, and
// then redirects the browser to APP_BASE_URL+"/dashboard". We can't proxy that
// cookie from a server component (the Set-Cookie would target the API origin),
// so we redirect the browser straight to the control-plane and let it set the
// cookie on the same domain it serves.
export default async function VerifyPage({
  searchParams,
}: {
  searchParams: Promise<{ token?: string }>;
}) {
  const { token } = await searchParams;
  if (!token) redirect("/sign-in");
  // External URL — typedRoutes only knows about app routes, so cast through
  // Route. The browser handles the cross-origin 302 + cookie set on the
  // control-plane response.
  redirect(
    `${API}/v1/auth/verify?token=${encodeURIComponent(token)}` as Route,
  );
}
