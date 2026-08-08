// Вкладка «Привилегии» субъекта — что она СПРАШИВАЕТ и что показывает.
//
// Предмет: привилегии субъекта резолвятся через listSubjectPrivileges (её видит
// и сам субъект, и администратор аккаунта). Прежний путь — listAccessBindings-
// BySubject — доступен ТОЛЬКО самому субъекту, поэтому у администратора он
// отвечал отказом, а вкладка показывала «Привилегий нет.»: отказ выглядел
// пустотой. Отсюда два утверждения — куда идём и что показываем при отказе.

import { jest } from "@jest/globals";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import type { DetailTab } from "@shared/components/organisms/DetailShell";

interface Privilege {
  binding_id: string;
  role_id?: string;
  role_name?: string;
  resource_type?: string;
  resource_id?: string;
  scope?: string;
  created_at?: string;
}

jest.unstable_mockModule("antd", async () => (await import("@/test/antd-double")).antdDouble);

// Клиент iam подменяется ТОЧЕЧНО — слежкой за методами настоящего объекта, а не
// подменой всего модуля: перечень его экспортов пришлось бы выписывать руками, и
// он разошёлся бы с деревом молча (первая редакция этой пробы так и упала —
// не хватало одного имени).
let listSubjectPrivileges: jest.Spied<(...a: never[]) => Promise<{ privileges?: Privilege[] }>>;
let listAccessBindingsBySubject: jest.Spied<(...a: never[]) => Promise<unknown>>;

let privilegesTab: DetailTab;

/**
 * Панель отказа. Её рисует общий `ErrorResult` через antd `Result`; в наборе проб
 * antd подменён, поэтому признак «это ошибка» читается атрибутом `status`, а не
 * ролью — роли у подмены нет by construction.
 */
const errorPane = (root: HTMLElement) => root.querySelector('[status="error"]');

function renderTab() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter>{privilegesTab.render()}</MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("вкладка «Привилегии» субъекта", () => {
  beforeAll(async () => {
    // Регистрация расширений — побочный эффект импорта модуля; берём вкладку из
    // общего реестра, то есть тем же путём, каким её получает карточка ресурса.
    await import("@/registerExtensions");
    const { detailExtension } = await import("@shared/components/organisms/ResourceDetailExtensions");
    const ext = detailExtension("users");
    const tabs = ext?.extraTabs?.({ data: { id: "usr-9" }, detailBase: "/iam/users/usr-9" } as never) ?? [];
    const tab = tabs.find((t) => t.id === "privileges");
    if (!tab) throw new Error("вкладки «Привилегии» у карточки пользователя нет — предмет пробы отсутствует");
    privilegesTab = tab;
  });

  beforeEach(async () => {
    const { iamApi } = await import("@shared/api/iam");
    listSubjectPrivileges = jest
      .spyOn(iamApi, "listSubjectPrivileges")
      .mockResolvedValue({ privileges: [] }) as unknown as typeof listSubjectPrivileges;
    listAccessBindingsBySubject = jest
      .spyOn(iamApi, "listAccessBindingsBySubject")
      .mockResolvedValue({ access_bindings: [] }) as unknown as typeof listAccessBindingsBySubject;
  });

  afterEach(() => {
    jest.restoreAllMocks();
  });

  it("карточка пользователя объявляет вкладку с человеческим названием", () => {
    expect(privilegesTab.label).toBe("Привилегии");
  });

  it("спрашивает привилегии видимым администратору способом, а не самообслуживанием", async () => {
    renderTab();

    await waitFor(() => expect(listSubjectPrivileges).toHaveBeenCalledWith("user", "usr-9", { page_size: "200" }));
    // Прежний путь виден только самому субъекту: администратор аккаунта получал
    // от него отказ, и вкладка показывала пустоту вместо ошибки.
    expect(listAccessBindingsBySubject).not.toHaveBeenCalled();
  });

  it("показывает имя роли, пришедшее с сервера, рядом с её идентификатором", async () => {
    listSubjectPrivileges.mockResolvedValue({
      privileges: [
        {
          binding_id: "acb-1",
          role_id: "rol-1",
          role_name: "vpc.network.admin",
          resource_type: "iam.project",
          resource_id: "prj-7",
          scope: "PROJECT",
        },
      ],
    });

    const { container } = renderTab();

    await waitFor(() => expect(container).toHaveTextContent("vpc.network.admin"));
    expect(container).toHaveTextContent("rol-1");
    expect(container).toHaveTextContent("prj-7");
  });

  it("привилегия без имени роли всё равно показывает её идентификатор", async () => {
    listSubjectPrivileges.mockResolvedValue({ privileges: [{ binding_id: "acb-2", role_id: "rol-2" }] });

    const { container } = renderTab();

    await waitFor(() => expect(container).toHaveTextContent("rol-2"));
  });

  it("отказ показывается ошибкой, а не «привилегий нет»", async () => {
    listSubjectPrivileges.mockRejectedValue(
      Object.assign(new Error("permission denied"), { code: "PERMISSION_DENIED", status: 403 }),
    );

    const { container } = renderTab();

    // Ложная пустота на отказе — самый тихий из возможных дефектов: оператор
    // делает вывод «прав нет», хотя ответа он не получал вовсе.
    await waitFor(() => expect(errorPane(container)).not.toBeNull());
    expect(container).not.toHaveTextContent("Привилегий нет.");
  });

  it("пустой ответ — это именно пусто, а не ошибка (положительный контроль)", async () => {
    const { container } = renderTab();

    await waitFor(() => expect(container).toHaveTextContent("Привилегий нет."));
    expect(errorPane(container)).toBeNull();
  });
});
