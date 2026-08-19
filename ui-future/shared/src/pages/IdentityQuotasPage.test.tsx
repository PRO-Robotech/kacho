// Человек видит свой предел на число аккаунтов и свой расход (#622).
//
// ПРЕДМЕТ. Сервер отдаёт пределы, носителем которых является ЛИЧНОСТЬ
// (`GET /iam/v1/quotas`), консоль их не показывала. Упёршийся в потолок аккаунтов
// получал отказ на первом действии, к которому платформа его приглашает, не видя
// ни величины, ни расхода, ни того, что потолок вообще есть.
//
// ЧТО ЗДЕСЬ ПРОВЕРЯЕТСЯ, А ЧТО — ПРОБОЙ БРАУЗЕРОМ. Здесь: страница спрашивает
// ИМЕННО тот путь, чтение не требует ни проекта, ни идентификатора, и число
// «занято» доходит до столбца. Там: край этот путь действительно подаёт, а адрес
// открывается без единого проекта (`e2e/specs/identity-quotas.spec.ts`).
// Модульная проба подменяет ответ и потому о крае не утверждает ничего.

import { render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { PageHeaderSlotProvider } from "@shared/components/molecules/PageHeaderSlot";
import { requestUrl } from "@shared/test/fetch-capture";
import { IdentityQuotasPage } from "./IdentityQuotasPage";

const realFetch = globalThis.fetch;
let urls: string[] = [];

function stub(body: unknown, ok = true) {
  urls = [];
  globalThis.fetch = (input: RequestInfo | URL) => {
    urls.push(requestUrl(input));
    return Promise.resolve({
      ok,
      status: ok ? 200 : 500,
      statusText: ok ? "OK" : "Internal Server Error",
      text: () => Promise.resolve(JSON.stringify(body)),
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
      {/* Адрес БЕЗ проекта — и это часть утверждения, а не деталь харнесса:
          страница, которой нужен `projectId`, недостижима для того, у кого
          проектов нет. */}
      <MemoryRouter initialEntries={["/iam/quotas"]}>
        <PageHeaderSlotProvider>
          <IdentityQuotasPage />
        </PageHeaderSlotProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

const личный = {
  kind: "iam.account",
  limit: 5,
  used: 2,
  source_scope: "DEFAULT",
  source_scope_id: "",
  carrier_type: "identity",
  carrier_id: "ory-subject-1",
};

/** Ячейки строки, чей текст содержит `label`. */
function rowCells(label: string): string[] {
  const cell = screen.getByText(label);
  const row = cell.closest("tr");
  if (!row) throw new Error(`строка «${label}» не найдена`);
  return Array.from(within(row as HTMLElement).queryAllByRole("cell")).map((c) => c.textContent ?? "");
}

describe("витрина квот личности", () => {
  it("спрашивает владельца величин и НИЧЕГО у него не уточняет", async () => {
    // У чтения нет полей запроса: предмет — проверенный вызывающий. Параметр,
    // приехавший сюда (проект, аккаунт, идентификатор), означал бы вопрос о
    // ком-то другом — то есть вопрос, которого контракт не задаёт.
    stub({ quotas: [личный] });
    renderPage();
    await waitFor(() => expect(urls.length).toBeGreaterThan(0));
    const u = new URL(urls[0], "http://x");
    expect(u.pathname).toBe("/iam/v1/quotas");
    expect(Array.from(u.searchParams.keys())).toEqual([]);
  });

  it("показывает четвёрку: вид, предел, занято, источник", async () => {
    stub({ quotas: [личный] });
    renderPage();
    const cells = await waitFor(() => rowCells("Аккаунты"));
    expect(cells.join(" | ")).toContain("5");
    expect(cells.join(" | ")).toContain("2");
    expect(cells.join(" | ")).toMatch(/умолчание/i);
  });

  it("расход показан ЧИСЛОМ, а не именем носителя", async () => {
    // Ровно тот дефект, который даёт неназванный предмет страницы: строка с
    // носителем-личностью прошла бы как «считается не здесь», и человек не
    // увидел бы единственного числа, ради которого пришёл.
    stub({ quotas: [личный] });
    renderPage();
    const cells = await waitFor(() => rowCells("Аккаунты"));
    const занято = cells.find((c) => c.trim() === "2");
    expect(занято ?? `РАСХОД НЕ ПОКАЗАН; ячейки: ${cells.join(" | ")}`).toBe("2");
    for (const c of cells) expect(c).not.toMatch(/Считается/);
  });

  it("предел 0 — это предел, а не отсутствие предела", async () => {
    // «Нельзя завести ни одного» и «ограничения нет» — противоположные
    // утверждения; спутав их, человек прочитает отказ как сбой платформы.
    stub({ quotas: [{ ...личный, limit: 0, used: 0 }] });
    renderPage();
    const cells = await waitFor(() => rowCells("Аккаунты"));
    expect(cells.filter((c) => c.trim() === "0")).toHaveLength(2);
    expect(screen.queryByText(/не назвал ни одного/i)).toBeNull();
  });

  it("пустой ответ — это НАХОДКА, а не «квот нет»", async () => {
    // Пустой набор достижим, когда величина не назначена ни на одной области.
    // «Потолок не назван» есть отказ, а не бесконечность: показать это как
    // «ограничений нет» значило бы пообещать возможность, которой нет.
    stub({ quotas: [] });
    renderPage();
    expect(await screen.findByText(/не назвал ни одного/i)).toBeTruthy();
  });

  it("отказ показывается отказом, а не пустой витриной", async () => {
    stub({ message: "boom" }, false);
    renderPage();
    expect(await screen.findByText(/Пределы не прочитаны/i)).toBeTruthy();
    expect(screen.queryByText(/не назвал ни одного/i)).toBeNull();
  });

  it("арендатору не предлагается менять величину", async () => {
    // Величину назначает администратор облака на внутреннем слушателе. Кнопка
    // здесь была бы обещанием действия, которое у арендатора отказом и кончится.
    stub({ quotas: [личный] });
    renderPage();
    await waitFor(() => rowCells("Аккаунты"));
    for (const подпись of [/Изменить предел/i, /Поднять квоту/i, /Редактировать/i]) {
      expect(screen.queryAllByRole("button", { name: подпись })).toHaveLength(0);
    }
  });
});
