#!/usr/bin/env node
// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

/**
 * Пин сборщика консоли живёт, пока у него есть ПРЕДМЕТ.
 *
 * ЗАЧЕМ. Девять корней консоли закреплены на шестом мажоре `vite` не по инерции, а
 * по названной причине: `@originjs/vite-plugin-federation` не переживает переход
 * сборщика на rolldown (разбор и замеры — `docs/architecture/known-divergences.md`,
 * задача #310). Причина, живущая только в тексте, переживает свой предмет молча:
 * плагин однажды почини́т это у себя или будет заменён, а заметить станет некому —
 * и пин останется навсегда, уже без основания.
 *
 * ЧТО ЭТО ЗА ГЕЙТ (способен упасть, и падает по четырём разным поводам):
 *   • корни разошлись по версии сборщика или плагина → федерацию собирают РАЗНЫМИ
 *     цепочками, а хост и remote обязаны говорить на одном контракте;
 *   • корень объявляет плагин федерации без сборщика (или наоборот) → у пина дыра;
 *   • у плагина в дереве БОЛЬШЕ НЕТ того места, из-за которого пин заведён →
 *     предмет исчез, запись и пин подлежат пересмотру (это находка, а не радость);
 *   • рассматривать оказалось нечего → предпосылка гейта исчезла.
 *
 * ЧЕГО ГЕЙТ НЕ ДЕЛАЕТ И НЕ МОЖЕТ. Он не отвечает на вопрос «работает ли федерация
 * под другим мажором» — на него отвечает только исполнение, и для этого рядом стоит
 * `check-federation-executes.mjs`. Здесь проверяется ровно одно: не пережила ли
 * ПРИЧИНА пина сам предмет. Признак того места намеренно узкий и назван прямо,
 * чтобы его ложное срабатывание читалось как «перемерить», а не как «всё сломалось».
 *
 * Гейт заявляет ОБЪЁМ ОСМОТРЕННОГО: «ноль находок» обязано быть отличимо от «ноль
 * прочитанного». Набор корней ВЫВОДИТСЯ из дерева, а не выписывается — выписанный
 * переживёт добавление корня и промолчит о нём.
 *
 * Запуск из ui-future/:  node scripts/check-federation-pin-still-needed.mjs
 */

import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";

const uiRoot = process.cwd();
if (!fs.existsSync(path.join(uiRoot, "package.json"))) {
  console.error("::error::запускать из ui-future/ (нет package.json в текущем каталоге)");
  process.exit(2);
}

const BUNDLER = "vite";
const FEDERATION = "@originjs/vite-plugin-federation";

const findings = [];

// ─── 1. Перепись объявлений по дереву ────────────────────────────────────────
const manifests = execFileSync("git", ["ls-files", "*/package.json"], { cwd: uiRoot, encoding: "utf8" })
  .split("\n")
  .filter(Boolean);

const declared = [];
for (const rel of manifests) {
  const m = JSON.parse(fs.readFileSync(path.join(uiRoot, rel), "utf8"));
  const deps = { ...m.dependencies, ...m.devDependencies };
  if (!deps[BUNDLER] && !deps[FEDERATION]) continue;
  declared.push({ root: rel.split("/")[0], bundler: deps[BUNDLER], federation: deps[FEDERATION] });
}

if (declared.length === 0) {
  console.error(`::error::рассматривать нечего: ни один корень не объявляет ${BUNDLER} — гейт потерял предмет`);
  process.exit(1);
}

// ─── 2. Цепочка у всех одна ──────────────────────────────────────────────────
for (const key of ["bundler", "federation"]) {
  const name = key === "bundler" ? BUNDLER : FEDERATION;
  const ranges = new Map();
  for (const d of declared) {
    if (!d[key]) continue;
    if (!ranges.has(d[key])) ranges.set(d[key], []);
    ranges.get(d[key]).push(d.root);
  }
  if (ranges.size > 1) {
    const shown = [...ranges.entries()].map(([r, roots]) => `«${r}» → ${roots.join(", ")}`).join("; ");
    findings.push(
      `корни разошлись по ${name}: ${shown}. Хост и remote'ы собираются в один контракт федерации — ` +
        "разная цепочка означает, что контракт собран разными сборщиками",
    );
  }
}

// ─── 3. У пина нет дыр ───────────────────────────────────────────────────────
for (const d of declared) {
  if (d.federation && !d.bundler) {
    findings.push(`${d.root}: объявляет ${FEDERATION}, но не объявляет ${BUNDLER} — версия сборщика не закреплена`);
  }
}

// ─── 4. Предмет пина ещё на месте ────────────────────────────────────────────
//
// Плагин передаёт список стилей из одного своего этапа в другой МЕТКОЙ ВНУТРИ уже
// собранного кода и находит её выражением, привязанным к виду кавычки. Минификатор
// вправе перекавычить строковый литерал — и тогда метка не находится, подстановка не
// происходит, а в бандл уезжает строка там, где рантайм ждёт список. Пока это место
// у плагина такое, у пина есть предмет.
const pluginDist = ["node_modules", FEDERATION, "dist", "index.mjs"];
const pluginFile = path.join(uiRoot, ...pluginDist);
let subjectStillThere = false;
let subjectNote = "";

if (!fs.existsSync(pluginFile)) {
  findings.push(
    `предпосылка гейта не выполнена: ${path.join(...pluginDist)} не найден — нужен npm ci ` +
      "(либо плагин заменён, и тогда запись о пине подлежит пересмотру)",
  );
} else {
  const src = fs.readFileSync(pluginFile, "utf8");
  const prefix = src.match(/DYNAMIC_LOADING_CSS_PREFIX\s*=\s*["'`]([^"'`]+)["'`]/);
  // Выражение поиска метки — то самое место. Нас интересует НАБОР КАВЫЧЕК, который оно
  // принимает: пока обратной кавычки в нём нет, предмет пина на месте.
  const locator = src.match(/new RegExp\(`\((\[[^\]]*\])\)\$\{DYNAMIC_LOADING_CSS_PREFIX\}/);
  if (!prefix || !locator) {
    findings.push(
      `у ${FEDERATION} в дереве больше НЕТ места, из-за которого заведён пин ` +
        "(метка стилей и её поиск не опознаются) — предмет исчез: перемерить федерацию под новым мажором " +
        "сборщика и пересмотреть запись в docs/architecture/known-divergences.md",
    );
  } else if (locator[1].includes("`")) {
    findings.push(
      `${FEDERATION} теперь принимает и обратную кавычку в поиске метки стилей ` +
        `(набор кавычек «${locator[1]}») — названная причина пина исчезла: перемерить и пересмотреть запись`,
    );
  } else {
    subjectStillThere = true;
    subjectNote = `метка «${prefix[1]}», поиск принимает кавычки ${locator[1]}`;
  }
}

// ─── Объём осмотренного ──────────────────────────────────────────────────────
const roots = declared.map((d) => d.root).join(", ");
console.log(
  `осмотрено: манифестов ${manifests.length}, из них объявляют сборочную цепочку ${declared.length} (${roots}); ` +
    `${BUNDLER} «${declared[0].bundler ?? "—"}», ${FEDERATION} «${declared[0].federation ?? "—"}»`,
);
console.log(`предмет пина: ${subjectStillThere ? `на месте (${subjectNote})` : "НЕ подтверждён"}`);

if (findings.length) {
  for (const f of findings) console.error(`::error::${f}`);
  console.error(`\n✖ находок: ${findings.length}`);
  process.exit(1);
}

console.log("✓ цепочка сборки консоли одна на все корни, и у её пина по-прежнему есть предмет");
