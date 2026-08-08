import { jest } from "@jest/globals";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { contextApi } from "@shared/lib/context-store";
import type { ContextUrlSync as ContextUrlSyncExport } from "./ContextUrlSync";

const get = jest.fn<(path: string) => Promise<unknown>>();

jest.unstable_mockModule("@shared/api/client", () => ({
  api: { get },
}));

let ContextUrlSync: typeof ContextUrlSyncExport;

let client: QueryClient;

function renderAt(path: string) {
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[path]}>
        <ContextUrlSync />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

/**
 * Дождаться, что ответ про проект РЕАЛЬНО применён компонентом.
 *
 * Признак — данные в кэше запроса: только после этого компонент перерисуется и
 * его эффект получит ответ. Без такого якоря отрицание («имя не изменилось»)
 * сработало бы раньше применения и осталось бы зелёным при снятой сверке
 * идентификаторов — то есть проверяло бы расписание, а не код.
 */
async function afterProjectResponseApplied(projectId: string) {
  await waitFor(() => expect(client.getQueryData(["hydrate-project", projectId])).toBeDefined());
  await act(async () => {
    await Promise.resolve();
  });
}

describe("ContextUrlSync", () => {
  beforeAll(async () => {
    ({ ContextUrlSync } = await import("./ContextUrlSync"));
  });

  beforeEach(() => {
    jest.clearAllMocks();
    get.mockResolvedValue({});
    window.localStorage.clear();
    contextApi.setAccount(null);
  });

  it("сам ничего не рисует — его предмет только контекст", () => {
    const { container } = renderAt("/accounts");

    expect(container).toBeEmptyDOMElement();
  });

  it("берёт account из адреса списка проектов", () => {
    renderAt("/accounts/acc-1/projects");

    expect(contextApi.get().account?.id).toBe("acc-1");
  });

  it("берёт project из адреса и наследует ему текущий account", () => {
    contextApi.setAccount({ id: "acc-1", name: "Первый" });

    renderAt("/projects/prj-7/vpc/networks");

    expect(contextApi.get().project?.id).toBe("prj-7");
    expect(contextApi.get().project?.accountId).toBe("acc-1");
    expect(contextApi.get().account?.name).toBe("Первый");
  });

  it("на корне и списке аккаунтов контекст сбрасывается целиком", () => {
    contextApi.setProject({ id: "prj-7", name: "Седьмой", accountId: "acc-1" });

    renderAt("/accounts");

    expect(contextApi.get().account).toBeNull();
    expect(contextApi.get().project).toBeNull();
  });

  it("догружает имя проекта по прямой ссылке и подтягивает его аккаунт", async () => {
    // Прямая ссылка в инкогнито: id в адресе есть, имён в хранилище нет.
    contextApi.setProject({ id: "prj-7", name: "", accountId: "" });
    get.mockResolvedValue({ id: "prj-7", name: "Седьмой", accountId: "acc-9" });

    renderAt("/projects/prj-7");

    await waitFor(() => expect(contextApi.get().project?.name).toBe("Седьмой"));
    expect(get).toHaveBeenCalledWith("/iam/v1/projects/prj-7");
    expect(contextApi.get().account?.id).toBe("acc-9");
  });

  it("не ходит за именем, которое уже известно", async () => {
    // Имена ОБОИХ уровней должны быть известны: у проекта своё, у аккаунта своё —
    // иначе догрузка запустится за родителем, и утверждение упало бы на подготовке,
    // а не на предмете.
    contextApi.setAccount({ id: "acc-9", name: "Девятый" });
    contextApi.setProject({ id: "prj-7", name: "Седьмой", accountId: "acc-9" });

    renderAt("/projects/prj-7");

    await waitFor(() => expect(contextApi.get().project?.id).toBe("prj-7"));
    expect(get).not.toHaveBeenCalled();
  });

  it("догружает имя аккаунта, когда известен только его идентификатор", async () => {
    contextApi.setAccount({ id: "acc-9", name: "" });
    get.mockResolvedValue({ id: "acc-9", name: "Девятый" });

    renderAt("/accounts/acc-9/projects");

    await waitFor(() => expect(contextApi.get().account?.name).toBe("Девятый"));
    expect(get).toHaveBeenCalledWith("/iam/v1/accounts/acc-9");
  });

  it("чужой ответ не переписывает контекст (гонка между сменами адреса)", async () => {
    contextApi.setProject({ id: "prj-7", name: "", accountId: "" });

    // Ответ пришёл про ДРУГОЙ проект — применять его к текущему нельзя.
    get.mockResolvedValue({ id: "prj-OTHER", name: "Чужой", accountId: "acc-x" });

    renderAt("/projects/prj-7");

    await afterProjectResponseApplied("prj-7");

    expect(contextApi.get().project?.name).toBe("");
    expect(contextApi.get().account?.id).not.toBe("acc-x");
  });

  it("свой ответ той же дорогой применяется (положительный контроль к предыдущему)", async () => {
    // Без этой пары отрицание выше зеленело бы и на компоненте, который вообще
    // ничего не применяет.
    contextApi.setProject({ id: "prj-7", name: "", accountId: "" });
    get.mockResolvedValue({ id: "prj-7", name: "Седьмой", accountId: "acc-9" });

    renderAt("/projects/prj-7");

    await afterProjectResponseApplied("prj-7");

    expect(contextApi.get().project?.name).toBe("Седьмой");
    expect(contextApi.get().account?.id).toBe("acc-9");
  });
});
