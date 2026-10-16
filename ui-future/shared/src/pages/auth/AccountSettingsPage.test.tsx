// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { jest } from "@jest/globals";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { EDGE_CREDENTIAL_STATE_UNKNOWN, laneAnswerOf } from "@shared/test/edge-answers";
import { SESSION, SIGNED_IN, installLane, refusal, type LaneAnswer } from "@shared/test/lane-fake";

// Экран «Безопасность» на `/settings` (приёмка F8-S7; прежде — параметры
// учётной записи, приёмка F8, S2): разделы, сводка, таблица способов второго
// фактора, заведение с QR-кодом, опасные действия с последствием словами —
// глаголами службы, не покидая консоли.

const { AccountSettingsPage } = await import("./AccountSettingsPage");

const NOT_ENROLLED: LaneAnswer = { status: 200, body: { totp: { enrolled: false } } };
const PENDING: LaneAnswer = { status: 200, body: { totp: { enrolled: false, pendingUntil: "2026-09-23T12:15:00Z" } } };
const ENROLLED: LaneAnswer = {
  status: 200,
  body: { totp: { enrolled: true, confirmedAt: "2026-09-23T12:00:00Z" }, backupCodes: { remaining: 10, total: 10 } },
};
/** Ответ формы SUA (Р6): моменты, доступность ключей и способы подтверждения. */
function suaState(over: Record<string, unknown> = {}): LaneAnswer {
  return {
    status: 200,
    body: {
      totp: { enrolled: true, confirmedAt: "2026-10-14T18:04:00Z" },
      backupCodes: { remaining: 10, total: 10, issuedAt: "2026-10-14T18:04:00Z" },
      accessKeys: { available: true },
      stepUpMethods: ["lookup_secret", "totp"],
      ...over,
    },
  };
}
const SUA_NOTHING: LaneAnswer = {
  status: 200,
  body: { totp: { enrolled: false }, accessKeys: { available: true }, stepUpMethods: [] },
};
const ENROLLMENT: LaneAnswer = {
  status: 200,
  body: { secret: "JBSWY3DPEHPK3PXP", otpauthUri: "otpauth://totp/kacho:a?secret=JBSWY3DPEHPK3PXP", expiresAt: "x" },
};
const CODES = ["0A1B2C3D4E", "5F6G7H8J9K"];
const CONFIRMED: LaneAnswer = {
  status: 200,
  body: {
    backupCodes: CODES,
    session: SESSION,
    assurance: { level: "2", level2Reachable: true, missingForLevel2: [] },
  },
};
const KEYS_EMPTY: LaneAnswer = { status: 200, body: {} };
const ONE_SESSION: LaneAnswer = {
  status: 200,
  body: { sessions: [{ id: "hss-1", current: true, userAgent: "этот браузер" }], nextPageToken: "" },
};
/** Чтения экрана, о которых проба не говорит: перечень ключей и сессий. */
const SCREEN_READS = {
  "GET /iam/v1/users/usr-1/accessKeys": KEYS_EMPTY,
  "GET /iam/v1/auth/sessions": ONE_SESSION,
} as const;

function renderPage() {
  render(
    <MemoryRouter initialEntries={["/settings"]}>
      <AccountSettingsPage />
    </MemoryRouter>,
  );
}

let lane: ReturnType<typeof installLane> | null = null;
afterEach(() => lane?.restore());

const region = (name: string) => screen.findByRole("region", { name });

describe("экран «Безопасность» (F8-S7, Р1)", () => {
  it("F8-S7-01 · заголовок «Безопасность», разделы в порядке задач, навигация на пять разделов переводит фокус", async () => {
    lane = installLane({
      "GET /iam/v1/auth/me": SIGNED_IN,
      "GET /iam/v1/auth/second-factor": suaState(),
      ...SCREEN_READS,
    });
    renderPage();
    expect(await screen.findByRole("heading", { level: 1, name: "Безопасность" })).toBeInTheDocument();
    await region("Активные сессии");
    const titles = screen
      .getAllByRole("region")
      .map((r) => document.getElementById(r.getAttribute("aria-labelledby") ?? "")?.textContent);
    expect(titles).toEqual([
      "Сводка",
      "Способ входа",
      "Второй фактор",
      "Ключи доступа",
      "Запасные коды",
      "Активные сессии",
      "Удаление учётной записи",
    ]);
    const nav = screen.getByRole("navigation", { name: "Разделы страницы" });
    expect(
      within(nav)
        .getAllByRole("link")
        .map((a) => a.textContent),
    ).toEqual(["Способ входа", "Второй фактор", "Ключи доступа", "Запасные коды", "Активные сессии"]);
    fireEvent.click(within(nav).getByRole("link", { name: "Ключи доступа" }));
    expect(screen.getByRole("heading", { level: 2, name: "Ключи доступа" })).toHaveFocus();
  });

  it("F8-S7-02 · без способов подтверждения: «не настроено», рекомендация и «Настроить» к разделу второго фактора", async () => {
    lane = installLane({
      "GET /iam/v1/auth/me": SIGNED_IN,
      "GET /iam/v1/auth/second-factor": SUA_NOTHING,
      ...SCREEN_READS,
    });
    renderPage();
    const summary = await region("Сводка");
    await waitFor(() => expect(summary).toHaveTextContent("Подтверждение действий: не настроено"));
    expect(summary).toHaveTextContent("Включите второй фактор: без него действия, меняющие доступ, недоступны");
    expect(summary).toHaveTextContent("Вход: a@kacho.local");
    await waitFor(() => expect(summary).toHaveTextContent("Активные сессии: 1"));
    fireEvent.click(within(summary).getByRole("button", { name: "Настроить" }));
    expect(screen.getByRole("heading", { level: 2, name: "Второй фактор" })).toHaveFocus();
  });

  it("F8-S7-02 · близнец: способы есть — названы словами, рекомендации нет", async () => {
    lane = installLane({
      "GET /iam/v1/auth/me": SIGNED_IN,
      "GET /iam/v1/auth/second-factor": suaState({ stepUpMethods: ["lookup_secret", "totp", "webauthn"] }),
      ...SCREEN_READS,
    });
    renderPage();
    const summary = await region("Сводка");
    await waitFor(() =>
      expect(summary).toHaveTextContent("Подтверждение действий: ключ доступа, код из приложения, запасной код"),
    );
    expect(summary).not.toHaveTextContent("Включите второй фактор");
  });

  it("служба способов не называет (ответ прежней формы): строки «Подтверждение действий» и рекомендации нет", async () => {
    lane = installLane({
      "GET /iam/v1/auth/me": SIGNED_IN,
      "GET /iam/v1/auth/second-factor": NOT_ENROLLED,
      ...SCREEN_READS,
    });
    renderPage();
    const summary = await region("Сводка");
    await waitFor(() => expect(summary).toHaveTextContent("Активные сессии: 1"));
    expect(summary).not.toHaveTextContent("Подтверждение действий");
    expect(summary).not.toHaveTextContent("Включите второй фактор");
  });
});

describe("второй фактор и запасные коды (F8-S7, Р2, Р4, Р6, Р8)", () => {
  it("F8-S7-10 · таблица способов: состояние, «Добавлен», «Последнее использование» из ответа службы", async () => {
    lane = installLane({
      "GET /iam/v1/auth/me": SIGNED_IN,
      "GET /iam/v1/auth/second-factor": suaState({
        totp: { enrolled: true, confirmedAt: "2026-10-14T18:04:00Z", lastUsedAt: "2026-10-15T09:30:00Z" },
        backupCodes: { remaining: 9, total: 10, issuedAt: "2026-10-14T18:04:00Z", lastUsedAt: "2026-10-15T09:31:00Z" },
      }),
      ...SCREEN_READS,
    });
    renderPage();
    const factor = await region("Второй фактор");
    const totp = await within(factor).findByRole("row", { name: /Приложение-аутентификатор/ });
    expect(totp).toHaveTextContent("Настроено");
    expect(totp).toHaveTextContent(/14 окт\. 2026/);
    expect(totp).toHaveTextContent(/15 окт\. 2026/);
    expect(within(factor).getByRole("row", { name: /Ключи доступа/ })).toHaveTextContent("Нет");
    const codes = within(factor).getByRole("row", { name: /Запасные коды/ });
    expect(codes).toHaveTextContent("Осталось 9 из 10");
    expect(codes).not.toHaveTextContent("Ещё не использовались");
  });

  it("F8-S7-11 · предъявлений не было: «Ещё не использовалось» у кода и «Ещё не использовались» у кодов; прочерков нет", async () => {
    lane = installLane({
      "GET /iam/v1/auth/me": SIGNED_IN,
      "GET /iam/v1/auth/second-factor": suaState(),
      ...SCREEN_READS,
    });
    renderPage();
    const factor = await region("Второй фактор");
    expect(await within(factor).findByRole("row", { name: /Приложение-аутентификатор/ })).toHaveTextContent(
      "Ещё не использовалось",
    );
    expect(within(factor).getByRole("row", { name: /Запасные коды/ })).toHaveTextContent("Ещё не использовались");
    expect(within(factor).queryByText(/^[—–-]$/)).toBeNull();
  });

  // kacho#3238: край отказал в перечне ключей (503 моста, прогон console-e2e на 897e11c8f) —
  // раздел «Второй фактор» оставался вращающимся индикатором без таблицы способов.
  const KEYS_REFUSED = refusal(503, 14, 'connection error: desc = "transport: Error while dialing"');

  it("3238 · перечень ключей отказан: таблица способов на месте, строка ключей называет непрочитанный перечень", async () => {
    lane = installLane({
      "GET /iam/v1/auth/me": SIGNED_IN,
      "GET /iam/v1/auth/second-factor": suaState(),
      "GET /iam/v1/users/usr-1/accessKeys": KEYS_REFUSED,
      "GET /iam/v1/auth/sessions": ONE_SESSION,
    });
    renderPage();
    const factor = await region("Второй фактор");
    const totp = await within(factor).findByRole("row", { name: /Приложение-аутентификатор/ });
    expect(totp).toHaveTextContent("Настроено");
    expect(totp).toHaveTextContent("Ещё не использовалось");
    const keys = within(factor).getByRole("row", { name: /Ключи доступа/ });
    expect(keys).toHaveTextContent("Перечень не прочитан");
    expect(keys).not.toHaveTextContent(/(^|\s)Нет(\s|$)/);
    expect(within(factor).getByRole("row", { name: /Запасные коды/ })).toHaveTextContent("Осталось 10 из 10");
    // Строки «Вход ключом доступа» нет: число ключей неизвестно, «Не настроен» не выдумывается.
    expect(await region("Способ входа")).not.toHaveTextContent("Вход ключом доступа");
  });

  it("3238 · близнец: ничего не заведено, перечень ключей отказан — приглашения «не настроен» нет, таблица называет отказ", async () => {
    lane = installLane({
      "GET /iam/v1/auth/me": SIGNED_IN,
      "GET /iam/v1/auth/second-factor": SUA_NOTHING,
      "GET /iam/v1/users/usr-1/accessKeys": KEYS_REFUSED,
      "GET /iam/v1/auth/sessions": ONE_SESSION,
    });
    renderPage();
    const factor = await region("Второй фактор");
    const keys = await within(factor).findByRole("row", { name: /Ключи доступа/ });
    expect(keys).toHaveTextContent("Перечень не прочитан");
    expect(factor).not.toHaveTextContent("Второй фактор не настроен");
    expect(within(factor).getByRole("row", { name: /Приложение-аутентификатор/ })).toHaveTextContent("Не настроено");
  });

  it("ответ прежней формы: «Ещё не использовалось» не выдумывается — моментов служба не называла", async () => {
    lane = installLane({
      "GET /iam/v1/auth/me": SIGNED_IN,
      "GET /iam/v1/auth/second-factor": ENROLLED,
      ...SCREEN_READS,
    });
    renderPage();
    const factor = await region("Второй фактор");
    const totp = await within(factor).findByRole("row", { name: /Приложение-аутентификатор/ });
    expect(totp).toHaveTextContent("Настроено");
    expect(factor).not.toHaveTextContent("Ещё не использ");
  });

  it("F8-S7-61 · ничего не заведено: пустые состояния с действием на пустом ответе", async () => {
    lane = installLane({
      "GET /iam/v1/auth/me": SIGNED_IN,
      "GET /iam/v1/auth/second-factor": SUA_NOTHING,
      ...SCREEN_READS,
    });
    renderPage();
    const factor = await region("Второй фактор");
    expect(
      await within(factor).findByText("Второй фактор не настроен. Он нужен для действий, меняющих доступ."),
    ).toBeInTheDocument();
    expect(within(factor).getByRole("button", { name: "Настроить приложение-аутентификатор" })).toBeInTheDocument();
    const keys = await region("Ключи доступа");
    expect(
      await within(keys).findByText("Ключей доступа нет. Входите и подтверждайте действия без пароля и кода."),
    ).toBeInTheDocument();
    expect(await region("Запасные коды")).toHaveTextContent(
      "Запасные коды появятся после настройки приложения-аутентификатора.",
    );
    const sessions = await region("Активные сессии");
    expect(await within(sessions).findByText("Других активных сессий нет.")).toBeInTheDocument();
    expect(within(sessions).queryByRole("button", { name: "Выйти на всех, кроме этой" })).toBeNull();
  });

  it("F8-26/F8-27/F8-S7-12/F8-S7-40 · заведение: QR-код и секрет в окне, подтверждение, коды один раз, «Готово» после отметки", async () => {
    let enrolled = false;
    lane = installLane({
      "GET /iam/v1/auth/me": SIGNED_IN,
      "GET /iam/v1/auth/second-factor": () => (enrolled ? ENROLLED : NOT_ENROLLED),
      "POST /iam/v1/auth/second-factor/enroll": ENROLLMENT,
      "POST /iam/v1/auth/second-factor/confirm": () => {
        enrolled = true;
        return CONFIRMED;
      },
      ...SCREEN_READS,
    });
    renderPage();
    const factor = await region("Второй фактор");
    fireEvent.click(await within(factor).findByRole("button", { name: "Настроить приложение-аутентификатор" }));
    const enroll = await screen.findByRole("dialog");
    expect(enroll).toHaveTextContent("Настроить приложение-аутентификатор");
    expect(within(enroll).getByRole("img", { name: "QR-код для приложения-аутентификатора" })).toBeInTheDocument();
    expect(within(enroll).getByText("JBSWY3DPEHPK3PXP")).toBeInTheDocument();
    // Адрес с секретом в разметку не попадает: QR-код рисуется на холсте.
    expect(enroll.innerHTML).not.toContain("otpauth://");
    fireEvent.change(within(enroll).getByLabelText("Код из приложения"), { target: { value: "123456" } });
    fireEvent.click(within(enroll).getByRole("button", { name: "Подтвердить" }));
    await waitFor(() => expect(screen.getByRole("dialog")).toHaveTextContent("Сохраните запасные коды"));
    const saved = screen.getByRole("dialog");
    const list = within(saved).getByRole("list", { name: "Запасные коды" });
    for (const code of CODES) expect(list).toHaveTextContent(code);
    expect(within(saved).getByRole("button", { name: "Готово" })).toBeDisabled();
    expect(within(saved).getByRole("button", { name: "Копировать" })).toBeEnabled();
    expect(within(saved).getByRole("button", { name: "Скачать текстовым файлом" })).toBeEnabled();
    fireEvent.click(within(saved).getByLabelText("Я сохранил коды"));
    fireEvent.click(within(saved).getByRole("button", { name: "Готово" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    for (const code of CODES) expect(document.body).not.toHaveTextContent(code);
    expect(await within(factor).findByRole("row", { name: /Приложение-аутентификатор/ })).toHaveTextContent(
      "Настроено",
    );
    expect(screen.getByRole("status")).toHaveTextContent("Приложение-аутентификатор настроено");
    // Признак — на ОДНУ отправку (условие C12): заведение унесло первый,
    // подтверждение несёт второй, добытый за ответом заведения.
    expect(lane.of("POST", "/iam/v1/auth/second-factor/enroll")[0].body).toEqual({ csrfToken: "tok-second-factor-1" });
    expect(lane.of("POST", "/iam/v1/auth/second-factor/confirm")[0].body).toEqual({
      code: "123456",
      csrfToken: "tok-second-factor-2",
    });
  });

  it("F8-28 · неверный первый код: отказ дословно, запасных кодов нет", async () => {
    lane = installLane({
      "GET /iam/v1/auth/me": SIGNED_IN,
      "GET /iam/v1/auth/second-factor": (_c, nth) => (nth === 1 ? NOT_ENROLLED : PENDING),
      "POST /iam/v1/auth/second-factor/enroll": ENROLLMENT,
      "POST /iam/v1/auth/second-factor/confirm": refusal(401, 16, "authentication failed"),
      ...SCREEN_READS,
    });
    renderPage();
    const factor = await region("Второй фактор");
    fireEvent.click(await within(factor).findByRole("button", { name: "Настроить приложение-аутентификатор" }));
    const enroll = await screen.findByRole("dialog");
    fireEvent.change(within(enroll).getByLabelText("Код из приложения"), { target: { value: "000000" } });
    fireEvent.click(within(enroll).getByRole("button", { name: "Подтвердить" }));
    expect(await within(enroll).findByRole("alert")).toHaveTextContent("authentication failed");
    expect(screen.queryByRole("list", { name: "Запасные коды" })).toBeNull();
    await waitFor(() =>
      expect(within(factor).getByRole("row", { name: /Приложение-аутентификатор/ })).toHaveTextContent(
        "Настройка не завершена",
      ),
    );
  });

  it("F8-29 · свежесть: окно подтверждения и возврат на ТОТ ЖЕ шаг — заведение повторено само", async () => {
    lane = installLane({
      "GET /iam/v1/auth/me": SIGNED_IN,
      "GET /iam/v1/auth/second-factor": NOT_ENROLLED,
      "POST /iam/v1/auth/second-factor/enroll": (_c, nth) =>
        nth === 1
          ? refusal(403, 7, "re-authentication required: present a credential again", "SESSION_NOT_FRESH")
          : ENROLLMENT,
      "POST /iam/v1/auth/step-up": {
        status: 200,
        body: {
          session: SESSION,
          assurance: { level: "1", level2Reachable: true, missingForLevel2: ["second-factor"] },
        },
      },
      ...SCREEN_READS,
    });
    renderPage();
    const factor = await region("Второй фактор");
    fireEvent.click(await within(factor).findByRole("button", { name: "Настроить приложение-аутентификатор" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(await within(dialog).findByLabelText("Код из приложения"), { target: { value: "123456" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Подтвердить" }));
    expect(await screen.findByText("JBSWY3DPEHPK3PXP")).toBeInTheDocument();
    expect(lane.of("POST", "/iam/v1/auth/second-factor/enroll")).toHaveLength(2);
    expect(lane.of("POST", "/iam/v1/auth/step-up")[0].body).toMatchObject({ method: "totp", code: "123456" });
  });

  it("F8-S7-42 · «Отключить»: последствие словами, последний способ «2» — вторая фраза, фокус на «Отмена», код в диалоге", async () => {
    lane = installLane({
      "GET /iam/v1/auth/me": SIGNED_IN,
      "GET /iam/v1/auth/second-factor": suaState(),
      ...SCREEN_READS,
    });
    renderPage();
    const factor = await region("Второй фактор");
    fireEvent.click(await within(factor).findByRole("button", { name: "Отключить" }));
    const dialog = await screen.findByRole("dialog");
    expect(dialog).toHaveTextContent("Коды из приложения и запасные коды перестанут работать.");
    expect(dialog).toHaveTextContent(
      "Действия, требующие второго фактора, станут недоступны, пока вы не настроите его снова.",
    );
    expect(within(dialog).getByRole("radio", { name: "Код из приложения" })).toHaveAttribute("aria-checked", "true");
    expect(within(dialog).getByLabelText("Код из приложения")).toBeInTheDocument();
  });

  it("F8-S7-42 · близнец: ключ остаётся способом «2» — второй фразы нет", async () => {
    lane = installLane({
      "GET /iam/v1/auth/me": SIGNED_IN,
      "GET /iam/v1/auth/second-factor": suaState({ stepUpMethods: ["lookup_secret", "totp", "webauthn"] }),
      ...SCREEN_READS,
    });
    renderPage();
    const factor = await region("Второй фактор");
    fireEvent.click(await within(factor).findByRole("button", { name: "Отключить" }));
    const dialog = await screen.findByRole("dialog");
    expect(dialog).toHaveTextContent("Коды из приложения и запасные коды перестанут работать.");
    expect(dialog).not.toHaveTextContent("станут недоступны");
  });

  it("F8-32 · снятие того, чего нет: текст службы назван в диалоге", async () => {
    lane = installLane({
      "GET /iam/v1/auth/me": SIGNED_IN,
      "GET /iam/v1/auth/second-factor": (_c, nth) => (nth === 1 ? ENROLLED : NOT_ENROLLED),
      "POST /iam/v1/auth/second-factor/remove": refusal(
        400,
        9,
        "second factor is not enrolled",
        "SECOND_FACTOR_NOT_ENROLLED",
      ),
      ...SCREEN_READS,
    });
    renderPage();
    const factor = await region("Второй фактор");
    fireEvent.click(await within(factor).findByRole("button", { name: "Отключить" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("radio", { name: "Запасной код" }));
    fireEvent.change(within(dialog).getByLabelText("Запасной код"), { target: { value: "ABCDEFGHJK" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Отключить" }));
    expect(await within(dialog).findByRole("alert")).toHaveTextContent("second factor is not enrolled");
    expect(lane.of("POST", "/iam/v1/auth/second-factor/remove")[0].body).toEqual({
      method: "lookup_secret",
      code: "ABCDEFGHJK",
      csrfToken: "tok-second-factor-1",
    });
  });

  it("F8-S7-41 · «Выпустить новые коды»: отмена ничего не выпускает; подтверждение кодом — 10 новых в окне один раз", async () => {
    const fresh = Array.from({ length: 10 }, (_, i) => `NEWCODE00${i}`);
    lane = installLane({
      "GET /iam/v1/auth/me": SIGNED_IN,
      "GET /iam/v1/auth/second-factor": suaState(),
      "POST /iam/v1/auth/second-factor/backup-codes": { status: 200, body: { backupCodes: fresh, session: SESSION } },
      ...SCREEN_READS,
    });
    renderPage();
    const codes = await region("Запасные коды");
    expect(await within(codes).findByText("Осталось 10 из 10")).toBeInTheDocument();
    expect(within(codes).getByRole("row", { name: /Выпущены/ })).toHaveTextContent(/14 окт\. 2026/);
    fireEvent.click(within(codes).getByRole("button", { name: "Выпустить новые коды" }));
    let dialog = await screen.findByRole("dialog");
    expect(dialog).toHaveTextContent("Прежние запасные коды перестанут работать сразу.");
    fireEvent.click(within(dialog).getByRole("button", { name: "Отмена" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(lane.of("POST", "/iam/v1/auth/second-factor/backup-codes")).toHaveLength(0);
    fireEvent.click(within(codes).getByRole("button", { name: "Выпустить новые коды" }));
    dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Код из приложения"), { target: { value: "123456" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Выпустить" }));
    await waitFor(() => expect(screen.getByRole("dialog")).toHaveTextContent("Сохраните запасные коды"));
    const saved = screen.getByRole("dialog");
    expect(within(saved).getAllByRole("listitem")).toHaveLength(10);
    // Положительная сторона: окно несёт ровно выпущенные коды — иначе отсутствие
    // их ниже было бы утверждением о ничём.
    const list = within(saved).getByRole("list", { name: "Запасные коды" });
    for (const c of fresh) expect(list).toHaveTextContent(c);
    expect(lane.of("POST", "/iam/v1/auth/second-factor/backup-codes")[0].body).toEqual({
      method: "totp",
      code: "123456",
      csrfToken: "tok-second-factor-1",
    });
    // Показ один раз и на пути перевыпуска: после «Готово» ни одного из 10 новых
    // кодов на странице нет (F8-S7-41, тот же признак, что F8-S7-40 у заведения).
    fireEvent.click(within(saved).getByLabelText("Я сохранил коды"));
    fireEvent.click(within(saved).getByRole("button", { name: "Готово" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    for (const c of fresh) expect(document.body).not.toHaveTextContent(c);
  });

  it("Р4 · остаток ≤ 2 — предупреждение «Запасные коды заканчиваются — выпустите новые»", async () => {
    lane = installLane({
      "GET /iam/v1/auth/me": SIGNED_IN,
      "GET /iam/v1/auth/second-factor": suaState({
        backupCodes: { remaining: 2, total: 10, issuedAt: "2026-10-14T18:04:00Z" },
      }),
      ...SCREEN_READS,
    });
    renderPage();
    const codes = await region("Запасные коды");
    expect(await within(codes).findByText("Запасные коды заканчиваются — выпустите новые")).toBeInTheDocument();
  });
});

describe("сессия края и способ входа (F8, C6, C8)", () => {
  it("F8-17 · адрес и признак его подтверждённости — из ответа края; действия «подтвердить» нет", async () => {
    lane = installLane({
      "GET /iam/v1/auth/me": SIGNED_IN,
      "GET /iam/v1/auth/second-factor": NOT_ENROLLED,
      ...SCREEN_READS,
    });
    renderPage();
    const signIn = await region("Способ входа");
    expect(signIn).toHaveTextContent("a@kacho.local");
    expect(signIn).toHaveTextContent("Адрес не подтверждён");
    expect(
      within(signIn)
        .getAllByRole("button")
        .map((b) => b.textContent),
    ).toEqual(["Сменить адрес", "Сменить пароль"]);
    expect(within(signIn).queryByRole("button", { name: /подтвердить/i })).toBeNull();
  });

  it("F8-23 · смена пароля окном: наш глагол с признаком своего вида, исход — областью статуса экрана", async () => {
    lane = installLane({
      "GET /iam/v1/auth/me": SIGNED_IN,
      "GET /iam/v1/auth/second-factor": NOT_ENROLLED,
      "POST /iam/v1/auth/password": { status: 200, body: { session: SESSION } },
      ...SCREEN_READS,
    });
    renderPage();
    const signIn = await region("Способ входа");
    fireEvent.click(within(signIn).getByRole("button", { name: "Сменить пароль" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Текущий пароль"), { target: { value: "old" } });
    fireEvent.change(within(dialog).getByLabelText("Новый пароль"), { target: { value: "new-one" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Сменить пароль" }));
    await waitFor(() => expect(screen.getByRole("status")).toHaveTextContent("Пароль сменён."));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(lane.of("POST", "/iam/v1/auth/password")[0].body).toEqual({
      currentPassword: "old",
      newPassword: "new-one",
      csrfToken: "tok-password-1",
    });
  });

  it("F8-25 · служба молчит краю на глаголе с носителем (KA1, Р1): ответ назван, повторить можно, окно на месте", async () => {
    lane = installLane({
      "GET /iam/v1/auth/me": SIGNED_IN,
      "GET /iam/v1/auth/second-factor": NOT_ENROLLED,
      "POST /iam/v1/auth/password": laneAnswerOf(EDGE_CREDENTIAL_STATE_UNKNOWN),
      ...SCREEN_READS,
    });
    renderPage();
    const signIn = await region("Способ входа");
    fireEvent.click(within(signIn).getByRole("button", { name: "Сменить пароль" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Сменить пароль" }));
    const alert = await within(dialog).findByRole("alert");
    expect(alert).toHaveTextContent("credential state could not be established");
    expect(alert).toHaveTextContent("Отправьте форму ещё раз.");
    expect(lane.of("POST", "/iam/v1/auth/password")).toHaveLength(1);
  });

  it("C6 · край не ответил о сессии — экран называет это и даёт спросить снова, а не зовёт входить", async () => {
    lane = installLane({
      "GET /iam/v1/auth/me": (_c, nth) => (nth === 1 ? refusal(503, 14, "unavailable") : SIGNED_IN),
      "GET /iam/v1/auth/second-factor": NOT_ENROLLED,
      ...SCREEN_READS,
    });
    renderPage();
    expect(await screen.findByRole("alert")).toHaveTextContent("unavailable");
    expect(screen.queryByRole("link", { name: "Войти" })).toBeNull();
    expect(screen.queryByText(/доступны после входа/)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Проверить снова" }));
    expect(await region("Способ входа")).toHaveTextContent("a@kacho.local");
  });

  it("C8 · поля подтверждённости в ответе края нет — признака нет, «не подтверждён» не выдумывается", async () => {
    lane = installLane({
      "GET /iam/v1/auth/me": {
        status: 200,
        body: { user: { id: "usr-1", email: "a@kacho.local", displayName: "a" }, session: { assuranceLevel: "1" } },
      },
      "GET /iam/v1/auth/second-factor": NOT_ENROLLED,
      ...SCREEN_READS,
    });
    renderPage();
    const signIn = await region("Способ входа");
    expect(signIn).toHaveTextContent("a@kacho.local");
    expect(signIn).not.toHaveTextContent("подтверждён");
  });

  it("без сессии — путь ко входу с возвратом сюда, и ни одного раздела", async () => {
    lane = installLane({});
    renderPage();
    const link = await screen.findByRole<HTMLAnchorElement>("link", { name: "Войти" });
    expect(new URL(link.href).searchParams.get("returnTo")).toBe("/settings");
    expect(screen.queryByRole("region")).toBeNull();
    expect(screen.queryByRole("navigation", { name: "Разделы страницы" })).toBeNull();
  });
});

// ═══ S4 — группа L. Ключи доступа на /settings (приёмка F8, ред. 12, Р11) ═══
//
// Раздел ходит глаголами Ф7 через существующие маршруты края; дублёр отвечает
// телами в той форме, в какой их отдаёт край (`protojson`: поля camelCase,
// `bytes` — строкой base64 стандартного алфавита с дополнением, `int64` —
// строкой). Интерфейс браузера ключей — подставной: модульная проба судит, что
// раздел передал браузеру и что перенёс из его ответа в тело глагола.

const USER_ID = "usr-1";
const KEYS = `/iam/v1/users/${USER_ID}/accessKeys`;
const BEGIN_REGISTRATION = `${KEYS}:beginRegistration`;

/** Байты испытания: в base64 они дают оба знака стандартного алфавита (`+`, `/`). */
const CHALLENGE_BYTES = [251, 255, 191, 0, 1, 2, 3, 250];
const REGISTRATION_CHALLENGE: LaneAnswer = {
  status: 200,
  body: {
    challenge: "+/+/AAECA/o=",
    rp: { id: "console.kacho.local", name: "Kachō" },
    user: { id: "dXNyLTE=", name: "a@kacho.local", displayName: "a" },
    pubKeyCredParams: [{ type: "public-key", alg: "-7" }],
    authenticatorSelection: { residentKey: "required", requireResidentKey: true, userVerification: "required" },
    attestation: "none",
    extensions: { credProps: true },
    expiresAt: "2026-10-07T12:05:00Z",
  },
};

function keyRecord(id: string, name: string, extra: Record<string, unknown> = {}) {
  return { id, userId: USER_ID, name, createdAt: "2026-10-07T10:00:00Z", ...extra };
}

function listOf(...keys: ReturnType<typeof keyRecord>[]): LaneAnswer {
  return { status: 200, body: keys.length > 0 ? { accessKeys: keys } : {} };
}

const OPERATION_PENDING: LaneAnswer = { status: 200, body: { id: "op-1", done: false } };
const OPERATION_DONE: LaneAnswer = { status: 200, body: { id: "op-1", done: true, response: {} } };

/** Отказ края с нарушением поля — `BadRequest.fieldViolations` (N36). */
function fieldRefusal(field: string, description: string): LaneAnswer {
  return {
    status: 400,
    body: {
      code: 3,
      message: "invalid argument",
      details: [{ "@type": "type.googleapis.com/google.rpc.BadRequest", fieldViolations: [{ field, description }] }],
    },
  };
}

const bytes = (...b: number[]) => new Uint8Array(b).buffer;
const b64 = (buf: ArrayBuffer) => Buffer.from(new Uint8Array(buf)).toString("base64");

/** Ответ браузера на создание удостоверения — с полями СВЕРХ объявленных (F8-55). */
function createdCredential(rk: boolean | undefined) {
  const rawId = bytes(1, 2, 3, 4, 250, 251);
  const clientDataJSON = bytes(123, 34, 116, 34, 125);
  const attestationObject = bytes(163, 99, 102, 109, 116);
  return {
    credential: {
      id: "AQIDBPr7",
      rawId,
      type: "public-key",
      authenticatorAttachment: "platform",
      response: {
        clientDataJSON,
        attestationObject,
        transports: ["internal"],
        publicKeyAlgorithm: -7,
        getTransports: () => ["internal"],
        getPublicKeyAlgorithm: () => -7,
      },
      getClientExtensionResults: () =>
        rk === undefined ? { appidExclude: false } : { credProps: { rk }, appidExclude: false },
    },
    wire: { id: b64(rawId), clientDataJson: b64(clientDataJSON), attestationObject: b64(attestationObject) },
  };
}

type CredentialsCreate = (options?: CredentialCreationOptions) => Promise<unknown>;

/** Подставной интерфейс ключей браузера; `present: false` — браузер без него. */
function installBrowserKeys(create: CredentialsCreate, present = true) {
  const w = window as unknown as Record<string, unknown>;
  const savedPkc = w.PublicKeyCredential;
  const savedCredentials = Object.getOwnPropertyDescriptor(navigator, "credentials");
  const spy = jest.fn(create);
  if (present) {
    w.PublicKeyCredential = function PublicKeyCredential() {};
    Object.defineProperty(navigator, "credentials", { configurable: true, value: { create: spy, get: jest.fn() } });
  } else {
    delete w.PublicKeyCredential;
    Object.defineProperty(navigator, "credentials", { configurable: true, value: undefined });
  }
  return {
    create: spy,
    restore() {
      if (savedPkc === undefined) delete w.PublicKeyCredential;
      else w.PublicKeyCredential = savedPkc;
      if (savedCredentials) Object.defineProperty(navigator, "credentials", savedCredentials);
      else delete (navigator as unknown as Record<string, unknown>).credentials;
    },
  };
}

/** Журнал браузера за время пробы — материал церемонии в него не уходит. */
function watchJournal() {
  const methods = ["log", "info", "warn", "error", "debug"] as const;
  const spies = methods.map((m) => jest.spyOn(console, m).mockImplementation(() => undefined));
  return {
    entries: () => spies.flatMap((s, i) => s.mock.calls.map((args) => `${methods[i]}: ${args.map(String).join(" ")}`)),
    restore: () => spies.forEach((s) => s.mockRestore()),
  };
}

let keys: ReturnType<typeof installBrowserKeys> | null = null;
let journal: ReturnType<typeof watchJournal> | null = null;
afterEach(() => {
  keys?.restore();
  keys = null;
  journal?.restore();
  journal = null;
});

const KEYS_BASE = {
  "GET /iam/v1/auth/me": SIGNED_IN,
  "GET /iam/v1/auth/second-factor": NOT_ENROLLED,
  "GET /iam/v1/auth/sessions": ONE_SESSION,
} as const;

const NO_KEYS = "Ключей доступа нет. Входите и подтверждайте действия без пароля и кода.";

async function keysRegion() {
  return screen.findByRole("region", { name: "Ключи доступа" });
}

/**
 * Добавить ключ окном «Добавить ключ доступа». Окно открыто — повторное
 * нажатие в разделе не нужно: заполняются его поля и «Добавить».
 */
function addKey(region: HTMLElement, name: string, description = "") {
  if (!screen.queryByRole("dialog")) {
    fireEvent.click(within(region).getByRole("button", { name: "Добавить ключ доступа" }));
  }
  const dialog = screen.getByRole("dialog");
  fireEvent.change(within(dialog).getByLabelText("Имя"), { target: { value: name } });
  fireEvent.change(within(dialog).getByLabelText("Описание"), { target: { value: description } });
  fireEvent.click(within(dialog).getByRole("button", { name: "Добавить" }));
}

/** Строка ключа в общей таблице раздела. */
function keyItem(region: HTMLElement, name: string) {
  return within(region)
    .queryAllByRole("row")
    .find((tr) => tr.querySelector("td") && tr.textContent?.includes(name));
}

const removeButton = (row: HTMLElement, name: string) =>
  within(row).getByRole("button", { name: `Удалить ключ доступа «${name}»` });

describe("ключи доступа на /settings (F8, S4, группа L)", () => {
  it("F8-55 · браузер получает испытание как есть, служба — ровно объявленные поля; раздел ждёт done и перечитывает перечень", async () => {
    journal = watchJournal();
    const { credential, wire } = createdCredential(true);
    let registered = false;
    lane = installLane({
      ...KEYS_BASE,
      [`GET ${KEYS}`]: () => (registered ? listOf(keyRecord("ak-1", "key-1", { description: "Ноутбук" })) : listOf()),
      [`POST ${BEGIN_REGISTRATION}`]: REGISTRATION_CHALLENGE,
      [`POST ${KEYS}`]: OPERATION_PENDING,
      "GET /operations/op-1": () => {
        registered = true;
        return OPERATION_DONE;
      },
    });
    keys = installBrowserKeys(() => Promise.resolve(credential));
    renderPage();
    const region = await keysRegion();
    expect(await within(region).findByText(NO_KEYS)).toBeInTheDocument();
    addKey(region, "key-1", "Ноутбук");
    await waitFor(() => expect(keyItem(region, "key-1")).toBeDefined());

    const options = keys.create.mock.calls[0][0] as CredentialCreationOptions;
    const pk = options.publicKey!;
    expect({
      challenge: Array.from(new Uint8Array(pk.challenge as ArrayBuffer)),
      rp: pk.rp,
      user: { ...pk.user, id: Buffer.from(new Uint8Array(pk.user.id as ArrayBuffer)).toString() },
      pubKeyCredParams: pk.pubKeyCredParams,
      authenticatorSelection: pk.authenticatorSelection,
      attestation: pk.attestation,
      extensions: pk.extensions,
    }).toEqual({
      challenge: CHALLENGE_BYTES,
      rp: { id: "console.kacho.local", name: "Kachō" },
      user: { id: "usr-1", name: "a@kacho.local", displayName: "a" },
      pubKeyCredParams: [{ type: "public-key", alg: -7 }],
      authenticatorSelection: { residentKey: "required", requireResidentKey: true, userVerification: "required" },
      attestation: "none",
      extensions: { credProps: true },
    });
    expect(lane.of("POST", KEYS)[0].body).toEqual({
      name: "key-1",
      description: "Ноутбук",
      credential: { ...wire, discoverable: true },
    });
    // Порядок: испытание → приём результата → опрос операции → перечень.
    const order = lane.calls
      .filter((c) => c.path.startsWith(KEYS) || c.path.startsWith("/operations/"))
      .map((c) => `${c.method} ${c.path}`);
    expect(order.slice(-4)).toEqual([
      `POST ${BEGIN_REGISTRATION}`,
      `POST ${KEYS}`,
      "GET /operations/op-1",
      `GET ${KEYS}`,
    ]);
    // Материал церемонии в журнал браузера не уходит.
    const material = ["+/+/AAECA/o=", wire.id, wire.clientDataJson, wire.attestationObject, "console.kacho.local"];
    expect(journal.entries().filter((e) => material.some((m) => e.includes(m)))).toEqual([]);
  });

  it("F8-55 · обнаружимость переносится тремя состояниями: rk=false — false, без credProps — поля нет", async () => {
    for (const [rk, expected] of [
      [false, { discoverable: false }],
      [undefined, {}],
    ] as const) {
      const { credential, wire } = createdCredential(rk);
      lane = installLane({
        ...KEYS_BASE,
        [`GET ${KEYS}`]: listOf(),
        [`POST ${BEGIN_REGISTRATION}`]: REGISTRATION_CHALLENGE,
        [`POST ${KEYS}`]: OPERATION_PENDING,
        "GET /operations/op-1": OPERATION_DONE,
      });
      keys = installBrowserKeys(() => Promise.resolve(credential));
      const { unmount } = render(
        <MemoryRouter initialEntries={["/settings"]}>
          <AccountSettingsPage />
        </MemoryRouter>,
      );
      const region = await keysRegion();
      await within(region).findByText(NO_KEYS);
      addKey(region, "key-x");
      await waitFor(() => expect(lane!.of("POST", KEYS)).toHaveLength(1));
      expect(lane.of("POST", KEYS)[0].body).toEqual({
        name: "key-x",
        description: "",
        credential: { ...wire, ...expected },
      });
      unmount();
      lane.restore();
      keys.restore();
      keys = null;
    }
  });

  it("F8-52 · человек отменил церемонию в браузере: текст консоли, приём результата не зовётся, кнопка снова доступна", async () => {
    journal = watchJournal();
    lane = installLane({
      ...KEYS_BASE,
      [`GET ${KEYS}`]: listOf(),
      [`POST ${BEGIN_REGISTRATION}`]: REGISTRATION_CHALLENGE,
    });
    keys = installBrowserKeys(() =>
      Promise.reject(
        new DOMException(
          "The operation either timed out or was not allowed. rp console.kacho.local",
          "NotAllowedError",
        ),
      ),
    );
    renderPage();
    const region = await keysRegion();
    await within(region).findByText(NO_KEYS);
    addKey(region, "key-52");
    expect(await within(region).findByText("Добавление ключа прервано в браузере — повторите")).toBeInTheDocument();
    expect(lane.of("POST", KEYS)).toHaveLength(0);
    expect(within(screen.getByRole("dialog")).getByRole("button", { name: "Добавить" })).toBeEnabled();
    expect(region).not.toHaveTextContent("NotAllowedError");
    expect(region).not.toHaveTextContent("console.kacho.local");
    expect(journal.entries().filter((e) => e.includes("console.kacho.local") || e.includes("NotAllowedError"))).toEqual(
      [],
    );
  });

  it("F8-53 · браузер без интерфейса ключей: добавить нельзя, перечень и удаление есть, испытания не просят", async () => {
    lane = installLane({ ...KEYS_BASE, [`GET ${KEYS}`]: listOf(keyRecord("ak-1", "key-53")) });
    keys = installBrowserKeys(() => Promise.reject(new Error("не должен зваться")), false);
    renderPage();
    const region = await keysRegion();
    await waitFor(() => expect(keyItem(region, "key-53")).toBeDefined());
    expect(
      within(region).getByText("Этот браузер не работает с ключами доступа — добавьте ключ в другом браузере"),
    ).toBeInTheDocument();
    expect(within(region).queryByRole("button", { name: "Добавить ключ доступа" })).toBeNull();
    expect(removeButton(keyItem(region, "key-53")!, "key-53")).toBeInTheDocument();
    expect(lane.of("POST", BEGIN_REGISTRATION)).toHaveLength(0);
  });

  it("F8-54 · операция заведения кончилась отказом: текст службы дословно, нового ключа нет, кнопка доступна", async () => {
    const ceiling =
      "access keys per user ceiling reached; WAY OUT: revoke an access key you no longer need, or raise own-ceilings.access-keys-per-user";
    lane = installLane({
      ...KEYS_BASE,
      [`GET ${KEYS}`]: listOf(keyRecord("ak-1", "key-old")),
      [`POST ${BEGIN_REGISTRATION}`]: REGISTRATION_CHALLENGE,
      [`POST ${KEYS}`]: OPERATION_PENDING,
      "GET /operations/op-1": { status: 200, body: { id: "op-1", done: true, error: { code: 8, message: ceiling } } },
    });
    keys = installBrowserKeys(() => Promise.resolve(createdCredential(true).credential));
    renderPage();
    const region = await keysRegion();
    await waitFor(() => expect(keyItem(region, "key-old")).toBeDefined());
    addKey(region, "key-54");
    expect(await within(region).findByText(ceiling)).toBeInTheDocument();
    expect(
      within(region)
        .getAllByRole("row")
        .filter((tr) => tr.querySelector("td")),
    ).toHaveLength(1);
    expect(within(screen.getByRole("dialog")).getByRole("button", { name: "Добавить" })).toBeEnabled();
  });

  it("F8-70 · отказ формы имени: текст стоит у поля «Имя», общий invalid argument у поля не стоит, введённое сохранено", async () => {
    const rule = "name must match ^[a-z][-a-z0-9]{1,61}[a-z0-9]$";
    lane = installLane({
      ...KEYS_BASE,
      [`GET ${KEYS}`]: listOf(),
      [`POST ${BEGIN_REGISTRATION}`]: REGISTRATION_CHALLENGE,
      [`POST ${KEYS}`]: fieldRefusal("name", rule),
    });
    keys = installBrowserKeys(() => Promise.resolve(createdCredential(true).credential));
    renderPage();
    const region = await keysRegion();
    await within(region).findByText(NO_KEYS);
    addKey(region, "Laptop", "Ноутбук");
    const name = within(region).getByLabelText("Имя");
    await waitFor(() => expect(name).toHaveAttribute("aria-invalid", "true"));
    expect(document.getElementById(name.getAttribute("aria-describedby")!)).toHaveTextContent(rule);
    expect(within(region).getByLabelText("Описание")).not.toHaveAttribute("aria-invalid");
    expect(region).not.toHaveTextContent("invalid argument");
    expect(lane.of("POST", KEYS)[0].body).toMatchObject({ name: "Laptop" });
    expect((name as HTMLInputElement).value).toBe("Laptop");
    expect(within(region).getByLabelText("Описание")).toHaveValue("Ноутбук");
    expect(lane.calls.filter((c) => c.path.startsWith("/operations/"))).toHaveLength(0);
    expect(within(screen.getByRole("dialog")).getByRole("button", { name: "Добавить" })).toBeEnabled();
  });

  it("F8-71 · отказ состояния испытания: текст службы у раздела, поля не отмечены, новое нажатие — новое испытание", async () => {
    const expired = "registration challenge expired: begin registration again";
    lane = installLane({
      ...KEYS_BASE,
      [`GET ${KEYS}`]: listOf(),
      [`POST ${BEGIN_REGISTRATION}`]: REGISTRATION_CHALLENGE,
      [`POST ${KEYS}`]: refusal(400, 9, expired, "CHALLENGE_EXPIRED"),
    });
    keys = installBrowserKeys(() => Promise.resolve(createdCredential(true).credential));
    renderPage();
    const region = await keysRegion();
    await within(region).findByText(NO_KEYS);
    addKey(region, "key-71");
    expect(await within(region).findByText(expired)).toBeInTheDocument();
    expect(within(region).getByLabelText("Имя")).not.toHaveAttribute("aria-invalid");
    expect(lane.calls.filter((c) => c.path.startsWith("/operations/"))).toHaveLength(0);
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Добавить" }));
    await waitFor(() => expect(lane!.of("POST", BEGIN_REGISTRATION)).toHaveLength(2));
  });

  it("F8-59 · снятие единственного ключа раздел не запрещает: DELETE по id, после done перечень пуст", async () => {
    let revoked = false;
    lane = installLane({
      ...KEYS_BASE,
      [`GET ${KEYS}`]: () => (revoked ? listOf() : listOf(keyRecord("ak-only", "key-only"))),
      [`DELETE ${KEYS}/ak-only`]: OPERATION_PENDING,
      "GET /operations/op-1": () => {
        revoked = true;
        return OPERATION_DONE;
      },
    });
    keys = installBrowserKeys(() => Promise.reject(new Error("не зовётся")));
    renderPage();
    const region = await keysRegion();
    await waitFor(() => expect(keyItem(region, "key-only")).toBeDefined());
    fireEvent.click(removeButton(keyItem(region, "key-only")!, "key-only"));
    const dialog = await screen.findByRole("dialog");
    expect(dialog).toHaveTextContent("key-only");
    expect(dialog).toHaveTextContent("Войти этим ключом будет нельзя.");
    fireEvent.click(within(dialog).getByRole("button", { name: "Удалить" }));
    expect(await within(region).findByText(NO_KEYS)).toBeInTheDocument();
    expect(lane.of("DELETE", `${KEYS}/ak-only`)).toHaveLength(1);
  });

  it("F8-60 · отмена в диалоге ничего не выпускает", async () => {
    lane = installLane({ ...KEYS_BASE, [`GET ${KEYS}`]: listOf(keyRecord("ak-only", "key-only")) });
    keys = installBrowserKeys(() => Promise.reject(new Error("не зовётся")));
    renderPage();
    const region = await keysRegion();
    await waitFor(() => expect(keyItem(region, "key-only")).toBeDefined());
    fireEvent.click(removeButton(keyItem(region, "key-only")!, "key-only"));
    const dialog = await screen.findByRole("dialog");
    expect(dialog).toHaveTextContent("key-only");
    fireEvent.click(within(dialog).getByRole("button", { name: "Отмена" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(lane.calls.filter((c) => c.method === "DELETE")).toHaveLength(0);
    expect(keyItem(region, "key-only")).toBeDefined();
  });

  it("F8-58 · последний способ входа: синхронный отказ снятия дословно, операции нет, ключ и кнопка на месте", async () => {
    const last = "last sign-in method cannot be revoked: enrol another sign-in method first";
    lane = installLane({
      ...KEYS_BASE,
      [`GET ${KEYS}`]: listOf(keyRecord("ak-only", "key-only")),
      [`DELETE ${KEYS}/ak-only`]: refusal(400, 9, last, "LAST_SIGN_IN_METHOD"),
    });
    keys = installBrowserKeys(() => Promise.reject(new Error("не зовётся")));
    renderPage();
    const region = await keysRegion();
    await waitFor(() => expect(keyItem(region, "key-only")).toBeDefined());
    fireEvent.click(removeButton(keyItem(region, "key-only")!, "key-only"));
    fireEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Удалить" }));
    expect(await within(region).findByText(last)).toBeInTheDocument();
    expect(lane.calls.filter((c) => c.path.startsWith("/operations/"))).toHaveLength(0);
    expect(removeButton(keyItem(region, "key-only")!, "key-only")).toBeInTheDocument();
  });

  it("F8-69 · последний способ входа при гонке: отказ из error операции дословно, перечень перечитан, ключ на месте", async () => {
    const last = "last sign-in method cannot be revoked: enrol another sign-in method first";
    lane = installLane({
      ...KEYS_BASE,
      [`GET ${KEYS}`]: listOf(keyRecord("ak-only", "key-only")),
      [`DELETE ${KEYS}/ak-only`]: OPERATION_PENDING,
      "GET /operations/op-1": { status: 200, body: { id: "op-1", done: true, error: { code: 9, message: last } } },
    });
    keys = installBrowserKeys(() => Promise.reject(new Error("не зовётся")));
    renderPage();
    const region = await keysRegion();
    await waitFor(() => expect(keyItem(region, "key-only")).toBeDefined());
    fireEvent.click(removeButton(keyItem(region, "key-only")!, "key-only"));
    fireEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Удалить" }));
    expect(await within(region).findByText(last)).toBeInTheDocument();
    await waitFor(() => expect(lane!.of("GET", KEYS)).toHaveLength(2));
    expect(removeButton(keyItem(region, "key-only")!, "key-only")).toBeInTheDocument();
  });

  it("F8-61 · перечень дочитывается до конца: второе обращение несёт pageToken; без токена обращение одно", async () => {
    lane = installLane({
      ...KEYS_BASE,
      [`GET ${KEYS}`]: (call) =>
        new URLSearchParams(call.query).get("pageToken") === "page-2"
          ? listOf(keyRecord("ak-2", "key-second"))
          : { status: 200, body: { accessKeys: [keyRecord("ak-1", "key-first")], nextPageToken: "page-2" } },
    });
    keys = installBrowserKeys(() => Promise.reject(new Error("не зовётся")));
    const first = render(
      <MemoryRouter initialEntries={["/settings"]}>
        <AccountSettingsPage />
      </MemoryRouter>,
    );
    let region = await keysRegion();
    await waitFor(() => expect(keyItem(region, "key-second")).toBeDefined());
    expect(keyItem(region, "key-first")).toBeDefined();
    expect(lane.of("GET", KEYS).map((c) => new URLSearchParams(c.query).get("pageToken"))).toEqual([null, "page-2"]);
    first.unmount();
    lane.restore();

    lane = installLane({ ...KEYS_BASE, [`GET ${KEYS}`]: listOf(keyRecord("ak-1", "key-first")) });
    renderPage();
    region = await keysRegion();
    await waitFor(() => expect(keyItem(region, "key-first")).toBeDefined());
    expect(lane.of("GET", KEYS)).toHaveLength(1);
  });

  it("F8-S7-50 · ключи выключены на установке: состояние раздела, добавления нет; «Удалить» без способов — сообщение, снятия нет", async () => {
    lane = installLane({
      ...KEYS_BASE,
      "GET /iam/v1/auth/second-factor": {
        status: 200,
        body: { totp: { enrolled: false }, accessKeys: { available: false }, stepUpMethods: [] },
      },
      [`GET ${KEYS}`]: listOf(keyRecord("ak-1", "key-50")),
    });
    keys = installBrowserKeys(() => Promise.reject(new Error("не зовётся")));
    renderPage();
    const region = await keysRegion();
    expect(
      await within(region).findByText("Ключи доступа на этой установке выключены администратором"),
    ).toBeInTheDocument();
    expect(within(region).queryByRole("button", { name: "Добавить ключ доступа" })).toBeNull();
    await waitFor(() => expect(keyItem(region, "key-50")).toBeDefined());
    expect(await screen.findByRole("region", { name: "Сводка" })).toHaveTextContent("Выключены на установке");
    fireEvent.click(removeButton(keyItem(region, "key-50")!, "key-50"));
    expect(screen.getByRole("status")).toHaveTextContent(
      "Удалить ключ сейчас нельзя: нужен второй фактор, а ключи на этой установке выключены. Попросите администратора установки сбросить ваши ключи доступа.",
    );
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(lane.calls.filter((c) => c.method === "DELETE")).toHaveLength(0);
    expect(lane.of("POST", BEGIN_REGISTRATION)).toHaveLength(0);
  });

  it("F8-S7-71 · удаление: «Ключ доступа «<имя>» удалён» областью статуса экрана; последний способ «2» назван в диалоге", async () => {
    let revoked = false;
    lane = installLane({
      ...KEYS_BASE,
      "GET /iam/v1/auth/second-factor": {
        status: 200,
        body: { totp: { enrolled: false }, accessKeys: { available: true }, stepUpMethods: ["webauthn"] },
      },
      [`GET ${KEYS}`]: () => (revoked ? listOf() : listOf(keyRecord("ak-1", "key-71"))),
      [`DELETE ${KEYS}/ak-1`]: OPERATION_PENDING,
      "GET /operations/op-1": () => {
        revoked = true;
        return OPERATION_DONE;
      },
    });
    keys = installBrowserKeys(() => Promise.reject(new Error("не зовётся")));
    renderPage();
    const region = await keysRegion();
    await waitFor(() => expect(keyItem(region, "key-71")).toBeDefined());
    fireEvent.click(removeButton(keyItem(region, "key-71")!, "key-71"));
    const dialog = await screen.findByRole("dialog");
    expect(dialog).toHaveTextContent("Войти этим ключом будет нельзя.");
    expect(dialog).toHaveTextContent("Действия, требующие второго фактора, станут недоступны.");
    fireEvent.click(within(dialog).getByRole("button", { name: "Удалить" }));
    await waitFor(() => expect(screen.getByRole("status")).toHaveTextContent("Ключ доступа «key-71» удалён"));
    expect(keyItem(region, "key-71")).toBeUndefined();
  });

  it("F8-S7-60 · ключи общей таблицей: «Имя», «Добавлен», «Последнее использование» и колонка «Удалить»; своих <ul> — 0", async () => {
    lane = installLane({
      ...KEYS_BASE,
      [`GET ${KEYS}`]: listOf(keyRecord("ak-1", "key-used", { lastUsedAt: "2026-10-09T11:00:00Z" })),
    });
    keys = installBrowserKeys(() => Promise.reject(new Error("не зовётся")));
    renderPage();
    const region = await keysRegion();
    await waitFor(() => expect(keyItem(region, "key-used")).toBeDefined());
    const table = within(region).getByRole("table");
    // Колонки — дословно и по порядку (Р3); колонка действия заголовка не несёт.
    expect(within(table).getAllByRole("columnheader").map((h) => h.textContent)).toEqual([
      "Имя",
      "Добавлен",
      "Последнее использование",
      "",
    ]);
    const row = keyItem(region, "key-used")!;
    expect(row).toHaveTextContent("7 окт. 2026");
    expect(row).toHaveTextContent("9 окт. 2026");
    expect(within(row).getByRole("button", { name: "Удалить ключ доступа «key-used»" })).toBeInTheDocument();
    expect(region.querySelectorAll("ul")).toHaveLength(0);
  });

  it("F8-48 · ключ показан именем, описанием и моментом заведения; не использованный — «Ещё не использовался»", async () => {
    lane = installLane({
      ...KEYS_BASE,
      [`GET ${KEYS}`]: listOf(
        keyRecord("ak-1", "seed-f8-48", { description: "Рабочий ноутбук" }),
        keyRecord("ak-2", "key-used", { lastUsedAt: "2026-10-07T11:00:00Z" }),
      ),
    });
    keys = installBrowserKeys(() => Promise.reject(new Error("не зовётся")));
    renderPage();
    const region = await keysRegion();
    await waitFor(() => expect(keyItem(region, "seed-f8-48")).toBeDefined());
    const fresh = keyItem(region, "seed-f8-48")!;
    expect(fresh).toHaveTextContent("Рабочий ноутбук");
    expect(fresh).toHaveTextContent("7 окт. 2026");
    expect(fresh).toHaveTextContent("Ещё не использовался");
    expect(keyItem(region, "key-used")).not.toHaveTextContent("Ещё не использовался");
    expect(within(region).getByRole("button", { name: "Добавить ключ доступа" })).toBeInTheDocument();
  });
});

// ═══ S2 — группа M. Первый пароль из живой сессии (Р13) ════════════════════

const ENROLL = "/iam/v1/auth/password/enroll";
const PASSWORD_BASE = {
  "GET /iam/v1/auth/me": SIGNED_IN,
  "GET /iam/v1/auth/second-factor": NOT_ENROLLED,
  [`GET ${KEYS}`]: listOf(),
  "GET /iam/v1/auth/sessions": ONE_SESSION,
} as const;

/** Окно «Пароль» из раздела «Способ входа». */
async function openPassword() {
  const signIn = await screen.findByRole("region", { name: "Способ входа" });
  fireEvent.click(within(signIn).getByRole("button", { name: "Сменить пароль" }));
  return screen.findByRole("dialog");
}

async function openEnroll() {
  const password = await openPassword();
  fireEvent.click(within(password).getByRole("button", { name: "У меня нет пароля — завести" }));
  return password;
}

/** Окно подтверждения — поверх окна «Пароль»: два окна, нужное — по заголовку. */
async function stepUpWindow() {
  await waitFor(() =>
    expect(screen.getAllByRole("dialog").some((d) => d.textContent?.includes("Подтверждение действия"))).toBe(true),
  );
  return screen.getAllByRole("dialog").find((d) => d.textContent?.includes("Подтверждение действия"))!;
}

describe("первый пароль на /settings (F8, S2, группа M)", () => {
  it("F8-62 · заведение первого пароля: форму выбрал человек, глагол получил ровно объявленное, затем снова смена", async () => {
    lane = installLane({ ...PASSWORD_BASE, [`POST ${ENROLL}`]: { status: 200, body: { session: SESSION } } });
    renderPage();
    const password = await openPassword();
    // До нажатия формы заведения нет: по умолчанию — смена (Р13).
    expect(within(password).getByLabelText("Текущий пароль")).toBeInTheDocument();
    expect(within(password).queryByRole("button", { name: "Завести пароль" })).toBeNull();
    fireEvent.click(within(password).getByRole("button", { name: "У меня нет пароля — завести" }));
    expect(within(password).queryByLabelText("Текущий пароль")).toBeNull();
    fireEvent.change(within(password).getByLabelText("Новый пароль"), { target: { value: "first-password-62" } });
    fireEvent.click(within(password).getByRole("button", { name: "Завести пароль" }));
    await waitFor(() => expect(screen.getByRole("status")).toHaveTextContent("Пароль заведён"));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(lane.calls.some((c) => c.path === "/iam/v1/auth/csrf" && c.query === "?form=password-enroll")).toBe(true);
    expect(lane.of("POST", ENROLL)[0].body).toEqual({
      newPassword: "first-password-62",
      csrfToken: "tok-password-enroll-1",
    });
  });

  it("F8-63 · человек с паролем выбрал заведение: отказ службы дословно, и раздел снова показывает смену", async () => {
    const already = "password is already set; change it with the current password";
    lane = installLane({ ...PASSWORD_BASE, [`POST ${ENROLL}`]: refusal(409, 6, already, "PASSWORD_ALREADY_SET") });
    renderPage();
    const password = await openEnroll();
    fireEvent.change(within(password).getByLabelText("Новый пароль"), { target: { value: "second-password-63" } });
    fireEvent.click(within(password).getByRole("button", { name: "Завести пароль" }));
    expect(await within(password).findByText(already)).toBeInTheDocument();
    expect(within(password).getByLabelText("Текущий пароль")).toBeInTheDocument();
    expect(within(password).getByRole("button", { name: "Сменить пароль" })).toBeInTheDocument();
  });

  it("F8-64/F8-S7-27 · свежесть истекла, способов нет: окно — «Нет способа подтверждения» и «Войти заново» с возвратом сюда", async () => {
    window.history.pushState({}, "", "/settings");
    const leave = jest.fn();
    lane = installLane({
      ...PASSWORD_BASE,
      "GET /iam/v1/auth/second-factor": SUA_NOTHING,
      [`POST ${ENROLL}`]: refusal(
        403,
        7,
        "re-authentication required: present a credential again",
        "SESSION_NOT_FRESH",
      ),
      "POST /iam/v1/auth/logout": { status: 200, body: {} },
    });
    render(
      <MemoryRouter initialEntries={["/settings"]}>
        <AccountSettingsPage leave={leave} />
      </MemoryRouter>,
    );
    const password = await openEnroll();
    fireEvent.change(within(password).getByLabelText("Новый пароль"), { target: { value: "first-password-64" } });
    fireEvent.click(within(password).getByRole("button", { name: "Завести пароль" }));
    const dialog = await stepUpWindow();
    expect(await within(dialog).findByText("Нет способа подтверждения")).toBeInTheDocument();
    // Пароль окно подтверждения больше не предлагает (F8-S7, §5: окно — способы `stepUpMethods`).
    expect(within(dialog).queryByLabelText("Пароль")).toBeNull();
    fireEvent.click(within(dialog).getByRole("button", { name: "Войти заново" }));
    await waitFor(() => expect(leave).toHaveBeenCalledWith("/login?returnTo=%2Fsettings"));
    expect(lane.of("POST", "/iam/v1/auth/logout")).toHaveLength(1);
    expect(lane.of("POST", ENROLL)).toHaveLength(1);
    window.history.pushState({}, "", "/");
  });

  it("F8-64 · после повышения заведение повторено тем же паролем со свежим признаком; окно уровня «Войти заново» не несёт", async () => {
    lane = installLane({
      ...PASSWORD_BASE,
      [`POST ${ENROLL}`]: (_c, nth) =>
        nth === 1
          ? refusal(403, 7, "re-authentication required: present a credential again", "SESSION_NOT_FRESH")
          : { status: 200, body: { session: SESSION } },
      "POST /iam/v1/auth/step-up": {
        status: 200,
        body: { session: SESSION, assurance: { level: "1", level2Reachable: true, missingForLevel2: [] } },
      },
    });
    renderPage();
    const password = await openEnroll();
    fireEvent.change(within(password).getByLabelText("Новый пароль"), { target: { value: "first-password-64" } });
    fireEvent.click(within(password).getByRole("button", { name: "Завести пароль" }));
    const dialog = await stepUpWindow();
    fireEvent.change(await within(dialog).findByLabelText("Код из приложения"), { target: { value: "123456" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Подтвердить" }));
    await waitFor(() => expect(screen.getByRole("status")).toHaveTextContent("Пароль заведён"));
    expect(lane.of("POST", ENROLL).map((c) => c.body)).toEqual([
      { newPassword: "first-password-64", csrfToken: "tok-password-enroll-1" },
      { newPassword: "first-password-64", csrfToken: "tok-password-enroll-2" },
    ]);
  });

  it("F8-64 · положительная сторона Р12: окно, открытое вызовом края на уровень, пути «Войти заново» не несёт", async () => {
    lane = installLane({
      ...PASSWORD_BASE,
      [`GET ${KEYS}`]: {
        status: 401,
        headers: {
          "WWW-Authenticate": 'Bearer realm="kacho", error="insufficient_user_authentication", acr_values="2"',
        },
        body: { code: 16, message: "insufficient user authentication" },
      },
    });
    renderPage();
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).queryByRole("button", { name: "Войти заново" })).toBeNull();
    expect(within(dialog).queryByLabelText("Паролем")).toBeNull();
  });

  it("F8-65 · правило пароля: поле «Новый пароль» отмечено текстом службы, ушло введённое как есть", async () => {
    const rule = "Illegal argument newPassword: must be at least 12 characters";
    lane = installLane({ ...PASSWORD_BASE, [`POST ${ENROLL}`]: refusal(400, 3, rule) });
    renderPage();
    const password = await openEnroll();
    const next = within(password).getByLabelText("Новый пароль");
    fireEvent.change(next, { target: { value: "short" } });
    fireEvent.click(within(password).getByRole("button", { name: "Завести пароль" }));
    await waitFor(() => expect(next).toHaveAttribute("aria-invalid", "true"));
    expect(document.getElementById(next.getAttribute("aria-describedby")!)).toHaveTextContent(rule);
    // Отказ стоит у поля, и только там: второго окна отказа у раздела нет.
    expect(within(password).getAllByRole("alert")).toEqual([
      document.getElementById(next.getAttribute("aria-describedby")!),
    ]);
    expect(lane.of("POST", ENROLL)[0].body).toMatchObject({ newPassword: "short" });
  });

  it("F8-66 · служба не ответила: отказ назван и предложено повторить, форма заведения открыта, введённое цело", async () => {
    lane = installLane({
      ...PASSWORD_BASE,
      [`POST ${ENROLL}`]: refusal(503, 14, "request not performed; try again later"),
    });
    renderPage();
    const password = await openEnroll();
    fireEvent.change(within(password).getByLabelText("Новый пароль"), { target: { value: "first-password-66" } });
    fireEvent.click(within(password).getByRole("button", { name: "Завести пароль" }));
    const alert = await within(password).findByRole("alert");
    expect(alert).toHaveTextContent("request not performed; try again later");
    expect(alert).toHaveTextContent("Отправьте форму ещё раз.");
    expect(within(password).getByLabelText("Новый пароль")).toHaveValue("first-password-66");
    expect(within(password).getByRole("button", { name: "Завести пароль" })).toBeInTheDocument();
  });

  it("F8-68 · отрицательная сторона: под отказом смены пароля (code 16) подсказки экрана входа нет", async () => {
    lane = installLane({ ...PASSWORD_BASE, "POST /iam/v1/auth/password": refusal(401, 16, "authentication failed") });
    renderPage();
    const password = await openPassword();
    fireEvent.click(within(password).getByRole("button", { name: "Сменить пароль" }));
    expect(await within(password).findByRole("alert")).toHaveTextContent("authentication failed");
    expect(password).not.toHaveTextContent("Не получается войти?");
  });
});

// ═══ S5 — свои сессии на /settings (приёмка F8-S5, Р2–Р5) ═════════════════════
//
// Раздел ходит тремя глаголами полосы формы; дублёр отвечает телами в форме
// службы (приёмка службы OS, Р4, Р5, Р7): перечень `{sessions, nextPageToken}`,
// снятие `{ended: true}`, «все, кроме этой» `{ended: N}`, отказ —
// `google.rpc.Status`.

const SESSIONS_PATH = "/iam/v1/auth/sessions";
const END_PATH = "/iam/v1/auth/sessions/end";
const END_OTHERS_PATH = "/iam/v1/auth/sessions/end-others";

const ROW_A = {
  id: "hss-0000000000000000a",
  current: false,
  authenticatedAt: "2026-10-07T09:00:00Z",
  lastPresentedAt: "2026-10-07T09:05:00Z",
  expiresAt: "2026-10-08T09:00:00Z",
  userAgent: "f8-s5-c-seed-a",
};
const ROW_B = {
  id: "hss-0000000000000000b",
  current: true,
  authenticatedAt: "2026-10-07T10:00:00Z",
  lastPresentedAt: "2026-10-07T10:00:00Z",
  expiresAt: "2026-10-08T10:00:00Z",
  userAgent: "f8-s5-c-seed-b",
};

const sessionsPage = (sessions: unknown[], nextPageToken = ""): LaneAnswer => ({
  status: 200,
  body: { sessions, nextPageToken },
});

const SESSIONS_BASE = {
  "GET /iam/v1/auth/me": SIGNED_IN,
  "GET /iam/v1/auth/second-factor": NOT_ENROLLED,
  "GET /iam/v1/users/usr-1/accessKeys": KEYS_EMPTY,
};

async function sessionsRegion() {
  return screen.findByRole("region", { name: "Активные сессии" });
}

/** Строки перечня — строки тела общей таблицы раздела (F8-S7, Р3). */
function rowsIn(region: HTMLElement) {
  return within(region)
    .queryAllByRole("row")
    .filter((tr) => tr.querySelector("td"));
}

async function findRows(region: HTMLElement) {
  await waitFor(() => expect(rowsIn(region).length).toBeGreaterThan(0));
  return rowsIn(region);
}

/** Строка перечня по описанию клиента — как прислано. */
async function rowOf(region: HTMLElement, text: string) {
  const items = await findRows(region);
  const row = items.find((li) => li.textContent?.includes(text));
  if (!row) throw new Error(`строки «${text}» в перечне нет: ${items.map((i) => i.textContent).join(" | ")}`);
  return row;
}

/** «Выйти» у строки — доступное имя называет сессию. */
const endButton = (row: HTMLElement) => within(row).getByRole("button", { name: /^Выйти из сессии/ });

/** «Выйти на всех, кроме этой» и подтверждение диалога (F8-S7, Р6). */
function endOthers(region: HTMLElement) {
  fireEvent.click(within(region).getByRole("button", { name: "Выйти на всех, кроме этой" }));
  const dialog = screen.getByRole("dialog");
  expect(dialog).toHaveTextContent("Все другие устройства выйдут из учётной записи.");
  fireEvent.click(within(dialog).getByRole("button", { name: "Выйти" }));
}

describe("свои сессии (F8-S5)", () => {
  it("F8-S5-01 · при открытии — одно чтение перечня; строки в порядке службы, текущая помечена и без «Выйти»", async () => {
    lane = installLane({ ...SESSIONS_BASE, [`GET ${SESSIONS_PATH}`]: sessionsPage([ROW_A, ROW_B]) });
    renderPage();
    const region = await sessionsRegion();
    const items = await findRows(region);
    expect(
      items.map((li) =>
        li.textContent?.includes(ROW_A.userAgent) ? "A" : li.textContent?.includes(ROW_B.userAgent) ? "B" : "?",
      ),
    ).toEqual(["A", "B"]);
    const a = await rowOf(region, ROW_A.userAgent);
    const b = await rowOf(region, ROW_B.userAgent);
    expect(within(b).getByText("Эта сессия")).toBeInTheDocument();
    expect(within(b).queryByRole("button", { name: /^Выйти/ })).toBeNull();
    expect(within(a).queryByText("Эта сессия")).toBeNull();
    expect(endButton(a)).toBeInTheDocument();
    for (const label of ["Вход", "Последнее предъявление способа входа", "Действует до"]) {
      expect(within(region).getByRole("columnheader", { name: label })).toBeInTheDocument();
    }
    expect(within(region).getByRole("button", { name: "Выйти на всех, кроме этой" })).toBeInTheDocument();
    expect(lane.of("GET", SESSIONS_PATH)).toHaveLength(1);
    // Перечень признака формы не берёт (Р4): выдачи признака при открытии экрана
    // — только своих видов форм экрана, а вида `session-end` — одна, держателем раздела.
    expect(lane.of("GET", SESSIONS_PATH)[0].query).toBe("");
  });

  it("F8-S5-08 · описание клиента — как прислано и целиком; отсутствие названо; дочитывание тем же путём", async () => {
    const long = "x".repeat(600);
    const rowC = { ...ROW_A, id: "hss-0000000000000000c", userAgent: "f8-s5-08-c" };
    const noAgent: Record<string, unknown> = { ...ROW_B };
    delete noAgent.userAgent;
    lane = installLane({
      ...SESSIONS_BASE,
      [`GET ${SESSIONS_PATH}`]: (call) =>
        call.query === "?pageToken=c1"
          ? sessionsPage([rowC], "")
          : sessionsPage([{ ...ROW_A, userAgent: long }, noAgent], "c1"),
    });
    renderPage();
    const region = await sessionsRegion();
    const a = await rowOf(region, long);
    // Значение целиком — 600 знаков, без обрезки консолью.
    expect(within(a).getByText(long)).toBeInTheDocument();
    const b = await rowOf(region, "Устройство не названо");
    expect(b).toHaveTextContent("Эта сессия");
    // Близнец (б) — (а): различие одно — ключа нет; у (а) «не названо» не пишется.
    expect(a).not.toHaveTextContent("Устройство не названо");
    fireEvent.click(within(region).getByRole("button", { name: "Показать ещё" }));
    await waitFor(() => expect(rowsIn(region)).toHaveLength(3));
    const order = rowsIn(region).map((li) =>
      li.textContent?.includes(long)
        ? "а"
        : li.textContent?.includes("Устройство не названо")
          ? "б"
          : li.textContent?.includes("f8-s5-08-c")
            ? "в"
            : "?",
    );
    expect(order).toEqual(["а", "б", "в"]);
    expect(within(region).queryByRole("button", { name: "Показать ещё" })).toBeNull();
    expect(lane.of("GET", SESSIONS_PATH).map((c) => c.query)).toEqual(["", "?pageToken=c1"]);
  });

  it("F8-S5-02 · «Выйти» у строки: признак вида session-end, тело {sessionId, csrfToken}, исход назван, перечень перечитан", async () => {
    let ended = false;
    lane = installLane({
      ...SESSIONS_BASE,
      [`GET ${SESSIONS_PATH}`]: () => sessionsPage(ended ? [ROW_B] : [ROW_A, ROW_B]),
      [`POST ${END_PATH}`]: () => {
        ended = true;
        return { status: 200, body: { ended: true } };
      },
    });
    renderPage();
    const region = await sessionsRegion();
    fireEvent.click(endButton(await rowOf(region, ROW_A.userAgent)));
    await waitFor(() => expect(screen.getByRole("status")).toHaveTextContent("Сессия завершена"));
    await waitFor(() => expect(rowsIn(region)).toHaveLength(1));
    expect(lane.of("POST", END_PATH).map((c) => c.body)).toEqual([
      { sessionId: ROW_A.id, csrfToken: "tok-session-end-1" },
    ]);
    expect(lane.calls.filter((c) => c.path === "/iam/v1/auth/csrf").map((c) => c.query)).toContain("?form=session-end");
    expect(lane.of("GET", SESSIONS_PATH)).toHaveLength(2);
  });

  it("F8-S5-03 · «Выйти на всех, кроме этой»: тело {csrfToken}, «Завершено сессий: 2», перечень перечитан", async () => {
    let ended = false;
    lane = installLane({
      ...SESSIONS_BASE,
      [`GET ${SESSIONS_PATH}`]: () =>
        sessionsPage(ended ? [ROW_B] : [ROW_A, { ...ROW_A, id: "hss-0000000000000000c" }, ROW_B]),
      [`POST ${END_OTHERS_PATH}`]: () => {
        ended = true;
        return { status: 200, body: { ended: 2 } };
      },
    });
    renderPage();
    const region = await sessionsRegion();
    await findRows(region);
    endOthers(region);
    await waitFor(() => expect(screen.getByRole("status")).toHaveTextContent("Завершено сессий: 2"));
    await waitFor(() => expect(rowsIn(region)).toHaveLength(1));
    expect(lane.of("POST", END_OTHERS_PATH).map((c) => c.body)).toEqual([{ csrfToken: "tok-session-end-1" }]);
    expect(lane.of("GET", SESSIONS_PATH)).toHaveLength(2);
  });

  it("F8-S7-61 · единственная сессия: «Других активных сессий нет.», кнопки «Выйти на всех, кроме этой» нет (замена F8-S5-04)", async () => {
    lane = installLane({ ...SESSIONS_BASE, [`GET ${SESSIONS_PATH}`]: sessionsPage([ROW_B]) });
    renderPage();
    const region = await sessionsRegion();
    await findRows(region);
    expect(within(region).getByText("Других активных сессий нет.")).toBeInTheDocument();
    expect(within(region).queryByRole("button", { name: "Выйти на всех, кроме этой" })).toBeNull();
  });

  it("F8-S7 Р6 · «Выйти на всех, кроме этой»: отмена диалога ничего не выпускает", async () => {
    lane = installLane({ ...SESSIONS_BASE, [`GET ${SESSIONS_PATH}`]: sessionsPage([ROW_A, ROW_B]) });
    renderPage();
    const region = await sessionsRegion();
    await findRows(region);
    fireEvent.click(within(region).getByRole("button", { name: "Выйти на всех, кроме этой" }));
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Отмена" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(lane.of("POST", END_OTHERS_PATH)).toHaveLength(0);
  });

  it("F8-S5-05 · SESSION_NOT_FRESH: окно повышения и РОВНО один повтор снятия", async () => {
    let ended = false;
    lane = installLane({
      ...SESSIONS_BASE,
      [`GET ${SESSIONS_PATH}`]: () => sessionsPage(ended ? [ROW_B] : [ROW_A, ROW_B]),
      [`POST ${END_PATH}`]: (_c, nth) => {
        if (nth === 1)
          return refusal(403, 7, "re-authentication required: present a credential again", "SESSION_NOT_FRESH");
        ended = true;
        return { status: 200, body: { ended: true } };
      },
      "POST /iam/v1/auth/step-up": {
        status: 200,
        body: { session: SESSION, assurance: { level: "1", level2Reachable: true, missingForLevel2: [] } },
      },
    });
    renderPage();
    const region = await sessionsRegion();
    fireEvent.click(endButton(await rowOf(region, ROW_A.userAgent)));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(await within(dialog).findByLabelText("Код из приложения"), { target: { value: "123456" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Подтвердить" }));
    await waitFor(() => expect(screen.getByRole("status")).toHaveTextContent("Сессия завершена"));
    expect(lane.of("POST", END_PATH)).toHaveLength(2);
    expect(lane.of("POST", "/iam/v1/auth/step-up")).toHaveLength(1);
  });

  it("F8-S5-06 · SESSION_NOT_FOUND: текст отказа дословно, перечень перечитан, строки уже нет", async () => {
    lane = installLane({
      ...SESSIONS_BASE,
      [`GET ${SESSIONS_PATH}`]: (_c, nth) => sessionsPage(nth === 1 ? [ROW_A, ROW_B] : [ROW_B]),
      [`POST ${END_PATH}`]: refusal(404, 5, "session not found", "SESSION_NOT_FOUND"),
    });
    renderPage();
    const region = await sessionsRegion();
    fireEvent.click(endButton(await rowOf(region, ROW_A.userAgent)));
    expect((await within(region).findByRole("alert")).textContent).toContain("session not found");
    await waitFor(() => expect(rowsIn(region)).toHaveLength(1));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(lane.of("POST", END_PATH)).toHaveLength(1);
  });

  it("F8-S5-07 · ответ края о недоступности службы: назван дословно, повтора и повышения нет", async () => {
    lane = installLane({
      ...SESSIONS_BASE,
      [`GET ${SESSIONS_PATH}`]: sessionsPage([ROW_A, ROW_B]),
      [`POST ${END_PATH}`]: laneAnswerOf(EDGE_CREDENTIAL_STATE_UNKNOWN),
    });
    renderPage();
    const region = await sessionsRegion();
    fireEvent.click(endButton(await rowOf(region, ROW_A.userAgent)));
    expect(await within(region).findByRole("alert")).toHaveTextContent("credential state could not be established");
    expect(lane.of("POST", END_PATH)).toHaveLength(1);
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(screen.getByRole("status")).toHaveTextContent("");
  });

  it("F8-S7 Р1 · раздел «Активные сессии» — последним, после «Запасных кодов» (замена F8-S5 Р2)", async () => {
    lane = installLane({ ...SESSIONS_BASE, [`GET ${SESSIONS_PATH}`]: sessionsPage([ROW_B]) });
    renderPage();
    await sessionsRegion();
    const titles = screen
      .getAllByRole("region")
      .map(
        (r) =>
          r.getAttribute("aria-labelledby") && document.getElementById(r.getAttribute("aria-labelledby")!)?.textContent,
      );
    // Под разделами Р1 стоит только необратимое удаление учётной записи
    // (#3186): из задач Р1 «Активные сессии» — последняя.
    expect(titles.slice(titles.indexOf("Активные сессии"))).toEqual(["Активные сессии", "Удаление учётной записи"]);
    expect(titles.indexOf("Активные сессии")).toBe(titles.indexOf("Запасные коды") + 1);
  });
});

// ═══ F8-S6 — смена адреса почты в разделе «Способ входа» (F8-S7, Р1) ═══════
//
// Три состояния раздела (Р3) и отказы (Р4). Дублёр отвечает телами и
// заголовками производителя: запрос смены — `200 {}` с `Retry-After`
// (`loginlanehttp.requestEmailChange`), предъявление — `{session}` той же формы,
// что у `verify-email/confirm`, отказы — `google.rpc.Status`.

const NEW_EMAIL = "n@kacho.local";
const CHANGE = "/iam/v1/auth/email-change";
const CHANGE_CONFIRM = "/iam/v1/auth/email-change/confirm";
const CONFIRMED_SESSION = { ...SESSION, emailVerified: true };
const CHANGED_ME: LaneAnswer = {
  status: 200,
  body: { user: { id: "usr-1", email: NEW_EMAIL, displayName: "a" }, session: CONFIRMED_SESSION },
};
const VERIFIED_ME: LaneAnswer = {
  status: 200,
  body: { user: { id: "usr-1", email: "a@kacho.local", displayName: "a" }, session: CONFIRMED_SESSION },
};
const CHANGE_ACCEPTED = (retryAfter: string): LaneAnswer => ({
  status: 200,
  body: {},
  headers: { "Retry-After": retryAfter },
});
const AUTHN_FAILED = refusal(401, 16, "authentication failed");

function renderPageLeaving(leave: (to: string) => void) {
  render(
    <MemoryRouter initialEntries={["/settings"]}>
      <AccountSettingsPage leave={leave} />
    </MemoryRouter>,
  );
}

async function accountRegion() {
  return screen.findByRole("region", { name: "Способ входа" });
}

/** Покой → форма → «Отправить код» с адресом `email`. */
async function submitNewEmail(region: HTMLElement, email = NEW_EMAIL) {
  fireEvent.click(await within(region).findByRole("button", { name: "Сменить адрес" }));
  fireEvent.change(await within(region).findByLabelText("Новый адрес"), { target: { value: email } });
  fireEvent.click(within(region).getByRole("button", { name: "Отправить код" }));
}

/** Состояние «код»: строка письма видна. */
async function codeState(region: HTMLElement, email = NEW_EMAIL) {
  return within(region).findByText(`Письмо с кодом отправлено на ${email}, если этот адрес может быть использован`);
}

function enterCode(region: HTMLElement, code: string) {
  fireEvent.change(within(region).getByLabelText("Код из письма"), { target: { value: code } });
  fireEvent.click(within(region).getByRole("button", { name: "Подтвердить" }));
}

/** Отказ по уровню, как его производит служба (EC Р2): `401`, `code 16`, вызов RFC 9470. */
const LEVEL_REFUSAL: LaneAnswer = {
  status: 401,
  headers: { "WWW-Authenticate": 'Bearer realm="kacho", error="insufficient_user_authentication", acr_values="2"' },
  body: {
    code: 16,
    message:
      "authentication level is insufficient: step up with a second factor, or present a credential of another kind",
    details: [],
  },
};
const STEP_UP_DONE: LaneAnswer = {
  status: 200,
  body: { session: SESSION, assurance: { level: "2", level2Reachable: true, missingForLevel2: [] } },
};
const STEP_UP_NOT_DONE = "Подтверждение личности не состоялось: смена адреса требует второго фактора";
const RETRY_WITH_STEP_UP = "Повторить с подтверждением";

/** Окно подтверждения: «Отмена». */
async function cancelStepUp() {
  const dialog = await screen.findByRole("dialog");
  fireEvent.click(within(dialog).getByRole("button", { name: "Отмена" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
}

/** Окно подтверждения: запасной код и «Подтвердить». */
async function stepUpWithBackupCode() {
  const dialog = await screen.findByRole("dialog");
  fireEvent.click(await within(dialog).findByRole("radio", { name: "Запасной код" }));
  fireEvent.change(within(dialog).getByLabelText("Запасной код"), { target: { value: "ABCDEFGHJK" } });
  fireEvent.click(within(dialog).getByRole("button", { name: "Подтвердить" }));
}

describe("смена адреса почты (F8-S6)", () => {
  it("F8S6-01 · покой → форма → код → подтверждение: отсчёт по Retry-After, затем новый адрес и «Адрес подтверждён»", async () => {
    let changed = false;
    lane = installLane({
      "GET /iam/v1/auth/me": () => (changed ? CHANGED_ME : VERIFIED_ME),
      "GET /iam/v1/auth/second-factor": NOT_ENROLLED,
      [`POST ${CHANGE}`]: CHANGE_ACCEPTED("60"),
      [`POST ${CHANGE_CONFIRM}`]: () => {
        changed = true;
        return { status: 200, body: { session: CONFIRMED_SESSION } };
      },
    });
    renderPage();
    const region = await accountRegion();
    expect(region).toHaveTextContent("a@kacho.local");
    await submitNewEmail(region);
    await codeState(region);
    expect(within(region).getByText("Отправить снова можно через 60 с")).toBeInTheDocument();
    expect(within(region).getByRole("button", { name: "Отправить снова" })).toBeDisabled();
    enterCode(region, "ABCDE-FGHJK");
    // Покой — и адрес раздела уже новый: строка письма с N ушла вместе с кодом.
    expect(await within(region).findByRole("button", { name: "Сменить адрес" })).toBeInTheDocument();
    await waitFor(() => expect(region).not.toHaveTextContent("a@kacho.local"));
    expect(region).toHaveTextContent(NEW_EMAIL);
    expect(region).toHaveTextContent("Адрес подтверждён");
    expect(within(region).queryByText(/Письмо с кодом отправлено/)).toBeNull();
    expect(lane.of("POST", CHANGE)[0].body).toEqual({ newEmail: NEW_EMAIL, csrfToken: "tok-email-change-1" });
    expect(lane.of("POST", CHANGE_CONFIRM)[0].body).toEqual({
      code: "ABCDE-FGHJK",
      csrfToken: "tok-email-change-confirm-1",
    });
    // «Кто я» после успеха спрошен заново — новый адрес берётся у края.
    expect(lane.of("GET", "/iam/v1/auth/me").length).toBeGreaterThanOrEqual(2);
  });

  it("F8S6-13 · «Отмена» из формы и из кода возвращает в покой и глаголов не зовёт", async () => {
    lane = installLane({
      "GET /iam/v1/auth/me": VERIFIED_ME,
      "GET /iam/v1/auth/second-factor": NOT_ENROLLED,
      [`POST ${CHANGE}`]: CHANGE_ACCEPTED("60"),
    });
    renderPage();
    const region = await accountRegion();
    fireEvent.click(await within(region).findByRole("button", { name: "Сменить адрес" }));
    fireEvent.change(await within(region).findByLabelText("Новый адрес"), { target: { value: NEW_EMAIL } });
    fireEvent.click(within(region).getByRole("button", { name: "Отмена" }));
    expect(await within(region).findByRole("button", { name: "Сменить адрес" })).toBeInTheDocument();
    expect(within(region).queryByLabelText("Новый адрес")).toBeNull();
    expect(lane.of("POST", CHANGE)).toHaveLength(0);

    await submitNewEmail(region);
    await codeState(region);
    fireEvent.click(within(region).getByRole("button", { name: "Отмена" }));
    expect(await within(region).findByRole("button", { name: "Сменить адрес" })).toBeInTheDocument();
    expect(region).toHaveTextContent("a@kacho.local");
    expect(lane.of("POST", CHANGE)).toHaveLength(1);
    expect(lane.of("POST", CHANGE_CONFIRM)).toHaveLength(0);
  });

  it("F8S6-07 · отказ поля newEmail — у поля, дословно; состояние «форма»", async () => {
    lane = installLane({
      "GET /iam/v1/auth/me": VERIFIED_ME,
      "GET /iam/v1/auth/second-factor": NOT_ENROLLED,
      [`POST ${CHANGE}`]: refusal(400, 3, "Illegal argument newEmail: invalid format"),
    });
    renderPage();
    const region = await accountRegion();
    await submitNewEmail(region, "not-an-address");
    const input = within(region).getByLabelText("Новый адрес");
    await waitFor(() => expect(input).toHaveAttribute("aria-invalid", "true"));
    expect(within(region).getByText("Illegal argument newEmail: invalid format")).toBeInTheDocument();
    expect(within(region).getByRole("button", { name: "Отправить код" })).toBeInTheDocument();
    expect(lane.of("POST", CHANGE_CONFIRM)).toHaveLength(0);
  });

  it("F8S6-06 · 429: «Слишком много попыток», отсчёт с Retry-After, «Отправить код» выключена; близнец — состояние «код»", async () => {
    lane = installLane({
      "GET /iam/v1/auth/me": VERIFIED_ME,
      "GET /iam/v1/auth/second-factor": NOT_ENROLLED,
      [`POST ${CHANGE}`]: (_c, nth) =>
        nth === 1
          ? {
              ...refusal(429, 8, "too many attempts; try again later", "TOO_MANY_ATTEMPTS"),
              headers: { "Retry-After": "42" },
            }
          : CHANGE_ACCEPTED("60"),
    });
    renderPage();
    const region = await accountRegion();
    await submitNewEmail(region);
    expect(await within(region).findByText("Слишком много попыток")).toBeInTheDocument();
    expect(within(region).getByText("Отправить код можно через 42 с")).toBeInTheDocument();
    expect(within(region).getByRole("button", { name: "Отправить код" })).toBeDisabled();
    expect(within(region).queryByText(/Письмо с кодом отправлено/)).toBeNull();
    expect(lane.of("POST", CHANGE)).toHaveLength(1);
  });

  it("F8S6-04 · 401 на предъявлении и «кто я» 200: «Код не подошёл», поле кода очищено, состояние «код»", async () => {
    lane = installLane({
      "GET /iam/v1/auth/me": VERIFIED_ME,
      "GET /iam/v1/auth/second-factor": NOT_ENROLLED,
      [`POST ${CHANGE}`]: CHANGE_ACCEPTED("60"),
      [`POST ${CHANGE_CONFIRM}`]: AUTHN_FAILED,
    });
    const leave = jest.fn();
    renderPageLeaving(leave);
    const region = await accountRegion();
    await submitNewEmail(region);
    await codeState(region);
    const meBefore = lane.of("GET", "/iam/v1/auth/me").length;
    enterCode(region, "WRONG-CODE0");
    expect(await within(region).findByText("Код не подошёл")).toBeInTheDocument();
    expect(within(region).getByLabelText<HTMLInputElement>("Код из письма").value).toBe("");
    await codeState(region);
    expect(region).toHaveTextContent("a@kacho.local");
    expect(lane.of("POST", CHANGE_CONFIRM)).toHaveLength(1);
    expect(lane.of("GET", "/iam/v1/auth/me").length - meBefore).toBe(1);
    expect(leave).not.toHaveBeenCalled();
  });

  it("F8S6-09 · 401 на предъявлении и «сессии нет»: экран входа, «Код не подошёл» нет, второго предъявления нет", async () => {
    let loggedOut = false;
    lane = installLane({
      "GET /iam/v1/auth/me": () => (loggedOut ? { status: 200, body: { user: null } } : VERIFIED_ME),
      "GET /iam/v1/auth/second-factor": NOT_ENROLLED,
      [`POST ${CHANGE}`]: CHANGE_ACCEPTED("60"),
      [`POST ${CHANGE_CONFIRM}`]: () => {
        loggedOut = true;
        return AUTHN_FAILED;
      },
    });
    const leave = jest.fn();
    renderPageLeaving(leave);
    const region = await accountRegion();
    await submitNewEmail(region);
    await codeState(region);
    enterCode(region, "ABCDE-FGHJK");
    await waitFor(() => expect(leave).toHaveBeenCalledWith("/login?returnTo=%2Fsettings"));
    expect(within(region).queryByText("Код не подошёл")).toBeNull();
    expect(lane.of("POST", CHANGE_CONFIRM)).toHaveLength(1);
  });

  it("F8S6-05 · 409 EMAIL_IN_USE: «Этот адрес уже используется» и состояние «форма», адрес прежний", async () => {
    lane = installLane({
      "GET /iam/v1/auth/me": VERIFIED_ME,
      "GET /iam/v1/auth/second-factor": NOT_ENROLLED,
      [`POST ${CHANGE}`]: CHANGE_ACCEPTED("60"),
      [`POST ${CHANGE_CONFIRM}`]: refusal(409, 6, "email address is already in use", "EMAIL_IN_USE"),
    });
    renderPage();
    const region = await accountRegion();
    await submitNewEmail(region);
    await codeState(region);
    enterCode(region, "ABCDE-FGHJK");
    expect(await within(region).findByText("Этот адрес уже используется")).toBeInTheDocument();
    expect(within(region).getByLabelText("Новый адрес")).toBeInTheDocument();
    expect(within(region).getByRole("button", { name: "Отправить код" })).toBeInTheDocument();
    expect(within(region).queryByLabelText("Код из письма")).toBeNull();
    expect(region).toHaveTextContent("a@kacho.local");
  });

  it("F8S6-12 · пустой код уходит к службе; отказ code — у поля, без вопроса «кто я»; состояние «код»", async () => {
    lane = installLane({
      "GET /iam/v1/auth/me": VERIFIED_ME,
      "GET /iam/v1/auth/second-factor": NOT_ENROLLED,
      [`POST ${CHANGE}`]: CHANGE_ACCEPTED("60"),
      [`POST ${CHANGE_CONFIRM}`]: refusal(400, 3, "Illegal argument code: required"),
    });
    renderPage();
    const region = await accountRegion();
    await submitNewEmail(region);
    await codeState(region);
    const codeInput = within(region).getByLabelText("Код из письма");
    expect(codeInput).not.toHaveAttribute("required");
    const meBefore = lane.of("GET", "/iam/v1/auth/me").length;
    const confirm = within(region).getByRole("button", { name: "Подтвердить" });
    expect(confirm).toBeEnabled();
    fireEvent.click(confirm);
    await waitFor(() => expect(codeInput).toHaveAttribute("aria-invalid", "true"));
    expect(within(region).getByText("Illegal argument code: required")).toBeInTheDocument();
    expect(lane.of("POST", CHANGE_CONFIRM)[0].body).toEqual({ code: "", csrfToken: "tok-email-change-confirm-1" });
    expect(lane.of("GET", "/iam/v1/auth/me").length).toBe(meBefore);
    await codeState(region);
  });

  it("F8S6-08 · ответ края на молчание службы: назван, «кто я» не спрошен, состояние «код»", async () => {
    lane = installLane({
      "GET /iam/v1/auth/me": VERIFIED_ME,
      "GET /iam/v1/auth/second-factor": NOT_ENROLLED,
      [`POST ${CHANGE}`]: CHANGE_ACCEPTED("60"),
      [`POST ${CHANGE_CONFIRM}`]: laneAnswerOf(EDGE_CREDENTIAL_STATE_UNKNOWN),
    });
    renderPage();
    const region = await accountRegion();
    await submitNewEmail(region);
    await codeState(region);
    const meBefore = lane.of("GET", "/iam/v1/auth/me").length;
    enterCode(region, "ABCDE-FGHJK");
    expect(await within(region).findByRole("alert")).toHaveTextContent("credential state could not be established");
    expect(lane.of("POST", CHANGE_CONFIRM)).toHaveLength(1);
    expect(lane.of("GET", "/iam/v1/auth/me").length).toBe(meBefore);
    await codeState(region);
  });

  it("F8S6-14 · «Отправить снова» включается концом отсчёта и зовёт запрос с тем же адресом, отсчёт — по новому ответу", async () => {
    lane = installLane({
      "GET /iam/v1/auth/me": VERIFIED_ME,
      "GET /iam/v1/auth/second-factor": NOT_ENROLLED,
      [`POST ${CHANGE}`]: (_c, nth) => CHANGE_ACCEPTED(nth === 1 ? "1" : "60"),
    });
    renderPage();
    const region = await accountRegion();
    await submitNewEmail(region);
    await codeState(region);
    const again = within(region).getByRole("button", { name: "Отправить снова" });
    expect(again).toBeDisabled();
    await waitFor(() => expect(again).toBeEnabled(), { timeout: 3000 });
    fireEvent.click(again);
    expect(await within(region).findByText("Отправить снова можно через 60 с")).toBeInTheDocument();
    expect(lane.of("POST", CHANGE).map((c) => c.body?.newEmail)).toEqual([NEW_EMAIL, NEW_EMAIL]);
    await codeState(region);
    expect(lane.of("POST", CHANGE_CONFIRM)).toHaveLength(0);
  });
  it("F8S6-15 · отказ по уровню на предъявлении, повышение отменено: назван как есть, код цел, «кто я» не спрошен; повтор через повышение", async () => {
    let changed = false;
    lane = installLane({
      "GET /iam/v1/auth/me": () => (changed ? CHANGED_ME : VERIFIED_ME),
      "GET /iam/v1/auth/second-factor": ENROLLED,
      "POST /iam/v1/auth/step-up": STEP_UP_DONE,
      [`POST ${CHANGE}`]: CHANGE_ACCEPTED("60"),
      [`POST ${CHANGE_CONFIRM}`]: (_c, nth) => {
        if (nth <= 2) return LEVEL_REFUSAL;
        changed = true;
        return { status: 200, body: { session: CONFIRMED_SESSION } };
      },
    });
    const leave = jest.fn();
    renderPageLeaving(leave);
    const region = await accountRegion();
    await submitNewEmail(region);
    await codeState(region);
    const meBefore = lane.of("GET", "/iam/v1/auth/me").length;
    enterCode(region, "ABCDE-FGHJK");
    await cancelStepUp();
    // (а) отказ назван как есть: код не проверялся — «Код не подошёл» ложь.
    expect(await within(region).findByText(STEP_UP_NOT_DONE)).toBeInTheDocument();
    expect(within(region).queryByText("Код не подошёл")).toBeNull();
    expect(within(region).getByLabelText<HTMLInputElement>("Код из письма").value).toBe("ABCDE-FGHJK");
    await codeState(region);
    expect(lane.of("GET", "/iam/v1/auth/me").length - meBefore).toBe(0);
    expect(lane.of("POST", CHANGE_CONFIRM)).toHaveLength(1);
    expect(lane.of("POST", "/iam/v1/auth/step-up")).toHaveLength(0);
    expect(leave).not.toHaveBeenCalled();
    // (б) повтор тем же кодом через повышение.
    fireEvent.click(within(region).getByRole("button", { name: RETRY_WITH_STEP_UP }));
    await stepUpWithBackupCode();
    expect(await within(region).findByRole("button", { name: "Сменить адрес" })).toBeInTheDocument();
    await waitFor(() => expect(region).toHaveTextContent(NEW_EMAIL));
    expect(lane.of("POST", CHANGE_CONFIRM).map((c) => c.body?.code)).toEqual([
      "ABCDE-FGHJK",
      "ABCDE-FGHJK",
      "ABCDE-FGHJK",
    ]);
    expect(lane.of("POST", "/iam/v1/auth/step-up")).toHaveLength(1);
  });

  it("F8S6-16 · отказ по уровню на запросе, повышение отменено: назван как есть, адрес цел, состояние «форма»; повтор через повышение", async () => {
    lane = installLane({
      "GET /iam/v1/auth/me": VERIFIED_ME,
      "GET /iam/v1/auth/second-factor": ENROLLED,
      "POST /iam/v1/auth/step-up": STEP_UP_DONE,
      [`POST ${CHANGE}`]: (_c, nth) => (nth <= 2 ? LEVEL_REFUSAL : CHANGE_ACCEPTED("60")),
    });
    renderPage();
    const region = await accountRegion();
    await submitNewEmail(region);
    await cancelStepUp();
    // (а) отказ назван как есть; адрес в поле, экран — «форма».
    expect(await within(region).findByText(STEP_UP_NOT_DONE)).toBeInTheDocument();
    expect(within(region).getByLabelText<HTMLInputElement>("Новый адрес").value).toBe(NEW_EMAIL);
    expect(within(region).queryByLabelText("Код из письма")).toBeNull();
    expect(region).toHaveTextContent("a@kacho.local");
    expect(lane.of("POST", CHANGE)).toHaveLength(1);
    expect(lane.of("POST", "/iam/v1/auth/step-up")).toHaveLength(0);
    // (б) повтор тем же адресом через повышение → «код».
    fireEvent.click(within(region).getByRole("button", { name: RETRY_WITH_STEP_UP }));
    await stepUpWithBackupCode();
    await codeState(region);
    expect(within(region).getByText("Отправить снова можно через 60 с")).toBeInTheDocument();
    expect(lane.of("POST", CHANGE).map((c) => c.body?.newEmail)).toEqual([NEW_EMAIL, NEW_EMAIL, NEW_EMAIL]);
    expect(lane.of("POST", "/iam/v1/auth/step-up")).toHaveLength(1);
  });
  it("F8S6-16 · отказ по уровню на «Отправить снова» в состоянии «код»: повтор — тот же запрос с тем же адресом, а не предъявление", async () => {
    lane = installLane({
      "GET /iam/v1/auth/me": VERIFIED_ME,
      "GET /iam/v1/auth/second-factor": ENROLLED,
      "POST /iam/v1/auth/step-up": STEP_UP_DONE,
      [`POST ${CHANGE}`]: (_c, nth) =>
        nth === 1 ? CHANGE_ACCEPTED("1") : nth <= 3 ? LEVEL_REFUSAL : CHANGE_ACCEPTED("60"),
    });
    renderPage();
    const region = await accountRegion();
    await submitNewEmail(region);
    await codeState(region);
    const again = within(region).getByRole("button", { name: "Отправить снова" });
    await waitFor(() => expect(again).toBeEnabled(), { timeout: 3000 });
    fireEvent.click(again);
    await cancelStepUp();
    expect(await within(region).findByText(STEP_UP_NOT_DONE)).toBeInTheDocument();
    await codeState(region);
    fireEvent.click(within(region).getByRole("button", { name: RETRY_WITH_STEP_UP }));
    await stepUpWithBackupCode();
    expect(await within(region).findByText("Отправить снова можно через 60 с")).toBeInTheDocument();
    expect(lane.of("POST", CHANGE).map((c) => c.body?.newEmail)).toEqual([NEW_EMAIL, NEW_EMAIL, NEW_EMAIL, NEW_EMAIL]);
    expect(lane.of("POST", CHANGE_CONFIRM)).toHaveLength(0);
  });
});

// ─── #3186 · удаление своей личности ──────────────────────────────────────────
//
// Служба объявляет самоудаление (`DELETE /iam/v1/users/{user_id}`, уровень сессии
// `2`, асинхронно; владеющему аккаунтом — `FAILED_PRECONDITION`). Экран ведёт к
// нему: id — только из ответа «кто я», необратимое — подтверждением вводом адреса,
// отказ «сперва аккаунты» — названным шагом, успех — уходом документом на вход.
describe("#3186 · удаление своей учётной записи", () => {
  function routes(operation: LaneAnswer): Record<string, LaneAnswer> {
    return {
      "GET /iam/v1/auth/me": SIGNED_IN,
      "GET /iam/v1/auth/second-factor": NOT_ENROLLED,
      "DELETE /iam/v1/users/usr-1": { status: 200, body: { id: "op-del-1", done: false } },
      "GET /operations/op-del-1": operation,
    };
  }

  async function confirmDeletion() {
    const section = await screen.findByRole("region", { name: "Удаление учётной записи" });
    fireEvent.click(within(section).getByRole("button", { name: "Удалить учётную запись" }));
    const dialog = await screen.findByRole("dialog");
    const confirm = within(dialog).getByRole("button", { name: "Удалить навсегда" });
    // Необратимое не подтверждается одним щелчком: кнопка закрыта до ввода адреса.
    expect(confirm).toBeDisabled();
    fireEvent.change(within(dialog).getByRole("textbox"), { target: { value: "другой@kacho.local" } });
    expect(confirm).toBeDisabled();
    fireEvent.change(within(dialog).getByRole("textbox"), { target: { value: "a@kacho.local" } });
    expect(confirm).toBeEnabled();
    fireEvent.click(confirm);
  }

  it("удаление адресовано СВОЕЙ личности из ответа «кто я»; успех уводит документом на вход", async () => {
    lane = installLane(routes({ status: 200, body: { id: "op-del-1", done: true } }));
    const leave = jest.fn<(to: string) => void>();
    render(
      <MemoryRouter initialEntries={["/settings?userId=usr-чужой"]}>
        <AccountSettingsPage leave={leave} />
      </MemoryRouter>,
    );
    await confirmDeletion();
    await waitFor(() => expect(leave).toHaveBeenCalledWith("/login"));
    expect(lane.calls.filter((c) => c.method === "DELETE").map((c) => c.path)).toEqual(["/iam/v1/users/usr-1"]);
  });

  it("владеющему аккаунтом служба отказывает — экран называет шаг «сперва аккаунты» и никуда не уводит", async () => {
    lane = installLane(
      routes({
        status: 200,
        body: { id: "op-del-1", done: true, error: { code: 9, message: "User usr-1 owns accounts and cannot be deleted" } },
      }),
    );
    const leave = jest.fn<(to: string) => void>();
    render(
      <MemoryRouter initialEntries={["/settings"]}>
        <AccountSettingsPage leave={leave} />
      </MemoryRouter>,
    );
    await confirmDeletion();
    expect(await screen.findByTestId("self-removal-next-step")).toHaveTextContent(
      "Сначала удалите аккаунты, которыми вы владеете",
    );
    expect(screen.getByText("User usr-1 owns accounts and cannot be deleted")).toBeInTheDocument();
    expect(leave).not.toHaveBeenCalled();
  });
});
