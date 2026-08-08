import { jest } from "@jest/globals";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { ApiError } from "@shared/api/client";
import type { DeleteButton as DeleteButtonExport } from "./DeleteButton";

const del = jest.fn<(path: string) => Promise<unknown>>();
const invalidate = jest.fn();
const extractOperationId = jest.fn<(resp: unknown) => string | null>();
const toastError = jest.fn();
let watchedOpId: string | null = null;
let notifyDone: ((success: boolean) => void) | null = null;

jest.unstable_mockModule("@shared/api/client", () => ({
  api: { delete: del },
  ApiError,
}));

jest.unstable_mockModule("@shared/lib/use-operation", () => ({
  useInvalidateResourceList: () => invalidate,
}));

jest.unstable_mockModule("@shared/components/molecules/OperationDialog", () => ({
  extractOperationId,
}));

// Наблюдатель операции подменён окном в его вход и обратный вызов: настоящий
// поллит сеть, а предмет пробы — ЧТО ему передали и что кнопка делает по «готово».
jest.unstable_mockModule("@shared/components/molecules/OperationToastWatcher", () => ({
  OperationToastWatcher: ({ opId, onDone }: { opId: string | null; onDone: (ok: boolean) => void }) => {
    watchedOpId = opId;
    notifyDone = onDone;
    return null;
  },
}));

jest.unstable_mockModule("@shared/lib/toast", () => ({
  toast: { error: toastError, success: jest.fn(), info: jest.fn(), loading: jest.fn(), dismiss: jest.fn() },
}));

let DeleteButton: typeof DeleteButtonExport;

const navigateTo = jest.fn();

function renderButton() {
  const client = new QueryClient({ defaultOptions: { mutations: { retry: false }, queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <DeleteButton
        apiPath="/vpc/v1/networks/net-1"
        resourceId="networks"
        name="frontend"
        resourceLabel="Network"
        projectId="prj-1"
        navigateTo={navigateTo}
      />
    </QueryClientProvider>,
  );
}

const openDialog = () => fireEvent.click(screen.getByRole("button", { name: /Delete/ }));
const confirm = () => fireEvent.click(screen.getByRole("button", { name: "Delete" }));

describe("DeleteButton", () => {
  beforeAll(async () => {
    ({ DeleteButton } = await import("./DeleteButton"));
  });

  beforeEach(() => {
    jest.clearAllMocks();
    watchedOpId = null;
    notifyDone = null;
    del.mockResolvedValue({});
    extractOperationId.mockReturnValue(null);
  });

  it("до подтверждения ничего не удаляет", () => {
    renderButton();

    expect(del).not.toHaveBeenCalled();
    expect(screen.queryByText("Действие необратимо.")).not.toBeInTheDocument();
  });

  it("по клику показывает, ЧТО и КАКИМ запросом будет удалено", () => {
    renderButton();

    openDialog();

    expect(screen.getByText("Удалить Network?")).toBeInTheDocument();
    expect(screen.getByText("frontend")).toBeInTheDocument();
    expect(screen.getByText("/vpc/v1/networks/net-1")).toBeInTheDocument();
  });

  it("подтверждение шлёт DELETE ровно по показанному пути", async () => {
    renderButton();
    openDialog();

    confirm();

    await waitFor(() => expect(del).toHaveBeenCalledWith("/vpc/v1/networks/net-1"));
    expect(del).toHaveBeenCalledTimes(1);
  });

  it("синхронный успех (без Operation) сразу инвалидирует список и уводит со страницы", async () => {
    renderButton();
    openDialog();

    confirm();

    await waitFor(() => expect(invalidate).toHaveBeenCalledWith("networks", "prj-1"));
    expect(navigateTo).toHaveBeenCalledTimes(1);
    expect(watchedOpId).toBeNull();
  });

  it("асинхронный успех отдаёт наблюдателю id операции и ждёт её, не уводя раньше времени", async () => {
    extractOperationId.mockReturnValue("opr-7");
    renderButton();
    openDialog();

    confirm();

    await waitFor(() => expect(watchedOpId).toBe("opr-7"));
    expect(invalidate).not.toHaveBeenCalled();
    expect(navigateTo).not.toHaveBeenCalled();

    notifyDone?.(true);

    await waitFor(() => expect(invalidate).toHaveBeenCalledWith("networks", "prj-1"));
    expect(navigateTo).toHaveBeenCalledTimes(1);
  });

  it("неудача операции обновляет список, но со страницы НЕ уводит", async () => {
    extractOperationId.mockReturnValue("opr-8");
    renderButton();
    openDialog();

    confirm();

    await waitFor(() => expect(watchedOpId).toBe("opr-8"));

    notifyDone?.(false);

    await waitFor(() => expect(invalidate).toHaveBeenCalledWith("networks", "prj-1"));
    expect(navigateTo).not.toHaveBeenCalled();
  });

  it("отказ сервера показывает код и текст прямо в окне и не закрывает его", async () => {
    del.mockRejectedValue(new ApiError(409, "FAILED_PRECONDITION", null, "network is not empty"));
    renderButton();
    openDialog();

    confirm();

    expect(await screen.findByText("FAILED_PRECONDITION: network is not empty")).toBeInTheDocument();
    expect(screen.getByText("Удалить Network?")).toBeInTheDocument();
    expect(toastError).toHaveBeenCalledWith("Delete Network frontend: FAILED_PRECONDITION: network is not empty");
    expect(navigateTo).not.toHaveBeenCalled();
  });
});
