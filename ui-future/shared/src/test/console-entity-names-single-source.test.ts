// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { readdirSync, readFileSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import ts from "typescript";

import { ENTITIES } from "@shared/lib/entity-names";

/**
 * Гейт: ОДНУ СУЩНОСТЬ КОНСОЛЬ НАЗЫВАЕТ ОДНИМ СЛОВОМ — тем, что в словаре.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧТО ЛОВИТ
 *
 * Словарь подписей (`entity-names`) заведён ровно против этого класса и в своей
 * шапке перечисляет прежние жертвы. Он всё равно расходился с экраном, потому
 * что расхождение НЕ ВИДНО НИ ОДНОЙ ПРОБЕ: строка непуста, компонент рисуется,
 * типы сходятся. Заметить можно только пройдя продукт глазами клиента — так обе
 * находки и сделаны.
 *
 *   · #1610 — один ресурс, два имени: словарь говорит «Таблицы маршрутов», а
 *     пустое состояние, колонки, витрина квот и плитка на главной — «таблицы
 *     маршрутизации». Поиск по консоли по одному написанию находил половину мест;
 *   · #1609 — на экране выдачи прав пара «аккаунт · проект» называлась ТРЕМЯ
 *     способами сразу, причём двумя словами из модели, которой в продукте нет:
 *     переключатель подписан «Облако»/«Каталог», заглушка звала выбрать
 *     «Project» латиницей, а пилюля в шапке — «Проект». Там же жил «фолдер» —
 *     уровня `folder` в Kachō нет вовсе (уровни: Account → Project). Цена здесь
 *     не косметическая: ошибка выбора области означает выданный не туда доступ.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧТО УТВЕРЖДАЕТСЯ
 *
 * Ни один СТРОКОВЫЙ ЛИТЕРАЛ консоли не называет сущность именем, которого нет в
 * словаре. Разбор идёт по узлам синтаксического дерева, поэтому комментарий и
 * проза под запрет не подпадают by construction — их не снимают текстом, их
 * просто не видно. Это существенно: комментарии этого дерева обсуждают и
 * маршрутизацию как механизм, и прежние имена как урок, и запрет по подстроке
 * краснел бы на собственном объяснении.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧЕГО ГЕЙТ НЕ ТРЕБУЕТ, И ЭТО ВАЖНЕЕ ТОГО, ЧТО ТРЕБУЕТ
 *
 * Он не запрещает слово — он запрещает НАЗЫВАТЬ ИМ СУЩНОСТЬ. «Статическая
 * маршрутизация» и «Маршрутизация через NAT-инстанс» — темы документации о
 * механизме, они законны и остаются. «Облачная сеть», «Сервисы облака»,
 * «Каталог типов дисков» — тоже: там слово стоит частью другого имени. Поэтому
 * запреты ниже привязаны либо к склеенной паре слов («таблица … маршрутизации»),
 * либо к литералу ЦЕЛИКОМ («Облако» как подпись уровня), а не к вхождению.
 *
 * Граница названа честно: гейт судит литералы, а не отрисованный экран. Подпись,
 * собранная из данных, ему не видна — за это отвечает сквозная проба.
 */

const consoleRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");
const SKIP_DIRS = new Set(["node_modules", "dist", "build", "coverage", ".vite", ".turbo", "playwright-report", "test-results"]);

interface Ban {
  /** Когда литерал считается находкой. */
  hit: (value: string) => boolean;
  why: string;
}

const BANS: Ban[] = [
  {
    // Ресурс `RouteTable`. Словарь называет его «Таблица маршрутов»; второе
    // написание — «таблица маршрутизации» — жило в пустом состоянии, колонках,
    // витрине квот и на плитке главной.
    hit: (v) => /таблиц[аыуе]?\s+маршрутизации/i.test(v),
    why: `ресурс назван не по словарю: у него имя «${ENTITIES["route-tables"].singular}»`,
  },
  {
    // Уровня `folder` в Kachō нет: уровни — аккаунт и проект.
    hit: (v) => /фолдер/i.test(v),
    why: "уровня «фолдер» в продукте нет — уровни называются аккаунт и проект",
  },
  {
    // Литерал ЦЕЛИКОМ: слово стоит подписью уровня. Внутри другого имени
    // («Облачная сеть», «Каталог типов дисков») оно законно и не трогается.
    hit: (v) => /^(Облако|Каталог|Фолдер)$/.test(v.trim()),
    why: "уровень назван словом из модели, которой в продукте нет (уровни: аккаунт → проект)",
  },
  {
    // РУССКАЯ фраза, называющая сущность ЛАТИНИЦЕЙ. «Выберите Account в шапке»
    // отправляет клиента искать ручку, которая подписана «Аккаунт»: он читает
    // два имени как два разных предмета. Замер при заведении — 14 мест.
    //
    // Условие про кириллицу здесь несущее, а не украшение: чисто латинский
    // литерал — это идентификатор, ключ словаря или подпись фикстуры, и
    // называть его находкой значило бы требовать перевода того, что клиент не
    // читает. Цена ошибки в эту сторону выше: гейт, краснеющий на верном коде,
    // отключают первым.
    hit: (v) => /[А-Яа-яЁё]/.test(v) && /\b(Account|Project|Folder|Cloud)s?\b/.test(v),
    why: "сущность названа латиницей внутри русской фразы — на ручке она подписана по-русски",
  },
];

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

export interface NameFinding {
  where: string;
  text: string;
  why: string;
}

/**
 * Находки в одном файле — по УЗЛАМ строк и текста разметки, не по подстроке.
 *
 * Экспортируется ради пробы инъекции: она подаёт сюда синтетику и требует, чтобы
 * дефект находился, а законный близнец молчал.
 */
export function entityNameHits(source: string, rel = "fixture.tsx"): NameFinding[] {
  const sf = ts.createSourceFile(rel, source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  const out: NameFinding[] = [];
  const seen = (value: string, node: ts.Node) => {
    for (const { hit, why } of BANS) {
      if (!hit(value)) continue;
      const { line } = sf.getLineAndCharacterOfPosition(node.getStart(sf));
      out.push({ where: `${rel}:${line + 1}`, text: value.trim().slice(0, 80), why });
      return;
    }
  };
  const visit = (node: ts.Node): void => {
    if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) seen(node.text, node);
    else if (ts.isJsxText(node)) seen(node.text, node);
    else if (ts.isTemplateExpression(node)) {
      // У шаблона судятся его ПОСТОЯННЫЕ куски: подставляемое значение придёт из
      // данных, и о нём этому гейту сказать нечего.
      seen(node.head.text, node);
      for (const span of node.templateSpans) seen(span.literal.text, node);
    }
    ts.forEachChild(node, visit);
  };
  visit(sf);
  return out;
}

const packages = consolePackages();
const files = packages.flatMap((p) => walk(path.join(consoleRoot, p, "src")));
const findings = files.flatMap((f) => entityNameHits(readFileSync(f, "utf8"), path.relative(consoleRoot, f)));

process.stdout.write(
  `\n  имена сущностей: пакетов ${packages.length}, файлов прочитано ${files.length}, ` +
    `запретов ${BANS.length}, находок ${findings.length}\n\n`,
);

describe("одну сущность консоль называет одним словом (#1609, #1610)", () => {
  it("перепись непуста: пустой обход — отказ, а не зелёное", () => {
    // «Ноль находок» обязано быть отличимо от «ноль прочитанного».
    expect(packages.length).toBeGreaterThan(0);
    expect(files.length).toBeGreaterThan(0);
  });

  it("ни один литерал не называет сущность именем вне словаря", () => {
    expect(findings.map((f) => `${f.where}: «${f.text}» — ${f.why}`)).toEqual([]);
  });

  it("запрет ловит дефект и молчит на законном близнеце", () => {
    // Инъекция в обе стороны прямо здесь: без неё запрет мог бы ловить форму, а
    // не существо, и первый же ложный срабат его отключил бы.
    expect(entityNameHits(`const a = "Таблицы маршрутизации";`).length).toBe(1);
    expect(entityNameHits(`const a = "Имя в пределах фолдера";`).length).toBe(1);
    expect(entityNameHits(`const a = "Облако";`).length).toBe(1);
    expect(entityNameHits(`const a = "Выберите Account в шапке";`).length).toBe(1);

    // ЗАКОННЫЕ БЛИЗНЕЦЫ — тема о механизме, слово внутри другого имени,
    // и обсуждение в комментарии.
    expect(entityNameHits(`const a = "Статическая маршрутизация";`)).toEqual([]);
    expect(entityNameHits(`const a = "Маршрутизация через NAT-инстанс";`)).toEqual([]);
    expect(entityNameHits(`const a = "Облачная сеть";`)).toEqual([]);
    expect(entityNameHits(`const a = "Сервисы облака";`)).toEqual([]);
    expect(entityNameHits(`const a = "Каталог типов дисков";`)).toEqual([]);
    expect(entityNameHits(`// прежде здесь стояла «Таблица маршрутизации» и «Облако»\nconst a = 1;`)).toEqual([]);
    // Чисто латинский литерал — идентификатор или ключ, а не фраза к клиенту.
    expect(entityNameHits(`const a = "Service Accounts";`)).toEqual([]);
    expect(entityNameHits(`const a = "AccountService";`)).toEqual([]);
    expect(entityNameHits(`const a = { AccountId: "prj" };`)).toEqual([]);
  });

  it("словарь называет ресурс, ради которого запрет заведён", () => {
    // Предпосылка запрета: у ресурса ЕСТЬ имя в словаре. Исчезни запись — запрет
    // сравнивал бы с пустотой и остался бы зелёным при любом написании.
    expect(ENTITIES["route-tables"].singular).toBe("Таблица маршрутов");
    expect(ENTITIES.accounts.singular).toBe("Аккаунт");
    expect(ENTITIES.projects.singular).toBe("Проект");
  });
});
