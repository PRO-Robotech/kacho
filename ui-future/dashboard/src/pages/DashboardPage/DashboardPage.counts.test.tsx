// Счётчики плиток — против того, что человек видит на главной.
//
// Плитка берёт число из `countsByModule[module.key]`, а загрузчики счётчиков
// заведены на странице поимённо (это хуки — циклом по витрине их не позвать).
// Плитка без своего загрузчика рисуется целиком — иконка, описание, подписи
// «Томов»/«Образов», — но на месте каждого числа стоит прочерк, НАВСЕГДА и при
// любом состоянии облака. Со стороны это неотличимо от «ресурсов нет»: тот же
// класс, что адрес без производителя на крае, только источник другой.
//
// Утверждается наблюдаемое: в КАЖДОЙ плитке витрины стоит число, а не прочерк,
// когда сервер отвечает непустыми списками. Перечня плиток здесь нет — проба
// идёт по самой витрине, поэтому новая плитка без загрузчика покраснеет сама.

import { render, screen, waitFor, within, renderHook } from "@testing-library/react";
import { jest } from "@jest/globals";

import { DashboardPage } from ".";
import { SERVICE_MODULES } from "../../lib/service-modules";
import { useModuleCounts } from "../../hooks/use-module-counts";
import type { HostContext } from "../../utils";

const projectContext: HostContext = {
  account: { id: "account-1", name: "Account 1" },
  project: { id: "project-1", name: "Project 1", accountId: "account-1" },
};

function jsonResponse(body: unknown) {
  return Promise.resolve({
    ok: true,
    text: () => Promise.resolve(JSON.stringify(body)),
    statusText: "OK",
  } as Response);
}

/**
 * Адрес запроса из аргумента fetch.
 *
 * Через `instanceof Request` его брать НЕЛЬЗЯ: в этом окружении `Request` не
 * объявлен вовсе (`typeof Request === "undefined"`), поэтому такая проверка
 * бросает прямо внутри подставного fetch, клиент ловит это как сетевой отказ и
 * КАЖДЫЙ счётчик остаётся пустым. Проба, утверждающая число, краснела бы тогда
 * на исправном продукте — из-за собственной фикстуры.
 */
function pathOf(input: unknown): string {
  if (typeof input === "string") return input;
  const url = (input as { url?: unknown })?.url;
  return typeof url === "string" ? url : String(input);
}

/**
 * Ответ на любой listPath витрины: один элемент под его же payloadKey.
 *
 * Аккаунты и проекты страница читает не только ради счётчиков, но и ради дерева
 * слева, поэтому их элементы правдоподобны: обрезанный элемент ронял бы
 * отрисовку дерева, и проба краснела бы на своей фикстуре, а не на предмете.
 */
function listAnswerFor(path: string): Record<string, unknown> | null {
  if (path.startsWith("/iam/v1/accounts")) return { accounts: [{ id: "account-1", name: "Account 1" }] };
  if (path.startsWith("/iam/v1/projects")) {
    return { projects: [{ id: "project-1", name: "Project 1", accountId: "account-1" }] };
  }
  for (const module of SERVICE_MODULES) {
    for (const stat of module.stats) {
      if (path.startsWith(stat.listPath)) return { [stat.payloadKey]: [{ id: "x-1", name: "x" }] };
    }
  }
  return null;
}

describe("счётчики плиток витрины", () => {
  beforeEach(() => {
    global.fetch = jest.fn<typeof fetch>();
    jest.spyOn(global, "fetch").mockImplementation((input) => jsonResponse(listAnswerFor(pathOf(input)) ?? {}));
  });

  afterEach(() => jest.restoreAllMocks());

  it("предпосылка: витрина непуста и каждая плитка обещает хотя бы один счётчик", () => {
    expect(SERVICE_MODULES.length).toBeGreaterThan(0);
    for (const m of SERVICE_MODULES) expect(m.stats.length).toBeGreaterThan(0);
  });

  it("в каждой плитке стоит число, а не прочерк навсегда", async () => {
    render(<DashboardPage context={projectContext} />);

    await waitFor(() => {
      const dashed = SERVICE_MODULES.filter((module) => {
        const tile = screen.getByTestId(`dashboard-tile-${module.key}`);
        const values = within(tile)
          .getAllByText(/^(\d+|—)$/)
          .map((el) => el.textContent);
        return values.length !== module.stats.length || values.includes("—");
      }).map((m) => m.key);
      expect(dashed).toEqual([]);
    });
  });

  it("страница спрашивает КАЖДЫЙ адрес, объявленный витриной", async () => {
    // Вторая половина того же факта: число в плитке не берётся из воздуха —
    // ему обязан соответствовать запрос по объявленному адресу.
    render(<DashboardPage context={projectContext} />);
    const wanted = SERVICE_MODULES.flatMap((m) => m.stats.map((s) => s.listPath));
    await waitFor(() => {
      const called = (global.fetch as jest.MockedFunction<typeof fetch>).mock.calls.map(([url]) => pathOf(url));
      const silent = wanted.filter((p) => !called.some((c) => c.startsWith(p)));
      expect(silent).toEqual([]);
    });
  });

  it("инъекция: неверный ключ ответа виден, а не выглядит нулём ресурсов", async () => {
    // Законный близнец утверждений выше: запрос уходит и ответ приходит, но
    // ключ не тот, что отдаёт край, — счётчик обязан отличаться от честного
    // числа. Без этого кейса «в плитке число» неотличимо от «всегда зелено».
    const legal = SERVICE_MODULES[0];
    const broken = { ...legal, stats: [{ ...legal.stats[0], payloadKey: "no-such-key" }] };

    const good = renderHook(() => useModuleCounts(legal, "project-1", "project_id"));
    await waitFor(() => expect(good.result.current[legal.stats[0].key]).toBe(1));

    const bad = renderHook(() => useModuleCounts(broken, "project-1", "project_id"));
    await waitFor(() => expect(bad.result.current[broken.stats[0].key]).toBe(0));
  });
});
