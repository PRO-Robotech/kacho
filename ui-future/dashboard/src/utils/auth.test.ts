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

// ─── C14 · второй выход той же вкладки ───────────────────────────────────────
//
// Панель учётной записи закрывается нажатием вне её и клавишей Escape и тогда,
// когда выход в полёте (`AccountPanel.tsx`), а смонтирована она условно: открытая
// заново, она несёт НОВЫЙ хук выхода. Нажатие «Выйти» на ней — второй выход той
// же вкладки рядом с первым. Второму служба отказывает (сессию погасил первый),
// и отказ второго не вправе снять выход первого: иначе следующий `401` чтения
// прежней страницы уводит на вход с её адресом возврата и отменяет переход
// выхода — тот же исход F8-18, что без метки вовсе.

const SECOND_AFTER_FIRST = "отказ второму приходит после подтверждения первого";
const SECOND_WHILE_FIRST = "отказ второму приходит, пока первый в полёте";

/** Сеть двух выходов: первый держится до `releaseFirst`, второй отвергается по `secondRefused`. */
function twoExitsNetwork(secondRefused: Promise<void>) {
  const original = globalThis.fetch;
  let releaseFirst: () => void = () => undefined;
  const firstAnswered = new Promise<void>((resolve) => {
    releaseFirst = resolve;
  });
  let logoutVerbs = 0;
  let issued = 0;
  globalThis.fetch = (input: RequestInfo | URL) => {
    const path = new URL(requestUrl(input), "http://console.test").pathname;
    if (path === "/iam/v1/auth/csrf") return Promise.resolve(answered(200, { csrfToken: `tok-logout-${++issued}` }));
    if (path === "/iam/v1/auth/logout") {
      logoutVerbs += 1;
      if (logoutVerbs === 1) return firstAnswered.then(() => answered(200, {}));
      // Сессию погасил первый выход — у второго её нет.
      return secondRefused.then(() =>
        answered(401, { code: 16, message: "session ended; sign in again", details: [] }),
      );
    }
    return Promise.resolve(answered(404, { code: 5, message: `дублёр: ${path} не объявлен пробой`, details: [] }));
  };
  return {
    releaseFirst,
    logoutVerbs: () => logoutVerbs,
    restore: () => {
      globalThis.fetch = original;
    },
  };
}

describe("C14 · второй выход той же вкладки не отменяет первого", () => {
  beforeEach(() => {
    window.history.pushState(null, "", PREVIOUS_PAGE);
  });
  afterEach(() => {
    delete (globalThis as unknown as Record<symbol, unknown>)[TAB_EXIT_KEY];
  });

  for (const order of [SECOND_AFTER_FIRST, SECOND_WHILE_FIRST] as const) {
    it(`F8-18 · панель закрыта и открыта заново в полёте выхода и нажата снова, ${order}: отказ 401 не уводит на вход с адресом прежнего человека`, async () => {
      let firstLeft: () => void = () => undefined;
      const left = new Promise<void>((resolve) => {
        firstLeft = resolve;
      });
      const net = twoExitsNetwork(order === SECOND_AFTER_FIRST ? left : Promise.resolve());
      try {
        const leaveFirst = jest.fn<(to: string) => void>(() => firstLeft());
        const leaveSecond = jest.fn<(to: string) => void>();
        const first = renderHook(() => useLogout(leaveFirst));
        let firstDone: Promise<void> = Promise.resolve();
        act(() => {
          firstDone = first.result.current.logout();
        });
        await waitFor(() => expect(net.logoutVerbs()).toBe(1));

        // Панель закрыта в полёте выхода и открыта снова — с новым хуком.
        first.unmount();
        const second = renderHook(() => useLogout(leaveSecond));
        const reopenedBusy = second.result.current.busy;
        let secondDone: Promise<void> = Promise.resolve();
        act(() => {
          secondDone = second.result.current.logout();
        });
        if (order === SECOND_WHILE_FIRST) await act(() => secondDone);

        net.releaseFirst();
        await act(() => firstDone);
        expect(leaveFirst.mock.calls).toEqual([["/login"]]);
        // Исход второго нажатия — какой бы он ни был — уже есть.
        await act(() => secondDone);

        const go = jest.fn<(to: string) => void>();
        redirectToLogin(go);
        expect({
          // Переход по 401 отменил бы переход выхода и увёл бы на страницу прежнего.
          navigatedTo: go.mock.calls,
          // Выход вкладки один: второй на службу не уходит, пока идёт первый.
          logoutVerbs: net.logoutVerbs(),
          // Открытая заново панель показывает, что выход идёт.
          reopenedBusy,
          secondLeft: leaveSecond.mock.calls,
        }).toEqual({ navigatedTo: [], logoutVerbs: 1, reopenedBusy: true, secondLeft: [] });
      } finally {
        net.restore();
      }
    });
  }

  it("после отказа выхода новое нажатие выходит: защиту вкладки отказ снимает", async () => {
    // Близнец: без него защита, не отпускающая вкладку никогда, была бы зелёной выше.
    const original = globalThis.fetch;
    let logoutVerbs = 0;
    let issued = 0;
    globalThis.fetch = (input: RequestInfo | URL) => {
      const path = new URL(requestUrl(input), "http://console.test").pathname;
      if (path === "/iam/v1/auth/csrf") return Promise.resolve(answered(200, { csrfToken: `tok-logout-${++issued}` }));
      if (path === "/iam/v1/auth/logout") {
        logoutVerbs += 1;
        return Promise.resolve(
          logoutVerbs === 1
            ? answered(503, { code: 14, message: "logout not performed; try again later", details: [] })
            : answered(200, {}),
        );
      }
      return Promise.resolve(answered(404, { code: 5, message: `дублёр: ${path} не объявлен пробой`, details: [] }));
    };
    try {
      const leaveFirst = jest.fn<(to: string) => void>();
      const first = renderHook(() => useLogout(leaveFirst));
      await act(() => first.result.current.logout());
      expect(first.result.current.refusal?.message).toBe("logout not performed; try again later");
      first.unmount();

      const leaveSecond = jest.fn<(to: string) => void>();
      const second = renderHook(() => useLogout(leaveSecond));
      await act(() => second.result.current.logout());

      const go = jest.fn<(to: string) => void>();
      redirectToLogin(go);
      expect({
        logoutVerbs,
        firstLeft: leaveFirst.mock.calls,
        secondLeft: leaveSecond.mock.calls,
        navigatedTo: go.mock.calls,
      }).toEqual({ logoutVerbs: 2, firstLeft: [], secondLeft: [["/login"]], navigatedTo: [] });
    } finally {
      globalThis.fetch = original;
    }
  });
});
