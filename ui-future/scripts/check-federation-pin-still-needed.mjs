#!/usr/bin/env node
// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

/**
 * Пин сборщика консоли живёт, пока у него есть ПРЕДМЕТ.
 *
 * ЗАЧЕМ. Девять корней консоли закреплены на седьмом мажоре `vite` не по инерции, а
 * по названной причине: `@originjs/vite-plugin-federation` не переживает переход
 * сборщика на rolldown (разбор и замеры — `docs/architecture/known-divergences.md`,
 * задача #310). Причина, живущая только в тексте, переживает свой предмет молча:
 * плагин однажды почини́т это у себя или будет заменён, а заметить станет некому —
 * и пин останется навсегда, уже без основания.
 *
 * ЧТО ЭТО ЗА ГЕЙТ (способен упасть, и падает по пяти разным поводам):
 *   • корни разошлись по версии сборщика или плагина → федерацию собирают РАЗНЫМИ
 *     цепочками, а хост и remote обязаны говорить на одном контракте;
 *   • корень объявляет плагин федерации без сборщика (или наоборот) → у пина дыра;
 *   • у плагина в дереве БОЛЬШЕ НЕТ того места, из-за которого пин заведён →
 *     предмет исчез, запись и пин подлежат пересмотру (это находка, а не радость);
 *   • пин ШИРЕ своей причины — держит мажор ниже того, в котором препятствие
 *     появляется впервые (см. ниже, это и есть задача #337);
 *   • рассматривать оказалось нечего → предпосылка гейта исчезла.
 *
 * ПОЧЕМУ ДОБАВЛЕНА ПЯТАЯ ПРОВЕРКА (#337). Пин закрывает мажоры «от и выше», а причина
 * у него точечная. Пока эти две границы совпадают, всё честно; разойдутся — и пин
 * начнёт держать мажоры, к которым названная причина не относится, причём МОЛЧА:
 * проверки 1-4 на это устройство слепы by construction (они смотрят на согласованность
 * корней и на плагин, а не на то, докуда пин дотягивается). Именно так и вышло: пин
 * стоял на шестом мажоре, тогда как препятствие живёт в восьмом, а седьмой был измерен
 * рабочим — то есть пин был шире причины на целый мажор, и покраснеть было нечему.
 *
 * ГРАНИЦА ПРЕПЯТСТВИЯ ВЫВОДИТСЯ, А НЕ ВЫПИСЫВАЕТСЯ. Названная причина — смена
 * сборочного ядра: у мажоров с `rollup` + `esbuild` подстановка метки стилей работает,
 * у мажора с `rolldown` минификатор перекавычивает литералы и она ломается. Значит
 * «первый мажор с препятствием» механически читается из реестра по составу
 * зависимостей самого сборщика, и число «восемь» в гейте не хранится: уедет причина —
 * уедет и граница. Выписанное число пережило бы свой предмет ровно так же, как
 * пережила его причина в тексте записи.
 *
 * ЧЕГО ГЕЙТ НЕ ДЕЛАЕТ И НЕ МОЖЕТ. Он не отвечает на вопрос «работает ли федерация
 * под другим мажором» — на него отвечает только исполнение, и для этого рядом стоит
 * `check-federation-executes.mjs`. Здесь проверяется ровно одно: не пережила ли
 * ПРИЧИНА пина сам предмет и не шире ли пин этой причины. Признак того места намеренно
 * узкий и назван прямо, чтобы его ложное срабатывание читалось как «перемерить», а не
 * как «всё сломалось».
 *
 * Гейт заявляет ОБЪЁМ ОСМОТРЕННОГО: «ноль находок» обязано быть отличимо от «ноль
 * прочитанного». Набор корней ВЫВОДИТСЯ из дерева, а не выписывается — выписанный
 * переживёт добавление корня и промолчит о нём. Проверка 5 отдельно докладывает, была
 * ли она вообще исполнена: реестр недоступен → она пропускается ВСЛУХ (так же, как это
 * делает `.github/scripts/check-pinned-tools.sh` при недоступном апстриме), а не
 * зачитывается в «ноль находок».
 *
 * Запуск из ui-future/:
 *   node scripts/check-federation-pin-still-needed.mjs
 *   node scripts/check-federation-pin-still-needed.mjs --self-test  // доказать, что
 *       проверка 5 способна упасть и способна смолчать (инъекция в обе стороны, без сети)
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

// ─── 5. Пин не шире своей причины ────────────────────────────────────────────
//
// Разбор ведётся ЧИСТОЙ функцией над данными реестра, поэтому самопроверка кормит её
// синтетикой и доказывает способность упасть, не выходя в сеть.

/** Мажор, который допускает диапазон вида `^X.Y.Z`. Иная форма — не догадываемся. */
function caretMajor(range) {
  const m = /^\^(\d+)\.(\d+)\.(\d+)$/.exec(String(range ?? ""));
  return m ? Number(m[1]) : null;
}

/**
 * Несёт ли РЕЛИЗ названное препятствие. Признак — сборочное ядро, а не номер:
 * `rolldown` вместо пары `rollup` + `esbuild`.
 */
function releaseCarriesObstacle(deps) {
  const names = Object.keys(deps ?? {});
  const rolldown = names.some((n) => n === "rolldown" || n.startsWith("rolldown-"));
  const rollup = names.some((n) => n === "rollup");
  return rolldown && !rollup;
}

/** Последний СТАБИЛЬНЫЙ релиз каждого мажора (предрелизы не свидетельствуют о линии). */
function latestStablePerMajor(versions) {
  const best = new Map();
  for (const [v, meta] of Object.entries(versions ?? {})) {
    const m = /^(\d+)\.(\d+)\.(\d+)$/.exec(v);
    if (!m) continue;
    const major = Number(m[1]);
    const key = [Number(m[1]), Number(m[2]), Number(m[3])];
    const prev = best.get(major);
    if (!prev || key[1] > prev.key[1] || (key[1] === prev.key[1] && key[2] > prev.key[2])) {
      best.set(major, { version: v, key, deps: meta?.dependencies });
    }
  }
  return best;
}

/**
 * Сводит ОБЪЯВЛЕННЫЙ пин с ГРАНИЦЕЙ ПРЕПЯТСТВИЯ, выведенной из реестра.
 * Возвращает находки и строку объёма — никогда не печатает и не выходит сам.
 */
export function adjudicatePinWidth({ declaredRange, versions }) {
  const out = { findings: [], note: "" };
  const pinned = caretMajor(declaredRange);
  if (pinned === null) {
    out.findings.push(
      `форма диапазона сборщика «${declaredRange}» не опознана (ожидалось «^X.Y.Z») — ` +
        "ширину пина не с чем сравнить, проверка 5 не может утверждать ничего",
    );
    return out;
  }

  const perMajor = latestStablePerMajor(versions);
  if (perMajor.size === 0) {
    out.findings.push("реестр не дал ни одного стабильного релиза сборщика — предпосылка проверки 5 не выполнена");
    return out;
  }

  const majors = [...perMajor.keys()].sort((a, b) => a - b);
  const withObstacle = majors.filter((m) => releaseCarriesObstacle(perMajor.get(m).deps));
  if (withObstacle.length === 0) {
    out.findings.push(
      "ни один опубликованный мажор сборщика не несёт названного препятствия " +
        "(сборочное ядро нигде не сменилось на rolldown) — причина пина реестром НЕ подтверждается: " +
        "перемерить федерацию и пересмотреть запись в docs/architecture/known-divergences.md",
    );
    return out;
  }

  const obstacle = withObstacle[0];
  const free = majors.filter((m) => m < obstacle).pop();
  out.note =
    `первый мажор с препятствием — ${obstacle} (${perMajor.get(obstacle).version}, ядро rolldown); ` +
    `свободен от него ${free} (${perMajor.get(free).version}); пин держит ${pinned}`;

  if (pinned >= obstacle) {
    out.findings.push(
      `пин «${declaredRange}» ДОПУСКАЕТ мажор ${obstacle}, несущий названное препятствие — ` +
        "федерация под ним не исполняется (см. запись о сборочной цепочке)",
    );
  } else if (free !== undefined && pinned < free) {
    out.findings.push(
      `пин «${declaredRange}» ШИРЕ своей причины: он держит мажор ${pinned}, тогда как препятствие ` +
        `появляется только в ${obstacle}, а ${free} от него свободен. Причина закрывает один мажор, ` +
        "а пин — два и больше: либо поднять пин до свободного мажора (вердикт даёт " +
        "check-federation-executes.mjs плюс сквозные пробы консоли), либо назвать в " +
        "docs/architecture/known-divergences.md ВТОРОЕ препятствие, которое держит его ниже",
    );
  }
  return out;
}

async function fetchBundlerVersions() {
  const url = `https://registry.npmjs.org/${BUNDLER}`;
  const res = await fetch(url, {
    headers: { accept: "application/vnd.npm.install-v1+json" },
    signal: AbortSignal.timeout(20000),
  });
  if (!res.ok) throw new Error(`реестр ответил ${res.status}`);
  return (await res.json()).versions;
}

// `--self-test` ДОБАВЛЯЕТ инъекцию к обычному прогону, а не заменяет его: самопроверка,
// глушащая боевой вердикт, — это «зелёное относится к меньшему, чем думаешь». Поэтому
// боевая проверка 5 идёт всегда, и в CI хватает одного вызова с флагом.
let widthNote = "";
try {
  const versions = await fetchBundlerVersions();
  const verdict = adjudicatePinWidth({ declaredRange: declared[0].bundler, versions });
  findings.push(...verdict.findings);
  widthNote = verdict.note || "разбор не дал границы";
} catch (err) {
  // Апстрим недоступен — это НЕ «ноль находок». Проверка пропускается вслух, чтобы
  // непроверенное не зачлось в проверенное (тот же порядок, что у check-pinned-tools.sh).
  const shown = err instanceof Error ? `${err.name}: ${err.message}` : String(err);
  widthNote = `ПРОПУЩЕНА — реестр недоступен (${shown}); ширина пина в этом прогоне НЕ сверена`;
}

// ─── Объём осмотренного ──────────────────────────────────────────────────────
const roots = declared.map((d) => d.root).join(", ");
console.log(
  `осмотрено: манифестов ${manifests.length}, из них объявляют сборочную цепочку ${declared.length} (${roots}); ` +
    `${BUNDLER} «${declared[0].bundler ?? "—"}», ${FEDERATION} «${declared[0].federation ?? "—"}»`,
);
console.log(`предмет пина: ${subjectStillThere ? `на месте (${subjectNote})` : "НЕ подтверждён"}`);
console.log(`ширина пина: ${widthNote}`);

// ─── САМОПРОВЕРКА проверки 5: инъекция в обе стороны, сети не требует ─────────
if (process.argv.includes("--self-test")) {
  // Синтетический реестр той же формы, что отдаёт настоящий: ядро сборки читается из
  // состава зависимостей релиза.
  const rollupCore = { rollup: "^4", esbuild: "^0.25" };
  const rolldownCore = { rolldown: "^1" };
  const registry = (obstacleFrom) =>
    Object.fromEntries(
      [6, 7, 8].map((m) => [`${m}.0.0`, { dependencies: m >= obstacleFrom ? rolldownCore : rollupCore }]),
    );

  const cases = [
    // Инъекция настоящим дефектом: препятствие в 8, пин держит 6 — ровно то состояние,
    // из-за которого заведена задача #337. Гейт ОБЯЗАН покраснеть.
    { name: "пин шире причины (препятствие в 8, пин ^6)", range: "^6.1.0", obstacleFrom: 8, expect: "красное" },
    // ЗАКОННЫЙ БЛИЗНЕЦ той же формы: пин дотянут до свободного мажора. Молчание обязательно,
    // иначе гейт ловит форму, а не существо, и первый же ложный срабат его снимет.
    { name: "пин по причине (препятствие в 8, пин ^7)", range: "^7.3.6", obstacleFrom: 8, expect: "молчание" },
    // Контроль того, что граница ВЫВОДИТСЯ, а не зашита: сдвигаем препятствие на 7 —
    // и тот же самый пин ^6, который выше был находкой, обязан стать законным.
    { name: "граница следует причине (препятствие в 7, пин ^6)", range: "^6.1.0", obstacleFrom: 7, expect: "молчание" },
    // Пин, впустивший мажор с препятствием.
    { name: "пин допускает мажор с препятствием (пин ^8)", range: "^8.0.0", obstacleFrom: 8, expect: "красное" },
    // Причина исчезла у апстрима целиком — это находка, а не радость.
    { name: "препятствия нет ни в одном мажоре", range: "^6.1.0", obstacleFrom: 99, expect: "красное" },
  ];

  let stFailures = 0;
  for (const c of cases) {
    const got = adjudicatePinWidth({ declaredRange: c.range, versions: registry(c.obstacleFrom) });
    const red = got.findings.length > 0;
    const ok = c.expect === "красное" ? red : !red;
    console.log(`  самопроверка[${ok ? "ok" : "ПРОВАЛ"}] ${c.name} → ${red ? "красное" : "молчание"} (ждали ${c.expect})`);
    if (!ok) {
      stFailures += 1;
      for (const f of got.findings) console.log(`      ${f}`);
    }
  }
  if (stFailures) {
    console.error(`::error::самопроверка проверки 5 провалена: случаев не сошлось ${stFailures} из ${cases.length}`);
    process.exit(1);
  }
  console.log(`самопроверка проверки 5: случаев сошлось ${cases.length} из ${cases.length} (инъекция ловится, законное молчит)`);
}

if (findings.length) {
  for (const f of findings) console.error(`::error::${f}`);
  console.error(`\n✖ находок: ${findings.length}`);
  process.exit(1);
}

console.log("✓ цепочка сборки консоли одна на все корни, и у её пина по-прежнему есть предмет");
