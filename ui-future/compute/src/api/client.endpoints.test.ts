// Шапка client.ts перечисляет эндпоинты, которые адресует это приложение. Перечень
// в комментарии — утверждение о дереве, и оно переживает свой предмет молча: домен
// уезжает в другой сервис, ресурс перестаёт вызываться, новый добавляется — а строка
// остаётся прежней и читается как действительность. Поэтому перечень утверждается
// против самого исходника, а не сверяется глазами.
//
// Сравниваются ДВА множества:
//   объявленное — токены пути из шапки client.ts (до первого import);
//   адресуемое  — токены пути из строковых литералов ВСЕХ нетестовых файлов src.
// Равенство в обе стороны: лишняя строка в шапке — находка ровно так же, как
// недостающая. Именно недостающая и была дефектом (перечень не называл
// /vpc/v1/networkInterfaces, который приложение адресует), а лишние строки
// назывались (networks/subnets/routeTables как top-level ресурсы vpc).
//
// Комментарии из «адресуемого» исключены ТОКЕНИЗАТОРОМ typescript, а не regexp'ом
// по тексту: в дереве есть комментарий, цитирующий путь в обратных кавычках
// (ResourceShell.tsx), и текстовый предикат зачёл бы его как вызов — то есть шапка
// подтверждала бы сама себя. Что дискриминатор работает, проверяется отдельным
// утверждением: шапка client.ts обязана дать НОЛЬ адресуемых токенов.
//
// Объём осмотренного печатается числами: «ноль находок» обязано быть отличимо от
// «ноль прочитанного».

import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

import ts from "typescript";

import { hasDependencyResolver } from "@/lib/dependency-graph";
import { REGISTRY } from "@/lib/resource-registry";

const SRC_DIR = fileURLToPath(new URL("..", import.meta.url));
const CLIENT_TS = join(SRC_DIR, "api", "client.ts");

/** Путь домена: /<domain>/v1/... либо /operations/... */
const PATH_RE = /^\/(?:[a-z][a-z0-9]*\/v1\/|operations(?:\/|$))/;

/**
 * Канонизация: динамический сегмент → `{id}`; хвостовой `/{id}` отбрасывается,
 * если после этого остаётся хотя бы три сегмента (`/vpc/v1/networkInterfaces/{id}`
 * и `/vpc/v1/networkInterfaces` — один и тот же ресурс). `/operations/{id}` короче
 * порога и потому сохраняет свой сегмент — это и есть его настоящий маршрут.
 */
function canon(raw: string): string | null {
  const withPlaceholders = raw.replace(/\$\{[^}]*\}/g, "{id}").replace(/\{[^}]*\}/g, "{id}");
  if (!PATH_RE.test(withPlaceholders)) return null;
  const segs = withPlaceholders.split("/").filter(Boolean);
  if (segs.length > 3 && segs[segs.length - 1] === "{id}") segs.pop();
  return `/${segs.join("/")}`;
}

/**
 * Текст литерала целиком. Template-выражение склеивается ИЗ ЧАСТЕЙ с подстановкой
 * на месте каждой дырки: по кускам `/vpc/v1/networks/` и `/subnets` путь читается
 * как два разных, и подколлекция теряется — ровно тот случай, когда предикат
 * молчит не потому, что находки нет.
 */
function literalText(node: ts.Node): string | null {
  if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) return node.text;
  if (ts.isTemplateExpression(node)) {
    return node.head.text + node.templateSpans.map((s) => "${x}" + s.literal.text).join("");
  }
  return null;
}

/** Токены пути из строковых литералов файла. Комментарии не читаются. */
function addressedIn(source: string, fileName: string): string[] {
  const sf = ts.createSourceFile(fileName, source, ts.ScriptTarget.ESNext, true, ts.ScriptKind.TSX);
  const out: string[] = [];
  const visit = (node: ts.Node): void => {
    const text = literalText(node);
    if (text !== null) {
      for (const m of text.matchAll(/\/[A-Za-z0-9${}._:-]+(?:\/[A-Za-z0-9${}._:-]+)*/g)) {
        const c = canon(m[0]);
        if (c) out.push(c);
      }
    }
    // Части template-выражения дальше не обходим: они уже склеены выше.
    if (text === null || !ts.isTemplateExpression(node)) ts.forEachChild(node, visit);
    else for (const s of node.templateSpans) visit(s.expression);
  };
  visit(sf);
  return out;
}

/** Шапка файла — всё до первого import. */
function header(source: string): string {
  const i = source.indexOf("\nimport ");
  return i === -1 ? source : source.slice(0, i);
}

/** Токены пути, ОБЪЯВЛЕННЫЕ в шапке. Плейсхолдеры карты методов (`<plural>`) не подходят под PATH_RE. */
function declaredIn(headerText: string): string[] {
  const out: string[] = [];
  for (const m of headerText.matchAll(/\/[A-Za-z0-9${}._:<>-]+(?:\/[A-Za-z0-9${}._:<>-]+)*/g)) {
    const c = canon(m[0]);
    if (c && !c.includes("<")) out.push(c);
  }
  return out;
}

// Обход ПРОД-исходников. Тестовая оснастка исключается ДВУМЯ признаками, и второй
// не избыточен: `src/test/` — каталог оснастки целиком (setup, стабы antd/стилей,
// моки, фикстуры аудитов), и лежащий там файл БЕЗ суффикса `.test.ts` под первый
// признак не подпадает. Именно так гейт и покраснел: аудит подмаршрута операций
// несёт литерал `"/operations"` как ПРЕДИКАТ ПОИСКА, а обход прочитал его как
// адрес, который приложение якобы зовёт.
//
// Это сужение области, а не послабление утверждения: внутри оставшегося набора
// равенство множеств по-прежнему точное — лишняя строка шапки находка ровно так
// же, как недостающая. Фикстуры оснастки адресацией продукта не являются by
// construction, а настоящая адресация из них недостижима: прод-код на `src/test/`
// не ссылается (иначе стабы уехали бы в бандл).
function tsFiles(dir: string, acc: string[] = []): string[] {
  for (const e of readdirSync(dir, { withFileTypes: true })) {
    const p = join(dir, e.name);
    if (e.isDirectory()) {
      if (e.name === "test") continue;
      tsFiles(p, acc);
    } else if (/\.tsx?$/.test(e.name) && !/\.test\.tsx?$/.test(e.name)) acc.push(p);
  }
  return acc;
}

describe("шапка client.ts перечисляет ровно те эндпоинты, которые адресует исходник", () => {
  const files = tsFiles(SRC_DIR);
  const clientSource = readFileSync(CLIENT_TS, "utf8");

  const addressed = new Set<string>();
  for (const f of files) {
    for (const token of addressedIn(readFileSync(f, "utf8"), f)) addressed.add(token);
  }

  const declared = new Set(declaredIn(header(clientSource)));

  it("прочитан непустой объём: файлы, объявленные и адресуемые токены", () => {
    // Положительный контроль объёма — без него «множества совпали» неотличимо от
    // «оба пусты, потому что предикат ничего не нашёл».
    expect(files.length).toBeGreaterThan(100);
    expect(declared.size).toBeGreaterThan(5);
    expect(addressed.size).toBeGreaterThan(5);
    // eslint-disable-next-line no-console
    console.log(
      `[endpoints] файлов прочитано: ${files.length}; объявлено: ${declared.size}; адресуется: ${addressed.size}`,
    );
  });

  it("дискриминатор отделяет код от комментария: шапка даёт ноль адресуемых токенов", () => {
    // Шапка целиком состоит из путей, но это комментарий. Если бы предикат читал
    // текст, а не литералы, перечень подтверждал бы сам себя и утверждение ниже
    // было бы зелёным всегда.
    expect(addressedIn(header(clientSource), CLIENT_TS)).toEqual([]);
  });

  it("объявленное множество равно адресуемому", () => {
    expect([...declared].sort()).toEqual([...addressed].sort());
  });
});

describe("оговорка шапки про недостижимые ветки разбора зависимостей", () => {
  // dependency-graph несёт ветки для видов ресурсов, спек которых в реестре этого
  // приложения нет: до них не доходит ни один маршрут, хотя их пути в исходнике
  // адресуются. Оговорка в шапке держится этим утверждением и сама истекает —
  // заведут в compute спеку networks, и она покраснеет.
  const specIds = new Set(Object.keys(REGISTRY));

  it.each(["networks", "subnets", "addresses"])("ветка %s разбора есть, а спеки в реестре нет", (id) => {
    expect(hasDependencyResolver(id)).toBe(true);
    expect(specIds.has(id)).toBe(false);
  });

  it("ветка network-interfaces, наоборот, достижима — спека в реестре есть", () => {
    // Законный близнец той же формы: без него утверждение выше зеленело бы на
    // реестре, в котором нет вообще ничего.
    expect(hasDependencyResolver("network-interfaces")).toBe(true);
    expect(specIds.has("network-interfaces")).toBe(true);
  });
});
