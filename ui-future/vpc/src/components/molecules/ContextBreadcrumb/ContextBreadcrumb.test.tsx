import { jest } from "@jest/globals";
import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { contextApi } from "@shared/lib/context-store";
import type { ContextBreadcrumb as ContextBreadcrumbExport } from "./ContextBreadcrumb";

const listAccounts = jest.fn<(q?: Record<string, string>) => Promise<{ accounts?: { id: string; name: string }[] }>>();
const listProjects = jest.fn<(q?: Record<string, string>) => Promise<{ projects?: { id: string; name: string }[] }>>();

jest.unstable_mockModule("@shared/api/iam", () => ({
  iamApi: { listAccounts, listProjects },
}));

let ContextBreadcrumb: typeof ContextBreadcrumbExport;

const renderCrumb = async () => {
  const { PageHeaderSlotProvider } = await import("@shared/components/molecules/PageHeaderSlot");
  return render(
    <MemoryRouter>
      <PageHeaderSlotProvider>
        <ContextBreadcrumb />
      </PageHeaderSlotProvider>
    </MemoryRouter>,
  );
};

describe("ContextBreadcrumb", () => {
  beforeAll(async () => {
    ({ ContextBreadcrumb } = await import("./ContextBreadcrumb"));
  });

  beforeEach(() => {
    jest.clearAllMocks();
    listAccounts.mockResolvedValue({ accounts: [] });
    listProjects.mockResolvedValue({ projects: [] });
    window.localStorage.clear();
    contextApi.setAccount(null);
  });

  it("на пустом контексте зовёт выбрать аккаунт, а не показывает пустое место", async () => {
    await renderCrumb();

    expect(screen.getByText("Выберите аккаунт")).toBeInTheDocument();
    expect(screen.getByText("Проект")).toBeInTheDocument();
  });

  it("отдаёт третий сегмент странице — что страница туда положит, то и покажет", async () => {
    const { PageHeaderSlotProvider, useBreadcrumb } = await import("@shared/components/molecules/PageHeaderSlot");
    // Узел вынесен из тела компонента НАМЕРЕННО: `useBreadcrumb` держит его в
    // зависимостях эффекта, поэтому новый элемент на каждый рендер даёт вечную
    // петлю «эффект → состояние → рендер → новый элемент». Настоящие страницы
    // передают сюда стабильный узел.
    const crumb = <span>frontend-subnet</span>;
    const Page = () => {
      useBreadcrumb(crumb);
      return null;
    };

    render(
      <MemoryRouter>
        <PageHeaderSlotProvider>
          <ContextBreadcrumb />
          <Page />
        </PageHeaderSlotProvider>
      </MemoryRouter>,
    );

    expect(await screen.findByText("frontend-subnet")).toBeInTheDocument();
  });

  it("выбранный аккаунт подписывает первый сегмент и открывает второй", async () => {
    contextApi.setAccount({ id: "acc-1", name: "Первый" });

    await renderCrumb();

    expect(screen.getByText("Первый")).toBeInTheDocument();
    expect(screen.queryByText("Выберите аккаунт")).not.toBeInTheDocument();
    // Второй сегмент по-прежнему ждёт выбора — аккаунт проект не подставляет.
    expect(screen.getByText("Проект")).toBeInTheDocument();
  });

  it("аккаунт без имени подписывается своим идентификатором", async () => {
    contextApi.setAccount({ id: "acc-1", name: "" });

    await renderCrumb();

    expect(screen.getByText("acc-1")).toBeInTheDocument();
  });

  it("выбранный проект подписывает второй сегмент", async () => {
    contextApi.setProject({ id: "prj-7", name: "Седьмой", accountId: "acc-1" });

    await renderCrumb();

    expect(screen.getByText("Седьмой")).toBeInTheDocument();
    expect(screen.queryByText("Проект")).not.toBeInTheDocument();
  });

  it("список аккаунтов запрашивается один раз при монтировании; проекты — нет", async () => {
    contextApi.setAccount({ id: "acc-1", name: "Первый" });

    await renderCrumb();

    await waitFor(() => expect(listAccounts).toHaveBeenCalledTimes(1));
    // Проекты грузятся ЛЕНИВО — только когда выпадающий список открыт. Иначе
    // каждая страница платила бы за перечень, который никто не смотрит.
    expect(listProjects).not.toHaveBeenCalled();
  });

  it("отказ перечня аккаунтов не роняет шапку", async () => {
    listAccounts.mockRejectedValue(new Error("503"));
    contextApi.setAccount({ id: "acc-1", name: "Первый" });

    await renderCrumb();

    await waitFor(() => expect(listAccounts).toHaveBeenCalled());
    expect(screen.getByText("Первый")).toBeInTheDocument();
  });
});
