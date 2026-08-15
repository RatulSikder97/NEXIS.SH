import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { deployments, type Deployment } from "@/lib/deployments";

// pollUntilTerminal is what replaced the old "await the whole build inline"
// design (apps/web/lib/deployments.ts) once POST /deploy started returning
// almost instantly with a "building" row instead of blocking for however
// long the clone+build+run+health-check actually took. These tests drive it
// with a fake clock so a multi-minute timeout budget doesn't cost real
// wall-clock time in CI.

function rowOf(
  status: Deployment["status"],
  overrides: Partial<Deployment> = {},
): Deployment {
  return {
    deployment_id: "dep-1",
    project_id: "proj-1",
    status,
    url: null,
    port: null,
    image_tag: "",
    dockerfile_source: "generated",
    detected_stack: "unknown",
    build_log: "",
    container_log: "",
    started_at: "2026-01-01T00:00:00Z",
    finished_at: "",
    ...overrides,
  };
}

function mockListResponses(sequence: Deployment[][]) {
  let call = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => {
      const rows = sequence[Math.min(call, sequence.length - 1)];
      call += 1;
      return new Response(JSON.stringify(rows), { status: 200 });
    }),
  );
}

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("deployments.pollUntilTerminal", () => {
  it("returns immediately when the row is already terminal", async () => {
    mockListResponses([[rowOf("running", { url: "http://x:1" })]]);

    const result = await deployments.pollUntilTerminal("proj-1", "dep-1", {
      intervalMs: 10,
      timeoutMs: 1000,
    });

    expect(result?.status).toBe("running");
    expect(result?.url).toBe("http://x:1");
    expect(fetch).toHaveBeenCalledTimes(1);
  });

  it("keeps polling through building and resolves on the status that follows it", async () => {
    mockListResponses([
      [rowOf("building")],
      [rowOf("building")],
      [rowOf("failed", { error: "health check timed out" })],
    ]);

    const promise = deployments.pollUntilTerminal("proj-1", "dep-1", {
      intervalMs: 10,
      timeoutMs: 10_000,
    });
    // Two intervals need to elapse for the three list() calls above to fire.
    await vi.advanceTimersByTimeAsync(10);
    await vi.advanceTimersByTimeAsync(10);
    const result = await promise;

    expect(result?.status).toBe("failed");
    expect(result?.error).toBe("health check timed out");
    expect(fetch).toHaveBeenCalledTimes(3);
  });

  it("gives up after timeoutMs and returns the last-seen building row rather than hanging", async () => {
    mockListResponses([[rowOf("building")]]);

    const promise = deployments.pollUntilTerminal("proj-1", "dep-1", {
      intervalMs: 10,
      timeoutMs: 25,
    });
    await vi.advanceTimersByTimeAsync(100);
    const result = await promise;

    expect(result?.status).toBe("building");
  });

  it("returns null if the deployment id disappears from the list rather than polling forever", async () => {
    mockListResponses([[rowOf("running", { deployment_id: "some-other-id" })]]);

    const result = await deployments.pollUntilTerminal("proj-1", "dep-1", {
      intervalMs: 10,
      timeoutMs: 1000,
    });

    expect(result).toBeNull();
  });

  it("stops issuing new requests once its AbortSignal fires", async () => {
    mockListResponses([
      [rowOf("building")],
      [rowOf("building")],
      [rowOf("building")],
    ]);
    const controller = new AbortController();

    const promise = deployments.pollUntilTerminal("proj-1", "dep-1", {
      intervalMs: 10,
      timeoutMs: 10_000,
      signal: controller.signal,
    });
    await vi.advanceTimersByTimeAsync(10); // let the first list() land
    controller.abort();
    await vi.advanceTimersByTimeAsync(10_000); // would be ~1000 more polls if not honoured
    await promise;

    // One initial call, plus at most one already-in-flight call racing the
    // abort — not the dozens more that 10 seconds of a 10ms interval would
    // otherwise produce.
    expect(
      (fetch as unknown as { mock: { calls: unknown[] } }).mock.calls.length,
    ).toBeLessThan(4);
  });
});
