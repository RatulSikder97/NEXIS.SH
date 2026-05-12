// Phase 3 Stage 10 — Invite claim landing.
//
// Lives outside the `(auth)` route group so the proxy's auth matcher
// (`/console/:path*`, `/dashboard/:path*`) leaves it alone — the user has
// no session yet when they land here.
//
// The page itself is a thin server component that pulls the token out of
// the URL and forwards it to the InviteClaimClient client component, which
// runs the fetch + form.

import { InviteClaimClient } from "./client";

export default async function InviteClaimPage({
  params,
}: {
  // Next 16 hands route params as a Promise; we await it before reading
  // .token rather than destructuring inline (the typecheck would otherwise
  // require the .then chain).
  params: Promise<{ token: string }>;
}) {
  const { token } = await params;
  return <InviteClaimClient token={token} />;
}
