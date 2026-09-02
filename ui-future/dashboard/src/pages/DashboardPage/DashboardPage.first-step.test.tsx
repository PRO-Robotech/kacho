import { render, screen, waitFor } from "@testing-library/react";
import { jest } from "@jest/globals";
import { DashboardPage } from ".";
import type { HostContext } from "../../utils";

/**
 * ПЕРВЫЙ ЭКРАН НАЗЫВАЕТ ПЕРВЫЙ ШАГ (#1613).
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧТО ЛОВИТ
 *
 * Человек, впервые вошедший в консоль, читал на главной «Выберите проект в
 * дереве слева» и то же самое на пяти плитках из шести. Выбирать было нечего:
 * дерево слева сообщало «Аккаунтов нет», а слов «создайте аккаунт» на экране не
 * было ни одного. Продукт велел сделать то, чего клиент сделать не может, и
 * молчал о настоящем первом шаге — отказ, не восстанавливающий следующий шаг.
 *
 * Ощущение при этом ложное вдвойне: пять плиток под замком читаются как «мне
 * сюда не выдали прав», а не как «мне надо завести аккаунт».
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПОЧЕМУ ТРИ СОСТОЯНИЯ, А НЕ ДВА
 *
 * «Проект выбран» и «проект не выбран» — недостаточное деление: во втором
 * прячутся два РАЗНЫХ положения с разными следующими шагами. Есть аккаунт, но
 * не выбран проект — шаг «выберите». Аккаунтов нет вовсе — шаг «создайте
 * аккаунт», и «выберите» здесь неисполнимо.
 *
 * Отличать их обязательно ещё и потому, что третье состояние — «список не
 * прочитан»: пока чтение идёт, утверждать «аккаунтов нет» нельзя, это было бы
 * утверждение, которого никто не проверял.
 */

const emptyContext: HostContext = { account: null, project: null };

function pathOf(input: unknown): string {
  if (typeof input === "string") return input;
  const url = (input as { url?: unknown })?.url;
  return typeof url === "string" ? url : String(input);
}

function jsonResponse(body: unknown) {
  return Promise.resolve({
    ok: true,
    text: () => Promise.resolve(JSON.stringify(body)),
    statusText: "OK",
  } as Response);
}

/** Стенд, у которого аккаунтов НЕТ — положение нового клиента. */
function stubNoAccounts() {
  jest.spyOn(global, "fetch").mockImplementation((input) => {
    const path = pathOf(input);
    if (path.startsWith("/iam/v1/accounts")) return jsonResponse({ accounts: [] });
    return jsonResponse({});
  });
}

/** Стенд, у которого аккаунт ЕСТЬ, а проект не выбран. */
function stubWithAccount() {
  jest.spyOn(global, "fetch").mockImplementation((input) => {
    const path = pathOf(input);
    if (path.startsWith("/iam/v1/accounts")) return jsonResponse({ accounts: [{ id: "account-1", name: "acc" }] });
    if (path.startsWith("/iam/v1/projects")) return jsonResponse({ projects: [] });
    return jsonResponse({});
  });
}

describe("первый экран называет первый шаг (#1613)", () => {
  beforeEach(() => {
    global.fetch = jest.fn<typeof fetch>();
  });
  afterEach(() => jest.restoreAllMocks());

  it("аккаунтов нет — экран зовёт завести аккаунт и даёт куда пойти", async () => {
    stubNoAccounts();
    render(<DashboardPage context={emptyContext} />);

    const step = await screen.findByTestId("dashboard-first-step");
    expect(step.textContent).toMatch(/аккаунт/i);

    // Не только слова, но и ХОД: шаг обязан быть достижим отсюда, иначе экран
    // называет действие и оставляет клиента его искать.
    const go = screen.getByTestId("dashboard-first-step-action");
    expect(go).toBeInTheDocument();
  });

  it("аккаунтов нет — экран НЕ велит выбирать проект: выбирать нечего", async () => {
    stubNoAccounts();
    render(<DashboardPage context={emptyContext} />);

    await screen.findByTestId("dashboard-first-step");
    // Отрицание в паре с положительным выше: без него оно было бы зелено и на
    // экране, который не говорит вообще ничего.
    expect(screen.queryByText(/Выберите проект в дереве слева/)).not.toBeInTheDocument();
  });

  it("аккаунт есть, проект не выбран — экран по-прежнему зовёт выбрать проект", async () => {
    // ВТОРАЯ СТОРОНА. Без неё правка могла бы снять подсказку про выбор проекта
    // вообще, и клиент с аккаунтом остался бы без следующего шага.
    stubWithAccount();
    render(<DashboardPage context={emptyContext} />);

    await waitFor(() => {
      expect(screen.getByText(/Выберите проект в дереве слева/)).toBeInTheDocument();
    });
    expect(screen.queryByTestId("dashboard-first-step-action")).not.toBeInTheDocument();
  });

  it("список ещё не прочитан — экран не утверждает, что аккаунтов нет", () => {
    // Третье состояние. «Ничего не прочитано» и «прочитано и пусто» — разные
    // вещи, и первое не даёт права на утверждение о втором.
    jest.spyOn(global, "fetch").mockImplementation(() => new Promise(() => {}));
    render(<DashboardPage context={emptyContext} />);

    expect(screen.queryByTestId("dashboard-first-step-action")).not.toBeInTheDocument();
  });
});
