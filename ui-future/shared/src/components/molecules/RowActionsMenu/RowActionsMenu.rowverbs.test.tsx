// Действие-ГЛАГОЛ на строке списка (`…/{id}:verb`) — состав меню, выбор по
// состоянию строки и то, что уходит к краю при подтверждении.
//
// Почему отдельным файлом от `RowActionsMenu.test.tsx`: там предмет — четыре
// встроенных пункта (просмотр/правка/перемещение/удаление). Здесь предмет —
// ОБЪЯВЛЯЕМОЕ действие, и подсказка с причиной недоступности входит в
// утверждение: пункт, выключенный молча, неотличим от возможности, которой нет.
//
// Дублёр `Tooltip` заменён НАМЕРЕННО: общий стенд роняет `title` (подсказка
// рисуется только детьми), поэтому на нём «причина названа» зеленело бы и при
// пустой причине — то есть проба утверждала бы ровно ничего.

import { jest } from "@jest/globals";
import React from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { antdStub } from "@shared/test/antd-stub";

jest.unstable_mockModule("antd", () => ({
  ...antdStub(),
  Tooltip: ({ children, title }: React.PropsWithChildren<{ title?: React.ReactNode }>) =>
    React.createElement("span", { "data-tooltip": typeof title === "string" ? title : "" }, children),
}));

// Подменяется ТОЛЬКО отправка: остальные экспорты берутся у настоящего модуля.
// Частичный дублёр, объявленный целиком, роняет связывание на первом же
// экспорте, которого он не перечислил, — и падение выглядит как дефект пробы,
// а не как её недосказанность.
const realClient = await import("@shared/api/client");
const apiAction = jest.fn<(path: string, body?: unknown) => Promise<unknown>>();
jest.unstable_mockModule("@shared/api/client", () => ({
  ...realClient,
  api: { ...realClient.api, action: apiAction },
}));

// Личность вызывающего — предмет одного из утверждений (предупреждение о
// действии над СОБОЙ). Подменяется, чтобы проба не поднимала поток входа.
const realAuth = await import("@shared/contexts/AuthContext");
const selfUserId = { current: undefined as string | undefined };
jest.unstable_mockModule("@shared/contexts/AuthContext", () => ({
  ...realAuth,
  useSelfUserId: () => selfUserId.current,
}));

const { REGISTRY } = await import("@shared/lib/resource-registry");
// Хранилище области — НАСТОЯЩЕЕ, а не дублёр: предмет утверждений ниже в том,
// что пункт исключения читает выбранный аккаунт оттуда же, откуда его читают
// страницы. Дублёр доказывал бы, что работает дублёр.
const { contextApi } = await import("@shared/lib/context-store");
const { RowActionsMenu, resourceHasRowActions } = await import("./RowActionsMenu");

// Пункт меню — `<li role="menuitem">`, как у настоящего antd, а не кнопка:
// собственный дублёр здесь заводил `<button disabled>`, то есть роль и свойство,
// которых настоящее меню не производит. Утверждение о выключенном пункте читает
// `aria-disabled` — единственное, что об этом говорит НАСТОЯЩИЙ виджет.
function menuItems(): HTMLElement[] {
  return screen.getAllByRole("menuitem");
}

function menuLabels(): string[] {
  return menuItems().map((b) => b.textContent ?? "");
}

function itemByLabel(label: string): HTMLElement | undefined {
  return menuItems().find((b) => (b.textContent ?? "").includes(label));
}

function Harness({ children }: React.PropsWithChildren) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return (
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={["/iam/users"]}>{children}</MemoryRouter>
    </QueryClientProvider>
  );
}

function renderUsersMenu(row: Record<string, unknown>) {
  return render(
    <Harness>
      <RowActionsMenu spec={REGISTRY.users} row={row} basePath="/iam/users" projectId={null} />
    </Harness>,
  );
}

beforeEach(() => {
  apiAction.mockReset();
  apiAction.mockResolvedValue({ operation: { id: "op-1", done: false } });
  selfUserId.current = undefined;
  // Область сбрасывается МЕЖДУ пробами: хранилище общее на весь файл, и
  // аккаунт, оставшийся от соседа, сделал бы «пункта нет без области» зелёным
  // по случайности порядка.
  contextApi.setAccount(null);
});

describe("действие-глагол на строке пользователя", () => {
  it("у действующего участия предлагается ЗАПРЕТ, и возврата рядом нет", () => {
    // verifies #440 — предмет: возможность есть у края и её не было в консоли.
    renderUsersMenu({ id: "usr-1", email: "a@kacho.local", invite_status: "ACTIVE" });
    expect(menuLabels()).toContain("Запретить участие");
    expect(menuLabels()).not.toContain("Вернуть участие");
  });

  it("у запрещённого участия предлагается ВОЗВРАТ, и запрета рядом нет", () => {
    // Действие ВЫБИРАЕТСЯ состоянием, а не показывается парой: предлагать
    // «запретить» уже запрещённому значит предлагать вызов, который ничего не
    // меняет.
    renderUsersMenu({ id: "usr-2", email: "b@kacho.local", invite_status: "BLOCKED" });
    expect(menuLabels()).toContain("Вернуть участие");
    expect(menuLabels()).not.toContain("Запретить участие");
  });

  it("у неподтверждённого приглашения пункт ВИДЕН, выключен и называет причину", () => {
    // Скрытый пункт неотличим от несуществующей возможности — пользователь
    // ищет её там, где её нет. Край такой вызов отвергает, поэтому живая
    // кнопка обещала бы то, чего не будет.
    renderUsersMenu({ id: "usr-3", email: "c@kacho.local", invite_status: "PENDING" });
    // Пункт назван состоянием, а не отсутствием: «нет пункта» и «пункт выключен»
    // — разные исходы, и первый здесь был бы дефектом.
    expect(menuLabels()).toContain("Запретить участие");
    expect(itemByLabel("Запретить участие")!.getAttribute("aria-disabled")).toBe("true");
    expect(
      itemByLabel("Запретить участие")!.querySelector("[data-tooltip]")?.getAttribute("data-tooltip") ?? "",
    ).toMatch(/приглашени/i);
  });

  it("подтверждение над СВОЕЙ строкой называет цену: снять запрет самому нельзя", () => {
    // Предупреждение обязано быть ДО нажатия. Иначе оператор выключает себя
    // одним движением и узнаёт цену потом.
    selfUserId.current = "usr-1";
    renderUsersMenu({ id: "usr-1", email: "a@kacho.local", invite_status: "ACTIVE" });
    fireEvent.click(itemByLabel("Запретить участие")!);
    expect(screen.getByRole("dialog").textContent ?? "").toMatch(/самостоятельно/i);
  });

  it("подтверждение над ЧУЖОЙ строкой цену самоблокировки НЕ называет", () => {
    // Парный положительный контроль: без него утверждение выше зеленело бы на
    // предупреждении, показанном ВСЕМ, — то есть на тексте, который перестал
    // что-либо различать.
    selfUserId.current = "usr-9";
    renderUsersMenu({ id: "usr-1", email: "a@kacho.local", invite_status: "ACTIVE" });
    fireEvent.click(itemByLabel("Запретить участие")!);
    const text = screen.getByRole("dialog").textContent ?? "";
    expect(text).not.toMatch(/самостоятельно/i);
    expect(text).toMatch(/a@kacho\.local/);
  });

  it("подтверждение уходит ГЛАГОЛОМ края, а не правкой поля", async () => {
    // У действия нет маски — значит «забыть поле» и выключить всех, кого
    // коснулся, здесь невозможно by construction. Адрес утверждается дословно:
    // именно он сверяется с контрактом переписью глаголов.
    //
    // Ожидание здесь — не пауза: отправка уходит в микрозадачу, и утверждение
    // без него читало бы состояние ДО вызова, то есть зеленело бы при любом
    // адресе и краснело при верном.
    renderUsersMenu({ id: "usr-1", email: "a@kacho.local", invite_status: "ACTIVE" });
    fireEvent.click(itemByLabel("Запретить участие")!);
    fireEvent.click(screen.getByRole("button", { name: "Запретить" }));
    // Второй довод — `undefined`, и он утверждается ЯВНО (#1127). У глагола
    // появилась возможность нести тело; у ЭТОГО глагола тела нет, и «нет тела»
    // обязано быть сказано, а не подразумеваться: `toHaveBeenCalledWith` с одним
    // доводом покраснел бы, а с двумя — закрепляет, что запрет не начал слать
    // ничего лишнего.
    await waitFor(() => expect(apiAction).toHaveBeenCalledWith("/iam/v1/users/usr-1:block", undefined));
  });

  it("возврат участия уходит своим глаголом", async () => {
    renderUsersMenu({ id: "usr-2", email: "b@kacho.local", invite_status: "BLOCKED" });
    fireEvent.click(itemByLabel("Вернуть участие")!);
    fireEvent.click(screen.getByRole("button", { name: "Вернуть" }));
    await waitFor(() => expect(apiAction).toHaveBeenCalledWith("/iam/v1/users/usr-2:unblock", undefined));
  });

  // ── исключение из аккаунта (#1127) ─────────────────────────────────────────
  //
  // ПОЧЕМУ ЭТО ВТОРОЙ ГЛАГОЛ, А НЕ ПЕРЕНАЦЕЛЕННЫЙ ПЕРВЫЙ. Запрет выше выключает
  // человеку вход НА ПЛАТФОРМУ (одна строка личности на все его аккаунты) и
  // требует прав администратора облака; исключение снимает строку ЧЛЕНСТВА в
  // названном аккаунте и остаётся распорядителю аккаунта. Разные предметы,
  // разные адресаты, разные отношения — значит и пункта два.
  //
  // ПАРА ОБЯЗАТЕЛЬНА: пункт ПОЯВЛЯЕТСЯ, когда область выбрана, и ЕГО НЕТ, когда
  // не выбрана. Без второй половины «пункт есть» зеленело бы и на реестре,
  // который показывает его всегда, — а такой пункт отправил бы запрос без
  // половины предмета.
  it("исключение из аккаунта предлагается, когда область выбрана", () => {
    contextApi.setAccount({ id: "acc-9", name: "acme" });
    renderUsersMenu({ id: "usr-1", email: "a@kacho.local", invite_status: "ACTIVE" });
    expect(menuLabels()).toContain("Исключить из аккаунта");
    // Контроль соседа: запрет остаётся рядом и своим предметом.
    expect(menuLabels()).toContain("Запретить участие");
  });

  it("без выбранной области пункта исключения НЕТ вовсе", () => {
    renderUsersMenu({ id: "usr-1", email: "a@kacho.local", invite_status: "ACTIVE" });
    expect(menuLabels()).not.toContain("Исключить из аккаунта");
  });

  it("исключение уходит своим глаголом И НЕСЁТ АККАУНТ ТЕЛОМ", async () => {
    // Тело — не оформление: предмет исключения ПАРА, у человека аккаунтов
    // бывает несколько, и запрос без второй половины вывел бы его не оттуда.
    contextApi.setAccount({ id: "acc-9", name: "acme" });
    renderUsersMenu({ id: "usr-1", email: "a@kacho.local", invite_status: "ACTIVE" });
    fireEvent.click(itemByLabel("Исключить из аккаунта")!);
    fireEvent.click(screen.getByRole("button", { name: "Исключить" }));
    await waitFor(() =>
      expect(apiAction).toHaveBeenCalledWith("/iam/v1/users/usr-1:removeFromAccount", { accountId: "acc-9" }),
    );
  });

  it("подтверждение исключения называет цену и НЕ обещает того, чего действие не делает", () => {
    contextApi.setAccount({ id: "acc-9", name: "acme" });
    renderUsersMenu({ id: "usr-1", email: "a@kacho.local", invite_status: "ACTIVE" });
    fireEvent.click(itemByLabel("Исключить из аккаунта")!);
    const text = screen.getByRole("dialog").textContent ?? "";
    // Границу действия текст обязан назвать: личность сохраняется.
    expect(text).toMatch(/личност/i);
    // И порядок, который держит база: пока права есть, край откажет.
    expect(text).toMatch(/прав/i);
  });

  // ── подтверждение не обещает пункта, которого в ЭТОМ ЖЕ меню нет (#1208) ───
  //
  // ИНВАРИАНТ ПАРЫ, а не два независимых утверждения: если подтверждение
  // НАЗЫВАЕТ пункт, то либо этот пункт есть в том же меню, либо подтверждение
  // называет условие его появления. Обе половины меряются в одном состоянии —
  // порознь они зеленеют друг на друге.
  //
  // ПОЧЕМУ НЕ «УБРАТЬ ФРАЗУ». Снятие обещания закрыло бы расхождение и потеряло
  // бы то, ради чего фраза написана: человек в области-невыбранной так и не
  // узнал бы, что узкое действие существует, — и выполнил бы широкое. Поэтому
  // пункт назван в ОБОИХ состояниях, и различается только условие.
  //
  // ЧЕГО ЭТОТ ЮНИТ НЕ ЗАКРЫВАЕТ, и это сказано, а не умолчано. Область он задаёт
  // прямой записью в хранилище — то есть в КОНЦЕ цепочки «шапка каркаса →
  // хранилище браузера → провязка страницы модуля → общее хранилище области», —
  // и зелен при её обрыве в любом месте. Пробы браузером на эту пару СЕЙЧАС НЕТ,
  // и она здесь не подразумевается: обещание живёт в НЕ-СВОЕЙ ветке
  // подтверждения, а чужую строку в браузере не завести (второго человека в
  // аккаунте требует приглашение со ступенью доверия 2). Долг записан с
  // предикатом снятия в `ui-future/e2e/specs/users.spec.ts`.
  it("без области подтверждение запрета НАЗЫВАЕТ УСЛОВИЕ пункта, которого в меню нет", () => {
    // verifies #1208 — КРАСНАЯ до фикса задачи.
    renderUsersMenu({ id: "usr-1", email: "a@kacho.local", invite_status: "ACTIVE" });

    // ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ к отрицанию ниже. «Пункта нет» истинно и на меню,
    // которое не отрисовалось вовсе, — сосед по ТОМУ ЖЕ перечню глаголов
    // доказывает, что меню собрано и глаголы разрешились.
    expect(menuLabels()).toContain("Запретить участие");
    // Половина первая: названный пункт в этом меню ОТСУТСТВУЕТ.
    expect(menuLabels()).not.toContain("Исключить из аккаунта");

    fireEvent.click(itemByLabel("Запретить участие")!);
    const text = screen.getByRole("dialog").textContent ?? "";
    // ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ окна: оно отрисовано и несёт свой предмет. Без
    // него утверждения о тексте зеленели бы на пустой строке.
    expect(text).toMatch(/НА ПЛАТФОРМУ/);

    // Половина вторая: узкое действие по-прежнему НАЗВАНО (иначе человек о нём
    // не узнает и выполнит широкое) — и названо условие, при котором оно
    // появляется.
    expect(text).toContain("Исключить из аккаунта");
    expect(text).toMatch(/выберите область/i);
  });

  it("с выбранной областью подтверждение обещает пункт БЕЗУСЛОВНО — и пункт в меню есть", () => {
    // verifies #1208 — вторая половина пары. Без неё «текст называет условие»
    // зеленело бы и на тексте, который называет его ВСЕГДА: тогда человек,
    // область уже выбравший, читал бы указание выбрать её ещё раз.
    contextApi.setAccount({ id: "acc-9", name: "acme" });
    renderUsersMenu({ id: "usr-1", email: "a@kacho.local", invite_status: "ACTIVE" });

    expect(menuLabels()).toContain("Запретить участие");
    expect(menuLabels()).toContain("Исключить из аккаунта");

    fireEvent.click(itemByLabel("Запретить участие")!);
    const text = screen.getByRole("dialog").textContent ?? "";
    expect(text).toMatch(/НА ПЛАТФОРМУ/);
    expect(text).toContain("Исключить из аккаунта");
    expect(text).not.toMatch(/выберите область/i);
  });
});

describe("resourceHasRowActions видит объявленные глаголы", () => {
  it("ресурс, у которого нет ни правки, ни удаления, ни перемещения, но есть глагол — действия имеет", () => {
    // Иначе объявленный глагол молча не доедет до экрана: столбец действий
    // вообще не рисуется, и объявление окажется формой без содержания.
    const spec = {
      ...REGISTRY.users,
      id: "accounts", // из закрытого списка «перемещать нечем»
      ops: { create: false, update: false, delete: false },
    };
    expect(resourceHasRowActions(spec)).toBe(true);
  });

  it("тот же ресурс БЕЗ объявленных глаголов действий не имеет", () => {
    // Парный отрицательный контроль: без него утверждение выше зеленело бы от
    // любой другой причины, по которой функция отвечает «да».
    const spec = {
      ...REGISTRY.users,
      id: "accounts",
      ops: { create: false, update: false, delete: false },
      rowVerbs: undefined,
    };
    expect(resourceHasRowActions(spec)).toBe(false);
  });
});
