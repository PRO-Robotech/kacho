// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

/**
 * САМОПРОВЕРКА РЕШЕНИЯ О БРАУЗЕРЕ (#1288): отказ обязан наступить на настоящем
 * входе и смолчать на законном близнеце. Не доказавший обоих — не доказательство.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПОЧЕМУ ЗДЕСЬ ДВА УРОВНЯ, А НЕ ОДИН
 *
 * Уровень первый — сама функция решения: дёшево и исчерпывающе по всем трём
 * формам выбора удалённого браузера.
 *
 * Уровень второй — ПРОВЯЗКА, и без него первый ничего не стоит: функция,
 * которую никто не зовёт, отказывает безупречно и не мешает ничему. Проверяется
 * она не чтением текста конфигурации (там слово нашлось бы и в комментарии,
 * объясняющем этот самый отказ), а ЗАГРУЗКОЙ конфигурации: её копия кладётся во
 * временный каталог рядом с подставным `@playwright/test`, и дальше смотрят, что
 * загрузка вернула. Подставной модуль отдаёт объявление как есть и НИЧЕГО не
 * утверждает — вердикт даёт поведение настоящей конфигурации.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ТРИ ПРОГОНА ПРОВЯЗКИ, А НЕ ДВА
 *
 *   контроль               — всё цело: конфигурация загружается;
 *   инъекция НОВОГО        — выбран удалённый браузер: загрузка отказывает, и
 *                            отказ называет и форму выбора, и предикат возврата;
 *   инъекция СУЩЕСТВУЮЩЕГО — снят адрес стенда: конфигурация обязана отказать
 *                            СВОИМ прежним отказом, а не новым. Без этого
 *                            прогона незаметно, что новая проверка проглотила
 *                            или подменила старую.
 *
 * Четвёртым идёт ОБРАТНЫЙ контроль провязки: копия конфигурации, из которой
 * вызов решения вынут. При том же удалённом входе она обязана загрузиться —
 * иначе красное второго прогона приходило бы откуда-то ещё, и провязка осталась
 * бы недоказанной.
 *
 * Запуск: node scripts/remote-browser-policy-selftest.ts
 */

import { mkdtempSync, mkdirSync, readFileSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

import { remoteBrowserRefusal, REMOTE_ENDPOINT_ENV } from "../remote-browser-policy.ts";

const HERE = path.dirname(fileURLToPath(import.meta.url));
const E2E = path.resolve(HERE, "..");

const cases: Array<{ name: string; ok: boolean; want: unknown; have: unknown }> = [];

function check(name: string, ok: boolean, want: unknown, have: unknown): void {
  cases.push({ name, ok, want, have });
}

// ─────────────────────────────────────────────────────────────────────────────
// УРОВЕНЬ 1 — сама функция решения
// ─────────────────────────────────────────────────────────────────────────────

// КОНТРОЛЬ: объявление такое, какое лежит в дереве, — блок `use` есть, выбора
// удалённого браузера в нём нет. Без этого случая молчание отказа на всём
// остальном ничего не значило бы.
check(
  "контроль: местный браузер отказа не вызывает",
  remoteBrowserRefusal({}, { use: {} }) === null,
  null,
  remoteBrowserRefusal({}, { use: {} }),
);

// ИНЪЕКЦИЯ, форма 1 — переменная окружения.
{
  const refusal = remoteBrowserRefusal({ [REMOTE_ENDPOINT_ENV]: "ws://pool:3000/" }, {});
  check(
    "форма 1: переменная окружения ловится",
    refusal !== null && refusal.includes(REMOTE_ENDPOINT_ENV),
    `отказ с упоминанием ${REMOTE_ENDPOINT_ENV}`,
    refusal === null ? "молчит" : "назвал",
  );
  check(
    "отказ называет ПРЕДИКАТ ВОЗВРАТА, а не просто запрещает",
    refusal !== null && refusal.includes("ПРЕДИКАТ ВОЗВРАТА") && refusal.includes("show-trace"),
    "предикат возврата в тексте",
    refusal !== null && refusal.includes("ПРЕДИКАТ ВОЗВРАТА") ? "назван" : "НЕ назван",
  );
  check(
    "отказ называет номер решения — иначе его снимут как непонятный",
    refusal !== null && refusal.includes("#1288") && refusal.includes("#1242"),
    "#1288 и #1242 в тексте",
    refusal !== null && refusal.includes("#1288") ? "названы" : "НЕ названы",
  );
}

// ИНЪЕКЦИЯ, форма 2 — объявление в блоке `use`.
{
  const refusal = remoteBrowserRefusal({}, { use: { connectOptions: { wsEndpoint: "ws://x/" } } });
  check(
    "форма 2: connectOptions в use ловится",
    refusal !== null && refusal.includes("use.connectOptions"),
    "отказ с упоминанием use.connectOptions",
    refusal === null ? "молчит" : "назвал",
  );
}

// ИНЪЕКЦИЯ, форма 3 — объявление в проекте. Проектов здесь сегодня ноль, и
// форма проверяется ИМЕННО ПОЭТОМУ: слепая зона заводится ровно там, куда
// сегодня не смотрят.
{
  const refusal = remoteBrowserRefusal({}, {
    projects: [
      { name: "местный", use: {} },
      { name: "пул", use: { connectOptions: { wsEndpoint: "ws://x/" } } },
    ],
  });
  check(
    "форма 3: connectOptions в проекте ловится и проект НАЗВАН",
    refusal !== null && refusal.includes("projects[1]") && refusal.includes("пул"),
    "отказ с координатой projects[1] («пул»)",
    refusal === null ? "молчит" : "назвал",
  );
}

// ЗАКОННЫЙ БЛИЗНЕЦ: соседние переменные без адреса удалённого браузера НЕ
// выбирают — прогонщик читает их только вместе с ним.
check(
  "соседние переменные без адреса отказа НЕ вызывают",
  remoteBrowserRefusal({ PW_TEST_CONNECT_HEADERS: "{}", PW_TEST_CONNECT_EXPOSE_NETWORK: "*" }, {}) === null,
  null,
  remoteBrowserRefusal({ PW_TEST_CONNECT_HEADERS: "{}" }, {}),
);

// ЗАКОННЫЙ БЛИЗНЕЦ: пустое значение адреса. Прогонщик на нём соединяться не
// пойдёт, и отказ здесь был бы ложной находкой.
check(
  "пустой адрес отказа НЕ вызывает",
  remoteBrowserRefusal({ [REMOTE_ENDPOINT_ENV]: "" }, {}) === null,
  null,
  remoteBrowserRefusal({ [REMOTE_ENDPOINT_ENV]: "" }, {}),
);

// ГРАНИЦА, где предикат обязан совпасть с прогонщиком, а не быть «похожим»:
// строка из одного пробела для прогонщика — адрес (значение истинно), значит и
// для отказа тоже. Проверка, обрезавшая бы пробелы, разошлась бы с транспортом
// ровно здесь.
check(
  "адрес из одного пробела ловится — предикат тот же, что у прогонщика",
  remoteBrowserRefusal({ [REMOTE_ENDPOINT_ENV]: " " }, {}) !== null,
  "отказ",
  remoteBrowserRefusal({ [REMOTE_ENDPOINT_ENV]: " " }, {}) === null ? "молчит" : "отказ",
);

// ЗАКОННЫЙ БЛИЗНЕЦ: поле объявлено пустым. Прогонщик такое объявление
// удалённым браузером не считает, и отказ не должен.
check(
  "connectOptions без значения отказа НЕ вызывает",
  remoteBrowserRefusal({}, { use: { connectOptions: undefined } }) === null,
  null,
  remoteBrowserRefusal({}, { use: { connectOptions: undefined } }),
);

// ─────────────────────────────────────────────────────────────────────────────
// УРОВЕНЬ 2 — ПРОВЯЗКА: настоящая конфигурация, загруженная по-настоящему
// ─────────────────────────────────────────────────────────────────────────────

const STUB = `export function defineConfig(config) { return config; }\n`;

/**
 * СОСЕДНИЕ МОДУЛИ КОНФИГУРАЦИИ — ВЫВОДЯТСЯ ИЗ НЕЁ САМОЙ, А НЕ ВЫПИСЫВАЮТСЯ.
 *
 * Здесь стоял ПЕРЕЧЕНЬ из одного имени, и он разошёлся с деревом на первом же
 * пополнении: конфигурация стала импортировать вторую половину отображения
 * имени (`./host-mapping.ts`, #1750), а песочница её не копировала — и ПЯТЬ
 * утверждений уровня 2 упали с `Cannot find module`. Отказ при этом выглядел
 * как опровержение решения о браузере, к которому он отношения не имеет:
 * находка называла не тот предмет.
 *
 * Перечень, который надо править вторым заходом, — второе место об одном
 * предмете; выведенный из самой конфигурации разойтись с ней не может
 * by construction.
 */
function siblingModules(configSource: string): string[] {
  const out = new Set<string>();
  for (const m of configSource.matchAll(/from\s+"\.\/([\w.-]+\.ts)"/g)) out.add(m[1]);
  return [...out];
}

/** Каталог с копией конфигурации, копиями её соседей и подставным прогонщиком. */
function sandbox(transformConfig: (source: string) => string): string {
  const dir = mkdtempSync(path.join(tmpdir(), "kacho-remote-browser-"));
  const stubDir = path.join(dir, "node_modules", "@playwright", "test");
  mkdirSync(stubDir, { recursive: true });
  writeFileSync(path.join(stubDir, "index.mjs"), STUB, "utf-8");
  writeFileSync(
    path.join(stubDir, "package.json"),
    JSON.stringify({ name: "@playwright/test", version: "0.0.0", type: "module", exports: { ".": "./index.mjs" } }),
    "utf-8",
  );
  const source = readFileSync(path.join(E2E, "playwright.config.ts"), "utf-8");
  const siblings = siblingModules(source);
  if (siblings.length === 0) {
    // Ноль соседей означает «распознаватель импортов ослеп», а не «их нет»:
    // конфигурация зовёт решение о браузере из соседнего модуля, и без него
    // уровень 2 доказывал бы свойство пустого файла.
    throw new Error(
      "в конфигурации проб не найдено НИ ОДНОГО соседнего модуля: сменилась форма " +
        "импорта, и песочница собрала бы конфигурацию без её половин",
    );
  }
  for (const rel of siblings) {
    writeFileSync(path.join(dir, rel), readFileSync(path.join(E2E, rel), "utf-8"), "utf-8");
  }
  writeFileSync(path.join(dir, "playwright.config.ts"), transformConfig(source), "utf-8");
  return dir;
}

let sequence = 0;

/** Загрузить копию конфигурации в заданном окружении; вернуть исход. */
async function load(
  env: Record<string, string | undefined>,
  transformConfig: (source: string) => string = (s) => s,
): Promise<{ thrown: string | null; trace: unknown }> {
  const dir = sandbox(transformConfig);
  const saved = { ...process.env };
  const quiet = console.log;
  try {
    for (const key of Object.keys(process.env)) {
      if (key.startsWith("PW_TEST_CONNECT") || key === "KACHO_CONSOLE_URL") delete process.env[key];
    }
    for (const [key, value] of Object.entries(env)) {
      if (value === undefined) delete process.env[key];
      else process.env[key] = value;
    }
    // Конфигурация печатает факт об отображении имени — здесь это шум.
    console.log = () => {};
    const url = pathToFileURL(path.join(dir, "playwright.config.ts")).href + `?n=${++sequence}`;
    const mod = (await import(url)) as { default?: { use?: { trace?: unknown } } };
    return { thrown: null, trace: mod.default?.use?.trace };
  } catch (err) {
    return { thrown: err instanceof Error ? err.message : String(err), trace: undefined };
  } finally {
    console.log = quiet;
    for (const key of Object.keys(process.env)) delete process.env[key];
    Object.assign(process.env, saved);
    rmSync(dir, { recursive: true, force: true });
  }
}

const STAND = { KACHO_CONSOLE_URL: "http://console.kacho.local:28080" };

// (1) КОНТРОЛЬ. Заодно доказывает, что загружена НАСТОЯЩАЯ конфигурация, а не
// пустышка: у неё читается объявленное значение записи трассы.
{
  const got = await load({ ...STAND });
  check(
    "провязка, контроль: конфигурация загружается и это ОНА",
    got.thrown === null && got.trace === "off",
    'без отказа, use.trace = "off"',
    got.thrown === null ? `use.trace = ${JSON.stringify(got.trace)}` : `отказ: ${got.thrown.split("\n")[0]}`,
  );
}

// (2) ИНЪЕКЦИЯ НОВОГО: выбран удалённый браузер.
{
  const got = await load({ ...STAND, [REMOTE_ENDPOINT_ENV]: "ws://pool.example:3000/" });
  check(
    "провязка, инъекция нового: удалённый браузер отвергается ЗАГРУЗКОЙ",
    got.thrown !== null && got.thrown.includes("#1288") && got.thrown.includes(REMOTE_ENDPOINT_ENV),
    "отказ с #1288 и именем переменной",
    got.thrown === null ? "загрузилась молча" : "отказ",
  );
  check(
    "провязка: отказ наступает ДО подъёма стенда — при загрузке конфигурации",
    got.thrown !== null && got.thrown.includes("ПРЕДИКАТ ВОЗВРАТА"),
    "предикат возврата виден оператору",
    got.thrown !== null && got.thrown.includes("ПРЕДИКАТ ВОЗВРАТА") ? "виден" : "НЕ виден",
  );
}

// (3) ИНЪЕКЦИЯ СУЩЕСТВУЮЩЕГО: снят адрес стенда. Прежний отказ обязан остаться
// собой — новая проверка не вправе его проглотить или подменить.
{
  const got = await load({ KACHO_CONSOLE_URL: undefined });
  check(
    "провязка, инъекция существующего: прежний отказ про адрес стенда цел",
    got.thrown !== null && got.thrown.includes("KACHO_CONSOLE_URL") && !got.thrown.includes("#1288"),
    "отказ про KACHO_CONSOLE_URL, БЕЗ упоминания #1288",
    got.thrown === null ? "загрузилась молча" : got.thrown.split("\n")[0],
  );
}

// (4) ОБРАТНЫЙ КОНТРОЛЬ ПРОВЯЗКИ: вызов решения вынут из копии конфигурации.
// При том же удалённом входе она обязана загрузиться — значит красное прогона
// (2) пришло именно от вызова, а не от чего-то ещё в этом файле.
{
  const CALL = /const refusal = remoteBrowserRefusal\(process\.env, config\);\s*if \(refusal\) \{\s*throw new Error\(refusal\);\s*\}\n?/;
  const source = readFileSync(path.join(E2E, "playwright.config.ts"), "utf-8");
  if (!CALL.test(source)) {
    // ЭТО НЕ ПАДЕНИЕ ИНСТРУМЕНТА, А НАХОДКА — и сказать её надо СЛУЧАЕМ, а не
    // исключением. Прежняя редакция здесь бросала: при вынутом вызове (ровно тот
    // дефект, ради которого случай заведён) самопроверка умирала, не напечатав
    // НИ ОДНОЙ строки, — то есть на своём предмете переставала быть читаемой.
    check(
      "обратный контроль: вызов решения найден в конфигурации",
      false,
      "вызов remoteBrowserRefusal в playwright.config.ts",
      "НЕ НАЙДЕН: либо его вынули (решение мертво), либо форма вызова изменилась " +
        "и обратный контроль перестал что-либо доказывать",
    );
  } else {
    const withoutCall = (src: string): string =>
      src
        .replace(CALL, "")
        // Политика перестаёт быть использованной — снимаем и её ввоз, иначе копия
        // спотыкается о неиспользованное имя там, где это проверяют.
        .replace(/import \{ remoteBrowserRefusal \} from "\.\/remote-browser-policy\.ts";\n/, "");
    const got = await load({ ...STAND, [REMOTE_ENDPOINT_ENV]: "ws://pool.example:3000/" }, withoutCall);
    check(
      "обратный контроль: без вызова решения тот же вход проходит",
      got.thrown === null,
      "без отказа (значит отказ давал именно вызов)",
      got.thrown === null ? "прошло" : `отказ: ${got.thrown.split("\n")[0]}`,
    );
  }
}

let failed = 0;
for (const c of cases) {
  console.log(`  ${c.ok ? "ОК " : "ПРОВАЛ"} ${c.name} (ждали ${String(c.want)}, получили ${String(c.have)})`);
  if (!c.ok) failed += 1;
}
console.log(`=== самопроверка решения о браузере: случаев ${cases.length}, провалов ${failed} ===`);
process.exit(failed === 0 ? 0 : 1);
