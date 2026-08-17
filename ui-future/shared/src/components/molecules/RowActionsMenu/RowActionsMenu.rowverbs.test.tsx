// Действие-ГЛАГОЛ на строке списка (`…/{id}:verb`) — состав меню, выбор по
// состоянию строки и то, что уходит к краю при подтверждении.
//
// Почему отдельным файлом от `RowActionsMenu.test.tsx`: там предмет — четыре
// встроенных пункта (просмотр/правка/перемещение/удаление), и его дублёр antd
// пунктов-подсказок не рисует вовсе. Здесь предмет — ОБЪЯВЛЯЕМОЕ действие, и
// подсказка с причиной недоступности входит в утверждение: пункт, выключенный
// молча, неотличим от возможности, которой нет.
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

interface MockItem {
  key?: string;
  label?: React.ReactNode;
  type?: string;
  disabled?: boolean;
  onClick?: (info: { domEvent: { stopPropagation: () => void } }) => void;
}

jest.unstable_mockModule("antd", () => ({
  ...antdStub(),
  Tooltip: ({ children, title }: React.PropsWithChildren<{ title?: React.ReactNode }>) =>
    React.createElement("span", { "data-tooltip": typeof title === "string" ? title : "" }, children),
  Dropdown: ({ children, menu }: React.PropsWithChildren<{ menu?: { items?: MockItem[] } }>) =>
    React.createElement(
      "div",
      null,
      children,
      React.createElement(
        "div",
        { "data-testid": "menu" },
        (menu?.items ?? []).map((item, i) =>
          item.type === "divider"
            ? React.createElement("hr", { key: `d${i}` })
            : React.createElement(
                "button",
                {
                  key: item.key ?? i,
                  type: "button",
                  disabled: Boolean(item.disabled),
                  onClick: (e: React.MouseEvent) =>
                    item.onClick?.({ domEvent: { stopPropagation: () => e.stopPropagation() } }),
                },
                item.label,
              ),
        ),
      ),
    ),
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
const { RowActionsMenu, resourceHasRowActions } = await import("./RowActionsMenu");

function menuButtons(): HTMLButtonElement[] {
  return [...screen.getByTestId("menu").querySelectorAll("button")] as HTMLButtonElement[];
}

function menuLabels(): string[] {
  return menuButtons().map((b) => b.textContent ?? "");
}

function itemByLabel(label: string): HTMLButtonElement | undefined {
  return menuButtons().find((b) => (b.textContent ?? "").includes(label));
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
    expect(itemByLabel("Запретить участие")!.disabled).toBe(true);
    expect(
      screen.getByTestId("menu").querySelector("[data-tooltip]")?.getAttribute("data-tooltip") ?? "",
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
    await waitFor(() => expect(apiAction).toHaveBeenCalledWith("/iam/v1/users/usr-1:block"));
  });

  it("возврат участия уходит своим глаголом", async () => {
    renderUsersMenu({ id: "usr-2", email: "b@kacho.local", invite_status: "BLOCKED" });
    fireEvent.click(itemByLabel("Вернуть участие")!);
    fireEvent.click(screen.getByRole("button", { name: "Вернуть" }));
    await waitFor(() => expect(apiAction).toHaveBeenCalledWith("/iam/v1/users/usr-2:unblock"));
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
