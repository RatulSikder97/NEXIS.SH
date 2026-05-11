import { describe, it, expect, vi } from "vitest";

vi.mock("@/lib/db", () => {
  const calls: any[] = [];
  return {
    db: {
      insert: () => ({
        values: (v: any) => ({
          onConflictDoNothing: () => ({ returning: async () => { calls.push(v); return [{ id: "uuid-x", email: v.email }]; } }),
        }),
      }),
    },
    schema: { waitlist: { name: "waitlist" } },
    __calls: calls,
  };
});

import { POST } from "@/app/api/waitlist/route";

describe("POST /api/waitlist", () => {
  it("rejects missing email", async () => {
    const res = await POST(new Request("http://x/api/waitlist", { method: "POST", body: "{}" }));
    expect(res.status).toBe(400);
  });

  it("accepts a valid email and persists", async () => {
    const res = await POST(new Request("http://x/api/waitlist", {
      method: "POST",
      body: JSON.stringify({ email: "user@example.com", source: "landing" }),
    }));
    expect(res.status).toBe(201);
    const body = await res.json();
    expect(body.email).toBe("user@example.com");
  });
});
