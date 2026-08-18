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
 * ЧТО ПРОВЕРЯЕТСЯ — ДВЕ ПОЛОВИНЫ ОДНОГО ПРЕДМЕТА.
 *
 * ПЕРВАЯ (#570). Имя, подменённое пустым `<div>`, не должно употребляться в
 * продуктовом коде с пропом из списка «несёт видимое оператору». Исходов два:
 * либо заменитель это имя рисует (тогда оно перестаёт быть пустым), либо оно
 * НАЗВАНО в перечне `NOT_DRAWN` с причиной — а перечень истекает сам: запись,
 * которой больше нечего исключать, роняет прогон.
 *
 * ВТОРАЯ (#625). Привести заменитель к настоящей форме — половина дела: пока на
 * приведённый вид нет пробы, наблюдаемость есть, а наблюдения нет. Хуже того,
 * пробу МОЖНО написать по атрибуту, который производил дублёр (настоящий antd
 * таких атрибутов не производит ни одного) — такая проба прибита к форме
 * дублёра и переживает продукт. Поэтому каждый вид, приведённый к настоящей
 * форме ради наблюдаемости, назван в перечне `PROBED_BY` вместе с пробой,
 * которая утверждает его НА ПРОДУКТОВОЙ ПОВЕРХНОСТИ.
 *
 * ГРАНИЦА ТОЧНОСТИ ВТОРОЙ ПОЛОВИНЫ, названная честно: гейт держит СУЩЕСТВОВАНИЕ
 * пробы у поверхности, а не содержание её утверждений. Содержание доказано
 * инъекцией в обе стороны при заведении каждой пробы (заменитель возвращён в
 * пустой `<div>` → красное; пункт снят из продукта → красное; законный набор →
 * молчание) и воспроизводится тем же способом. Механического судьи содержания
 * здесь нет, и делать вид, что есть, — тот самый класс, который гейт ловит.
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

/**
 * Виды, приведённые к настоящей форме ради наблюдаемости, и пробы, которые
 * утверждают их на ПРОДУКТОВОЙ поверхности.
 *
 * Перечень счётный и самоистекающий по трём осям сразу: запись падает, если
 * (а) проба исчезла из дерева, (б) вид снова стал проходным `<div>` — тогда
 * проба утверждает о форме дублёра, (в) продукт перестал употреблять вид с
 * видимым пропом — тогда держать нечего.
 *
 * Проба обязана лежать РЯДОМ с продуктовым файлом, который вид употребляет:
 * проба, стоящая в стороне, обычно утверждает о заменителе, а не о продукте
 * (пробы самого заменителя живут в `shared/src/test/antd-stub.test.tsx` и
 * второй половиной гейта намеренно НЕ засчитываются — они утверждают о дублёре).
 */
const PROBED_BY = {
  Badge: "shared/src/components/organisms/DetailShell/DetailShell.badge.test.tsx",
  Collapse: "shared/src/components/organisms/form/SgRulesEditor/SgRulesEditor.test.tsx",
  Dropdown: "shared/src/components/molecules/RowActionsMenu/RowActionsMenu.test.tsx",
  List: "shared/src/pages/auth/Settings.passkeys-list.test.tsx",
  Menu: "shared/src/components/organisms/DetailShell/DetailShell.test.tsx",
  Popconfirm: "iam/src/components/organisms/SaKeysPanel/SaKeysPanel.test.tsx",
  Spin: "shared/src/components/organisms/ResourceEditPage/ResourceEditPage.loading-caption.test.tsx",
  Statistic: "shared/src/pages/DashboardPage.statistic.test.tsx",
  Tabs: "iam/src/pages/iam/AccessPage/AccessPage.role-tabs.test.tsx",
  Tree: "shared/src/components/organisms/DependencyTreePanel/DependencyTreePanel.test.tsx",
};

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
function stubNames(stubSource) {
  const sf = ts.createSourceFile(STUB, stubSource, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS);
  const names = new Set();
  const drawn = new Set();
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
  if (returned === null) return { names, drawn };
  for (const p of returned.properties) {
    // Сокращённая запись (`Dropdown,`) — имя, у которого есть своя реализация:
    // подставить пустой `Component` таким способом нельзя by construction.
    if (ts.isShorthandPropertyAssignment(p)) {
      drawn.add(p.name.text);
      continue;
    }
    if (!ts.isPropertyAssignment(p) || !(ts.isIdentifier(p.name) || ts.isStringLiteral(p.name))) continue;
    const empty = ts.isIdentifier(p.initializer) && p.initializer.text === "Component";
    (empty ? names : drawn).add(p.name.text);
  }
  return { names, drawn };
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
  const { names, drawn } = stubNames(stubSource);
  const { found, parsed } = usages(files, names);
  // Употребления НАРИСОВАННЫХ имён с видимым пропом — предмет второй половины:
  // именно у них обязана быть проба на продуктовой поверхности.
  const { found: drawnFound } = usages(files, drawn);
  return { names, drawn, found, drawnFound, parsed };
}

/**
 * Вторая половина как ЧИСТАЯ функция: её же зовёт `--self-test`, поэтому
 * доказательство способности упасть относится к тому самому коду, который
 * судит дерево, а не к его копии.
 */
function probeFindings({ drawn, drawnFound }, trackedProbes) {
  const out = [];
  for (const [kind, probe] of Object.entries(PROBED_BY)) {
    if (!drawn.has(kind)) {
      out.push(
        `перечень PROBED_BY держит «${kind}», а заменитель его НЕ рисует (снова проходной <div> либо имя ушло) — ` +
          `проба ${probe} утверждает о форме дублёра, а не о том, что видит оператор`,
      );
      continue;
    }
    if (!trackedProbes.has(probe)) {
      out.push(
        `перечень PROBED_BY называет пробу ${probe} для «${kind}», а в индексе git её нет — ` +
          `наблюдаемость есть, наблюдения нет`,
      );
      continue;
    }
    const users = drawnFound.filter((h) => h.name === kind);
    if (users.length === 0) {
      out.push(
        `перечень PROBED_BY держит «${kind}», но в продуктовом коде нет ни одного его употребления ` +
          `с видимым пропом — держать нечего, запись снять вместе с пробой ${probe}`,
      );
      continue;
    }
    const probeDir = path.dirname(probe);
    if (!users.some((h) => path.dirname(h.file) === probeDir)) {
      out.push(
        `проба ${probe} для «${kind}» не лежит рядом ни с одним его продуктовым потребителем ` +
          `(${users.map((h) => h.file).slice(0, 3).join(", ")}) — проба в стороне утверждает о заменителе, а не о продукте`,
      );
    }
  }
  return out;
}

/** Отслеживаемые пробы — из индекса git, а не с диска: файл, лежащий рядом, но
 *  не добавленный, до конвейера не доедет, и перечень утверждал бы о том, чего
 *  в дереве нет. */
function trackedProbeSet() {
  return new Set(
    execFileSync("git", ["ls-files", "*.test.ts", "*.test.tsx"], { cwd: uiRoot, encoding: "utf8" })
      .split("\n")
      .filter(Boolean),
  );
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
  // ВТОРАЯ ПОЛОВИНА, инъекция тем же способом и в обе стороны: вид из PROBED_BY
  // возвращается в проходной `<div>` — тогда названная проба утверждает о форме
  // дублёра, и перечень обязан это назвать. На законном заменителе — молчание.
  const tracked = trackedProbeSet();
  const kind = Object.keys(PROBED_BY)[0];
  const brokenKind = stubSource.replace(new RegExp(`\\n {4}${kind},\\n`), `\n    ${kind}: Component,\n`);
  if (brokenKind === stubSource) {
    console.error(
      `::error::самопроверка второй половины не смогла внести дефект: в перечне заменителя нет строки «    ${kind},» — ` +
        "предпосылка самопроверки исчезла вместе с формой файла, чинить надо её, а не гейт",
    );
    process.exit(1);
  }
  const redProbe = probeFindings(analyze(brokenKind, files), tracked).filter((f) => f.includes(`«${kind}»`));
  if (redProbe.length === 0) {
    console.error(`::error::самопроверка: возвращённый в <div> «${kind}» НЕ пойман второй половиной — она не способна упасть`);
    process.exit(1);
  }
  const greenProbe = probeFindings(green, tracked);
  if (greenProbe.length > 0) {
    console.error(`::error::самопроверка: вторая половина краснеет на ЗАКОННОМ дереве: ${greenProbe[0]}`);
    process.exit(1);
  }

  console.log(
    `самопроверка: с внесённым дефектом находок про Dropdown ${redHits.length} ` +
      `(первая — ${redHits[0].file}:${redHits[0].line}), на законном заменителе 0; ` +
      `вторая половина на возвращённом в <div> «${kind}» дала «${redProbe[0].slice(0, 80)}…», на законном дереве 0; ` +
      `осмотрено файлов ${green.parsed}, видов в PROBED_BY ${Object.keys(PROBED_BY).length}`,
  );
  process.exit(0);
}

const { names, drawn, found, drawnFound, parsed } = analyze(stubSource, files);

const trackedProbes = trackedProbeSet();

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

// ВТОРАЯ ПОЛОВИНА (#625): у приведённого вида есть проба на продуктовой поверхности.
findings.push(...probeFindings({ drawn, drawnFound }, trackedProbes));

console.log(
  `осмотрено: продуктовых .tsx ${parsed}, имён с пустым заменителем ${names.size}, ` +
    `нарисованных ${drawn.size}, пропов в закрытом списке ${VISIBLE_PROPS.size}; ` +
    `употреблений «пустое имя × видимый проп» ${found.length}, ` +
    `названо намеренно не рисующими ${Object.keys(NOT_DRAWN).length}; ` +
    `видов с пробой на продуктовой поверхности ${Object.keys(PROBED_BY).length}, ` +
    `отслеживаемых проб в дереве ${trackedProbes.size}`,
);

if (parsed === 0 || names.size === 0) {
  console.error(
    "::error::предпосылка гейта не выполнена: прочитано 0 файлов либо в заменителе нет ни одного пустого имени — " +
      "«ноль находок» здесь означало бы «ноль прочитанного»",
  );
  process.exit(1);
}
// Пустой NOT_DRAWN — ЦЕЛЬ (и он пуст сегодня). Пустой PROBED_BY — предпосылка:
// приведённые виды в дереве есть, и перечень без единой записи означал бы, что
// вторая половина гейта не судит ни о чём.
if (Object.keys(PROBED_BY).length === 0) {
  console.error(
    "::error::предпосылка гейта не выполнена: перечень PROBED_BY пуст — вторая половина не судит ни об одном виде",
  );
  process.exit(1);
}

if (findings.length) {
  for (const f of findings) console.error(`::error::${f}`);
  console.error(`\n✖ находок: ${findings.length}`);
  process.exit(1);
}

console.log(
  "✓ ни одно имя с пустым заменителем не получает в продуктовом коде видимого оператору пропа, " +
    "и каждый приведённый вид назван пробой на продуктовой поверхности",
);
