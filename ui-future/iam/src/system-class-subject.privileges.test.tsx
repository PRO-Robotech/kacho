// Системный класс субъекта во вкладке «Привилегии» области и в фильтре перечня
// выдач (kacho#3238; приёмка kaname#829, SC-53). Вкладка и страница
// ИСПОЛНЯЮТСЯ, утверждается видимое человеку: класс назван подписью, а не
// идентификатором с копированием, и у класса есть свой вариант отбора.
//
// Каждое утверждение о классе стоит в паре с близнецом-человеком: то же место
// у человека показывает идентификатор — значит, его отсутствие у класса
// говорит о ветке класса, а не о том, что ячейка не рисует идентификаторы вообще.

import { jest } from "@jest/globals";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import type { DetailTab } from "@shared/components/organisms/DetailShell";
import { antdStub } from "@shared/test/antd-stub";

jest.unstable_mockModule("antd", () => antdStub());
// Поток изменений в наборе проб не открывается: предмет пробы — отбор, а не
// провязка потока; без подмены клиент потока полез бы в сеть jsdom.
jest.unstable_mockModule("@shared/lib/subscription/use-resource-stream", () => ({
  useResourceStream: () => ({ streamed: false }),
}));

function show(node: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter>{node}</MemoryRouter>
    </QueryClientProvider>,
  );
}

const CLASS_LABEL = "Аноним (без входа)";

describe("SC-53: вкладка «Привилегии» области", () => {
  let tab: DetailTab;
  let byResource: jest.Spied<(...a: never[]) => Promise<unknown>>;

  beforeAll(async () => {
    await import("@/registerExtensions");
    const { detailExtension } = await import("@shared/components/organisms/ResourceDetailExtensions");
    const tabs =
      detailExtension("accounts")?.extraTabs?.({ data: { id: "acc-1" }, detailBase: "/iam/accounts/acc-1" } as never) ??
      [];
    const found = tabs.find((t) => t.id === "privileges");
    if (!found) throw new Error("вкладки «Привилегии» у карточки аккаунта нет — предмет пробы отсутствует");
    tab = found;
  });

  beforeEach(async () => {
    const { iamApi } = await import("@shared/api/iam");
    byResource = jest.spyOn(iamApi, "listAccessBindingsByResource") as unknown as typeof byResource;
    jest.spyOn(iamApi, "listRoles").mockResolvedValue({ roles: [] } as never);
    jest.spyOn(iamApi, "listUsers").mockResolvedValue({ users: [] } as never);
  });

  afterEach(() => jest.restoreAllMocks());

  it("класс назван подписью, его идентификатор не показан к копированию", async () => {
    byResource.mockResolvedValue({
      access_bindings: [{ id: "acb-1", subject_type: "system_class", subject_id: "anonymous", role_id: "rol-1" }],
    });
    const { container } = show(tab.render());
    // Положительный контроль: строка выдачи отрисована (её роль видна).
    await waitFor(() => expect(container).toHaveTextContent("rol-1"));
    expect(container.querySelector('[data-testid="subject-system-class"]')?.textContent).toBe(CLASS_LABEL);
    // Голое имя класса — это идентификатор записи, которой нет: показанный к
    // копированию, он выдавал бы класс за запись.
    expect(container.textContent).not.toMatch(/(^|[^a-z_])anonymous([^a-z_]|$)/);
  });

  it("близнец: у человека та же ячейка показывает идентификатор субъекта", async () => {
    byResource.mockResolvedValue({
      access_bindings: [{ id: "acb-2", subject_type: "user", subject_id: "usr-7", role_id: "rol-1" }],
    });
    const { container } = show(tab.render());
    await waitFor(() => expect(container).toHaveTextContent("rol-1"));
    expect(container).toHaveTextContent("usr-7");
    expect(container.querySelector('[data-testid="subject-system-class"]')).toBeNull();
  });

  it("у класса свой цвет вида, отличный от прочих и от цвета по умолчанию", async () => {
    const { subjectColor } = await import("@/registerExtensions");
    const cls = subjectColor("system_class");
    expect(cls).not.toBe("default");
    expect(["user", "service_account", "group"].map(subjectColor)).not.toContain(cls);
    // Провод и проекция пишут вид по-разному; цвет — один на вид.
    expect(subjectColor("SUBJECT_TYPE_SYSTEM_CLASS")).toBe(cls);
  });
});

describe("перечень выдач: отбор по виду субъекта", () => {
  let byAccount: jest.Spied<(...a: never[]) => Promise<unknown>>;

  beforeEach(async () => {
    const { iamApi } = await import("@shared/api/iam");
    byAccount = jest
      .spyOn(iamApi, "listAccessBindingsByAccount")
      .mockResolvedValue({ access_bindings: [] } as never) as unknown as typeof byAccount;
    const { contextApi } = await import("@shared/lib/context-store");
    contextApi.setAccount({ id: "acc-1", name: "acc" });
  });

  afterEach(() => jest.restoreAllMocks());

  async function page() {
    const { AccessBindingsPage } = await import("@/pages/iam/AccessBindingsPage");
    const { PageHeaderSlotProvider } = await import("@shared/components/molecules/PageHeaderSlot");
    return show(
      <PageHeaderSlotProvider>
        <AccessBindingsPage />
      </PageHeaderSlotProvider>,
    );
  }

  it("вариант «Системный класс» есть и спрашивает край отбором system_class", async () => {
    const { getByTestId } = await page();
    const filter = getByTestId("access-bindings-subject-filter") as HTMLSelectElement;
    const option = Array.from(filter.options).find((o) => o.value === "system_class");
    expect(option?.textContent).toBe("Системный класс");
    fireEvent.change(filter, { target: { value: "system_class" } });
    await waitFor(() =>
      expect(byAccount).toHaveBeenCalledWith("acc-1", expect.objectContaining({ subject_type_filter: "system_class" })),
    );
  });

  it("близнец: вариант «Пользователь» спрашивает край отбором user", async () => {
    const { getByTestId } = await page();
    fireEvent.change(getByTestId("access-bindings-subject-filter"), { target: { value: "user" } });
    await waitFor(() =>
      expect(byAccount).toHaveBeenCalledWith("acc-1", expect.objectContaining({ subject_type_filter: "user" })),
    );
  });
});
