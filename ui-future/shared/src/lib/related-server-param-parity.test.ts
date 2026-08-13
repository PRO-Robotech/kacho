// Типизированное поле сужения, объявленное вкладкой дочернего ресурса, обязано
// быть полем, которое ВЛАДЕЛЕЦ ребёнка объявил в контракте И ЧИТАЕТ.
//
// Предмет. Ребро `related` может объявить `serverParamField` — имя поля
// списочного запроса, которым список ребёнка сужается по родителю НА СЕРВЕРЕ.
// Консоль отправляет его отдельным параметром (`?<поле>=<id родителя>`; форму
// запроса пиннит `ResourceShell.related.test.tsx`).
//
// Это ДРУГОЙ механизм, чем выражение `filter`, и его паритет ломается иначе.
// Выражение проверяется по закрытому белому списку, поэтому лишнее поле —
// громкий `InvalidArgument`. Типизированное поле проверяется тем, что вообще
// объявлено в сообщении запроса: незнакомый параметр запроса край просто
// ОТБРАСЫВАЕТ. Значит расхождение здесь ТИХОЕ — вкладка показывает первую
// страницу списка проекта, выдавая её за список ребёнка, и никакой ошибки
// пользователь не видит. Именно поэтому проба нужна отдельная, а не «такая же».
//
// Утверждается ПАРА, а не одно объявление:
//   1. поле объявлено в контракте владельца (`List<Ресурс>Request` в `proto/`);
//   2. поле ЧИТАЕТСЯ прод-кодом владельца. Объявленное и непрочитанное поле —
//      это «принято и проигнорировано»: сужение отправлено, ответ не сужен, и
//      снова тихо. Одного объявления мало by construction.
//
// Что остаётся ВНЕ: поля белого списка выражения `filter` — их держит
// `related-server-filter-parity.test.ts`. Объявить оба механизма на одном ребре
// нельзя вовсе: это запрещено типом `RelatedSpec` (`never`), поэтому проверять
// здесь нечего — противоречие не представимо.

import { execFileSync } from "node:child_process";
import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import { join, resolve } from "node:path";

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
    .filter((r) => !!r.serverParamField)
    .map((r) => ({ parentId: parent.id, childId: r.childId, field: r.serverParamField! })),
);

// ── контракт владельца: proto ────────────────────────────────────────────────

/** `addresses` / `route_tables` / `routeTables` → `Addresses` / `RouteTables`. */
function pascal(payloadKey: string): string {
  return payloadKey
    .replace(/[_-]+(.)/g, (_, c: string) => c.toUpperCase())
    .replace(/^(.)/, (_, c: string) => c.toUpperCase());
}

/** Домен ребёнка из его REST-пути: `/vpc/v1/addresses` → `vpc`. */
function domainOf(apiPath: string): string {
  return apiPath.split("/").filter(Boolean)[0] ?? "";
}

function walk(dir: string, match: RegExp, out: string[] = []): string[] {
  for (const entry of readdirSync(dir)) {
    const p = join(dir, entry);
    if (statSync(p).isDirectory()) walk(p, match, out);
    else if (match.test(p)) out.push(p);
  }
  return out;
}

interface ProtoRead {
  /** Имя сообщения → набор имён его полей. */
  messages: Map<string, Set<string>>;
  filesRead: number;
}

const protoCache = new Map<string, ProtoRead | null>();

/** Читает контракты домена; `null` — каталога такого домена в `proto/` нет. */
function readProto(domain: string): ProtoRead | null {
  if (protoCache.has(domain)) return protoCache.get(domain)!;
  const dir = join(REPO_ROOT, "proto", "kacho", "cloud", domain);
  if (!existsSync(dir)) {
    protoCache.set(domain, null);
    return null;
  }
  const files = walk(dir, /\.proto$/);
  const messages = new Map<string, Set<string>>();
  for (const file of files) {
    const text = readFileSync(file, "utf8");
    // Тело сообщения — до первой закрывающей скобки в начале строки: вложенных
    // сообщений в списочных запросах Kachō нет, и появись они — предикат ниже
    // просто не найдёт поле, то есть ошибётся в сторону НАХОДКИ, а не тишины.
    for (const m of text.matchAll(/^message\s+(\w+)\s*\{([\s\S]*?)^\}/gm)) {
      const body = stripComments(m[2]);
      const fields = new Set<string>();
      for (const f of body.matchAll(/(\w+)\s*=\s*\d+\s*[[;]/g)) fields.add(f[1]);
      messages.set(m[1], fields);
    }
  }
  const read = { messages, filesRead: files.length };
  protoCache.set(domain, read);
  return read;
}

// ── читатель поля: прод-код владельца ────────────────────────────────────────

/** Функция прод-кода, ПРИНИМАЮЩАЯ списочный запрос: имя параметра и тело. */
interface RequestHandler {
  param: string;
  body: string;
}

interface GoRead {
  byRequestType: Map<string, RequestHandler[]>;
  filesRead: number;
}

const goCache = new Map<string, GoRead | null>();

/** Тело функции, чья сигнатура найдена по индексу `from`: от её `{` до парной. */
function functionBody(code: string, from: number): string {
  const open = code.indexOf("{", from);
  if (open < 0) return "";
  let depth = 0;
  for (let i = open; i < code.length; i++) {
    if (code[i] === "{") depth++;
    else if (code[i] === "}") {
      depth--;
      if (depth === 0) return code.slice(open, i + 1);
    }
  }
  return code.slice(open);
}

function readGo(domain: string): GoRead | null {
  if (goCache.has(domain)) return goCache.get(domain)!;
  const dir = join(REPO_ROOT, "services", domain);
  if (!existsSync(dir)) {
    goCache.set(domain, null);
    return null;
  }
  const files = walk(dir, /\.go$/).filter((f) => !f.endsWith("_test.go"));
  const byRequestType = new Map<string, RequestHandler[]>();
  // Ищется не упоминание типа в файле, а ФУНКЦИЯ, принимающая запрос: тогда
  // «поле читается» означает, что его читают У ЭТОГО запроса. Проба инъекции
  // показала, зачем это нужно: файловый предикат оставался зелёным на снятом
  // чтении, потому что рядом, в другой функции того же файла, стояло обращение к
  // одноимённому полю ЧУЖОГО сообщения.
  const sig = /[(,]\s*(\w+)\s+\*[\w.]*\bList(\w+)Request\b/g;
  for (const file of files) {
    const code = stripComments(readFileSync(file, "utf8"));
    for (const m of code.matchAll(sig)) {
      const key = `List${m[2]}Request`;
      const bucket = byRequestType.get(key) ?? [];
      bucket.push({ param: m[1], body: functionBody(code, m.index ?? 0) });
      byRequestType.set(key, bucket);
    }
  }
  const read = { byRequestType, filesRead: files.length };
  goCache.set(domain, read);
  return read;
}

/** Имя поля контракта → имя поля сгенерённой структуры: `subnet_id` → `SubnetId`. */
function goFieldName(protoField: string): string {
  return protoField.replace(/(^|_)(\w)/g, (_, __, c: string) => c.toUpperCase());
}

/** Читает ли прод-код владельца это поле ИМЕННО У ЭТОГО запроса. */
function ownerReads(domain: string, requestType: string, protoField: string): boolean {
  const go = readGo(domain);
  if (!go) return false;
  const handlers = go.byRequestType.get(requestType) ?? [];
  const name = goFieldName(protoField);
  return handlers.some((h) => new RegExp(`\\b${h.param}\\.(${name}\\b|Get${name}\\(\\))`).test(h.body));
}

/** Находка по одному ребру; `null` — владелец такое сужение принимает и читает. */
function findingFor(edge: DeclaredEdge): string | null {
  const spec = REGISTRY[edge.childId];
  if (!spec) return `${edge.parentId} → ${edge.childId}: ребёнка «${edge.childId}» нет в реестре`;
  const domain = domainOf(spec.apiPath);
  const proto = readProto(domain);
  if (!proto) {
    return (
      `${edge.parentId} → ${edge.childId}: контрактов домена «proto/kacho/cloud/${domain}» ` +
      "в дереве нет — проба не может установить, что владелец принимает; расширь её вместе с доменом"
    );
  }
  const requestType = `List${pascal(spec.payloadKey)}Request`;
  const fields = proto.messages.get(requestType);
  if (!fields) {
    return (
      `${edge.parentId} → ${edge.childId}: в контрактах домена «${domain}» нет сообщения ` +
      `«${requestType}» — либо у ребёнка нет списочного запроса, либо правило имени разошлось с деревом`
    );
  }
  if (!fields.has(edge.field)) {
    return (
      `${edge.parentId} → ${edge.childId}: объявлено серверное поле «${edge.field}», ` +
      `но «${requestType}» несёт только ${[...fields].sort().join(", ")} — ` +
      "край отбросит параметр МОЛЧА, и вкладка покажет список проекта как список ребёнка"
    );
  }
  if (!ownerReads(domain, requestType, edge.field)) {
    return (
      `${edge.parentId} → ${edge.childId}: поле «${edge.field}» объявлено в «${requestType}», ` +
      "но прод-код владельца его не читает — принято и проигнорировано: сужение отправлено, ответ не сужен"
    );
  }
  return null;
}

// ── объём осмотренного ───────────────────────────────────────────────────────

describe("объём осмотренного — «ноль находок» отличимо от «ноль прочитанного»", () => {
  it("прочитаны контракты владельца, и списочные запросы в них найдены", () => {
    const proto = readProto("vpc");
    expect(proto).not.toBeNull();
    expect(proto!.filesRead).toBeGreaterThan(5);
    expect(proto!.messages.size).toBeGreaterThan(20);
    expect(proto!.messages.get("ListAddressesRequest")).toBeDefined();
  });

  it("прочитано прод-дерево владельца, и функции, принимающие списочные запросы, найдены", () => {
    const go = readGo("vpc");
    expect(go).not.toBeNull();
    expect(go!.filesRead).toBeGreaterThan(100);
    expect(go!.byRequestType.get("ListAddressesRequest")?.length).toBeGreaterThan(0);
    // Тело функции извлечено, а не пусто: на пустом теле «поле не читается» было
    // бы верно всегда, и вторая половина предиката превратилась бы в отказ всему.
    for (const h of go!.byRequestType.get("ListAddressesRequest")!) {
      expect(h.body.length).toBeGreaterThan(10);
      expect(h.param).not.toBe("");
    }
  });

  it("хотя бы одно ребро объявляет типизированное поле — иначе перечень ниже пуст", () => {
    // Пустой перечень означал бы, что сужение по родителю там, где выражением
    // фильтра оно не выражается, снова целиком клиентское. Это находка, а не
    // «нечего проверять».
    expect(declared.length).toBeGreaterThanOrEqual(1);
  });

  it("перепись реестров: типизированное поле объявляет только тот реестр, что читает эта проба", () => {
    const registries = execFileSync("git", ["ls-files", "*/src/lib/resource-registry.tsx"], {
      cwd: UI_ROOT,
      encoding: "utf8",
    })
      .split("\n")
      .filter(Boolean);
    expect(registries.length).toBeGreaterThanOrEqual(5);
    const declaring = registries.filter((rel) =>
      stripComments(readFileSync(join(UI_ROOT, rel), "utf8")).includes("serverParamField"),
    );
    expect(declaring).toEqual(["shared/src/lib/resource-registry.tsx"]);
  });
});

// ── контроль в обе стороны ───────────────────────────────────────────────────

describe("сопоставитель различает — контроль в обе стороны", () => {
  it("законное поле признаётся", () => {
    expect(findingFor({ parentId: "subnets", childId: "addresses", field: "subnet_id" })).toBeNull();
    // Второе законное поле того же владельца — иначе «признаёт» могло бы значить
    // «признаёт что угодно из одного знакомого имени».
    expect(findingFor({ parentId: "subnets", childId: "addresses", field: "ip_address" })).toBeNull();
  });

  it("поля, которого в контракте нет, — находка, и отказ называет предмет", () => {
    // Правдоподобное имя: у адреса есть зона, но списочный запрос её не несёт.
    // Край отбросил бы параметр молча — ровно та тишина, ради которой проба есть.
    const f = findingFor({ parentId: "subnets", childId: "addresses", field: "zone_id" });
    expect(f).not.toBeNull();
    expect(f).toContain("zone_id");
    expect(f).toContain("ListAddressesRequest");
  });

  it("поиск читателя умеет отвечать «нет» — иначе вторая половина пары беспредметна", () => {
    // Половина предиката «поле читается» зеленела бы всегда, если бы поиск не
    // умел не находить. Положительная сторона — в первом утверждении этого блока.
    expect(ownerReads("vpc", "ListAddressesRequest", "subnet_id")).toBe(true);
    expect(ownerReads("vpc", "ListAddressesRequest", "totally_absent_field")).toBe(false);
  });

  it("ребёнок, чьего владельца не установить, — находка, а не тишина", () => {
    expect(findingFor({ parentId: "subnets", childId: "нет-такого", field: "subnet_id" })).toContain(
      "нет в реестре",
    );
  });
});

// ── сами объявления ──────────────────────────────────────────────────────────

describe("каждое объявленное типизированное поле принимается и читается владельцем", () => {
  it.each(declared.map((e) => [`${e.parentId} → ${e.childId} (${e.field})`, e] as const))("%s", (_label, edge) => {
    expect(findingFor(edge) ?? "").toBe("");
  });
});
