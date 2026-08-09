// Выбор чужого ресурса по ссылке. Здесь ошибаются молча: пустой список
// неотличим от «нет прав / не выбран проект / не выбрано поле выше», а
// значение, которого нет среди кандидатов, выглядит выбранным, хотя ссылается
// в никуда. Поэтому каждое из трёх состояний обязано СКАЗАТЬ, чего оно ждёт.

import { jest } from "@jest/globals";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@shared/api/client";

const list = jest.fn<(path: string, q: Record<string, string>) => Promise<Record<string, unknown>>>();
let project: { id: string; accountId?: string } | null = { id: "prj-1" };

jest.unstable_mockModule("@shared/api/client", () => ({
  api: { list, get: jest.fn(), create: jest.fn(), update: jest.fn(), delete: jest.fn(), action: jest.fn() },
  ApiError,
}));

jest.unstable_mockModule("@shared/lib/context-store", () => ({
  useProjectStore: (sel: (s: { project: typeof project }) => unknown) => sel({ project }),
  useContext: (sel: (s: { account: null }) => unknown) => sel({ account: null }),
}));

const { RefSelect } = await import("./RefSelect");

function show(over: Partial<Parameters<typeof RefSelect>[0]> = {}) {
  const onChange = jest.fn<(uid: string) => void>();
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <RefSelect refResource="networks" refProjectScoped onChange={onChange} {...over} />
    </QueryClientProvider>,
  );
  return { onChange };
}

const picker = () => screen.getByRole("combobox");
const optionLabels = () => [...picker().querySelectorAll("option")].map((o) => o.textContent ?? "");

beforeEach(() => {
  jest.clearAllMocks();
  project = { id: "prj-1" };
  list.mockResolvedValue({ networks: [{ id: "net-1", name: "frontend" }, { id: "net-2", name: "backend" }] });
});

describe("RefSelect", () => {
  it("неизвестный тип ссылки назван прямо, а не показан пустым списком", () => {
    show({ refResource: "no-such-resource" });

    expect(screen.getByText(/Unknown ref: no-such-resource/)).toBeInTheDocument();
    expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
  });

  it("кандидаты загружаются в области выбранного проекта", async () => {
    show();

    await waitFor(() => expect(list).toHaveBeenCalledWith("/vpc/v1/networks", { project_id: "prj-1" }));
    await waitFor(() => expect(optionLabels()).toContain("frontend"));
    expect(optionLabels()).toContain("backend");
  });

  it("без выбранного проекта список не запрашивается и сказано, чего ждать", () => {
    project = null;
    show();

    expect(list).not.toHaveBeenCalled();
    expect(screen.getByText("Выберите проект в шапке для загрузки.")).toBeInTheDocument();
    expect(picker()).toBeDisabled();
  });

  it("пока поле-источник выше не заполнено, список не запрашивается и это названо", () => {
    show({ refQueryFromField: { param: "subnet_id", field: "subnet_id" }, formValue: {} });

    expect(list).not.toHaveBeenCalled();
    expect(screen.getByText('Сначала выберите «subnet_id» выше.')).toBeInTheDocument();
  });

  it("заполненное поле-источник уезжает в запрос параметром", async () => {
    show({ refQueryFromField: { param: "subnet_id", field: "subnet_id" }, formValue: { subnet_id: "sub-7" } });

    await waitFor(() =>
      expect(list).toHaveBeenCalledWith("/vpc/v1/networks", { project_id: "prj-1", subnet_id: "sub-7" }),
    );
  });

  it("клиентский фильтр убирает из выбора то, что полю не подходит", async () => {
    show({ refFilter: (r) => (r.name as string) === "backend" });

    await waitFor(() => expect(optionLabels()).toContain("backend"));
    expect(optionLabels()).not.toContain("frontend");
  });

  it("выбор кандидата отдаёт вызывающему его идентификатор", async () => {
    const { onChange } = show();

    await waitFor(() => expect(optionLabels()).toContain("frontend"));
    fireEvent.change(picker(), { target: { value: "net-2" } });

    expect(onChange).toHaveBeenCalledWith("net-2");
  });

  it("значение вне списка кандидатов не выдаётся за выбранное — сказано, что его нет", async () => {
    show({ value: "net-999" });

    expect(await screen.findByText(/ID не найден в списке/)).toBeInTheDocument();
  });

  it("значение из списка предупреждения не вызывает", async () => {
    show({ value: "net-1" });

    await waitFor(() => expect(optionLabels()).toContain("frontend"));
    expect(screen.queryByText(/ID не найден в списке/)).not.toBeInTheDocument();
  });

  it("отказ края показан, а не превращён в пустой список", async () => {
    list.mockRejectedValue(new ApiError(403, "PERMISSION_DENIED", null, "no access"));
    show();

    expect(await screen.findByText(/no access/)).toBeInTheDocument();
  });

  it("без права создавать пункта «Создать» в списке нет", async () => {
    show();

    await waitFor(() => expect(optionLabels()).toContain("frontend"));
    expect(optionLabels().some((l) => l.startsWith("+ Создать"))).toBe(false);
  });

  it("выбор пункта «Создать» открывает окно, но значения поля не меняет", async () => {
    const { onChange } = show({ createResource: "networks" });

    await waitFor(() => expect(optionLabels().some((l) => l.startsWith("+ Создать"))).toBe(true));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();

    fireEvent.change(picker(), { target: { value: "__create__" } });

    expect(await screen.findByRole("dialog")).toBeInTheDocument();
    expect(onChange).not.toHaveBeenCalled();
  });

  it("подсказка выбора называет тип ресурса, если своей не задано", async () => {
    show();

    expect(optionLabels()[0]).toMatch(/^Выбрать /);
  });
});
