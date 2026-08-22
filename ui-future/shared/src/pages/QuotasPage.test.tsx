// Арендатор видит свои пределы, потребление и источник значения (#364).
//
// ПРЕДМЕТ. Квоты включены на сервере, а в консоли их нет: упёршийся в предел не
// видит ни величины, ни потребления, ни того, кто эту величину задал. Отказ на
// пределе тогда неотличим для него от сбоя платформы, и каждый такой отказ
// становится обращением в поддержку.
//
// ГЛАВНОЕ, ЧТО ЗДЕСЬ ПРОВЕРЯЕТСЯ, — ЧЕГО СТРАНИЦА НЕ ПОКАЗЫВАЕТ. Виды, которые
// считаются внутри родительского ресурса, не имеют на уровне проекта
// единственного «занято». Ноль там не факт, а прочерк на месте живого факта —
// утверждение о ресурсе, которого никто не делал (`ui.md` правило 9).

import { render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router";
import { PageHeaderSlotProvider } from "@shared/components/molecules/PageHeaderSlot";
import { requestUrl } from "@shared/test/fetch-capture";
import { QuotasPage } from "./QuotasPage";

const realFetch = globalThis.fetch;
let urls: string[] = [];

/**
 * Подставной край.
 *
 * Страница спрашивает ПЯТЬ владельцев — по одному на домен, который списывает
 * квоту. Тело отдаётся ТОЛЬКО первому спрошенному (это владелец, чьи виды
 * называет проба), остальным — пустой набор.
 *
 * Один ответ на всех был бы дублёром СНИСХОДИТЕЛЬНЕЕ настоящего края: он вернул
 * бы одну и ту же строку пять раз, и витрина показала бы пятикратный дубль там,
 * где настоящий край отдаёт разные виды разных доменов. Проба на таком дублёре
 * краснела бы на исправной странице — то есть измеряла бы фикстуру, а не предмет.
 */
function stub(body: unknown, ok = true) {
  urls = [];
  globalThis.fetch = (input: RequestInfo | URL) => {
    const url = requestUrl(input);
    const first = urls.length === 0;
    urls.push(url);
    return Promise.resolve({
      ok,
      status: ok ? 200 : 500,
      statusText: ok ? "OK" : "Internal Server Error",
      text: () => Promise.resolve(JSON.stringify(first ? body : { quotas: [] })),
    } as Response);
  };
}

afterEach(() => {
  globalThis.fetch = realFetch;
});

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={["/projects/prj-1/quotas"]}>
        <PageHeaderSlotProvider>
          <Routes>
            <Route path="/projects/:projectId/quotas" element={<QuotasPage />} />
          </Routes>
        </PageHeaderSlotProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

const flat = {
  kind: "vpc.network",
  limit: 5,
  used: 2,
  source_scope: "DEFAULT",
  source_scope_id: "",
  carrier_type: "project",
  carrier_id: "prj-1",
};

const nested = {
  kind: "vpc.network.subnet",
  limit: 10,
  used: 0,
  source_scope: "ACCOUNT",
  source_scope_id: "acc-7",
  carrier_type: "vpc.network",
  carrier_id: "",
};

/** Ячейки строки, чей текст содержит `label`. */
function rowCells(label: string): string[] {
  const cell = screen.getByText(label);
  const row = cell.closest("tr");
  if (!row) throw new Error(`строка «${label}» не найдена`);
  return Array.from(within(row as HTMLElement).queryAllByRole("cell")).map((c) => c.textContent ?? "");
}

describe("витрина квот арендатора", () => {
  it("спрашивает пределы своего проекта", async () => {
    stub({ quotas: [flat] });
    renderPage();
    await waitFor(() => expect(urls.length).toBeGreaterThan(0));
    const u = new URL(urls[0], "http://x");
    expect(u.pathname).toBe("/vpc/v1/quotas");
    expect(u.searchParams.get("projectId")).toBe("prj-1");
  });

  it("спрашивает КАЖДОГО владельца, который списывает квоту", async () => {
    // Предмет пробы — полнота витрины, а не факт запроса. Домен, который
    // списывает и здесь не спрошен, оставляет своего арендатора упираться в
    // предел, которого тот не видит: ровно та беда, ради которой чтение и
    // заводилось. Перечень путей выписан, потому что он и есть утверждение;
    // вывести его из страницы значило бы сверять страницу с ней же.
    stub({ quotas: [flat] });
    renderPage();
    await waitFor(() => expect(urls.length).toBe(5));
    const paths = urls.map((u) => new URL(u, "http://x").pathname).sort();
    expect(paths).toEqual([
      "/compute/v1/quotas",
      "/nlb/v1/quotas",
      "/registry/v1/quotas",
      "/storage/v1/quotas",
      "/vpc/v1/quotas",
    ]);
  });

  it("показывает четвёрку: вид, предел, занято, источник", async () => {
    stub({ quotas: [flat] });
    renderPage();
    const cells = await waitFor(() => rowCells("Облачные сети"));
    expect(cells.join(" | ")).toContain("5");
    expect(cells.join(" | ")).toContain("2");
    expect(cells.join(" | ")).toMatch(/умолчание/i);
  });

  it("вид, считающийся внутри носителя, потребления НЕ показывает", async () => {
    // Ни числа, ни прочерка: значения нет вовсе. Ноль здесь читался бы как
    // «ничего не создано», а это неправда — просто счёт ведётся не тут.
    stub({ quotas: [nested] });
    renderPage();
    const cells = await waitFor(() => rowCells("Подсети в одной сети"));
    // Отказ обязан назвать, ЧТО было показано вместо носителя, — иначе разбор
    // упавшей пробы начинается с повторного запуска.
    const usedCell = cells.find((c) => /Считается в каждом/.test(c)) ?? `НЕТ НОСИТЕЛЯ; ячейки: ${cells.join(" | ")}`;
    expect(usedCell).toMatch(/Считается в каждом/);
    for (const c of cells) {
      expect(c).not.toBe("0");
      expect(c).not.toBe("—");
    }
  });

  it("а вид, считающийся в проекте, потребление показывает — положительный контроль", async () => {
    // Без него «не показывает» означало бы «не показывает никогда».
    stub({ quotas: [flat, nested] });
    renderPage();
    await waitFor(() => rowCells("Облачные сети"));
    expect(rowCells("Облачные сети").some((c) => c === "2")).toBe(true);
  });

  it("источник назван так, что видно, куда идти", async () => {
    stub({ quotas: [nested] });
    renderPage();
    const cells = await waitFor(() => rowCells("Подсети в одной сети"));
    expect(cells.join(" | ")).toContain("acc-7");
  });

  it("незнакомый вид показывается, а не пропадает", async () => {
    // Каталог видов растёт на сервере; витрина, знающая закрытый перечень,
    // молча теряла бы новые пределы — те самые, о которых арендатор не знает.
    stub({ quotas: [{ ...flat, kind: "будущий.вид" }] });
    renderPage();
    expect(await screen.findByText("будущий.вид")).toBeTruthy();
  });

  it("пустой ответ — это НАХОДКА, а не «квот нет»", async () => {
    // Контракт обещает полный набор видов всегда: проект, ничего не создавший,
    // получает их с нулями. Пустой массив означает, что что-то не так с
    // ответом, и выдать его за «ограничений нет» — сказать неправду.
    stub({ quotas: [] });
    renderPage();
    expect(await screen.findByText(/не назвал ни одного/i)).toBeTruthy();
  });

  it("отказ показывается отказом, а не пустой витриной", async () => {
    stub({ message: "boom" }, false);
    renderPage();
    await waitFor(() => expect(screen.queryByText(/не назвал ни одного/i)).toBeNull());
    expect(screen.queryByText(/Облачные сети/)).toBeNull();
  });
});
