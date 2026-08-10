// Dev-прокси system обязан покрывать домены, к которым модуль реально ходит.
//
// Промах прокси в dev не печатает ничего: список просто пуст, форма просто без
// вариантов, — поэтому расхождение между «какие ресурсы модуль показывает» и
// «куда он умеет дозвониться» живёт молча.
//
// Обе стороны берутся ЗНАЧЕНИЯМИ: маршрутизируемые спеки — тот самый перечень,
// по которому построены маршруты (`ROUTED_SPECS`), домены страницы поиска — её
// собственная таблица целей, правила прокси — `server.proxy` загруженного
// `vite.config.ts`. Прежняя редакция разбирала SystemPage.tsx и vite.config.ts
// как ТЕКСТ: такой разбор молча возвращает пустой набор при любой смене формы
// записи, и «все домены покрыты» становится истинным даром.
//
// Страница поиска включена сюда намеренно: она монтируется ЭТИМ модулем, ходит
// в чужие домены (`/iam`, `/vpc`, `/geo`) и на промах прокси отвечает пустой
// выдачей, неотличимой от «ничего не найдено». Отдельная проба, стерегущая
// СЛОВА о её доменах в комментариях, снята: она утверждала о тексте, а не о
// достижимости, и упасть на промахе прокси не могла.

import { jest } from "@jest/globals";
import { SEARCH_DOMAINS } from "@shared/pages/SystemSearchPage";
import { ROUTED_SPECS } from "./SystemPage";

const covered = (address: string, prefixes: string[]) => prefixes.some((p) => address.startsWith(p));

/** Все адреса, по которым модуль может пойти ради маршрутизируемых спек. */
function specAddresses(): string[] {
  const out = new Set<string>();
  for (const spec of ROUTED_SPECS) {
    for (const p of [spec.apiPath, spec.admin?.basePath, spec.internalGetPath]) {
      if (p) out.add(p);
    }
  }
  return [...out].sort();
}

const searchAddresses = () => [...new Set(SEARCH_DOMAINS.map((d) => d.path))].sort();

let prefixes: string[] = [];

beforeAll(async () => {
  jest.unstable_mockModule("vite", () => ({ defineConfig: (c: unknown) => c }));
  jest.unstable_mockModule("@originjs/vite-plugin-federation", () => ({ default: () => ({ name: "federation" }) }));
  jest.unstable_mockModule("@vitejs/plugin-react", () => ({ default: () => ({ name: "react" }) }));
  (globalThis as unknown as { __dirname: string }).__dirname = new URL("../../..", import.meta.url).pathname;
  const configModule = "../../../vite.config";
  const mod = (await import(configModule)) as unknown as {
    default: { server?: { proxy?: Record<string, unknown> } };
  };
  prefixes = Object.keys(mod.default.server?.proxy ?? {});
});

describe("system dev-прокси против адресов, по которым ходит модуль", () => {
  it("объём осмотренного назван: сколько спек, адресов и прокси прочитано", () => {
    expect(ROUTED_SPECS.length).toBeGreaterThanOrEqual(3);
    expect(specAddresses().length).toBeGreaterThanOrEqual(3);
    expect(searchAddresses().length).toBeGreaterThanOrEqual(5);
    expect(prefixes.length).toBeGreaterThanOrEqual(3);
    // Модуль обязан выходить за пределы одного домена — иначе утверждения ниже
    // были бы тождественно истинными.
    const domains = new Set([...specAddresses(), ...searchAddresses()].map((a) => a.split("/")[1]));
    expect(domains.size).toBeGreaterThanOrEqual(3);
  });

  it("каждый адрес маршрутизируемой спеки резолвится через прокси", () => {
    expect(specAddresses().filter((a) => !covered(a, prefixes))).toEqual([]);
  });

  it("каждый адрес страницы поиска резолвится через прокси", () => {
    expect(searchAddresses().filter((a) => !covered(a, prefixes))).toEqual([]);
  });

  it("непокрытый префикс предикат считает недостижимым (контроль в обратную сторону)", () => {
    expect(covered("/geo/v1/regions", prefixes)).toBe(true);
    expect(covered("/nothing/v1/here", prefixes)).toBe(false);
  });
});
