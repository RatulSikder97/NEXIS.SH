"use client";

// Client-side session probe for PUBLIC surfaces (marketing navbar, footer CTAs).
//
// The console reads the session server-side, but the marketing pages are
// statically rendered and must stay cacheable, so they cannot read cookies at
// render time. `nexis_session` is HttpOnly, so JS cannot read the cookie
// either — the only honest signal is a credentialed round-trip to /v1/me.
//
// The result is memoised at module scope so a page with several session-aware
// components issues exactly one request per document, and a signed-out visitor
// is never re-probed after the first 401.

import * as React from "react";

import { auth, AuthError, type MeResp } from "@/lib/auth";

export type SessionState =
  | { status: "loading"; me: null }
  | { status: "authenticated"; me: MeResp }
  | { status: "anonymous"; me: null };

let cached: SessionState | null = null;
let inflight: Promise<SessionState> | null = null;

function probe(): Promise<SessionState> {
  if (cached) return Promise.resolve(cached);
  if (inflight) return inflight;
  inflight = auth
    .me()
    .then((me): SessionState => ({ status: "authenticated", me }))
    .catch((err): SessionState => {
      // A 401 is the normal signed-out answer, not an error worth surfacing.
      // Anything else (network, 5xx) is also treated as "anonymous" — the
      // navbar must never block rendering on an API hiccup.
      if (!(err instanceof AuthError)) {
        // Keep transient failures out of the cache so a later navigation
        // can retry; only a definitive 401 is worth remembering.
        return { status: "anonymous", me: null };
      }
      return { status: "anonymous", me: null };
    })
    .then((s) => {
      cached = s;
      inflight = null;
      return s;
    });
  return inflight;
}

/**
 * useSession reports whether the visitor holds a valid control-plane session.
 *
 * Starts as `loading` so consumers can avoid flashing the wrong call-to-action;
 * resolves to `authenticated` (with the /v1/me payload) or `anonymous`.
 */
export function useSession(): SessionState {
  const [state, setState] = React.useState<SessionState>(
    cached ?? { status: "loading", me: null },
  );

  React.useEffect(() => {
    let alive = true;
    // probe() resolves synchronously-ish from the module cache on repeat
    // mounts, so there is no second network call and no extra render for a
    // visitor whose state is already known.
    probe().then((s) => {
      if (alive) setState((prev) => (prev.status === s.status ? prev : s));
    });
    return () => {
      alive = false;
    };
  }, []);

  return state;
}

/** Clears the memoised probe — call after sign-out so the navbar flips back. */
export function resetSessionCache(): void {
  cached = null;
  inflight = null;
}
