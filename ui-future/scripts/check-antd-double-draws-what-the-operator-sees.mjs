#!/usr/bin/env node
// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

/**
 * Общий стенд-заменитель `antd` обязан РИСОВАТЬ то, что видит оператор.
 *
 * ПРЕДМЕТ (#570). Часть компонентов antd получает видимое оператору не детьми, а
 * ПРОПОМ: состав меню — `menu`, вкладки и панели — `items`, узлы дерева —
 * `treeData`, строки списка — `dataSource`/`renderItem`, вопрос подтверждения —
 * `title`/`description`, число значка — `count`. Заменитель, подменяющий такое
 * имя пустым `<div>{children}</div>`, рисует НИЧЕГО — и всякая проба о составе
 * меню, вкладок, дерева становится истинной при любом составе, включая пустой.
 * Такая проба хуже отсутствующей: слот занят, уверенность создана, предмета нет.
 *
 * Замер в день заведения гейта: восемь имён, 31 употребление, 27 файлов.
 *
 * ЧТО ПРОВЕРЯЕТСЯ. Имя, подменённое пустым `<div>`, не должно употребляться в
 * продуктовом коде с пропом из списка «несёт видимое оператору». Исходов два:
 * либо заменитель это имя рисует (тогда оно перестаёт быть пустым), либо оно
 * НАЗВАНО в перечне `NOT_DRAWN` с причиной — а перечень истекает сам: запись,
 * которой больше нечего исключать, роняет прогон.
 *
 * ПОЧЕМУ AST, А НЕ ТЕКСТ. Регулярное выражение по тексту спотыкается о стрелку
 * `=>` внутри тела тега (тело обрывается на первом `>`), поэтому текстовая
 * перепись НЕДОСЧИТЫВАЕТ: при заведении гейта она потеряла `Tabs` и `Collapse`
 * ровно по этой причине. И она же нашла бы имя в комментарии, объясняющем эту
 * самую проверку, — в том числе в шапке этого файла.
 *
 * ОБЪЁМ ОСМОТРЕННОГО заявляется всегда: «ноль находок» обязано быть отличимо от
 * «ноль прочитанного». Пустой перечень исключений — не поломка, а ЦЕЛЬ, поэтому
 * на нём гейт проходит; способность упасть доказывается `--self-test`.
 *
 * Запуск из ui-future/:  node scripts/check-antd-double-draws-what-the-operator-sees.mjs
 *                        node scripts/check-antd-double-draws-what-the-operator-sees.mjs --self-test
 */

import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";

import ts from "typescript";

const STUB = "shared/src/test/antd-stub.ts";

/**
 * Пропы, которыми настоящий компонент antd доносит до оператора ВИДИМОЕ. Список
 * закрытый и объявлен здесь, а не выведен: он и есть предмет проверки, и его
 * расширение — осознанное решение, а не побочный эффект правки дерева.
 *
 * `tip` добавлен к перечню из #570: настоящая вертушка показывает подпись
 * загрузки рядом с собой, и без неё состояние ожидания неотличимо от пустого
 * экрана.
 */
const VISIBLE_PROPS = new Set([
  "menu",
  "items",
  "treeData",
  "dataSource",
  "renderItem",
  "title",
  "description",
  "value",
  "count",
  "options",
  "overlay",
  "content",
  "label",
  "extra",
  "subTitle",
  "tip",
]);

/**
 * Имена, которые заменитель НАМЕРЕННО не рисует, — с причиной по каждому.
 * Перечень счётный и самоистекающий: запись, под которую в дереве не нашлось ни
 * одного употребления, роняет прогон, потому что исключать ей уже нечего.
 *
 * Сегодня он ПУСТ, и это цель, а не недосмотр.
 */
const NOT_DRAWN = Object.create(null);

const uiRoot = process.cwd();
if (!fs.existsSync(path.join(uiRoot, "package.json"))) {
  console.error("::error::запускать из ui-future/ (нет package.json в текущем каталоге)");
  process.exit(2);
}

/**
 * Имена, подменённые пустым `<div>`: ПРЯМЫЕ свойства возвращаемого объекта, чьё
 * значение — тот самый `Component`. Берётся разбором, а не текстом.
 *
 * Только прямые — и это несущее ограничение, а не экономия. Внутри заменителя
 * есть пространства имён (`Form.List`, `Typography.Text`), чьи члены совпадают
 * по имени с самостоятельными компонентами (`List`). Собирая все присваивания
 * подряд, гейт объявлял бы пустым `List`, который на самом деле нарисован, —
 * и находка указывала бы на исправное место. Симметрично и в разборе
 * употреблений рассматриваются только ПРОСТЫЕ теги: `<List.Item>` принадлежит
 * своему пространству имён, а не этому перечню.
 */
function emptyNames(stubSource) {
  const sf = ts.createSourceFile(STUB, stubSource, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS);
  const names = new Set();
  let returned = null;
  const walk = (node) => {
    if (
      returned === null &&
      ts.isReturnStatement(node) &&
      node.expression &&
      ts.isObjectLiteralExpression(node.expression) &&
      node.expression.properties.some(
        (p) => ts.isPropertyAssignment(p) && ts.isIdentifier(p.name) && p.name.text === "__esModule",
      )
    ) {
      returned = node.expression;
    }
    ts.forEachChild(node, walk);
  };
  walk(sf);
  if (returned === null) return names;
  for (const p of returned.properties) {
    if (
      ts.isPropertyAssignment(p) &&
      ts.isIdentifier(p.initializer) &&
      p.initializer.text === "Component" &&
      (ts.isIdentifier(p.name) || ts.isStringLiteral(p.name))
    ) {
      names.add(p.name.text);
    }
  }
  return names;
}

/**
 * Продуктовые `.tsx` из индекса git. Пробы и тестовая оснастка исключены: они и
 * есть потребитель заменителя, судить их им же — тавтология.
 */
function productFiles() {
  return execFileSync("git", ["ls-files", "*.tsx"], { cwd: uiRoot, encoding: "utf8" })
    .split("\n")
    .filter(Boolean)
    .filter((f) => !/\.test\.tsx$/.test(f))
    .filter((f) => !/(^|\/)src\/test\//.test(f))
    .filter((f) => !/^e2e\//.test(f));
}

/**
 * Употребления простых имён (без точки) с пропами из закрытого списка.
 * Составные теги (`List.Item`, `Typography.Text`) — предмет своих пространств
 * имён, и здесь намеренно не рассматриваются: перечень заменителя объявляет их
 * отдельно.
 */
function usages(files, names) {
  const found = [];
  let parsed = 0;
  for (const f of files) {
    const src = fs.readFileSync(path.join(uiRoot, f), "utf8");
    const sf = ts.createSourceFile(f, src, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
    parsed += 1;
    const walk = (node) => {
      if (ts.isJsxOpeningElement(node) || ts.isJsxSelfClosingElement(node)) {
        const tag = node.tagName;
        if (ts.isIdentifier(tag) && names.has(tag.text)) {
          const props = node.attributes.properties
            .filter((p) => ts.isJsxAttribute(p) && ts.isIdentifier(p.name))
            .map((p) => p.name.text)
            .filter((n) => VISIBLE_PROPS.has(n));
          if (props.length) {
            const { line } = sf.getLineAndCharacterOfPosition(node.getStart(sf));
            found.push({ name: tag.text, file: f, line: line + 1, props });
          }
        }
      }
      ts.forEachChild(node, walk);
    };
    walk(sf);
  }
  return { found, parsed };
}

/** Разбор одного состояния дерева. Чистая функция — её же зовёт `--self-test`. */
function analyze(stubSource, files) {
  const names = emptyNames(stubSource);
  const { found, parsed } = usages(files, names);
  return { names, found, parsed };
}

const files = productFiles();
const stubPath = path.join(uiRoot, STUB);
if (!fs.existsSync(stubPath)) {
  console.error(`::error::предпосылка гейта не выполнена: ${STUB} не найден — заменителя нет, судить нечего`);
  process.exit(1);
}
const stubSource = fs.readFileSync(stubPath, "utf8");

if (process.argv.includes("--self-test")) {
  // ИНЪЕКЦИЯ НАСТОЯЩИМ ВХОДОМ, в обе стороны. Синтетического файла в дерево не
  // вносим: возвращаем заменителю ровно то состояние, в котором он был до
  // починки (`Dropdown` подменён пустым `<div>`), и требуем красного с именем и
  // координатой. Затем — законный близнец: неизменённый заменитель на тех же
  // файлах обязан молчать про то же имя.
  const broken = stubSource.replace(/\n {4}Dropdown,\n/, "\n    Dropdown: Component,\n");
  if (broken === stubSource) {
    console.error(
      "::error::самопроверка не смогла внести дефект: в перечне заменителя нет строки «    Dropdown,» — " +
        "предпосылка самопроверки исчезла вместе с формой файла, чинить надо её, а не гейт",
    );
    process.exit(1);
  }
  const red = analyze(broken, files);
  const redHits = red.found.filter((h) => h.name === "Dropdown");
  if (redHits.length === 0) {
    console.error("::error::самопроверка: внесённый дефект НЕ пойман — гейт не способен упасть");
    process.exit(1);
  }
  const green = analyze(stubSource, files);
  const greenHits = green.found.filter((h) => h.name === "Dropdown");
  if (greenHits.length > 0) {
    console.error(
      `::error::самопроверка: гейт краснеет на ЗАКОННОМ заменителе (${greenHits.length} находок про Dropdown) — ` +
        "он ловит форму, а не существо",
    );
    process.exit(1);
  }
  console.log(
    `самопроверка: с внесённым дефектом находок про Dropdown ${redHits.length} ` +
      `(первая — ${redHits[0].file}:${redHits[0].line}), на законном заменителе 0; осмотрено файлов ${green.parsed}`,
  );
  process.exit(0);
}

const { names, found, parsed } = analyze(stubSource, files);

const findings = [];
for (const hit of found) {
  if (hit.name in NOT_DRAWN) continue;
  findings.push(
    `${hit.file}:${hit.line}: <${hit.name}> получает ${hit.props.map((p) => `\`${p}\``).join(", ")} — ` +
      `видимое оператору приходит ПРОПОМ, а заменитель этого имени рисует пустой <div>: ` +
      `проба о его составе истинна при любом составе`,
  );
}
// Послабление истекает САМО: записи, под которую в дереве нет ни одного
// употребления, исключать больше нечего.
for (const name of Object.keys(NOT_DRAWN)) {
  if (!names.has(name)) {
    findings.push(`перечень NOT_DRAWN держит «${name}», а заменитель его уже рисует — запись снять`);
    continue;
  }
  if (!found.some((h) => h.name === name)) {
    findings.push(
      `перечень NOT_DRAWN держит «${name}», но в продуктовом коде нет ни одного его употребления ` +
        `с видимым пропом — исключать нечего, запись снять`,
    );
  }
}

console.log(
  `осмотрено: продуктовых .tsx ${parsed}, имён с пустым заменителем ${names.size}, ` +
    `пропов в закрытом списке ${VISIBLE_PROPS.size}; употреблений «пустое имя × видимый проп» ${found.length}, ` +
    `названо намеренно не рисующими ${Object.keys(NOT_DRAWN).length}`,
);

if (parsed === 0 || names.size === 0) {
  console.error(
    "::error::предпосылка гейта не выполнена: прочитано 0 файлов либо в заменителе нет ни одного пустого имени — " +
      "«ноль находок» здесь означало бы «ноль прочитанного»",
  );
  process.exit(1);
}

if (findings.length) {
  for (const f of findings) console.error(`::error::${f}`);
  console.error(`\n✖ находок: ${findings.length}`);
  process.exit(1);
}

console.log("✓ ни одно имя с пустым заменителем не получает в продуктовом коде видимого оператору пропа");
