// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { jest } from "@jest/globals";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { SIGNED_IN, installLane, refusal } from "@shared/test/lane-fake";

// Экран входа: что он делает на каждом исходе глагола (приёмка F8, S1). Сеть —
// дублёр полосы с телами службы; экран — настоящий. Сквозная проба того же
// предмета — `ui-future/e2e/specs/identity-ceremony.spec.ts`; здесь закреплены
// свойства, которые браузерная проба видит только исходом.

const { LoginPage, SECOND_FACTOR_TOGGLE_HINT } = await import("./LoginPage");

function renderAt(url: string, leave = jest.fn<(to: string) => void>()) {
  render(
    <MemoryRouter initialEntries={[url]}>
      <LoginPage leave={leave} />
    </MemoryRouter>,
  );
  return leave;
}

const email = () => screen.getByLabelText<HTMLInputElement>("Адрес электронной почты");
const password = () => screen.getByLabelText<HTMLInputElement>("Пароль");
const submit = () => screen.getByRole("button", { name: "Войти" });

async function formShown() {
  await screen.findByRole("form", { name: "Вход в консоль" });
}

let lane: ReturnType<typeof installLane> | null = null;
afterEach(() => lane?.restore());

describe("экран входа", () => {
  it("F8-04 · вход зовёт НАШ глагол с признаком своего вида и уводит на адрес возврата", async () => {
    lane = installLane({ "POST /iam/v1/auth/login": SIGNED_IN });
    const leave = renderAt("/login?returnTo=/dashboard");
    await formShown();
    fireEvent.change(email(), { target: { value: "a@kacho.local" } });
    fireEvent.change(password(), { target: { value: "p" } });
    fireEvent.click(submit());
    await waitFor(() => expect(leave).toHaveBeenCalledWith("/dashboard"));
    const [posted] = lane.of("POST", "/iam/v1/auth/login");
    expect(posted.body).toEqual({ email: "a@kacho.local", password: "p", csrfToken: "tok-login-1" });
    expect(lane.of("GET", "/iam/v1/auth/csrf").map((c) => c.query)).toEqual(["?form=login"]);
    // Отрицание в паре с положительным выше: ни одного обращения мимо полосы.
    expect(lane.calls.every((c) => c.path.startsWith("/iam/v1/auth/"))).toBe(true);
  });

  it("F8-06 · поле, названное службой, отмечено — и только оно; своего правила экран не применял", async () => {
    lane = installLane({
      "POST /iam/v1/auth/login": refusal(400, 3, "Illegal argument password: required"),
    });
    renderAt("/login");
    await formShown();
    fireEvent.change(email(), { target: { value: "a@kacho.local" } });
    fireEvent.click(submit());
    await screen.findByText("Illegal argument password: required");
    expect(password()).toHaveAttribute("aria-invalid", "true");
    expect(email()).not.toHaveAttribute("aria-invalid");
    // Пустой пароль ушёл службе — консоль его не задержала (Р2).
    expect(lane.of("POST", "/iam/v1/auth/login")[0].body).toMatchObject({ password: "" });
  });

  it("F8-07 · отказ о поле, которого на форме нет, назван предупреждением, а не пустым экраном", async () => {
    lane = installLane({ "POST /iam/v1/auth/login": refusal(400, 3, "Illegal argument csrfToken: required") });
    renderAt("/login");
    await formShown();
    fireEvent.click(submit());
    expect(await screen.findByRole("alert")).toHaveTextContent("Illegal argument csrfToken: required");
  });

  it("F8-08 · отвергнутый признак формы: экран СРАЗУ добывает свежий, и повторная отправка несёт его", async () => {
    lane = installLane({
      "POST /iam/v1/auth/login": (_c, nth) =>
        nth === 1 ? refusal(403, 7, "form token rejected", "FORM_TOKEN_REJECTED") : SIGNED_IN,
    });
    const leave = renderAt("/login");
    await formShown();
    fireEvent.change(email(), { target: { value: "a@kacho.local" } });
    fireEvent.change(password(), { target: { value: "p" } });
    fireEvent.click(submit());
    expect(await screen.findByRole("alert")).toHaveTextContent("form token rejected");
    await waitFor(() => expect(lane!.of("GET", "/iam/v1/auth/csrf")).toHaveLength(2));
    expect(email().value).toBe("a@kacho.local");
    fireEvent.click(submit());
    await waitFor(() => expect(leave).toHaveBeenCalledWith("/"));
    expect(lane.of("POST", "/iam/v1/auth/login")[1].body).toMatchObject({ csrfToken: "tok-login-2" });
  });

  it("F8-09 · отказ по частоте: срок назван числом из Retry-After, отправка закрыта", async () => {
    lane = installLane({
      "POST /iam/v1/auth/login": {
        ...refusal(429, 8, "too many attempts; try again later", "TOO_MANY_ATTEMPTS"),
        headers: { "Retry-After": "897" },
      },
    });
    renderAt("/login");
    await formShown();
    fireEvent.click(submit());
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("too many attempts; try again later");
    expect(alert).toHaveTextContent("Повторить можно через 897 с.");
    expect(submit()).toBeDisabled();
    // Отправка формы мимо кнопки — тоже закрыта: обращений не прибавилось.
    act(() => {
      fireEvent.submit(screen.getByRole("form", { name: "Вход в консоль" }));
    });
    expect(lane.of("POST", "/iam/v1/auth/login")).toHaveLength(1);
  });

  it("F8-10 · служба не ответила: текст службы, приглашение повторить, введённое цело", async () => {
    lane = installLane({ "POST /iam/v1/auth/login": refusal(503, 14, "request not performed; try again later") });
    renderAt("/login");
    await formShown();
    fireEvent.change(email(), { target: { value: "a@kacho.local" } });
    fireEvent.click(submit());
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("request not performed; try again later");
    expect(alert).toHaveTextContent("Отправьте форму ещё раз.");
    expect(submit()).toBeEnabled();
    expect(email().value).toBe("a@kacho.local");
  });

  it("F8-11 · у человека с живой сессией формы нет — он уходит на адрес возврата", async () => {
    lane = installLane({ "GET /iam/v1/auth/me": SIGNED_IN });
    const leave = renderAt("/login?returnTo=/dashboard");
    await waitFor(() => expect(leave).toHaveBeenCalledWith("/dashboard"));
    expect(screen.queryByLabelText("Адрес электронной почты")).toBeNull();
  });

  it("F8-12 · негодный носитель — та же форма, что без сессии, и экран о причине молчит", async () => {
    // Край отвечает на негодный носитель ТЕМ ЖЕ, что на его отсутствие:
    // `200 {"user":null}` (`session_identity_handler.go`, `meFromOwnSession`).
    lane = installLane({ "GET /iam/v1/auth/me": { status: 200, body: { user: null } } });
    const leave = renderAt("/login?returnTo=/dashboard");
    await formShown();
    expect(leave).not.toHaveBeenCalled();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("C6 · край не ответил о сессии — форма есть, неответ назван, и экран никуда не уводит", async () => {
    lane = installLane({
      "GET /iam/v1/auth/me": (_c, nth) => (nth === 1 ? refusal(503, 14, "unavailable") : SIGNED_IN),
    });
    const leave = renderAt("/login?returnTo=/dashboard");
    await formShown();
    expect(leave).not.toHaveBeenCalled();
    expect(screen.getByRole("alert")).toHaveTextContent("unavailable");
    // Спросить снова — и живая сессия уводит на адрес возврата.
    fireEvent.click(screen.getByRole("button", { name: "Проверить снова" }));
    await waitFor(() => expect(leave).toHaveBeenCalledWith("/dashboard"));
  });

  it("C10 · отскок живой сессии идёт через тот же валидатор: чужой адрес возврата — на корень", async () => {
    for (const hostile of ["//evil.example/dashboard", "https://evil.example/x", "/\\evil.example/x"]) {
      lane = installLane({ "GET /iam/v1/auth/me": SIGNED_IN });
      const leave = jest.fn<(to: string) => void>();
      const { unmount } = render(
        <MemoryRouter initialEntries={[`/login?returnTo=${encodeURIComponent(hostile)}`]}>
          <LoginPage leave={leave} />
        </MemoryRouter>,
      );
      await waitFor(() => expect(leave).toHaveBeenCalled());
      expect([hostile, leave.mock.calls[0][0]]).toEqual([hostile, "/"]);
      unmount();
      lane.restore();
    }
  });

  it("C10 · путь на регистрацию несёт адрес возврата тем же именем параметра", async () => {
    lane = installLane({});
    renderAt("/login?returnTo=%2Fiam%2Fusers");
    await formShown();
    const link = screen.getByRole<HTMLAnchorElement>("link", { name: "Завести учётную запись" });
    expect(new URL(link.href).pathname).toBe("/registration");
    expect(new URL(link.href).searchParams.get("returnTo")).toBe("/iam/users");
  });

  it("C11 · отказ по частоте без Retry-After — срок не выдуман и отправка не закрыта", async () => {
    lane = installLane({
      "POST /iam/v1/auth/login": refusal(429, 8, "too many attempts; try again later", "TOO_MANY_ATTEMPTS"),
    });
    renderAt("/login");
    await formShown();
    fireEvent.click(submit());
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("too many attempts; try again later");
    expect(alert).not.toHaveTextContent("Повторить можно через");
    expect(submit()).toBeEnabled();
  });

  it("C26 · форма входа не уходит нативной отправкой: пароль в адрес не попадает", async () => {
    lane = installLane({ "POST /iam/v1/auth/login": refusal(401, 16, "authentication failed") });
    renderAt("/login");
    await formShown();
    fireEvent.change(password(), { target: { value: "секрет-пароль" } });
    const form = screen.getByRole("form", { name: "Вход в консоль" });
    const event = new Event("submit", { bubbles: true, cancelable: true });
    act(() => {
      form.dispatchEvent(event);
    });
    // Отправку ведёт обработчик консоли, а не браузер: у нативной отправки
    // формы без метода значения полей уходят строкой запроса адреса.
    expect(event.defaultPrevented).toBe(true);
    // У полей нет имён — нативной отправке нечего было бы положить в адрес.
    expect([...form.querySelectorAll("input")].filter((i) => i.name !== "")).toEqual([]);
    await waitFor(() => expect(lane!.of("POST", "/iam/v1/auth/login")).toHaveLength(1));
    expect(lane.calls.some((c) => c.query.includes("секрет-пароль") || c.path.includes("секрет-пароль"))).toBe(false);
  });

  it("F8-13 · чужой адрес возврата отвергнут во всех четырёх формах, свой — соблюдён", async () => {
    for (const [returnTo, expected] of [
      ["https://evil.example/dashboard", "/"],
      ["//evil.example/dashboard", "/"],
      ["/\\evil.example/dashboard", "/"],
      ["javascript:alert(1)", "/"],
      ["/dashboard?f8-13=kontrol", "/dashboard?f8-13=kontrol"],
    ]) {
      lane = installLane({ "POST /iam/v1/auth/login": SIGNED_IN });
      const leave = jest.fn<(to: string) => void>();
      const { unmount } = render(
        <MemoryRouter initialEntries={[`/login?returnTo=${encodeURIComponent(returnTo)}`]}>
          <LoginPage leave={leave} />
        </MemoryRouter>,
      );
      await formShown();
      fireEvent.click(submit());
      await waitFor(() => expect(leave).toHaveBeenCalled());
      expect([returnTo, leave.mock.calls[0][0]]).toEqual([returnTo, expected]);
      unmount();
      lane.restore();
    }
  });

  it("F8-39 · пути на подтверждение адреса нет, на регистрацию — есть", async () => {
    // Половина о `/recovery` обращена приёмкой F8-S3 (§3.1): её держит F8S3-03.
    lane = installLane({});
    renderAt("/login");
    await formShown();
    const destinations = screen.getAllByRole("link").map((a) => new URL((a as HTMLAnchorElement).href).pathname);
    expect(destinations).not.toContain("/verification");
    expect(destinations).toContain("/registration");
  });

  it("F8S3-03 · путь «Не получается войти?» ведёт на /recovery с адресом возврата и без адреса почты", async () => {
    lane = installLane({});
    renderAt("/login?returnTo=/dashboard");
    await formShown();
    fireEvent.change(email(), { target: { value: "a@kacho.local" } });
    const link = screen.getByRole<HTMLAnchorElement>("link", { name: "Не получается войти?" });
    const url = new URL(link.href);
    expect([url.pathname, url.search]).toEqual(["/recovery", "?returnTo=%2Fdashboard"]);
    expect(decodeURIComponent(link.href)).not.toContain("a@kacho.local");
    // Положительная сторона распознавателя: ссылка регистрации тем же предикатом найдена.
    const registration = screen.getByRole<HTMLAnchorElement>("link", { name: "Завести учётную запись" });
    expect(new URL(registration.href).pathname).toBe("/registration");
  });

  it("#2953 · отказ входа: текст службы дословно, рядом — следующий шаг без раскрытия причины", async () => {
    lane = installLane({ "POST /iam/v1/auth/login": refusal(401, 16, "authentication failed") });
    renderAt("/login");
    await formShown();
    fireEvent.change(email(), { target: { value: "a@kacho.local" } });
    fireEvent.click(submit());
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toBe("authentication failed");
    const step = screen.getByTestId("refusal-next-step");
    expect(step.textContent).toContain("«Не получается войти?»");
    expect(step.textContent).not.toMatch(/неверн|не найден|не заведён|заблокир/i);
  });

  it("#2953 · флажок второго фактора объясняет, когда его отмечать", async () => {
    lane = installLane({});
    renderAt("/login");
    await formShown();
    expect(screen.getByText(SECOND_FACTOR_TOGGLE_HINT)).toBeInTheDocument();
    expect(SECOND_FACTOR_TOGGLE_HINT).toMatch(/заводили второй фактор/);
  });

  it("F8-40 · экран отказа — функция ТОЛЬКО тела ответа: заведён адрес или нет, экран один", async () => {
    const screens: string[] = [];
    for (const address of ["known@kacho.local", "unknown@kacho.local"]) {
      lane = installLane({ "POST /iam/v1/auth/login": refusal(401, 16, "authentication failed") });
      const { container, unmount } = render(
        <MemoryRouter initialEntries={["/login"]}>
          <LoginPage leave={jest.fn()} />
        </MemoryRouter>,
      );
      await formShown();
      fireEvent.change(email(), { target: { value: address } });
      fireEvent.click(submit());
      await screen.findByRole("alert");
      screens.push(screen.getByRole("alert").textContent ?? "");
      // Поля формы те же; значение адреса — введённое самим человеком.
      screens.push(String(container.querySelectorAll("input").length));
      unmount();
      lane.restore();
    }
    expect(screens[0]).toBe("authentication failed");
    expect(screens[0]).toBe(screens[2]);
    expect(screens[1]).toBe(screens[3]);
  });
});

// ═══ F8-S4 — вход ключом доступа: модульные сценарии ════════════════════════
//
// Приёмка F8-S4 (§6.1): экран над подставным транспортом полосы и подставным
// интерфейсом браузера; часы — управляемые. Положительный близнец — F8S4-08;
// каждое отрицание меняет против него ОДИН факт (§6.2).

/** Ответ службы на испытание — форма `loginlanehttp.accessKeyBegin`. */
const BEGIN_ANSWER = {
  status: 200,
  body: {
    publicKey: {
      challenge: "AQID",
      rpId: "console.test",
      timeout: 300000,
      userVerification: "preferred",
      allowCredentials: [],
    },
  },
};

const bytesOf = (...xs: number[]) => new Uint8Array(xs).buffer;

/**
 * Удостоверение, которое отдаёт интерфейс браузера: сериализация его несёт,
 * кроме объявленных, `clientExtensionResults` и `authenticatorAttachment`.
 */
function browserCredential() {
  return {
    id: "Y3JlZA",
    rawId: bytesOf(0x63, 0x72, 0x65, 0x64),
    type: "public-key",
    authenticatorAttachment: "platform",
    getClientExtensionResults: () => ({ credProps: { rk: true } }),
    toJSON: () => ({
      id: "Y3JlZA",
      rawId: "Y3JlZA",
      type: "public-key",
      clientExtensionResults: {},
      authenticatorAttachment: "platform",
      response: {},
    }),
    response: {
      clientDataJSON: bytesOf(0x7b, 0x7d),
      authenticatorData: bytesOf(1, 2, 3),
      signature: bytesOf(4, 5, 6),
      userHandle: bytesOf(7, 8),
    },
  };
}

/** Тело предъявления, которое обязан собрать экран из `browserCredential()`. */
const EXPECTED_CREDENTIAL = {
  id: "Y3JlZA",
  rawId: "Y3JlZA",
  type: "public-key",
  response: { clientDataJSON: "e30", authenticatorData: "AQID", signature: "BAUG", userHandle: "Bwg" },
};

/** Текст консоли на отказ церемонии браузером (Р5) — один на все причины. */
const BROWSER_REFUSED = "Вход ключом прерван в браузере — повторите или войдите паролем";

type CredentialsGet = (options?: CredentialRequestOptions) => Promise<unknown>;

/** Подставной интерфейс ключей браузера; `present: false` — браузер без него (Р7). */
function installBrowserKeys(get: CredentialsGet, present = true) {
  const w = window as unknown as Record<string, unknown>;
  const savedPkc = w.PublicKeyCredential;
  const savedCredentials = Object.getOwnPropertyDescriptor(navigator, "credentials");
  const spy = jest.fn(get);
  if (present) w.PublicKeyCredential = function PublicKeyCredential() {};
  else delete w.PublicKeyCredential;
  Object.defineProperty(navigator, "credentials", { configurable: true, value: { get: spy } });
  return {
    get: spy,
    restore() {
      if (savedPkc === undefined) delete w.PublicKeyCredential;
      else w.PublicKeyCredential = savedPkc;
      if (savedCredentials) Object.defineProperty(navigator, "credentials", savedCredentials);
      else delete (navigator as unknown as Record<string, unknown>).credentials;
    },
  };
}

/**
 * Журнал браузера и необработанные отклонения за время пробы (условие К4):
 * экран полосы ключа не печатает ничего и не бросает мимо обработчика.
 */
function watchJournal() {
  const methods = ["log", "info", "warn", "error", "debug"] as const;
  const spies = methods.map((m) => jest.spyOn(console, m).mockImplementation(() => undefined));
  const rejections: unknown[] = [];
  const onRejection = (reason: unknown) => rejections.push(reason);
  process.on("unhandledRejection", onRejection);
  return {
    entries: () => spies.flatMap((s, i) => s.mock.calls.map((args) => `${methods[i]}: ${args.map(String).join(" ")}`)),
    rejections,
    restore() {
      process.off("unhandledRejection", onRejection);
      spies.forEach((s) => s.mockRestore());
    },
  };
}

const keyButton = () => screen.getByRole("button", { name: "Войти ключом доступа" });

let keys: ReturnType<typeof installBrowserKeys> | null = null;
let journal: ReturnType<typeof watchJournal> | null = null;
afterEach(() => {
  keys?.restore();
  keys = null;
  journal?.restore();
  journal = null;
  jest.useRealTimers();
});

/** Журнал пуст и отклонений нет — утверждение каждой модульной пробы S4. */
async function expectQuietJournal() {
  // Отклонение промиса доходит до процесса после очереди микрозадач.
  await new Promise((r) => setTimeout(r, 0));
  expect({ journal: journal!.entries(), rejections: journal!.rejections.map(String) }).toEqual({
    journal: [],
    rejections: [],
  });
}

describe("экран входа · вход ключом доступа (F8-S4)", () => {
  it("F8S4-08 · утверждение браузера уходит ровно объявленными полями, признаки — своих видов, уход на адрес возврата", async () => {
    journal = watchJournal();
    lane = installLane({ "POST /iam/v1/auth/access-key/begin": BEGIN_ANSWER, "POST /iam/v1/auth/access-key/login": SIGNED_IN });
    keys = installBrowserKeys(() => Promise.resolve(browserCredential()));
    const leave = renderAt("/login?returnTo=/dashboard");
    await formShown();
    fireEvent.click(keyButton());
    await waitFor(() => expect(leave).toHaveBeenCalledWith("/dashboard"));

    // Параметры церемонии — из ответа службы дословно (К6).
    expect(keys.get).toHaveBeenCalledTimes(1);
    const publicKey = keys.get.mock.calls[0][0]?.publicKey;
    expect({ ...publicKey, challenge: [...new Uint8Array(publicKey!.challenge as ArrayBuffer)] }).toEqual({
      challenge: [1, 2, 3],
      rpId: "console.test",
      timeout: 300000,
      userVerification: "preferred",
      allowCredentials: [],
    });

    const [begin] = lane.of("POST", "/iam/v1/auth/access-key/begin");
    expect(begin.body).toEqual({ csrfToken: "tok-access-key-begin-1" });
    const [login] = lane.of("POST", "/iam/v1/auth/access-key/login");
    expect(login.body).toEqual({ credential: EXPECTED_CREDENTIAL, csrfToken: "tok-access-key-login-1" });
    const kinds = lane.of("GET", "/iam/v1/auth/csrf").map((c) => c.query);
    expect(kinds).toEqual(expect.arrayContaining(["?form=access-key-begin", "?form=access-key-login"]));
    // Вход выпущен после ответа на испытание: признак входа добыт после него.
    const order = lane.calls.map((c) => `${c.method} ${c.path}${c.query}`);
    expect(order.indexOf("POST /iam/v1/auth/access-key/begin")).toBeLessThan(
      order.indexOf("GET /iam/v1/auth/csrf?form=access-key-login"),
    );
    expect(lane.of("POST", "/iam/v1/auth/login")).toHaveLength(0);
    await expectQuietJournal();
  });

  it("F8S4-06 · отказ церемонии браузером: назван текст консоли, глагол входа не зовётся", async () => {
    journal = watchJournal();
    lane = installLane({ "POST /iam/v1/auth/access-key/begin": BEGIN_ANSWER, "POST /iam/v1/auth/access-key/login": SIGNED_IN });
    // Текст отказа браузера несёт адрес — экран обязан его не повторить (К2).
    const marker = "https://marker.invalid/webauthn-not-allowed";
    keys = installBrowserKeys(() =>
      Promise.reject(new DOMException(`The operation either timed out or was not allowed. See: ${marker}`, "NotAllowedError")),
    );
    const leave = renderAt("/login?returnTo=/dashboard");
    await formShown();
    fireEvent.click(keyButton());
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toBe(BROWSER_REFUSED);
    expect(document.body.textContent).not.toContain(marker);
    expect(document.body.textContent).not.toContain("NotAllowedError");
    expect(lane.of("POST", "/iam/v1/auth/access-key/login")).toHaveLength(0);
    expect(leave).not.toHaveBeenCalled();
    expect(keyButton()).toBeEnabled();
    expect(submit()).toBeEnabled();
    expect(email()).toBeEnabled();
    expect(password()).toBeEnabled();
    await expectQuietJournal();
  });

  it("F8S4-06 · любой иной отказ браузера назван тем же одним текстом — причины консоль не угадывает", async () => {
    for (const thrown of [
      new DOMException("x https://marker.invalid/abort", "AbortError"),
      new DOMException("rp id https://marker.invalid/rp", "SecurityError"),
      new TypeError("https://marker.invalid/type"),
      "https://marker.invalid/string",
    ]) {
      lane = installLane({ "POST /iam/v1/auth/access-key/begin": BEGIN_ANSWER });
      // Браузер вправе отклонить и не исключением — проба подаёт ровно такое.
      // eslint-disable-next-line @typescript-eslint/prefer-promise-reject-errors
      keys = installBrowserKeys(() => Promise.reject(thrown));
      const { unmount } = render(
        <MemoryRouter initialEntries={["/login"]}>
          <LoginPage leave={jest.fn()} />
        </MemoryRouter>,
      );
      await formShown();
      fireEvent.click(keyButton());
      const alert = await screen.findByRole("alert");
      expect([String(thrown), alert.textContent]).toEqual([String(thrown), BROWSER_REFUSED]);
      expect(document.body.textContent).not.toContain("marker.invalid");
      expect(lane.of("POST", "/iam/v1/auth/access-key/login")).toHaveLength(0);
      unmount();
      keys.restore();
      lane.restore();
    }
  });

  it("F8S4-07 · браузер без интерфейса ключей кнопки не получает", async () => {
    lane = installLane({ "POST /iam/v1/auth/access-key/begin": BEGIN_ANSWER, "POST /iam/v1/auth/access-key/login": SIGNED_IN });
    keys = installBrowserKeys(() => Promise.resolve(browserCredential()), false);
    renderAt("/login?returnTo=/dashboard");
    await formShown();
    expect(screen.queryByRole("button", { name: "Войти ключом доступа" })).toBeNull();
    expect(email()).toBeVisible();
    expect(password()).toBeVisible();
    expect(submit()).toBeVisible();
    expect(lane.calls.filter((c) => c.path.includes("/access-key/") || c.query.includes("access-key"))).toEqual([]);
  });

  it("F8S4-13 · частота: экран называет срок и до его истечения испытания не просит", async () => {
    journal = watchJournal();
    // Управляемые часы — только таймер срока; опрос и наблюдение DOM — настоящие.
    jest.useFakeTimers({
      doNotFake: ["nextTick", "setImmediate", "setInterval", "clearInterval", "queueMicrotask", "Date", "performance"],
    });
    lane = installLane({
      "POST /iam/v1/auth/access-key/begin": {
        ...refusal(429, 8, "too many attempts; try again later", "TOO_MANY_ATTEMPTS"),
        headers: { "Retry-After": "30" },
      },
    });
    keys = installBrowserKeys(() => Promise.resolve(browserCredential()));
    renderAt("/login");
    await formShown();
    fireEvent.click(keyButton());
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("too many attempts; try again later");
    expect(alert).toHaveTextContent("Повторить можно через 30 с.");
    expect(keyButton()).toBeDisabled();
    fireEvent.click(keyButton());
    act(() => {
      jest.advanceTimersByTime(29_000);
    });
    fireEvent.click(keyButton());
    expect(lane.of("POST", "/iam/v1/auth/access-key/begin")).toHaveLength(1);
    expect(keys.get).not.toHaveBeenCalled();
    // Форма пароля доступна всё время: какую ось исчерпал отказ, консоль не судит.
    expect(submit()).toBeEnabled();
    act(() => {
      jest.advanceTimersByTime(1_000);
    });
    await waitFor(() => expect(keyButton()).toBeEnabled());
    expect(submit()).toBeEnabled();
    jest.useRealTimers();
    await expectQuietJournal();
  });

  it("F8S4-14 · служба не ответила на испытание: отказ назван дословно, церемония браузера не начата, повтор предложен", async () => {
    journal = watchJournal();
    lane = installLane({ "POST /iam/v1/auth/access-key/begin": refusal(503, 14, "request not performed; try again later") });
    keys = installBrowserKeys(() => Promise.resolve(browserCredential()));
    const leave = renderAt("/login");
    await formShown();
    fireEvent.click(keyButton());
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("request not performed; try again later");
    expect(alert).toHaveTextContent("Отправьте форму ещё раз.");
    expect(keys.get).not.toHaveBeenCalled();
    expect(keyButton()).toBeEnabled();
    expect(submit()).toBeEnabled();
    expect(leave).not.toHaveBeenCalled();
    await expectQuietJournal();
  });

  it("К3 · ответ испытания не той формы: назван отказ полосы, церемония браузера не начата, вход без эха", async () => {
    const marker = "https://marker.invalid/challenge";
    lane = installLane({ "POST /iam/v1/auth/access-key/begin": { status: 200, body: { publicKey: { challenge: marker } } } });
    keys = installBrowserKeys(() => Promise.resolve(browserCredential()));
    renderAt("/login");
    await formShown();
    fireEvent.click(keyButton());
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("Служба не ответила по существу");
    expect(document.body.textContent).not.toContain(marker);
    expect(keys.get).not.toHaveBeenCalled();
    expect(lane.of("POST", "/iam/v1/auth/access-key/login")).toHaveLength(0);
  });

  it("К7 · двойное нажатие — одна попытка: второго испытания нет, вход паролем на время церемонии закрыт", async () => {
    let release: (v: unknown) => void = () => undefined;
    lane = installLane({ "POST /iam/v1/auth/access-key/begin": BEGIN_ANSWER, "POST /iam/v1/auth/access-key/login": SIGNED_IN });
    keys = installBrowserKeys(() => new Promise((r) => (release = r)));
    const leave = renderAt("/login");
    await formShown();
    fireEvent.click(keyButton());
    fireEvent.click(keyButton());
    await waitFor(() => expect(keys!.get).toHaveBeenCalledTimes(1));
    fireEvent.click(keyButton());
    expect(submit()).toBeDisabled();
    expect(lane.of("POST", "/iam/v1/auth/access-key/begin")).toHaveLength(1);
    release(browserCredential());
    await waitFor(() => expect(leave).toHaveBeenCalledWith("/"));
    expect(lane.of("POST", "/iam/v1/auth/access-key/login")).toHaveLength(1);
  });

  it("К9 · отказ службы на входе ключом — дословно, без причины и кода, сессии нет", async () => {
    lane = installLane({
      "POST /iam/v1/auth/access-key/begin": BEGIN_ANSWER,
      "POST /iam/v1/auth/access-key/login": refusal(401, 16, "authentication failed"),
    });
    keys = installBrowserKeys(() => Promise.resolve(browserCredential()));
    const leave = renderAt("/login");
    await formShown();
    fireEvent.click(keyButton());
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toBe("authentication failed");
    expect(leave).not.toHaveBeenCalled();
    expect(keyButton()).toBeEnabled();
    expect(submit()).toBeEnabled();
  });
});
