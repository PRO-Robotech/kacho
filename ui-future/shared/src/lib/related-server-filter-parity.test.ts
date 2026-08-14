// Серверное поле сужения, объявленное вкладкой дочернего ресурса, обязано быть
// полем, которое ВЛАДЕЛЕЦ ребёнка действительно принимает.
//
// Предмет. Ребро `related` может объявить `serverFilterField` — имя поля, которым
// список ребёнка сужается по родителю НА СЕРВЕРЕ. Консоль отправляет его
// выражением фильтра (`filter=<поле>="<id родителя>"`; форму запроса пиннит
// `ResourceShell.related.test.tsx`), а владелец разбирает это выражение по
// СОБСТВЕННОМУ белому списку полей. Список закрытый: поле вне него — не «фильтр
// без эффекта», а `InvalidArgument`. Значит объявление, разошедшееся со списком
// владельца, ломает вкладку целиком, и ломает молча: в консоли ничего не
// компилируется иначе, на ревью строка выглядит правдоподобно, а увидит это
// первым арендатор, у которого вкладка отвечает отказом.
//
// ИСТОЧНИК ИСТИНЫ — дерево сервиса, а не список в этом файле. Белые списки
// читаются из прод-кода владельца (`filter.Parse(f.Filter, []string{…})`), поэтому
// снятие поля из белого списка делает эту пробу красной без чьей-либо памяти.
//
// Проба несёт: (а) проверку СВОЕЙ предпосылки — не прочитав дерева и не найдя ни
// одного объявления, она падает, а не объявляет «ноль находок»; (б) контроль в
// обе стороны — законное поле обязано признаваться, правдоподобно-неверное
// обязано отвергаться; (в) перепись реестров, объявляющих такое поле: реестр,
// которого эта проба не читает, — находка, а не тишина.
//
// Что остаётся ВНЕ: типизированные поля списочного запроса владельца (напр. у
// адреса — `subnet_id`, отдельное поле запроса, а не элемент белого списка
// фильтра). Это ДРУГОЙ механизм сужения; ребро его не объявляет, и проба про него
// не утверждает ничего.

import { execFileSync } from "node:child_process";
import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import { basename, join, resolve } from "node:path";

import { stripComments } from "@shared/test/strip-comments";
import { REGISTRY } from "./resource-registry";

// cwd прогона — каталог приложения (ui-future/<app>): так запускают все девять.
const APP_DIR = process.cwd();
const REPO_ROOT = resolve(APP_DIR, "../..");
const UI_ROOT = resolve(APP_DIR, "..");

interface DeclaredEdge {
  parentId: string;
  childId: string;
  field: string;
}

const declared: DeclaredEdge[] = Object.values(REGISTRY).flatMap((parent) =>
  (parent.related ?? [])
    .filter((r) => !!r.serverFilterField)
    .map((r) => ({ parentId: parent.id, childId: r.childId, field: r.serverFilterField! })),
);

// ── белые списки владельца ───────────────────────────────────────────────────

function walk(dir: string, match: RegExp, out: string[] = []): string[] {
  for (const entry of readdirSync(dir)) {
    const p = join(dir, entry);
    if (statSync(p).isDirectory()) walk(p, match, out);
    else if (match.test(p)) out.push(p);
  }
  return out;
}

/** `filter.Parse(f.Filter, []string{"name", "network_id"})` → набор имён полей. */
const PARSE_CALL = /filter\.Parse\(\s*[A-Za-z_][\w.]*\s*,\s*\[\]string\{([^}]*)\}/g;

interface DomainRead {
  /** basename файла прод-кода → объединение его белых списков. */
  whitelists: Map<string, Set<string>>;
  filesRead: number;
}

const domainCache = new Map<string, DomainRead | null>();

/** Читает прод-дерево сервиса домена; `null` — каталога такого домена нет. */
function readDomain(domain: string): DomainRead | null {
  if (domainCache.has(domain)) return domainCache.get(domain)!;
  const dir = join(REPO_ROOT, "services", domain);
  if (!existsSync(dir)) {
    domainCache.set(domain, null);
    return null;
  }
  const files = walk(dir, /\.go$/).filter((f) => !f.endsWith("_test.go"));
  const whitelists = new Map<string, Set<string>>();
  for (const file of files) {
    const code = stripComments(readFileSync(file, "utf8"));
    for (const m of code.matchAll(PARSE_CALL)) {
      const fields = [...m[1].matchAll(/"([^"]+)"/g)].map((q) => q[1]);
      if (fields.length === 0) continue;
      const key = basename(file);
      const set = whitelists.get(key) ?? new Set<string>();
      for (const f of fields) set.add(f);
      whitelists.set(key, set);
    }
  }
  const read = { whitelists, filesRead: files.length };
  domainCache.set(domain, read);
  return read;
}

/** Домен ребёнка из его REST-пути: `/vpc/v1/subnets` → `vpc`. */
function domainOf(apiPath: string): string {
  return apiPath.split("/").filter(Boolean)[0] ?? "";
}

/**
 * Имена файлов владельца, где может лежать список этого ресурса: ключ полезной
 * нагрузки в единственном числе (`route_tables` → `route_table.go`). Правило
 * механическое; если оно не сойдётся с деревом, это находка, а не тишина.
 */
function ownerFileCandidates(payloadKey: string): string[] {
  const snake = payloadKey.replace(/([A-Z])/g, "_$1").toLowerCase();
  const bases = new Set<string>([snake]);
  if (snake.endsWith("es")) bases.add(snake.slice(0, -2));
  if (snake.endsWith("s")) bases.add(snake.slice(0, -1));
  return [...bases].map((b) => `${b}.go`);
}

interface Resolved {
  file: string;
  fields: Set<string>;
}

/** Белый список владельца ребёнка либо причина, по которой его не установить. */
function ownerWhitelist(childId: string): Resolved | { problem: string } {
  const spec = REGISTRY[childId];
  if (!spec) return { problem: `ребёнка «${childId}» нет в реестре` };
  const domain = domainOf(spec.apiPath);
  const read = readDomain(domain);
  if (!read) {
    return {
      problem:
        `каталога сервиса-владельца «services/${domain}» в дереве нет — ` +
        "проба не может установить, что владелец принимает; расширь её вместе с доменом",
    };
  }
  const candidates = ownerFileCandidates(spec.payloadKey).filter((f) => read.whitelists.has(f));
  if (candidates.length === 0) {
    return {
      problem:
        `в «services/${domain}» нет файла со списочным фильтром для «${spec.payloadKey}» ` +
        `(искали ${ownerFileCandidates(spec.payloadKey).join(", ")}); ` +
        "либо владелец не разбирает выражение фильтра вовсе, либо правило имени файла разошлось с деревом",
    };
  }
  if (candidates.length > 1) {
    return { problem: `владелец «${spec.payloadKey}» неоднозначен: ${candidates.join(", ")}` };
  }
  return { file: candidates[0], fields: read.whitelists.get(candidates[0])! };
}

/** Находка по одному ребру; `null` — владелец такое сужение принимает. */
function findingFor(edge: DeclaredEdge): string | null {
  const owner = ownerWhitelist(edge.childId);
  if ("problem" in owner) {
    return `${edge.parentId} → ${edge.childId}: ${owner.problem}`;
  }
  if (!owner.fields.has(edge.field)) {
    return (
      `${edge.parentId} → ${edge.childId}: объявлено серверное поле «${edge.field}», ` +
      `но белый список владельца (${owner.file}) принимает только ` +
      `${[...owner.fields].sort().join(", ")} — запрос вкладки вернёт InvalidArgument`
    );
  }
  return null;
}

// ── объём осмотренного ───────────────────────────────────────────────────────

describe("объём осмотренного — «ноль находок» отличимо от «ноль прочитанного»", () => {
  it("прочитано прод-дерево владельца, и белые списки в нём найдены", () => {
    const vpc = readDomain("vpc");
    expect(vpc).not.toBeNull();
    expect(vpc!.filesRead).toBeGreaterThan(100);
    expect(vpc!.whitelists.size).toBeGreaterThanOrEqual(4);
  });

  it("хотя бы одно ребро объявляет серверное поле — иначе перечень ниже пуст", () => {
    // Пустой перечень означал бы, что сужение по родителю снова целиком
    // клиентское: вкладка показывала бы первую страницу списка проекта как весь
    // список ребёнка. Это находка, а не «нечего проверять».
    expect(declared.length).toBeGreaterThanOrEqual(3);
  });

  it("перепись реестров: серверное поле объявляет только тот реестр, что читает эта проба", () => {
    // Приватные реестры (compute / nlb / registry / storage) эта проба не
    // импортирует — они не входят в её REGISTRY. Начнёт объявлять любой из них —
    // перепись покраснеет, и расширение пробы станет обязательным.
    const registries = execFileSync("git", ["ls-files", "*/src/lib/resource-registry.tsx"], {
      cwd: UI_ROOT,
      encoding: "utf8",
    })
      .split("\n")
      .filter(Boolean);
    expect(registries.length).toBeGreaterThanOrEqual(5);
    const declaring = registries.filter((rel) =>
      stripComments(readFileSync(join(UI_ROOT, rel), "utf8")).includes("serverFilterField"),
    );
    expect(declaring).toEqual(["shared/src/lib/resource-registry.tsx"]);
  });
});

// ── контроль в обе стороны ───────────────────────────────────────────────────

describe("сопоставитель различает — контроль в обе стороны", () => {
  it("законное поле признаётся", () => {
    expect(findingFor({ parentId: "networks", childId: "subnets", field: "network_id" })).toBeNull();
    // Тот же файл владельца, другое законное поле — иначе «признаёт» могло бы
    // означать «признаёт что угодно из одного знакомого имени».
    expect(findingFor({ parentId: "networks", childId: "subnets", field: "zone_id" })).toBeNull();
    expect(findingFor({ parentId: "subnets", childId: "addresses", field: "name" })).toBeNull();
  });

  it("правдоподобно-неверное отвергается — и отказ называет предмет", () => {
    // Ровно та ошибка, ради которой проба существует: адрес хранит подсеть внутри
    // jsonb, поэтому фильтром по имени колонки она не выражается, и белый список
    // владельца её не принимает. Объявление выглядело бы безупречно.
    const f = findingFor({ parentId: "subnets", childId: "addresses", field: "subnet_id" });
    expect(f).not.toBeNull();
    expect(f).toContain("subnet_id");
    expect(f).toContain("address.go");
  });

  it("ребёнок, чьего владельца не установить, — находка, а не тишина", () => {
    // Пустой белый список неотличим от непрочитанного файла, поэтому «владельца
    // нет» обязано быть отдельным исходом с названной причиной.
    expect(findingFor({ parentId: "networks", childId: "нет-такого", field: "network_id" })).toContain(
      "нет в реестре",
    );
  });
});

// ── сами объявления ──────────────────────────────────────────────────────────

describe("каждое объявленное серверное поле принимается владельцем ребёнка", () => {
  it.each(declared.map((e) => [`${e.parentId} → ${e.childId} (${e.field})`, e] as const))("%s", (_label, edge) => {
    expect(findingFor(edge) ?? "").toBe("");
  });
});
