// End-to-end tests for the auth surface. These are designed to run against
// a live stack (`docker compose up -d`) — they perform real signups, real
// cookie-based session, and real RLS-backed API key lifecycles.
//
// Each test mints a unique email so re-running against the same DB doesn't
// collide with rows from a previous run.
import { test, expect } from "@playwright/test";
import { generateSync } from "otplib";

const API_URL = process.env.E2E_API_URL ?? "http://localhost:8080";
const password = "correct-horse-battery-staple";

test("signup → MFA enroll → MFA verify → logout → login with MFA → dashboard", async ({ page }) => {
  const email = `e2e+${Date.now()}@example.com`;

  // --- Signup ---
  await page.goto("/sign-up");
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password").fill(password);
  await page.getByLabel("Org name").fill(`E2E ${Date.now()}`);
  await page.getByRole("button", { name: /sign up/i }).click();
  await expect(page).toHaveURL(/\/dashboard$/);
  await expect(page.getByText(/welcome/i)).toBeVisible();

  // --- MFA enroll (via control-plane directly so we can grab the secret) ---
  // The session cookie was set on the apex of the API origin; Playwright's
  // request context shares the page's cookie jar, so this call is
  // authenticated with the same session.
  const enrollResp = await page.request.post(`${API_URL}/v1/auth/mfa/enroll`, {
    headers: { "content-type": "application/json" },
  });
  expect(enrollResp.status()).toBe(200);
  const enrollBody = await enrollResp.json();
  expect(enrollBody.qr_data_url).toMatch(/^data:image\/png;base64,/);
  expect(typeof enrollBody.secret).toBe("string");
  expect(enrollBody.secret.length).toBeGreaterThan(0);
  const secret = enrollBody.secret as string;

  // --- MFA verify (commits the enrollment) ---
  const verifyResp = await page.request.post(`${API_URL}/v1/auth/mfa/verify`, {
    headers: { "content-type": "application/json" },
    data: { code: generateSync({ secret, strategy: "totp" }) },
  });
  expect(verifyResp.status()).toBe(204);

  // --- API key create/list (covers protected-route + RLS + audit) ---
  const createResp = await page.request.post(`${API_URL}/v1/apikeys`, {
    headers: { "content-type": "application/json" },
    data: { name: "e2e", scopes: ["read"] },
  });
  expect(createResp.status()).toBe(201);
  const keyBody = await createResp.json();
  expect(keyBody.plaintext_once).toMatch(/^nx_live_/);

  const listResp = await page.request.get(`${API_URL}/v1/apikeys`);
  expect(listResp.status()).toBe(200);
  const list = await listResp.json();
  expect(Array.isArray(list)).toBe(true);
  expect(list.length).toBeGreaterThanOrEqual(1);

  // --- Logout ---
  await page.goto("/dashboard");
  await page.getByRole("button", { name: /log out/i }).click();
  await expect(page).toHaveURL(/\/sign-in/);

  // --- Login (MFA required since we just enrolled) ---
  // The sign-in form starts without the MFA field; the first submit returns
  // a 400 with "mfa required" which the page handles by revealing the field.
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password").fill(password);
  await page.getByRole("button", { name: /sign in/i }).click();
  // After the first submit the MFA field becomes visible.
  const mfaField = page.getByLabel(/mfa code/i);
  await expect(mfaField).toBeVisible();
  await mfaField.fill(generateSync({ secret, strategy: "totp" }));
  await page.getByRole("button", { name: /sign in/i }).click();
  await expect(page).toHaveURL(/\/dashboard$/);
});

test("dashboard requires session", async ({ page }) => {
  await page.context().clearCookies();
  await page.goto("/dashboard");
  await expect(page).toHaveURL(/\/sign-in(\?next=.*)?$/);
});
