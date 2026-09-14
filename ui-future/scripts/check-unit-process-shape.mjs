#!/usr/bin/env node
// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

/**
 * Форма ПРОЦЕССА, в котором пакет консоли гоняет свои юниты.
 *
 * ПРЕДМЕТ. Объявление `scripts.test` пакета решает, сколько памяти прогон удержит
 * в ОДНОМ процессе. `jest --runInBand` держит ВСЕ сюиты пакета в одном процессе, и
 * потребление растёт вместе с деревом — на пределе это `Ineffective mark-compacts
 * near heap limit` и код 134, то есть «не выполнилось», а не находка о продукте.
 *
 * ЭТО НАБЛЮДАЛОСЬ, а не выведено. 2026-09-13, пять требуемых контекстов защиты
 * ствола (`unit (compute)`, `unit (iam)`, `unit (nlb)`, `unit (registry)`,
 * `unit (system)`) падали кодом 134 у ТРЁХ разных запросов слияния, ни один из
 * которых консоли не трогал, — и падали только на наших ранерах: образ
 * GitHub-hosted давал больше памяти, и тот же коммит там зеленел. Вердикт о
 * продукте зависел от того, кому очередь отдала задание.
 *
 * ПОЧЕМУ РАЗМЕР ДЕРЕВА ЗДЕСЬ НЕ СЛУЧАЕН. Семь пакетов из девяти включают в свой
 * `testMatch` общие пробы из `../shared/src` (образец `.test.ts`/`.test.tsx`) —
 * это 282 сюиты, то есть
 * основная масса прогона у каждого. Предикат:
 *   python3 -c "import glob,io,re; print(sum(1 for f in glob.glob('ui-future/'+chr(42)+'/jest.config.cjs') if (lambda m: bool(m and 'shared/src' in m.group(1)))(re.search(r'testMatch.{0,4}\[(.+?)\]', io.open(f).read(), re.S))))"
 * даёт 7 — и единица счёта здесь ВАЖНА: по подстроке `shared/src` вообще
 * находятся все девять, потому что алиас `@shared/` объявлен и у `dashboard` с
 * `host`. Считать надо вхождение в `testMatch`, а не в файл.
 * Пока общий пакет судится под каждым потребителем, объём прогона пакета не
 * уменьшится сам.
 *
 * ЧТО ТРЕБУЕТСЯ. Прогон обязан ВОЗВРАЩАТЬ память ОС по ходу дела, а не к концу:
 * либо воркеры с пределом простоя (`--workerIdleMemoryLimit`, воркер
 * перезапускается по достижении предела), либо порционный прогонщик, зовущий jest
 * несколькими процессами. `--runInBand` без такого предела запрещён.
 *
 * ПОДПОРКА `--max-old-space-size` ОТВЕРГНУТА ОСОЗНАННО, и это уже решалось в этом
 * дереве: #934 сняла её у гейта линта, заменив УСТРОЙСТВОМ (процесс на пакет),
 * потому что подпорка отодвигает предел, а не убирает рост. Поэтому она здесь не
 * считается выполнением требования — и гейт говорит об этом отдельной находкой,
 * а не молчит.
 *
 * ЗАМЕР, которым выбрано устройство (пакет `system`, 288 сюит, 3149 проб, одно
 * дерево, различие только в форме процесса):
 *   --runInBand, один процесс                        пик 3.58 ГиБ · 128 с
 *   порции по 40 файлов, --runInBand в каждой         пик 1.09 ГиБ · 107 с
 *   --maxWorkers=2 --workerIdleMemoryLimit=700MB      пик 0.96 ГиБ ·  50 с
 * Третья форма выигрывает по всем трём осям при том же покрытии, поэтому требуется
 * она, а порционный прогонщик остаётся законной альтернативой.
 *
 * ГРАНИЦА, названная честно. Гейт судит ОБЪЯВЛЕНИЕ, а не потребление: он не
 * измеряет память прогона и не может — для этого нужен сам прогон. Он исключает
 * форму, про которую известно, что она не возвращает память; он НЕ доказывает, что
 * прогон в предел уложится.
 *
 * ПУСТОЙ ОБХОД — НАХОДКА: «ноль пакетов без предела» обязано быть отличимо от
 * «ни одного объявления не прочитано».
 */

import { readFileSync, readdirSync, statSync } from "node:fs";
import { join, dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), "..");

// Формы, возвращающие память ОС по ходу прогона.
const LIMIT_FLAG = "--workerIdleMemoryLimit";
const CHUNKED_RUNNER = "run-unit-chunks";
// Подпорка, отвергнутая #934: отодвигает предел, роста не убирает.
const PROP = "--max-old-space-size";
const IN_BAND = "--runInBand";

export function judge(declarations) {
  const findings = [];
  for (const { pkg, test } of declarations) {
    const hasLimit = test.includes(LIMIT_FLAG) || test.includes(CHUNKED_RUNNER);
    if (hasLimit) {
      if (test.includes(IN_BAND) && !test.includes(CHUNKED_RUNNER)) {
        findings.push(
          `${pkg}: предел простоя воркера задан вместе с ${IN_BAND} — в одном ` +
            `процессе предел не действует, воркеров нет`,
        );
      }
      continue;
    }
    if (test.includes(PROP)) {
      findings.push(
        `${pkg}: память отодвинута подпоркой ${PROP} вместо возврата ОС. ` +
          `#934 сняла эту подпорку у гейта линта, заменив устройством: подпорка ` +
          `отодвигает предел, а не убирает рост`,
      );
      continue;
    }
    findings.push(
      `${pkg}: прогон держит все сюиты пакета в ОДНОМ процессе ` +
        `(${test.includes(IN_BAND) ? IN_BAND : "нет ни воркеров, ни порций"}). ` +
        `Нужен ${LIMIT_FLAG}=<N>MB с воркерами либо порционный прогонщик ` +
        `(${CHUNKED_RUNNER})`,
    );
  }
  return findings;
}

function declarationsInTree() {
  const out = [];
  for (const name of readdirSync(ROOT).sort()) {
    const dir = join(ROOT, name);
    if (name === "node_modules" || name === "scripts") continue;
    let st;
    try {
      st = statSync(dir);
    } catch {
      continue;
    }
    if (!st.isDirectory()) continue;
    let doc;
    try {
      doc = JSON.parse(readFileSync(join(dir, "package.json"), "utf8"));
    } catch {
      continue;
    }
    const test = doc?.scripts?.test;
    if (typeof test !== "string" || !test.includes("jest")) continue;
    out.push({ pkg: name, test });
  }
  return out;
}

function selfTest() {
  const cases = [
    ["контроль: воркеры с пределом простоя → молчит", [{ pkg: "a", test: "jest --maxWorkers=2 --workerIdleMemoryLimit=700MB" }], 0, null],
    ["дефект: один процесс на всё дерево → находка", [{ pkg: "a", test: "jest --runInBand --ci" }], 1, "ОДНОМ процессе"],
    ["дефект: подпорка вместо возврата памяти → находка про #934", [{ pkg: "a", test: "node --max-old-space-size=6144 jest --runInBand" }], 1, "#934"],
    ["дефект: предел простоя рядом с --runInBand → находка (предел не действует)", [{ pkg: "a", test: "jest --runInBand --workerIdleMemoryLimit=700MB" }], 1, "не действует"],
    ["законный близнец: порционный прогонщик → молчит", [{ pkg: "a", test: "node ../scripts/run-unit-chunks.mjs" }], 0, null],
    ["дефект в одном из двух: второй не прикрывает первого", [{ pkg: "a", test: "jest --workerIdleMemoryLimit=700MB" }, { pkg: "b", test: "jest --runInBand" }], 1, "b:"],
  ];
  let ok = true;
  for (const [name, decls, want, needle] of cases) {
    const f = judge(decls);
    const good = f.length === want && (needle === null || f.some((x) => x.includes(needle)));
    ok &&= good;
    console.log(`  [${good ? "OK " : "ОТКАЗ"}] ${name}: находок ${f.length} (ждали ${want})`);
  }
  // Пустой обход: судить нечего — и это НЕ зелёное.
  const empty = judge([]);
  const emptyOk = empty.length === 0;
  console.log(`  [${emptyOk ? "OK " : "ОТКАЗ"}] пустой вход даёт ноль находок — поэтому объём обхода печатает вызывающий, а не эта функция`);
  ok &&= emptyOk;
  console.log("самопроба:", ok ? "ПРОЙДЕНА" : "ПРОВАЛЕНА");
  return ok ? 0 : 1;
}

function main() {
  if (process.argv.includes("--self-test")) return selfTest();
  const decls = declarationsInTree();
  console.log(`осмотрено объявлений прогона юнитов: ${decls.length} (${decls.map((d) => d.pkg).join(", ")})`);
  if (decls.length === 0) {
    console.error(
      "ОТКАЗ: не прочитано НИ ОДНОГО объявления `scripts.test` с jest. " +
        "Это «не выполнилось», а не «нарушений нет»: обход пуст, судить нечего",
    );
    return 2;
  }
  const findings = judge(decls);
  if (findings.length === 0) {
    console.log(
      `форма процесса сходится: у всех ${decls.length} пакетов прогон возвращает ` +
        `память ОС по ходу дела (предел простоя воркера либо порционный прогонщик)`,
    );
    return 0;
  }
  console.error(`НАХОДОК ${findings.length}:`);
  for (const f of findings) console.error(`  · ${f}`);
  return 1;
}

process.exit(main());
