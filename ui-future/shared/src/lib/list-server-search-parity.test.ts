// Право спросить сервер о поиске обязано сходиться с деревом ВЛАДЕЛЬЦА (#373).
//
// ПРЕДМЕТ. Спека может объявить `serverSearchField` — тогда строка поиска уходит
// запросом `filter=<поле> CONTAINS "…"` и спрашивает про ВЕСЬ список, а не про
// прочитанные страницы. Объявление верно, только когда владелец удовлетворяет
// ДВУМ условиям, и второе из первого не следует:
//
//   1. поле стоит в его белом списке выражения — иначе страница целиком получает
//      `InvalidArgument`. Это видно быстро и громко;
//   2. владелец применяет разобранный узел через `ToSQL`, а не забирает из него
//      одно ЗНАЧЕНИЕ. Забирающий значение теряет ОПЕРАТОР: подстрока молча
//      становится точным равенством, поиск отвечает «ничего не найдено» на любой
//      неполный ввод — то есть ровно тем, ради устранения чего он и уехал на
//      сервер. Это не видно ни в консоли, ни на ревью: обе стороны выглядят
//      исправно, а расходятся они в третьем месте.
//
// Второе условие — не теория. На день заведения этой пробы белый список с `name`
// несли 16 файлов владельцев, а применяли узел через `ToSQL` — 10; шесть
// оставшихся (nlb, storage, registry, iam) забирали значение. Эти шестеро
// починены в #460 — они применяют оператор либо отвергают неподдерживаемый явно,
// — поэтому число выше историческое и как описание дерева больше не читается.
// Действующую перепись даёт `internal/repohygiene`
// `TestFilterOwnerHonoursTheParsedOperator`: он обходит дерево и печатает объём
// осмотренного. Условие же остаётся в силе — следующий владелец заведётся так же.
//
// ИСТОЧНИК ИСТИНЫ — дерево сервиса, а не список в этом файле: и белый список, и
// применение узла читаются из прод-кода владельца, поэтому смена любого из них
// роняет пробу без чьей-либо памяти.
//
// Проба несёт: (а) проверку СВОЕЙ предпосылки — не прочитав дерева, она падает,
// а не объявляет «ноль находок»; (б) контроль в обе стороны, причём отрицаний
// два — «нет белого списка» и «список есть, оператор теряется»; (в) перепись
// реестров: реестр, который эта проба не читает, но объявляет поле, — находка.

import { execFileSync } from "node:child_process";
import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import { basename, join, resolve } from "node:path";

import { stripComments } from "@shared/test/strip-comments";
import { REGISTRY } from "./resource-registry";

const APP_DIR = process.cwd();
const REPO_ROOT = resolve(APP_DIR, "../..");
const UI_ROOT = resolve(APP_DIR, "..");

/** `filter.Parse(f.Filter, []string{"name", "zone_id"})` — в т.ч. с переносом строки. */
const PARSE_CALL = /filter\.Parse\(\s*[A-Za-z_][\w.]*\s*,\s*\n?\s*\[\]string\{([^}]*)\}/g;
/** Применение разобранного узла — то, что сохраняет ОПЕРАТОР. */
const TOSQL_CALL = /\.ToSQL\(/;

interface DomainRead {
  /** basename файла → белый список полей выражения. */
  whitelists: Map<string, Set<string>>;
  /** basename файла → применяет ли он узел через ToSQL. */
  appliesNode: Map<string, boolean>;
  filesRead: number;
}

function walk(dir: string, match: RegExp, out: string[] = []): string[] {
  for (const entry of readdirSync(dir)) {
    const p = join(dir, entry);
    if (statSync(p).isDirectory()) walk(p, match, out);
    else if (match.test(p)) out.push(p);
  }
  return out;
}

const domainCache = new Map<string, DomainRead | null>();

function readDomain(domain: string): DomainRead | null {
  if (domainCache.has(domain)) return domainCache.get(domain)!;
  const dir = join(REPO_ROOT, "services", domain);
  if (!existsSync(dir)) {
    domainCache.set(domain, null);
    return null;
  }
  const files = walk(dir, /\.go$/).filter((f) => !f.endsWith("_test.go"));
  const whitelists = new Map<string, Set<string>>();
  const appliesNode = new Map<string, boolean>();
  for (const file of files) {
    const code = stripComments(readFileSync(file, "utf8"));
    const fields = [...code.matchAll(PARSE_CALL)].flatMap((m) => [...m[1].matchAll(/"([^"]+)"/g)].map((q) => q[1]));
    if (fields.length === 0) continue;
    const key = basename(file);
    const set = whitelists.get(key) ?? new Set<string>();
    for (const f of fields) set.add(f);
    whitelists.set(key, set);
    appliesNode.set(key, appliesNode.get(key) === true || TOSQL_CALL.test(code));
  }
  const read = { whitelists, appliesNode, filesRead: files.length };
  domainCache.set(domain, read);
  return read;
}

/** Домен из REST-пути: `/vpc/v1/subnets` → `vpc`. */
function domainOf(apiPath: string): string {
  return apiPath.split("/").filter(Boolean)[0] ?? "";
}

/**
 * Имена файлов владельца, где может лежать списочный фильтр этого ресурса.
 *
 * Правило механическое: ключ полезной нагрузки в единственном числе, с суффиксом
 * `_repo` и без него (владельцы держат список то в файле ресурса — `network.go`,
 * то в файле репозитория — `instance_repo.go`). Не сойдётся с деревом — это
 * находка с названной причиной, а не тишина.
 */
function ownerFileCandidates(payloadKey: string): string[] {
  const snake = payloadKey.replace(/([A-Z])/g, "_$1").toLowerCase();
  const bases = new Set<string>([snake]);
  if (snake.endsWith("ies")) bases.add(snake.slice(0, -3) + "y"); // registries → registry
  if (snake.endsWith("es")) bases.add(snake.slice(0, -2)); // addresses → address
  if (snake.endsWith("s")) bases.add(snake.slice(0, -1)); // networks → network
  return [...bases].flatMap((b) => [`${b}.go`, `${b}_repo.go`]);
}

/** Находка либо `null` — владелец такой поиск принимает И сохраняет оператор. */
function findingFor(domain: string, payloadKey: string, field: string): string | null {
  const read = readDomain(domain);
  if (!read) {
    return `${domain}/${payloadKey}: каталога «services/${domain}» в дереве нет — проба не может установить, что владелец принимает`;
  }
  const candidates = ownerFileCandidates(payloadKey).filter((f) => read.whitelists.has(f));
  if (candidates.length === 0) {
    return (
      `${domain}/${payloadKey}: в «services/${domain}» нет файла со списочным фильтром ` +
      `(искали ${ownerFileCandidates(payloadKey).join(", ")}) — владелец либо не разбирает выражение вовсе ` +
      `(тогда оно будет ПРОИГНОРИРОВАНО, и список вернётся полным под видом отфильтрованного), ` +
      `либо правило имени файла разошлось с деревом`
    );
  }
  if (candidates.length > 1) {
    return `${domain}/${payloadKey}: владелец неоднозначен: ${candidates.join(", ")}`;
  }
  const file = candidates[0];
  const fields = read.whitelists.get(file)!;
  if (!fields.has(field)) {
    return (
      `${domain}/${payloadKey}: объявлено поле поиска «${field}», но белый список владельца (${file}) ` +
      `принимает только ${[...fields].sort().join(", ")} — запрос вернёт InvalidArgument на всю страницу`
    );
  }
  if (read.appliesNode.get(file) !== true) {
    return (
      `${domain}/${payloadKey}: владелец (${file}) разбирает выражение, но НЕ применяет узел через ToSQL — ` +
      `он забирает из него одно значение и теряет ОПЕРАТОР: подстрока молча станет точным равенством, ` +
      `и поиск будет отвечать «ничего не найдено» на любой неполный ввод`
    );
  }
  return null;
}

interface Declared {
  specId: string;
  domain: string;
  payloadKey: string;
  field: string;
}

const declared: Declared[] = Object.values(REGISTRY)
  .filter((s) => !!s.serverSearchField)
  .map((s) => ({
    specId: s.id,
    domain: domainOf(s.apiPath),
    payloadKey: s.payloadKey,
    field: s.serverSearchField!,
  }));

// ── объём осмотренного ───────────────────────────────────────────────────────

describe("объём осмотренного — «ноль находок» отличимо от «ноль прочитанного»", () => {
  it("прочитаны прод-деревья обоих владельцев, и разбор выражения в них найден", () => {
    for (const d of ["vpc", "compute"]) {
      const read = readDomain(d);
      expect(read).not.toBeNull();
      expect(read!.filesRead).toBeGreaterThan(50);
      expect(read!.whitelists.size).toBeGreaterThanOrEqual(3);
    }
  });

  it("хотя бы один список умеет искать на сервере", () => {
    // Пустой перечень означал бы, что поиск снова целиком клиентский: страница
    // отвечала бы «ничего не найдено» про ресурс, который существует. Это
    // находка, а не «нечего проверять».
    expect(declared.length).toBeGreaterThanOrEqual(5);
  });

  it("перепись реестров: поле поиска объявляет только тот реестр, что читает эта проба", () => {
    const registries = execFileSync("git", ["ls-files", "*/src/lib/resource-registry.tsx"], {
      cwd: UI_ROOT,
      encoding: "utf8",
    })
      .split("\n")
      .filter(Boolean);
    expect(registries.length).toBeGreaterThanOrEqual(5);
    const declaring = registries.filter((rel) =>
      stripComments(readFileSync(join(UI_ROOT, rel), "utf8")).includes("serverSearchField"),
    );
    expect(declaring).toEqual(["shared/src/lib/resource-registry.tsx"]);
  });
});

// ── контроль в обе стороны ───────────────────────────────────────────────────

describe("сопоставитель различает — контроль в обе стороны", () => {
  it("законное объявление признаётся", () => {
    expect(findingFor("vpc", "networks", "name")).toBeNull();
    // Другой домен и другая форма имени файла (`instance_repo.go`) — иначе
    // «признаёт» могло бы означать «признаёт единственную знакомую форму».
    expect(findingFor("compute", "instances", "name")).toBeNull();
    // Другое законное поле того же владельца.
    expect(findingFor("vpc", "subnets", "zone_id")).toBeNull();
  });

  // Отрицательные случаи — на СИНТЕТИКЕ, а не на живых владельцах.
  //
  // Прежняя редакция брала в качестве отрицаний реальные сервисы: registry и
  // storage «теряют оператор», iam «не разбирает вовсе». Это работало ровно до
  // того дня, когда их починили (#460): фикстура была привязана к предмету
  // запрета и истекла ВМЕСТЕ С НИМ, унеся доказательство, что сопоставитель
  // вообще способен отвергнуть. Проба не имеет права краснеть на достижении
  // своей цели, поэтому вход теперь конструируется здесь и живёт независимо от
  // того, что творится в дереве владельцев.
  //
  // Живое дерево при этом не перестаёт проверяться — этим занят блок
  // «каждое объявленное поле поиска…» ниже: он читает НАСТОЯЩИХ владельцев.
  const withSyntheticDomain = (name: string, read: DomainRead, body: () => void) => {
    domainCache.set(name, read);
    try {
      body();
    } finally {
      domainCache.delete(name);
    }
  };

  it("владелец, теряющий ОПЕРАТОР, отвергается — и отказ называет предмет", () => {
    // Белый список с `name` ЕСТЬ, объявление выглядело бы безупречно, а поиск
    // отвечал бы точным равенством.
    withSyntheticDomain(
      "синтетика-без-оператора",
      {
        whitelists: new Map([["thing.go", new Set(["name"])]]),
        appliesNode: new Map([["thing.go", false]]),
        filesRead: 99,
      },
      () => {
        const f = findingFor("синтетика-без-оператора", "things", "name");
        expect(f).not.toBeNull();
        expect(f).toContain("ToSQL");
        expect(f).toContain("ОПЕРАТОР");
      },
    );
  });

  it("тот же владелец, научившийся применять узел, принимается", () => {
    // Парный положительный контроль к предыдущему: без него «отвергается» было
    // бы неотличимо от «отвергается всегда», и проба зеленела бы на сломанном
    // сопоставителе.
    withSyntheticDomain(
      "синтетика-с-оператором",
      {
        whitelists: new Map([["thing.go", new Set(["name"])]]),
        appliesNode: new Map([["thing.go", true]]),
        filesRead: 99,
      },
      () => {
        expect(findingFor("синтетика-с-оператором", "things", "name")).toBeNull();
      },
    );
  });

  it("владелец, не разбирающий выражение вовсе, отвергается отдельной причиной", () => {
    // «Проигнорируют» и «отвергнут» — разные исходы, и причина обязана их
    // различать: первый тише и опаснее.
    withSyntheticDomain(
      "синтетика-без-разбора",
      { whitelists: new Map(), appliesNode: new Map(), filesRead: 99 },
      () => {
        const f = findingFor("синтетика-без-разбора", "things", "name");
        expect(f).not.toBeNull();
        expect(f).toContain("ПРОИГНОРИРОВАНО");
      },
    );
  });

  it("поле вне белого списка отвергается", () => {
    const f = findingFor("vpc", "networks", "description");
    expect(f).toContain("InvalidArgument");
  });

  it("домен, которого нет, — находка, а не тишина", () => {
    expect(findingFor("нет-такого", "x", "name")).toContain("в дереве нет");
  });
});

// ── сами объявления ──────────────────────────────────────────────────────────

describe("каждое объявленное поле поиска принимается владельцем и сохраняет оператор", () => {
  it.each(declared.map((d) => [`${d.specId} (${d.domain}/${d.payloadKey}.${d.field})`, d] as const))(
    "%s",
    (_label, d) => {
      expect(findingFor(d.domain, d.payloadKey, d.field) ?? "").toBe("");
    },
  );
});
