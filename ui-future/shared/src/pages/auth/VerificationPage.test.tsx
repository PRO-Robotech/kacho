// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { jest } from "@jest/globals";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { UNKNOWN_SESSION_TEXT } from "@shared/api/login-lane";
import { SESSION, installLane, refusal, type LaneAnswer } from "@shared/test/lane-fake";

// Экран подтверждения адреса почты (приёмка F6b, S2: Р7–Р9). Сеть — дублёр
// полосы с телами и заголовками, объявленными службой (Р6 и Р9 службы), и
// никакими иными; экран — настоящий. Сквозная проба того же предмета —
// `ui-future/e2e/specs/address-confirmation.spec.ts`; здесь закреплены исходы
// двух глаголов, которые на стенде не построить (ответ без `Retry-After`,
// отвергнутый признак формы, недоступность, негодное приглашение).

const { VerificationPage } = await import("./VerificationPage");

const CONFIRM = "/iam/v1/auth/verify-email/confirm";
const REQUEST = "/iam/v1/auth/verify-email";
const ME = "GET /iam/v1/auth/me";
const ADDRESS = "a@kacho.local";

function me(emailVerified: boolean | undefined): LaneAnswer {
  const session: Record<string, unknown> = { expiresAt: SESSION.expiresAt, assuranceLevel: SESSION.assuranceLevel };
  if (emailVerified !== undefined) session.emailVerified = emailVerified;
  return {
    status: 200,
    body: {
      user: { id: "usr-1", email: ADDRESS, displayName: "a", subjectType: "user", permissions: [] },
      session,
    },
  };
}

const UNVERIFIED = me(false);
const VERIFIED = me(true);
const CONFIRMED: LaneAnswer = {
  status: 200,
  body: { user: { id: "usr-1", email: ADDRESS, displayName: "a" }, session: { ...SESSION, emailVerified: true } },
};
const AUTH_FAILED = refusal(401, 16, "authentication failed");
const ALREADY = refusal(400, 9, "email address is already verified", "EMAIL_ALREADY_VERIFIED");
const INVITE = refusal(
  400,
  9,
  "invite is no longer valid; ask an account administrator to invite again",
  "INVITE_NOT_VALID",
);
const FORM_REJECTED = refusal(403, 7, "form token rejected", "FORM_TOKEN_REJECTED");
const UNAVAILABLE = refusal(503, 14, "request not performed; try again later");
const TOO_MANY: LaneAnswer = {
  ...refusal(429, 8, "too many attempts; try again later", "TOO_MANY_ATTEMPTS"),
  headers: { "Retry-After": "45" },
};

const CHECK_CODE = "Проверьте код или отправьте новое письмо.";
const SENT = `Письмо с новым кодом отправлено на ${ADDRESS}. Прежний код больше не действует.`;
const countdown = (n: number) => `Отправить новое письмо можно через ${n} с`;

function renderAt(url: string, leave = jest.fn<(to: string) => void>()) {
  const view = render(
    <MemoryRouter initialEntries={[url]}>
      <VerificationPage leave={leave} />
    </MemoryRouter>,
  );
  return { leave, unmount: view.unmount };
}

const heading = () => screen.findByRole("heading", { name: "Подтвердите адрес почты" });
const code = () => screen.getByLabelText<HTMLInputElement>("Код из письма");
const confirmButton = () => screen.getByRole("button", { name: "Подтвердить" });
const resendButton = () => screen.getByRole("button", { name: "Отправить новое письмо" });
const logoutButton = () => screen.getByRole("button", { name: "Выйти" });

let lane: ReturnType<typeof installLane> | null = null;
afterEach(() => lane?.restore());

describe("экран подтверждения адреса: вид и обращения открытия (Р7, Р8)", () => {
  it("F6b-18 · вид экрана и обращения при открытии: вопрос о сессии и признаки формы, ни одного POST", async () => {
    lane = installLane({ [ME]: UNVERIFIED });
    renderAt("/verification");
    await heading();
    expect(
      screen.getByText(
        `Чтобы продолжить работу в консоли, подтвердите адрес ${ADDRESS}: введите код из письма, отправленного на этот адрес.`,
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByText("Письмо с кодом приходит после регистрации. Если письма нет или код не подходит, отправьте новое."),
    ).toBeInTheDocument();
    expect(code()).toBeInTheDocument();
    expect(confirmButton()).toBeEnabled();
    expect(resendButton()).toBeEnabled();
    expect(logoutButton()).toBeEnabled();
    expect(screen.queryByRole("button", { name: "Продолжить" })).toBeNull();
    await waitFor(() => expect(lane!.of("GET", "/iam/v1/auth/csrf").length).toBeGreaterThan(0));
    const seen = new Set(lane.calls.map((c) => `${c.method} ${c.path}`));
    expect([...seen].sort()).toEqual(["GET /iam/v1/auth/csrf", "GET /iam/v1/auth/me"]);
  });

  it("Р7 · без сессии экран уводит документ на вход без адреса возврата", async () => {
    lane = installLane({ [ME]: { status: 200, body: { user: null } } });
    const { leave } = renderAt("/verification?returnTo=%2Fdashboard");
    await waitFor(() => expect(leave).toHaveBeenCalledWith("/login"));
    expect(lane.calls.filter((c) => c.method === "POST")).toEqual([]);
  });

  it("Р7 · подтверждённая сессия уходит на адрес возврата: подтверждать нечего", async () => {
    lane = installLane({ [ME]: VERIFIED });
    const { leave } = renderAt("/verification?returnTo=%2Fdashboard");
    await waitFor(() => expect(leave).toHaveBeenCalledWith("/dashboard"));
    expect(screen.queryByLabelText("Код из письма")).toBeNull();
  });

  it("Р7 · подтверждённость не названа — своя страница, а не экран и не уход", async () => {
    lane = installLane({ [ME]: me(undefined) });
    const { leave } = renderAt("/verification");
    expect(
      await screen.findByText("Не удалось узнать, подтверждён ли адрес почты: служба доступа не сообщила этого в ответе о сессии."),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Проверить снова" })).toBeInTheDocument();
    expect(logoutButton()).toBeInTheDocument();
    expect(leave).not.toHaveBeenCalled();
  });
});

describe("предъявление кода (Р9)", () => {
  it("F6b-23 · код уходит как введён, с признаком своего вида, и экран уводит на адрес возврата", async () => {
    lane = installLane({ [ME]: UNVERIFIED, [`POST ${CONFIRM}`]: CONFIRMED });
    const { leave } = renderAt("/verification?returnTo=%2Fdashboard");
    await heading();
    fireEvent.change(code(), { target: { value: " abcd-EFGH-23 " } });
    fireEvent.click(confirmButton());
    await waitFor(() => expect(leave).toHaveBeenCalledWith("/dashboard"));
    expect(lane.of("POST", CONFIRM).map((c) => c.body)).toEqual([
      { code: " abcd-EFGH-23 ", csrfToken: "tok-verify-email-confirm-1" },
    ]);
  });

  it("F6b-24 · неверный код: текст службы дословно и строка про код; экран на месте", async () => {
    lane = installLane({ [ME]: UNVERIFIED, [`POST ${CONFIRM}`]: AUTH_FAILED });
    const { leave } = renderAt("/verification?returnTo=%2Fdashboard");
    await heading();
    fireEvent.change(code(), { target: { value: "0000000000" } });
    fireEvent.click(confirmButton());
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("authentication failed");
    expect(alert).toHaveTextContent(CHECK_CODE);
    // На `401` экран один раз спросил «кто я»: сессия жива — остаёмся.
    await waitFor(() => expect(lane!.of("GET", "/iam/v1/auth/me")).toHaveLength(2));
    expect(leave).not.toHaveBeenCalled();
  });

  it("F6b-30 · сессия снята: после 401 «кто я» — «сессии нет», и экран уводит на вход (оба глагола)", async () => {
    for (const verb of [CONFIRM, REQUEST]) {
      lane = installLane({
        [ME]: (_c, nth) => (nth === 1 ? UNVERIFIED : { status: 200, body: { user: null } }),
        [`POST ${verb}`]: AUTH_FAILED,
      });
      const { leave, unmount } = renderAt("/verification?returnTo=%2Fdashboard");
      await heading();
      fireEvent.click(verb === CONFIRM ? confirmButton() : resendButton());
      await waitFor(() => expect(leave).toHaveBeenCalledWith("/login"));
      expect([verb, lane.of("POST", verb).length]).toEqual([verb, 1]);
      unmount();
      lane.restore();
    }
  });

  it("F6b-39 · консоль попыток не считает: шесть отказов — шесть POST, по «кто я» после каждого, поле открыто", async () => {
    lane = installLane({ [ME]: UNVERIFIED, [`POST ${CONFIRM}`]: AUTH_FAILED });
    renderAt("/verification");
    await heading();
    for (let i = 1; i <= 6; i++) {
      fireEvent.change(code(), { target: { value: `code-${i}` } });
      fireEvent.click(confirmButton());
      await waitFor(() => expect(lane!.of("POST", CONFIRM)).toHaveLength(i));
      await waitFor(() => expect(lane!.of("GET", "/iam/v1/auth/me")).toHaveLength(1 + i));
      await waitFor(() => expect(confirmButton()).toBeEnabled());
      expect(screen.getByRole("alert")).toHaveTextContent("authentication failed");
      expect(screen.getByRole("alert")).toHaveTextContent(CHECK_CODE);
    }
    expect(code()).toBeEnabled();
    expect(document.body.textContent).not.toMatch(/попыт/i);
  });

  it("F6b-42 · «адрес уже подтверждён» уводит на адрес возврата с обоих глаголов; близнец 401 — нет", async () => {
    for (const verb of [CONFIRM, REQUEST]) {
      lane = installLane({ [ME]: UNVERIFIED, [`POST ${verb}`]: ALREADY });
      const { leave, unmount } = renderAt("/verification?returnTo=%2Fdashboard");
      await heading();
      fireEvent.click(verb === CONFIRM ? confirmButton() : resendButton());
      await waitFor(() => expect(leave).toHaveBeenCalledWith("/dashboard"));
      unmount();
      lane.restore();
    }
    lane = installLane({ [ME]: UNVERIFIED, [`POST ${CONFIRM}`]: AUTH_FAILED });
    const { leave } = renderAt("/verification?returnTo=%2Fdashboard");
    await heading();
    fireEvent.click(confirmButton());
    await screen.findByRole("alert");
    expect(leave).not.toHaveBeenCalled();
  });

  it("F6b-44 · негодное приглашение: текст дословно, без строки про код, без «кто я»; выход — тот же", async () => {
    lane = installLane({
      [ME]: UNVERIFIED,
      [`POST ${CONFIRM}`]: INVITE,
      "POST /iam/v1/auth/logout": { status: 200, body: {} },
    });
    const { leave } = renderAt("/verification?returnTo=%2Fdashboard");
    await heading();
    fireEvent.change(code(), { target: { value: "ABCDEFGH23" } });
    fireEvent.click(confirmButton());
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("invite is no longer valid; ask an account administrator to invite again");
    expect(alert).not.toHaveTextContent(CHECK_CODE);
    expect(lane.of("GET", "/iam/v1/auth/me")).toHaveLength(1);
    expect(lane.of("POST", CONFIRM)).toHaveLength(1);
    await waitFor(() => expect(confirmButton()).toBeEnabled());
    expect(code()).toBeEnabled();
    expect(resendButton()).toBeEnabled();
    expect(logoutButton()).toBeEnabled();
    expect(leave).not.toHaveBeenCalled();
    // Выход человека — «Выйти»: тот же выход, что F6b-22.
    fireEvent.click(logoutButton());
    await waitFor(() => expect(leave).toHaveBeenCalledWith("/login"));
    expect(lane.calls.filter((c) => c.query === "?form=logout")).toHaveLength(1);
    expect(lane.of("POST", "/iam/v1/auth/logout")).toHaveLength(1);
  });

  it("F6b-44 · контроль решения по причине: неназванная причина того же статуса — показать, без повтора", async () => {
    lane = installLane({ [ME]: UNVERIFIED, [`POST ${CONFIRM}`]: refusal(400, 9, "something new", "SOMETHING_NEW") });
    const { leave } = renderAt("/verification?returnTo=%2Fdashboard");
    await heading();
    fireEvent.click(confirmButton());
    expect(await screen.findByRole("alert")).toHaveTextContent("something new");
    expect(lane.of("POST", CONFIRM)).toHaveLength(1);
    expect(leave).not.toHaveBeenCalled();
  });
});

describe("запрос нового письма (Р8, Р9)", () => {
  it("F6b-19 · успех с Retry-After: текст, отсчёт службы, кнопка и клавиша ввода закрыты", async () => {
    lane = installLane({ [ME]: UNVERIFIED, [`POST ${REQUEST}`]: { status: 200, body: {}, headers: { "Retry-After": "60" } } });
    renderAt("/verification");
    await heading();
    fireEvent.click(resendButton());
    expect(await screen.findByText(SENT)).toBeInTheDocument();
    expect(screen.getByText(countdown(60))).toBeInTheDocument();
    expect(resendButton()).toBeDisabled();
    expect(lane.of("POST", REQUEST).map((c) => c.body)).toEqual([{ csrfToken: "tok-verify-email-1" }]);
    // Мимо кнопки — клавишей ввода и нажатием на закрытую — второго POST нет.
    act(() => {
      fireEvent.keyDown(resendButton(), { key: "Enter" });
      fireEvent.click(resendButton());
    });
    expect(lane.of("POST", REQUEST)).toHaveLength(1);
  });

  it("F6b-19 · отсчёт идёт от числа службы и снимает закрытие, когда срок вышел", async () => {
    lane = installLane({ [ME]: UNVERIFIED, [`POST ${REQUEST}`]: { status: 200, body: {}, headers: { "Retry-After": "1" } } });
    renderAt("/verification");
    await heading();
    fireEvent.click(resendButton());
    expect(await screen.findByText(countdown(1))).toBeInTheDocument();
    expect(resendButton()).toBeDisabled();
    await waitFor(() => expect(resendButton()).toBeEnabled(), { timeout: 4000 });
    expect(screen.queryByText(/Отправить новое письмо можно через/)).toBeNull();
  });

  it("F6b-20 · ответ без Retry-After отсчёта не получает: срок не выдуман", async () => {
    lane = installLane({ [ME]: UNVERIFIED, [`POST ${REQUEST}`]: { status: 200, body: {} } });
    renderAt("/verification");
    await heading();
    fireEvent.click(resendButton());
    expect(await screen.findByText(SENT)).toBeInTheDocument();
    expect(screen.queryByText(/Отправить новое письмо можно через/)).toBeNull();
    await waitFor(() => expect(resendButton()).toBeEnabled());
  });

  it("F6b-21 · отказ по частоте: текст службы дословно, её срок, кнопка закрыта, письма «отправлено» нет", async () => {
    lane = installLane({ [ME]: UNVERIFIED, [`POST ${REQUEST}`]: TOO_MANY });
    renderAt("/verification");
    await heading();
    fireEvent.click(resendButton());
    expect(await screen.findByRole("alert")).toHaveTextContent("too many attempts; try again later");
    expect(screen.getByText(countdown(45))).toBeInTheDocument();
    expect(resendButton()).toBeDisabled();
    expect(screen.queryByText(SENT)).toBeNull();
  });
});

describe("отказы, общие двум глаголам (Р9)", () => {
  it("F6b-38 · отвергнутый признак: один свежий признак своего вида, повторяет человек (оба глагола)", async () => {
    for (const [verb, kind] of [
      [CONFIRM, "verify-email-confirm"],
      [REQUEST, "verify-email"],
    ] as const) {
      lane = installLane({
        [ME]: UNVERIFIED,
        [`POST ${verb}`]: (_c, nth) => (nth === 1 ? FORM_REJECTED : verb === CONFIRM ? CONFIRMED : { status: 200, body: {} }),
      });
      const { leave, unmount } = renderAt("/verification?returnTo=%2Fdashboard");
      await heading();
      const button = () => (verb === CONFIRM ? confirmButton() : resendButton());
      await waitFor(() => expect(lane!.calls.filter((c) => c.query === `?form=${kind}`)).toHaveLength(1));
      fireEvent.click(button());
      expect(await screen.findByRole("alert")).toHaveTextContent("form token rejected");
      await waitFor(() => expect(lane!.calls.filter((c) => c.query === `?form=${kind}`)).toHaveLength(2));
      expect(lane.of("POST", verb)).toHaveLength(1);
      await waitFor(() => expect(button()).toBeEnabled());
      fireEvent.click(button());
      await waitFor(() => expect(lane!.of("POST", verb)).toHaveLength(2));
      expect(lane.of("POST", verb)[1].body).toMatchObject({ csrfToken: `tok-${kind}-2` });
      if (verb === CONFIRM) await waitFor(() => expect(leave).toHaveBeenCalledWith("/dashboard"));
      else expect(await screen.findByText(SENT)).toBeInTheDocument();
      unmount();
      lane.restore();
    }
  });

  it("F6b-38 · близнец: признак принят с первого нажатия — лишнего признака нет", async () => {
    lane = installLane({ [ME]: UNVERIFIED, [`POST ${CONFIRM}`]: CONFIRMED });
    const { leave } = renderAt("/verification?returnTo=%2Fdashboard");
    await heading();
    await waitFor(() => expect(lane!.calls.filter((c) => c.query === "?form=verify-email-confirm")).toHaveLength(1));
    fireEvent.click(confirmButton());
    await waitFor(() => expect(leave).toHaveBeenCalledWith("/dashboard"));
    expect(lane.calls.filter((c) => c.query === "?form=verify-email-confirm")).toHaveLength(1);
  });

  it("F6b-43 · прочие отказы — дословно, и ничего не повторяется само", async () => {
    const cases: Array<[string, string, LaneAnswer | "no-answer", string]> = [
      ["а", CONFIRM, refusal(400, 3, "code: required"), "code: required"],
      ["б", CONFIRM, UNAVAILABLE, "request not performed; try again later"],
      ["в", CONFIRM, "no-answer", UNKNOWN_SESSION_TEXT],
      ["б", REQUEST, UNAVAILABLE, "request not performed; try again later"],
      ["в", REQUEST, "no-answer", UNKNOWN_SESSION_TEXT],
    ];
    for (const [label, verb, answer, text] of cases) {
      lane = installLane({
        [ME]: UNVERIFIED,
        [`POST ${verb}`]: () => {
          if (answer === "no-answer") throw new TypeError("Failed to fetch");
          return answer;
        },
      });
      const { leave, unmount } = renderAt("/verification?returnTo=%2Fdashboard");
      await heading();
      fireEvent.click(verb === CONFIRM ? confirmButton() : resendButton());
      expect([label, verb, (await screen.findByRole("alert")).textContent]).toEqual([
        label,
        verb,
        expect.stringContaining(text),
      ]);
      expect([label, verb, lane.of("POST", verb).length]).toEqual([label, verb, 1]);
      if (label === "а") expect(lane.of("POST", verb)[0].body).toMatchObject({ code: "" });
      expect(leave).not.toHaveBeenCalled();
      unmount();
      lane.restore();
    }
  });
});

describe("F6b-36 · адрес возврата — только своего происхождения, на КАЖДОМ выходе с экрана", () => {
  const HOSTILE = [
    "https://evil.example/dashboard",
    "//evil.example/dashboard",
    "/\\evil.example/dashboard",
    "javascript:alert(1)",
  ];
  type Rendered = ReturnType<typeof renderAt>;
  const exits: Array<[string, (url: string) => Promise<Rendered>]> = [
    [
      "успех предъявления",
      async (url) => {
        lane = installLane({ [ME]: UNVERIFIED, [`POST ${CONFIRM}`]: CONFIRMED });
        const view = renderAt(url);
        await heading();
        fireEvent.click(confirmButton());
        return view;
      },
    ],
    [
      "«уже подтверждён» на предъявлении",
      async (url) => {
        lane = installLane({ [ME]: UNVERIFIED, [`POST ${CONFIRM}`]: ALREADY });
        const view = renderAt(url);
        await heading();
        fireEvent.click(confirmButton());
        return view;
      },
    ],
    [
      "«уже подтверждён» на запросе письма",
      async (url) => {
        lane = installLane({ [ME]: UNVERIFIED, [`POST ${REQUEST}`]: ALREADY });
        const view = renderAt(url);
        await heading();
        fireEvent.click(resendButton());
        return view;
      },
    ],
    [
      "открытие подтверждённой сессией",
      (url) => {
        lane = installLane({ [ME]: VERIFIED });
        return Promise.resolve(renderAt(url));
      },
    ],
    [
      "«Проверить снова» после «неизвестно»",
      async (url) => {
        lane = installLane({ [ME]: (_c, nth) => (nth === 1 ? refusal(503, 14, "unavailable") : VERIFIED) });
        const view = renderAt(url);
        fireEvent.click(await screen.findByRole("button", { name: "Проверить снова" }));
        return view;
      },
    ],
  ];

  for (const [exit, run] of exits) {
    it(`F6b-36 · ${exit}: чужой адрес возврата — корень консоли; свой — соблюдён`, async () => {
      for (const [returnTo, expected] of [...HOSTILE.map((h) => [h, "/"]), ["/dashboard", "/dashboard"]]) {
        const { leave, unmount } = await run(`/verification?returnTo=${encodeURIComponent(returnTo)}`);
        await waitFor(() => expect(leave).toHaveBeenCalled());
        expect([exit, returnTo, leave.mock.calls[0][0]]).toEqual([exit, returnTo, expected]);
        unmount();
        lane?.restore();
      }
    });
  }
});
