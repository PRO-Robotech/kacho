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
/** Владелец, чьи виды называет тело проб: все они из домена vpc. */
const BODY_OWNER = "/vpc/v1/quotas";

function stub(body: unknown, ok = true, owner = BODY_OWNER) {
  urls = [];
  globalThis.fetch = (input: RequestInfo | URL) => {
    const url = requestUrl(input);
    urls.push(url);
    // Тело достаётся владельцу ПО АДРЕСУ, а не по позиции в очереди.
    //
    // Здесь стоял счётчик (`urls.length === 0`). Недетерминированным дублёр от
    // этого НЕ был — замер показал пять вызовов подряд, ровно в порядке
    // объявления доменов, без повторов, — но получатель тела зависел от порядка
    // объявления на странице, к предмету пробы отношения не имеющего.
    //
    // Адрес такой связи не имеет: тело принадлежит домену, чьи виды в нём
    // названы, и это видно из самого тела.
    const mine = url.includes(owner);
    return Promise.resolve({
      ok,
      status: ok ? 200 : 500,
      statusText: ok ? "OK" : "Internal Server Error",
      text: () => Promise.resolve(JSON.stringify(mine ? body : { quotas: [] })),
    } as Response);
  };
}

/**
 * Подставной край на посадке БЕЗ домена величин (#2515).
 *
 * Отказывают ВСЕ пятеро — посадка одна на установку, и владелец, ответивший
 * иначе, означал бы, что они разошлись в понимании установки. Тело отказа несёт
 * машинный признак: клиент различает полосы по нему, а не по прозе.
 */
function stubAuthorityAbsent() {
  urls = [];
  globalThis.fetch = (input: RequestInfo | URL) => {
    urls.push(requestUrl(input));
    return Promise.resolve({
      ok: false,
      status: 400,
      statusText: "Bad Request",
      text: () =>
        Promise.resolve(
          JSON.stringify({
            code: 9,
            message:
              "resource count limits are not stated in this installation: no limit " +
              "authority is deployed, so no ceiling applies to any kind and no " +
              "mutation is refused for exceeding one",
            details: [{ reason: "QUOTA_AUTHORITY_ABSENT", domain: "vpc.kacho.cloud" }],
          }),
        ),
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
  // ОБЪЯВЛЕННОЕ ОТСУТСТВИЕ ДОМЕНА ВЕЛИЧИН — НЕ СБОЙ (#2515).
  //
  // Ручка домена величин принимает два законных значения, и второе объявляет,
  // что потолков в этой установке нет вовсе. Пока витрина этого не различала,
  // законная посадка показывалась красной плашкой «Пределы не прочитаны» — то
  // есть ровно тем состоянием, ради устранения которого витрина и заведена.
  it("посадку без домена величин НАЗЫВАЕТ, а не выдаёт за сбой", async () => {
    stubAuthorityAbsent();
    renderPage();

    // Названо: арендатор узнаёт, что потолков нет и просить не у кого.
    expect(await screen.findByText(/в этой установке не задаются/i)).toBeTruthy();
    // И не названо сбоем: «не прочитаны» утверждает поломку.
    expect(screen.queryByText(/Пределы не прочитаны/i)).toBeNull();
  });

  // Положительный близнец: тот же путь, отличается РОВНО ОДНИМ фактом — отказ
  // без машинного признака. Без него проба зеленела бы на витрине, которая
  // объявляет посадку на любой отказ подряд, — то есть научилась бы молчать о
  // настоящих сбоях.
  it("отказ БЕЗ этого признака по-прежнему показывается сбоем", async () => {
    stub({ message: "boom" }, false);
    renderPage();
    expect(await screen.findByText(/Пределы не прочитаны/i)).toBeTruthy();
    expect(screen.queryByText(/в этой установке не задаются/i)).toBeNull();
  });
});
