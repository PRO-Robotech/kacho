#!/usr/bin/env node
// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

/**
 * Гейт, ИСПОЛНЯЮЩИЙ федерацию.
 *
 * Предмет. Консоль собрана из хоста и восьми remote'ов через
 * `@originjs/vite-plugin-federation`. Ни типы, ни модульные пробы федерацию не
 * исполняют вовсе: они разбирают исходники, а федерация живёт в коде, который
 * плагин ДОПИСЫВАЕТ в уже собранный бандл. Поэтому на этом участке у нас не было
 * ни одной проверки, способной упасть, — и она была нужна: подъём сборщика
 * оставлял типы и пробы зелёными при мёртвом рантайме, а узнавалось это только
 * полным стендом (#309, откат 6368136c; разбор и замеры — #310 и
 * `docs/architecture/known-divergences.md`, раздел про пин сборочной цепочки).
 *
 * Что делает. Собирает МАЛЕНЬКИЙ remote тем же сборщиком и тем же плагином, что
 * и продукт (оснастка резолвится подъёмом в `ui-future/node_modules` — второго
 * объявления версий не заводится by construction), после чего ВЫПОЛНЯЕТ его точку
 * входа и требует от неё исполнения контракта:
 *
 *   1. `get(<имя>)` отдаёт фабрику и НЕ бросает — это и есть то место, где
 *      федерация умирала: точка входа грузилась, а первый же `get` падал;
 *   2. отданный модуль несёт маркер — значит чанк выполнился, а не просто
 *      зарезолвился именем;
 *   3. общая зависимость приехала из shared-области рабочей (вызываемой), а не
 *      заглушкой;
 *   4. стиль, который точка входа ОБЪЯВЛЯЕТ, существует в собранном бандле —
 *      объявление собирается плагином уже после сборки, и именно оно ломалось.
 *
 * Стенд не нужен: всё исполняется в этом же процессе за секунды. Это и было
 * смежным долгом #310 — «каждая итерация стоит полного стенда».
 *
 * Запуск из ui-future/:
 *   node scripts/check-federation-executes.mjs
 *   node scripts/check-federation-executes.mjs --self-test   // доказать, что гейт способен упасть
 *
 * Выход ненулевой — есть находки.
 */

import fs from "node:fs";
import os from "node:os";
import path from "node:path";

import federation from "@originjs/vite-plugin-federation";
import { build } from "vite";

const uiRoot = process.cwd();
if (!fs.existsSync(path.join(uiRoot, "package.json"))) {
  console.error("::error::запускать из ui-future/ (нет package.json в текущем каталоге)");
  process.exit(2);
}

const PROBE_DIR = path.join(uiRoot, "scripts", "federation-probe");
// Имён два, как у настоящего remote (страница и её навигация): разбор бандла плагин
// делает ПО КАЖДОМУ имени отдельно, и проба с единственным именем не отличила бы
// «работает» от «работает для первого».
const EXPOSES = { "./ProbePage": "./src/expose.js", "./navigation": "./src/navigation.js" };

// Общий набор берётся у настоящего remote, а не выписывается здесь второй раз:
// проба обязана исполнять ТУ ЖЕ форму федерации, что продукт. Разойдётся — гейт
// скажет об этом сам, вместо того чтобы молча проверять устаревшую форму.
const REFERENCE_REMOTE = path.join(uiRoot, "vpc", "vite.config.ts");

function referenceShared() {
  const src = fs.readFileSync(REFERENCE_REMOTE, "utf8");
  const m = src.match(/shared:\s*\[([^\]]*)\]/);
  if (!m) return null;
  return m[1]
    .split(",")
    .map((s) => s.trim().replace(/^["']|["']$/g, ""))
    .filter(Boolean);
}

const findings = [];
const note = (msg) => findings.push(msg);

const shared = referenceShared();
if (!shared) {
  note(
    `не удалось прочитать набор общих зависимостей из ${path.relative(uiRoot, REFERENCE_REMOTE)} — ` +
      "предпосылка гейта не выполняется, форма федерации могла измениться",
  );
}

async function buildProbe(outDir) {
  // Плагин резолвит выставленные пути ОТНОСИТЕЛЬНО текущего каталога процесса, а не
  // относительно `root` сборки, — поэтому каталог меняется на время сборки и
  // возвращается обратно. Это свойство плагина, а не наше удобство: без смены
  // каталога он ищет `./src/expose.js` в ui-future/ и не находит.
  const cwd = process.cwd();
  process.chdir(PROBE_DIR);
  try {
    await build({
      root: PROBE_DIR,
      logLevel: "error",
      configFile: false,
      plugins: [
        federation({
          name: "federation_probe",
          filename: "remoteEntry.js",
          exposes: EXPOSES,
          shared: shared ?? ["react"],
        }),
      ],
      // Посадка сборки — та же, что у всех восьми remote'ов: иначе проба исполняла бы
      // не ту ветку объявления стилей, которая работает в продукте.
      build: {
        outDir,
        emptyOutDir: true,
        target: "esnext",
        modulePreload: false,
        cssCodeSplit: false,
      },
    });
  } finally {
    process.chdir(cwd);
  }
}

/**
 * Исполняет собранную точку входа и возвращает наблюдения.
 * Ни одна ошибка отсюда не гасится: молчаливый `catch` превратил бы гейт в тот
 * самый «зелёный при сломанном рантайме», против которого он и написан.
 */
async function exerciseRemoteEntry(assetsDir) {
  const announcedCss = [];
  // Носитель ровно тех двух вызовов, которые делает объявление стилей. Дублёр не
  // снисходительнее настоящего: он ПРИНИМАЕТ href и запоминает его, поэтому мусор
  // в объявлении становится наблюдаемым, а не проглатывается.
  globalThis.window = globalThis;
  globalThis.document = {
    createElement: () => ({ rel: "", href: "" }),
    head: {
      appendChild(node) {
        announcedCss.push(node.href);
      },
    },
  };

  const entry = path.join(assetsDir, "remoteEntry.js");
  const mod = await import(`file://${entry}?t=${Date.now()}`);

  const results = [];
  for (const key of Object.keys(EXPOSES)) {
    const factory = await mod.get(key);
    const exposed = await factory();
    results.push({ key, exposed });
  }
  return { results, announcedCss };
}

async function run(outDir, label) {
  const assetsDir = path.join(outDir, "assets");
  let observed;
  try {
    observed = await exerciseRemoteEntry(assetsDir);
  } catch (err) {
    // Отказ пересказывается ДОСЛОВНО: диагноз ставится по тексту отказа, а не по имени
    // упавшего шага, поэтому подменять сообщение своей формулировкой нельзя.
    const shown = err instanceof Error ? `${err.name}: ${err.message}` : String(err);
    note(`${label}: точка входа собралась, но исполнение федерации упало — ${shown}`);
    return null;
  }

  for (const { key, exposed } of observed.results) {
    const value = exposed?.default ?? exposed;
    if (value?.marker !== "kacho-federation-probe") {
      note(`${label}: «${key}» отдан, но чанк не выполнился — маркера в модуле нет`);
      continue;
    }
    if (value.sharedResolved() !== true) {
      note(`${label}: «${key}» выполнился, но общая зависимость не приехала из shared-области`);
    }
  }

  // Стиль ОБЪЯВЛЕН — значит объявление обязано указывать на существующий файл.
  // Дефект, ради которого гейт написан, ломает именно это: вместо списка файлов в
  // собранный код уезжает неподставленная метка.
  const emitted = new Set(fs.readdirSync(assetsDir));
  if (observed.announcedCss.length === 0) {
    note(`${label}: точка входа не объявила ни одного стиля, хотя выставленный модуль его импортирует`);
  }
  for (const href of observed.announcedCss) {
    const base = path.basename(String(href));
    if (!emitted.has(base)) {
      note(`${label}: объявлен стиль «${href}», которого в собранном бандле нет`);
    }
  }

  return observed;
}

const outRoot = fs.mkdtempSync(path.join(os.tmpdir(), "kacho-federation-probe-"));
let selfTest = { ran: false, caught: false, twinSilent: false };

try {
  const outDir = path.join(outRoot, "dist");
  await buildProbe(outDir);
  const observed = await run(outDir, "проба");

  if (process.argv.includes("--self-test")) {
    // ИНЪЕКЦИЯ настоящим дефектом: возвращаем в собранный код неподставленную метку
    // ровно в той форме, в какой её оставляет сборщик, под чьей минификацией
    // подстановка не срабатывает. Гейт обязан покраснеть.
    selfTest.ran = true;
    const injectedDir = path.join(outRoot, "injected");
    fs.cpSync(outDir, injectedDir, { recursive: true });
    const entry = path.join(injectedDir, "assets", "remoteEntry.js");
    const code = fs.readFileSync(entry, "utf8");
    const broken = code.replace(/\[("[^"]*\.css")(,"[^"]*")*\]/, "`__v__css__/absolute/path/src/expose.js`");
    if (broken === code) {
      note("самопроверка: не удалось внести дефект — форма объявления стилей изменилась, инъекция потеряла предмет");
    } else {
      fs.writeFileSync(entry, broken);
      const before = findings.length;
      await run(injectedDir, "самопроверка(инъекция)");
      selfTest.caught = findings.length > before;
      // Находки инъекции — не находки дерева: снимаем их, оставив только вердикт.
      findings.length = before;
      if (!selfTest.caught) {
        note("самопроверка: гейт НЕ заметил внесённый дефект — он не способен упасть");
      }
    }

    // ЗАКОННЫЙ БЛИЗНЕЦ той же формы: нетронутая сборка обязана пройти молча,
    // иначе гейт ловит форму, а не существо, и первый же ложный срабат его снимет.
    const twinDir = path.join(outRoot, "twin");
    fs.cpSync(outDir, twinDir, { recursive: true });
    const beforeTwin = findings.length;
    await run(twinDir, "самопроверка(законный близнец)");
    selfTest.twinSilent = findings.length === beforeTwin;
    if (!selfTest.twinSilent) {
      note("самопроверка: гейт краснеет на ЗАКОННОЙ сборке — он ловит форму, а не дефект");
    }
  }

  // Объём осмотренного печатается всегда: «ноль находок» обязано быть отличимо от
  // «ноль исполненного».
  const names = Object.keys(EXPOSES);
  console.log(
    `осмотрено: выставленных имён исполнено ${observed ? observed.results.length : 0} из ${names.length}; ` +
      `объявлено стилей ${observed ? observed.announcedCss.length : 0}; ` +
      `общих зависимостей в наборе ${shared ? shared.length : 0} (взят из ${path.relative(uiRoot, REFERENCE_REMOTE)})`,
  );
  if (selfTest.ran) {
    console.log(
      `самопроверка: инъекция поймана — ${selfTest.caught ? "да" : "НЕТ"}; ` +
        `законный близнец молчит — ${selfTest.twinSilent ? "да" : "НЕТ"}`,
    );
  }
} finally {
  fs.rmSync(outRoot, { recursive: true, force: true });
}

if (findings.length) {
  for (const f of findings) console.error(`::error::${f}`);
  console.error(`\nфедерация не исполняет свой контракт: находок ${findings.length}`);
  process.exit(1);
}
console.log("✓ федерация исполнена: точка входа отдала выставленные модули, они выполнились, общее и стили на месте");
