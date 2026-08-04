// Вкладка «Операции» обещает подмаршрут `<apiPath>/{id}/operations`. Он есть НЕ
// у всякого ресурса: у части каталожных и админских ресурсов мутации либо
// синхронны, либо их нет вовсе, и ствол такого связывания не объявляет. Вкладка,
// собранная из `apiPath` любой спеки, в этих случаях бьёт в адрес, которого нет,
// — и пользователь видит не «операций нет», а отказ края.
//
// Здесь проверяется ЕДИНСТВЕННАЯ таблица, из которой консоль берёт этот путь:
// она обязана совпадать с деревом proto ТОЧНО и в обе стороны — лишняя запись
// (маршрут сняли, таблица помнит) и недостающая (маршрут завели, вкладки нет)
// одинаково находки. Источник истины — `google.api.http` связывания, а не
// перечень, переписанный рядом.

import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, resolve } from "node:path";

import { REGISTRY } from "./resource-registry";
import { OPERATIONS_LIST_PATHS, hasOperationsSubroute, operationsListPath } from "./operations-subroute";

const APP_DIR = process.cwd();
const PROTO_DIR = join(resolve(APP_DIR, "../.."), "proto");

function walk(dir: string, match: RegExp, out: string[] = []): string[] {
  for (const entry of readdirSync(dir)) {
    const p = join(dir, entry);
    if (statSync(p).isDirectory()) walk(p, match, out);
    else if (match.test(p)) out.push(p);
  }
  return out;
}

const protoFiles = walk(PROTO_DIR, /\.proto$/);

/**
 * Связывания вида `GET <base>/{param}/operations` — ровно та форма, которую
 * строит вкладка. Глагольная (`…/operations:all`) сюда НЕ попадает: якорь конца
 * строки отделяет её намеренно, это другой маршрут с другим ответом.
 */
const protoOperationBases = [
  ...new Set(
    protoFiles.flatMap((f) =>
      [...readFileSync(f, "utf8").matchAll(/get:\s*"([^"]+)\/\{[^}]+\}\/operations"/g)].map((m) => m[1]),
    ),
  ),
].sort();

describe("объём осмотренного — «ноль находок» отличимо от «ноль прочитанного»", () => {
  it("дерево proto прочитано и подмаршруты операций из него извлечены", () => {
    expect(protoFiles.length).toBeGreaterThan(100);
    expect(protoOperationBases.length).toBeGreaterThan(15);
  });

  it("выборка отделяет глагольную форму от слэшевой", () => {
    // `/iam/v1/accounts/{account_id}/operations:all` существует в стволе и
    // подмаршрутом ресурса НЕ является — это отдельный аккаунт-широкий список.
    const raw = protoFiles.flatMap((f) => [...readFileSync(f, "utf8").matchAll(/get:\s*"([^"]+operations[^"]*)"/g)]);
    expect(raw.some((m) => m[1].endsWith("/operations:all"))).toBe(true);
    expect(protoOperationBases.some((b) => b.includes("operations"))).toBe(false);
  });
});

describe("таблица подмаршрутов операций совпадает с деревом ствола", () => {
  const tableBases = [...OPERATIONS_LIST_PATHS].map((p) => p.replace("/{id}/operations", "")).sort();

  it("ни одной лишней записи — каждая имеет связывание в proto", () => {
    expect(tableBases.filter((b) => !protoOperationBases.includes(b))).toEqual([]);
  });

  it("ни одной недостающей — каждое связывание proto есть в таблице", () => {
    expect(protoOperationBases.filter((b) => !tableBases.includes(b))).toEqual([]);
  });

  it("форма записи — подстановка `{id}`, а не имя параметра владельца", () => {
    // Имя параметра у каждого владельца своё (`{subnet_id}`, `{user_id}`);
    // консоль подставляет один и тот же идентификатор, поэтому запись
    // нормализована. Расхождение формы сделало бы поиск по apiPath промахом.
    for (const p of OPERATIONS_LIST_PATHS) expect(p.endsWith("/{id}/operations")).toBe(true);
  });
});

describe("реестр shared и таблица — согласованы поимённо", () => {
  const specs = Object.values(REGISTRY);

  it("реестр прочитан", () => {
    expect(specs.length).toBeGreaterThan(20);
  });

  it("у спеки вкладка появляется ровно тогда, когда ствол несёт подмаршрут", () => {
    const disagree = specs
      .filter((s) => hasOperationsSubroute(s.apiPath) !== protoOperationBases.includes(s.apiPath))
      .map((s) => `${s.id} (${s.apiPath})`);
    expect(disagree).toEqual([]);
  });

  it("спеки без подмаршрута названы поимённо — это утверждение, а не умолчание", () => {
    const without = specs
      .filter((s) => !hasOperationsSubroute(s.apiPath))
      .map((s) => s.id)
      .sort();
    // Семь спек реестра адресуют пять путей: каталог размещения geo прочитан
    // дважды (`regions`/`compute-regions`, `zones`/`compute-zones`).
    expect(without).toEqual([
      "address-pools",
      "compute-regions",
      "compute-zones",
      "disk-types",
      "machine-types",
      "regions",
      "zones",
    ]);
    expect(new Set(specs.filter((s) => !hasOperationsSubroute(s.apiPath)).map((s) => s.apiPath)).size).toBe(5);
  });
});

describe("агрегатор операций проекта не отсеивает молча", () => {
  // OperationsPage перебирает РУКОПИСНЫЙ перечень типов и берёт путь операций из
  // таблицы; ресурс без подмаршрута он пропускает. Пропуск обязан быть пустым —
  // иначе страница молча не показывает операции целого типа, и это выглядит как
  // «операций нет». Утверждается ЗДЕСЬ, потому что перечень рукописный: снимут
  // подмаршрут у одного из семи — упадёт это, а не тишина на странице.
  const pageSrc = readFileSync(join(resolve(APP_DIR, "../shared/src"), "pages/OperationsPage.tsx"), "utf8");
  const block = /const VPC_RESOURCES = \[([\s\S]*?)\] as const;/.exec(pageSrc);

  it("перечень страницы прочитан", () => {
    expect(block).not.toBeNull();
  });

  it("у каждого типа перечня подмаршрут операций есть", () => {
    const ids = [...(block?.[1] ?? "").matchAll(/id:\s*"([^"]+)"/g)].map((m) => m[1]);
    expect(ids.length).toBe(7);
    const missing = ids.filter((id) => !REGISTRY[id] || !hasOperationsSubroute(REGISTRY[id].apiPath));
    expect(missing).toEqual([]);
  });
});

describe("построитель пути", () => {
  it("даёт путь ствола ресурсу, у которого подмаршрут есть", () => {
    expect(operationsListPath("/vpc/v1/subnets", "sbn-1")).toBe("/vpc/v1/subnets/sbn-1/operations");
    expect(operationsListPath("/compute/v1/instances", "ins-1")).toBe("/compute/v1/instances/ins-1/operations");
  });

  it("отказывает — а не выдумывает адрес — там, где подмаршрута нет", () => {
    expect(operationsListPath("/vpc/v1/addressPools", "apl-1")).toBeNull();
    expect(operationsListPath("/geo/v1/zones", "ru-central1-a")).toBeNull();
    expect(operationsListPath("/compute/v1/machineTypes", "mt-1")).toBeNull();
    expect(operationsListPath("/storage/v1/diskTypes", "dt-1")).toBeNull();
    // И чужой путь, которого в реестре нет вовсе, не проходит по совпадению
    // префикса: таблица ключуется целым apiPath.
    expect(operationsListPath("/vpc/v1/subnetsX", "x")).toBeNull();
    expect(operationsListPath("/vpc/v1", "x")).toBeNull();
  });

  it("идентификатор попадает в путь как есть, не теряясь", () => {
    expect(operationsListPath("/iam/v1/users", "usr-42")).toContain("usr-42");
  });
});
