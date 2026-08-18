#!/usr/bin/env node
// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

/**
 * Посадка проб консоли — ОДНА (#626).
 *
 * ПРЕДМЕТ. Окружений проб было три, а не одно: семь пакетов брали
 * `shared/src/test/setup.ts`, а `host` и `dashboard` держали свои — и подмены
 * общего заменителя виджетов в них не было вовсе. Цена та же, что у снятого
 * второго дублёра: правка общего заменителя до этих двух НЕ ДОЕЗЖАЛА, а
 * поведение их проб определялось тем, чего в остальных семи пакетах нет.
 * Разойтись это могло МОЛЧА — прогон обоих пакетов был зелёным и до, и после
 * любой правки общего заменителя.
 *
 * ЧТО ПРОВЕРЯЕТСЯ. Файл, ПОДМЕНЯЮЩИЙ модуль виджетов, в дереве ровно один
 * («посадка»). Остальные окружения — надстройки: им разрешено добавлять своё
 * (заглушку сети, свои отображения), но подменять модуль виджетов второй раз
 * нельзя, и каждое обязано ДОСТАВАТЬ до посадки по цепочке импортов. Пакет,
 * чьё окружение до посадки не достаёт, работает на другом заменителе — том,
 * которого нет ни у кого больше.
 *
 * ПОЧЕМУ ГЕЙТ, А НЕ `grep`. Признак задачи — «поиск имени модуля по файлам
 * окружений называет один файл» — считает попадания и в ПРОЗЕ: разбор,
 * объясняющий эту самую подмену, делает предикат ложным, а подгонка
 * комментария под инструмент есть подгонка, а не проверка. Здесь читается
 * ВЫЗОВ подмены и граф импортов, поэтому упоминание имени в комментарии не
 * значит ничего, а второй вызов значит.
 *
 * ОБЪЁМ ОСМОТРЕННОГО заявляется всегда: «ноль находок» обязано быть отличимо
 * от «ноль прочитанного». Способность упасть доказана `--self-test` — инъекция
 * в обе стороны на синтетическом дереве во временном каталоге.
 *
 * Запуск из ui-future/:  node scripts/check-single-test-landing.mjs
 *                        node scripts/check-single-test-landing.mjs --self-test
 */

import { execFileSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

/** Модуль, подмена которого и делает окружение ПОСАДКОЙ, а не надстройкой. */
const SUBSTITUTED = "antd";

const uiRoot = process.cwd();
if (!fs.existsSync(path.join(uiRoot, "package.json"))) {
  console.error("::error::запускать из ui-future/ (нет package.json в текущем каталоге)");
  process.exit(2);
}

/** Вызов подмены модуля виджетов — именно ВЫЗОВ, а не упоминание имени. */
function substitutesWidgets(source) {
  return new RegExp(String.raw`unstable_mockModule\(\s*["']${SUBSTITUTED}["']`).test(source);
}

/** Пути, которые файл импортирует. Относительные и по алиасу `@shared/…`. */
function importsOf(source) {
  const out = [];
  const re = /(?:^|\n)\s*import\s+(?:[^'"\n]*?from\s*)?["']([^"']+)["']/g;
  let m;
  while ((m = re.exec(source)) !== null) out.push(m[1]);
  return out;
}

/**
 * Резолв импорта окружения в путь дерева. Полноценного резолва здесь не нужно:
 * окружения импортируют друг друга только двумя способами — относительным
 * путём и алиасом `@shared/…`, тем же, что объявлен в отображениях jest.
 */
function resolveImport(spec, fromFile, root) {
  const base = spec.startsWith("@shared/")
    ? path.join(root, "shared", "src", spec.slice("@shared/".length))
    : spec.startsWith(".")
      ? path.resolve(path.dirname(path.join(root, fromFile)), spec)
      : null;
  if (base === null) return null;
  for (const cand of [base, `${base}.ts`, `${base}.tsx`, path.join(base, "index.ts")]) {
    if (fs.existsSync(cand) && fs.statSync(cand).isFile()) return path.relative(root, cand);
  }
  return null;
}

/** Разбор одного состояния дерева. Чистая функция — её же зовёт `--self-test`. */
function analyze(root, configs) {
  const setups = new Map(); // путь → { source, substitutes }
  const perPackage = []; // { pkg, config, setup }
  const findings = [];

  const readSetup = (rel) => {
    if (setups.has(rel)) return setups.get(rel);
    const abs = path.join(root, rel);
    if (!fs.existsSync(abs)) return null;
    const source = fs.readFileSync(abs, "utf8");
    const rec = { source, substitutes: substitutesWidgets(source) };
    setups.set(rel, rec);
    return rec;
  };

  for (const config of configs) {
    const pkg = config.split("/")[0];
    const source = fs.readFileSync(path.join(root, config), "utf8");
    const m = source.match(/setupFilesAfterEnv:\s*\[([^\]]*)\]/);
    if (!m) {
      findings.push(`${pkg}: в ${config} нет setupFilesAfterEnv — пробы пакета идут БЕЗ окружения вовсе`);
      continue;
    }
    const entries = [...m[1].matchAll(/["']([^"']+)["']/g)].map((x) => x[1]);
    if (entries.length === 0) {
      findings.push(`${pkg}: setupFilesAfterEnv в ${config} пуст — пробы пакета идут БЕЗ окружения вовсе`);
      continue;
    }
    for (const entry of entries) {
      const rel = path.relative(root, path.resolve(path.join(root, pkg), entry.replace("<rootDir>/", "")));
      if (readSetup(rel) === null) {
        findings.push(`${pkg}: ${config} называет окружение ${rel}, а файла нет — гейт судить не может`);
        continue;
      }
      perPackage.push({ pkg, config, setup: rel });
    }
  }

  // Достаёт ли окружение до посадки по цепочке импортов.
  const reaches = (rel, seen = new Set()) => {
    if (seen.has(rel)) return null;
    seen.add(rel);
    const rec = readSetup(rel);
    if (rec === null) return null;
    if (rec.substitutes) return rel;
    for (const spec of importsOf(rec.source)) {
      const next = resolveImport(spec, rel, root);
      if (next === null) continue;
      const hit = reaches(next, seen);
      if (hit !== null) return hit;
    }
    return null;
  };

  // Обход ПЕРЕД переписью посадок: посадка бывает не названа объявлением
  // напрямую, а достигаться импортом надстройки — тогда до обхода она ещё не
  // прочитана, и перепись насчитала бы ноль там, где посадка есть.
  for (const { pkg, setup } of perPackage) {
    const landing = reaches(setup);
    if (landing === null) {
      findings.push(
        `${pkg}: окружение ${setup} не достаёт до посадки — пакет гоняет пробы БЕЗ общего заменителя виджетов, ` +
          `и правка заменителя до него не доезжает`,
      );
    }
  }
  const landings = [...setups.entries()].filter(([, r]) => r.substitutes).map(([k]) => k);
  if (landings.length > 1) {
    findings.push(
      `посадок ${landings.length}, а должна быть одна: ${landings.join(", ")} — ` +
        `правка одного заменителя до пакетов другого не доезжает, и разойтись они могут молча`,
    );
  }

  return { findings, landings, perPackage, setups };
}

/** Отслеживаемые объявления jest — из индекса git, а не с диска. */
function trackedConfigs(root) {
  return execFileSync("git", ["ls-files", "*/jest.config.cjs"], { cwd: root, encoding: "utf8" })
    .split("\n")
    .filter(Boolean);
}

/**
 * Самопроверка — инъекция в обе стороны на синтетическом дереве во временном
 * каталоге. В репозиторий ничего не вносится: писать в чужое рабочее состояние
 * ради пробы запрещено.
 */
function selfTest() {
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), "single-landing-selftest-"));
  const mk = (pkg, setupBody, extra = "") => {
    fs.mkdirSync(path.join(tmp, pkg, "src", "test"), { recursive: true });
    fs.writeFileSync(
      path.join(tmp, pkg, "jest.config.cjs"),
      `module.exports = { setupFilesAfterEnv: ["${extra || `<rootDir>/src/test/setup.ts`}"] };\n`,
    );
    fs.writeFileSync(path.join(tmp, pkg, "src", "test", "setup.ts"), setupBody);
  };
  fs.mkdirSync(path.join(tmp, "shared", "src", "test"), { recursive: true });
  fs.writeFileSync(
    path.join(tmp, "shared", "src", "test", "setup.ts"),
    `jest.unstable_mockModule("${SUBSTITUTED}", () => stub());\n`,
  );
  // Законная надстройка: своё плюс импорт посадки.
  mk("legal", `import "@shared/test/setup";\nglobal.fetch = () => Promise.reject(new Error("no"));\n`);
  const configs = ["legal/jest.config.cjs"];

  const green = analyze(tmp, configs);
  if (green.findings.length > 0) {
    console.error(`::error::самопроверка: гейт краснеет на ЗАКОННОМ дереве: ${green.findings[0]}`);
    fs.rmSync(tmp, { recursive: true, force: true });
    process.exit(1);
  }
  if (green.landings.length !== 1) {
    console.error(`::error::самопроверка: на законном дереве посадок ${green.landings.length}, а не одна`);
    fs.rmSync(tmp, { recursive: true, force: true });
    process.exit(1);
  }

  // Сторона «красное» №1: вторая посадка.
  mk("second", `jest.unstable_mockModule("${SUBSTITUTED}", () => ({}));\n`);
  const redTwo = analyze(tmp, [...configs, "second/jest.config.cjs"]);
  if (!redTwo.findings.some((f) => f.startsWith("посадок"))) {
    console.error("::error::самопроверка: вторая посадка НЕ поймана — гейт не способен упасть");
    fs.rmSync(tmp, { recursive: true, force: true });
    process.exit(1);
  }

  // Сторона «красное» №2: окружение, которое до посадки не достаёт.
  mk("orphan", `global.fetch = () => Promise.reject(new Error("no"));\n`);
  const redOrphan = analyze(tmp, [...configs, "orphan/jest.config.cjs"]);
  if (!redOrphan.findings.some((f) => f.startsWith("orphan:"))) {
    console.error("::error::самопроверка: окружение без пути до посадки НЕ поймано — гейт не способен упасть");
    fs.rmSync(tmp, { recursive: true, force: true });
    process.exit(1);
  }

  fs.rmSync(tmp, { recursive: true, force: true });
  console.log(
    "самопроверка: на законном дереве посадка одна и находок 0; " +
      `вторая посадка поймана («${redTwo.findings.find((f) => f.startsWith("посадок")).slice(0, 60)}…»), ` +
      "окружение без пути до посадки поймано",
  );
  process.exit(0);
}

if (process.argv.includes("--self-test")) selfTest();

const configs = trackedConfigs(uiRoot);
const { findings, landings, perPackage, setups } = analyze(uiRoot, configs);

console.log(
  `осмотрено: объявлений jest ${configs.length}, пар «пакет × окружение» ${perPackage.length}, ` +
    `окружений прочитано ${setups.size}; посадок ${landings.length}` +
    (landings.length ? ` [${landings.join(", ")}]` : ""),
);

if (configs.length === 0 || perPackage.length === 0) {
  console.error(
    "::error::предпосылка гейта не выполнена: объявлений jest или пар «пакет × окружение» ноль — " +
      "«ноль находок» здесь означало бы «ноль прочитанного»",
  );
  process.exit(1);
}
if (landings.length === 0) {
  console.error(
    `::error::подмены модуля «${SUBSTITUTED}» нет НИ В ОДНОМ окружении — либо её сняли, либо гейт ищет не то; ` +
      "в обоих случаях он потерял предмет и молчать не вправе",
  );
  process.exit(1);
}

if (findings.length) {
  for (const f of findings) console.error(`::error::${f}`);
  console.error(`\n✖ находок: ${findings.length}`);
  process.exit(1);
}
console.log("✓ посадка проб одна, и каждое окружение до неё достаёт");
