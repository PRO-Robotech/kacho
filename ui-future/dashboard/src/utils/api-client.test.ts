import { jest } from "@jest/globals";

const redirectToLogin = jest.fn();
jest.unstable_mockModule("./auth", () => ({
  redirectToLogin,
  loginUrl: () => "/login",
}));

const { apiGet } = await import("./api-client");

const jsonResponse = (body: unknown) => {
  return Promise.resolve({
    ok: true,
    text: () => Promise.resolve(JSON.stringify(body)),
    statusText: "OK",
  } as Response);
};

const rawResponse = (status: number, body: string) => {
  return Promise.resolve({
    ok: status >= 200 && status < 300,
    status,
    text: () => Promise.resolve(body),
    statusText: status === 401 ? "Unauthorized" : "Error",
  } as Response);
};

describe("api-client", () => {
  afterEach(() => {
    jest.restoreAllMocks();
    redirectToLogin.mockClear();
  });

  it("includes browser credentials on API requests", async () => {
    jest.spyOn(global, "fetch").mockImplementation(() => jsonResponse({ ok: true }));

    await apiGet("/vpc/v1/networks");

    const fetchMock = jest.mocked(global.fetch);
    const [, init] = fetchMock.mock.calls[0] as [RequestInfo | URL, RequestInit];

    expect(init.credentials).toBe("include");
  });

  it("redirects to login on a 401 with a non-JSON (HTML/plaintext) body", async () => {
    jest
      .spyOn(global, "fetch")
      .mockImplementation(() => rawResponse(401, "<html><body>401 Unauthorized</body></html>"));

    await expect(apiGet("/vpc/v1/networks")).rejects.toBeInstanceOf(Error);

    // The 401 -> login redirect must fire even though the body is not JSON.
    expect(redirectToLogin).toHaveBeenCalledTimes(1);
  });

  it("does not surface a JSON parse error on a non-JSON error body", async () => {
    jest.spyOn(global, "fetch").mockImplementation(() => rawResponse(500, "upstream connect error"));

    // The rejection must carry the HTTP-derived message, not an opaque SyntaxError.
    await expect(apiGet("/vpc/v1/networks")).rejects.not.toThrow(SyntaxError);
  });
});
