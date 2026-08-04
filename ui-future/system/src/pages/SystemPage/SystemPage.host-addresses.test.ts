// Каждый адрес под `/system/…`, который рекламирует оболочка, обязан быть
// объявлен маршрутом system-remote'а.
//
// Хост монтирует `/system/*` на этот remote (host/src/App.tsx) и держит в рейле
// постоянный пункт «Поиск» → `/system/search` (host HostRail; два собственных
// теста хоста утверждают, что кнопка отрисована). Пока такого маршрута здесь нет,
// нажатие уводит на `*` → `regions`: пользователь получает список регионов вместо
// поиска, а отказ неотличим от «так и задумано» — никто не падает, ничего не
// логируется.
//
// Почему адрес принадлежит именно system-remote'у: страница поиска строит ссылки
// на `/system/regions/:id`, `/system/zones/:id`, `/system/address-pools/:id`, то
// есть на маршруты ЭТОГО remote'а, а его dev-прокси покрывает каждый домен, к
// которому она обращается (/geo, /iam, /vpc). Обе половины этого довода —
// названные домены и покрытие — держит SystemPage.search-domains.test.ts.
//
// Предикат читает ОБА исходника с диска (рейл хоста и таблицу маршрутов), поэтому
// расхождение между двумя приложениями видно в одном месте. Рендер здесь не
// используется намеренно: общий antd-стаб (shared/src/test/setup.ts) не отдаёт
// ConfigProvider, и ESM-линкер валит суиту раньше первого утверждения — предмет
// же проверки чисто маршрутный.

import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const systemRoot = path.resolve(here, "../../..");
const uiRoot = path.resolve(systemRoot, "..");

const pageSource = readFileSync(path.join(here, "SystemPage.tsx"), "utf8");
const navSource = readFileSync(path.join(systemRoot, "src/navigation.ts"), "utf8");
const railSource = readFileSync(path.join(uiRoot, "host/src/components/organisms/HostRail/HostRail.tsx"), "utf8");

/** Адреса под /system/…, рекламируемые рейлом хоста и навигацией remote'а. */
function advertised(): string[] {
  const out = new Set<string>();
  for (const src of [railSource, navSource]) {
    for (const m of src.matchAll(/"(\/system\/[a-z0-9/-]+)"/g)) out.add(m[1]);
  }
  return [...out].sort();
}

/** Пути, объявленные в <Route path="…"> обоих блоков этого модуля. */
function declared(): string[] {
  return [...pageSource.matchAll(/path="([^"]+)"/g)].map((m) => m[1]);
}

/** Объявлен ли адрес: точное совпадение либо покрытие префиксным `x/*`. */
function isDeclared(address: string, routes: string[]): boolean {
  const rel = address.replace(/^\/system\//, "");
  return routes.some((r) => {
    if (r === rel) return true;
    if (r.endsWith("/*")) return rel === r.slice(0, -2) || rel.startsWith(r.slice(0, -1));
    return false;
  });
}

describe("SystemPage — адреса, рекламируемые оболочкой", () => {
  const addresses = advertised();
  const routes = declared();

  it("объём осмотренного назван: сколько адресов и сколько маршрутов прочитано", () => {
    // «Ноль находок» обязано быть отличимо от «ноль прочитанного»: если любой из
    // двух парсеров замолчит, утверждение ниже станет вакуумным.
    expect(addresses.length).toBeGreaterThanOrEqual(6);
    expect(routes.length).toBeGreaterThanOrEqual(8);
    expect(addresses).toContain("/system/search");
  });

  it("каждый рекламируемый адрес объявлен маршрутом", () => {
    const orphans = addresses.filter((a) => !isDeclared(a, routes));
    expect(orphans).toEqual([]);
  });

  it("несуществующий адрес предикат НЕ считает объявленным (контроль в обратную сторону)", () => {
    // Без этого предыдущее утверждение зеленело бы на предикате «всё объявлено».
    expect(isDeclared("/system/regions", routes)).toBe(true);
    expect(isDeclared("/system/tokens/user-tokens", routes)).toBe(true);
    expect(isDeclared("/system/nothing-like-this", routes)).toBe(false);
  });
});
