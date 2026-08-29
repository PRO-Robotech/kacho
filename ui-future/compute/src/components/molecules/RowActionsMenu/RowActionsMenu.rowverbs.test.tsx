// Объявленный глагол строки обязан доехать до меню ЭТОГО модуля (#560).
//
// Предмет пробы — не общая реализация (её проверяет собственная суита в
// `shared/`), а ПУТЬ, которым пользуется модуль: `ResourceShell` показывает
// встроенную таблицу ребёнка и берёт меню по адресу `@/components/molecules/
// RowActionsMenu`. Пока по этому адресу лежала копия, объявление `spec.rowVerbs`
// до экрана не доезжало молча: спека глагол несёт, столбца действий нет, отказа
// нет — форма без содержания на уровне объявления.
//
// Инъекция, доказывающая, что проба способна упасть: вернуть копию по этому
// адресу — оба утверждения краснеют (копия про `rowVerbs` не знает и при
// отсутствии правки/удаления/перемещения не рисует ВООБЩЕ ничего).
//
// Спека здесь синтетическая, и это осознанно: предмет — чтение поля `rowVerbs`
// самим меню, а не состав реестра модуля. Реестр меняется своей задачей, и
// проба, привязанная к его сегодняшнему составу, истекла бы вместе с ним.
//
// ФОРМА показа здесь НЕ утверждается — только то, что глагол доезжает до
// экрана. У ресурса, чьё единственное действие — глагол, столбец показывает его
// подписанной кнопкой, а не пунктом меню (#687): два нажатия там, где хватает
// одного. Утверждение, прибитое к роли `menuitem`, закрепляло бы форму, а
// предмет пробы — путь модуля.

import { jest } from "@jest/globals";
import React from "react";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { antdStub } from "@shared/test/antd-stub";

jest.unstable_mockModule("antd", () => ({
  ...antdStub(),
  Tooltip: ({ children, title }: React.PropsWithChildren<{ title?: React.ReactNode }>) =>
    React.createElement("span", { "data-tooltip": typeof title === "string" ? title : "" }, children),
}));

const { REGISTRY } = await import("@/lib/resource-registry");
// Адрес МОДУЛЯ, а не `@shared/…`: по нему лежала копия, и проба через `@shared/…`
// осталась бы зелёной при живом дефекте. Сегодня по этому адресу ре-экспорт, и
// предмет пробы стал уже: она утверждает, что адрес модуля ведёт к реализации,
// читающей `spec.rowVerbs`, — вернуть сюда копию значит снова её уронить.
const { RowActionsMenu, resourceHasRowActions } = await import("@/components/molecules/RowActionsMenu");

/** Ресурс без правки/удаления/перемещения, у которого ЕДИНСТВЕННОЕ действие — глагол. */
function specWithVerb() {
  const base = Object.values(REGISTRY)[0] as unknown as Record<string, unknown>;
  return {
    ...base,
    // `accounts` — из закрытого списка «перемещать нечем», поэтому ни один из
    // встроенных пунктов не применим и в меню может остаться только глагол.
    id: "accounts",
    ops: { create: false, update: false, delete: false },
    rowVerbs: [
      {
        key: "block",
        resolve: () => ({
          label: "Запретить участие",
          icon: null,
          danger: true,
          verbPath: (id: string) => `/iam/v1/users/${id}:block`,
          confirmLabel: "Запретить",
          title: "Запретить участие",
          body: "тело подтверждения",
        }),
      },
    ],
  } as never;
}

/**
 * Подписи ДЕЙСТВИЙ строки, в какой бы форме столбец их ни показывал: пунктом
 * меню либо кнопкой единственного действия. Обе роли спрашиваются вместе —
 * иначе проба закрепила бы форму вместо предмета.
 */
function actionLabels(): string[] {
  return [...screen.queryAllByRole("menuitem"), ...screen.queryAllByRole("button")].map((b) => b.textContent ?? "");
}

describe("объявленный глагол доезжает до меню строки этого модуля (#560)", () => {
  it("ресурс, у которого действие ТОЛЬКО глагол, столбец действий получает", () => {
    // Без этого слагаемого встроенная таблица ребёнка не нарисует столбец вовсе,
    // и пункт не появится независимо от меню.
    expect(resourceHasRowActions(specWithVerb())).toBe(true);
  });

  it("действие-глагол доезжает до столбца действий строки", () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <MemoryRouter initialEntries={["/x"]}>
          <RowActionsMenu
            spec={specWithVerb()}
            row={{ id: "usr-1", invite_status: "ACTIVE" }}
            basePath="/x"
            projectId={null}
            editAsPanel
          />
        </MemoryRouter>
      </QueryClientProvider>,
    );
    expect(actionLabels()).toContain("Запретить участие");
  });
});
