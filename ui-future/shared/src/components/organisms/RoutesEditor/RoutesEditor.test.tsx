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
// Линия строки берётся из ОБЩЕГО источника геометрии, а не выписывается здесь:
// проба, повторившая литерал, разошлась бы с продуктом молча.
import { editorRowStyle } from "@shared/components/organisms/form/editor-surface";

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
    const address = screen.getByDisplayValue("10.0.0.1");
    fireEvent.change(address, { target: { value: "10.0.0.2" } });
    expect(calls.at(-1)).toEqual([{ destination_prefix: "10.0.0.0/24", next_hop_address: "10.0.0.2" }]);
  });

  it("ветвь шлюза выбирается и доезжает до значения", () => {
    const calls: RouteEntry[][] = [];
    renderEditor([{ destination_prefix: "0.0.0.0/0", next_hop_address: "" }], (n) => calls.push(n));

    // Переключатель ветви — свой у каждой строки: маршруты в одной таблице
    // независимы, и общий переключатель менял бы их все разом.
    const select = screen.getByLabelText("Вид следующего узла");
    fireEvent.change(select, { target: { value: "gateway" } });

    expect(calls.at(-1)).toEqual([{ destination_prefix: "0.0.0.0/0", gateway_id: "" }]);
  });

  it("две ветви одновременно в строке не представимы", () => {
    // Группа взаимоисключающая: строка, несущая обе, — отказ сервера. Смена
    // ветви обязана СНИМАТЬ прежнюю, а не дописывать рядом.
    const calls: RouteEntry[][] = [];
    renderEditor([{ destination_prefix: "0.0.0.0/0", next_hop_address: "10.0.0.1" }], (n) => calls.push(n));
    fireEvent.change(screen.getByLabelText("Вид следующего узла"), { target: { value: "gateway" } });
    const row = calls.at(-1)![0];
    expect(row).not.toHaveProperty("next_hop_address");
    expect(row).toHaveProperty("gateway_id");
  });

  it("первая строка стыкуется с шапкой ОДНОЙ линией, у остальных линия своя", () => {
    // Решение владельца: линия РАЗДЕЛЯЕТ, поэтому первой строке она не нужна —
    // над ней уже стоит нижняя граница шапки колонок. Здесь редактор рисует не
    // таблицу, а сетку, и линии ей не схлопывает ничто: без правила на стыке
    // выходило 2px там, где по всей консоли 1px.
    //
    // Строки берутся ПОЛОЖЕНИЕМ в поверхности, а не поиском по стилю: у сетки
    // нет ролей, а совпадение по стилю выбрало бы и шапку — она той же сетки.
    // Состав поверхности утверждается, чтобы перестановка её частей роняла
    // пробу, а не заставляла её тихо смотреть на чужой узел.
    const { container } = renderEditor(
      [
        { destination_prefix: "10.0.0.0/24", next_hop_address: "10.0.0.1" },
        { destination_prefix: "0.0.0.0/0", next_hop_address: "10.0.0.2" },
      ],
      () => {},
    );

    const surface = container.firstElementChild as HTMLElement;
    const parts = Array.from(surface.children) as HTMLElement[];
    // шапка · две строки · подвал с «Добавить маршрут»
    expect(parts).toHaveLength(4);

    // `border-top: none` CSSOM не хранит вовсе, поэтому «линии нет» читается
    // пустым объявлением. Рядом — признак того, что строка вообще оформлена
    // (у неё своя сетка колонок): иначе пустое значило бы «стиля нет».
    expect(parts[1].style.gridTemplateColumns).not.toBe("");
    expect(parts[1].style.borderTop).toBe("");

    // Положительный контроль: у второй строки линия ЕСТЬ, и она — ровно та,
    // что объявлена общим источником. Без него «у первой линии нет» зеленело
    // бы на редакторе, потерявшем разделители целиком.
    const ref = document.createElement("div");
    ref.style.borderTop = String(editorRowStyle.borderTop);
    expect(parts[2].style.borderTop).toBe(ref.style.borderTop);
  });

  it("строка со шлюзом открывается в ветви шлюза, а не сбрасывается в адрес", () => {
    // Маршрут, приехавший с сервера со шлюзом, обязан открыться тем, чем он
    // является. Иначе первое же сохранение молча переводило бы его в адрес.
    renderEditor([{ destination_prefix: "0.0.0.0/0", gateway_id: "gw-1" }], () => {});
    expect(screen.getByLabelText<HTMLSelectElement>("Вид следующего узла").value).toBe("gateway");
  });
});
