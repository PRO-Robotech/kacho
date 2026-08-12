#!/usr/bin/env node
// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

/**
 * Гейт: у КАЖДОГО принятого отступления от политики контента консоли есть предмет,
 * запись о нём и предикат снятия, который РОНЯЕТ прогон, когда наступает.
 *
 * ── Предмет ──────────────────────────────────────────────────────────────────
 *
 * Отступление от CSP принималось прозой: текст объяснял, почему не чиним, и называл
 * условие снятия («когда провайдер перестанет эмитить пустой скрипт», «когда antd
 * начнёт ставить nonce»). Условие при этом не держалось ничем. Такая запись переживает
 * своё основание и через месяц читается как принятое решение: предмет мог исчезнуть,
 * версия — уехать, политика — быть ослабленной ровно тем способом, который запись
 * запрещает, — и ни одно место в дереве об этом не скажет.
 *
 * ── Что проверяется ──────────────────────────────────────────────────────────
 *
 *  1. ОТСТУПЛЕНИЕ В ПОЛИТИКЕ ИМЕЕТ ЗАПИСЬ. Каждый токен объявленной политики за
 *     пределами базового набора (см. BASELINE) обязан быть заявлен записью. Новое
 *     послабление, введённое без записи, — находка.
 *  2. У ЗАПИСИ ЕСТЬ ПРЕДМЕТ. Исчез предмет — запись принимает несуществующее: находка
 *     («снять запись»), а не тихое зеленение. Послабление истекает само.
 *  3. ИЗМЕРЕНИЕ ОТНОСИТСЯ К ТОМУ, ЧТО В ДЕРЕВЕ. Запись называет пины, против которых
 *     она измерена (версия чужого образа, версия чужой библиотеки). Пин уехал —
 *     измерение больше не относится к развёрнутому: находка «перемерить».
 *  4. ЗАПИСЬ, КОТОРУЮ НИЧТО НЕ ДЕРЖИТ. Строка-якорь в доке с идентификатором, которого
 *     этот гейт не знает, — находка: запись есть, предмета у неё в проверке нет.
 *
 * Пин — ПРОКСИ-предикат: он ломается раньше своего предмета (образ могли поднять по
 * причине, к встроенному скрипту не относящейся). Направление этой неточности выбрано
 * осознанно: ложное срабатывание требует ПЕРЕМЕРИТЬ и обновить запись, а не оставляет
 * запись тихо жить после того, как её основание исчезло.
 *
 * ── Проверка СВОЕЙ предпосылки ───────────────────────────────────────────────
 *
 * Гейт обоснован тем, что в дереве есть: объявления политики, шаблон края консоли,
 * умбрелла с пином провайдера, объявления antd и сам файл записей. Пропадёт любое —
 * это ПАДЕНИЕ с кодом 2, а не «ноль находок»: иначе гейт переживёт свой предмет.
 * Печатается объём осмотренного, чтобы «ноль находок» было отличимо от «ноль
 * прочитанного».
 *
 * Единица счёта — отслеживаемый git-элемент (`git ls-files`), а не то, что лежит на диске.
 *
 * Запуск из ui-future/:  node scripts/check-csp-divergence-records.mjs
 * Выход 1 — есть находки; 2 — не выполнена предпосылка.
 */

import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";

// ── предпосылка: запускаемся из ui-future/, рядом лежит дерево развёртывания ──
const uiRoot = process.cwd();
if (!fs.existsSync(path.join(uiRoot, "package.json"))) {
  console.error("::error::запускать из ui-future/ (нет package.json в текущем каталоге)");
  process.exit(2);
}
const repoRoot = path.resolve(uiRoot, "..");
const UMBRELLA = "deploy/helm/umbrella";
if (!fs.existsSync(path.join(repoRoot, UMBRELLA))) {
  console.error(
    `::error::нет ${UMBRELLA} рядом с ui-future — пин провайдера страницы входа брать неоткуда. ` +
      "Гейт обоснован его существованием: это падение, а не «ноль находок».",
  );
  process.exit(2);
}

const RECORDS_DOC = "docs/architecture/known-divergences.md";
const VALUES = "deploy/values.yaml";
const NGINX_TEMPLATE = "deploy/templates/configmap-nginx.yaml";

const volume = { files: 0 };
function read(absPath) {
  const text = fs.readFileSync(absPath, "utf8");
  volume.files += 1;
  return text;
}
function trackedFiles(root, pattern) {
  return execFileSync("git", ["ls-files", pattern], { cwd: root, encoding: "utf8" })
    .split("\n")
    .filter(Boolean);
}

// ── объявления политики ──────────────────────────────────────────────────────

/** Разбирает текст политики в карту «директива → токены». */
function parsePolicy(text) {
  const out = new Map();
  for (const chunk of text.split(";")) {
    const parts = chunk.trim().split(/\s+/).filter(Boolean);
    if (parts.length === 0) continue;
    out.set(parts[0], parts.slice(1));
  }
  return out;
}

/**
 * Все объявления политики в дереве консоли: значение чарта (его рендерит helm на
 * каждый listener) и запечённые в образ nginx.conf (они действуют, когда образ
 * запускают напрямую). Читаются оба вида: послабление, введённое в один из них,
 * действует ровно там, где его никто не искал.
 */
function cspDeclarations() {
  const found = [];

  const valuesText = read(path.join(uiRoot, VALUES));
  const lines = valuesText.split("\n");
  for (let i = 0; i < lines.length; i += 1) {
    const inline = /^(\s*)contentSecurityPolicy:\s*(?:>-?|\|-?)?\s*(.*)$/.exec(lines[i]);
    if (!inline) continue;
    const indent = inline[1].length;
    let policy = inline[2].replace(/^["']|["']$/g, "").trim();
    if (policy === "") {
      // блочный скаляр: собираем более отступленные строки до дедента
      for (let j = i + 1; j < lines.length; j += 1) {
        if (lines[j].trim() === "") continue;
        const curIndent = lines[j].length - lines[j].trimStart().length;
        if (curIndent <= indent) break;
        policy += ` ${lines[j].trim()}`;
      }
    }
    found.push({ where: `${VALUES}:${i + 1}`, policy: policy.trim() });
  }

  for (const rel of trackedFiles(uiRoot, "*nginx.conf")) {
    const text = read(path.join(uiRoot, rel));
    text.split("\n").forEach((line, idx) => {
      const m = /add_header\s+Content-Security-Policy\s+"([^"]*)"/.exec(line);
      if (m && !m[1].includes("{{")) found.push({ where: `${rel}:${idx + 1}`, policy: m[1] });
    });
  }
  return found;
}

/**
 * Токены, которые отступлением НЕ являются: сам источник, запрет, и две схемы,
 * которыми консоль встраивает свои же ассеты. Всё остальное — послабление и обязано
 * быть заявлено записью.
 */
const BASELINE = new Set(["'self'", "'none'", "data:", "blob:"]);

// ── пины, против которых измерены записи ─────────────────────────────────────

/**
 * Пины стороннего UI входа/регистрации: `repository` + `tag` умбреллы (умолчание
 * чарта и переопределения профилей). Читается ОБЪЯВЛЕНИЕ, а не отрендеренный
 * шаблон: рендер требует helm и сети, поэтому пропускался бы ровно там, где некому
 * заметить.
 */
function providerPins() {
  const pins = new Set();
  const marker = "kratos-selfservice-ui";
  for (const rel of trackedFiles(repoRoot, `${UMBRELLA}/*.yaml`).concat(
    trackedFiles(repoRoot, `${UMBRELLA}/charts/*/values.yaml`),
  )) {
    const lines = read(path.join(repoRoot, rel)).split("\n");
    for (let i = 0; i < lines.length; i += 1) {
      const line = lines[i];
      if (/^\s*#/.test(line)) continue;

      const single = /^\s*image:\s*["']?([^"'\s#]+)["']?\s*$/.exec(line);
      if (single && single[1].includes(marker)) {
        pins.add(single[1]);
        continue;
      }

      const repo = /^(\s*)repository:\s*["']?([^"'\s#]+)["']?/.exec(line);
      if (!repo || !repo[2].includes(marker)) continue;
      const indent = repo[1].length;
      let tag = null;
      // `tag` — СОСЕД `repository` в том же отображении. Между ними законно стоят
      // другие ключи (`pullPolicy`, `digest`) и комментарии, поэтому поиск
      // прекращается только на дедента и на начале следующего образа, а не на
      // первом же соседе — иначе законная раскладка читалась бы как «без тега».
      for (let j = i + 1; j < lines.length; j += 1) {
        if (lines[j].trim() === "" || /^\s*#/.test(lines[j])) continue;
        const curIndent = lines[j].length - lines[j].trimStart().length;
        if (curIndent < indent) break;
        if (/^\s*repository:/.test(lines[j])) break;
        const t = /^\s*tag:\s*["']?([^"'\s#]+)["']?/.exec(lines[j]);
        if (t && curIndent === indent) {
          tag = t[1];
          break;
        }
      }
      pins.add(tag ? `${repo[2]}:${tag}` : `${repo[2]}:БЕЗ-ТЕГА(${rel}:${i + 1})`);
    }
  }
  return pins;
}

/** Пины UI-набора: объявления antd в отслеживаемых package.json консоли. */
function antdPins() {
  const pins = new Set();
  for (const rel of trackedFiles(uiRoot, "*package.json")) {
    if (rel.includes("node_modules/")) continue;
    let pkg;
    try {
      pkg = JSON.parse(read(path.join(uiRoot, rel)));
    } catch (e) {
      console.error(`::error::${rel} не разобрался как JSON: ${e.message}`);
      process.exit(2);
    }
    const range = pkg.dependencies?.antd ?? pkg.devDependencies?.antd;
    if (range) pins.add(`antd@${range}`);
  }
  return pins;
}

// ── предметы записей ─────────────────────────────────────────────────────────

/**
 * Предмет записи о `style-src`: послабление объявлено хотя бы в одном объявлении
 * политики. Пропало — принимать больше нечего.
 */
function subjectStyleSrcUnsafeInline(declarations) {
  const at = declarations.filter((d) => {
    const styleSrc = parsePolicy(d.policy).get("style-src") ?? [];
    return styleSrc.includes("'unsafe-inline'");
  });
  return at.length > 0
    ? { ok: true, detail: `объявлений с послаблением: ${at.length}` }
    : {
        ok: false,
        why:
          "ни одно объявление политики больше не несёт 'unsafe-inline' в style-src — " +
          "принимать нечего: снять запись из дока и её ветку из этого гейта",
      };
}

/**
 * Предмет записи о странице входа: край консоли проксирует полосу входа стороннему
 * UI провайдера личности, и серверные заголовки (в т.ч. политика) на эту полосу
 * НАСЛЕДУЮТСЯ. nginx наследует `add_header` с внешнего уровня, только пока у полосы
 * нет СВОИХ: появился хоть один — политика на страницу входа больше не едет, и
 * запись описывает не то, что происходит.
 */
function subjectProxiedLoginUnderOurPolicy(declarations) {
  const text = read(path.join(uiRoot, NGINX_TEMPLATE));
  const lines = text.split("\n");

  let head = -1;
  for (let i = 0; i < lines.length; i += 1) {
    if (/^\s*location\s+.*\blogin\b/.test(lines[i]) && lines[i].includes("{")) head = i;
  }
  if (head === -1) {
    return {
      ok: false,
      why:
        `${NGINX_TEMPLATE} больше не объявляет полосу входа — предмет записи исчез: ` +
        "страницу входа отдаёт что-то другое, запись обязана быть перемерена или снята",
    };
  }

  const indent = lines[head].length - lines[head].trimStart().length;
  let end = lines.length;
  for (let i = head + 1; i < lines.length; i += 1) {
    const cur = lines[i].length - lines[i].trimStart().length;
    if (lines[i].trim() === "}" && cur === indent) {
      end = i;
      break;
    }
  }
  const block = lines.slice(head, end).join("\n");

  if (!/proxy_pass\s+http:\/\/\$/.test(block) || !/KRATOS_UI/.test(block)) {
    return {
      ok: false,
      why:
        "полоса входа больше не проксируется стороннему UI провайдера личности — " +
        "запись принимала нарушение на ЧУЖОЙ странице, у неё нет предмета",
    };
  }
  if (/add_header/.test(block)) {
    return {
      ok: false,
      why:
        "у полосы входа появились СВОИ заголовки: nginx наследует внешние add_header, " +
        "только пока своих нет, — значит политика консоли на страницу входа больше не " +
        "едет, и запись описывает не то, что происходит",
    };
  }

  const relaxed = declarations.filter((d) => {
    const scriptSrc = parsePolicy(d.policy).get("script-src") ?? [];
    return scriptSrc.some((t) => t !== "'self'");
  });
  if (relaxed.length > 0) {
    return {
      ok: false,
      why:
        `script-src ослаблен (${relaxed.map((r) => r.where).join(", ")}) — ` +
        "запись прямо запрещает этот исход: послабление ради чужого встроенного скрипта " +
        "открывает исполнение произвольных встроенных скриптов на странице ввода пароля",
    };
  }
  return { ok: true, detail: "полоса входа проксируется, политика на неё наследуется" };
}

// ── записи, известные гейту ──────────────────────────────────────────────────

const RECORDS = [
  {
    id: "style-src-unsafe-inline",
    claims: (dev) => dev.directive === "style-src" && dev.token === "'unsafe-inline'",
    subject: subjectStyleSrcUnsafeInline,
    pins: antdPins,
    pinsAbout: "версия UI-набора, чей рантайм-движок стилей и требует послабления",
  },
  {
    id: "login-page-inline-script",
    claims: () => false, // не послабление политики, а нарушение, оставленное стоять
    subject: subjectProxiedLoginUnderOurPolicy,
    pins: providerPins,
    pinsAbout: "версия стороннего UI входа — единственное, от чего зависит его разметка",
  },
];

// ── разбор записей в доке ────────────────────────────────────────────────────

const ANCHOR = /^\*\*Запись гейта:\*\*\s*`csp:([a-z0-9-]+)`/;
const MEASURED = /^\*\*Измерено против:\*\*\s*(.+)$/;

function docRecords() {
  const abs = path.join(uiRoot, RECORDS_DOC);
  if (!fs.existsSync(abs)) {
    console.error(
      `::error::нет ${RECORDS_DOC} — файла принятых отступлений консоли не существует. ` +
        "Гейт обоснован его существованием: это падение, а не «ноль находок».",
    );
    process.exit(2);
  }
  const lines = read(abs).split("\n");
  const records = new Map();
  for (let i = 0; i < lines.length; i += 1) {
    const a = ANCHOR.exec(lines[i]);
    if (!a) continue;
    let pins = null;
    for (let j = i + 1; j < Math.min(i + 6, lines.length); j += 1) {
      const m = MEASURED.exec(lines[j]);
      if (m) {
        pins = [...m[1].matchAll(/`([^`]+)`/g)].map((x) => x[1]);
        break;
      }
    }
    records.set(a[1], { line: i + 1, pins });
  }
  return records;
}

// ── прогон ───────────────────────────────────────────────────────────────────

const declarations = cspDeclarations();
if (declarations.length === 0) {
  console.error(
    "::error::в дереве консоли не найдено НИ ОДНОГО объявления политики контента — " +
      "читать было нечего, это падение, а не «ноль находок».",
  );
  process.exit(2);
}

const doc = docRecords();
const findings = [];

// (1) каждое послабление в объявленной политике заявлено записью
const deviations = [];
for (const d of declarations) {
  for (const [directive, tokens] of parsePolicy(d.policy)) {
    for (const token of tokens) {
      if (!BASELINE.has(token)) deviations.push({ where: d.where, directive, token });
    }
  }
}
for (const dev of deviations) {
  const owner = RECORDS.find((r) => r.claims(dev));
  if (!owner) {
    findings.push(
      `${dev.where}: политика несёт «${dev.token}» в ${dev.directive}, и этого послабления ` +
        `не заявляет ни одна запись. Послабление без записи не имеет ни причины, ни условия ` +
        `снятия: либо убрать токен, либо завести запись в ${RECORDS_DOC} и её ветку здесь.`,
    );
  }
}

// (2)-(3) у каждой известной записи есть предмет, объявление в доке и совпадающие пины
let pinsCompared = 0;
for (const record of RECORDS) {
  const subject = record.subject(declarations);
  const declared = doc.get(record.id);

  if (!subject.ok) {
    findings.push(`запись csp:${record.id} — ${subject.why}`);
    continue;
  }
  if (!declared) {
    findings.push(
      `предмет записи csp:${record.id} в дереве ЕСТЬ (${subject.detail}), а самой записи в ` +
        `${RECORDS_DOC} нет: отступление принято молча, условия снятия не назвал никто. ` +
        "Завести запись со строками «**Запись гейта:**» и «**Измерено против:**».",
    );
    continue;
  }
  if (!declared.pins || declared.pins.length === 0) {
    findings.push(
      `${RECORDS_DOC}:${declared.line} — у записи csp:${record.id} нет строки ` +
        "«**Измерено против:**» с пинами. Без неё запись нечем просрочить: " +
        `назвать ${record.pinsAbout}.`,
    );
    continue;
  }

  const actual = [...record.pins()].sort();
  const expected = [...declared.pins].sort();
  pinsCompared += actual.length;
  if (actual.length === 0) {
    console.error(
      `::error::пины для csp:${record.id} не извлеклись из дерева — сравнивать не с чем ` +
        "(предпосылка гейта не выполняется, а не «совпало»).",
    );
    process.exit(2);
  }
  if (actual.join(" ") !== expected.join(" ")) {
    findings.push(
      `${RECORDS_DOC}:${declared.line} — запись csp:${record.id} измерена против ` +
        `[${expected.join(", ")}], а дерево несёт [${actual.join(", ")}]. ` +
        "Условие снятия наступило: перемерить и либо снять запись, либо обновить в ней пины.",
    );
  }
}

// (4) якорь в доке, которого гейт не знает
for (const [id, at] of doc) {
  if (!RECORDS.some((r) => r.id === id)) {
    findings.push(
      `${RECORDS_DOC}:${at.line} — запись csp:${id} не держится ничем: этот гейт про неё ` +
        "не знает, поэтому её условие снятия не может наступить наблюдаемо. " +
        "Добавить ветку с предметом и пинами в scripts/check-csp-divergence-records.mjs.",
    );
  }
}

console.log(
  `осмотрено: файлов прочитано ${volume.files}, объявлений политики ${declarations.length}, ` +
    `токенов вне базового набора ${deviations.length}, записей в доке ${doc.size}, ` +
    `записей известно гейту ${RECORDS.length}, пинов сверено ${pinsCompared}`,
);
console.log(`  объявления: ${declarations.map((d) => d.where).join(", ")}`);

if (findings.length) {
  for (const f of findings) console.error(`::error::${f}`);
  console.error(`\n✖ находок: ${findings.length}`);
  process.exit(1);
}
console.log("✓ каждое принятое отступление от политики контента имеет предмет, запись и живой предикат снятия");
