// Каждый API-путь, который shared произносит в прод-коде, обязан принадлежать
// поверхности ствола.
//
// Путь, которого на поверхности нет, не даёт ошибки сборки и не виден на ревью:
// он существует как строка, доезжает до края и возвращает 404 — а страница,
// которая его зовёт, выглядит просто пустой. Именно так `/compute/v1/regions`
// прожил в поиске по системе рядом с реестром, который в том же пакете уже знал
// правильный адрес: два места об одном предмете, из которых верно одно.
//
// ИСТОЧНИК ИСТИНЫ — дерево, а не список в этом файле:
//   (1) http-аннотации `proto/**/*.proto` — 172 уникальных пути на ревизии,
//       где эта проба заведена;
//   (2) маршруты, которые край регистрирует САМ, мимо proto (`/iam/v1/auth/me`
//       регистрируется в gateway/internal/middleware напрямую). Их тоже читаем
//       из дерева, а не выписываем: выписанный список разошёлся бы с ним молча.
//
// Проба несёт проверку СВОЕЙ предпосылки: если proto/gateway/shared не читаются
// или читаются пусто, она падает, а не объявляет «ноль находок». И несёт
// контроль в обе стороны — законный путь обязан признаваться, выдуманный обязан
// отвергаться, иначе «все пути прошли» означало бы лишь, что сопоставитель
// согласен на всё.
//
// ЧТО ЧИТАЕТСЯ. Первая часть — цельные литералы. Её одной было НЕДОСТАТОЧНО, и
// это не мелочь охвата: путь, собранный из головы-подстановки
// (`${spec.apiPath}/…/operations`), для неё не существует вовсе — а именно там и
// сидел подмаршрут, которого ствол не подаёт. Вторая часть (ниже) читает
// составные выражения, резолвя голову тем же способом, что сосед
// `src/test/console-verb-routes-exist.test.ts`.
//
// ЧТО ОСТАЁТСЯ ВНЕ — названо, а не умолчано: выражение, чей ГЛАГОЛ подставляется
// переменной (`…/${id}:${verb}`). Имя действия статически неизвестно ни здесь,
// ни у соседа; такие места перечислены поимённо отдельным утверждением, и их
// число обязано меняться вместе с кодом.

import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, resolve } from "node:path";

import { parseSource } from "@shared/test/console-verb-literals";
import { stripComments } from "@shared/test/strip-comments";
import { REGISTRY } from "./resource-registry";

// cwd прогона — каталог приложения (ui-future/<app>), одинаково у всех девяти:
// их package.json запускает jest из своего каталога. Отсюда и относительные
// координаты дерева.
const APP_DIR = process.cwd();
const REPO_ROOT = resolve(APP_DIR, "../..");
const PROTO_DIR = join(REPO_ROOT, "proto");
const GATEWAY_DIR = join(REPO_ROOT, "gateway");
const SHARED_SRC = resolve(APP_DIR, "../shared/src");

/** Домены, чьи пути край обслуживает; всё остальное — SPA-роутер, не API. */
const API_PREFIX = /^\/(vpc|compute|iam|geo|nlb|storage|registry|loadbalancer)\/v1(\/|$)|^\/operations(\/|$)/;

function walk(dir: string, match: RegExp, out: string[] = []): string[] {
  for (const entry of readdirSync(dir)) {
    const p = join(dir, entry);
    if (statSync(p).isDirectory()) walk(p, match, out);
    else if (match.test(p)) out.push(p);
  }
  return out;
}

// `stripComments` (снятие комментариев с сохранением строковых литералов) живёт
// в `@shared/test/strip-comments`: тем же предикатом читают исходники и другие
// пробы (напр. белые списки фильтра в Go-дереве), а две редакции одного
// разборщика разошлись бы молча — оба ведь возвращают «текст» на любом входе.

/**
 * Нормализует путь к сегментам сопоставления: снимает query, а всякую
 * подстановку — proto `{network_id}`, шаблон реестра `{id}`, интерполяцию
 * `${...}` — сводит к `*`. Верб остаётся при сегменте: `{network_id}:internal`
 * → `*:internal`, поэтому глагольная форма не сходится со слэшевой.
 */
function segments(path: string): string[] {
  const noQuery = path.split("?")[0];
  return noQuery
    .split("/")
    .map((s) => s.replace(/\$\{[^}]*\}/g, "*").replace(/\{[^}]*\}/g, "*"))
    .filter((s, idx) => !(idx === 0 && s === ""));
}

/** proto-параметр `{repository=**}` съедает один сегмент или больше. */
const GREEDY = "**";

function protoSegments(path: string): string[] {
  return path
    .split("/")
    .map((s) => s.replace(/\{[^}]*=\*\*\}/g, GREEDY).replace(/\{[^}]*\}/g, "*"))
    .filter((s, idx) => !(idx === 0 && s === ""));
}

/**
 * Сегмент — это пара «база + верб»: `{network_id}:internal` → `*` + `internal`.
 * Верб сравнивается строго и отдельно, иначе параметр ствола проглотил бы любой
 * суффикс, и `…/{id}:no-such-verb` прошло бы как обычный детальный GET.
 */
function splitSeg(seg: string): { base: string; verb: string } {
  const i = seg.indexOf(":");
  return i < 0 ? { base: seg, verb: "" } : { base: seg.slice(0, i), verb: seg.slice(i + 1) };
}

function matches(actual: string[], surface: string[]): boolean {
  const walkFrom = (i: number, j: number): boolean => {
    if (j === surface.length) return i === actual.length;
    const want = splitSeg(surface[j]);
    if (want.base === GREEDY) {
      for (let k = i + 1; k <= actual.length; k++) {
        if (splitSeg(actual[k - 1]).verb !== want.verb) continue;
        if (walkFrom(k, j + 1)) return true;
      }
      return false;
    }
    if (i === actual.length) return false;
    const got = splitSeg(actual[i]);
    if (got.verb !== want.verb) return false;
    // База-параметр ствола принимает любую базу вызывающего; конкретная база —
    // только себя (после сведения подстановок к `*`).
    if (want.base !== "*" && want.base !== got.base) return false;
    return walkFrom(i + 1, j + 1);
  };
  return walkFrom(0, 0);
}

// ── Поверхность ствола ───────────────────────────────────────────────────────

const protoFiles = walk(PROTO_DIR, /\.proto$/);
const protoPaths = [
  ...new Set(
    protoFiles.flatMap((f) =>
      [...readFileSync(f, "utf8").matchAll(/(?:get|post|patch|put|delete):\s*"([^"]+)"/g)].map((m) => m[1]),
    ),
  ),
];

// Край регистрирует часть маршрутов сам, вне proto. Читаем их из его исходников
// тем же способом, иначе живой `/iam/v1/auth/me` попал бы в находки ложно.
const gatewayFiles = walk(GATEWAY_DIR, /\.go$/).filter((f) => !f.endsWith("_test.go"));
const gatewayNative = [
  ...new Set(
    gatewayFiles.flatMap((f) =>
      [...readFileSync(f, "utf8").matchAll(/\.(?:HandleFunc|Handle)\(\s*"([^"]+)"/g)].map((m) => m[1]),
    ),
  ),
].filter((p) => API_PREFIX.test(p));

const SURFACE = [...protoPaths, ...gatewayNative].map(protoSegments);

function belongs(path: string): boolean {
  const segs = segments(path);
  return SURFACE.some((s) => matches(segs, s));
}

// ── Что произносит shared ────────────────────────────────────────────────────

const sharedFiles = walk(SHARED_SRC, /\.(ts|tsx)$/).filter(
  (f) => !/\.test\.(ts|tsx)$/.test(f) && !f.includes(`${join("src", "test")}`),
);

const spoken = new Map<string, string[]>();
for (const file of sharedFiles) {
  const code = stripComments(readFileSync(file, "utf8"));
  for (const m of code.matchAll(/["'`]([^"'`\n]*)["'`]/g)) {
    const lit = m[1];
    if (!API_PREFIX.test(lit)) continue;
    if (!spoken.has(lit)) spoken.set(lit, []);
    const seen = spoken.get(lit)!;
    const rel = file.slice(SHARED_SRC.length + 1);
    if (!seen.includes(rel)) seen.push(rel);
  }
}

describe("объём осмотренного — «ноль находок» отличимо от «ноль прочитанного»", () => {
  it("прочитал дерево proto и получил из него поверхность", () => {
    expect(protoFiles.length).toBeGreaterThan(100);
    expect(protoPaths.length).toBeGreaterThan(150);
  });

  it("прочитал исходники края и нашёл маршруты, которые он регистрирует сам", () => {
    expect(gatewayFiles.length).toBeGreaterThan(10);
    // Ровно один такой маршрут лежит под доменным префиксом (`/iam/v1/auth/me`);
    // `/healthz`, `/readyz`, `/oauth/logout` под предикат API не попадают вовсе.
    expect(gatewayNative.length).toBeGreaterThanOrEqual(1);
  });

  it("прочитал прод-исходники shared и нашёл в них API-пути", () => {
    expect(sharedFiles.length).toBeGreaterThan(150);
    expect(spoken.size).toBeGreaterThan(40);
  });
});

describe("сопоставитель различает — контроль в обе стороны", () => {
  it("признаёт законные формы: список, детальный, вложенный, глагольный", () => {
    expect(belongs("/vpc/v1/subnets")).toBe(true);
    expect(belongs("/vpc/v1/addresses/${id}")).toBe(true);
    expect(belongs("/vpc/v1/addressPools/${poolId}/utilization?pageSize=200")).toBe(true);
    expect(belongs("/vpc/v1/networks/${id}:add-cidr-blocks")).toBe(true);
    expect(belongs("/vpc/v1/networks/{network_id}:internal")).toBe(true);
    expect(belongs("/iam/v1/auth/me")).toBe(true);
  });

  it("отвергает выдуманное, а не соглашается на всё", () => {
    expect(belongs("/vpc/v1/nope")).toBe(false);
    // Каталог размещения: живой адрес принадлежит geo, а тот, что раньше стоял в
    // поиске по системе, не существовал никогда — в proto compute нет ни одного
    // сообщения Region/Zone. Пара утверждений держит именно этот случай, а не
    // «какой-то путь вообще».
    expect(belongs("/geo/v1/regions")).toBe(true);
    expect(belongs("/geo/v1/zones")).toBe(true);
    expect(belongs("/compute/v1/regions")).toBe(false);
    expect(belongs("/compute/v1/zones")).toBe(false);
    expect(belongs("/vpc/v1/networks/${id}:no-such-verb")).toBe(false);
    // Слэшевая форма там, где ствол несёт глагольную, — разные пути.
    expect(belongs("/vpc/v1/networks/${id}/internal")).toBe(false);
    // Регистр сегмента значим: край не нормализует kebab-case в camelCase.
    expect(belongs("/vpc/v1/route-tables")).toBe(false);
  });
});

describe("shared не адресует ничего вне поверхности ствола", () => {
  const cases = [...spoken.entries()].sort(([a], [b]) => a.localeCompare(b));

  it.each(cases)("%s", (path, files) => {
    expect(belongs(path) ? "" : `${path} ← ${files.join(", ")}`).toBe("");
  });
});

// ── Составные выражения пути ─────────────────────────────────────────────────
//
// Всё выше читает ЦЕЛЬНЫЕ литералы. Их недостаточно, и это не мелочь охвата:
// путь, собранный из головы-подстановки (`${spec.apiPath}/…`), для этой части
// пробы не существует вовсе — а именно так консоль строит вложенные подмаршруты
// generic-компонентов, где имя ресурса неизвестно на месте написания.
//
// Голова резолвится ТЕМ ЖЕ способом, что у соседа
// (`src/test/console-verb-routes-exist.test.ts`): строковая константа, запись
// объектной карты путей, стрелка-константа, псевдоним записи реестра. Свободное
// `<что-то>.apiPath` (проп generic-компонента) разворачивается по ВСЕМ apiPath
// реестра — компонент обещает этот путь для КАЖДОЙ спеки, которую ему могут
// передать, и обещание проверяется для каждой.

const registryApiPaths = [...new Set(Object.values(REGISTRY).map((s) => s.apiPath))].sort();

/** `const X = { a: "/p" } as const;` — карты путей (`IAM.users`, `CLUSTER.admins`). */
const OBJECT_CONST = /(?:^|\n)\s*(?:export\s+)?const\s+([A-Za-z_$][\w$]*)\s*=\s*\{([^}]*)\}\s*as const;/g;
const OBJECT_ENTRY = /([A-Za-z_$][\w$]*)\s*:\s*"([^"]*)"/g;
/** `const X = "/p";` */
const STRING_CONST = /(?:^|\n)\s*(?:export\s+)?const\s+([A-Za-z_$][\w$]*)\s*(?::[^=]*)?=\s*"([^"]*)"\s*;/g;
/** `const X = (a: T) => `/p/${a}/q`;` — константа-построитель пути. */
const ARROW_CONST = /(?:^|\n)\s*(?:export\s+)?const\s+([A-Za-z_$][\w$]*)\s*(?::[^=]*)?=\s*\([^)]*\)\s*=>\s*`([^`]*)`/g;
/** `const spec = REGISTRY["subnets"];` / `REGISTRY.subnets` — псевдоним спеки. */
const REGISTRY_ALIAS =
  /(?:^|\n)\s*(?:export\s+)?const\s+([A-Za-z_$][\w$]*)\s*=\s*REGISTRY(?:\[\s*"([^"]+)"\s*\]|\.([A-Za-z_$][\w$]*))\s*;/g;

const objectConsts = new Map<string, string>();
for (const file of sharedFiles) {
  const src = readFileSync(file, "utf8");
  for (const o of src.matchAll(OBJECT_CONST)) {
    for (const e of o[2].matchAll(OBJECT_ENTRY)) objectConsts.set(`${o[1]}.${e[1]}`, e[2]);
  }
}

interface Composite {
  file: string;
  literal: string;
  /** Пути, которые это выражение может произнести (для generic-спеки — все). */
  candidates: string[];
}

/** Головы, которые резолвятся В ОДИН путь; `null` — «это не голова API-пути». */
function headOf(
  expr: string,
  strings: Map<string, string>,
  arrows: Map<string, string>,
  aliases: Map<string, string>,
): string[] | null {
  const e = expr.trim();
  // `REGISTRY["subnets"].apiPath` / `REGISTRY.subnets.apiPath` — точная спека.
  const direct = /^REGISTRY(?:\[\s*"([^"]+)"\s*\]|\.([A-Za-z_$][\w$]*))\s*\.apiPath$/.exec(e);
  if (direct) {
    const spec = REGISTRY[direct[1] ?? direct[2]];
    return spec ? [spec.apiPath] : null;
  }
  // `spec.apiPath` / `spec!.apiPath` / `spec?.apiPath`.
  const dotApi = /^([A-Za-z_$][\w$]*)\s*[!?]?\.?\s*[!?]?\.apiPath$/.exec(e);
  if (dotApi) {
    const alias = aliases.get(dotApi[1]);
    if (alias !== undefined) {
      const spec = REGISTRY[alias];
      return spec ? [spec.apiPath] : null;
    }
    // Свободная переменная — generic-компонент получает ЛЮБУЮ спеку реестра.
    return registryApiPaths;
  }
  // `FN(arg)` — константа-построитель пути.
  const call = /^([A-Za-z_$][\w$]*)\s*\(/.exec(e);
  if (call) {
    const body = arrows.get(call[1]);
    return body?.startsWith("/") ? [body] : null;
  }
  // `OBJ.key` — запись карты путей.
  if (objectConsts.has(e)) return [objectConsts.get(e)!];
  // Одиночная строковая константа.
  const str = strings.get(e);
  if (str?.startsWith("/")) return [str];
  return null;
}

const composites: Composite[] = [];
/** Хвост несёт подставляемый ГЛАГОЛ — какой именно, статически неизвестно. */
const dynamicVerb: Composite[] = [];
let compositeLiteralsSeen = 0;
/** Литералов РАЗОБРАНО всего — объём осмотренного самой переписи. */
let literalsParsed = 0;
/** Литералы с подстановкой, как их прочла перепись — для проверки целостности. */
const parsedWithSubstitution: { file: string; literal: string }[] = [];

for (const file of sharedFiles) {
  const raw = readFileSync(file, "utf8");
  const strings = new Map<string, string>();
  for (const m of raw.matchAll(STRING_CONST)) strings.set(m[1], m[2]);
  const arrows = new Map<string, string>();
  for (const m of raw.matchAll(ARROW_CONST)) arrows.set(m[1], m[2]);
  const aliases = new Map<string, string>();
  for (const m of raw.matchAll(REGISTRY_ALIAS)) aliases.set(m[1], m[2] ?? m[3]);

  // ЛИТЕРАЛЫ ЧИТАЕТ РАЗБОР, А НЕ РЕГЕКСП (#568). Прежде здесь стояло
  // `code.matchAll(/`([^`\n]*)`/g)` по снятому от комментариев тексту, и класс
  // «#559: парность обратных кавычек» этим закрыт НЕ БЫЛ: шаблон, содержащий
  // ВЛОЖЕННЫЙ шаблон внутри подстановки, рвёт пару на первой же внутренней
  // кавычке. Замер по дереву дал восемь мест сразу — пять литералов перепись не
  // видела вовсе, а ещё три ВЫДУМЫВАЛА: обрубок вида «${o.name || o.uid}${o.extra ? »
  // считался литералом и попадал в счёт. Обрубок безобиден лишь до тех пор, пока
  // его голова не резолвится в путь; резолвится — и в поверхность уезжает
  // склейка, которой в коде нет.
  //
  // `parseSource` читает синтаксическое дерево и восстанавливает шаблон вместе с
  // подстановками, поэтому закрывает разом и вложенность, и границу строки
  // (многострочных шаблонов в этом корпусе сегодня ноль — предикат остаётся
  // верным и когда они появятся). Третьей редакции разборщика здесь не заводится:
  // он один на все пробы, читающие исходный текст.
  for (const literal of parseSource(file, raw).literals) {
    literalsParsed++;
    if (literal.includes("${")) {
      parsedWithSubstitution.push({ file: file.slice(SHARED_SRC.length + 1), literal });
    }
    const head = /^\$\{([^}]*)\}/.exec(literal);
    if (!head) continue; // цельный литерал — его читает часть выше
    compositeLiteralsSeen++;
    const bases = headOf(head[1], strings, arrows, aliases);
    if (bases === null) continue; // не API-путь (маршрут SPA, вёрстка, URL окна)
    const tail = literal.slice(head[0].length);
    const candidates = bases.map((b) => b + tail).filter((p) => API_PREFIX.test(p));
    if (candidates.length === 0) continue;
    const rel = file.slice(SHARED_SRC.length + 1);
    const entry: Composite = { file: rel, literal, candidates };
    // Глагол, собираемый на месте (`:${verb}`), этой пробе не разрешить: имя
    // действия — значение переменной. Такие места выносим в объявленный остаток,
    // а не считаем проверенными.
    if (/:\$\{/.test(tail)) dynamicVerb.push(entry);
    else composites.push(entry);
  }
}

describe("составные пути — объём осмотренного", () => {
  it("реестр прочитан, головы резолвятся, остаток объявлен числом", () => {
    expect(registryApiPaths.length).toBeGreaterThan(20);
    expect(objectConsts.size).toBeGreaterThan(5);
    // Шаблонных литералов с подстановкой в голове — столько; из них API-путём
    // оказываются те, чья голова резолвится в путь. Остальное — маршруты SPA и
    // вёрстка, и это НЕ «ноль находок»: они прочитаны и классифицированы.
    expect(compositeLiteralsSeen).toBeGreaterThan(40);
    expect(composites.length).toBeGreaterThan(10);
    // Литералов РАЗОБРАНО — отдельное число, и оно заведомо больше числа
    // составных: цельные строки тоже читаются. Без него «ноль составных» было бы
    // неотличимо от «разборщик не отработал», а именно так выглядел бы возврат к
    // регекспу, рвущему пару на вложенном шаблоне.
    expect(literalsParsed).toBeGreaterThan(compositeLiteralsSeen);
    expect(literalsParsed).toBeGreaterThan(1000);
  });

  it("остаток — только выражения с подставляемым глаголом, и он назван", () => {
    // Их проверяет не эта проба: имя действия статически неизвестно ни здесь, ни
    // у соседа (`console-verb-routes-exist`), который такую форму тоже пропускает.
    // Число обязано меняться вместе с кодом — молчаливый рост остатка виден здесь.
    // Вид секции CIDR общий (`CidrTableSection`), но ПУТЬ строит владелец
    // ресурса — иначе голова литерала стала бы пропом, статически не
    // резолвимым, и оба ресурса ушли бы из-под наблюдения этой пробы вовсе.
    // У сети и у набора префиксов таких мест по два: секции v4 и v6 адресуются
    // каждая своей.
    expect(dynamicVerb.map((c) => `${c.file} ${c.literal}`).sort()).toEqual([
      "components/organisms/AddressPoolCidrManager/AddressPoolCidrManager.tsx " +
        "${POOLS_API}/${poolId}:${params.verb}CidrBlocks",
      "components/organisms/CidrGroupBlocksManager/CidrGroupBlocksManager.tsx " +
        "${CIDR_GROUPS_API}/${cidrGroupId}:${verb}-cidr-blocks",
      "components/organisms/CidrGroupBlocksManager/CidrGroupBlocksManager.tsx " +
        "${CIDR_GROUPS_API}/${cidrGroupId}:${verb}-cidr-blocks",
      "components/organisms/NetworkCidrManager/NetworkCidrManager.tsx " +
        "${NETWORKS_API}/${networkId}:${verb}-cidr-blocks",
      "components/organisms/NetworkCidrManager/NetworkCidrManager.tsx " +
        "${NETWORKS_API}/${networkId}:${verb}-cidr-blocks",
      "components/organisms/ResourceDetailPage/ResourceDetailPage.tsx ${spec.apiPath}/${uid}:${verb}",
      "components/organisms/SubnetCidrManager/SubnetCidrManager.tsx " +
        "${SUBNETS_API}/${subnetId}:${verb}-cidr-blocks",
    ]);
  });
});

describe("перепись литералов — контроль в обе стороны (#568)", () => {
  // Судья тот же, что читает дерево: `parseSource`. Проба, подменившая его своим
  // разбором, доказала бы свойство своей копии, а не переписи.
  const literalsOf = (src: string): string[] => parseSource("probe.ts", src).literals;

  it("МНОГОСТРОЧНЫЙ шаблон с путём — виден", () => {
    // Форма, на которой прежний однострочный регексп слеп by construction:
    // `[^`\n]` обрывает совпадение на переводе строки. В сегодняшнем корпусе
    // таких литералов ноль, поэтому свойство держится этой пробой, а не деревом:
    // prettier переносит длинные шаблоны, и первый же перенесённый путь ушёл бы
    // из-под надзора молча.
    const src = "const p = `${IAM.users}/${encodeURIComponent(id)}\n  :block`;\n";
    expect(literalsOf(src)).toContain("${IAM.users}/${encodeURIComponent(id)}\n  :block");
  });

  it("ВЛОЖЕННЫЙ шаблон читается целиком, а не обрубком", () => {
    // Это живой механизм, а не умозрительный: в дереве он давал восемь мест —
    // пять литералов не читались, три читались обрубками. Обрубок опаснее
    // пропуска: он попадает в счёт и делает «перепись прочитала N» неправдой.
    const src = 'const s = `${o.name}${o.extra ? ` · ${o.extra}` : ""}`;\n';
    const lits = literalsOf(src);
    expect(lits).toContain('${o.name}${o.extra ? ` · ${o.extra}` : ""}');
    // Обрубка, который производил регексп, среди литералов быть не должно.
    expect(lits).not.toContain("${o.name}${o.extra ? ");
  });

  it("ни один литерал ДЕРЕВА не оборван внутри подстановки", () => {
    // Эта проба привязывает МЕСТО, а не ответ. Три предыдущие спрашивают
    // `parseSource` напрямую и остались бы зелёными, верни кто-нибудь перепись на
    // регексп: они закрепляют, что разборщик умеет, а не что им пользуются.
    //
    // Здесь проверяется свойство того, что перепись ПРОЧЛА. Обрубок регекспа
    // всегда обрывается внутри незакрытой подстановки — после последней `}`
    // остаётся ещё одна `${`. Настоящий разбор такого произвести не может.
    //
    // Проба не падает на достижении своей цели: дерево без вложенных шаблонов
    // проходит её пусто, а не краснеет.
    const truncated = parsedWithSubstitution.filter(({ literal }) => {
      const lastClose = literal.lastIndexOf("}");
      const tail = lastClose === -1 ? literal : literal.slice(lastClose + 1);
      return tail.includes("${");
    });
    expect(truncated.map((t) => `${t.file}  ${JSON.stringify(t.literal)}`)).toEqual([]);
    expect(parsedWithSubstitution.length).toBeGreaterThan(compositeLiteralsSeen);
  });

  it("путь в КОММЕНТАРИИ литералом не становится", () => {
    // Обратная сторона: разбор обязан читать исполняемую часть. Комментарий,
    // называющий снятый маршрут, — это объяснение, а не вызов; находка на нём
    // запрещала бы объяснять.
    const src = '// был `/iam/v1/users/${id}:listByResource`\nconst p = "/iam/v1/users";\n';
    const lits = literalsOf(src);
    expect(lits).toContain("/iam/v1/users");
    expect(lits.some((l) => l.includes("listByResource"))).toBe(false);
  });
});

describe("резолвер головы различает — контроль в обе стороны", () => {
  const strings = new Map([["LOCAL", "/vpc/v1/networks"]]);
  const arrows = new Map([["BUILD", "/iam/v1/users/${id}/tokens"]]);
  const aliases = new Map([["sgSpec", "security-groups"]]);

  it("резолвит все четыре формы головы", () => {
    expect(headOf("LOCAL", strings, arrows, aliases)).toEqual(["/vpc/v1/networks"]);
    expect(headOf("BUILD(x)", strings, arrows, aliases)).toEqual(["/iam/v1/users/${id}/tokens"]);
    expect(headOf("sgSpec.apiPath", strings, arrows, aliases)).toEqual(["/vpc/v1/securityGroups"]);
    expect(headOf("IAM.users", strings, arrows, aliases)).toEqual(["/iam/v1/users"]);
    expect(headOf('REGISTRY["subnets"].apiPath', strings, arrows, aliases)).toEqual(["/vpc/v1/subnets"]);
  });

  it("свободное `.apiPath` разворачивается по всему реестру, а не по одной спеке", () => {
    expect(headOf("spec.apiPath", strings, arrows, aliases)).toEqual(registryApiPaths);
    expect(registryApiPaths.length).toBeGreaterThan(1);
  });

  it("не выдаёт за API-путь то, что им не является", () => {
    expect(headOf("basePath", strings, arrows, aliases)).toBeNull();
    expect(headOf("window.location.protocol", strings, arrows, aliases)).toBeNull();
    expect(headOf("detailBase", strings, arrows, aliases)).toBeNull();
  });
});

describe("составной путь shared принадлежит поверхности ствола", () => {
  const cases = composites
    .flatMap((c) => c.candidates.map((p) => [`${c.file} ${c.literal} → ${p}`, p] as const))
    .sort(([a], [b]) => a.localeCompare(b));

  it("их не ноль — иначе утверждение ниже пустое", () => {
    expect(cases.length).toBeGreaterThan(100);
  });

  it.each(cases)("%s", (label, path) => {
    expect(belongs(path) ? "" : label).toBe("");
  });
});
