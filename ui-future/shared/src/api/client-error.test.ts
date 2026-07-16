import { api, ApiError, apiErrorFromBody } from "./client";

describe("apiErrorFromBody preserves backend detail", () => {
  it("uses code/message/details from a JSON error envelope", () => {
    const e = apiErrorFromBody(
      409,
      "Conflict",
      JSON.stringify({ code: "ALREADY_EXISTS", message: "network exists", details: [{ x: 1 }] }),
    );
    expect(e).toBeInstanceOf(ApiError);
    expect(e.status).toBe(409);
    expect(e.code).toBe("ALREADY_EXISTS");
    expect(e.message).toBe("network exists");
    expect(e.details).toEqual([{ x: 1 }]);
  });

  it("does NOT discard a non-JSON error body (regression: bare catch swallowed it)", () => {
    const body = "upstream connect error or disconnect/reset before headers";
    const e = apiErrorFromBody(502, "Bad Gateway", body);
    expect(e.message).toContain("upstream connect error");
    expect(e.details).toBe(body);
    expect(e.status).toBe(502);
  });

  it("falls back to statusText when the body is empty", () => {
    const e = apiErrorFromBody(500, "Internal Server Error", "");
    expect(e.message).toBe("Internal Server Error");
    expect(e.code).toBe("500");
  });

  it("truncates a very large non-JSON body to bound memory", () => {
    const e = apiErrorFromBody(500, "Internal Server Error", "x".repeat(10_000));
    expect(typeof e.details).toBe("string");
    expect((e.details as string).length).toBeLessThanOrEqual(2048);
  });
});

describe("api client preserves non-JSON error bodies", () => {
  const realFetch = globalThis.fetch;
  afterEach(() => {
    globalThis.fetch = realFetch;
  });

  function mockFetch(status: number, statusText: string, body: string) {
    // jsdom has no global Response; a minimal Response-like object is enough
    // for fetchJson (it only calls res.ok / status / statusText / text()).
    globalThis.fetch = (() =>
      Promise.resolve({
        ok: status >= 200 && status < 300,
        status,
        statusText,
        text: () => Promise.resolve(body),
      })) as unknown as typeof fetch;
  }

  it("surfaces a plaintext gateway 5xx body instead of swallowing it", async () => {
    mockFetch(502, "Bad Gateway", "upstream connect error or disconnect/reset before headers");
    await expect(api.get("/vpc/v1/networks/x")).rejects.toMatchObject({
      status: 502,
      details: "upstream connect error or disconnect/reset before headers",
    });
  });

  it("unwraps a JSON error envelope as before", async () => {
    mockFetch(404, "Not Found", JSON.stringify({ code: "NOT_FOUND", message: "Network x not found" }));
    const err = (await api.get("/vpc/v1/networks/x").catch((e) => e)) as ApiError;
    expect(err).toBeInstanceOf(ApiError);
    expect(err.code).toBe("NOT_FOUND");
    expect(err.message).toBe("Network x not found");
  });
});
