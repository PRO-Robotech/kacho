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
 * ── Чего гейт НЕ читает, и почему это решение, а не упущение ────────────────
 *
 *  · `placeholder` — несёт ПРИМЕР ЗНАЧЕНИЯ (`my-instance`, `user@example.com`,
 *    `ru-central1-a`, `grpc.health.v1.Health`), а значения не переводятся.
 *    Перепись таких площадок печатается отдельной строкой, чтобы исключение
 *    было видно числом, а не подразумевалось.
 *  · `name` — имя поля формы и HTML-атрибут, пользователю не показывается.
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
 * находок». Объём осмотренного печатается всегда.
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
// ─────────────────────────────────────────────────────────────────────────────
const TERMS = new Map([
  ["kid", "имя поля JWK (идентификатор ключа) — показывается как в стандарте"],
  ["passkey", "имя способа входа (WebAuthn); русского эквивалента нет"],
  ["cloud-init", "имя механизма первичной настройки машины"],
  ["user-data", "имя поля cloud-init"],
  ["docker", "имя стороннего инструмента в примере команды"],
  ["pull", "подкоманда docker — часть команды, которую набирает пользователь"],
  ["push", "подкоманда docker — часть команды, которую набирает пользователь"],
  ["pem", "имя формата файла ключа (расширение)"],
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
  ["Kachō Console", "имя продукта на странице входа"],
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

/** Собирает подписи одного файла: `{ line, kind, key, value }`. */
function collectLabels(rel, source) {
  const sf = ts.createSourceFile(
    rel,
    source,
    ts.ScriptTarget.Latest,
    /* setParentNodes */ true,
    rel.endsWith(".tsx") ? ts.ScriptKind.TSX : ts.ScriptKind.TS,
  );
  const labels = [];
  let valueSites = 0;
  let proseSites = 0;

  const literalOf = (node) => {
    if (!node) return null;
    if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) return node.text;
    if (ts.isJsxExpression(node) && node.expression) return literalOf(node.expression);
    return null;
  };
  const at = (node) => sf.getLineAndCharacterOfPosition(node.getStart(sf)).line + 1;

  const walk = (node) => {
    if (ts.isJsxAttribute(node) && node.name && ts.isIdentifier(node.name)) {
      const key = node.name.text;
      if (VALUE_KEYS.has(key)) valueSites++;
      else if (LABEL_KEYS.has(key) || LABEL_ATTRS_ONLY.has(key)) {
        const v = literalOf(node.initializer);
        if (v !== null) labels.push({ line: at(node), kind: "атрибут JSX", key, value: v });
      }
    }
    if (ts.isPropertyAssignment(node) && (ts.isIdentifier(node.name) || ts.isStringLiteral(node.name))) {
      const key = node.name.text;
      if (VALUE_KEYS.has(key)) valueSites++;
      else if (LABEL_KEYS.has(key)) {
        const v = literalOf(node.initializer);
        if (v !== null) labels.push({ line: at(node), kind: "свойство", key, value: v });
      }
    }
    if (ts.isJsxText(node) && node.text.trim() !== "") {
      labels.push({ line: at(node), kind: "текст JSX", key: "", value: node.text.trim() });
    }
    // Подпись, собранная помощником: `labelWithInfo("Имя", "пояснение")`.
    if (ts.isCallExpression(node) && ts.isIdentifier(node.expression) && LABEL_HELPERS.has(node.expression.text)) {
      for (const arg of node.arguments) {
        const v = literalOf(arg);
        if (v !== null) labels.push({ line: at(arg), kind: `аргумент ${node.expression.text}`, key: "", value: v });
      }
    }
    ts.forEachChild(node, walk);
  };
  walk(sf);
  for (const l of labels) if (isProse(l.value.replace(ENTITY, " ").trim())) proseSites++;
  return { labels, valueSites, proseSites };
}

/** Прогон по перечню файлов. Возвращает находки и перепись. */
export function scan(root, files) {
  const findings = [];
  const usedTerms = new Set();
  const usedVerbatim = new Set();
  const census = { files: 0, labels: 0, valueSites: 0, proseSites: 0, cyrillic: 0 };

  for (const rel of files) {
    const source = fs.readFileSync(path.join(root, rel), "utf8");
    const { labels, valueSites, proseSites } = collectLabels(rel, source);
    census.files++;
    census.valueSites += valueSites;
    census.proseSites += proseSites;
    for (const l of labels) {
      census.labels++;
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
