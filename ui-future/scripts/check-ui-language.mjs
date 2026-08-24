#!/usr/bin/env node
// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

/**
 * Гейт языка консоли: подпись, видимая пользователю, не бывает переводимым
 * английским словосочетанием.
 *
 * Предмет (продукт #478). Интерфейс русский, а названия сущностей в нём стояли
 * английские — и стояли НЕПОСЛЕДОВАТЕЛЬНО: в одном месте «Группы безопасности»,
 * строкой ниже `Security Group`; колонка `External ID` в таблице, где соседние
 * подписи «Эл. почта», «Отображаемое имя», «Статус», «Аккаунт», «Создан».
 * Пользователь читает это как разные предметы, хотя предмет один. Отдельный вид
 * того же — гибрид «Целевые группы (Target Groups)»: английское имя не заменяет
 * русское, а дублирует, удлиняя строку вдвое и ничего не сообщая.
 *
 * ── Что проверяется ─────────────────────────────────────────────────────────
 *
 * Правил ДВА, и второе существует потому, что первое намеренно не судит прозу.
 *
 * **Правило 1 — имя не бывает английским.** Строковый литерал в позиции ПОДПИСИ
 * (см. `LABEL_KEYS`) и текст JSX. Каждое латинское слово в нём обязано быть либо
 * принятым термином (`TERMS`), либо частью машинного идентификатора (см.
 * `looksMachine`), либо строка целиком объявлена дословной (`VERBATIM`). Иначе —
 * находка. Наличие кириллицы рядом латинское слово не оправдывает: иначе гибрид
 * «Целевые группы (Target Groups)» прошёл бы.
 *
 * **Правило 2 — имя РЕСУРСА Kachō по-английски запрещено везде** (`BANNED`),
 * включая прозу. Оно закрывает ровно ту дыру, которую оставляет исключение прозы
 * в правиле 1: перечисление «LoadBalancer, Listener, Target Group» внутри
 * русского предложения длиннее `PROSE_WORDS` слов, и без правила 2 гейт молчал
 * бы на нём — а именно с него заведена задача #478. Найдено зеркальной формой
 * предиката, а не рассуждением: первая редакция гейта это место пропускала.
 *
 * ── Подпись, ПРИШЕДШАЯ ВЫЧИСЛЕНИЕМ ──────────────────────────────────────────
 *
 * Подпись не обязана стоять литералом в разметке. Её выносят в переменную,
 * выбирают тернарником, собирают шаблоном, подставляют через `||` — и это
 * обычный, правильный приём, а не обход гейта. Первая редакция читала ТОЛЬКО
 * прямой литерал, поэтому перенос строки в переменную снимал проверку МОЛЧА:
 * гейт продолжал печатать тысячи прочитанных подписей и выглядел работающим.
 *
 * Класс найден не рассуждением, а собственным самоистечением словаря. Подпись
 * «Приватный ключ (PEM, PKCS#8)» переехала из текста JSX в `const valueLabel =
 * …`, единственное вхождение термина `pem` пропало из поля зрения — и гейт
 * потребовал снять термин из `TERMS`. Диагноз «снять» был бы МАСКИРОВКОЙ
 * потери покрытия: словарь опустел бы, гейт позеленел, а подписи в переменных
 * так и остались бы непроверяемыми. Замер по дереву на тот момент: подписей
 * вне наблюдения — 165 позиций, 209 строк.
 *
 * Поэтому значение в позиции подписи РАЗРЕШАЕТСЯ (`literalsOf`): тернарник
 * даёт обе ветви, `||`/`??` — обе стороны, шаблон и склейка — свой текст с
 * подстановкой на месте вычисляемой части, имя — то, чем оно объявлено в этом
 * же файле. Вызов и обращение к полю не дают ничего: там данные, а не подпись.
 *
 * ── Почему это НЕ расширяет находки на данные ───────────────────────────────
 *
 * Различитель — ПОЗИЦИЯ, и она не менялась. Что считать подписью, гейт решил
 * раньше (`LABEL_KEYS`, `LABEL_ATTRS_ONLY`, `LABEL_HELPERS`, текст JSX);
 * глубже стало только разрешение ЗНАЧЕНИЯ в уже объявленной позиции. Что бы
 * ни лежало в переменной, стоящей в `label=`, — это и есть подпись.
 *
 * У текста JSX граница проведена отдельно и строже: выражение судится, только
 * если оно ЕДИНСТВЕННОЕ содержимое своего элемента. `<Text>{valueLabel}</Text>`
 * — текст элемента целиком, подпись. `Скачать .{valueExt}` — значение,
 * вставленное в фразу: сама фраза уже прочитана как текст JSX, а вставка несёт
 * расширение файла (`txt`, `pem`), имя поля, путь. Судить её значило бы завести
 * находки там, где переводить нечего. Плюс `VERBATIM_CONTENT_TAGS`: содержимое
 * `<style>`/`<pre>`/`<code>` верно́ дословно — без этого исключения лист стилей,
 * вставленный выражением, читался бы как подпись длиной в семь килобайт.
 *
 * ── Чего гейт НЕ читает, и почему это решение, а не упущение ────────────────
 *
 *  · `placeholder` — несёт ПРИМЕР ЗНАЧЕНИЯ (`my-instance`, `user@example.com`,
 *    `ru-central1-a`, `grpc.health.v1.Health`), а значения не переводятся.
 *    Перепись таких площадок печатается отдельной строкой, чтобы исключение
 *    было видно числом, а не подразумевалось.
 *  · `name` — имя поля формы и HTML-атрибут, пользователю не показывается.
 *  · `index.html` — заголовок вкладки браузера (`<title>`) в корне каждого
 *    модуля. Гейт читает `<модуль>/src` и только `.ts`/`.tsx`, потому что судит
 *    РАЗОБРАННОЕ дерево, а у разметки страницы разбора здесь нет. Граница
 *    названа, а не умолчана: таких заголовков десять, и они между собой не
 *    согласованы — три написания бренда и два внутренних имени сборки в
 *    тексте, который видит пользователь. Это отдельный предмет; чинить его
 *    расширением ЭТОГО гейта значило бы завести в нём вторую, нечитающую
 *    дерево полосу.
 *  · тесты (`*.test.*`, `*.spec.*`, `test/`, `__tests__/`) — там английский
 *    литерал бывает ФИКСТУРОЙ, то есть предметом проверки, а не подписью.
 *  · ПОЯСНИТЕЛЬНАЯ ПРОЗА — строка длиннее `PROSE_WORDS` слов. Гейт стережёт
 *    ИМЕНА («Группа безопасности» вместо `Security Group`), а не абзацы
 *    помощи, где английский термин бывает цитатой значения контракта
 *    («меняется на STOPPED», «least-privilege»). Смешивать их в одном
 *    предикате нельзя: половина находок была бы законной, и первый же ложный
 *    срабат отключил бы гейт целиком. Проза — отдельный предмет, и она
 *    названа числом в переписи, а не умолчана.
 *
 * ── Почему разбор AST, а не поиск по тексту ─────────────────────────────────
 *
 * Регулярное выражение по исходнику находит `Security Group` в комментарии,
 * который эту же проверку и объясняет, — в том числе в шапке этого файла.
 * Читается только исполняемая часть: литерал в позиции подписи и текст JSX.
 *
 * ── Самоистечение ──────────────────────────────────────────────────────────
 *
 * `TERMS` и `VERBATIM` — ЗАКРЫТЫЕ перечни. Запись, которой в дереве больше
 * нечего исключать, — находка: иначе послабление переживает свой предмет и
 * унаследует следующую слепую зону.
 *
 * ── Проверка своей предпосылки ─────────────────────────────────────────────
 *
 * Гейт обоснован тем, что дерево консоли существует и содержит подписи. Ноль
 * прочитанных файлов, ноль осмотренных площадок — ПАДЕНИЕ, а не тихое «ноль
 * находок». Объём осмотренного печатается всегда — и подписи в разметке, и
 * подписи в вычисляемых значениях РАЗДЕЛЬНО. Одно число скрыло бы ровно то,
 * ради чего вторая полоса заведена: перепись росла бы вместе с деревом, а
 * доля непроверяемого оставалась бы невидимой. Отдельной строкой названа и
 * ГРАНИЦА — позиции подписи, значение которых приходит из данных: там текста
 * нет, и резолвить нечего.
 *
 * Запуск из ui-future/:  node scripts/check-ui-language.mjs
 *              инъекция: node scripts/check-ui-language.mjs --self-test
 * Выход ненулевой — есть находки.
 */

import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";

import ts from "typescript";

// ─────────────────────────────────────────────────────────────────────────────
// Позиции, чей строковый литерал видит пользователь.
// ─────────────────────────────────────────────────────────────────────────────

/** Имена свойств объекта и атрибутов JSX, несущие ПОДПИСЬ. */
const LABEL_KEYS = new Set([
  "label",
  "header",
  "title",
  "menuTitle",
  "singular",
  "plural",
  "genitive",
  "accusative",
  "serviceTitle",
  "allLabel",
  "description",
  "tooltip",
  "okText",
  "cancelText",
  "emptyText",
  "addLabel",
]);

/**
 * Только атрибуты JSX: `FieldLabel` — единая подпись поля формы (текст +
 * пояснение в подсказке). Как СВОЙСТВО объекта эти же имена значат другое
 * (`info: "var(--toast-info-accent)"` — цвет), поэтому перечень раздельный.
 */
const LABEL_ATTRS_ONLY = new Set(["text", "info"]);

/** Читается отдельно — только ради переписи (см. шапку, «чего гейт не читает»). */
const VALUE_KEYS = new Set(["placeholder"]);

/**
 * Помощники, СОБИРАЮЩИЕ подпись из аргументов. Без них подпись, переданная
 * вызовом (`label={labelWithInfo("Имя", "…")}`), гейту не видна вовсе — а
 * именно так записана подсказка, с которой заведена задача #478.
 */
const LABEL_HELPERS = new Set(["labelWithInfo"]);

/** Строка длиннее — пояснительная проза, не имя (см. шапку). */
const PROSE_WORDS = 5;

// ─────────────────────────────────────────────────────────────────────────────
// TERMS — принятые латинские токены. Закрытый перечень, самоистекающий.
//
// Критерий приёма ОДИН: у термина нет устоявшегося русского эквивалента, и
// перевод сделал бы подпись менее понятной, а не более. Переводимое слово
// (`Name`, `Add`, `Created at`, `External ID`) сюда не попадает никогда.
//
// СНЯТИЕ ЗАПИСИ — НЕ СПОСОБ ПОГАСИТЬ КРАСНОЕ. Отказ «словарю нечего исключать»
// означает одно из двух, и различить их обязательно:
//   (а) термин действительно исчез из подписей — тогда снимать;
//   (б) гейт перестал ВИДЕТЬ подпись, в которой он стоит, — тогда чинить гейт,
//       потому что снятие в этом случае маскирует потерю покрытия: словарь
//       опустеет, гейт позеленеет, а подписи так и останутся непроверяемыми.
// Различает вопрос: где термин лежит СЕГОДНЯ — в подписи или в значении?
// Оба случая уже были, и в один день. `pem` ушёл по-настоящему: он остался
// только расширением файла в подстановке («Скачать .{ext}») — то есть
// значением, а не словом подписи, и заглавное `PEM` в «Приватный ключ (PEM,
// PKCS#8)» объясняется ФОРМОЙ, словарь ему не нужен. А `passkey` был назван
// иначе, чем тот же способ входа в соседнем окне («ключом доступа»), — то есть
// его исчезновение из словаря оплачено СОГЛАСОВАНИЕМ имени, а не умолчанием.
// Тем же отказом обнаружились 165 позиций живых подписей, вынесенных в
// переменные и тернарники и невидимых гейту; чинилось это `literalsOf`.
// Порядок был именно такой: сперва научить видеть, потом снимать.
// ─────────────────────────────────────────────────────────────────────────────
const TERMS = new Map([
  ["kid", "имя поля JWK (идентификатор ключа) — показывается как в стандарте"],
  ["cloud-init", "имя механизма первичной настройки машины"],
  ["user-data", "имя поля cloud-init"],
  ["docker", "имя стороннего инструмента в примере команды"],
  ["pull", "подкоманда docker — часть команды, которую набирает пользователь"],
  ["push", "подкоманда docker — часть команды, которую набирает пользователь"],
  ["json", "имя формата файла (расширение)"],
  ["helm", "имя стороннего инструмента (тип артефакта реестра)"],
  ["kacho", "бренд"],
]);

// ─────────────────────────────────────────────────────────────────────────────
// VERBATIM — строки целиком, показываемые дословно. Закрытый перечень,
// самоистекающий. У каждой записи — причина, а не «так исторически».
//
// §Разделы. Раздел консоли назван доменом Kachō — тем же именем, каким домен
// назван в контракте (`kacho.cloud.compute.v1`), в пути REST (`/compute/v1/…`),
// в каталоге дерева (`services/compute/`) и в документации. Это НЕ «оставили
// по-английски»: подпись раздела и есть имя предмета в остальных пяти местах
// продукта, и перевод развёл бы их. Раздел `system` — исключение, которое
// правило подтверждает: он не домен Kachō, а собственная админ-область
// консоли, поэтому назван по-русски («Администрирование»).
// ─────────────────────────────────────────────────────────────────────────────
const VERBATIM = new Map([
  ["Compute", "имя домена Kachō (раздел консоли), см. §Разделы"],
  ["Storage", "имя домена Kachō (раздел консоли), см. §Разделы"],
  ["Registry", "имя домена Kachō (раздел консоли), см. §Разделы"],
  ["Geography", "имя домена Kachō (раздел консоли), см. §Разделы"],
  ["Load Balancer", "имя домена Kachō (раздел консоли), см. §Разделы"],
  ["Virtual Private Cloud", "расшифровка аббревиатуры VPC в заголовке группы меню"],
  ["Identity and Access Management", "расшифровка аббревиатуры IAM в заголовке группы меню"],
  ["Kratos", "имя стороннего компонента на служебной странице доступности"],
  ["Hydra", "имя стороннего компонента на служебной странице доступности"],
  ["cluster-admin", "имя системной роли — набирается дословно, перевод сделал бы его несуществующим"],
]);

/**
 * BANNED — английские имена РЕСУРСОВ Kachō. Запрещены ВЕЗДЕ, включая прозу.
 *
 * Это второе правило, и оно нужно ровно потому, что первое прозу не судит.
 * Гибрид «Целевые группы (Target Groups)» и перечисление «LoadBalancer,
 * Listener, Target Group» внутри русского предложения — то, с чего заведена
 * задача #478, и по длине они попадают в прозу. Здесь список ЗАПРЕТА, а не
 * послабления, поэтому самоистечения у него нет: запись, которой нечего
 * находить, — это успех, а не находка.
 *
 * Русское имя каждого — в `shared/src/lib/entity-names.ts` (`ENTITIES`).
 * Исключение — строка целиком в `VERBATIM` (имя раздела «Load Balancer»).
 */
const BANNED = [
  "Security Group",
  "SecurityGroup",
  "Network Interface",
  "NetworkInterface",
  "Route Table",
  "RouteTable",
  "Address Pool",
  "AddressPool",
  "Load Balancer",
  "LoadBalancer",
  "Target Group",
  "TargetGroup",
  "Machine Type",
  "MachineType",
  "Disk Type",
  "DiskType",
  "Access Binding",
  "AccessBinding",
  "Service Account",
  "ServiceAccount",
];

// ─────────────────────────────────────────────────────────────────────────────
// Классификатор.
// ─────────────────────────────────────────────────────────────────────────────

const CYRILLIC = /[А-Яа-яЁё]/;
/** Латиница вместе с диакритикой: `ō` в «Kachō» — такая же буква слова. */
const LATIN = "A-Za-z\\u00C0-\\u024F";
const TOKEN = new RegExp(`[${LATIN}0-9_.:/@#=-]+`, "g");
const HAS_LATIN = new RegExp(`[${LATIN}]`);
const EDGE = new RegExp(`^[^${LATIN}0-9]+|[^${LATIN}0-9]+$`, "g");
/** Сущности разметки (`&nbsp;`, `&quot;`) — не слова; до разбора снимаются. */
const ENTITY = /&[a-zA-Z]+;|&#\d+;/g;

/**
 * Машинный ли это токен — идентификатор, путь, значение перечисления, образец.
 *
 * Признаки, каждого достаточно: цифра внутри (`IPv4`, `region-1`, `sha256`);
 * внутренний разделитель пути или имени (`grpc.health.v1`, `system_admin`,
 * `/healthz`, `user@example.com`, `cluster:root`); горб верблюда
 * (`matchLabels`, `authoredVerbs`, `scopeId`). Прозы такой формы не бывает.
 */
function looksMachine(token) {
  if (/\d/.test(token)) return true;
  if (new RegExp(`[${LATIN}0-9][._:/@#=][${LATIN}0-9]`).test(token)) return true;
  if (/[a-z][A-Z]/.test(token)) return true;
  // ЗАГЛАВНЫМИ — либо аббревиатура (`CIDR`, `NIC`, `JSON`, `IAM`), либо
  // дословное значение перечисления контракта (`INGRESS`, `STOPPED`,
  // `REGIONAL`, `UP`). И то и другое показывается латиницей ПО ЗАМЫСЛУ: имя
  // протокола не переводят, а значение — это то, что вернул сервер.
  //
  // Цена названа честно: подпись, целиком набранная заглавными, гейт пропустит,
  // даже если она переводима (`NLB TG`). Обратный размен был бы дороже —
  // словарь всех аббревиатур и всех значений всех перечислений семи доменов
  // отставал бы от контракта и краснел бы на каждом новом значении.
  if (/[A-Z]/.test(token) && !/[a-z]/.test(token) && (token.match(/[A-Z]/g) ?? []).length >= 2) return true;
  return false;
}

/** Проза ли это — пояснение, а не имя (см. шапку). */
function isProse(text) {
  return text.split(/\s+/).filter(Boolean).length > PROSE_WORDS;
}

/**
 * Разбор одной строки. Возвращает `null`, если строка законна, иначе — список
 * слов, из-за которых она находка. Побочно отмечает использованные послабления.
 */
function adjudicate(value, usedTerms, usedVerbatim) {
  const text = value.replace(ENTITY, " ").trim();
  if (text === "") return null;
  if (VERBATIM.has(value.trim())) {
    usedVerbatim.add(value.trim());
    return null;
  }

  // Правило 2 — имя ресурса по-английски. Судится и в прозе тоже.
  //
  // Перед сверкой снимаются ПУТИ и ИДЕНТИФИКАТОРЫ С РАЗДЕЛИТЕЛЕМ
  // (`network.defaultRouteTableId°`, `nlb_target_group`): там имя ресурса —
  // часть координаты контракта, а не обращение к человеку. Горб верблюда сам
  // по себе НЕ снимается — иначе `AccessBinding` и `MachineType`, набранные
  // человеку, ушли бы из-под правила вместе с ним.
  const humanText = text.replace(TOKEN, (raw) => {
    const t = raw.replace(EDGE, "");
    const separated = new RegExp(`[${LATIN}0-9][._:/@#=][${LATIN}0-9]`).test(t);
    return /\d/.test(t) || separated ? " " : raw;
  });
  const banned = BANNED.filter((n) => humanText.includes(n));
  if (banned.length) return banned;

  const unexplained = [];
  for (const raw of text.match(TOKEN) ?? []) {
    const token = raw.replace(EDGE, "");
    if (!HAS_LATIN.test(token)) continue;
    // Форма разбирается ПЕРВОЙ: словарь обязан нести только то, чего форма не
    // объясняет, иначе его записи «используются» и не истекают никогда.
    if (looksMachine(token)) continue;
    const lower = token.toLowerCase();
    if (TERMS.has(lower)) {
      usedTerms.add(lower);
      continue;
    }
    // Составной термин не распознан целиком — разбираем по дефису.
    for (const word of token.split("-")) {
      if (word.length < 2) continue; // одиночная буква утверждения не несёт
      if (!HAS_LATIN.test(word)) continue;
      const w = word.toLowerCase();
      if (TERMS.has(w)) {
        usedTerms.add(w);
        continue;
      }
      unexplained.push(word);
    }
  }
  if (unexplained.length === 0) return null;
  // Проза учитывается словарём (термины засчитаны выше), но не судится.
  if (isProse(text)) return null;
  return unexplained;
}

// ─────────────────────────────────────────────────────────────────────────────
// Обход дерева.
// ─────────────────────────────────────────────────────────────────────────────

const TS_FILE = /\.(ts|tsx)$/;
const TEST_FILE = /\.test\.|\.spec\.|\/test\/|\/__tests__\//;

/** Подстановка на месте вычисляемой части. Не буква и не цифра — разбору не мешает. */
const HOLE = "\u2026";
/** Предел раскрытия имён: защита от взаимных ссылок, а не ограничение смысла. */
const MAX_RESOLVE_DEPTH = 8;

/**
 * Содержимое элемента верно́ ДОСЛОВНО и подписью не является. `code` здесь —
 * атрибут antd `Typography.Text code`, который рендерится в `<code>`.
 */
const VERBATIM_CONTENT_TAGS = new Set(["style", "script", "pre", "code"]);

/**
 * Имена файла, объявленные значением: `const X = …`.
 *
 * Имя, объявленное ДВАЖДЫ, а также имя параметра и элемента деструктуризации,
 * из карты СНИМАЕТСЯ. Причина не в аккуратности: `{ label }` в пропсах
 * компонента затеняет модульный `const label`, и без этого правила подпись
 * судилась бы по чужому объявлению. Разбор областей видимости здесь не нужен —
 * достаточно отказаться судить неоднозначное: не судить безопаснее, чем судить
 * не то.
 */
function collectBindings(sf) {
  const bound = new Map();
  const ambiguous = new Set();
  const claim = (name, init) => {
    if (bound.has(name) || ambiguous.has(name)) {
      bound.delete(name);
      ambiguous.add(name);
      return;
    }
    if (init) bound.set(name, init);
    else ambiguous.add(name);
  };
  const walk = (n) => {
    if (ts.isVariableDeclaration(n) && ts.isIdentifier(n.name)) claim(n.name.text, n.initializer);
    else if (ts.isParameter(n) && ts.isIdentifier(n.name)) claim(n.name.text, null);
    else if (ts.isBindingElement(n) && ts.isIdentifier(n.name)) claim(n.name.text, null);
    ts.forEachChild(n, walk);
  };
  walk(sf);
  return bound;
}

/** Прямой литерал — ровно то, что читала первая редакция гейта. */
function isDirectLiteral(node) {
  if (!node) return false;
  if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) return true;
  if (ts.isJsxExpression(node) && node.expression) return isDirectLiteral(node.expression);
  return false;
}

/**
 * Все строки, которыми выражение МОЖЕТ оказаться перед пользователем.
 *
 * Тернарник даёт обе ветви — обе показываются, в разных состояниях. `||`/`??`
 * дают обе стороны: правая и есть подпись по умолчанию. Шаблон и склейка —
 * свой текст с `HOLE` на месте подстановки: так соседство слов сохраняется
 * («Создание: …»), а вычисленная часть не выдаётся за подпись.
 *
 * Вызов, обращение к полю, всё остальное — ПУСТО. Это граница, и она названа
 * числом в переписи, а не умолчана.
 */
function literalsOf(node, bound, depth = 0, seen = new Set()) {
  if (!node || depth > MAX_RESOLVE_DEPTH) return [];
  if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) return [node.text];
  if (ts.isParenthesizedExpression(node) || ts.isAsExpression(node) || ts.isNonNullExpression(node))
    return literalsOf(node.expression, bound, depth + 1, seen);
  if (ts.isSatisfiesExpression(node)) return literalsOf(node.expression, bound, depth + 1, seen);
  if (ts.isJsxExpression(node)) return node.expression ? literalsOf(node.expression, bound, depth + 1, seen) : [];
  if (ts.isConditionalExpression(node))
    return [
      ...literalsOf(node.whenTrue, bound, depth + 1, seen),
      ...literalsOf(node.whenFalse, bound, depth + 1, seen),
    ];
  if (ts.isBinaryExpression(node)) {
    const op = node.operatorToken.kind;
    if (op === ts.SyntaxKind.BarBarToken || op === ts.SyntaxKind.QuestionQuestionToken)
      return [
        ...literalsOf(node.left, bound, depth + 1, seen),
        ...literalsOf(node.right, bound, depth + 1, seen),
      ];
    if (op === ts.SyntaxKind.PlusToken) {
      const left = literalsOf(node.left, bound, depth + 1, seen);
      const right = literalsOf(node.right, bound, depth + 1, seen);
      return [(left[0] ?? HOLE) + (right[0] ?? HOLE)];
    }
    return [];
  }
  if (ts.isTemplateExpression(node)) {
    let out = node.head.text;
    for (const span of node.templateSpans) out += HOLE + span.literal.text;
    return [out];
  }
  if (ts.isIdentifier(node)) {
    if (seen.has(node.text)) return [];
    const init = bound.get(node.text);
    if (!init) return [];
    const deeper = new Set(seen);
    deeper.add(node.text);
    return literalsOf(init, bound, depth + 1, deeper);
  }
  return [];
}

/** Элемент, чьё содержимое дословно (`<style>`, `<pre>`, `<code>`, antd `code`). */
function isVerbatimContent(el) {
  if (!ts.isJsxElement(el)) return false;
  const opening = el.openingElement;
  const tag = opening.tagName.getText();
  if (VERBATIM_CONTENT_TAGS.has(tag) || VERBATIM_CONTENT_TAGS.has(tag.split(".").pop() ?? "")) return true;
  return opening.attributes.properties.some(
    (a) => ts.isJsxAttribute(a) && ts.isIdentifier(a.name) && a.name.text === "code",
  );
}

/**
 * Выражение — ЕДИНСТВЕННОЕ содержимое элемента, то есть его текст целиком.
 *
 * Различитель не косметический, он и держит границу между подписью и данными
 * в тексте JSX. Стоящее в одиночку выражение пользователь читает как текст
 * элемента; стоящее среди других детей — как значение, вставленное в фразу, а
 * фразу гейт уже прочитал сам.
 */
function isSoleChild(expr) {
  const el = expr.parent;
  if (!el || !(ts.isJsxElement(el) || ts.isJsxFragment(el))) return false;
  if (isVerbatimContent(el)) return false;
  return el.children.every((c) => c === expr || (ts.isJsxText(c) && c.text.trim() === ""));
}

/** Собирает подписи одного файла: `{ line, kind, key, value, origin }`. */
function collectLabels(rel, source) {
  const sf = ts.createSourceFile(
    rel,
    source,
    ts.ScriptTarget.Latest,
    /* setParentNodes */ true,
    rel.endsWith(".tsx") ? ts.ScriptKind.TSX : ts.ScriptKind.TS,
  );
  const bound = collectBindings(sf);
  const labels = [];
  let valueSites = 0;
  let proseSites = 0;
  let dataSites = 0;

  const at = (node) => sf.getLineAndCharacterOfPosition(node.getStart(sf)).line + 1;

  /**
   * Кладёт подпись из уже объявленной ПОЗИЦИИ, разрешая её значение.
   *
   * `countData` считает границу: позиция подписи есть, а текста в ней нет —
   * значение приходит из данных. У текста JSX это не считается: там кандидатов
   * сотни, и почти все они — данные по замыслу, а не потерянная подпись.
   */
  const push = (node, kind, key, init, countData) => {
    const origin = isDirectLiteral(init) ? "разметка" : "вычислено";
    const values = literalsOf(init, bound);
    if (values.length === 0) {
      if (countData) dataSites++;
      return;
    }
    for (const value of values) labels.push({ line: at(node), kind, key, value, origin });
  };

  const walk = (node) => {
    if (ts.isJsxAttribute(node) && node.name && ts.isIdentifier(node.name)) {
      const key = node.name.text;
      if (VALUE_KEYS.has(key)) valueSites++;
      else if (LABEL_KEYS.has(key) || LABEL_ATTRS_ONLY.has(key))
        push(node, "атрибут JSX", key, node.initializer, true);
    }
    if (ts.isPropertyAssignment(node) && (ts.isIdentifier(node.name) || ts.isStringLiteral(node.name))) {
      const key = node.name.text;
      if (VALUE_KEYS.has(key)) valueSites++;
      else if (LABEL_KEYS.has(key)) push(node, "свойство", key, node.initializer, true);
    }
    if (ts.isJsxText(node) && node.text.trim() !== "") {
      labels.push({ line: at(node), kind: "текст JSX", key: "", value: node.text.trim(), origin: "разметка" });
    }
    // Текст элемента, пришедший ВЫЧИСЛЕНИЕМ: `<Text>{valueLabel}</Text>`.
    // Только единственное содержимое элемента — см. `isSoleChild`.
    if (ts.isJsxExpression(node) && node.expression && isSoleChild(node)) {
      push(node, "текст JSX", "", node.expression, false);
    }
    // Подпись, собранная помощником: `labelWithInfo("Имя", "…")`.
    if (ts.isCallExpression(node) && ts.isIdentifier(node.expression) && LABEL_HELPERS.has(node.expression.text)) {
      for (const arg of node.arguments) push(arg, `аргумент ${node.expression.text}`, "", arg, true);
    }
    ts.forEachChild(node, walk);
  };
  walk(sf);
  for (const l of labels) if (isProse(l.value.replace(ENTITY, " ").trim())) proseSites++;
  return { labels, valueSites, proseSites, dataSites };
}

/** Прогон по перечню файлов. Возвращает находки и перепись. */
export function scan(root, files) {
  const findings = [];
  const usedTerms = new Set();
  const usedVerbatim = new Set();
  const census = { files: 0, labels: 0, markup: 0, computed: 0, valueSites: 0, proseSites: 0, dataSites: 0, cyrillic: 0 };

  for (const rel of files) {
    const source = fs.readFileSync(path.join(root, rel), "utf8");
    const { labels, valueSites, proseSites, dataSites } = collectLabels(rel, source);
    census.files++;
    census.valueSites += valueSites;
    census.proseSites += proseSites;
    census.dataSites += dataSites;
    for (const l of labels) {
      census.labels++;
      if (l.origin === "вычислено") census.computed++;
      else census.markup++;
      if (CYRILLIC.test(l.value)) census.cyrillic++;
      const words = adjudicate(l.value, usedTerms, usedVerbatim);
      if (words) findings.push({ file: rel, ...l, words });
    }
  }
  return { findings, census, usedTerms, usedVerbatim };
}

// ─────────────────────────────────────────────────────────────────────────────
// Инъекция в обе стороны: настоящий вход из дерева + законный близнец.
// ─────────────────────────────────────────────────────────────────────────────

/**
 * Обе фикстуры — ДОСЛОВНЫЕ строки продукта. Нарушение — колонка, с которой
 * заведена задача #478; близнец — колонка из того же реестра, законная по
 * `TERMS`. Синтетика здесь не годится: она доказывала бы, что гейт понимает
 * выдуманный вход, а не тот, что встречается в дереве.
 */
const INJECTION = {
  violation: '{ header: "External ID", path: "external_id", format: "uid-short" },',
  // Близнец по ФОРМЕ: аббревиатура объясняется видом токена, без словаря.
  twinShape: '{ header: "CIDR", path: "cidr", format: "code" },',
  // Близнец по СЛОВАРЮ: строчный термин, который форма объяснить не может.
  twinTerm: '{ label: "user-data (cloud-init)", name: "user_data" },',

  // ── Подпись, пришедшая ВЫЧИСЛЕНИЕМ ─────────────────────────────────────────
  // Все пять — дословные строки дерева на день, когда гейт научился их видеть.
  // Каждая была НАЙДЕНА этим разбором и в дереве уже исправлена, поэтому здесь
  // они и стоят: синтетика доказывала бы, что гейт понимает выдуманный вход.
  varLabel: 'const label = kind === "v4" ? "IPv4 CIDR blocks" : "IPv6 CIDR blocks";',
  ternaryAttr: 'title={row.is_system ? "system roles read-only" : "Изменить"}',
  templateAttr:
    "description={`Удалить «${row.name}»? Custom role с активными AccessBinding → FailedPrecondition.`}",
  concatProp:
    '{ description: "Единый канал размера инстанса — каталог MachineType. " + "Сменить размер можно на остановленном инстансе." },',
  orDefault: '{name || "Security Group"}',

  // Законный близнец ПОЛОСЫ: значение, вставленное в фразу, — расширение файла.
  // Судить его значило бы требовать перевода у `txt`.
  fragmentValue: 'const valueExt = isSecret ? "txt" : "pem";',
  // Законный близнец ВЕРБАТИМА: содержимое `<style>` верно́ дословно.
  styleSheet: 'const CSS = ".kc-props { color: red; }";',
};

function selfTest(root) {
  const dir = fs.mkdtempSync(path.join(root, ".ui-language-selftest-"));
  let failures = 0;
  const say = (ok, what) => {
    console.log(`  ${ok ? "OK  " : "FAIL"}  ${what}`);
    if (!ok) failures++;
  };
  try {
    const red = "red.tsx";
    const shape = "twin-shape.tsx";
    const term = "twin-term.tsx";
    fs.writeFileSync(path.join(dir, red), `export const C = [\n  ${INJECTION.violation}\n];\n`);
    fs.writeFileSync(path.join(dir, shape), `export const C = [\n  ${INJECTION.twinShape}\n];\n`);
    fs.writeFileSync(path.join(dir, term), `export const C = [\n  ${INJECTION.twinTerm}\n];\n`);

    const r = scan(dir, [red]);
    say(r.findings.length === 1, `нарушение найдено (найдено ${r.findings.length}, ожидалось 1)`);
    say(r.findings[0]?.file === red && r.findings[0]?.line === 2, `координата названа: ${red}:2`);
    say(
      (r.findings[0]?.words ?? []).join(",") === "External",
      `названо слово-причина: ${(r.findings[0]?.words ?? []).join(",") || "—"}`,
    );

    const s = scan(dir, [shape]);
    say(s.findings.length === 0, `законный CIDR рядом не помечен (найдено ${s.findings.length}, ожидалось 0)`);

    const t = scan(dir, [term]);
    say(t.findings.length === 0, `законный термин словаря не помечен (найдено ${t.findings.length}, ожидалось 0)`);
    say(t.usedTerms.has("cloud-init"), "термин словаря засчитан (послабление не висит впустую)");

    // Гейт обязан читать ИСПОЛНЯЕМУЮ часть: то же слово в комментарии — не находка.
    const comment = "comment.tsx";
    fs.writeFileSync(path.join(dir, comment), `// ${INJECTION.violation}\nexport const C = [];\n`);
    const c = scan(dir, [comment]);
    say(c.findings.length === 0, `та же строка в комментарии молчит (найдено ${c.findings.length}, ожидалось 0)`);

    // Гибрид: кириллица рядом не оправдывает латинское слово.
    const hybrid = "hybrid.tsx";
    fs.writeFileSync(path.join(dir, hybrid), `export const C = [\n  { label: "Целевые группы (Target Groups)" },\n];\n`);
    const h = scan(dir, [hybrid]);
    say(h.findings.length === 1, `гибрид «русское (English)» найден (найдено ${h.findings.length}, ожидалось 1)`);

    // Правило 2: имя ресурса по-английски внутри ПРОЗЫ — правило 1 её не судит.
    const prose = "prose.tsx";
    fs.writeFileSync(
      path.join(dir, prose),
      `export const C = [\n  { description: "L4 балансировщики трафика TCP/UDP: LoadBalancer, Listener, Target Group." },\n];\n`,
    );
    const p = scan(dir, [prose]);
    say(p.findings.length === 1, `имя ресурса в прозе найдено (найдено ${p.findings.length}, ожидалось 1)`);
    // Законный близнец правила 2: та же проза без имён ресурсов — молчит.
    const proseOk = "prose-ok.tsx";
    fs.writeFileSync(
      path.join(dir, proseOk),
      `export const C = [\n  { description: "Балансировка трафика TCP/UDP на четвёртом уровне: балансировщики и цели." },\n];\n`,
    );
    const po = scan(dir, [proseOk]);
    say(po.findings.length === 0, `та же проза по-русски молчит (найдено ${po.findings.length}, ожидалось 0)`);
    // И раздел «Load Balancer» остаётся законным — он объявлен дословным.
    const section = "section.tsx";
    fs.writeFileSync(path.join(dir, section), `export const S = { nlb: { title: "Load Balancer" } };\n`);
    const sec = scan(dir, [section]);
    say(sec.findings.length === 0, `объявленное имя раздела молчит (найдено ${sec.findings.length}, ожидалось 0)`);

    // ── Полоса вычисляемой подписи. Каждая ось — с законным близнецом ────────
    const w = (name, body) => {
      fs.writeFileSync(path.join(dir, name), body);
      return scan(dir, [name]);
    };

    // 1. Подпись, вынесенная в ПЕРЕМЕННУЮ, судится наравне с литералом.
    const v1 = w(
      "computed-var.tsx",
      `${INJECTION.varLabel}\nexport const C = () => <Text>{label}</Text>;\n`,
    );
    say(v1.findings.length === 2, `подпись из переменной найдена в обеих ветвях (найдено ${v1.findings.length}, ожидалось 2)`);
    say(v1.findings[0]?.line === 2, `координата названа — строка ПОКАЗА, не объявления: ${v1.findings[0]?.line}`);
    say((v1.findings[0]?.words ?? []).join(",") === "blocks", `названо слово-причина: ${(v1.findings[0]?.words ?? []).join(",") || "—"}`);

    // 1-близнец. Значение, вставленное В ФРАЗУ, подписью не считается: там
    // расширение файла, а не слово. Без этой границы `txt` требовал бы перевода.
    const v1t = w(
      "computed-fragment.tsx",
      `${INJECTION.fragmentValue}\nexport const C = () => <Button>Скачать .{valueExt}</Button>;\n`,
    );
    say(v1t.findings.length === 0, `значение в подстановке молчит (найдено ${v1t.findings.length}, ожидалось 0)`);

    // 2. Тернарник в атрибуте.
    const v2 = w("computed-ternary.tsx", `export const C = () => <Button ${INJECTION.ternaryAttr} />;\n`);
    say(v2.findings.length === 1, `тернарник в атрибуте найден (найдено ${v2.findings.length}, ожидалось 1)`);

    // 3. Шаблон с подстановкой — правило 2 (имя ресурса) сквозь неё.
    const v3 = w("computed-template.tsx", `export const C = () => <Popconfirm ${INJECTION.templateAttr} />;\n`);
    say(v3.findings.length === 1, `имя ресурса в шаблоне найдено (найдено ${v3.findings.length}, ожидалось 1)`);
    say((v3.findings[0]?.words ?? []).join(",") === "AccessBinding", `названо имя ресурса: ${(v3.findings[0]?.words ?? []).join(",") || "—"}`);

    // 4. Склейка строк `+` — так собраны длинные пояснения реестра.
    const v4 = w("computed-concat.tsx", `export const C = [\n  ${INJECTION.concatProp}\n];\n`);
    say(v4.findings.length === 1, `имя ресурса в склейке найдено (найдено ${v4.findings.length}, ожидалось 1)`);

    // 5. Подпись по умолчанию через `||` — правая сторона тоже показывается.
    const v5 = w("computed-or.tsx", `export const C = () => <Text>${INJECTION.orDefault}</Text>;\n`);
    say(v5.findings.length === 1, `умолчание через || найдено (найдено ${v5.findings.length}, ожидалось 1)`);

    // 6. Дословное содержимое: `<style>` не подпись. И ЗЕРКАЛО — тот же текст
    //    в обычном элементе находкой ЯВЛЯЕТСЯ, иначе исключение было бы пустым.
    const v6 = w("verbatim-style.tsx", `${INJECTION.styleSheet}\nexport const C = () => <style>{CSS}</style>;\n`);
    say(v6.findings.length === 0, `лист стилей в <style> молчит (найдено ${v6.findings.length}, ожидалось 0)`);
    const v6m = w("verbatim-mirror.tsx", `${INJECTION.styleSheet}\nexport const C = () => <Text>{CSS}</Text>;\n`);
    say(v6m.findings.length === 1, `тот же текст в обычном элементе — находка (найдено ${v6m.findings.length}, ожидалось 1)`);

    // 7. Затенение: параметр перекрывает модульное имя, и гейт НЕ судит по
    //    чужому объявлению. Зеркало — без затенения та же подпись находится.
    const v7 = w(
      "shadowed.tsx",
      'const label = "External ID";\nexport function C({ label }) {\n  return <Text>{label}</Text>;\n}\n',
    );
    say(v7.findings.length === 0, `затенённое имя не судится по модульному объявлению (найдено ${v7.findings.length}, ожидалось 0)`);
    const v7m = w(
      "shadowed-mirror.tsx",
      'const label = "External ID";\nexport function C() {\n  return <Text>{label}</Text>;\n}\n',
    );
    say(v7m.findings.length === 1, `без затенения та же подпись найдена (найдено ${v7m.findings.length}, ожидалось 1)`);

    // 8. Перепись различает ДВЕ полосы. Одно число скрыло бы ровно ту потерю
    //    покрытия, ради которой полоса заведена.
    const v8 = w(
      "census.tsx",
      'const t = "Заголовок";\nexport const C = () => (\n  <div>\n    <Text title="Прямо">Текст</Text>\n    <Text>{t}</Text>\n  </div>\n);\n',
    );
    say(
      v8.census.markup === 2 && v8.census.computed === 1,
      `перепись называет обе величины: в разметке ${v8.census.markup} (ожидалось 2), вычислением ${v8.census.computed} (ожидалось 1)`,
    );

    // 9. Граница названа числом: позиция подписи есть, текста в ней нет.
    const v9 = w("boundary.tsx", "export const C = () => <Text title={row.name} />;\n");
    say(v9.census.dataSites === 1, `позиция без текста сосчитана (${v9.census.dataSites}, ожидалось 1)`);
    say(v9.findings.length === 0, `данные в позиции подписи находкой не становятся (найдено ${v9.findings.length}, ожидалось 0)`);
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
  console.log(failures === 0 ? "самопроверка: пройдена" : `самопроверка: провалено проверок ${failures}`);
  return failures === 0 ? 0 : 1;
}

// ─────────────────────────────────────────────────────────────────────────────
// Запуск.
// ─────────────────────────────────────────────────────────────────────────────

function main() {
  const uiRoot = process.cwd();
  if (!fs.existsSync(path.join(uiRoot, "package.json"))) {
    console.error("::error::запускать из ui-future/ (нет package.json в текущем каталоге)");
    return 2;
  }
  if (process.argv.includes("--self-test")) return selfTest(uiRoot);

  // Единица счёта — отслеживаемый git-элемент, а не то, что лежит на диске.
  const files = execFileSync("git", ["ls-files", "*/src/**"], { cwd: uiRoot, encoding: "utf8", maxBuffer: 64 << 20 })
    .split("\n")
    .filter(Boolean)
    .filter((f) => TS_FILE.test(f) && !TEST_FILE.test(f));

  const { findings, census, usedTerms, usedVerbatim } = scan(uiRoot, files);

  console.log("── язык подписей консоли ─────────────────────────────────────");
  console.log(`осмотрено файлов:            ${census.files}`);
  console.log(`подписей прочитано:          ${census.labels} (с кириллицей ${census.cyrillic})`);
  console.log(`  · литералом в разметке:     ${census.markup}`);
  console.log(`  · вычисляемым значением:    ${census.computed} (переменная, тернарник, шаблон, ??/||)`);
  console.log(`граница: позиций без текста: ${census.dataSites} (значение приходит из данных — резолвить нечего)`);
  console.log(`из них проза, не судится:    ${census.proseSites} (длиннее ${PROSE_WORDS} слов — пояснение, не имя)`);
  console.log(`площадок значений пропущено: ${census.valueSites} (placeholder — пример значения, не подпись)`);
  console.log(`терминов в словаре:          ${TERMS.size}, задействовано ${usedTerms.size}`);
  console.log(`дословных строк:             ${VERBATIM.size}, задействовано ${usedVerbatim.size}`);

  // Предпосылка: дереву есть что показать. Ноль прочитанного — падение.
  if (census.files === 0 || census.labels === 0) {
    console.error("::error::предпосылка гейта не выполнена: прочитано 0 файлов или 0 подписей");
    return 2;
  }

  let rc = 0;

  for (const f of findings) {
    console.error(`::error file=ui-future/${f.file},line=${f.line}::подпись по-английски: ${JSON.stringify(f.value)} — переводимые слова: ${f.words.join(", ")} (${f.kind}${f.key ? " " + f.key : ""})`);
  }
  if (findings.length) {
    console.error(`\nнаходок: ${findings.length}`);
    rc = 1;
  }

  // Самоистечение послаблений: записи, которой нечего исключать, быть не должно.
  const staleTerms = [...TERMS.keys()].filter((t) => !usedTerms.has(t));
  const staleVerbatim = [...VERBATIM.keys()].filter((v) => !usedVerbatim.has(v));
  for (const t of staleTerms) {
    console.error(`::error::словарю нечего исключать: термин "${t}" не встретился ни в одной подписи — снять из TERMS`);
    rc = 1;
  }
  for (const v of staleVerbatim) {
    console.error(`::error::словарю нечего исключать: строка ${JSON.stringify(v)} не встретилась ни в одной подписи — снять из VERBATIM`);
    rc = 1;
  }

  if (rc === 0) console.log("находок нет");
  return rc;
}

process.exit(main());
