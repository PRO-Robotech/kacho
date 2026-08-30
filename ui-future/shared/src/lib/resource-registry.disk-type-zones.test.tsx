// Зоны класса диска — ССЫЛКИ, а не список машинных идентификаторов.
//
// Предмет (#407, правило 2 канона консоли). Поле, значение которого есть
// идентификатор другого ресурса, показывается ссылкой: иконка типа, имя,
// переход. Правило названо «любой поверхностью» — колонка списка от строки
// карточки не освобождена, и множественность тоже не освобождает: `zone_ids`
// это НЕСКОЛЬКО ссылок, а не другой вид значения.
//
// Наблюдаемое. В той же консоли зона показана именем везде, где она одна:
// у тома, у снимка, у класса диска в форме выбора. В каталоге классов она
// оставалась строкой вида `zone-…`, и один и тот же ресурс читался двумя
// способами на соседних экранах — ровно то, что правило 3 называет «другим
// местом продукта».
//
// Зона — ГЛОБАЛЬНЫЙ справочник geo, поэтому `RefNameLink` спрашивает её без
// project_id; на странице `/system/*` проекта в контексте нет вовсе, и колонка,
// требующая его, не резолвила бы имя ни разу.
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { REGISTRY } from "./resource-registry";

const realFetch = globalThis.fetch;

beforeEach(() => {
  globalThis.fetch = () =>
    Promise.resolve({
      ok: true,
      status: 200,
      statusText: "OK",
      text: () =>
        Promise.resolve(
          JSON.stringify({
            zones: [
              { id: "zone-a", name: "ru-central1-a" },
              { id: "zone-b", name: "ru-central1-b" },
            ],
          }),
        ),
    } as Response);
});
afterEach(() => {
  globalThis.fetch = realFetch;
});

function show(ui: React.ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>{ui}</MemoryRouter>
    </QueryClientProvider>,
  );
}

function zonesColumn() {
  const col = (REGISTRY["disk-types"]?.columns ?? []).find((c) => c.path === "zone_ids");
  if (!col) throw new Error("колонка zone_ids не найдена — предмет пробы исчез, а не прошёл");
  return col;
}

describe("каталог классов диска: зоны показаны ссылками", () => {
  it("предмет пробы существует — колонка зон в спеке есть", () => {
    // Иначе утверждения ниже зеленели бы на исчезнувшей колонке.
    expect(zonesColumn().path).toBe("zone_ids");
  });

  it("каждая зона показана ИМЕНЕМ и ведёт на свою карточку", async () => {
    const col = zonesColumn();
    show(<>{col.render?.({ id: "dt-1", zone_ids: ["zone-a", "zone-b"] })}</>);

    expect(await screen.findByText("ru-central1-a")).toBeInTheDocument();
    expect(await screen.findByText("ru-central1-b")).toBeInTheDocument();
    const hrefs = screen.getAllByRole("link").map((a) => a.getAttribute("href"));
    expect(hrefs).toEqual(expect.arrayContaining(["/system/zones/zone-a", "/system/zones/zone-b"]));
  });

  it("пустой список остаётся прочерком, а не пустой строкой ссылок (контроль)", () => {
    const col = zonesColumn();
    show(<>{col.render?.({ id: "dt-1", zone_ids: [] })}</>);

    expect(screen.queryAllByRole("link")).toHaveLength(0);
    expect(screen.getByText("—")).toBeInTheDocument();
  });

  it("отсутствие поля читается как пусто, а не как ошибка (контроль)", () => {
    const col = zonesColumn();
    show(<>{col.render?.({ id: "dt-1" })}</>);

    expect(screen.queryAllByRole("link")).toHaveLength(0);
    expect(screen.getByText("—")).toBeInTheDocument();
  });
});
