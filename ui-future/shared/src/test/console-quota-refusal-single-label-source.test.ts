// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { readdirSync, readFileSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import ts from "typescript";

import { ApiError } from "@shared/api/client";
import { presentError } from "@shared/lib/error-presentation";
import { kindLabel } from "@shared/lib/quota-view";

/**
 * Гейт: ВИД РЕСУРСА В ОТКАЗЕ ПО ПРЕДЕЛУ НАЗЫВАЕТСЯ ИЗ ЕДИНСТВЕННОГО СЛОВАРЯ (#1605).
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧТО ЛОВИТ
 *
 * Отказ по пределу назвал вид машинным именем (`vpc.network`), и напрашивался
 * очевидный ход: выписать перевод рядом с отказом. Тогда одно и то же слово
 * живёт в ДВУХ местах — на витрине квот и в отказе, — и расходится молча: на
 * витрине «Обработчики», в отказе «Слушатели». Клиент читает это как два разных
 * предмета, а поиск по консоли находит половину мест. Класс не гипотетический:
 * ровно им заведён соседний словарь подписей сущностей и его гейт
 * (`console-entity-names-single-source.test.ts`, #1609/#1610).
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧТО УТВЕРЖДАЕТСЯ — ДВЕ РАЗНЫЕ ВЕЩИ, И НИ ОДНА НЕ ВЫВОДИТСЯ ИЗ ДРУГОЙ
 *
 *  1. СЛОВАРЬ ОДИН. Ни один файл консоли, кроме `lib/quota-view.ts`, не
 *     объявляет отображения «имя вида квоты → русская подпись». Разбор идёт по
 *     узлам синтаксического дерева: комментарий и проза под запрет не подпадают
 *     by construction — их просто не видно. Это существенно, потому что имена
 *     видов обсуждаются в комментариях этого дерева, и запрет по подстроке
 *     краснел бы на собственном объяснении.
 *  2. ОТКАЗ ХОДИТ ИМЕННО В НЕГО. Для КАЖДОГО вида, который словарь знает, отказ
 *     по пределу называет ресурс тем же словом. Первое утверждение без второго
 *     зеленело бы на отказе, который словарь игнорирует вовсе; второе без
 *     первого — на второй копии, синхронной сегодня и разошедшейся завтра.
 *
 * Перечень видов не выписан: он ВЫВОДИТСЯ из самого словаря тем же разбором.
 * Заведут вид — гейт проверит и его, не дожидаясь правки этого файла.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ГРАНИЦА НАЗВАНА ЧЕСТНО
 *
 * Гейт судит ОБЪЯВЛЕНИЯ, а не отрисованный экран. Подпись, собранная из данных
 * в момент показа, ему не видна — за это отвечает сквозная проба браузером
 * (`ui.md` правило 12).
 */

const consoleRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");
const SKIP_DIRS = new Set([
  "node_modules",
  "dist",
  "build",
  "coverage",
  ".vite",
  ".turbo",
  "playwright-report",
  "test-results",
]);

/** Единственный законный дом словаря — относительно корня консоли. */
const LABEL_SOURCE = path.join("shared", "src", "lib", "quota-view.ts");

/**
 * Имя вида квоты: точечный путь из двух и более сегментов (`vpc.network`,
 * `vpc.network.subnet`). Именно эту форму закрепляет ограничение хранилища
 * владельца, и по ней вид отличается от любого другого ключа.
 */
const KIND_KEY = /^[a-z][A-Za-z0-9]*(\.[A-Za-z][A-Za-z0-9]*)+$/;
const CYRILLIC = /[А-Яа-яЁё]/;

/**
 * Словарём считается объектный литерал, где ДВЕ И БОЛЕЕ пары «имя вида → русская
 * подпись». Порог именно два: одна такая пара — это запись о конкретном ресурсе
 * (подпись кнопки, пример в фикстуре), а не отображение каталога, и называть её
 * второй копией значило бы краснеть на верном коде.
 */
const DICTIONARY_MIN_ENTRIES = 2;

export interface LabelMapFinding {
  where: string;
  entries: string[];
}

/**
 * Объявления словарей в одном файле — по узлам, не по подстроке.
 *
 * Экспортируется ради инъекции: она подаёт сюда синтетику и требует, чтобы
 * вторая копия находилась, а законный близнец молчал.
 */
export function kindLabelMaps(source: string, rel = "fixture.ts"): LabelMapFinding[] {
  const sf = ts.createSourceFile(rel, source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  const out: LabelMapFinding[] = [];
  const visit = (node: ts.Node): void => {
    if (ts.isObjectLiteralExpression(node)) {
      const entries: string[] = [];
      for (const prop of node.properties) {
        if (!ts.isPropertyAssignment(prop)) continue;
        const name = prop.name;
        const key = ts.isStringLiteral(name) ? name.text : ts.isIdentifier(name) ? name.text : null;
        if (key === null || !KIND_KEY.test(key)) continue;
        const value = prop.initializer;
        if (!ts.isStringLiteral(value) && !ts.isNoSubstitutionTemplateLiteral(value)) continue;
        if (!CYRILLIC.test(value.text)) continue;
        entries.push(`${key} → ${value.text}`);
      }
      if (entries.length >= DICTIONARY_MIN_ENTRIES) {
        const { line } = sf.getLineAndCharacterOfPosition(node.getStart(sf));
        out.push({ where: `${rel}:${line + 1}`, entries });
      }
    }
    ts.forEachChild(node, visit);
  };
  visit(sf);
  return out;
}

/** Имена видов, которые словарь знает, — выводятся из него, а не выписываются. */
export function kindsDeclaredIn(source: string): string[] {
  return kindLabelMaps(source, LABEL_SOURCE).flatMap((m) => m.entries.map((e) => e.split(" → ")[0]));
}

function walk(dir: string, out: string[] = []): string[] {
  for (const e of readdirSync(dir, { withFileTypes: true })) {
    if (SKIP_DIRS.has(e.name) || e.name.startsWith(".")) continue;
    const full = path.join(dir, e.name);
    if (e.isDirectory()) walk(full, out);
    else if (/\.(ts|tsx)$/.test(e.name) && !/\.test\.tsx?$/.test(e.name)) out.push(full);
  }
  return out;
}

function consolePackages(): string[] {
  return readdirSync(consoleRoot)
    .filter((d) => {
      try {
        return statSync(path.join(consoleRoot, d, "src")).isDirectory();
      } catch {
        return false;
      }
    })
    .sort();
}

const packages = consolePackages();
const files = packages.flatMap((p) => walk(path.join(consoleRoot, p, "src")));
const maps = files.flatMap((f) => kindLabelMaps(readFileSync(f, "utf8"), path.relative(consoleRoot, f)));
const strays = maps.filter((m) => !m.where.startsWith(LABEL_SOURCE));
const kinds = kindsDeclaredIn(readFileSync(path.join(consoleRoot, LABEL_SOURCE), "utf8"));

process.stdout.write(
  `\n  словарь видов квот: пакетов ${packages.length}, файлов прочитано ${files.length}, ` +
    `объявлений словаря ${maps.length} (вне единственного дома ${strays.length}), видов сверено ${kinds.length}\n\n`,
);

/** Отказ по пределу с названным видом — так, как его собирает край. */
function refusalNaming(kind: string) {
  return presentError(
    new ApiError(
      429,
      8,
      [{ "@type": "type.googleapis.com/google.rpc.ErrorInfo", reason: "QUOTA_EXCEEDED", metadata: { kind } }],
      `project prj-1 has reached its limit of 1 ${kind}`,
    ),
  );
}

describe("вид в отказе по пределу назван из единственного словаря (#1605)", () => {
  it("перепись непуста: пустой обход — отказ, а не зелёное", () => {
    // «Ноль находок» обязано быть отличимо от «ноль прочитанного», а «видов
    // сверено 0» — от «все виды сошлись».
    expect(packages.length).toBeGreaterThan(0);
    expect(files.length).toBeGreaterThan(0);
    expect(kinds.length).toBeGreaterThan(0);
  });

  it("второй копии словаря в консоли нет", () => {
    expect(strays.map((m) => `${m.where}: ${m.entries.length} записей`)).toEqual([]);
    // Предпосылка запрета: дом словаря СУЩЕСТВУЕТ. Исчезни он — запрет сравнивал
    // бы с пустотой и остался бы зелёным при любом числе копий.
    expect(maps.some((m) => m.where.startsWith(LABEL_SOURCE))).toBe(true);
  });

  it("отказ называет КАЖДЫЙ известный вид тем же словом, что витрина", () => {
    const wrong = kinds.filter((k) => {
      const label = kindLabel(k);
      return !(refusalNaming(k).subTitle ?? "").includes(`«${label}»`);
    });
    expect(wrong).toEqual([]);
  });

  it("незнакомый вид назван своим именем — каталог растёт на сервере", () => {
    // Положительный контроль к утверждению выше: без него «все известные виды
    // названы» зеленело бы и на отказе, который вид не называет вовсе.
    expect(refusalNaming("future.widget").subTitle ?? "").toContain("«future.widget»");
  });

  it("распознаватель ловит вторую копию и молчит на законном близнеце", () => {
    // ИНЪЕКЦИЯ — настоящей второй копией.
    expect(
      kindLabelMaps(`const L = { "vpc.network": "Облачные сети", "vpc.subnet": "Подсети" };`).length,
    ).toBe(1);

    // ЗАКОННЫЕ БЛИЗНЕЦЫ.
    // Точечные ключи, но значения не подписи — это карта путей, а не словарь.
    expect(kindLabelMaps(`const P = { "vpc.network": "/vpc/v1/networks", "vpc.subnet": "/vpc/v1/subnets" };`)).toEqual(
      [],
    );
    // Русские подписи, но ключи не имена видов — это соседний словарь сущностей.
    expect(kindLabelMaps(`const E = { networks: "Облачные сети", subnets: "Подсети" };`)).toEqual([]);
    // Одна пара — запись о ресурсе, а не отображение каталога.
    expect(kindLabelMaps(`const one = { "vpc.network": "Облачные сети" };`)).toEqual([]);
    // Проза о предмете запрета — не запрет.
    expect(kindLabelMaps(`// прежде «vpc.network» переводили как "Облачные сети" рядом с отказом\nconst a = 1;`)).toEqual(
      [],
    );
  });
});
