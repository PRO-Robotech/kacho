// Маршрут может указывать на ШЛЮЗ, а не только на адрес (#375).
//
// ПРЕДМЕТ. Контракт статического маршрута несёт взаимоисключающую группу из двух
// ветвей: следующий узел — адрес ЛИБО шлюз. Форма знала одну. Ветвь шлюза была
// объявлена контрактом, реализована сервером (домен хранит поле, обработчик
// проверяет исключающее ИЛИ, на неё есть свои пробы) — и невыразима из консоли.
//
// ОТДЕЛЬНО — ПОЧЕМУ ЭТО ХУЖЕ ПРОСТОГО ПРОПУСКА. Отсутствие было ОБЪЯВЛЕНО, и
// объявлено неверно: два комментария утверждали, что сервер ветви не умеет.
// Следующий читатель принимал это за причину и не проверял. Утверждение о чужом
// коде живёт ровно до тех пор, пока чужой код его подтверждает, — а этот перестал
// подтверждать давно.

import { render, screen, fireEvent } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { RoutesEditor, type RouteEntry } from "./RoutesEditor";

const realFetch = globalThis.fetch;

beforeEach(() => {
  // Список шлюзов проекта — источник вариантов для ветви шлюза.
  globalThis.fetch = () =>
    Promise.resolve({
      ok: true,
      status: 200,
      statusText: "OK",
      text: () => Promise.resolve(JSON.stringify({ gateways: [{ id: "gw-1", name: "выход-в-интернет" }] })),
    } as Response);
});
afterEach(() => {
  globalThis.fetch = realFetch;
});

function renderEditor(value: RouteEntry[], onChange: (next: RouteEntry[]) => void) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={["/projects/p1/vpc/routeTables/create"]}>
        <RoutesEditor value={value} onChange={onChange} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("следующий узел маршрута — обе ветви контракта", () => {
  it("ветвь адреса выражается — положительный контроль", () => {
    const calls: RouteEntry[][] = [];
    renderEditor([{ destination_prefix: "10.0.0.0/24", next_hop_address: "10.0.0.1" }], (n) => calls.push(n));
    const адрес = screen.getByDisplayValue("10.0.0.1");
    fireEvent.change(адрес, { target: { value: "10.0.0.2" } });
    expect(calls.at(-1)).toEqual([{ destination_prefix: "10.0.0.0/24", next_hop_address: "10.0.0.2" }]);
  });

  it("ветвь шлюза выбирается и доезжает до значения", () => {
    const calls: RouteEntry[][] = [];
    renderEditor([{ destination_prefix: "0.0.0.0/0", next_hop_address: "" }], (n) => calls.push(n));

    // Переключатель ветви — свой у каждой строки: маршруты в одной таблице
    // независимы, и общий переключатель менял бы их все разом.
    const выбор = screen.getByLabelText("Вид следующего узла");
    fireEvent.change(выбор, { target: { value: "gateway" } });

    expect(calls.at(-1)).toEqual([{ destination_prefix: "0.0.0.0/0", gateway_id: "" }]);
  });

  it("две ветви одновременно в строке не представимы", () => {
    // Группа взаимоисключающая: строка, несущая обе, — отказ сервера. Смена
    // ветви обязана СНИМАТЬ прежнюю, а не дописывать рядом.
    const calls: RouteEntry[][] = [];
    renderEditor([{ destination_prefix: "0.0.0.0/0", next_hop_address: "10.0.0.1" }], (n) => calls.push(n));
    fireEvent.change(screen.getByLabelText("Вид следующего узла"), { target: { value: "gateway" } });
    const строка = calls.at(-1)![0];
    expect(строка).not.toHaveProperty("next_hop_address");
    expect(строка).toHaveProperty("gateway_id");
  });

  it("строка со шлюзом открывается в ветви шлюза, а не сбрасывается в адрес", () => {
    // Маршрут, приехавший с сервера со шлюзом, обязан открыться тем, чем он
    // является. Иначе первое же сохранение молча переводило бы его в адрес.
    renderEditor([{ destination_prefix: "0.0.0.0/0", gateway_id: "gw-1" }], () => {});
    expect((screen.getByLabelText("Вид следующего узла") as HTMLSelectElement).value).toBe("gateway");
  });
});
