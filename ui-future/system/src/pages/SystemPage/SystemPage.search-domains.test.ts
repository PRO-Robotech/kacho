// Комментарий, называющий домены страницы поиска, — утверждение о дереве наравне
// с кодом, и стареет он молча.
//
// Предмет. `/system/search` монтируется ЭТИМ модулем, а сама страница живёт в
// shared. Оба здешних текста (шапка таблицы маршрутов и шапка соседнего теста)
// объясняют, ПОЧЕМУ адрес принадлежит system-remote'у, и в обоснование называют
// домены, которые страница опрашивает. Домены страницы могут поменяться в чужом
// каталоге — тогда обоснование останется убедительным и станет ложным, а падать
// будет нечему: dev-прокси покрывает шире, чем страница спрашивает, поэтому
// расхождение вообще ничего не ломает в рантайме.
//
// Поэтому здесь два разных утверждения, и второе не подменяет первое:
//   1. НАЗВАННЫЙ домен обязан быть настоящим — каждый домен API, упомянутый в
//      абзаце про страницу поиска, страница действительно опрашивает;
//   2. СУЩЕСТВО обоснования — все домены, которые страница опрашивает, покрыты
//      dev-прокси этого модуля (иначе адрес здесь и правда был бы не на месте).
//
// Словарь доменов выводится из общего реестра ресурсов (первый сегмент apiPath),
// а не выписывается: тогда `/system` (маршрут оболочки, не домен API) не считается
// доменом by construction, а новый домен продукта попадает в словарь сам.

import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { REGISTRY } from "@shared/lib/resource-registry";

const here = path.dirname(fileURLToPath(import.meta.url));
const systemRoot = path.resolve(here, "../../..");
const uiRoot = path.resolve(systemRoot, "..");

const pageSource = readFileSync(path.join(here, "SystemPage.tsx"), "utf8");
const hostAddressesTestSource = readFileSync(path.join(here, "SystemPage.host-addresses.test.ts"), "utf8");
const viteSource = readFileSync(path.join(systemRoot, "vite.config.ts"), "utf8");
const searchPageSource = readFileSync(path.join(uiRoot, "shared/src/pages/SystemSearchPage.tsx"), "utf8");

/** Словарь доменов API — первые сегменты apiPath общего реестра. */
function apiDomains(): Set<string> {
  const out = new Set<string>();
  for (const spec of Object.values(REGISTRY) as Array<{ apiPath?: string }>) {
    const seg = spec?.apiPath?.split("/")[1];
    if (seg) out.add(seg);
  }
  return out;
}

/** Домены, которые страница поиска РЕАЛЬНО опрашивает (её собственные path'ы). */
function searchDomains(): string[] {
  const out = new Set<string>();
  for (const m of searchPageSource.matchAll(/path:\s*"(\/[a-z][a-zA-Z0-9]*)\/v1\//g)) out.add(m[1].slice(1));
  return [...out].sort();
}

/** Абзац комментария, говорящий про страницу поиска, в произвольном исходнике. */
function searchParagraphs(src: string): string[] {
  const out: string[] = [];
  let buf: string[] = [];
  for (const line of src.split("\n")) {
    const m = /^\s*\/\/(.*)$/.exec(line);
    if (m && m[1].trim() !== "") {
      buf.push(m[1]);
      continue;
    }
    if (buf.length) {
      const text = buf.join(" ");
      if (/поиск/i.test(text) || text.includes("/system/search")) out.push(text);
      buf = [];
    }
  }
  if (buf.length) {
    const text = buf.join(" ");
    if (/поиск/i.test(text) || text.includes("/system/search")) out.push(text);
  }
  return out;
}

/** Домены API, названные в тексте (только те, что есть в словаре реестра). */
function namedDomains(text: string, vocabulary: Set<string>): string[] {
  const out = new Set<string>();
  for (const m of text.matchAll(/\/([a-z][a-zA-Z0-9]*)/g)) {
    if (vocabulary.has(m[1])) out.add(m[1]);
  }
  return [...out].sort();
}

function proxyPrefixes(): string[] {
  const start = viteSource.indexOf("proxy: {");
  expect(start).toBeGreaterThan(-1);
  const end = viteSource.indexOf("\n    },", start);
  return [...viteSource.slice(start, end).matchAll(/^\s{6}"(\/[^"]+)":/gm)].map((m) => m[1]);
}

describe("SystemPage — домены страницы поиска в коде и в обосновании", () => {
  const vocabulary = apiDomains();
  const domains = searchDomains();
  const prefixes = proxyPrefixes();
  const sources: Array<[string, string]> = [
    ["SystemPage.tsx", pageSource],
    ["SystemPage.host-addresses.test.ts", hostAddressesTestSource],
  ];

  it("объём осмотренного назван: словарь доменов, домены страницы, абзацы, прокси", () => {
    // «Ноль находок» обязано быть отличимо от «ноль прочитанного»: если любой из
    // парсеров замолчит, утверждения ниже станут вакуумными.
    expect(vocabulary.size).toBeGreaterThanOrEqual(4);
    expect(vocabulary.has("geo")).toBe(true);
    expect(domains.length).toBeGreaterThanOrEqual(2);
    expect(prefixes.length).toBeGreaterThanOrEqual(3);
    for (const [name, src] of sources) {
      expect([name, searchParagraphs(src).length > 0]).toEqual([name, true]);
    }
  });

  it("каждый домен, названный в обосновании, страница поиска действительно опрашивает", () => {
    const wrong: string[] = [];
    for (const [name, src] of sources) {
      for (const para of searchParagraphs(src)) {
        for (const d of namedDomains(para, vocabulary)) {
          if (!domains.includes(d)) wrong.push(`${name}: /${d}`);
        }
      }
    }
    expect(wrong).toEqual([]);
  });

  it("существо обоснования: dev-прокси модуля покрывает каждый домен страницы", () => {
    const uncovered = domains.filter((d) => !prefixes.some((p) => `/${d}`.startsWith(p) || p.startsWith(`/${d}`)));
    expect(uncovered).toEqual([]);
  });

  it("предикат отличает названный домен от неназванного (контроль в обе стороны)", () => {
    // Без этого предыдущие утверждения зеленели бы на предикате «ничего не найдено».
    expect(namedDomains("прокси несёт /geo и /vpc", vocabulary)).toEqual(["geo", "vpc"]);
    expect(namedDomains("маршрут /system/search и /nothing-like-this", vocabulary)).toEqual([]);
    expect(searchParagraphs("// про поиск\n\nconst x = 1;\n")).toEqual([" про поиск"]);
    expect(searchParagraphs("// про регионы\n")).toEqual([]);
  });
});
