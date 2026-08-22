// Создание пула адресов. Край требует непустым хотя бы одно семейство
// диапазонов, и клиентская проверка — то единственное, что отделяет опечатку от
// отказа в конце формы. Второй предмет — тело: пул зональный ИЛИ глобальный,
// и пустая зона обязана уехать как «зоны нет», а не как пустая строка.

import { jest } from "@jest/globals";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { formDividers } from "@shared/test/form-divider";
import { ApiError } from "@shared/api/client";

const create = jest.fn<(path: string, body: unknown) => Promise<unknown>>();
const list = jest.fn<(path: string, q: unknown) => Promise<unknown>>();
const toastError = jest.fn();
const toastSuccess = jest.fn();
const invalidate = jest.fn();

jest.unstable_mockModule("@shared/api/client", () => ({
  api: { create, list, get: jest.fn(), update: jest.fn(), delete: jest.fn(), action: jest.fn() },
  ApiError,
}));

jest.unstable_mockModule("@shared/lib/toast", () => ({
  toast: { error: toastError, success: toastSuccess, info: jest.fn(), loading: jest.fn(), dismiss: jest.fn() },
}));

jest.unstable_mockModule("@shared/lib/use-operation", () => ({
  useInvalidateResourceList: () => invalidate,
  useOperation: () => ({ data: undefined }),
}));

const { InlineAddressPoolCreateForm } = await import("./InlineAddressPoolCreateForm");

function show() {
  const onCancel = jest.fn();
  const onSuccess = jest.fn();
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <InlineAddressPoolCreateForm onCancel={onCancel} onSuccess={onSuccess} />
    </QueryClientProvider>,
  );
  return { onCancel, onSuccess };
}

// Кнопка отправки называет ДЕЙСТВИЕ и только его: предмет уже назван заголовком
// формы над ней (канон консоли, правило 3). Имя ищется ТОЧНЫМ совпадением —
// образец `/Создать/` совпал бы и с прежней подписью «Создать пул адресов», то
// есть проба пережила бы возврат предмета в кнопку и промолчала.
const submit = () => fireEvent.click(screen.getByRole("button", { name: "Создать" }));
const body = () => create.mock.calls[0][1] as Record<string, unknown>;

/** Поле по подписи: у формы горизонтальная раскладка «подпись → контрол». */
function underLabel(label: string | RegExp): HTMLElement {
  let el: HTMLElement | null = screen.getByText(label);
  while (el && !el.querySelector("input, textarea, select")) el = el.parentElement;
  if (!el) throw new Error(`контрол под подписью «${String(label)}» не найден`);
  return el;
}

function addCidr(family: 0 | 1, value: string) {
  const inputs = within(underLabel(/IPv4 и IPv6 CIDR/)).getAllByRole("textbox");
  fireEvent.change(inputs[family], { target: { value } });
  fireEvent.keyDown(inputs[family], { key: "Enter" });
}

beforeEach(() => {
  jest.clearAllMocks();
  create.mockResolvedValue({});
  // Каталог размещения отвечает одним полем идентичности (#716).
  list.mockResolvedValue({ zones: [{ id: "ru-central1-a" }] });
});

describe("InlineAddressPoolCreateForm", () => {
  it("пул без единого диапазона на край не уходит и объясняет отказ", () => {
    show();

    submit();

    // Отказ стоит В СТРОКЕ поля блоков, а не всплывашкой в углу.
    const alert = screen
      .queryAllByRole("alert")
      .find((el) => (el.parentElement?.textContent ?? "").includes("IPv4 и IPv6 CIDR"));
    expect(alert).toHaveTextContent("«IPv4 и IPv6 CIDR»: нужен хотя бы один блок — IPv4 либо IPv6.");
    expect(toastError).not.toHaveBeenCalled();
    expect(create).not.toHaveBeenCalled();
  });

  it("одного семейства достаточно — второе уезжает пустым списком", async () => {
    show();

    addCidr(0, "198.51.100.0/24");
    submit();

    await waitFor(() => expect(create).toHaveBeenCalledWith("/vpc/v1/addressPools", expect.anything()));
    expect(body().v4_cidr_blocks).toEqual(["198.51.100.0/24"]);
    expect(body().v6_cidr_blocks).toEqual([]);
  });

  it("зоны предлагаются из каталога, и глобальный пул назван прямо", async () => {
    show();

    const zone = within(underLabel("Зона")).getByRole("combobox");
    await waitFor(() =>
      expect([...zone.querySelectorAll("option")].map((o) => o.textContent)).toContain("ru-central1-a"),
    );
    expect([...zone.querySelectorAll("option")].map((o) => o.textContent)).toContain("(глобальный — без зоны)");
  });

  it("глобальный пул уезжает БЕЗ зоны, а не с пустой строкой", async () => {
    show();

    addCidr(0, "198.51.100.0/24");
    submit();

    await waitFor(() => expect(create).toHaveBeenCalled());
    expect(body().zone_id).toBeUndefined();
  });

  it("выбранная зона доезжает до тела", async () => {
    show();

    const zone = within(underLabel("Зона")).getByRole("combobox");
    await waitFor(() => expect(within(zone).getByText("ru-central1-a")).toBeInTheDocument());
    fireEvent.change(zone, { target: { value: "ru-central1-a" } });
    addCidr(0, "198.51.100.0/24");
    submit();

    await waitFor(() => expect(create).toHaveBeenCalled());
    expect(body().zone_id).toBe("ru-central1-a");
  });

  it("безымянный пул уезжает без имени — пустая строка была бы именем", async () => {
    show();

    addCidr(0, "198.51.100.0/24");
    submit();

    await waitFor(() => expect(create).toHaveBeenCalled());
    expect(body().name).toBeUndefined();
  });

  it("имя доезжает до тела и попадает в сообщение об успехе", async () => {
    const { onCancel, onSuccess } = show();

    fireEvent.change(within(underLabel("Имя")).getByRole("textbox"), { target: { value: "pool-a" } });
    addCidr(0, "198.51.100.0/24");
    submit();

    await waitFor(() => expect(toastSuccess).toHaveBeenCalledWith("Пул адресов pool-a создан"));
    expect(body().name).toBe("pool-a");
    expect(invalidate).toHaveBeenCalledWith("address-pools", null);
    expect(onSuccess).toHaveBeenCalledTimes(1);
    expect(onCancel).toHaveBeenCalledTimes(1);
  });

  it("отказ края показан текстом сервера, без кода протокола, форма не закрывается", async () => {
    create.mockRejectedValue(new ApiError(400, 3, null, "cidr overlaps existing pool"));
    const { onCancel } = show();

    addCidr(0, "198.51.100.0/24");
    submit();

    await waitFor(() => expect(toastError).toHaveBeenCalledWith("Создать пул адресов: cidr overlaps existing pool"));
    expect(onCancel).not.toHaveBeenCalled();
  });
});

describe("InlineAddressPoolCreateForm — черта", () => {
  // ПОРЯДОК ПОЛЕЙ ОДИН НА ВСЕ ФОРМЫ (решение владельца): общие поля, черта,
  // поля самого ресурса. Рукописная форма подчиняется тому же порядку, что и
  // общее тело формы, — иначе две соседние формы читаются как два разных места
  // продукта (канон консоли, правило 9).
  //
  // Утверждается МЕСТО черты, а не её наличие: черта, уехавшая в конец формы,
  // тоже «есть» и при этом ничего не отделяет.
  it("стоит между «Описание» и «Тип»", async () => {
    show();
    await screen.findByText("Тип");

    const [divider] = formDividers();
    expect(divider).toBeDefined();

    const position = (el: Element) => [...document.body.querySelectorAll("*")].indexOf(el);
    expect(position(screen.getByText("Описание"))).toBeLessThan(position(divider));
    expect(position(divider)).toBeLessThan(position(screen.getByText("Тип")));
  });
});
