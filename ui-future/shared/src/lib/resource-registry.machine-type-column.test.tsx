// Тип машины в списке машин — ССЫЛКА, а не моноширинный идентификатор.
//
// Предмет (#406, правило 2 канона консоли). Значение, которое есть
// идентификатор ДРУГОГО ресурса, показывается ссылкой: иконка типа, имя,
// переход. Тип машины — навигируемая запись каталога размера, у неё есть своя
// карточка и свой маршрут; показывать вместо неё `mt-…` значит адресовать
// человеку то, что адресовано машине.
//
// Наблюдаемое. В той же строке таблицы зона УЖЕ ссылка, а тип машины рядом с
// ней — моноширинный текст: одна строка, два поведения. Читается это как «этот
// переход не сделали», и сделать его человеку неоткуда — идентификатор он
// вынужден искать в каталоге руками.
//
// ДВЕ РАЗНЫЕ ОСИ, и путать их нельзя. Каталог типов машин ГЛОБАЛЕН по области
// ЧТЕНИЯ (`scope: "global"`) — `RefNameLink` спрашивает его без `project_id`, —
// но его РАЗДЕЛ смонтирован внутри проекта (`/projects/:projectId/compute/…`),
// потому что раздел рисует модуль compute. Поэтому проект здесь подаётся
// МАРШРУТОМ: ссылка берёт его из параметров, и без совпадения образца проба
// мерила бы собственный харнесс, а не продукт — что и случилось на первой её
// редакции.
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router";
import { REGISTRY } from "./resource-registry";

const realFetch = globalThis.fetch;

beforeEach(() => {
  globalThis.fetch = () =>
    Promise.resolve({
      ok: true,
      status: 200,
      statusText: "OK",
      text: () => Promise.resolve(JSON.stringify({ machine_types: [{ id: "mt-1", name: "standard-v3-2-8" }] })),
    } as Response);
});
afterEach(() => {
  globalThis.fetch = realFetch;
});

function show(ui: React.ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={["/projects/prj-1/compute/instances"]}>
        <Routes>
          <Route path="/projects/:projectId/compute/instances" element={<>{ui}</>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

function columnOf(specId: string, path: string) {
  const col = (REGISTRY[specId]?.columns ?? []).find((c) => c.path === path);
  if (!col) throw new Error(`колонка ${path} у ${specId} не найдена — предмет пробы исчез, а не прошёл`);
  return col;
}

describe("список машин: тип машины показан ссылкой", () => {
  it("предмет пробы существует — колонка типа машины в спеке есть", () => {
    // Иначе утверждения ниже зеленели бы на исчезнувшей колонке.
    expect(columnOf("compute-instances", "machine_type_id").header).toBe("Тип машины");
  });

  it("тип машины показан ИМЕНЕМ и ведёт на свою карточку", async () => {
    const col = columnOf("compute-instances", "machine_type_id");
    show(<>{col.render?.({ id: "ins-1", machine_type_id: "mt-1" })}</>);
    const link = await screen.findByRole("link", { name: /standard-v3-2-8/ });
    expect(link.getAttribute("href")).toBe("/projects/prj-1/compute/machine-types/mt-1");
  });

  // Отрицание в паре с положительным: без этого утверждение выше зеленело бы и
  // на правке, превратившей в ссылку каждую колонку подряд. Близнец выбран
  // РИСУЮЩИЙ — размер машины, — иначе «ссылки нет» получалось бы даром от того,
  // что колонка вообще ничего не рисует.
  it("размер машины ссылкой НЕ является — он не идентификатор чужого ресурса", () => {
    const col = columnOf("compute-instances", "effective_resources");
    const { container } = show(
      <>{col.render?.({ id: "ins-1", effective_resources: { v_cpu: 2, memory_mib: "8192" } })}</>,
    );
    expect(container.textContent).toContain("2 vCPU");
    expect(container.querySelector("a")).toBeNull();
  });
});
