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
 * Текст заглушки «ничего нет».
 *
 * Он приходит компоненту `Empty` СВОЙСТВОМ `description`, а не потомком, поэтому
 * поиском по тексту не находится: в наборе проб antd подменён, и свойство
 * оседает атрибутом. Читаем именно его — иначе утверждение проверяло бы форму
 * подмены, а не то, что видит пользователь.
 */
const emptyText = (root: HTMLElement): string | null =>
  root.querySelector("[description]")?.getAttribute("description") ?? null;

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

    expect(emptyText(container)).toBe("Выберите Account вверху секции, чтобы увидеть Projects.");
  });

  it("объяснение называет ИМЕННО тот ресурс, о котором речь", () => {
    const { container } = render(
      <IamScopedListShell spec={{ id: "service-accounts", plural: "Service Accounts" } as ResourceSpec} />,
    );

    expect(emptyText(container)).toBe("Выберите Account вверху секции, чтобы увидеть Service Accounts.");
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
