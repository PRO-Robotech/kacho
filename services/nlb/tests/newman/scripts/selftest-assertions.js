#!/usr/bin/env node
// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1
//
// scripts/selftest-assertions.js — гейт ЧЕСТНОСТИ утверждений суиты kacho-nlb.
//
// Без сети, без стенда, без newman: исполняет prerequest/test-скрипты СГЕНЕРИРОВАННЫХ
// коллекций против подставленных ответов и смотрит, что именно они утверждают.
// Вердикт — из ИСПОЛНЕНИЯ, не из наличия слова в тексте: grep находит слово и в
// комментарии, объясняющем эту же защиту.
//
// Три предиката:
//
//   A (взаимоисключающие исходы) — кейс, у которого шаг молча принимает И успех, И
//       отказ, и НИ ОДНО утверждение кейса не падает в «мире успеха». Такой кейс не
//       может упасть на том, ради чего назван: отказ, который он проверяет, и успех,
//       который он должен ловить, для него неразличимы.               → ОТКАЗ
//
//   B (подмена субъекта умолчанием) — коллекционный prerequest, который при
//       НЕЗАСЕЯННОМ субъекте суиты ставит заголовок с ДРУГИМ, более полномочным
//       субъектом. Тогда вся суита проверяет каскад прав администратора, а не то, что
//       заявлено, и отчитывается зелёным. Отсутствие фикстуры обязано быть отказом, а
//       не тихой подменой.                                            → ОТКАЗ
//
//   C (опрос ведёт не тот субъект) — шаг выполнен под явным субъектом, а следующий
//       за ним опрос операции оставлен на умолчании коллекции. Чтение операции
//       owner-scoped, поэтому чужой опрос получает 404, неотличимый от «нет такой»:
//       исход мутации остаётся НЕизвестен, а кейс выглядит продуктовым дефектом.
//                                                                     → ОТКАЗ
//
// Плюс перепись осмотренного: «ноль находок» обязано быть отличимо от «ноль
// прочитанного». И проверка собственной предпосылки: инъекция дефекта ловится,
// законная конструкция той же формы пропускается.
//
// Запуск: node scripts/selftest-assertions.js   (из tests/newman)
'use strict';

const fs = require('fs');
const path = require('path');

const QUIET_CONSOLE = { log: () => {}, warn: () => {}, error: () => {}, info: () => {} };
const NEWMAN_DIR = path.resolve(__dirname, '..');
const COLLECTIONS_DIR = path.join(NEWMAN_DIR, 'collections');

// --- минимальный pm-харнесс -------------------------------------------------

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
      an: (t) => { if (t === 'array' ? !Array.isArray(actual) : (actual === null || typeof actual !== t)) say(`expected an ${t}`); },
      a: (t) => { if (typeof actual !== t) say(`expected a ${t}`); },
      property: (k) => { if (!(actual && Object.prototype.hasOwnProperty.call(actual, k))) say(`expected property ${k}`); },
      above: (n) => { if (!(actual > n)) say(`expected ${actual} > ${n}`); },
      least: (n) => { if (!(actual >= n)) say(`expected ${actual} >= ${n}`); },
      lengthOf: (n) => { if (!actual || actual.length !== n) say(`expected length ${n}`); },
    };
    const negated = {
      eql: (want) => { if (same(want)) say(`expected not to equal ${JSON.stringify(want)}`); },
      equal: (want) => { if (same(want)) say(`expected not to equal ${JSON.stringify(want)}`); },
      match: (re) => { if (re.test(String(actual))) say(`expected ${actual} not to match ${re}`); },
      include: (sub) => { if (String(actual).includes(sub)) say(`expected ${actual} not to include ${sub}`); },
      have: { property: (k) => { if (actual && Object.prototype.hasOwnProperty.call(actual, k)) say(`expected NOT to have property ${k}`); } },
      be: { an: () => {}, a: () => {}, oneOf: (arr) => { if (arr.includes(actual)) say('expected not one of'); } },
    };
    const chain = Object.assign({}, node, { not: negated, have: { property: node.property }, to: null });
    chain.be = Object.assign({}, node);
    Object.defineProperty(chain.be, 'true', { get: () => { if (actual !== true) say(`expected ${JSON.stringify(actual)} to be true`); } });
    Object.defineProperty(chain.be, 'false', { get: () => { if (actual !== false) say(`expected ${JSON.stringify(actual)} to be false`); } });
    Object.defineProperty(chain.be, 'undefined', { get: () => { if (actual !== undefined) say(`expected ${JSON.stringify(actual)} to be undefined`); } });
    Object.defineProperty(chain, 'undefined', { get: () => { if (actual !== undefined) say(`expected ${JSON.stringify(actual)} to be undefined`); } });
    chain.to = chain;
    chain.be.to = chain;
    return chain;
  };
  expect.fail = (msg) => fail(msg);
  return expect;
}

// runScript — исполняет один скрипт. Возвращает перепись исполненного/упавшего,
// плюс заголовки, которые скрипт поставил (нужно предикату B).
function runScript(exec, ctx) {
  const executed = [];
  const failed = [];
  const headers = [];
  let retried = false;
  let threw = null;

  const fail = (msg) => { throw new Error(msg); };
  const pm = {
    response: {
      code: ctx.response.code,
      json: () => JSON.parse(JSON.stringify(ctx.response.body)),
      text: () => JSON.stringify(ctx.response.body),
    },
    environment: {
      get: (k) => ctx.env.get(k), set: (k, v) => ctx.env.set(k, String(v)),
      unset: (k) => ctx.env.delete(k), has: (k) => ctx.env.has(k),
    },
    variables: { get: (k) => ctx.env.get(k) },
    request: {
      headers: {
        has: (k) => headers.some((h) => h.key === k),
        upsert: (h) => headers.push(h),
        remove: (k) => { for (let i = headers.length - 1; i >= 0; i -= 1) if (headers[i].key === k) headers.splice(i, 1); },
      },
    },
    info: { requestName: ctx.name },
    execution: { setNextRequest: () => { retried = true; } },
    expect: makeExpect(fail),
    test: (name, fn) => { executed.push(name); try { fn(); } catch (e) { failed.push({ name, message: e.message }); } },
  };

  // Часы подменяются: busy-wait между поллами написан по настоящему времени, а гейт
  // исполняет тот же код тысячи раз. Ни одна ветка логики от этого не меняется.
  const FakeDate = function () { return new Date(0); };
  let clock = 0;
  FakeDate.now = () => { clock += 60000; return clock; };
  FakeDate.prototype = Date.prototype;

  try {
    // eslint-disable-next-line no-new-func
    new Function('pm', 'Date', 'console', exec.join('\n'))(pm, FakeDate, QUIET_CONSOLE);
  } catch (e) {
    threw = e.message;
  }
  return { executed, failed, headers, retried, threw };
}

const scriptOf = (holder, listen) => {
  const ev = (holder.event || []).find((e) => e.listen === listen);
  return ev ? ev.script.exec : null;
};

// runStep — prerequest + test одного шага, с доведением петли ожидания до конца
// (иначе шаг с bounded-retry выглядел бы немым: он просто ЖДЁТ).
function runStep(item, response, env) {
  const acc = { executed: [], failed: [], threw: null };
  for (let i = 0; i < 400; i += 1) {
    let retried = false;
    for (const listen of ['prerequest', 'test']) {
      const exec = scriptOf(item, listen);
      if (!exec) continue;
      const r = runScript(exec, { response, env, name: item.name });
      acc.executed.push(...r.executed);
      acc.failed.push(...r.failed);
      if (r.retried) retried = true;
      if (r.threw) { acc.threw = `${listen}: ${r.threw}`; break; }
    }
    if (acc.threw || acc.executed.length > 0 || !retried) break;
  }
  return acc;
}

// --- подставляемые ответы ----------------------------------------------------

const OP_OK = { id: 'nlb00000000selftest0', done: true, metadata: { loadBalancerId: 'nlb00000000selftest0', listenerId: 'lst00000000selftest0', targetGroupId: 'tgr00000000selftest0' }, response: { id: 'nlb00000000selftest0', name: 'selftest' } };
const ROW = { id: 'nlb00000000selftest0', name: 'selftest', projectId: 'prj00000000selftest0', regionId: 'ru-central1' };
// ДВЕ формы «мира успеха». Одной недостаточно: конверт операции не несёт списочных
// полей, поэтому утверждение о ПУСТОТЕ списка проходит на нём так же, как на законном
// пустом ответе, — и списочный негатив выглядел бы неразличающим, хотя он различает.
const SUCCESSES = [
  { label: '200-op-succeeded', code: 200, body: OP_OK },
  { label: '200-list-with-rows', code: 200, body: {
      networkLoadBalancers: [ROW], loadBalancers: [ROW], listeners: [ROW], targetGroups: [ROW], targets: [ROW],
      operations: [{ id: 'nlb00000000selftest0', done: true }], nextPageToken: '',
      id: 'nlb00000000selftest0', done: true, metadata: { loadBalancerId: 'nlb00000000selftest0' },
      response: ROW } },
];
const SUCCESS = SUCCESSES[0];

// successShapesFor — формы успеха, СОБРАННЫЕ ПОД ШАГ. Списочный негатив («чужак не
// видит ресурс редактора») проверяет НЕВКЛЮЧЕНИЕ конкретного идентификатора из
// окружения. Если подставить список с посторонними строками, утверждение честно
// пройдёт, и шаг выглядел бы неразличающим, хотя он различает. Поэтому в списки
// добавляются строки с ровно теми идентификаторами, которые читает сам шаг: тогда
// «утечка» в мире успеха действительно наступает.
function successShapesFor(item) {
  const keys = new Set();
  for (const listen of ['prerequest', 'test']) {
    const exec = scriptOf(item, listen) || [];
    for (const line of exec) {
      for (const m of line.matchAll(/environment\.get\('([A-Za-z_][A-Za-z0-9_]*)'\)/g)) keys.add(m[1]);
    }
  }
  const rows = [ROW, ...[...keys].map((k) => ({ ...ROW, id: `foreign-${k}` }))];
  const listBody = {
    networkLoadBalancers: rows, loadBalancers: rows, listeners: rows, targetGroups: rows, targets: rows,
    operations: rows.map((r) => ({ id: r.id, done: true })), items: rows, nextPageToken: '',
    id: 'nlb00000000selftest0', done: true, metadata: { loadBalancerId: 'nlb00000000selftest0' },
    response: ROW,
  };
  return [SUCCESSES[0], { label: '200-list-with-rows', code: 200, body: listBody }];
}
const REFUSALS = [
  { label: '400', code: 400, body: { code: 3, message: 'Illegal argument x' } },
  { label: '403', code: 403, body: { code: 7, message: 'permission denied' } },
  { label: '404', code: 404, body: { code: 5, message: 'LoadBalancer nlb00000000selftest0 not found' } },
  { label: '409', code: 409, body: { code: 6, message: 'load balancer with this name already exists' } },
];

// baseEnv — окружение, в котором ЧУЖОЕ значение выглядит чужим. Неизвестный ключ
// отдаёт не undefined, а различимую строку: иначе `expect(j.someId).to.eql(
// pm.environment.get('someId'))` сравнивало бы undefined с undefined и ПРОХОДИЛО на
// любом ответе — правдоподобная (здесь: пустая) фикстура спрятала бы тот самый
// дефект, который проба ищет.
function foreignAwareMap(seed) {
  const m = new Map(seed);
  const get = m.get.bind(m);
  m.get = (k) => (m.has(k) ? get(k) : `foreign-${k}`);
  return m;
}

function baseEnv() {
  const e = new Map();
  for (const k of ['jwtProjectEditorA', 'jwtProjectEditorB', 'jwtProjectViewerA', 'jwtProjectOwnerA',
    'jwtStranger', 'jwtServiceAccountEditor', 'jwtGroupMemberEditor', 'jwtCustomRoleTargetManager', 'jwtBootstrap', 'jwtNoBindings', 'jwtPureNoBindings']) e.set(k, 'tok-' + k);
  for (const [k, v] of Object.entries({
    runId: 'selftest', baseUrl: 'http://127.0.0.1:1', existingProjectId: 'prj00000000selftest0',
    existingProjectCrossId: 'prj00000001selftest0', existingRegionId: 'ru-central1',
    existingRegionAltId: 'ru-central2', _suiteProjectId: 'prj00000000selftest0',
    _suiteProjectCrossId: 'prj00000001selftest0', _suiteRegionId: 'ru-central1',
    _suiteRegionAltId: 'ru-central2', createdId: 'nlb00000000selftest0',
    opId: 'nlb00000000selftest0', lastOpError: '',
  })) e.set(k, v);
  return foreignAwareMap(e);
}

const declaredTeardown = [];   // заявленная терпимость уборки: считается и печатается

// --- предикат A: кейс, принимающий взаимоисключающие исходы ------------------
// Кейс прогоняется дважды. В «мире успеха» ВСЕ его шаги получают 200 с УДАВШЕЙСЯ
// операцией. Если при этом ни одно утверждение кейса не падает, а какой-то его шаг
// при этом же скрипте принимает и отказ (4xx) — кейс не различает исходы.
function checkCaseA(caseItem, steps) {
  // Кейс РАЗЛИЧАЕТ исходы, если в мире успеха у него хоть где-то падает утверждение
  // ИЛИ бросает скрипт (брошенный скрипт newman учитывает как ошибку шага — это тоже
  // красный, а не тишина). Достаточно одной формы успеха: кейс делает свою работу для
  // той формы, которая к нему относится.
  let executedAnywhere = 0;
  let distinguishes = false;
  const passedSteps = new Map();   // имя шага -> прошёл при ВСЕХ формах успеха
  for (const shapeIdx of [0, 1]) {
    const env = baseEnv();
    let failed = 0;
    let threw = 0;
    for (const st of steps) {
      const r = runStep(st, successShapesFor(st)[shapeIdx], env);
      executedAnywhere += r.executed.length;
      failed += r.failed.length;
      if (r.threw) threw += 1;
      const clean = r.executed.length > 0 && r.failed.length === 0 && !r.threw;
      passedSteps.set(st.name, (passedSteps.get(st.name) !== false) && clean);
    }
    if (failed > 0 || threw > 0) distinguishes = true;
  }
  if (executedAnywhere === 0) return null;   // немой кейс — предмет другого гейта
  if (distinguishes) return null;

  // Ни при одной форме успеха кейс не возразил. Называем шаг, который при том же
  // скрипте принимает и отказ: он и делает исходы неразличимыми.
  for (const st of steps) {
    if (passedSteps.get(st.name) !== true) continue;
    for (const ref of REFUSALS) {
      const r = runStep(st, ref, baseEnv());
      if (r.executed.length === 0 || r.failed.length > 0 || r.threw) continue;
      // ЗАЯВЛЕННАЯ терпимость разбора фикстуры — не находка, но и не невидимка.
      // Уборка после кейса вправе не ронять кейс, и это сказано в САМОМ ИМЕНИ
      // утверждения («best-effort … never fails the case»), то есть видно и в отчёте
      // прогона. Исключение считается и печатается отдельной строкой: список, которому
      // больше нечего исключать, обязан быть заметен, иначе он унаследует слепую зону.
      const declared = r.executed.some((n) => /best-effort/i.test(n));
      if (declared) { declaredTeardown.push(`${st.name}: ${r.executed.find((n) => /best-effort/i.test(n))}`); continue; }
      return `шаг «${st.name}» принимает И успех (200, операция удалась), И отказ (${ref.label}), ` +
        'и ни при одной форме успеха кейс не возражает — упасть на том, ради чего он назван, кейс не может';
    }
  }
  return null;
}

// --- предикат B: подмена субъекта умолчанием ---------------------------------
// Коллекционный prerequest прогоняется при НЕЗАСЕЯННОМ субъекте суиты, но
// засеянном администраторе. Законное поведение: заголовок субъектом-администратором
// НЕ ставится, и эмитится утверждение с именем недостающей переменной.
const SUITE_SUBJECT = 'jwtProjectEditorA';
const ADMIN_SUBJECT = 'jwtBootstrap';
const ADMIN_TOKEN = 'tok-cluster-admin-selftest';

function checkCollectionB(col, label) {
  const exec = scriptOf(col, 'prerequest');
  if (!exec) return null;
  const env = baseEnv();
  env.set(SUITE_SUBJECT, '');
  env.set(ADMIN_SUBJECT, ADMIN_TOKEN);
  const r = runScript(exec, { response: SUCCESS, env, name: `${label} :: collection-prerequest` });
  const substituted = r.headers.some((h) => h.key === 'Authorization' && String(h.value).includes(ADMIN_TOKEN));
  if (substituted) {
    return `коллекционный prerequest при пустом ${SUITE_SUBJECT} ставит Authorization токеном ${ADMIN_SUBJECT} ` +
      '(другой, более полномочный субъект) — суита проверяет каскад прав администратора, а не заявленное; ' +
      'отсутствие фикстуры обязано быть отказом, а не подменой';
  }
  if (r.executed.length === 0) {
    return `коллекционный prerequest при пустом ${SUITE_SUBJECT} не эмитит ни одного утверждения — ` +
      'незасеянный субъект проходит молча';
  }
  return null;
}

// --- проверка предпосылки гейта (инъекция в обе стороны) ---------------------

function prove() {
  const problems = [];
  const mkStep = (name, exec) => ({ name, request: { method: 'POST' }, event: [{ listen: 'test', script: { exec } }] });

  // A(а) дефект: шаг принимает и 200, и 409, и больше кейс ничего не утверждает.
  const defectA = [mkStep('dup-tolerant', [
    "pm.test('rejected (sync 409 or async error)', () => pm.expect(pm.response.code).to.be.oneOf([200, 409]));",
  ])];
  if (!checkCaseA({}, defectA)) problems.push('предикат A не поймал возвращённый дефект (шаг принимает 200 и 409)');

  // A(б) законная форма: 200 допускается, но кейс требует ошибки в операции.
  const legitA = [
    mkStep('dup', [
      "pm.test('rejected (sync 409 or async error)', () => pm.expect(pm.response.code).to.be.oneOf([200, 409]));",
      "if (pm.response.code === 200) { pm.environment.set('opId', pm.response.json().id); }",
    ]),
    mkStep('poll', [
      "const j = pm.response.json();",
      "pm.test('operation refused the request (carries an error)', () => pm.expect(j.error, JSON.stringify(j)).to.be.an('object'));",
    ]),
  ];
  if (checkCaseA({}, legitA)) problems.push('предикат A ошибочно поймал законную форму (200 допущен, но операция обязана нести ошибку)');

  // A(в) законная толерантность негатива: только отказные коды, успех не принимается.
  const legitTolerant = [mkStep('neg', [
    "pm.test('denied (400/403/404), never 200', () => pm.expect(pm.response.code).to.be.oneOf([400, 403, 404]));",
  ])];
  if (checkCaseA({}, legitTolerant)) problems.push('предикат A ошибочно поймал законный толерантный негатив (только отказные коды)');

  // B(а) дефект: умолчание подставляет администратора.
  const defectB = { event: [{ listen: 'prerequest', script: { exec: [
    "const __d = pm.environment.get('jwtProjectEditorA') || pm.environment.get('jwtBootstrap') || '';",
    "if (__d && !pm.request.headers.has('Authorization')) { pm.request.headers.upsert({key: 'Authorization', value: 'Bearer ' + __d}); }",
  ] } }] };
  if (!checkCollectionB(defectB, 'injected')) problems.push('предикат B не поймал возвращённый дефект (подмена администратором)');

  // B(б) законная форма: отказ с именем переменной, заголовок не ставится.
  const legitB = { event: [{ listen: 'prerequest', script: { exec: [
    "const __d = pm.environment.get('jwtProjectEditorA') || '';",
    "if (!__d) { pm.test('FIXTURE REQUIRED: jwtProjectEditorA', () => pm.expect.fail('not seeded')); }",
    "if (__d && !pm.request.headers.has('Authorization')) { pm.request.headers.upsert({key: 'Authorization', value: 'Bearer ' + __d}); }",
  ] } }] };
  if (checkCollectionB(legitB, 'injected')) problems.push('предикат B ошибочно поймал законную форму (отказ вместо подмены)');

  // --- предикат C: инъекция в обе стороны ---
  const stepWith = (name, subj) => ({
    name,
    request: { method: 'GET', url: { raw: 'x' } },
    event: subj ? [{ listen: 'prerequest', script: { exec: [
      `// AZD per-step: bearer from env '${subj}'`,
      `const __t = pm.environment.get('${subj}') || '';`,
    ] } }] : [],
  });
  // ДЕФЕКТ: мутация под явным субъектом, опрос — на умолчании.
  if (checkCaseC([stepWith('cr-as-sa', 'jwtServiceAccountEditor'), stepWith('poll-op-1', null)]).length !== 1) {
    problems.push('предикат C не поймал опрос, оставленный на умолчании после явного субъекта');
  }
  // ЗАКОННО: тот же субъект у обоих — молчит.
  if (checkCaseC([stepWith('cr-as-sa', 'jwtServiceAccountEditor'),
    stepWith('poll-op-1', 'jwtServiceAccountEditor')]).length !== 0) {
    problems.push('предикат C покраснел на согласованном субъекте');
  }
  // ЗАКОННО: шаг без явного субъекта — заявки нет, сверять нечего.
  if (checkCaseC([stepWith('cr', null), stepWith('poll-op-1', null)]).length !== 0) {
    problems.push('предикат C потребовал субъекта там, где его никто не заявлял');
  }
  // ЗАКОННО: за шагом идёт не опрос — правило о нём ничего не говорит.
  if (checkCaseC([stepWith('cr-as-sa', 'jwtServiceAccountEditor'), stepWith('get-after', null)]).length !== 0) {
    problems.push('предикат C сработал на шаге, который операцию не опрашивает');
  }

  return problems;
}

// --- предикат C: опрос операции ведётся НЕ ТЕМ субъектом ---------------------
//
// Чтение операции owner-scoped по построению: предикат владельца — пара
// (тип принципала, идентификатор) создателя прямо в SQL, а посторонний получает
// тот же 404 без утечки, что и на несуществующий идентификатор. Поэтому шаг,
// выполненный под явным субъектом, и следующий за ним опрос, оставленный на
// умолчании коллекции, спрашивают об операции РАЗНЫХ субъектов — и честный отказ
// читается как «операция пропала».
//
// Измерено 2026-07-30: служебная учётка создала балансировщик (200, операция
// заведена), а два опроса сразу за этим вернули 404. Кейс выглядел продуктовым
// дефектом и был потерянной личностью. Форма встречалась 27 раз в двух
// коллекциях; в пяти из них субъекты действительно различались.
const PER_STEP_AUTH = /per-step: (?:anonymous|bearer from env '([^']+)')/;

function stepSubject(step) {
  for (const ev of step.event || []) {
    if (ev.listen !== 'prerequest') continue;
    const src = ((ev.script || {}).exec || []).join('\n');
    const m = PER_STEP_AUTH.exec(src);
    if (m) return m[1] || 'anonymous';
  }
  return null;   // наследует умолчание коллекции
}

// Возвращает список расхождений внутри одного кейса.
function checkCaseC(steps) {
  const out = [];
  for (let i = 0; i < steps.length - 1; i += 1) {
    const subj = stepSubject(steps[i]);
    if (!subj) continue;                                  // умолчание — не заявка
    const next = steps[i + 1];
    if (!/^poll-op/.test(next.name || '')) continue;      // опрос идёт СРАЗУ за шагом
    const pollSubj = stepSubject(next);
    if (pollSubj !== subj) {
      out.push(`шаг ${steps[i].name} выполнен субъектом ${subj}, а опрос ${next.name} — `
        + `${pollSubj || 'умолчанием коллекции'}: операция читается по владельцу, `
        + 'и чужой опрос получит 404, неотличимый от «нет такой»');
    }
  }
  return out;
}

const proveProblems = prove();
if (proveProblems.length > 0) {
  console.error('SELFTEST FAIL: гейт не прошёл собственную проверку предпосылки:');
  for (const p of proveProblems) console.error('  - ' + p);
  process.exit(1);
}
console.log('проверка предпосылки: по каждому предикату дефект пойман, законные конструкции той же формы пропущены');

// --- обход коллекций ---------------------------------------------------------

function* cases(items, trail) {
  for (const it of items || []) {
    const steps = (it.item || []).filter((x) => x.request);
    if (steps.length > 0) yield { caseItem: it, steps, trail: trail.concat(it.name) };
    const folders = (it.item || []).filter((x) => x.item);
    if (folders.length > 0) yield* cases(folders, trail.concat(it.name));
  }
}

const collections = fs.existsSync(COLLECTIONS_DIR)
  ? fs.readdirSync(COLLECTIONS_DIR).filter((f) => f.endsWith('.postman_collection.json')).sort()
  : [];
if (collections.length === 0) {
  console.error(`SELFTEST FAIL: в ${COLLECTIONS_DIR} нет коллекций (сгенерируйте: python3 scripts/gen.py)`);
  process.exit(1);
}

const problemsA = [];
const problemsB = [];
const problemsC = [];
let nCases = 0;
let nSteps = 0;
let nAuthedSteps = 0;   // перепись предмета предиката C: «ноль находок» != «ноль осмотренного»

for (const file of collections) {
  const label = file.replace('.postman_collection.json', '');
  const col = JSON.parse(fs.readFileSync(path.join(COLLECTIONS_DIR, file), 'utf8'));

  const b = checkCollectionB(col, label);
  if (b) problemsB.push(`${label}: ${b}`);

  for (const { caseItem, steps, trail } of cases(col.item, [label])) {
    nCases += 1;
    nSteps += steps.length;
    const a = checkCaseA(caseItem, steps);
    if (a) problemsA.push(`${trail.join(' :: ')}: ${a}`);
    for (const st of steps) if (stepSubject(st)) nAuthedSteps += 1;
    for (const c of checkCaseC(steps)) problemsC.push(`${trail.join(' :: ')}: ${c}`);
  }
}

console.log(`selftest-assertions: коллекций ${collections.length}, кейсов ${nCases}, шагов ${nSteps}`);
console.log(`  предикат A (принимает взаимоисключающие исходы): ${problemsA.length}`);
console.log(`  предикат B (умолчание подменяет субъекта):        ${problemsB.length}`);
console.log(`  заявленная терпимость уборки (исключено):        ${declaredTeardown.length}`);
console.log(`  предикат C (опрос ведёт не тот субъект):          ${problemsC.length}`
  + `   [шагов с явным субъектом осмотрено: ${nAuthedSteps}]`);

const fatal = problemsA.concat(problemsB).concat(problemsC);
if (fatal.length > 0) {
  console.error(`SELFTEST FAIL: ${fatal.length} находка(ок):`);
  for (const p of fatal) console.error('  - ' + p);
  process.exit(1);
}
if (nCases === 0) {
  console.error('SELFTEST FAIL: ни одного кейса не осмотрено — гейт ничего не проверил');
  process.exit(1);
}
if (nAuthedSteps === 0) {
  // Предпосылка предиката C: в дереве ЕСТЬ шаги с явным субъектом. Их отсутствие —
  // не чистота, а сломанный признак (форма записи субъекта изменилась) либо
  // исчезнувший предмет.
  console.error('SELFTEST FAIL: ни одного шага с явным субъектом — предикату C нечего проверять');
  process.exit(1);
}
console.log(`SELFTEST: PASS — ${nCases} кейс(ов) различают успех и отказ; умолчание субъекта не подменяется; `
  + `${nAuthedSteps} шаг(ов) с явным субъектом опрашиваются им же`);
