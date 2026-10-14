// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { jest } from "@jest/globals";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { SIGNED_IN, installLane, refusal, type LaneAnswer } from "@shared/test/lane-fake";

// Экран восстановления доступа `/recovery` (приёмка F8-S3, #2952). Сеть — дублёр
// полосы с телами службы; экран — настоящий. Сквозная проба того же предмета —
// `ui-future/e2e/specs/recovery.spec.ts`; здесь — модульные половины F8S3-11,
// F8S3-15 и F8S3-16 и свойства, которые браузерная проба видит только исходом.

const { RecoveryPage, RECOVERY_CODE_SENT_TEXT } = await import("./RecoveryPage");
const { refusalHint, retryNotice } = await import("@shared/components/molecules/auth/LaneRefusalAlert");
const { LaneRefusal } = await import("@shared/api/login-lane");

const REQUESTED: LaneAnswer = { status: 200, body: {} };
const TITLE = "Восстановление доступа";

function renderAt(url: string, leave = jest.fn<(to: string) => void>()) {
  const view = render(
    <MemoryRouter initialEntries={[url]}>
      <RecoveryPage leave={leave} />
    </MemoryRouter>,
  );
  return { leave, ...view };
}

const email = () => screen.getByLabelText<HTMLInputElement>("Адрес почты");
const code = () => screen.getByLabelText<HTMLInputElement>("Код из письма");
const newPassword = () => screen.getByLabelText<HTMLInputElement>("Новый пароль");
const sendCode = () => screen.getByRole("button", { name: "Отправить код" });
const complete = () => screen.getByRole("button", { name: "Сменить пароль и войти" });
const resend = () => screen.getByRole("button", { name: "Отправить код ещё раз" });

let lane: ReturnType<typeof installLane> | null = null;
afterEach(() => lane?.restore());

/** Ступень 1 пройдена: код запрошен на `address`, отрисована ступень 2. */
async function atStageTwo(address = "a@kacho.local") {
  await screen.findByRole("form", { name: TITLE });
  fireEvent.change(email(), { target: { value: address } });
  fireEvent.click(sendCode());
  await screen.findByText(RECOVERY_CODE_SENT_TEXT);
}

function fillStageTwo(c = "ABCDE-FGHJK", p = "new-password-1") {
  fireEvent.change(code(), { target: { value: c } });
  fireEvent.change(newPassword(), { target: { value: p } });
}

describe("экран восстановления доступа", () => {
  it("F8S3-01 · без сессии — форма запроса кода, и открытие не отправляет ни одной формы", async () => {
    lane = installLane({});
    renderAt("/recovery");
    // Заголовок стоит и на время вопроса о сессии — ждётся форма, а не он.
    await screen.findByRole("form", { name: TITLE });
    expect(screen.getByRole("heading", { name: TITLE })).toBeInTheDocument();
    expect(email()).toBeInTheDocument();
    expect(sendCode()).toBeEnabled();
    expect(screen.getByRole("link", { name: "Вернуться ко входу" })).toBeInTheDocument();
    expect(lane.calls.filter((c) => c.method === "POST")).toEqual([]);
  });

  it("F8S3-02 · у подтверждённой сессии формы нет — документ уходит на адрес возврата", async () => {
    lane = installLane({ "GET /iam/v1/auth/me": SIGNED_IN });
    const { leave } = renderAt("/recovery?returnTo=/dashboard");
    await waitFor(() => expect(leave).toHaveBeenCalledWith("/dashboard"));
    expect(screen.queryByLabelText("Адрес почты")).toBeNull();
    expect(lane.of("POST", "/iam/v1/auth/recovery")).toEqual([]);
  });

  it("F8S3-04 · запрос кода — признак своего вида, тело ровно {email, csrfToken}, ступень 2 с одним текстом", async () => {
    lane = installLane({ "POST /iam/v1/auth/recovery": REQUESTED });
    renderAt("/recovery?returnTo=/dashboard");
    await atStageTwo("a@kacho.local");
    expect(lane.of("POST", "/iam/v1/auth/recovery").map((c) => c.body)).toEqual([
      { email: "a@kacho.local", csrfToken: "tok-recovery-1" },
    ]);
    expect(lane.of("GET", "/iam/v1/auth/csrf").map((c) => c.query)).toContain("?form=recovery");
    expect(RECOVERY_CODE_SENT_TEXT).toBe(
      "Если адрес заведён и подтверждён, на него отправлено письмо с кодом восстановления. Код действует " +
        "ограниченное время и применяется один раз. Введите код из последнего письма и новый пароль.",
    );
    // Введённый адрес — только для чтения: адрес завершения — значение ступени 1.
    expect(email()).toHaveAttribute("readonly");
    expect(email().value).toBe("a@kacho.local");
    expect(code()).toBeInTheDocument();
    expect(newPassword()).toBeInTheDocument();
    expect(complete()).toBeEnabled();
    expect(resend()).toBeEnabled();
  });

  it("F8S3-05 · ответ запроса один на любой исход — экран его не обогащает", async () => {
    const screens: string[] = [];
    for (const address of ["known@kacho.local", "unknown@kacho.local"]) {
      lane = installLane({ "POST /iam/v1/auth/recovery": REQUESTED });
      const { container, unmount } = renderAt("/recovery");
      await atStageTwo(address);
      screens.push((container.textContent ?? "").replace(address, "<адрес>"));
      unmount();
      lane.restore();
      lane = null;
    }
    expect(screens[0]).toBe(screens[1]);
  });

  it("F8S3-06 · пустой адрес уходит службе, и отмечено поле, которое она назвала", async () => {
    const message = "Illegal argument email: required";
    lane = installLane({ "POST /iam/v1/auth/recovery": refusal(400, 3, message) });
    renderAt("/recovery");
    await screen.findByRole("form", { name: TITLE });
    fireEvent.click(sendCode());
    await screen.findByText(message);
    expect(lane.of("POST", "/iam/v1/auth/recovery")[0].body).toEqual({ email: "", csrfToken: "tok-recovery-1" });
    expect(email()).toHaveAttribute("aria-invalid", "true");
    expect(screen.queryByText(RECOVERY_CODE_SENT_TEXT)).toBeNull();
  });

  it("F8S3-07 · служба не ответила на запрос: текст службы, приглашение повторить, введённое цело", async () => {
    lane = installLane({ "POST /iam/v1/auth/recovery": refusal(503, 14, "request not performed; try again later") });
    renderAt("/recovery");
    await screen.findByRole("form", { name: TITLE });
    fireEvent.change(email(), { target: { value: "a@kacho.local" } });
    fireEvent.click(sendCode());
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("request not performed; try again later");
    expect(alert).toHaveTextContent("Отправьте форму ещё раз.");
    expect(sendCode()).toBeEnabled();
    expect(email().value).toBe("a@kacho.local");
    expect(screen.queryByText(RECOVERY_CODE_SENT_TEXT)).toBeNull();
  });

  it("F8S3-08 · завершение — признак своего вида, тело ровно {email, code, newPassword, csrfToken}, уход на адрес возврата", async () => {
    lane = installLane({ "POST /iam/v1/auth/recovery": REQUESTED, "POST /iam/v1/auth/recovery/complete": SIGNED_IN });
    const { leave } = renderAt("/recovery?returnTo=/dashboard");
    await atStageTwo("a@kacho.local");
    fillStageTwo("ABCDE-FGHJK", "new-password-1");
    fireEvent.click(complete());
    await waitFor(() => expect(leave).toHaveBeenCalledWith("/dashboard"));
    expect(lane.of("POST", "/iam/v1/auth/recovery/complete").map((c) => c.body)).toEqual([
      { email: "a@kacho.local", code: "ABCDE-FGHJK", newPassword: "new-password-1", csrfToken: "tok-recovery-complete-1" },
    ]);
    expect(lane.of("GET", "/iam/v1/auth/csrf").map((c) => c.query)).toContain("?form=recovery-complete");
    expect(lane.calls.every((c) => c.path.startsWith("/iam/v1/auth/"))).toBe(true);
  });

  it("F8S3-09 · неверный код: текст службы дословно, подсказка шага рядом, ступень 2 и «код ещё раз» на месте", async () => {
    lane = installLane({
      "POST /iam/v1/auth/recovery": REQUESTED,
      "POST /iam/v1/auth/recovery/complete": refusal(401, 16, "authentication failed"),
    });
    const { leave } = renderAt("/recovery");
    await atStageTwo();
    fillStageTwo("WRONG-CODE0");
    fireEvent.click(complete());
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toBe("authentication failed");
    const hint = refusalHint(new LaneRefusal(401, 16, "authentication failed", null, null, null), "recovery")!;
    expect(screen.getByText(hint)).toBeInTheDocument();
    expect(leave).not.toHaveBeenCalled();
    expect(resend()).toBeEnabled();
    expect(code().value).toBe("WRONG-CODE0");
  });

  it("F8S3-10 · новый пароль не по правилу: отмечено только поле пароля, тот же код проходит вторым заходом", async () => {
    const message = "Illegal argument newPassword: shorter than the declared minimum length";
    lane = installLane({
      "POST /iam/v1/auth/recovery": REQUESTED,
      "POST /iam/v1/auth/recovery/complete": (_c, nth) => (nth === 1 ? refusal(400, 3, message) : SIGNED_IN),
    });
    const { leave } = renderAt("/recovery?returnTo=/dashboard");
    await atStageTwo();
    fillStageTwo("ABCDE-FGHJK", "Ab1");
    fireEvent.click(complete());
    await screen.findByText(message);
    expect(newPassword()).toHaveAttribute("aria-invalid", "true");
    expect(code()).not.toHaveAttribute("aria-invalid");
    expect(lane.of("POST", "/iam/v1/auth/recovery")).toHaveLength(1);
    fireEvent.change(newPassword(), { target: { value: "new-password-1" } });
    fireEvent.click(complete());
    await waitFor(() => expect(leave).toHaveBeenCalledWith("/dashboard"));
    expect(lane.of("POST", "/iam/v1/auth/recovery/complete")[1].body).toMatchObject({ code: "ABCDE-FGHJK" });
    // Нового письма экран не потребовал: запрос кода был один.
    expect(lane.of("POST", "/iam/v1/auth/recovery")).toHaveLength(1);
  });

  it("F8S3-11 · частота: срок из Retry-After убывает, до него ни кнопка, ни клавиша ввода не отправляют", async () => {
    lane = installLane({
      "POST /iam/v1/auth/recovery": REQUESTED,
      "POST /iam/v1/auth/recovery/complete": {
        ...refusal(429, 8, "too many attempts; try again later", "TOO_MANY_ATTEMPTS"),
        headers: { "Retry-After": "30" },
      },
    });
    renderAt("/recovery");
    await atStageTwo();
    fillStageTwo();
    fireEvent.click(complete());
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("too many attempts; try again later");
    expect(alert).toHaveTextContent(retryNotice(30));
    expect(complete()).toBeDisabled();
    // Число убывает: отсчёт экрана, а не застывший заголовок.
    await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent(retryNotice(29)), { timeout: 2_500 });
    act(() => {
      fireEvent.submit(screen.getByRole("form", { name: "Смена пароля по коду" }));
    });
    fireEvent.keyDown(newPassword(), { key: "Enter", code: "Enter" });
    expect(lane.of("POST", "/iam/v1/auth/recovery/complete")).toHaveLength(1);
  });

  it("F8S3-11 · ответ без Retry-After срока не получает: текст есть, отсчёта нет, отправка открыта", async () => {
    lane = installLane({
      "POST /iam/v1/auth/recovery": REQUESTED,
      "POST /iam/v1/auth/recovery/complete": refusal(429, 8, "too many attempts; try again later", "TOO_MANY_ATTEMPTS"),
    });
    renderAt("/recovery");
    await atStageTwo();
    fillStageTwo();
    fireEvent.click(complete());
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("too many attempts; try again later");
    expect(alert).not.toHaveTextContent("Повторить можно через");
    expect(complete()).toBeEnabled();
  });

  it("F8S3-12 · служба не ответила на завершение: текст и приглашение, введённое цело", async () => {
    lane = installLane({
      "POST /iam/v1/auth/recovery": REQUESTED,
      "POST /iam/v1/auth/recovery/complete": refusal(503, 14, "request not performed; try again later"),
    });
    const { leave } = renderAt("/recovery");
    await atStageTwo();
    fillStageTwo("ABCDE-FGHJK");
    fireEvent.click(complete());
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("request not performed; try again later");
    expect(alert).toHaveTextContent("Отправьте форму ещё раз.");
    expect(complete()).toBeEnabled();
    expect(code().value).toBe("ABCDE-FGHJK");
    expect(leave).not.toHaveBeenCalled();
  });

  it("F8S3-13 · отвергнутый признак формы: свежий признак своего вида, поля целы, повтор несёт свежий", async () => {
    lane = installLane({
      "POST /iam/v1/auth/recovery": REQUESTED,
      "POST /iam/v1/auth/recovery/complete": (_c, nth) =>
        nth === 1 ? refusal(403, 7, "form token rejected", "FORM_TOKEN_REJECTED") : SIGNED_IN,
    });
    const { leave } = renderAt("/recovery?returnTo=/dashboard");
    await atStageTwo();
    fillStageTwo("ABCDE-FGHJK", "new-password-1");
    fireEvent.click(complete());
    expect(await screen.findByRole("alert")).toHaveTextContent("form token rejected");
    await waitFor(() =>
      expect(lane!.of("GET", "/iam/v1/auth/csrf").filter((c) => c.query === "?form=recovery-complete")).toHaveLength(2),
    );
    expect([code().value, newPassword().value]).toEqual(["ABCDE-FGHJK", "new-password-1"]);
    fireEvent.click(complete());
    await waitFor(() => expect(leave).toHaveBeenCalledWith("/dashboard"));
    expect(lane.of("POST", "/iam/v1/auth/recovery/complete")[1].body).toMatchObject({
      csrfToken: "tok-recovery-complete-2",
    });
  });

  it("F8S3-14 · «Отправить код ещё раз» — тот же запрос тем же адресом, текст тот же", async () => {
    lane = installLane({ "POST /iam/v1/auth/recovery": REQUESTED });
    renderAt("/recovery");
    await atStageTwo("a@kacho.local");
    fireEvent.click(resend());
    await waitFor(() => expect(lane!.of("POST", "/iam/v1/auth/recovery")).toHaveLength(2));
    expect(lane.of("POST", "/iam/v1/auth/recovery").map((c) => c.body?.email)).toEqual([
      "a@kacho.local",
      "a@kacho.local",
    ]);
    expect(Object.keys(lane.of("POST", "/iam/v1/auth/recovery")[1].body ?? {}).sort()).toEqual(["csrfToken", "email"]);
    expect(await screen.findByText(RECOVERY_CODE_SENT_TEXT)).toBeInTheDocument();
  });

  it("F8S3-15 · адрес возврата чужого происхождения уводит на корень во всех четырёх формах; свой — туда", async () => {
    for (const [returnTo, expected] of [
      ["https://evil.example/dashboard", "/"],
      ["//evil.example/dashboard", "/"],
      ["/\\evil.example/dashboard", "/"],
      ["javascript:alert(1)", "/"],
      ["/dashboard", "/dashboard"],
    ] as const) {
      lane = installLane({ "POST /iam/v1/auth/recovery": REQUESTED, "POST /iam/v1/auth/recovery/complete": SIGNED_IN });
      const { leave, unmount } = renderAt(`/recovery?returnTo=${encodeURIComponent(returnTo)}`);
      await atStageTwo();
      fillStageTwo();
      fireEvent.click(complete());
      await waitFor(() => expect(leave).toHaveBeenCalled());
      expect([returnTo, leave.mock.calls[0][0]]).toEqual([returnTo, expected]);
      unmount();
      lane.restore();
      lane = null;
    }
  });

  it("F8S3-15 · адрес возврата экран читает ТОЛЬКО через useReturnTo — своего разбора нет", () => {
    const file = fileURLToPath(new URL("./RecoveryPage.tsx", import.meta.url));
    const text = readFileSync(file, "utf8");
    expect(/import \{[^}]*\buseReturnTo\b[^}]*\} from "\.\/use-return-to"/.test(text)).toBe(true);
    expect(text.match(/URLSearchParams|useSearchParams|location\.search|RETURN_TO_PARAM|safeInternalPath/g)).toBeNull();
  });

  it("F8S3-16 · отказ у поля связан с полем: aria-invalid и aria-describedby на текст отказа", async () => {
    const message = "Illegal argument newPassword: shorter than the declared minimum length";
    lane = installLane({
      "POST /iam/v1/auth/recovery": REQUESTED,
      "POST /iam/v1/auth/recovery/complete": refusal(400, 3, message),
    });
    renderAt("/recovery");
    await atStageTwo();
    fillStageTwo("ABCDE-FGHJK", "Ab1");
    fireEvent.click(complete());
    await screen.findByText(message);
    const field = newPassword();
    expect(field).toHaveAttribute("aria-invalid", "true");
    const describedBy = field.getAttribute("aria-describedby") ?? "";
    expect(describedBy).not.toBe("");
    expect(document.getElementById(describedBy)?.textContent).toBe(message);
  });

  it("путь ко входу несёт адрес возврата и не несёт адреса почты", async () => {
    lane = installLane({ "POST /iam/v1/auth/recovery": REQUESTED });
    renderAt("/recovery?returnTo=%2Fiam%2Fusers");
    await atStageTwo("a@kacho.local");
    const link = screen.getByRole<HTMLAnchorElement>("link", { name: "Вернуться ко входу" });
    const url = new URL(link.href);
    expect([url.pathname, url.searchParams.get("returnTo")]).toEqual(["/login", "/iam/users"]);
    expect(link.href).not.toContain("kacho.local%40");
    expect(decodeURIComponent(link.href)).not.toContain("a@kacho.local");
  });
});
