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

import { existsSync, readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

import ts from "typescript";

import { hasDependencyResolver } from "@shared/lib/dependency-graph";
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

// Обход идёт ЗА ШИМ. Файл-шим — это исходник ЭТОГО приложения, у которого тело
// лежит в `shared/`: он не несёт собственных объявлений и целиком состоит из
// `export * from "@shared/…"` (#405, сведение форков). Не пойти за него значит
// перестать видеть адресацию, которая никуда не делась: `src/api/iam.ts` после
// сведения не содержит ни одного литерала, а приложение по-прежнему зовёт
// `/iam/v1/accounts`. Тогда равенство множеств чинилось бы ВЫЧЁРКИВАНИЕМ строк
// из шапки — то есть предикат заставлял бы документ лгать, чтобы сойтись.
//
// Следование одноуровневое и только за шимом: внутренности `shared/`, которые
// шим не называет, адресацией этого приложения не являются. Признак истекает
// сам — снимут шим, и его цель перестанет читаться.
const SHARED_SRC = join(SRC_DIR, "..", "..", "shared", "src");

function shimTarget(source: string): string | null {
  const code = source
    .replace(/\/\*[\s\S]*?\*\//g, "")
    .replace(/^\s*\/\/.*$/gm, "")
    .trim();
  const m = /^export \* from "@shared\/([A-Za-z0-9/_.-]+)";$/.exec(code);
  return m ? m[1] : null;
}

function resolveShared(spec: string): string | null {
  for (const ext of [".ts", ".tsx", "/index.ts", "/index.tsx"]) {
    const p = join(SHARED_SRC, spec + ext);
    if (existsSync(p)) return p;
  }
  return null;
}

describe("шапка client.ts перечисляет ровно те эндпоинты, которые адресует исходник", () => {
  const files = tsFiles(SRC_DIR);
  const clientSource = readFileSync(CLIENT_TS, "utf8");

  const behindShims: string[] = [];
  for (const f of files) {
    const spec = shimTarget(readFileSync(f, "utf8"));
    if (!spec) continue;
    const target = resolveShared(spec);
    if (target && !behindShims.includes(target)) behindShims.push(target);
  }

  const addressed = new Set<string>();
  for (const f of [...files, ...behindShims]) {
    for (const token of addressedIn(readFileSync(f, "utf8"), f)) addressed.add(token);
  }

  const declared = new Set(declaredIn(header(clientSource)));

  it("прочитан непустой объём: файлы, объявленные и адресуемые токены", () => {
    // Положительный контроль объёма — без него «множества совпали» неотличимо от
    // «оба пусты, потому что предикат ничего не нашёл».
    // Порог — защита от «прочитано ноль», а НЕ пин сегодняшнего размера дерева.
    // Прежние 100 были ровно текущим числом файлов и стали ложью в тот день,
    // когда сведение форков к прослойкам удалило из приложения 16 недостижимых
    // модулей: тест покраснел на УМЕНЬШЕНИИ копий, то есть на достижении своей
    // же цели. Порог отвязан от размера и держит только непустоту обхода.
    expect(files.length).toBeGreaterThan(50);
    expect(behindShims.length).toBeGreaterThan(0);
    expect(declared.size).toBeGreaterThan(5);
    expect(addressed.size).toBeGreaterThan(5);
    // eslint-disable-next-line no-console
    console.log(
      `[endpoints] файлов прочитано: ${files.length}; за шимами: ${behindShims.length}; объявлено: ${declared.size}; адресуется: ${addressed.size}`,
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

describe("разбор зависимостей: у каждой его ветви в реестре есть спека", () => {
  // Здесь стояло ОБРАТНОЕ утверждение — что у трёх ветвей разбора
  // (`networks`/`subnets`/`addresses`) спеки в реестре этого приложения НЕТ, и
  // потому до них не доходит ни один маршрут. Оно было верно, пока реестр был
  // копией на восемь записей, и само объявляло свой предикат снятия: «заведут в
  // compute спеку networks — покраснеет». Реестр сведён к единственному на всю
  // консоль (#406), спеки завелись все сразу, и оговорка истекла ровно так, как
  // обещала.
  //
  // Заменено не пустотой, а ЗЕРКАЛОМ: теперь утверждается, что ветвь разбора без
  // спеки — находка. Это ловит противоположный дефект: ветвь, которая строит
  // дерево зависимостей для вида ресурса, реестру неизвестного, отвечает пустым
  // деревом на каждый вызов, и удаление выглядит безопасным там, где оно не
  // проверено.
  const specIds = new Set(Object.keys(REGISTRY));
  const BRANCHES = ["networks", "subnets", "addresses", "network-interfaces"];

  it(`перепись: ветвей разбора ${BRANCHES.length}, записей реестра ${specIds.size}`, () => {
    // Пустой реестр сделал бы утверждение ниже красным по чужой причине, а
    // пустой перечень ветвей — вакуумно зелёным.
    expect(specIds.size).toBeGreaterThan(0);
    expect(BRANCHES.length).toBeGreaterThan(0);
  });

  it("своя предпосылка: перечень ветвей совпадает с тем, что признаёт разбор", () => {
    // Перечень выписан, поэтому обязан сверяться с исходником: ветвь,
    // добавленная в разбор и забытая здесь, осталась бы вне наблюдения.
    for (const id of BRANCHES) expect(hasDependencyResolver(id)).toBe(true);
    // Законный близнец: вид, ветви для которого нет, ею и не считается — иначе
    // предикат отвечал бы «да» на что угодно.
    expect(hasDependencyResolver("machine-types")).toBe(false);
  });

  it("ни одна ветвь разбора не осталась без спеки", () => {
    expect(BRANCHES.filter((id) => !specIds.has(id))).toEqual([]);
  });
});
