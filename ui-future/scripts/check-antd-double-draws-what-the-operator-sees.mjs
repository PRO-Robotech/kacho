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
 * ЧТО ПРОВЕРЯЕТСЯ — ТРИ ЧАСТИ ОДНОГО ПРЕДМЕТА.
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
 * ТРЕТЬЯ (#1225). Вид, приведённый к настоящей форме, может остаться БЕЗ
 * продуктового потребителя — носитель снят, приведение осталось. Тогда его
 * запись из `PROBED_BY` обязана уйти: пробы рядом с носителем не бывает, когда
 * носителя нет, и вторая часть говорит это сама («держать нечего»). Но
 * молчаливое снятие теряет предмет — заменитель продолжает рисовать, а обещание
 * «появится носитель, появится и проба» не держится ничем. Поэтому вид уходит в
 * перечень `AWAITING_CARRIER` с причиной, и первое же его употребление с видимым
 * пропом роняет прогон, требуя записи в `PROBED_BY` вместе с пробой.
 *
 * ЗНАМЕНАТЕЛЬ (#1265). `PROBED_BY` — ведомость ПРИНЕСЁННОГО, а не покрытие: в
 * неё попадает то, что кто-то однажды внёс, а не то, что употребляется. Строка
 * «видов с пробой 9» читалась как «9 из 9», тогда как верное прочтение — «9 из
 * 25». Числитель есть, величины, относительно которой считать, нет — тот же
 * класс, что «ноль находок неотличим от ноля прочитанного», только этажом выше.
 * Поэтому перепись называет ОБЕ величины и перечисляет непокрытые виды поимённо.
 *
 * Долг гейт НЕ роняет, и это решение, а не недосмотр: закрывается он пробами у
 * носителей — работой с отдельной ценой, — а не записями в перечне. Роняющий
 * долг гейт чинили бы записью, то есть ровно тем, из чего долг и вырос. Но и
 * молчать о нём нельзя: молчание читается как полнота.
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
  // Проба падает утверждением «называет радиус секрета прямо в окне выдачи»:
  // текст предупреждения приходит пропом `message`, и проходной <div> роняет
  // его в атрибут — оператор видит пустое окно.
  Alert: "shared/src/components/organisms/system/OneTimeSecretModal/OneTimeSecretModal.test.tsx",
  Badge: "shared/src/components/organisms/DetailShell/DetailShell.badge.test.tsx",
  // Падает на «копирует ответ края целиком» и «сериализация — с отступами»:
  // обе жмут кнопку по её РОЛИ, а проходной <div> кнопкой не является.
  Button: "shared/src/components/organisms/DetailShell/JsonTab.test.tsx",
  // Падает на «сводка направлений считает то, что в наборе есть» и «„Добавить
  // правило“ кладёт заготовку»: счётчик и действие живут в шапке карточки
  // (`title`/`extra`), а проходной <div> шапки не рисует вовсе.
  Card: "shared/src/components/organisms/form/SgRulesEditor/SgRulesEditor.test.tsx",
  // Падает на положительном контроле «флажки на странице находятся» — том
  // самом, без которого отрицание «группового удаления нет» зеленело бы на
  // экране без единого флажка.
  Checkbox: "shared/src/components/organisms/ResourceListPage/ResourceListPage.no-bulk-delete.test.tsx",
  Collapse: "shared/src/components/organisms/form/SgRulesEditor/SgRulesEditor.test.tsx",
  Dropdown: "shared/src/components/molecules/RowActionsMenu/RowActionsMenu.test.tsx",
  // Падает на «пустое дерево прямо говорит, что удалять можно»: объяснение
  // пустоты приходит пропом `description`.
  Empty: "shared/src/components/organisms/DependencyTreePanel/DependencyTreePanel.test.tsx",
  // Падает на пяти утверждениях правки пар «ключ→значение»: печатать некуда,
  // когда поле ввода подменено <div>.
  Input: "shared/src/components/molecules/EditableKVTable/EditableKVTable.test.tsx",
  Menu: "shared/src/components/organisms/DetailShell/DetailShell.test.tsx",
  // Падает на одиннадцати утверждениях окна построчного глагола: заголовок и
  // кнопки согласия/отказа рисует само окно, а не его содержимое.
  Modal: "shared/src/components/molecules/RowActionsMenu/RowActionsMenu.rowverbs.test.tsx",
  // Переехало вслед за предметом: панель ключей сведена к тонкой обёртке и
  // `Popconfirm` больше не рисует — его рисует общая реализация, и проба
  // теперь лежит рядом с НЕЙ. Прежняя запись пережила свой предмет, и гейт
  // это назвал сам, второй половиной самопроверки.
  Popconfirm: "iam/src/components/organisms/TokensPanel/TokensPanel.test.tsx",
  // Падает на трёх утверждениях про снятие выбранного адреса при смене режима:
  // сами режимы приходят пропом `options`, и выбрать нечего, когда их не рисуют.
  Segmented: "shared/src/components/organisms/form/NicSpecFields/NicSpecFields.test.tsx",
  // Падает на паре «своя ветвь у шлюза» + положительный контроль «адрес
  // открывается полем адреса»: ветвь выбирают списком, чьи варианты приходят
  // пропом `options`.
  Select: "shared/src/components/organisms/RoutesPanel/RoutesPanel.test.tsx",
  Spin: "shared/src/components/organisms/ResourceEditPage/ResourceEditPage.loading-caption.test.tsx",
  Statistic: "shared/src/pages/DashboardPage.statistic.test.tsx",
  // Падает на семи утверждениях журнала операций: строки, столбцы и текст
  // отказа приходят пропами `dataSource`/`columns`, а не детьми.
  Table: "shared/src/components/molecules/OperationsTable/OperationsTable.test.tsx",
  Tabs: "iam/src/pages/iam/AccessPage/AccessPage.role-tabs.test.tsx",
  Tree: "shared/src/components/organisms/DependencyTreePanel/DependencyTreePanel.test.tsx",
};

/**
 * Виды, приведённые к настоящей форме, у которых СЕГОДНЯ нет ни одного
 * продуктового потребителя, — с причиной по каждому.
 *
 * ЗАЧЕМ ОТДЕЛЬНЫЙ ПЕРЕЧЕНЬ, А НЕ ПРОСТО СНЯТИЕ ЗАПИСИ. Вид, приведённый ради
 * наблюдаемости, но никем не употребляемый, из `PROBED_BY` обязан уйти: пробу
 * рядом с носителем не написать, когда носителя нет, и гейт говорит это сам
 * («держать нечего»). Но молчаливое снятие теряет предмет: заменитель
 * продолжает рисовать вид, а обещание «появится носитель — появится и проба»
 * не держится ничем. Здесь оно держится ЛОВУШКОЙ: запись падает ровно в тот
 * день, когда носитель возвращается.
 *
 * Перечень счётный и самоистекающий по двум осям: запись падает, если
 * (а) продукт СНОВА употребляет вид с видимым пропом — тогда её место в
 * `PROBED_BY` вместе с пробой рядом с носителем, (б) заменитель перестал вид
 * рисовать — тогда ждать возврата нечему.
 *
 * Пустой перечень — ЦЕЛЬ, а не поломка: на нём гейт проходит.
 */
const AWAITING_CARRIER = {
  // Носителем был перечень ключей доступа на странице параметров личности
  // (`shared/src/pages/auth/Settings.tsx`). Каталог из семи страниц церемоний
  // сняли целиком (a85792df0, #1225): ни один из 162 маршрутов дерева их не
  // монтировал, а те же адреса раздача отдаёт внешнему поставщику личности.
  // Проба перечня уехала вместе с ними — и правильно: она утверждала о
  // странице, которой оператор не видел никогда.
  //
  // Заменитель `List` при этом рисовать НЕ перестал, и снимать его не за что:
  // форма верна и ждёт первого настоящего потребителя.
  //
  // Предикат возврата — ловушка ниже: первое же `<List>` с видимым пропом в
  // продуктовом коде уронит прогон и потребует записи в `PROBED_BY`.
  List:
    "носитель (страница параметров личности) снят вместе с каталогом церемоний как не монтируемый ни одним маршрутом — a85792df0, #1225",
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
function usages(files, names, extraSources = []) {
  const found = [];
  let parsed = 0;
  const inputs = [...files.map((f) => [f, null]), ...extraSources];
  for (const [f, virtualSrc] of inputs) {
    const src = virtualSrc === null ? fs.readFileSync(path.join(uiRoot, f), "utf8") : virtualSrc;
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
function analyze(stubSource, files, extraSources = []) {
  const { names, drawn } = stubNames(stubSource);
  const { found, parsed } = usages(files, names, extraSources);
  // Употребления НАРИСОВАННЫХ имён с видимым пропом — предмет второй половины:
  // именно у них обязана быть проба на продуктовой поверхности.
  const { found: drawnFound } = usages(files, drawn, extraSources);
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

/**
 * ЗНАМЕНАТЕЛЬ ПОКРЫТИЯ (#1265). Тоже чистая функция, и её же зовёт `--self-test`.
 *
 * `PROBED_BY` — ведомость ПРИНЕСЁННОГО, а не покрытие: в неё попадает то, что
 * кто-то однажды внёс, а не то, что употребляется. Строка «видов с пробой 9»
 * читается как «9 из 9», тогда как верное прочтение — «9 из стольких-то». Это
 * тот же класс, что «ноль находок неотличим от ноля прочитанного», только на
 * уровне знаменателя: числитель есть, величины, относительно которой считать,
 * нет.
 *
 * Знаменатель считается ПО ДЕРЕВУ: вид, нарисованный заменителем и хотя бы раз
 * употреблённый продуктом с видимым оператору свойством. Тот же вид без такого
 * свойства в знаменатель не входит — рисовать ему нечего, и проба у него была
 * бы о форме дублёра.
 *
 * Функция НИЧЕГО не роняет: разрыв между числителем и знаменателем — долг, а не
 * находка, и закрывается он пробами, которых сегодня нет. Предмет здесь —
 * сделать долг ВИДИМЫМ; молчаливый числитель создавал впечатление полноты.
 */
function coverageCensus({ drawn, drawnFound }) {
  const inUse = [...new Set(drawnFound.map((h) => h.name))].filter((k) => drawn.has(k)).sort();
  const probed = inUse.filter((k) => k in PROBED_BY);
  const unprobed = inUse.filter((k) => !(k in PROBED_BY));
  return { inUse, probed, unprobed };
}

/**
 * ТРЕТЬЯ ЧАСТЬ — ЛОВУШКА НА ВОЗВРАТ НОСИТЕЛЯ. Тоже чистая функция, и её же
 * зовёт `--self-test`: доказательство способности упасть относится к тому
 * самому коду, который судит дерево.
 *
 * Вид, приведённый к настоящей форме, но никем не употребляемый, из `PROBED_BY`
 * уходит — пробы рядом с носителем не бывает, когда носителя нет. Уходя, он
 * попадает СЮДА, и первое же его употребление с видимым пропом роняет прогон:
 * тогда наблюдаемость снова есть что держать, и держать её обязана проба.
 */
function awaitingFindings({ drawn, drawnFound }) {
  const out = [];
  for (const [kind, reason] of Object.entries(AWAITING_CARRIER)) {
    if (kind in PROBED_BY) {
      out.push(
        `«${kind}» назван сразу в PROBED_BY и в AWAITING_CARRIER — два места об одном предмете, ` +
          `из которых верно одно; оставить то, что описывает дерево`,
      );
      continue;
    }
    if (!drawn.has(kind)) {
      out.push(
        `перечень AWAITING_CARRIER ждёт носителя для «${kind}», а заменитель его уже НЕ рисует ` +
          `(снова проходной <div> либо имя ушло) — ждать возврата нечему, запись снять`,
      );
      continue;
    }
    const users = drawnFound.filter((h) => h.name === kind);
    if (users.length) {
      out.push(
        `у «${kind}» СНОВА есть продуктовый носитель (${users
          .map((h) => `${h.file}:${h.line}`)
          .slice(0, 3)
          .join(", ")}) — запись из AWAITING_CARRIER снять и внести «${kind}» в PROBED_BY ` +
          `вместе с пробой РЯДОМ с носителем; причина ожидания была: ${reason}`,
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
  // Вид для инъекции берётся НЕ как первый ключ перечня, а как первый ключ,
  // объявленный в заменителе СОКРАЩЁННОЙ формой (`    Kind,`), — только её
  // подмена на `Component` выражается одной строкой.
  //
  // Прежняя редакция брала `Object.keys(PROBED_BY)[0]` и тем молча зависела от
  // того, что первый ключ окажется сокращённым. Предпосылка держалась
  // алфавитом: пока первым был `Badge`, всё сходилось. Перечень пополнился по
  // #1285, первым стал `Alert` — объявленный ПОЛНОЙ формой, — и самопроверка
  // перестала вносить дефект, сообщив об этом сама. То есть способность второй
  // половины упасть переставала доказываться от одной лишь записи в перечне,
  // и заметить это можно было только прогоном.
  //
  // Предпосылка теперь ВЫРАЖЕНА: если сокращённых ключей не осталось ни одного,
  // самопроверка говорит это прямо, а не выдаёт молчание за доказательство.
  const shorthandInStub = (k) => new RegExp(`\\n {4}${k},\\n`).test(stubSource);
  const kind = Object.keys(PROBED_BY).find(shorthandInStub);
  if (kind === undefined) {
    console.error(
      "::error::самопроверка второй половины не смогла внести дефект: ни один вид из PROBED_BY не объявлен " +
        "в заменителе сокращённой формой «    Kind,» — вносить дефект одной строкой не во что; " +
        "чинить надо самопроверку (научить её полной форме), а не гейт",
    );
    process.exit(1);
  }
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
  // ТРЕТЬЯ ЧАСТЬ, инъекция тем же способом и в обе стороны. Дефект вносится
  // НАСТОЯЩИМ входом — исходником `.tsx`, который разбирается тем же путём, что
  // и файлы дерева; на диск он не кладётся, потому что предмет ловушки — то,
  // чего в дереве ещё нет. Законный близнец рядом: тот же вид БЕЗ видимого
  // пропа носителем не является и молчать обязан.
  const awaitKinds = Object.keys(AWAITING_CARRIER);
  if (awaitKinds.length) {
    const awaited = awaitKinds[0];
    const carrier = [
      "__self-test__/CarrierReturned.tsx",
      `export const CarrierReturned = () => <${awaited} dataSource={[]} renderItem={() => null} />;\n`,
    ];
    const bystander = [
      "__self-test__/LawfulTwin.tsx",
      `export const LawfulTwin = () => <${awaited} className="x" />;\n`,
    ];
    const redAwait = awaitingFindings(analyze(stubSource, files, [carrier]));
    if (!redAwait.some((f) => f.includes(`«${awaited}»`))) {
      console.error(
        `::error::самопроверка: вернувшийся носитель «${awaited}» НЕ пойман третьей частью — ловушка не способна упасть`,
      );
      process.exit(1);
    }
    const greenAwait = awaitingFindings(analyze(stubSource, files, [bystander]));
    if (greenAwait.length > 0) {
      console.error(
        `::error::самопроверка: третья часть краснеет на ЗАКОННОМ близнеце (${awaited} без видимого пропа): ${greenAwait[0]}`,
      );
      process.exit(1);
    }
    const greenTree = awaitingFindings(green);
    if (greenTree.length > 0) {
      console.error(`::error::самопроверка: третья часть краснеет на ЗАКОННОМ дереве: ${greenTree[0]}`);
      process.exit(1);
    }
    // Вторая ось того же перечня: заменитель перестал рисовать ожидаемый вид —
    // ждать возврата нечему.
    const unDrawn = stubSource.replace(new RegExp(`\\n {4}${awaited},\\n`), `\n    ${awaited}: Component,\n`);
    if (unDrawn === stubSource) {
      console.error(
        `::error::самопроверка третьей части не смогла внести дефект: в перечне заменителя нет строки «    ${awaited},» — ` +
          "предпосылка самопроверки исчезла вместе с формой файла, чинить надо её, а не гейт",
      );
      process.exit(1);
    }
    if (!awaitingFindings(analyze(unDrawn, files)).some((f) => f.includes(`«${awaited}»`))) {
      console.error(
        `::error::самопроверка: возвращённый в <div> «${awaited}» НЕ пойман третьей частью — вторая её ось не способна упасть`,
      );
      process.exit(1);
    }
  }

  // ── ЗНАМЕНАТЕЛЬ ПОКРЫТИЯ (#1265), инъекция в обе стороны ───────────────────
  // Числитель без знаменателя читается как «9 из 9». Здесь доказывается, что
  // знаменатель считается ПО ДЕРЕВУ, что вид без пробы называется по имени и что
  // тот же вид без видимого свойства в знаменатель не попадает.
  const treeCoverage = coverageCensus(green);
  const unprobedKind = "Descriptions";
  if (unprobedKind in PROBED_BY) {
    console.error(
      `::error::самопроверка знаменателя: «${unprobedKind}» уже назван в PROBED_BY — ` +
        "предпосылка инъекции исчезла, взять для неё другой вид без пробы",
    );
    process.exit(1);
  }
  if (!green.drawn.has(unprobedKind)) {
    console.error(
      `::error::самопроверка знаменателя: заменитель не рисует «${unprobedKind}» — ` +
        "предпосылка инъекции исчезла вместе с формой файла, чинить надо её, а не гейт",
    );
    process.exit(1);
  }
  if (treeCoverage.inUse.includes(unprobedKind)) {
    console.error(
      `::error::самопроверка знаменателя: «${unprobedKind}» УЖЕ употребляется деревом — ` +
        "инъекция перестала создавать условие и доказывала бы наличие того, что и так есть; " +
        "взять для неё вид, которого в знаменателе нет",
    );
    process.exit(1);
  }
  const unprobedCarrier = [
    "__self-test__/UnprobedCarrier.tsx",
    `export const UnprobedCarrier = () => <${unprobedKind} items={[]} />;\n`,
  ];
  const withCarrier = coverageCensus(analyze(stubSource, files, [unprobedCarrier]));
  if (!withCarrier.unprobed.includes(unprobedKind)) {
    console.error(
      `::error::самопроверка: употреблённый вид «${unprobedKind}» БЕЗ пробы не попал в долг — ` +
        "знаменатель не считается по дереву, и «9 из 9» вернётся",
    );
    process.exit(1);
  }
  if (withCarrier.inUse.length <= withCarrier.probed.length) {
    console.error(
      `::error::самопроверка: знаменатель (${withCarrier.inUse.length}) не превысил числителя ` +
        `(${withCarrier.probed.length}) при заведомо непокрытом виде`,
    );
    process.exit(1);
  }
  // Законный близнец: тот же вид БЕЗ видимого свойства носителем не является.
  const unprobedTwin = [
    "__self-test__/UnprobedTwin.tsx",
    `export const UnprobedTwin = () => <${unprobedKind} className="x" />;\n`,
  ];
  const withTwin = coverageCensus(analyze(stubSource, files, [unprobedTwin]));
  if (withTwin.inUse.includes(unprobedKind)) {
    console.error(
      `::error::самопроверка: «${unprobedKind}» без видимого свойства попал в знаменатель — ` +
        "гейт считает форму, а не то, что видит оператор",
    );
    process.exit(1);
  }
  // И обратная сторона: числитель не берётся из одного перечня. Вид, названный в
  // PROBED_BY, но не употребляемый деревом, в числитель не входит — иначе долг
  // уменьшался бы записью в перечне, а не пробой у носителя.
  if (!treeCoverage.probed.every((k) => k in PROBED_BY && treeCoverage.inUse.includes(k))) {
    console.error("::error::самопроверка: числитель считается по перечню в обход дерева");
    process.exit(1);
  }
  if (treeCoverage.probed.length > treeCoverage.inUse.length) {
    console.error(
      `::error::самопроверка: числитель (${treeCoverage.probed.length}) больше знаменателя ` +
        `(${treeCoverage.inUse.length}) — величины считаются по разным множествам`,
    );
    process.exit(1);
  }

  console.log(
    `самопроверка: с внесённым дефектом находок про Dropdown ${redHits.length} ` +
      `(первая — ${redHits[0].file}:${redHits[0].line}), на законном заменителе 0; ` +
      `вторая половина на возвращённом в <div> «${kind}» дала «${redProbe[0].slice(0, 80)}…», на законном дереве 0; ` +
      `третья часть проверена на ${awaitKinds.length} видах, ждущих носителя (обе оси + законный близнец); ` +
      `знаменатель покрытия проверен в обе стороны на «${unprobedKind}» (в долг попал, без видимого свойства — нет); ` +
      `осмотрено файлов ${green.parsed}, видов в PROBED_BY ${Object.keys(PROBED_BY).length}, ` +
      `в AWAITING_CARRIER ${awaitKinds.length}`,
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
// ТРЕТЬЯ ЧАСТЬ: вид, ждущий носителя, обязан упасть в день его возврата.
findings.push(...awaitingFindings({ drawn, drawnFound }));

const coverage = coverageCensus({ drawn, drawnFound });

console.log(
  `осмотрено: продуктовых .tsx ${parsed}, имён с пустым заменителем ${names.size}, ` +
    `нарисованных ${drawn.size}, пропов в закрытом списке ${VISIBLE_PROPS.size}; ` +
    `употреблений «пустое имя × видимый проп» ${found.length}, ` +
    `названо намеренно не рисующими ${Object.keys(NOT_DRAWN).length}; ` +
    `видов, ждущих возврата носителя ${Object.keys(AWAITING_CARRIER).length}, ` +
    `отслеживаемых проб в дереве ${trackedProbes.size}`,
);

// ОБЕ ВЕЛИЧИНЫ, а не одна (#1265). Числитель без знаменателя читается как
// «покрыто всё»: перечень PROBED_BY — ведомость принесённого, и одно число из
// него создавало впечатление полноты. Долг называется вслух и поимённо: он
// закрывается пробами у носителей, а не записями в перечне.
console.log(
  `покрытие пробами на продуктовой поверхности: ${coverage.probed.length} из ${coverage.inUse.length} ` +
    `(видов, нарисованных заменителем, ${drawn.size}; из них употребляются продуктом с видимым ` +
    `оператору свойством ${coverage.inUse.length})`,
);
if (coverage.unprobed.length) {
  console.log(
    `ДОЛГ: без пробы у носителя ${coverage.unprobed.length} видов — ${coverage.unprobed.join(", ")}`,
  );
  console.log(
    "  (это долг, а не фон: гейт его не роняет, потому что закрывается он пробами, " +
      "которых сегодня нет; но и молчать о нём нельзя — молчание читается как полнота)",
  );
} else {
  console.log("ДОЛГА нет: каждый употребляемый вид назван пробой у своего носителя");
}

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
    "и каждая запись перечня PROBED_BY описывает дерево (вид рисуется, проба есть и лежит у носителя). " +
    "О ПОЛНОТЕ покрытия это не говорит — её называет строка «покрытие пробами» выше",
);
