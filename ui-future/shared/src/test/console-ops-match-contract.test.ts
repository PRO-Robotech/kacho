// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { readdirSync, readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { REGISTRY } from "@shared/lib/resource-registry";
import type { ResourceSpec } from "@shared/lib/resource-spec";

/**
 * Гейт: «КОНСОЛЬ НЕ ДАЁТ» И «ПЛАТФОРМА НЕ УМЕЕТ» — РАЗНЫЕ УТВЕРЖДЕНИЯ (#1593).
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧТО ЛОВИТ
 *
 * Спека объявляет `ops: {create:false, update:false, delete:false}`, а край
 * публично обслуживает эти же глаголы. Со стороны клиента разницы нет: он видит
 * список без единого действия и заключает, что ресурс неизменяем — то есть
 * читает выбор КОНСОЛИ как свойство ПЛАТФОРМЫ. Дальше он идёт в документацию,
 * находит там то же самое и строит на этом свою работу.
 *
 * Так и было у репозитория реестра: консоль объявляла его read-only с
 * комментарием «репозитории НЕ создаются через API», документация повторяла
 * «read-only проекция… не создаётся отдельным control-plane-вызовом», а контракт
 * нёс `CreateRepository`, `UpdateRepository`, `DeleteRepository` и
 * `RenameRepository` — все с публичной привязкой REST. Цена названа в задаче и
 * она не теоретическая: `visibility` репозитория — ЕДИНСТВЕННЫЙ рычаг, делающий
 * образ публичным, и путь к нему шёл ровно через `UpdateRepository`. То есть
 * «опубликовать образ» было тупиком у клиента, который следует продукту.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧТО УТВЕРЖДАЕТСЯ
 *
 * Пара «край умеет · консоль не даёт» обязана быть ОБЪЯВЛЕНА причиной
 * (`spec.mutationsNotOffered`), а не молчать. Гейт не требует давать мутацию:
 * консоль вправе не выставлять глагол — она не вправе делать это НЕЗАМЕТНО.
 * Причина — то, что прочтёт следующий инженер вместо повторного замера.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * КАК СВЯЗЫВАЮТСЯ ДВЕ СТОРОНЫ — ПО АДРЕСУ, А НЕ ПО ИМЕНИ
 *
 * Имя спеки консоли (`compute-instances`) и имя RPC (`CreateInstance`) связаны
 * соглашением, которое нигде не записано, — сверять по нему значило бы
 * измерять соглашение об именовании, а не предмет. Адрес же объявлен ОБЕИМИ
 * сторонами дословно: `apiPath` спеки и `google.api.http` контракта. Различаются
 * они только написанием подстановки (`{registryId}` против `{registry_id}`),
 * поэтому подстановки приводятся к `{}` — и сравниваются пути, а не слова.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ГРАНИЦА НАЗВАНА ЧИСЛОМ, А НЕ УМОЛЧАНИЕМ
 *
 * Спека, чей адрес не нашёлся в контрактах, этим гейтом НЕ судится: у неё нет
 * второй стороны, и молчание о ней означает «не с чем сверять», а не «сверено».
 * Число таких спек печатается отдельно — иначе «ноль находок» было бы неотличимо
 * от «ноль сверенного».
 */

const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(here, "../../../..");
const protoRoot = path.join(repoRoot, "proto");

/** Подстановки приводятся к `{}`: стороны пишут их по-разному, предмет один. */
function normalizePath(p: string): string {
  return p.replace(/\{[^}]*\}/g, "{}").replace(/\/+$/, "");
}

type Verb = "create" | "update" | "delete";

/** Публичные привязки REST контрактов: метод → множество нормализованных путей. */
function contractRoutes(): { routes: Map<string, Set<string>>; files: number } {
  const routes = new Map<string, Set<string>>([
    ["post", new Set()],
    ["patch", new Set()],
    ["put", new Set()],
    ["delete", new Set()],
  ]);
  let files = 0;
  const walk = (dir: string): void => {
    for (const e of readdirSync(dir, { withFileTypes: true })) {
      const full = path.join(dir, e.name);
      if (e.isDirectory()) walk(full);
      else if (e.name.endsWith(".proto")) {
        files += 1;
        const text = readFileSync(full, "utf8");
        // Привязка объявляется одной из двух форм — однострочной и блочной;
        // распознаватель обязан знать обе, иначе целая форма записи уходит из
        // наблюдения молча.
        for (const m of text.matchAll(/\b(post|patch|put|delete)\s*:\s*"([^"]+)"/g)) {
          routes.get(m[1])?.add(normalizePath(m[2]));
        }
      }
    }
  };
  try {
    walk(protoRoot);
  } catch {
    throw new Error(
      `контракты не прочитаны — ${protoRoot} недоступен. Сверять не с чем, и молчание ` +
        `здесь означало бы «расхождений нет», чего никто не проверял.`,
    );
  }
  return { routes, files };
}

const { routes, files: protoFiles } = contractRoutes();

/** Умеет ли край этот глагол по адресу спеки. */
function edgeServes(spec: ResourceSpec, verb: Verb): boolean {
  const base = normalizePath(spec.apiPath);
  const item = `${base}/{}`;
  if (verb === "create") return routes.get("post")!.has(base);
  if (verb === "update") return routes.get("patch")!.has(item) || routes.get("put")!.has(item);
  return routes.get("delete")!.has(item);
}

const specs = Object.entries(REGISTRY);
/** Спеки, чей адрес вообще встречается в контрактах, — только они и судятся. */
const matched = specs.filter(([, s]) =>
  (["create", "update", "delete"] as Verb[]).some((v) => edgeServes(s, v)),
);

interface Gap {
  id: string;
  verbs: Verb[];
}

const gaps: Gap[] = matched
  .map(([id, s]) => ({
    id,
    verbs: (["create", "update", "delete"] as Verb[]).filter((v) => edgeServes(s, v) && s.ops[v] !== true),
  }))
  .filter((g) => g.verbs.length > 0);

const unexplained = gaps.filter(
  ({ id }) => ((REGISTRY)[id].mutationsNotOffered ?? "").trim().length === 0,
);

process.stdout.write(
  `\n  глаголы консоли против контракта: файлов контракта прочитано ${protoFiles}, ` +
    `спек ${specs.length}, из них сверено с краем ${matched.length} ` +
    `(вне сверки ${specs.length - matched.length} — адрес в контрактах не найден), ` +
    `пар «край умеет · консоль не даёт» ${gaps.length}, из них без причины ${unexplained.length}\n\n`,
);

describe("«консоль не даёт» объявлено, а не умолчано (#1593)", () => {
  it("перепись непуста: пустой обход — отказ, а не зелёное", () => {
    expect(protoFiles).toBeGreaterThan(0);
    expect(specs.length).toBeGreaterThan(0);
    // Сверить хоть что-то гейт обязан: ноль сопоставленных спек означал бы, что
    // сравнение адресов сломано, а вердикт при этом зелёный.
    expect(matched.length).toBeGreaterThan(0);
  });

  it("у каждой пары «край умеет · консоль не даёт» названа причина", () => {
    expect(
      unexplained.map(
        ({ id, verbs }) =>
          `${id}: край обслуживает ${verbs.join(", ")}, консоль не даёт, причина не названа ` +
          `(клиент прочтёт это как свойство платформы, а не как выбор консоли)`,
      ),
    ).toEqual([]);
  });

  it("сравнение адресов работает — контроль на известной паре", () => {
    // Без этого контроля отрицание выше зеленело бы на сломанном сравнении:
    // «пар не найдено» верно и тогда, когда не найдено НИЧЕГО.
    const network = (REGISTRY).networks;
    expect(edgeServes(network, "create")).toBe(true);
    expect(edgeServes(network, "update")).toBe(true);
    expect(edgeServes(network, "delete")).toBe(true);

    // И обратная сторона: у выдуманного адреса край ничего не обслуживает.
    expect(edgeServes({ ...network, apiPath: "/нет/такого/пути" }, "create")).toBe(false);
  });

  it("репозиторий реестра сверяется — именно он и был предметом", () => {
    // Предпосылка: адрес репозитория ДОЛЖЕН находиться в контракте. Разойдись
    // он — гейт перестал бы судить ровно тот ресурс, ради которого заведён, и
    // остался бы зелёным.
    const repositories = (REGISTRY).repositories;
    expect(edgeServes(repositories, "create")).toBe(true);
    expect(edgeServes(repositories, "update")).toBe(true);
    expect(edgeServes(repositories, "delete")).toBe(true);
  });
});
