// Набор ссылок на чужие ресурсы (адреса NIC, группы безопасности). Значение
// поля — идентификаторы, а пользователь работает с ИМЕНАМИ: неразрешённое имя
// оставляет его наедине с `adr-…`, а уже выбранный элемент, оставшийся в списке
// выбора, приводит к молчаливому дубликату. Потолок (`maxItems`) обязан
// закрывать сам селектор, а не отказывать после выбора.

import { jest } from "@jest/globals";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@shared/api/client";

const list = jest.fn<(path: string, q: Record<string, string>) => Promise<Record<string, unknown>>>();

jest.unstable_mockModule("@shared/api/client", () => ({
  api: { list, get: jest.fn(), create: jest.fn(), update: jest.fn(), delete: jest.fn(), action: jest.fn() },
  ApiError,
}));

const { ResourceRefChips } = await import("./ResourceRefChips");

const ADDRESSES = [
  { id: "adr-1", name: "web-vip", internal_ipv4_address: { address: "10.0.1.5" } },
  { id: "adr-2", name: "db-vip", internal_ipv4_address: { address: "10.0.1.6" } },
];

function show(props: Partial<Parameters<typeof ResourceRefChips>[0]> = {}) {
  const onChange = jest.fn<(next: string[]) => void>();
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <ResourceRefChips
        title="IPv4-адреса"
        refResource="addresses"
        projectId="prj-1"
        value={[]}
        onChange={onChange}
        {...props}
      />
    </QueryClientProvider>,
  );
  return { onChange };
}

const picker = () => screen.getByRole("combobox");
const optionLabels = () => [...picker().querySelectorAll("option")].map((o) => o.textContent ?? "");
const chips = () => screen.queryAllByRole("button", { name: "close" });

beforeEach(() => {
  jest.clearAllMocks();
  list.mockResolvedValue({ addresses: ADDRESSES });
});

describe("ResourceRefChips", () => {
  it("пустой набор называет пустым, а не показывает пустоту", () => {
    show();

    expect(screen.getByText("— пусто —")).toBeInTheDocument();
  });

  it("показывает выбранные ссылки ИМЕНАМИ, а не идентификаторами", async () => {
    show({ value: ["adr-1"] });

    expect(await screen.findByText("web-vip: 10.0.1.5")).toBeInTheDocument();
    expect(screen.queryByText("adr-1")).not.toBeInTheDocument();
  });

  it("пока список ресурсов не пришёл, показывает идентификатор — не пустое место", () => {
    list.mockReturnValue(new Promise(() => {}));
    show({ value: ["adr-9"] });

    expect(screen.getByText("adr-9")).toBeInTheDocument();
  });

  it("считает выбранное и называет потолок", async () => {
    show({ value: ["adr-1"], maxItems: 2 });

    expect(await screen.findByText("1 / 2")).toBeInTheDocument();
  });

  it("уже выбранное из списка выбора убрано — дубликат недостижим", async () => {
    show({ value: ["adr-1"] });

    await waitFor(() => expect(optionLabels()).toContain("db-vip: 10.0.1.6"));
    expect(optionLabels()).not.toContain("web-vip: 10.0.1.5");
  });

  it("выбор добавляет ссылку сразу, без отдельного «Добавить»", async () => {
    const { onChange } = show({ value: ["adr-1"] });

    await waitFor(() => expect(optionLabels()).toContain("db-vip: 10.0.1.6"));
    fireEvent.change(picker(), { target: { value: "adr-2" } });

    expect(onChange).toHaveBeenCalledWith(["adr-1", "adr-2"]);
  });

  it("крестик на чипе снимает ровно его", async () => {
    const { onChange } = show({ value: ["adr-1", "adr-2"] });

    await screen.findByText("web-vip: 10.0.1.5");
    expect(chips()).toHaveLength(2);
    fireEvent.click(chips()[0]);

    expect(onChange).toHaveBeenCalledWith(["adr-2"]);
  });

  it("на потолке селектор закрыт и говорит почему", async () => {
    show({ value: ["adr-1"], maxItems: 1 });

    await waitFor(() => expect(picker()).toBeDisabled());
    expect(within(picker()).getByText("Максимум 1")).toBeInTheDocument();
  });

  it("выключенный селектор объясняет, чего ждёт", () => {
    show({ disabled: true, disabledHint: "Сначала выберите подсеть" });

    expect(picker()).toBeDisabled();
    expect(within(picker()).getByText("Сначала выберите подсеть")).toBeInTheDocument();
  });

  it("без права создавать пункта «Создать» в списке нет", async () => {
    show();

    await waitFor(() => expect(optionLabels()).toContain("web-vip: 10.0.1.5"));
    expect(optionLabels().some((l) => l.startsWith("+ Создать"))).toBe(false);
  });

  it("окно создания открывается выбором пункта «Создать», а не само", async () => {
    show({ createResource: "addresses", createTitle: "Резервирование IP-адреса" });

    await waitFor(() => expect(optionLabels().some((l) => l.startsWith("+ Создать"))).toBe(true));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();

    fireEvent.change(picker(), { target: { value: "__create__" } });

    expect(await screen.findByRole("dialog")).toBeInTheDocument();
  });

  it("клиентский фильтр убирает из выбора то, что полю не подходит", async () => {
    show({ refFilter: (r) => (r.name as string) === "db-vip" });

    await waitFor(() => expect(optionLabels()).toContain("db-vip: 10.0.1.6"));
    expect(optionLabels()).not.toContain("web-vip: 10.0.1.5");
  });
});
