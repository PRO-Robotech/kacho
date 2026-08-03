#!/usr/bin/env node
// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1
//
// scripts/selftest-assertions.js — гейт: НИ ОДИН шаг суиты kacho-registry не имеет
// права исполнить запрос и не спросить ничего.
//
// Без сети, без стенда, без newman: исполняет prerequest- и test-скрипты из
// СГЕНЕРИРОВАННЫХ коллекций против подставленных ответов и СЧИТАЕТ, сколько
// утверждений при этом реально исполнилось. Вердикт выносится ИСПОЛНЕНИЕМ, а не
// наличием строки в тексте — grep по слову находит его и в комментарии, который эту
// же защиту объясняет.
//
// Prerequest считается наравне с test: newman учитывает pm.test из prerequest в
// assertions.total/failed и роняет код возврата (проверено инъекцией). Гейт,
// смотрящий только на test-скрипт, объявил бы шаг «немым», пока он на самом деле
// краснеет — и наоборот.
//
// Три предиката, и каждый называет своё число:
//
//   P (немой шаг)          — 0 утверждений при ЛЮБОМ из подставленных ответов и при
//                            любом состоянии окружения. Такой шаг упасть не может:
//                            он хуже отсутствующего — занимает слот и отчитывается
//                            зелёным.                                    → ОТКАЗ
//   Q (шаг, замолчавший    — 0 утверждений при ПУСТЫХ переменных субъекта и ≥1 при
//      от нехватки         — заполненных. Отсутствие фикстуры обязано быть отказом,
//      фикстуры)             а не тишиной.                                → ОТКАЗ
//   R (шаг, замолчавший    — ≥1 утверждение при одних ответах и 0 при других: для
//      от формы ответа)      этих ответов инвариант молча не проверяется. Печатается
//                            переписью с числом и координатами — это открытый долг,
//                            который обязан быть виден.                   → перепись
//
// Плюс перепись осмотренного: «ноль находок» обязано быть отличимо от «ноль
// прочитанного».
//
// ОБЛАСТЬ СЧЁТА — СОБСТВЕННЫЕ скрипты шага, без коллекционного prerequest. Это
// осознанно: коллекционный prerequest исполняется перед КАЖДЫМ запросом, поэтому
// одно утверждение в нём (например, страж обязательных фикстур) сделало бы «немым»
// невозможным ни один шаг, и предикаты ослепли бы. Гейт спрашивает про шаг: что
// утверждает ОН САМ.
//
// Запуск: node scripts/selftest-assertions.js   (из tests/newman)
'use strict';

const fs = require('fs');
const path = require('path');

// Скрипты шагов пишут в console.log свои пояснения. Гейт их глушит: его вывод — это
// вердикт в числах, и утонуть в чужих сообщениях он не должен.
const QUIET_CONSOLE = { log: () => {}, warn: () => {}, error: () => {}, info: () => {} };

const NEWMAN_DIR = path.resolve(__dirname, '..');
const COLLECTIONS_DIR = path.join(NEWMAN_DIR, 'collections');

// --- минимальный pm-харнесс -------------------------------------------------
// Поддерживает ровно те pm.*, которые встречаются в коллекциях registry (перепись
// API снята по дереву). Незнакомый вызов бросает — и шаг попадёт в отчёт как
// «скрипт упал», а не тихо зачтётся немым.

function makeExpect(fail) {
  const expect = (actual, hint) => {
    const say = (what) => fail((hint ? hint + ': ' : '') + what);
    const same = (want) => JSON.stringify(actual) === JSON.stringify(want);
    const node = {
      eql: (want) => { if (!same(want)) say(`expected ${JSON.stringify(actual)} to equal ${JSON.stringify(want)}`); },
      equal: (want) => { if (!same(want)) say(`expected ${JSON.stringify(actual)} to equal ${JSON.stringify(want)}`); },
      match: (re) => { if (!re.test(String(actual))) say(`expected ${actual} to match ${re}`); },
      include: (sub) => { if (!String(actual).includes(sub)) say(`expected ${actual} to include ${sub}`); },
      oneOf: (arr) => { if (!arr.includes(actual)) say(`expected ${actual} to be one of ${JSON.stringify(arr)}`); },
      an: (t) => { if (t === 'array' ? !Array.isArray(actual) : typeof actual !== t) say(`expected an ${t}`); },
      a: (t) => { if (typeof actual !== t) say(`expected a ${t}`); },
      property: (k) => { if (!(actual && Object.prototype.hasOwnProperty.call(actual, k))) say(`expected property ${k}`); },
      above: (n) => { if (!(actual > n)) say(`expected ${actual} > ${n}`); },
      least: (n) => { if (!(actual >= n)) say(`expected ${actual} >= ${n}`); },
      true: undefined,
    };
    const negated = {
      eql: (want) => { if (same(want)) say(`expected not to equal ${JSON.stringify(want)}`); },
      equal: (want) => { if (same(want)) say(`expected not to equal ${JSON.stringify(want)}`); },
      match: (re) => { if (re.test(String(actual))) say(`expected ${actual} not to match ${re}`); },
      include: (sub) => { if (String(actual).includes(sub)) say(`expected ${actual} not to include ${sub}`); },
      have: { property: (k) => { if (actual && Object.prototype.hasOwnProperty.call(actual, k)) say(`expected NOT to have property ${k}`); } },
      be: { an: () => {}, a: () => {}, oneOf: (arr) => { if (arr.includes(actual)) say('expected not one of'); }, true: undefined },
    };
    const chain = Object.assign({}, node, { not: negated, have: { property: node.property }, to: null });
    chain.be = Object.assign({}, node);
    Object.defineProperty(chain.be, 'true', { get: () => { if (actual !== true) say(`expected ${JSON.stringify(actual)} to be true`); } });
    Object.defineProperty(chain.be, 'false', { get: () => { if (actual !== false) say(`expected ${JSON.stringify(actual)} to be false`); } });
    chain.to = chain;
    chain.be.to = chain;
    return chain;
  };
  expect.fail = (msg) => fail(msg);
  return expect;
}

// runStep — исполняет prerequest + test одного шага. Возвращает перепись того, что
// реально исполнилось. `env` — Map начального окружения (мутируется скриптами).
function runStep(item, response, env, onRetry) {
  const executed = [];
  const failed = [];
  let threw = null;

  const fail = (msg) => { throw new Error(msg); };
  const pm = {
    response: {
      code: response.code,
      json: () => JSON.parse(JSON.stringify(response.body)),
      text: () => JSON.stringify(response.body),
    },
    environment: {
      get: (k) => env.get(k),
      set: (k, v) => env.set(k, String(v)),
      unset: (k) => env.delete(k),
      has: (k) => env.has(k),
    },
    variables: { get: (k) => env.get(k) },
    request: { headers: { has: () => true, upsert: () => {}, remove: () => {} } },
    info: { requestName: item.name },
    execution: { setNextRequest: () => { if (onRetry) onRetry(); } },
    expect: makeExpect(fail),
    test: (name, fn) => {
      executed.push(name);
      try { fn(); } catch (e) { failed.push({ name, message: e.message }); }
    },
  };

  // Петли между поллами написаны как busy-wait по настоящим часам
  // (`while (Date.now() - t < 500)`) — единственный способ реально разнести поллы под
  // newman (testing.md). Гейт исполняет тот же код тысячи раз, поэтому часы
  // подменяются: каждый вызов сдвигает время на минуту, busy-wait завершается сразу,
  // и ни одна ветка логики от этого не меняется.
  const FakeDate = function () { return new Date(0); };
  let clock = 0;
  FakeDate.now = () => { clock += 60000; return clock; };
  FakeDate.prototype = Date.prototype;

  for (const listen of ['prerequest', 'test']) {
    const ev = (item.event || []).find((e) => e.listen === listen);
    if (!ev) continue;
    try {
      // eslint-disable-next-line no-new-func
      new Function('pm', 'Date', 'console', ev.script.exec.join('\n'))(pm, FakeDate, QUIET_CONSOLE);
    } catch (e) {
      threw = `${listen}: ${e.message}`;
      break;
    }
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

// Подставляемые ответы: код + JSON-тело. Форма конверта операции — как у настоящего
// ответа Kachō (id аллоцируется ДО записи, поэтому у провалившейся операции он тоже
// есть). Набор покрывает все исходы, которые утверждают шаги registry.
const RESPONSES = [
  { label: '200-op', code: 200, body: { id: 'rop00000000selftest0', done: true, metadata: { registryId: 'reg00000000selftest0', repository: 'x' }, response: { id: 'reg00000000selftest0', name: 'x' } } },
  { label: '200-list', code: 200, body: { registries: [], repositories: [], tags: [], nextPageToken: '' } },
  { label: '400', code: 400, body: { code: 3, message: 'Illegal argument x' } },
  { label: '401', code: 401, body: { code: 16, message: 'subject: unauthenticated request' } },
  { label: '403', code: 403, body: { code: 7, message: 'permission denied' } },
  { label: '404', code: 404, body: { code: 5, message: 'Registry reg00000000selftest0 not found' } },
  { label: '409', code: 409, body: { code: 6, message: 'already exists' } },
];

// Переменные окружения, которые НЕСУТ СУБЪЕКТА. Пустота именно этих переменных —
// состояние всех закоммиченных окружений суиты, и именно её проверяет предикат Q.
const SUBJECT_VARS = [
  'jwtProjectEditorA', 'jwtProjectEditorB', 'jwtProjectViewerA', 'jwtProjectOwnerA',
  'jwtStranger', 'jwtServiceAccountEditor', 'jwtGroupMemberEditor',
  'jwtCustomRoleTargetManager', 'jwtBootstrap',
  'jwtNoBindings', 'jwtPureNoBindings',
];

// Непустое окружение: субъекты заполнены, плюс идентификаторы, которые шаги читают.
function populatedEnv() {
  const e = new Map();
  for (const v of SUBJECT_VARS) e.set(v, 'selftest-bearer-token');
  e.set('runId', 'selftest');
  e.set('regIdAz', 'reg00000000selftest0');
  e.set('existingProjectId', 'prj00000000selftest0');
  e.set('existingRegionId', 'ru-central1');
  e.set('regMissMsg', 'Registry reg00000000selftest0 not found');
  e.set('lastOpError', '');
  e.set('opId', 'rop00000000selftest0');
  return e;
}
function emptySubjectEnv() {
  const e = populatedEnv();
  for (const v of SUBJECT_VARS) e.set(v, '');
  return e;
}

// settle — исполняет шаг ТАК, КАК ЕГО ИСПОЛНЯЕТ NEWMAN: ограниченный повтор через
// setNextRequest(своё имя) на одном и том же окружении, пока петля сама не выйдет.
// Без этого гейт объявил бы «немым» любой шаг с bounded-retry (`retry_until_authorized`,
// поллер операции): на первом проходе такой шаг действительно молчит — он ЖДЁТ. Его
// утверждение исполняется по исчерпании бюджета, и именно это и надо проверять.
const SETTLE_CAP = 400;

function settle(item, response, env) {
  let executed = 0;
  let threw = null;
  for (let i = 0; i < SETTLE_CAP; i += 1) {
    let retried = false;
    const res = runStep(item, response, env, () => { retried = true; });
    if (res.threw) { threw = res.threw; break; }
    executed += res.executed.length;
    if (executed > 0 || !retried) break;   // спросил либо перестал ждать
  }
  return { executed, threw };
}

// survey — по каждому подставленному ответу: сколько утверждений шаг спросил, доведя
// собственную петлю ожидания до конца. Плюс список ответов, при которых он не спросил
// ничего.
function survey(item, mkEnv) {
  let max = 0;
  const mute = [];
  const threws = [];
  for (const r of RESPONSES) {
    const res = settle(item, r, mkEnv());
    if (res.threw) { threws.push(`${r.label}: ${res.threw}`); continue; }
    if (res.executed === 0) mute.push(r.label);
    if (res.executed > max) max = res.executed;
  }
  return { max, mute, threws };
}

// --- проверка предпосылки гейта: инъекция В ОБЕ СТОРОНЫ ---------------------
// Гейт обязан краснеть на возвращённом дефекте и МОЛЧАТЬ на законной конструкции той
// же формы. Без второй половины он ловил бы форму, а не существо, и первый же ложный
// срабат его отключил бы. Оба образца — синтетические, реального дерева не касаются.
function prove() {
  const mk = (name, exec) => ({ name, request: { method: 'GET' }, event: [{ listen: 'test', script: { exec } }] });

  // (а) ВОЗВРАЩЁННЫЙ ДЕФЕКТ: шаг молчит при пустой переменной субъекта.
  const defect = mk('injected-mute', [
    "const _v = pm.environment.get('jwtProjectViewerA') || '';",
    "if (!_v) { console.log('SKIP: no fixture'); return; }",
    "pm.test('status 200', () => pm.expect(pm.response.code).to.eql(200));",
  ]);
  // (б) ЗАКОННАЯ конструкция той же формы: тот же гейт, но отсутствие фикстуры —
  //     отказ, а не тишина.
  const legit = mk('injected-refusal', [
    "const _v = pm.environment.get('jwtProjectViewerA') || '';",
    "if (!_v) { pm.test('fixture missing', () => pm.expect.fail('jwtProjectViewerA is empty')); return; }",
    "pm.test('status 200', () => pm.expect(pm.response.code).to.eql(200));",
  ]);
  // (в) ЗАКОННАЯ конструкция с ограниченным повтором: молчит на первом проходе,
  //     спрашивает по исчерпании бюджета. Гейт обязан её пропустить.
  const retry = mk('injected-bounded-retry', [
    "const _n = parseInt(pm.environment.get('_n') || '0', 10);",
    "if (pm.response.code === 404 && _n < 5) { pm.environment.set('_n', String(_n + 1)); pm.execution.setNextRequest(pm.info.requestName); return; }",
    "pm.test('settled', () => pm.expect(pm.response.code).to.eql(200));",
  ]);

  const q = (it) => {
    const full = survey(it, populatedEnv);
    const bare = survey(it, emptySubjectEnv);
    return { caught: bare.max === 0 && full.max > 0, full: full.max, bare: bare.max, mute: full.mute };
  };

  const problems = [];
  const d = q(defect);
  if (!d.caught) {
    problems.push(`инъекция дефекта НЕ поймана предикатом Q (при заполненных ${d.full}, при пустых ${d.bare}) — ` +
      'гейт не различает тишину от отказа');
  }
  const l = q(legit);
  if (l.caught) problems.push('законная конструкция (отказ вместо тишины) ОШИБОЧНО поймана предикатом Q');
  const r = q(retry);
  if (r.caught || r.mute.length > 0) {
    problems.push(`законный ограниченный повтор ОШИБОЧНО помечен (Q=${r.caught}, R=[${r.mute.join(',')}]) — ` +
      'гейт не доводит петлю ожидания до конца');
  }
  return problems;
}

const proveProblems = prove();
if (proveProblems.length > 0) {
  console.error('SELFTEST FAIL: гейт не прошёл собственную проверку предпосылки:');
  for (const p of proveProblems) console.error('  - ' + p);
  process.exit(1);
}
console.log('проверка предпосылки: инъекция дефекта поймана, две законные конструкции той же формы пропущены');

const collections = fs.existsSync(COLLECTIONS_DIR)
  ? fs.readdirSync(COLLECTIONS_DIR).filter((f) => f.endsWith('.postman_collection.json')).sort()
  : [];
if (collections.length === 0) {
  console.error(`SELFTEST FAIL: в ${COLLECTIONS_DIR} нет коллекций — проверять нечего ` +
    '(сгенерируйте их: python3 scripts/gen.py)');
  process.exit(1);
}

const mute = [];          // P
const fixtureMuted = [];  // Q
const responseMuted = []; // R
const broken = [];        // скрипт упал на всех ответах
let steps = 0;

for (const file of collections) {
  const col = JSON.parse(fs.readFileSync(path.join(COLLECTIONS_DIR, file), 'utf8'));
  for (const { item, trail } of walk(col.item, [file.replace('.postman_collection.json', '')])) {
    if (!(item.event || []).some((e) => e.listen === 'test' || e.listen === 'prerequest')) continue;
    steps += 1;
    const where = trail.join(' :: ');

    const full = survey(item, populatedEnv);
    const bare = survey(item, emptySubjectEnv);

    if (full.threws.length === RESPONSES.length) {
      broken.push(`${where}: скрипт падает при каждом подставленном ответе — ${full.threws[0]}`);
      continue;
    }

    // P — немой при любом ответе и любом окружении.
    if (full.max === 0 && bare.max === 0) {
      mute.push(`${where}: 0 утверждений при любом из ${RESPONSES.length} ответов — шаг не может упасть`);
      continue;
    }

    // Q — замолчал именно от нехватки субъекта.
    if (bare.max === 0 && full.max > 0) {
      fixtureMuted.push(`${where}: при ПУСТЫХ переменных субъекта не исполняется ни одно ` +
        `утверждение (при заполненных — ${full.max}); отсутствие фикстуры обязано быть отказом, не тишиной`);
      continue;
    }

    // R — замолчал от формы ответа.
    if (full.mute.length > 0) {
      responseMuted.push(`${where}: 0 утверждений при ответах [${full.mute.join(', ')}]`);
    }
  }
}

console.log(`selftest-assertions: коллекций ${collections.length}, шагов со скриптами ${steps}, ` +
  `подставлено ответов на шаг ${RESPONSES.length} × 2 состояния окружения`);
console.log(`  предикат P (немой шаг):                     ${mute.length}`);
console.log(`  предикат Q (замолчал от нехватки фикстуры):  ${fixtureMuted.length}`);
console.log(`  предикат R (замолчал от формы ответа):       ${responseMuted.length}`);
console.log(`  скрипт падает на всех ответах:               ${broken.length}`);

if (responseMuted.length > 0) {
  console.log('перепись R — инвариант молча не проверяется при названных ответах (открытый долг):');
  for (const p of responseMuted) console.log('  · ' + p);
}

const fatal = mute.concat(fixtureMuted, broken);
if (fatal.length > 0) {
  console.error(`SELFTEST FAIL: ${fatal.length} шаг(ов) исполняют запрос и не спрашивают ничего:`);
  for (const p of fatal) console.error('  - ' + p);
  process.exit(1);
}
// Проверка собственной предпосылки: гейт, ничего не осмотревший, обязан сказать это
// отказом, а не молчанием.
if (steps === 0) {
  console.error('SELFTEST FAIL: ни одного шага со скриптом не найдено — гейт ничего не осмотрел');
  process.exit(1);
}
console.log(`SELFTEST: PASS — все ${steps} шаг(ов) эмитят хотя бы одно утверждение ` +
  'и при пустых, и при заполненных переменных субъекта');
