import { jest } from "@jest/globals";
import { render, screen } from "@testing-library/react";
import { contextApi } from "@shared/lib/context-store";
import type { ResourceSpec } from "@shared/lib/resource-registry";
import type { IamScopedListShell as IamScopedListShellExport } from "./IamScopedListShell";

jest.unstable_mockModule("@/components/organisms/ResourceListPage", () => ({
  ResourceListPage: (p: Record<string, unknown>) => (
    <div
      data-testid="list-page"
      data-spec={String((p.spec as ResourceSpec).id)}
      data-parent-field={String(p.parentField)}
      data-parent-value={String(p.parentValue)}
      data-disable-child-route={String(p.disableChildRoute)}
      data-panel-forms={String(p.panelForms)}
    />
  ),
}));

let IamScopedListShell: typeof IamScopedListShellExport;

const spec = { id: "projects", plural: "Projects" } as ResourceSpec;

/**
 * Текст заглушки «ничего нет» — ровно тот, что ВИДИТ пользователь.
 *
 * Прежняя редакция читала его из атрибута `description`, потому что старый
 * дублёр antd прятал свойство туда вместо отрисовки. Это утверждало форму
 * подмены, а не наблюдаемое: настоящий `Empty` рисует пояснение текстом.
 * Дублёр приведён к настоящему поведению, и проба следует за ним — теперь она
 * читает то же, что прочёл бы человек.
 */
const emptyText = (root: HTMLElement): string | null => root.textContent?.match(/Выберите[^.]*\./)?.[0] ?? null;

describe("IamScopedListShell", () => {
  beforeAll(async () => {
    ({ IamScopedListShell } = await import("./IamScopedListShell"));
  });

  beforeEach(() => {
    window.localStorage.clear();
    contextApi.setAccount(null);
  });

  it("без выбранного аккаунта список не запрашивается вовсе", () => {
    // Backend требует account_id; список без него означал бы запрос, который
    // отвергается, и пустую таблицу вместо объяснения.
    render(<IamScopedListShell spec={spec} />);

    expect(screen.queryByTestId("list-page")).not.toBeInTheDocument();
  });

  it("вместо пустой таблицы объясняет, чего не хватает, и называет ресурс", () => {
    const { container } = render(<IamScopedListShell spec={spec} />);

    // Слово области — то же, что на ручке в шапке, и берётся у словаря подписей.
    // Здесь пиннилось «Account» латиницей: экран отправлял искать ручку, которая
    // подписана «Аккаунт» (#1609).
    expect(emptyText(container)).toBe("Выберите аккаунт вверху секции, чтобы увидеть Projects.");
  });

  it("объяснение называет ИМЕННО тот ресурс, о котором речь", () => {
    const { container } = render(
      <IamScopedListShell spec={{ id: "service-accounts", plural: "Service Accounts" } as ResourceSpec} />,
    );

    expect(emptyText(container)).toBe("Выберите аккаунт вверху секции, чтобы увидеть Service Accounts.");
  });

  it("с выбранным аккаунтом показывает список, привязанный к этому аккаунту", () => {
    contextApi.setAccount({ id: "acc-1", name: "Первый" });

    const { container } = render(<IamScopedListShell spec={spec} />);

    const list = screen.getByTestId("list-page");
    expect(list).toHaveAttribute("data-spec", "projects");
    expect(list).toHaveAttribute("data-parent-field", "account_id");
    expect(list).toHaveAttribute("data-parent-value", "acc-1");
    expect(list).toHaveAttribute("data-panel-forms", "true");
    expect(emptyText(container)).toBeNull();
  });

  it("запрет дочернего маршрута доезжает до списка как есть", () => {
    contextApi.setAccount({ id: "acc-1", name: "Первый" });

    render(<IamScopedListShell spec={spec} disableChildRoute />);

    expect(screen.getByTestId("list-page")).toHaveAttribute("data-disable-child-route", "true");
  });
});
