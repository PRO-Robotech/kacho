// Создание сетевого интерфейса. Подсеть у интерфейса — якорь размещения и
// неизменяема после создания, поэтому форма обязана:
//
//  1. не отправлять запрос без подсети и сказать об этом сама — иначе человек
//     получает отказ края на то, что видно на месте;
//  2. держать выбор адресов и групп безопасности ЗАПЕРТЫМ, пока подсеть не
//     выбрана: адрес чужой подсети край отвергнет, а на вид он ничем не хуже;
//  3. отправлять подсеть тем полем, которое несёт контракт, и не досылать
//     ничего сверх набора.
//
// Проверяется наблюдаемое: показанный текст отказа, состояние полей и ТЕЛО
// ушедшего запроса.

import { jest } from "@jest/globals";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@shared/api/client";

const list = jest.fn<(path: string, q?: unknown) => Promise<Record<string, unknown>>>();
const create = jest.fn<(path: string, body: unknown) => Promise<unknown>>();
const toastError = jest.fn();

jest.unstable_mockModule("@shared/api/client", () => ({
  api: { list, create, get: jest.fn(), update: jest.fn(), delete: jest.fn(), action: jest.fn() },
  ApiError,
}));

jest.unstable_mockModule("@shared/lib/toast", () => ({
  toast: { error: toastError, success: jest.fn(), info: jest.fn(), loading: jest.fn(), dismiss: jest.fn() },
}));

jest.unstable_mockModule("@shared/lib/use-operation", () => ({
  useInvalidateResourceList: () => jest.fn(),
  useOperation: () => ({ data: undefined }),
}));

const { InlineNetworkInterfaceCreateForm } = await import("./InlineNetworkInterfaceCreateForm");

function show(props: Record<string, unknown> = {}) {
  const onCancel = jest.fn();
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <InlineNetworkInterfaceCreateForm projectId="prj-1" onCancel={onCancel} {...props} />
    </QueryClientProvider>,
  );
  return { onCancel };
}

const selectShowing = (optionText: string) =>
  [...document.querySelectorAll("select")].find((s) => [...s.options].some((o) => o.textContent === optionText));

// Кнопка отправки называет ДЕЙСТВИЕ и только его: предмет уже назван заголовком
// формы над ней (канон консоли, правило 3). Имя ищется ТОЧНЫМ совпадением —
// образец `/Создать/` совпал бы и с прежней подписью «Создать сетевой интерфейс», то
// есть проба пережила бы возврат предмета в кнопку и промолчала.
const save = () => fireEvent.click(screen.getByRole("button", { name: "Создать" }));
const body = () => create.mock.calls[0][1] as Record<string, unknown>;

beforeEach(() => {
  jest.clearAllMocks();
  // Порт возвращает промис — заменитель возвращает его же, но ждать ему нечего:
  // ответ известен сразу. `async` без `await` обещало ожидание, которого нет.
  list.mockImplementation((path: string) => {
    if (path.includes("/subnets")) return Promise.resolve({ subnets: [{ id: "sub-1", name: "внутренняя" }] });
    if (path.includes("/addresses"))
      return Promise.resolve({
        addresses: [
          { id: "adr-free", name: "запасной", internal_ipv4_address: { subnet_id: "sub-1", address: "10.0.0.5" } },
          // Занятость адрес сообщает САМ — полем `used` (address.proto, тег 16).
          { id: "adr-busy", name: "уже-в-деле", used: true, internal_ipv4_address: { subnet_id: "sub-1", address: "10.0.0.6" } },
        ],
      });
    return Promise.resolve({});
  });
  create.mockResolvedValue({});
});

describe("InlineNetworkInterfaceCreateForm", () => {
  it("без подсети запрос не уходит, и человеку сказано почему", async () => {
    show();

    await waitFor(() => expect(selectShowing("внутренняя")).toBeDefined());
    save();

    // Отказ стоит В СТРОКЕ поля, а не всплывашкой в углу: сообщение обязано
    // лежать внутри той же обёртки, что и подпись «Подсеть», иначе «рядом с
    // полем» осталось бы утверждением о вкусе, а не о разметке.
    const alert = screen.queryAllByRole("alert").find((el) => (el.parentElement?.textContent ?? "").includes("Подсеть"));
    expect(alert).toHaveTextContent("«Подсеть»: поле обязательное — интерфейс создаётся внутри подсети.");
    expect(toastError).not.toHaveBeenCalled();
    expect(create).not.toHaveBeenCalled();
  });

  it("выбранная подсеть уходит своим полем", async () => {
    show();

    await waitFor(() => expect(selectShowing("внутренняя")).toBeDefined());
    fireEvent.change(selectShowing("внутренняя")!, { target: { value: "sub-1" } });
    save();

    await waitFor(() => expect(create).toHaveBeenCalledWith("/vpc/v1/networkInterfaces", expect.anything()));
    expect(body().subnet_id).toBe("sub-1");
    expect(body().project_id).toBe("prj-1");
  });

  it("занятый адрес в списке не предлагается, свободный — предлагается", async () => {
    // Прежде список предлагал ВСЕ адреса подсети, включая привязанные к другому
    // интерфейсу: выбор проходил форму целиком и умирал на крае («address adr…
    // is already in use»). Консоль звала сделать то, что край отвергает by
    // construction, и узнать об этом можно было только отправкой.
    //
    // Пара обязательна: одно «занятого нет» выполнимо списком, который не
    // предлагает НИЧЕГО, — то есть полем, сломанным целиком.
    show();

    await waitFor(() => expect(selectShowing("внутренняя")).toBeDefined());
    fireEvent.change(selectShowing("внутренняя")!, { target: { value: "sub-1" } });

    await waitFor(() => expect(screen.getAllByText("запасной · 10.0.0.5").length).toBeGreaterThan(0));
    expect(screen.queryByText("уже-в-деле · 10.0.0.6")).not.toBeInTheDocument();
  });

  it("заданная подсеть заперта — интерфейс не переносят между подсетями", () => {
    show({ subnetId: "sub-1" });

    expect(selectShowing("Выберите подсеть")!.disabled).toBe(true);
  });

  it("незаданная подсеть выбирается — контроль в обратную сторону", async () => {
    show();

    await waitFor(() => expect(selectShowing("внутренняя")).toBeDefined());
    expect(selectShowing("внутренняя")!.disabled).toBe(false);
  });

  it("пока подсеть не выбрана, адреса и группы выбрать нельзя и сказано почему", async () => {
    show();

    await waitFor(() => expect(selectShowing("внутренняя")).toBeDefined());
    // Подсказка объясняет запрет; без неё поле выглядит просто сломанным.
    expect(screen.getAllByText("Сначала выберите подсеть").length).toBeGreaterThanOrEqual(3);
  });

  it("после выбора подсети запрет снимается", async () => {
    // Положительный близнец: без него предыдущее утверждение зеленело бы на
    // форме, где эти поля заперты всегда.
    show();

    await waitFor(() => expect(selectShowing("внутренняя")).toBeDefined());
    fireEvent.change(selectShowing("внутренняя")!, { target: { value: "sub-1" } });

    await waitFor(() => expect(screen.queryByText("Сначала выберите подсеть")).not.toBeInTheDocument());
  });
});
