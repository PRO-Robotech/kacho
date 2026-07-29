#!/usr/bin/env node
// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1
//
// scripts/selftest-assertions.js — самопроверка ЧЕСТНОСТИ шагов-мутаций суиты
// kacho-geo. Без сети, без стенда, без newman: исполняет test-script'ы из
// СГЕНЕРИРОВАННЫХ коллекций против подставленных ответов и смотрит, что именно
// они утверждают.
//
// Зачем. Мутация каталога geo завершается синхронно, и её ОТКАЗ приезжает не
// кодом HTTP, а полем `error` внутри 200-конверта (syncop.Fail: 23505/23503
// финализируются в Operation{done:true,error}). Форма конверта у провалившейся
// мутации неотличима от успешной — id на месте, metadata на месте, потому что id
// ресурса аллоцируется ДО записи. Значит шаг, проверяющий только форму, зачитывает
// ПРОВАЛ как принятую мутацию и остаётся зелёным ровно на том, что должен ловить.
//
// Проверка идёт в обе стороны, иначе она ловила бы форму, а не существо:
//   * успешная операция  → утверждение о результате обязано МОЛЧАТЬ;
//   * операция с ошибкой → то же утверждение обязано УПАСТЬ.
// Плюс перепись: сколько шагов осмотрено. «Ноль находок» обязано быть отличимо от
// «ноль прочитанного».
//
// Плюс структурный запрет: шаг-мутация на cluster-internal listener, ожидающий
// 200, ОБЯЗАН нести одно из двух утверждений о результате операции
// (assert_operation_envelope / assert_operation_failed). Иначе следующий такой шаг
// снова будет проверять форму конверта и ничего больше.
//
// Запуск: node scripts/selftest-assertions.js   (из tests/newman)
'use strict';

const fs = require('fs');
const path = require('path');

const NEWMAN_DIR = path.resolve(__dirname, '..');
const COLLECTIONS_DIR = path.join(NEWMAN_DIR, 'collections');

// Имена утверждений, которые эмитит gen.py. Совпадение по имени — только СПОСОБ
// НАЙТИ утверждение; вердикт выносится ИСПОЛНЕНИЕМ, а не наличием строки.
const ACCEPT_ASSERTION = 'mutation ACCEPTED — operation carries no result.error';
const REJECT_ASSERTION_RE = /^mutation REJECTED — result\.error code (\d+)/;

// --- минимальный pm-харнесс -------------------------------------------------

function runTestScript(scriptLines, responseBody, responseCode) {
  const executed = [];   // имена pm.test, которые реально исполнились
  const failed = [];     // {name, message}
  const env = new Map();

  const fail = (msg) => { throw new Error(msg); };
  const expect = (actual, hint) => {
    const say = (what) => fail((hint ? hint + ': ' : '') + what);
    const same = (want) => JSON.stringify(actual) === JSON.stringify(want);
    const node = {
      eql: (want) => { if (!same(want)) say(`expected ${JSON.stringify(actual)} to equal ${JSON.stringify(want)}`); },
      equal: (want) => { if (!same(want)) say(`expected ${JSON.stringify(actual)} to equal ${JSON.stringify(want)}`); },
      match: (re) => { if (!re.test(String(actual))) say(`expected ${actual} to match ${re}`); },
      include: (sub) => { if (!String(actual).includes(sub)) say(`expected ${actual} to include ${sub}`); },
      oneOf: (arr) => { if (!arr.includes(actual)) say(`expected ${actual} to be one of ${JSON.stringify(arr)}`); },
      an: (t) => { if (t === 'object' && (actual === null || typeof actual !== 'object')) say('expected an object'); },
      a: (t) => { if (typeof actual !== t) say(`expected a ${t}`); },
      property: (k) => { if (!(actual && Object.prototype.hasOwnProperty.call(actual, k))) say(`expected property ${k}`); },
      empty: undefined,
      above: (n) => { if (!(actual > n)) say(`expected ${actual} > ${n}`); },
      least: (n) => { if (!(actual >= n)) say(`expected ${actual} >= ${n}`); },
    };
    const negated = {
      eql: (want) => { if (same(want)) say(`expected not to equal ${JSON.stringify(want)}`); },
      equal: (want) => { if (same(want)) say(`expected not to equal ${JSON.stringify(want)}`); },
      match: (re) => { if (re.test(String(actual))) say(`expected ${actual} not to match ${re}`); },
      include: (sub) => { if (String(actual).includes(sub)) say(`expected ${actual} not to include ${sub}`); },
      have: { property: (k) => { if (actual && Object.prototype.hasOwnProperty.call(actual, k)) say(`expected NOT to have property ${k}`); } },
      be: { an: () => {}, a: () => {}, oneOf: (arr) => { if (arr.includes(actual)) say('expected not one of'); } },
    };
    const chain = Object.assign({}, node, {
      not: negated,
      be: Object.assign({}, node),
      have: { property: node.property },
      to: null,
    });
    chain.to = chain;
    chain.be.to = chain;
    return chain;
  };
  expect.fail = (msg) => fail(msg);

  const pm = {
    response: {
      code: responseCode,
      json: () => JSON.parse(JSON.stringify(responseBody)),
      text: () => JSON.stringify(responseBody),
    },
    environment: {
      get: (k) => env.get(k),
      set: (k, v) => env.set(k, String(v)),
      unset: (k) => env.delete(k),
      has: (k) => env.has(k),
    },
    variables: { get: () => undefined },
    request: { headers: { has: () => true, upsert: () => {}, remove: () => {} } },
    info: { requestName: 'selftest-step' },
    execution: { setNextRequest: () => {} },
    expect,
    test: (name, fn) => {
      executed.push(name);
      try { fn(); } catch (e) { failed.push({ name, message: e.message }); }
    },
  };

  let threw = null;
  try {
    // eslint-disable-next-line no-new-func
    new Function('pm', scriptLines.join('\n'))(pm);
  } catch (e) {
    threw = e.message;
  }
  return { executed, failed, threw };
}

// --- обход коллекций --------------------------------------------------------

function* walk(items, trail) {
  for (const it of items || []) {
    if (it.item) { yield* walk(it.item, trail.concat(it.name)); }
    if (it.request) { yield { item: it, trail: trail.concat(it.name) }; }
  }
}

function testScriptOf(item) {
  const ev = (item.event || []).find((e) => e.listen === 'test');
  return ev ? ev.script.exec : null;
}

const ACCEPTED_OP = {
  id: 'geo01hxyzabcdefghijk',
  metadata: { regionId: 'selftest-region', zoneId: 'selftest-region-a' },
  done: true,
  response: { id: 'selftest-region', name: 'Selftest' },
};
const erroredOp = (code) => ({
  id: 'geo01hxyzabcdefghijk',
  metadata: { regionId: 'selftest-region', zoneId: 'selftest-region-a' },
  done: true,
  error: { code, message: 'selftest: the mutation was rejected' },
});

const problems = [];
let examinedAccept = 0;
let examinedReject = 0;
let examinedSteps = 0;

const collections = fs.existsSync(COLLECTIONS_DIR)
  ? fs.readdirSync(COLLECTIONS_DIR).filter((f) => f.endsWith('.postman_collection.json'))
  : [];
if (collections.length === 0) {
  console.error(`SELFTEST FAIL: в ${COLLECTIONS_DIR} нет коллекций — проверять нечего ` +
    '(сгенерируйте их: python3 scripts/gen.py)');
  process.exit(1);
}

for (const file of collections.sort()) {
  const col = JSON.parse(fs.readFileSync(path.join(COLLECTIONS_DIR, file), 'utf8'));
  for (const { item, trail } of walk(col.item, [file.replace('.postman_collection.json', '')])) {
    const script = testScriptOf(item);
    if (!script) continue;
    examinedSteps += 1;
    const where = trail.join(' :: ');
    const src = script.join('\n');

    const isAccept = src.includes(ACCEPT_ASSERTION);
    const rejectName = script
      .map((l) => (l.match(/pm\.test\('(mutation REJECTED — result\.error code \d+[^']*)'/) || [])[1])
      .find(Boolean);

    // --- структурный запрет: мутация на internal listener, ждущая 200, обязана
    // утверждать что-то о РЕЗУЛЬТАТЕ операции, а не только о форме конверта.
    const method = (item.request.method || '').toUpperCase();
    const raw = (item.request.url && item.request.url.raw) || '';
    const mutatingInternal = raw.includes('{{internalBaseUrl}}') &&
      ['POST', 'PATCH', 'PUT', 'DELETE'].includes(method);
    const expects200 = src.includes("pm.test('status 200'");
    if (mutatingInternal && expects200 && !isAccept && !rejectName) {
      problems.push(`${where}: мутация ожидает 200, но НЕ проверяет result.error — ` +
        'операция, завершившаяся ошибкой, зачтётся как принятая. Нужен ' +
        'assert_operation_envelope() либо assert_operation_failed(code, …)');
      continue;
    }

    // --- поведенческая проверка в обе стороны.
    if (isAccept) {
      examinedAccept += 1;
      const ok = runTestScript(script, ACCEPTED_OP, 200);
      if (!ok.executed.includes(ACCEPT_ASSERTION)) {
        problems.push(`${where}: утверждение о результате операции не исполнилось на УСПЕШНОЙ ` +
          `операции (script threw: ${ok.threw || 'нет'}) — проверка не проверяет`);
      } else if (ok.failed.some((f) => f.name === ACCEPT_ASSERTION)) {
        problems.push(`${where}: утверждение о результате падает на УСПЕШНОЙ операции — ` +
          'ложное срабатывание: ' + JSON.stringify(ok.failed.find((f) => f.name === ACCEPT_ASSERTION)));
      }

      const bad = runTestScript(script, erroredOp(6), 200);
      if (!bad.failed.some((f) => f.name === ACCEPT_ASSERTION)) {
        problems.push(`${where}: операция, завершившаяся ОШИБКОЙ, принята шагом как удавшаяся ` +
          `мутация (исполнено утверждений: ${bad.executed.length}, упало: ${bad.failed.length}` +
          `${bad.threw ? ', script threw: ' + bad.threw : ''})`);
      }
    }

    if (rejectName) {
      examinedReject += 1;
      const wantCode = Number(rejectName.match(REJECT_ASSERTION_RE)[1]);
      const good = runTestScript(script, erroredOp(wantCode), 200);
      if (!good.executed.includes(rejectName)) {
        problems.push(`${where}: негативное утверждение не исполнилось (script threw: ${good.threw || 'нет'})`);
      } else if (good.failed.some((f) => f.name === rejectName)) {
        problems.push(`${where}: негативное утверждение падает на ОЖИДАЕМОЙ ошибке ` +
          `(code ${wantCode}): ` + JSON.stringify(good.failed.find((f) => f.name === rejectName)));
      }

      const passed = runTestScript(script, ACCEPTED_OP, 200);
      if (!passed.failed.some((f) => f.name === rejectName)) {
        problems.push(`${where}: мутация, которая ПРОШЛА, зачтена негативным шагом как отвергнутая`);
      }

      const otherCode = wantCode === 6 ? 9 : 6;
      const wrong = runTestScript(script, erroredOp(otherCode), 200);
      if (!wrong.failed.some((f) => f.name === rejectName)) {
        problems.push(`${where}: негативный шаг принимает ЛЮБУЮ ошибку (подставлен code ` +
          `${otherCode} вместо ${wantCode}) — он не различает причину отказа`);
      }
    }
  }
}

console.log(`selftest-assertions: коллекций ${collections.length}, шагов с test-script ${examinedSteps}, ` +
  `мутаций-успехов ${examinedAccept}, мутаций-негативов ${examinedReject}`);

// Сначала — координаты: они и есть то, что чинят. Перепись ниже ловит случай, когда
// координат нет, потому что гейт ничего не осмотрел.
if (problems.length > 0) {
  console.error(`SELFTEST FAIL: ${problems.length} шаг(ов) не проверяют результат мутации:`);
  for (const p of problems) console.error('  - ' + p);
  process.exit(1);
}
// Проверка собственной предпосылки: если ни одного шага-мутации не нашлось, гейт
// ничего не проверил — и обязан сказать это отказом, а не молчанием.
if (examinedAccept === 0) {
  console.error('SELFTEST FAIL: не найдено ни одного шага, утверждающего успех мутации — ' +
    'либо имена утверждений в gen.py разошлись с этим гейтом, либо суита их потеряла');
  process.exit(1);
}
console.log('SELFTEST: PASS — каждый шаг-мутация краснеет на операции с ошибкой и молчит на успешной');
