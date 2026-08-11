// Web client SDK for the control-plane auth + multi-tenancy API.
//
// Notes:
//   - All calls run client-side (or in route handlers / server components that
//     forward the user's cookie). They use `credentials: "include"` so the
//     browser carries the `nexis_session` cookie set by the control-plane on
//     successful auth.
//   - 204 No Content is normalised to `undefined`. Non-2xx responses throw an
//     `AuthError` carrying the server-supplied `error` message and HTTP status.
//   - This module must never throw at import time — all network access is
//     deferred until a method is called.

const API =
  typeof window === "undefined"
    ? (process.env.API_URL_INTERNAL ??
      process.env.NEXT_PUBLIC_API_URL ??
      "http://localhost:8080")
    : (process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080");

export type AuthUser = { id: string; email: string };
export type AuthOrg = { id: string; name: string; slug: string };

// AuthResp matches dto.AuthResp from the control-plane: signup/login/magic
// return only ids + expiry; richer user/org data is fetched separately via
// /v1/me. The session cookie is set on these responses automatically.
export type AuthResp = {
  user_id: string;
  org_id: string;
  expires_at: string;
};

// MeResp matches dto.MeResp from the control-plane.
export type MeResp = {
  user: AuthUser;
  org: AuthOrg;
  role: "owner" | "admin" | "member";
};

export type APIKeyCreated = {
  id: string;
  prefix: string;
  name: string;
  scopes: string[];
  plaintext_once: string;
};

// SessionInfo matches dto.SessionResp from the control-plane — one row in
// the GET /v1/me/sessions response. `is_current` marks the session backing
// the request; the UI hides the revoke button for that row.
export type SessionInfo = {
  id: string;
  created_at: string;
  last_seen_at: string;
  user_agent: string;
  ip: string;
  is_current: boolean;
};

export type MFAEnrollResp = {
  qr_data_url: string;
  // secret is the raw base32 TOTP secret. It is also embedded in the
  // otpauth:// URL inside the QR PNG; we surface it explicitly so test
  // harnesses can generate a TOTP code without OCR'ing the QR. Treat it
  // as one-time use — never persist client-side.
  secret: string;
  recovery_codes: string[];
};

export class AuthError extends Error {
  status: number;
  constructor(msg: string, status: number) {
    super(msg);
    this.name = "AuthError";
    this.status = status;
  }
}

async function call<T>(path: string, init: RequestInit = {}): Promise<T> {
  const r = await fetch(`${API}${path}`, {
    ...init,
    credentials: "include",
    headers: {
      "content-type": "application/json",
      ...(init.headers ?? {}),
    },
  });
  if (r.status === 204) return undefined as T;
  const body = await r.json().catch(() => ({}) as Record<string, unknown>);
  if (!r.ok) {
    const msg =
      typeof body === "object" && body !== null && "error" in body
        ? String((body as { error: unknown }).error)
        : r.statusText;
    throw new AuthError(msg, r.status);
  }
  return body as T;
}

export const auth = {
  signup: (input: { email: string; password: string; org_name: string }) =>
    call<AuthResp>("/v1/auth/signup", {
      method: "POST",
      body: JSON.stringify(input),
    }),

  login: (input: { email: string; password: string; mfa_code?: string }) =>
    call<AuthResp>("/v1/auth/login", {
      method: "POST",
      body: JSON.stringify(input),
    }),

  logout: () => call<void>("/v1/auth/logout", { method: "POST" }),

  me: () => call<MeResp>("/v1/me"),

  magic: (input: { email: string; purpose?: string }) =>
    call<void>("/v1/auth/magic", {
      method: "POST",
      body: JSON.stringify({ purpose: "login", ...input }),
    }),

  mfaEnroll: () =>
    call<MFAEnrollResp>("/v1/auth/mfa/enroll", { method: "POST" }),

  mfaVerify: (code: string) =>
    call<void>("/v1/auth/mfa/verify", {
      method: "POST",
      body: JSON.stringify({ code }),
    }),

  mfaDisable: () => call<void>("/v1/auth/mfa", { method: "DELETE" }),

  // Phase 9 — password reset. The request endpoint always returns 202 for a
  // well-formed email (unknown addresses included) so it can't be used to
  // probe for accounts; confirm returns 204 and revokes every session.
  requestPasswordReset: (email: string) =>
    call<void>("/v1/auth/password-reset/request", {
      method: "POST",
      body: JSON.stringify({ email }),
    }),

  confirmPasswordReset: (token: string, newPassword: string) =>
    call<void>("/v1/auth/password-reset/confirm", {
      method: "POST",
      body: JSON.stringify({ token, new_password: newPassword }),
    }),

  // Phase 9 — session management.
  listSessions: () => call<SessionInfo[]>("/v1/me/sessions"),

  revokeSession: (id: string) =>
    call<void>(`/v1/me/sessions/${id}`, { method: "DELETE" }),
};
