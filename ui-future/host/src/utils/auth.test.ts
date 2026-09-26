import { jest } from "@jest/globals";
import { act, renderHook, waitFor } from "@testing-library/react";
import { useLogout } from "@shared/pages/auth/use-logout";
import { requestUrl } from "@shared/test/fetch-capture";
import { installLane, refusal } from "@shared/test/lane-fake";
import { loginUrl, redirectToLogin } from "./auth";

describe("auth utils", () => {
  it("ведёт на экран входа консоли с текущим адресом возврата", () => {
    window.history.pushState(null, "", "/projects/project-1/dashboard?tab=overview#top");

    expect(loginUrl()).toBe("/login?returnTo=%2Fprojects%2Fproject-1%2Fdashboard%3Ftab%3Doverview%23top");
  });

  it("не называет чужого поставщика ни одной формой адреса", () => {
    window.history.pushState(null, "", "/dashboard");

    // Отрицание в паре с положительным выше: адрес собран, и он — наш.
    expect(loginUrl()).not.toMatch(/\/\.ory\/|\/oauth2|\/self-service\//);
    expect(loginUrl()).toMatch(/^\/login\?returnTo=/);
  });
});

// ─── C14 · уход на вход по отказу `401` и выход вкладки ──────────────────────
//
// После подтверждённого выхода носителя у браузера нет, и каждое чтение
// страницы, выпущенное до ухода документа, получает `401`. Переход на вход по
// такому отказу несёт адрес возврата — страницу ПРЕЖНЕГО человека, — и, начатый
// после перехода выхода, отменяет его: браузер исполняет последний. Следующий
// человек, вошедший в этом браузере, уводился входом на проект прежнего (F8-18,
// прогон 36208863788: `/login` отменён, `/login?returnTo=%2Fprojects%2F…` исполнен).

const PREVIOUS_PAGE = "/projects/prj-first/dashboard";
const WITH_PREVIOUS_RETURN = "/login?returnTo=%2Fprojects%2Fprj-first%2Fdashboard";
const TAB_EXIT_KEY = Symbol.for("kacho.console.tab-exit");

/** Ответ в той форме, какую читает клиент полосы. */
function answered(status: number, body: unknown): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: "",
    headers: { get: () => null },
    text: () => Promise.resolve(JSON.stringify(body)),
  } as unknown as Response;
}

describe("C14 · уход на вход по отказу 401 и выход вкладки", () => {
  let lane: ReturnType<typeof installLane> | null = null;
  beforeEach(() => {
    window.history.pushState(null, "", PREVIOUS_PAGE);
  });
  afterEach(() => {
    lane?.restore();
    lane = null;
    // Подтверждённый выход метку не снимает — документ уходит. Здесь документ
    // остаётся, и следующая проба начинает вкладку заново.
    delete (globalThis as unknown as Record<symbol, unknown>)[TAB_EXIT_KEY];
  });

  it("без выхода отказ 401 уводит на вход с адресом возврата", () => {
    const go = jest.fn<(to: string) => void>();
    redirectToLogin(go);
    expect(go.mock.calls).toEqual([[WITH_PREVIOUS_RETURN]]);
  });

  it("F8-18 · после подтверждённого выхода отказ 401 не уводит на вход с адресом прежнего человека", async () => {
    lane = installLane({ "POST /iam/v1/auth/logout": { status: 200, body: {} } });
    const leave = jest.fn<(to: string) => void>();
    const { result } = renderHook(() => useLogout(leave));
    await act(() => result.current.logout());
    expect(leave.mock.calls).toEqual([["/login"]]);

    const go = jest.fn<(to: string) => void>();
    redirectToLogin(go);
    // Переход по 401 отменил бы переход выхода.
    expect(go.mock.calls).toEqual([]);
  });

  it("F8-18 · пока выход в полёте, отказ 401 не уводит: чтение, дошедшее до края после гашения сессии, отвечает раньше выхода", async () => {
    const original = globalThis.fetch;
    let release: () => void = () => undefined;
    const held = new Promise<void>((resolve) => {
      release = resolve;
    });
    let logoutIssued = false;
    globalThis.fetch = (input: RequestInfo | URL) => {
      const path = new URL(requestUrl(input), "http://console.test").pathname;
      if (path === "/iam/v1/auth/csrf") return Promise.resolve(answered(200, { csrfToken: "tok-logout-1" }));
      if (path === "/iam/v1/auth/logout") {
        logoutIssued = true;
        return held.then(() => answered(200, {}));
      }
      return Promise.resolve(answered(404, { code: 5, message: `дублёр: ${path} не объявлен пробой`, details: [] }));
    };
    try {
      const leave = jest.fn<(to: string) => void>();
      const { result } = renderHook(() => useLogout(leave));
      let done: Promise<void> = Promise.resolve();
      act(() => {
        done = result.current.logout();
      });
      await waitFor(() => expect(logoutIssued).toBe(true));

      const go = jest.fn<(to: string) => void>();
      redirectToLogin(go);
      // Отказ 401 увёл бы вкладку, пока выход не получил ответа.
      expect(go.mock.calls).toEqual([]);

      release();
      await act(() => done);
      expect(leave.mock.calls).toEqual([["/login"]]);
    } finally {
      globalThis.fetch = original;
    }
  });

  it("выход отвергнут службой: вкладка не уходит, и отказ 401 уводит на вход как обычно", async () => {
    lane = installLane({ "POST /iam/v1/auth/logout": refusal(503, 14, "logout not performed; try again later") });
    const leave = jest.fn<(to: string) => void>();
    const { result } = renderHook(() => useLogout(leave));
    await act(() => result.current.logout());
    expect(leave).not.toHaveBeenCalled();
    expect(result.current.refusal?.message).toBe("logout not performed; try again later");

    const go = jest.fn<(to: string) => void>();
    redirectToLogin(go);
    expect(go.mock.calls).toEqual([[WITH_PREVIOUS_RETURN]]);
  });
});
