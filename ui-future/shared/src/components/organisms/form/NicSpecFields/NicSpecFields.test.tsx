// Секция сетевого интерфейса в форме создания машины. Все «сырые» поля живут в
// объекте формы, а тело запроса собирается из них позже, — поэтому предмет
// пробы: что этот объект СОДЕРЖИТ после действий человека.
//
// Два свойства ломаются молча и стоят дорого:
//
//  1. смена режима публичного адреса с «Список» на любой другой ОБЯЗАНА снять
//     ранее выбранный адрес. Не снятый, он останется в объекте формы и уедет в
//     тело — машина получит публичный адрес, которого человек уже отказался
//     запрашивать;
//  2. снятие галочки «использовать существующий интерфейс» обязано снять и сам
//     идентификатор интерфейса. Оставшийся, он делает всю остальную секцию
//     бессмысленной: сборщик тела отдаёт только его.
//
// `Segmented` общего стенда-заменителя — пустой `<div>`: варианты он не рисует,
// поэтому переключение режима было бы недостижимо, а утверждение о нём —
// истинным при любом поведении. Здесь он переопределён кнопками.

import { jest } from "@jest/globals";
import React from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { antdStub } from "@shared/test/antd-stub";

interface SegOption {
  value: string;
  label?: React.ReactNode;
}

interface SegProps {
  value?: string;
  options?: SegOption[];
  onChange?: (value: string) => void;
}

jest.unstable_mockModule("antd", () => ({
  ...antdStub(),
  Segmented: ({ value, options, onChange }: SegProps) =>
    React.createElement(
      "div",
      { "data-testid": "segmented" },
      (options ?? []).map((o) =>
        React.createElement(
          "button",
          {
            key: o.value,
            type: "button",
            "aria-pressed": value === o.value,
            onClick: () => onChange?.(o.value),
          },
          o.label,
        ),
      ),
    ),
}));

const list = jest.fn<(path: string, q?: unknown) => Promise<Record<string, unknown>>>();

jest.unstable_mockModule("@shared/api/client", () => ({
  api: { list, get: jest.fn(), create: jest.fn(), update: jest.fn(), delete: jest.fn(), action: jest.fn() },
  ApiError: class ApiError extends Error {},
}));

const { contextApi } = await import("@shared/lib/context-store");
const { NicSpecFields } = await import("./NicSpecFields");

const PREFIX = "network_interface_specs[0]";

function show(initial: Record<string, unknown>) {
  let current = initial;
  const onChange = jest.fn((next: Record<string, unknown>) => {
    current = next;
  });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <NicSpecFields pathPrefix={PREFIX} value={initial} onChange={onChange} />
    </QueryClientProvider>,
  );
  return { onChange, latest: () => current };
}

const nic = (over: Record<string, unknown>) => ({ network_interface_specs: [over] });
const specOf = (obj: Record<string, unknown>) =>
  (obj.network_interface_specs as Record<string, unknown>[])[0] ?? {};

beforeEach(() => {
  jest.clearAllMocks();
  contextApi.setProject({ id: "prj-1", name: "проект", accountId: "acc-1" });
  // Порт возвращает промис — заменитель возвращает его же, но ждать ему нечего:
  // ответ известен сразу. `async` без `await` обещало ожидание, которого нет.
  list.mockImplementation((path: string) => {
    if (path.includes("/networks")) return Promise.resolve({ networks: [{ id: "net-1", name: "основная" }] });
    if (path.includes("/subnets")) {
      return Promise.resolve({ subnets: [{ id: "sub-1", name: "внутренняя", network_id: "net-1" }] });
    }
    if (path.includes("/addresses")) {
      return Promise.resolve({
        addresses: [
          { id: "adr-ext", name: "публичный", external_ipv4_address: { address: "203.0.113.7" } },
          { id: "adr-int", internal_ipv4_address: { subnet_id: "sub-1", address: "10.0.1.5" } },
        ],
      });
    }
    return Promise.resolve({});
  });
});

const segButton = (label: string) =>
  [...screen.getByTestId("segmented").querySelectorAll("button")].find((b) => b.textContent === label)!;

describe("NicSpecFields — публичный адрес", () => {
  it("выбранный адрес снимается при отказе от режима «Список»", () => {
    const { onChange, latest } = show(
      nic({ subnet_id: "sub-1", _ext_mode: "list", _ext_addr_id: "adr-ext", _ext_addr_value: "203.0.113.7" }),
    );

    fireEvent.click(segButton("Без адреса"));

    expect(onChange).toHaveBeenCalled();
    const after = specOf(latest());
    expect(after._ext_mode).toBe("none");
    // Оставшийся идентификатор уехал бы в тело: машина получила бы публичный
    // адрес, от которого человек только что отказался.
    expect(after).not.toHaveProperty("_ext_addr_id");
    expect(after).not.toHaveProperty("_ext_addr_value");
  });

  it("переход в «Автоматически» тоже снимает ранее выбранный адрес", () => {
    const { latest } = show(nic({ subnet_id: "sub-1", _ext_mode: "list", _ext_addr_id: "adr-ext" }));

    fireEvent.click(segButton("Автоматически"));

    expect(specOf(latest())._ext_mode).toBe("auto");
    expect(specOf(latest())).not.toHaveProperty("_ext_addr_id");
  });

  it("переход В «Список» ничего не снимает — контроль в обратную сторону", () => {
    // Без этой пары «снимает» удовлетворялось бы обработчиком, который чистит
    // всегда, — и выбрать адрес стало бы невозможно.
    const { latest } = show(nic({ subnet_id: "sub-1", _ext_mode: "none", _ext_addr_id: "adr-ext" }));

    fireEvent.click(segButton("Список"));

    expect(specOf(latest())._ext_mode).toBe("list");
    expect(specOf(latest())._ext_addr_id).toBe("adr-ext");
  });
});

describe("NicSpecFields — существующий интерфейс", () => {
  it("снятие галочки снимает и идентификатор интерфейса", () => {
    const { latest } = show(nic({ _use_existing_nic: true, nic_id: "nic-1" }));

    fireEvent.click(screen.getByRole("switch"));

    expect(specOf(latest())._use_existing_nic).toBe(false);
    // Иначе сборщик тела отдаст только nic_id, и вся остальная секция —
    // подсеть, адреса — окажется молча выброшенной.
    expect(specOf(latest())).not.toHaveProperty("nic_id");
  });
});

describe("NicSpecFields — что видит человек", () => {
  it("подсеть без внутреннего адреса подписана явно", () => {
    show(nic({ subnet_id: "sub-1" }));

    expect(screen.getByText("без внутреннего адреса")).toBeInTheDocument();
  });

  it("зональное требование названо на месте, а не только в отказе края", () => {
    show(nic({ subnet_id: "sub-1" }));

    expect(screen.getByText("Подсеть должна быть в той же зоне, что и ВМ.")).toBeInTheDocument();
  });
});
