import { jest } from "@jest/globals";

const redirectToLogin = jest.fn();
jest.unstable_mockModule("./auth", () => ({
  redirectToLogin,
  loginUrl: () => "/login",
}));

const { apiGet } = await import("./api-client");
const { noteBearerRotated } = await import("@shared/api/lane-epochs");
const { setStepUpRequester } = await import("@shared/api/step-up");

/** Ответ с заголовками — так, как его видит `fetch`. */
const answered = (status: number, body: string, headers: Record<string, string> = {}) => {
  const h = Object.fromEntries(Object.entries(headers).map(([k, v]) => [k.toLowerCase(), v]));
  return Promise.resolve({
    ok: status >= 200 && status < 300,
    status,
    headers: { get: (n: string) => h[n.toLowerCase()] ?? null },
    text: () => Promise.resolve(body),
    statusText: String(status),
  } as unknown as Response);
};

/** Вызов пола края RFC 9470 — тот же производитель, что у платформы (`authz.go`). */
const FLOOR = {
  "WWW-Authenticate":
    'Bearer error="insufficient_user_authentication", error_description="Required ACR 2", acr_values="2"',
};
/** Край: носитель не находит записи (`writeHTTPUnauthorized`). */
const ENDED = { "WWW-Authenticate": 'Bearer error="invalid_token", error_description="session ended; sign in again"' };

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
    setStepUpRequester(null);
  });

  it("C15 · вызов пола — повышение и ОДИН повтор, а не переход на вход", async () => {
    const asked = jest.fn(() => Promise.resolve());
    setStepUpRequester(asked);
    let n = 0;
    jest
      .spyOn(global, "fetch")
      .mockImplementation(() =>
        ++n === 1
          ? answered(401, '{"code":16,"message":"insufficient_user_authentication"}', FLOOR)
          : jsonResponse({ ok: 1 }),
      );
    await expect(apiGet("/vpc/v1/networks")).resolves.toEqual({ ok: 1 });
    expect(asked).toHaveBeenCalledWith({ cause: "floor", acr: "2" });
    expect(redirectToLogin).not.toHaveBeenCalled();
  });

  it("C15 · вызов пола, повышать некому — отказ назван, на вход не уводит", async () => {
    jest
      .spyOn(global, "fetch")
      .mockImplementation(() => answered(401, '{"code":16,"message":"insufficient_user_authentication"}', FLOOR));
    await expect(apiGet("/vpc/v1/networks")).rejects.toBeInstanceOf(Error);
    expect(redirectToLogin).not.toHaveBeenCalled();
  });

  it("C18 · носитель перевыпущен ЭТОЙ вкладкой, пока запрос шёл: ОДИН повтор, на вход не уводит", async () => {
    let n = 0;
    jest.spyOn(global, "fetch").mockImplementation(() => {
      n += 1;
      if (n === 1) {
        // Ответ приходит ПОСЛЕ перевыпуска: смена пароля в этой вкладке ответила раньше.
        noteBearerRotated();
        return answered(401, '{"code":16,"message":"session ended; sign in again"}', ENDED);
      }
      return jsonResponse({ ok: 2 });
    });
    await expect(apiGet("/vpc/v1/networks")).resolves.toEqual({ ok: 2 });
    expect(n).toBe(2);
    expect(redirectToLogin).not.toHaveBeenCalled();
  });

  it("C18 · тот же отказ без перевыпуска в этой вкладке — сессия кончилась: на вход, повтора нет", async () => {
    let n = 0;
    jest.spyOn(global, "fetch").mockImplementation(() => {
      n += 1;
      return answered(401, '{"code":16,"message":"session ended; sign in again"}', ENDED);
    });
    await expect(apiGet("/vpc/v1/networks")).rejects.toBeInstanceOf(Error);
    expect(n).toBe(1);
    expect(redirectToLogin).toHaveBeenCalledTimes(1);
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
