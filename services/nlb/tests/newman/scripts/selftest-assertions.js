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
// Предикаты. Перечень НЕ выписан числом: он сверяется с накопителями вердикта
// (см. «шапка сверяется с ВЕРДИКТОМ» ниже), поэтому запись без предиката и
// предикат без записи одинаково роняют гейт.
//
//   A (взаимоисключающие исходы) — кейс, у которого шаг молча принимает И успех, И
//       отказ, НИ ОДНО утверждение кейса не падает в «мире успеха», И исход этого шага
//       ни с чем не связан. Такой кейс не может упасть на том, ради чего назван: отказ,
//       который он проверяет, и успех, который он должен ловить, для него неразличимы.
//       Последнее условие спрашивается отдельно и разнородным миром: мир успеха
//       гомогенен, поэтому утверждение о СВЯЗИ двух шагов («повторный вызов отвечает ТО
//       ЖЕ, что первый» — идемпотентность, у которой исход первого вызова не
//       зафиксирован заранее) в нём тождественно истинно, и без второго вопроса целый
//       вид законного утверждения обвинялся бы напрасно. Связью при этом считается не
//       всякое возражение разнородного мира: возражение засчитывается, только если
//       возразивший шаг ЧУВСТВИТЕЛЕН К СВОЕМУ СОБСТВЕННОМУ ответу. Иначе возражает
//       КАСКАД — шаг, разыменовывающий величину, которую отказанный шаг публикует лишь
//       на успехе; он падает при любом своём ответе и о связи не говорит ничего.
//                                                                     → ОТКАЗ
//
//   B (подмена субъекта умолчанием) — коллекционный prerequest, который при
//       НЕЗАСЕЯННОМ субъекте суиты ставит заголовок с ДРУГИМ, более полномочным
//       субъектом. Тогда вся суита проверяет каскад прав администратора, а не то, что
//       заявлено, и отчитывается зелёным. Отсутствие фикстуры обязано быть отказом, а
//       не тихой подменой.                                            → ОТКАЗ
//
//   C (опрос ведёт не тот субъект) — операцию завёл шаг под явным субъектом, а её
//       опрос ведёт другой субъект (или умолчание коллекции). Чтение операции
//       owner-scoped, поэтому чужой опрос получает 404, неотличимый от «нет такой»:
//       исход мутации остаётся НЕизвестен, а кейс выглядит продуктовым дефектом.
//       Референт опроса — шаг, ЗАХВАТИВШИЙ opId, а не сосед по списку.
//                                                                     → ОТКАЗ
//
//   D (ретрай peer-полосы) — шаг, обёрнутый bounded-ретраем над окном видимости
//       чужой ссылки, обязан ПОВТОРЯТЬСЯ на переходном отказе
//       (`ErrorInfo.reason = PEER_RESOURCE_MISSING`) и НЕ повторяться на
//       терминальном (та же генерическая проза, код 3, без token'а). Первое без
//       второго маскирует негативы; второе без первого сжигает шаг с первой
//       попытки на здоровом продукте.                                 → ОТКАЗ
//
//   E (ведомость ожидания окна прав) — обёртка ожидания окна материализации прав
//       обязана оставлять СЛЕД: исчерпание бюджета отличимо от того, что ждать не
//       пришлось. Спрашивается исполнением, в обе стороны: переходный ответ,
//       держащийся всегда, обязан исчерпать бюджет и записать в ведомость счёт И
//       имя шага; ответ вне полосы повтора обязан не записать ничего. Счётчик,
//       растущий безусловно, прошёл бы первое утверждение и лгал бы на каждом
//       здоровом прогоне.                                            → ОТКАЗ
//
//   F (ведомость ожидания прочих видов) — то же требование к четырём остальным
//       видам ожидания, ведущим ту же ведомость: видимость чужой ссылки, переезд
//       операции, сходимость состояния, появление в списке. Расширить на них E
//       нельзя — он выводит переходный ответ из полосы КОДОВ, а у этих видов
//       переходность задаётся телом (замерено: 123 находки из 123, все ложные).
//       Поэтому полоса объявляется ПО ВИДУ и проверяется исполнением: шаг обязан
//       повториться на объявленном переходном и не повториться на объявленном
//       спокойном, иначе полоса сменилась и судить нечем. Вид без описания —
//       тоже отказ: иначе новая форма ожидания завела бы слепую зону.  → ОТКАЗ
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

// Признак шага уборки. Терпимость к отказу заявляется маркером в имени утверждения,
// но заявить её вправе только уборка: её предмет — убрать за собой, а не проверить.
const TEARDOWN_STEP = /^(cleanup|teardown)/i;

const declaredTeardown = [];   // заявленная терпимость уборки: считается и печатается
const linkedOutcome = [];      // исход шага СВЯЗАН со следующим: считается и печатается

// stepObjectsInEveryWorld — возражает ли ЭТОТ шаг при ЛЮБОМ СВОЁМ ответе, стартуя
// из одного и того же состояния окружения. Шаг, сверяющий свой исход с чужим
// опубликованным («второй вызов ответил то же, что первый»), при СОГЛАСОВАННОМ
// ответе проходит — значит его возражение было о СООТНОШЕНИИ. Шаг, который просто
// разыменовывает недостающую величину, падает при каждом своём ответе: он говорит
// не о соотношении, а о величине, которой нет.
function stepObjectsInEveryWorld(step, envSnapshot) {
  for (const world of [...successShapesFor(step), ...REFUSALS]) {
    const r = runStep(step, world, foreignAwareMap(envSnapshot));
    if (r.failed.length === 0 && !r.threw) return false;
  }
  return true;
}

// caseObjectsToTheRefusedOutcome — возражает ли кейс ИМЕННО НА ИСХОД этого шага,
// когда шаг получил отказ, а прочие шаги — успех.
//
// Спрашивается ИСПОЛНЕНИЕМ, а не разбором текста: как именно исход публикуется
// (переменная окружения, ветка скрипта, что угодно ещё), гейт знать не обязан — он
// смотрит на последствия. Поэтому у ветки нет перечня «законных форм записи»,
// который пришлось бы держать полным.
//
// ВОПРОСА ДВА, и второй — несущий. Первого («возразил ли кто-нибудь в разнородном
// мире») НЕДОСТАТОЧНО: возразить может КАСКАД. Шаг, который лишь разыменовывает
// величину, опубликованную отказанным шагом ТОЛЬКО на успехе, падает не потому, что
// кейс что-то утверждает о связи, а потому, что величины нет, — а исходы для кейса
// по-прежнему неразличимы. Форма эта в дереве ДОМИНИРУЕТ («на успехе положил в
// переменную, дальше её читают»), поэтому засчитывать её за связь значит открыть
// вход целому классу, а не краю.
//
// Второй вопрос разделяет одно и другое: ЧУВСТВИТЕЛЕН ЛИ возразивший шаг к своему
// СОБСТВЕННОМУ ответу (stepObjectsInEveryWorld). Чувствителен — возражение о
// соотношении, связь есть. Падает при любом своём ответе — каскад, и находка
// остаётся. Сужение проверять «упало утверждение» вместо «упало ИЛИ бросило» тут не
// работает и проверено: бросок происходит ВНУТРИ колбэка pm.test и приходит как
// упавшее утверждение, а не как брошенный скрипт.
//
// ЧЕГО ЭТОТ ВОПРОС НЕ РАЗЛИЧАЕТ — названо, чтобы граница не выдавалась за полноту:
//
//   - чувствительность меряется ШЕСТЬЮ подставленными ответами (две формы успеха и
//     четыре отказа). Шаг, чьё утверждение различает исходы ВНУТРИ одной формы (по
//     полю тела, а не по коду), выглядит нечувствительным и будет назван каскадом.
//     Такой кейс, впрочем, обычно возражает уже в мире успеха и сюда не доходит;
//   - «связь» здесь означает лишь, что возражение зависит от СВОЕГО ответа
//     возразившего шага. Что связь эта осмысленна — вопрос не машинный: кейс,
//     сверяющий свой код с чужим опубликованным по неверному правилу, признаётся
//     связанным. Гейт судит наличие утверждения о соотношении, а не его истинность;
//   - ветка молчит о том, ПРАВИЛЬНОЕ ли значение публикует отказанный шаг: она
//     смотрит последствия, а не запись.
function caseObjectsToTheRefusedOutcome(steps, target, refusal) {
  for (const shapeIdx of [0, 1]) {
    const env = baseEnv();
    for (const st of steps) {
      const before = new Map(env);
      const world = st === target ? refusal : successShapesFor(st)[shapeIdx];
      const r = runStep(st, world, env);
      if (r.failed.length === 0 && !r.threw) continue;
      if (!stepObjectsInEveryWorld(st, before)) return true;
      // Каскад: это возражение не об исходе. Кейс на этом НЕ обрывается — дальше по
      // списку может стоять шаг, который об исходе как раз говорит, и оборвать
      // здесь значило бы обвинить кейс за то, что перед его настоящим утверждением
      // оказался шаг-каскад.
    }
  }
  return false;
}

// --- предикат A: кейс, принимающий взаимоисключающие исходы ------------------
// Кейс прогоняется в двух ГОМОГЕННЫХ «мирах успеха»: ВСЕ его шаги получают 200 с
// УДАВШЕЙСЯ операцией. Если при этом ни одно утверждение кейса не падает, а какой-то
// его шаг при том же скрипте принимает и отказ (4xx) — спрашивается третье, и только
// после него выносится вердикт: возражает ли кейс в РАЗНОРОДНОМ мире, где отказ
// получает именно этот шаг. Не возражает и там — исходы для кейса действительно
// неразличимы. Возражает — исход шага связан с последующим, и находки нет.
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
      // Маркер действует ТОЛЬКО на шаге уборки. Без этого условия исключение
      // приклеивается к содержательному шагу одним словом в имени утверждения —
      // то есть становится способом замолчать гейт, а не заявлением о предмете.
      // Перепись дерева: маркер несут 4 шага, и все четыре — уборочные, поэтому
      // сужение ничего не отнимает у существующих и закрывает вход следующему.
      const isTeardown = TEARDOWN_STEP.test(st.name || '');
      const declared = isTeardown && r.executed.some((n) => /best-effort/i.test(n));
      if (declared) { declaredTeardown.push(`${st.name}: ${r.executed.find((n) => /best-effort/i.test(n))}`); continue; }

      // Мир успеха ГОМОГЕНЕН: в нём все шаги получают один и тот же вид ответа.
      // Утверждение о СВЯЗИ двух шагов («повторный вызов отвечает ТО ЖЕ, что первый»)
      // в таком мире тождественно истинно by construction — не потому, что кейс
      // неразличающий, а потому, что мир о связи не высказывается вовсе. Целый вид
      // утверждения оказывался вне наблюдения; отказ ему приписывался ошибочно.
      //
      // Спрашиваем прямо: пусть ЭТОТ шаг получит отказ, а прочие — успех, — и
      // возражение засчитывается за связь ТОЛЬКО если возразивший шаг чувствителен
      // к своему собственному ответу. Иначе возражением оказывается КАСКАД: шаг,
      // разыменовывающий величину, которую отказанный шаг публикует лишь на успехе,
      // падает при любом своём ответе, ничего не утверждая о связи. Развёрнуто —
      // у caseObjectsToTheRefusedOutcome.
      //
      // Это не послабление формы, а второй вопрос: он не пропускает ничего, что
      // прежний вопрос ловил по существу, и снимает только то, что прежний вопрос
      // не умел спросить. Все три стороны закреплены инъекцией: связь молчит
      // (prove: A(г)), публикация без связи ловится (A(д)), каскад ловится (A(з)),
      // а условная публикация, о которой кейс ГОВОРИТ, молчит (A(и)).
      if (caseObjectsToTheRefusedOutcome(steps, st, ref)) {
        linkedOutcome.push(`${st.name}: отказ ${ref.label} связан с последующим шагом`);
        continue;
      }

      return `шаг «${st.name}» принимает И успех (200, операция удалась), И отказ (${ref.label}), ` +
        'и ни при одной форме успеха кейс не возражает; исход шага ни с чем не связан — ' +
        'отказ и успех для кейса неразличимы, упасть на том, ради чего он назван, он не может';
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

  // A(г) ЗАКОННЫЙ БЛИЗНЕЦ ветки разнородного мира: исход шага не зафиксирован заранее
  // (успел ли вызов до завершения операции — свойство времени, а не контракта), но он
  // ПУБЛИКУЕТСЯ и СВЯЗЫВАЕТСЯ со следующим шагом. Такой кейс падает на регрессе
  // идемпотентности — «второй ответил не то, что первый» — и находкой быть не должен.
  const legitLinked = [
    mkStep('cancel-first', [
      "pm.test('исход первой отмены — законный', () => pm.expect(pm.response.code).to.be.oneOf([200, 400]));",
      "pm.environment.set('cancelFirstCode', String(pm.response.code));",
    ]),
    mkStep('cancel-second', [
      "pm.test('повторная отмена вернула тот же исход, что первая', () => "
      + "pm.expect(String(pm.response.code)).to.eql(pm.environment.get('cancelFirstCode')));",
    ]),
  ];
  if (checkCaseA({}, legitLinked)) problems.push('предикат A ошибочно поймал законную связанную форму (исход публикуется и связывается со следующим шагом)');

  // A(д) ДЕФЕКТ той же формы, без которого ветка выше стала бы дырой: исход
  // публикуется — и НИЧЕМ не связан. Публикация сама по себе ничего не утверждает;
  // отличает связанное от несвязанного только последствие, а не наличие записи.
  const defectPublishedUnlinked = [
    mkStep('cancel-first', [
      "pm.test('исход первой отмены — законный', () => pm.expect(pm.response.code).to.be.oneOf([200, 400]));",
      "pm.environment.set('cancelFirstCode', String(pm.response.code));",
    ]),
    mkStep('cancel-second', [
      "pm.test('повторная отмена ответила', () => pm.expect(pm.response.code).to.be.oneOf([200, 400]));",
    ]),
  ];
  if (!checkCaseA({}, defectPublishedUnlinked)) problems.push('предикат A не поймал публикацию исхода, которую ничто не связывает');

  // A(з) КАСКАД — дефект, ради которого у ветки связи появился второй вопрос.
  // Шаг публикует величину ТОЛЬКО на успехе; следующий шаг об исходе первого не
  // говорит ничего и просто её разыменовывает. В разнородном мире он падает — но
  // от отсутствия величины, а не от рассогласования. Форма в дереве доминирует,
  // поэтому засчитывать её за связь значило бы открыть вход целому классу.
  const defectCascade = [
    mkStep('step-one', [
      "pm.test('исход шага 1 — законный', () => pm.expect(pm.response.code).to.be.oneOf([200, 400]));",
      "if (pm.response.code === 200) { pm.environment.set('someVar', 'abcdef'); }",
    ]),
    mkStep('step-two', [
      "const v = pm.environment.get('someVar');",
      "pm.test('префикс годен', () => pm.expect(v.substring(0, 3)).to.eql('abc'));",
    ]),
  ];
  if (!checkCaseA({}, defectCascade)) problems.push('предикат A засчитал КАСКАД за связь: шаг разыменовал величину, которой нет, и это принято за утверждение о связи');

  // A(и) ЗАКОННЫЙ БЛИЗНЕЦ каскада: публикация ТОЖЕ условная — и ровно поэтому проба
  // нужна. Отличает не форма записи, а то, ГОВОРИТ ли следующий шаг о соотношении:
  // здесь он сверяет свой исход с наличием публикации, поэтому при согласованном
  // ответе проходит. Сужение, отвергающее условную публикацию как таковую, обвиняло
  // бы этот кейс напрасно.
  const legitConditionalButSpoken = [
    mkStep('step-one', [
      "pm.test('исход шага 1 — законный', () => pm.expect(pm.response.code).to.be.oneOf([200, 400]));",
      "if (pm.response.code === 200) { pm.environment.set('firstSucceeded', 'yes'); }",
    ]),
    mkStep('step-two', [
      "pm.test('второй исход согласован с первым', () => pm.expect(pm.response.code === 200)"
      + ".to.eql(pm.environment.get('firstSucceeded') === 'yes'));",
    ]),
  ];
  if (checkCaseA({}, legitConditionalButSpoken)) problems.push('предикат A обвинил условную публикацию, о соотношении с которой кейс ГОВОРИТ');

  // A(к) ЗАКОННАЯ форма, в которой каскад стоит ПЕРЕД настоящим утверждением о
  // связи. Возражение каскада в счёт не идёт, но и обрывать на нём кейс нельзя:
  // шаг, который об исходе ГОВОРИТ, стоит следующим. Без этой пробы починка
  // каскада сама завела бы ложные находки на кейсах из трёх и более шагов.
  const legitLinkedBehindCascade = [
    mkStep('first', [
      "pm.test('исход первого — законный', () => pm.expect(pm.response.code).to.be.oneOf([200, 400]));",
      "pm.environment.set('firstCode', String(pm.response.code));",
      "if (pm.response.code === 200) { pm.environment.set('someVar', 'abcdef'); }",
    ]),
    mkStep('cascading-middle', [
      "const v = pm.environment.get('someVar');",
      "pm.test('префикс годен', () => pm.expect(v.substring(0, 3)).to.eql('abc'));",
    ]),
    mkStep('second', [
      "pm.test('повторный вызов вернул тот же исход, что первый', () => "
      + "pm.expect(String(pm.response.code)).to.eql(pm.environment.get('firstCode')));",
    ]),
  ];
  if (checkCaseA({}, legitLinkedBehindCascade)) problems.push('предикат A оборвал кейс на шаге-каскаде и не увидел утверждения о связи за ним');

  // A(е) ЗАКОННАЯ заявленная терпимость уборки: маркер в имени утверждения ПЛЮС
  // уборочное имя шага. Такой шаг исключается и печатается отдельной строкой.
  const legitTeardown = [mkStep('cleanup-thing', [
    "pm.test('reclaim best-effort (never fails the case)', () => pm.expect(pm.response.code).to.be.oneOf([200, 404]));",
  ])];
  if (checkCaseA({}, legitTeardown)) problems.push('предикат A ошибочно поймал заявленную терпимость уборки');

  // A(ж) ДЕФЕКТ: тот же маркер на СОДЕРЖАТЕЛЬНОМ шаге. Без этой пробы исключение
  // выродилось бы в слово, которым гейт замолкает на чём угодно.
  const defectMarkerOutsideTeardown = [mkStep('create-thing', [
    "pm.test('create best-effort (never fails the case)', () => pm.expect(pm.response.code).to.be.oneOf([200, 404]));",
  ])];
  if (!checkCaseA({}, defectMarkerOutsideTeardown)) problems.push('предикат A принял маркер терпимости на шаге, который уборкой не является');

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
  // `captures` — шаг КЛАДЁТ opId в окружение; именно это делает его референтом
  // последующего опроса (см. checkCaseC).
  const stepWith = (name, subj, captures) => ({
    name,
    request: { method: 'GET', url: { raw: 'x' } },
    event: [
      ...(subj ? [{ listen: 'prerequest', script: { exec: [
        `// AZD per-step: bearer from env '${subj}'`,
        `const __t = pm.environment.get('${subj}') || '';`,
      ] } }] : []),
      ...(captures ? [{ listen: 'test', script: { exec: [
        "pm.environment.set('opId', String(pm.response.json().id));",
      ] } }] : []),
    ],
  });
  // ДЕФЕКТ: мутация под явным субъектом, опрос — на умолчании.
  if (checkCaseC([stepWith('cr-as-sa', 'jwtServiceAccountEditor', true), stepWith('poll-op-1', null)]).length !== 1) {
    problems.push('предикат C не поймал опрос, оставленный на умолчании после явного субъекта');
  }
  // ЗАКОННО: тот же субъект у обоих — молчит.
  if (checkCaseC([stepWith('cr-as-sa', 'jwtServiceAccountEditor', true),
    stepWith('poll-op-1', 'jwtServiceAccountEditor')]).length !== 0) {
    problems.push('предикат C покраснел на согласованном субъекте');
  }
  // ЗАКОННО: шаг без явного субъекта — заявки нет, сверять нечего.
  if (checkCaseC([stepWith('cr', null, true), stepWith('poll-op-1', null)]).length !== 0) {
    problems.push('предикат C потребовал субъекта там, где его никто не заявлял');
  }
  // ЗАКОННО: за шагом идёт не опрос — правило о нём ничего не говорит.
  if (checkCaseC([stepWith('cr-as-sa', 'jwtServiceAccountEditor', true), stepWith('get-after', null)]).length !== 0) {
    problems.push('предикат C сработал на шаге, который операцию не опрашивает');
  }
  // ЗАКОННО и ровно та форма, на которой прежняя редакция давала ложный срабат:
  // между захватом операции и её опросом стоит ОТВЕРГНУТЫЙ шаг ДРУГОГО субъекта
  // (негатив «не создатель не может отменить»). Он операции не заводит, поэтому
  // референт опроса — по-прежнему захвативший её создатель.
  if (checkCaseC([
    stepWith('cr-as-A', 'jwtProjectEditorA', true),
    stepWith('cancel-as-B', 'jwtProjectEditorB'),
    stepWith('poll-op-1', 'jwtProjectEditorA'),
  ]).length !== 0) {
    problems.push('предикат C покраснел на отвергнутом шаге другого субъекта, который операции не заводит');
  }
  // И зеркально: если опрос ведёт тот, кому только что отказали, — это дефект.
  if (checkCaseC([
    stepWith('cr-as-A', 'jwtProjectEditorA', true),
    stepWith('cancel-as-B', 'jwtProjectEditorB'),
    stepWith('poll-op-1', 'jwtProjectEditorB'),
  ]).length !== 1) {
    problems.push('предикат C не поймал опрос под субъектом, которому операция не видна');
  }

  // --- предикат E: инъекция в обе стороны (четыре оси) ---
  // Порождаемые строки воспроизводятся ЗДЕСЬ в той форме, какую даёт `_budget_ledger`
  // генератора. Это вторая копия, и она названа вслух: гейт на JS не может
  // импортировать генератор на python, а сверять их ТЕКСТАМИ значило бы читать чужой
  // исходник как текст. Расхождение копии с оригиналом ловит не эта пара, а обход
  // коллекций ниже — он судит ПОРОЖДЁННОЕ, а не воспроизведённое здесь.
  const warmGuard = (tail) => [
    "if (pm.environment.get('_authRetryStarted') !== pm.info.requestName) {",
    "  pm.environment.set('_authRetryCount', '0');",
    "  pm.environment.set('_authRetryStarted', pm.info.requestName);",
    "}",
    "const _arc = parseInt(pm.environment.get('_authRetryCount') || '0', 10);",
    "if ([403,404].includes(pm.response.code) && _arc < 3) {",
    "  pm.environment.set('_authRetryCount', String(_arc + 1));",
    "  pm.execution.setNextRequest(pm.info.requestName);",
    "  return;",
    "}",
  ].concat(tail).concat([
    "pm.environment.unset('_authRetryCount');",
    "pm.environment.unset('_authRetryStarted');",
    "pm.test('status 200', () => pm.expect(pm.response.code).to.eql(200));",
  ]);
  const LEDGER = [
    "if ([403,404].includes(pm.response.code) && _arc >= 3) {",
    "  const _wbE = (parseInt(pm.environment.get('warmBudgetExhausted'), 10) || 0) + 1;",
    "  pm.environment.set('warmBudgetExhausted', String(_wbE));",
    "  const _wbL = pm.environment.get('warmBudgetExhaustedSteps') || '';",
    "  pm.environment.set('warmBudgetExhaustedSteps',",
    "    (_wbL ? _wbL + ' ' : '') + pm.info.requestName);",
    "}",
  ];

  // E(а) дефект — концовка ДО задачи #1251: оба исхода сливаются в сброс счётчиков.
  if (!checkStepE(mkStep('warm-before-rya1', warmGuard([])))) {
    problems.push('предикат E не поймал возвращённый дефект (исчерпание бюджета не оставляет следа)');
  }
  // E(б) законная форма — ведомость ведётся: дефекта нет, гейт обязан молчать.
  if (checkStepE(mkStep('warm-after-rya2', warmGuard(LEDGER)))) {
    problems.push('предикат E ошибочно поймал законную форму (ведомость ведётся)');
  }
  // E(в) счётчик, растущий БЕЗУСЛОВНО, — тоже дефект: величина, ненулевая всегда,
  // о бюджете не говорит ничего. Первое утверждение E прошло бы её одну.
  if (!checkStepE(mkStep('warm-always-rya3', warmGuard([
    "const _wbE = (parseInt(pm.environment.get('warmBudgetExhausted'), 10) || 0) + 1;",
    "pm.environment.set('warmBudgetExhausted', String(_wbE));",
    "pm.environment.set('warmBudgetExhaustedSteps', pm.info.requestName);",
  ])))) {
    problems.push('предикат E не поймал безусловный счётчик (растёт и там, где ждать было нечего)');
  }
  // E(г) сменившаяся форма обёртки: полоса повтора из скрипта не читается — судить
  // вслепую нельзя, и молчать об этом тоже.
  if (!checkStepE(mkStep('warm-shapeless-rya4', [
    "pm.test('status 200', () => pm.expect(pm.response.code).to.eql(200));",
  ]))) {
    problems.push('предикат E не заметил, что полоса повтора из скрипта не читается');
  }

  // --- предикат F: инъекция в обе стороны, ПО ВИДАМ ---
  // Обёртки прочих видов воспроизводятся здесь в той форме, какую даёт генератор
  // (та же вторая копия, что у E, и по той же причине: гейт на JS не импортирует
  // генератор на python). Расхождение копии с оригиналом ловит не эта пара, а обход
  // коллекций: он судит ПОРОЖДЁННОЕ, и на неопознанной полосе отказывает.
  const kindOf = (key) => WAIT_KINDS.find((k) => k.key === key);
  const ledgerTail = (guard) => [
    `if (${guard}) {`,
    "  const _wbE = (parseInt(pm.environment.get('warmBudgetExhausted'), 10) || 0) + 1;",
    "  pm.environment.set('warmBudgetExhausted', String(_wbE));",
    "  const _wbL = pm.environment.get('warmBudgetExhaustedSteps') || '';",
    "  pm.environment.set('warmBudgetExhaustedSteps',",
    "    (_wbL ? _wbL + ' ' : '') + pm.info.requestName);",
    "}",
  ];
  const crGuard = (tail) => [
    "if (pm.environment.get('_crRetryStarted') !== pm.info.requestName) {",
    "  pm.environment.set('_crRetryCount', '0');",
    "  pm.environment.set('_crRetryStarted', pm.info.requestName);",
    "}",
    "const _crc = parseInt(pm.environment.get('_crRetryCount') || '0', 10);",
    "let _crNotFound = false;",
    "try {",
    "  const _crb = pm.response.json();",
    "  _crNotFound = (_crb.details || []).some(d => d && d.reason === 'PEER_RESOURCE_MISSING');",
    "} catch (e) {}",
    "if (_crNotFound && _crc < 3) {",
    "  pm.environment.set('_crRetryCount', String(_crc + 1));",
    "  pm.execution.setNextRequest(pm.info.requestName);",
    "  return;",
    "}",
  ].concat(tail).concat([
    "pm.environment.unset('_crRetryCount');",
    "pm.environment.unset('_crRetryStarted');",
    "pm.test('status 200', () => pm.expect(pm.response.code).to.eql(200));",
  ]);
  const stGuard = (tail) => [
    "if (pm.environment.get('_stRetryStarted') !== pm.info.requestName) {",
    "  pm.environment.set('_stRetryCount', '0');",
    "  pm.environment.set('_stRetryStarted', pm.info.requestName);",
    "}",
    "const _stc = parseInt(pm.environment.get('_stRetryCount') || '0', 10);",
    "let _converged = false;",
    "try { _converged = !!(pm.response.json().converged === true); } catch (e) { _converged = false; }",
    "const _stTransient = [403,404].includes(pm.response.code) || (pm.response.code === 200 && !_converged);",
    "if (_stTransient && _stc < 3) {",
    "  pm.environment.set('_stRetryCount', String(_stc + 1));",
    "  pm.execution.setNextRequest(pm.info.requestName);",
    "  return;",
    "}",
  ].concat(tail).concat([
    "pm.environment.unset('_stRetryCount');",
    "pm.environment.unset('_stRetryStarted');",
  ]);

  // F(а) дефект — концовка без ведомости: исчерпание бесследно.
  if (checkStepF(mkStep('peer-wait-before-cr1', crGuard([])), kindOf('cr')).verdict !== 'finding') {
    problems.push('предикат F не поймал возвращённый дефект (исчерпание бюджета прочего вида бесследно)');
  }
  // F(б) законная форма — ведомость ведётся: гейт обязан молчать.
  if (checkStepF(mkStep('peer-wait-after-cr2', crGuard(ledgerTail('_crNotFound && _crc >= 3'))), kindOf('cr')).verdict !== 'ok') {
    problems.push('предикат F ошибочно поймал законную форму прочего вида (ведомость ведётся)');
  }
  // F(в) счётчик, растущий БЕЗУСЛОВНО: первое утверждение F прошло бы его одно.
  if (checkStepF(mkStep('peer-wait-always-cr3', crGuard(ledgerTail('true'))), kindOf('cr')).verdict !== 'finding') {
    problems.push('предикат F не поймал безусловный счётчик прочего вида (растёт там, где ждать было нечего)');
  }
  // F(г) сменившаяся полоса: шаг не повторяется на объявленном переходном — судить
  // вслепую нельзя, и молчать об этом тоже.
  if (checkStepF(mkStep('peer-wait-shapeless-cr4', [
    "pm.test('status 200', () => pm.expect(pm.response.code).to.eql(200));",
  ]), kindOf('cr')).verdict !== 'unrecognised') {
    problems.push('предикат F не заметил, что объявленная полоса повтора вида не та');
  }
  // F(д) ДРУГОЙ вид, судимый СВОИМ описанием: полоса у него иная (переходным
  // считается и успешный ответ с несошедшимся телом) — и он обязан пройти.
  if (checkStepF(mkStep('state-wait-after-st5', stGuard(ledgerTail('_stTransient && _stc >= 3'))), kindOf('st')).verdict !== 'ok') {
    problems.push('предикат F не признал исправным ожидание сходимости состояния, судимое своим описанием');
  }
  // F(е) ТО ЖЕ исправное ожидание, судимое описанием ЧУЖОГО вида. Ровно из-за этого
  // расширение предиката E на прочие виды дало 123 ложные находки. F обязан не
  // обвинить, а ОТКАЗАТЬСЯ судить: смешать эти два исхода значило бы либо вернуть
  // слепую зону, либо краснеть на исправном.
  if (checkStepF(mkStep('state-wait-after-st6', stGuard(ledgerTail('_stTransient && _stc >= 3'))), kindOf('cr')).verdict !== 'unrecognised') {
    problems.push('предикат F судил ожидание чужим описанием вида вместо отказа (обвинение исправного)');
  }
  // F(ж) разбор вида: имя ведёт к описанию, посторонняя форма — ни к какому.
  for (const [nm, key] of [['x-cr7', 'cr'], ['poll-op-7', 'op'], ['x-st7', 'st'], ['x-lst7', 'lst']]) {
    const got = waitKindOf({ name: nm });
    if (!got || got.key !== key) problems.push(`разбор вида ожидания: '${nm}' не отнесён к виду '${key}'`);
  }
  if (waitKindOf({ name: 'wait-of-a-brand-new-shape-9' })) {
    problems.push('разбор вида ожидания: посторонняя форма отнесена к известному виду — новая слепая зона прошла бы молча');
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

// CAPTURES_OP — шаг КЛАДЁТ идентификатор операции в окружение. Это и есть признак
// владения: опрос читает `{{opId}}`, поэтому его референт — не сосед по списку, а
// последний шаг, ПОЛОЖИВШИЙ туда значение.
const CAPTURES_OP = /pm\.environment\.set\('opId'/;

function stepCapturesOp(step) {
  for (const ev of step.event || []) {
    if (ev.listen !== 'test') continue;
    if (CAPTURES_OP.test(((ev.script || {}).exec || []).join('\n'))) return true;
  }
  return false;
}

// Возвращает список расхождений внутри одного кейса.
//
// Референт опроса — последний шаг, ЗАХВАТИВШИЙ opId, а не предыдущий по списку.
// Прежняя редакция сравнивала с соседом и краснела на законной конструкции: между
// созданием и опросом законно стоит ОТВЕРГНУТЫЙ шаг другого субъекта (негатив
// «не создатель не может отменить»). Такой шаг операции не заводит — сравнивать
// опрос с ним значит требовать, чтобы операцию A опрашивал B, то есть ровно то,
// что кейс этажом выше объявляет невозможным. Гейт, красный на законной форме,
// снимают целиком — поэтому признак привязан к владению значением, а не к
// соседству.
function checkCaseC(steps) {
  const out = [];
  let owner = null;        // субъект последнего шага, захватившего opId
  let ownerName = null;
  for (const step of steps) {
    if (/^poll-op/.test(step.name || '')) {
      if (!owner) continue;                               // захват был умолчанием — не заявка
      const pollSubj = stepSubject(step);
      if (pollSubj !== owner) {
        out.push(`операцию завёл шаг ${ownerName} под субъектом ${owner}, а опрос ${step.name} ведёт `
          + `${pollSubj || 'умолчание коллекции'}: операция читается по владельцу, `
          + 'и чужой опрос получит 404, неотличимый от «нет такой»');
      }
      continue;
    }
    if (stepCapturesOp(step)) { owner = stepSubject(step); ownerName = step.name; }
  }
  return out;
}

// --- предикат D: ретрай peer-полосы ловит ПЕРЕХОДНОЕ и не глотает ТЕРМИНАЛЬНОЕ ---
//
// `retry_create_until_present` перекрывает окно, в котором чужая ссылка (vpc-шная
// подсеть/адрес, заведённая тем же вызывающим шагом раньше) ещё не видна
// пообъектному authz. У окна два предъявления, и они выглядят ПО-РАЗНОМУ:
//
//   - подсеть → проза `"subnet <id> not found"` (её и ловил прежний признак);
//   - адрес   → `FAILED_PRECONDITION` с намеренно ГЕНЕРИЧЕСКОЙ прозой
//     `"Illegal argument addressId"` (анти-oracle) и машинным
//     `ErrorInfo.reason = PEER_RESOURCE_MISSING` в деталях.
//
// Разбором прозы вторая полоса неотличима от ТЕРМИНАЛЬНОГО mismatch'а, у которого
// текст ровно тот же, — поэтому шаг сжигал свою единственную попытку и падал
// (CI 30919903252: cr-link / cr-ext-link / cr-linked, по одному исполнению каждый).
//
// Здесь проверяется ровно то, что делает признак ИСПОЛНЕНИЕМ, в обе стороны:
// на переходном ответе шаг обязан повториться, на терминальном — НЕТ. Второе не
// менее важно: ретрай, срабатывающий на терминальном отказе, маскировал бы
// негативы, ради которых генерическая проза и заводилась.
const PEER_MISS_BODY = {
  code: 9,
  message: 'Illegal argument addressId',
  details: [{ '@type': 'type.googleapis.com/google.rpc.ErrorInfo', reason: 'PEER_RESOURCE_MISSING', domain: 'nlb.kacho.cloud' }],
};
const TERMINAL_BODY = { code: 3, message: 'Illegal argument addressId', details: [] };

// Шаг, обёрнутый retry_create_until_present, помечен суффиксом `-cr<N>` (гарантия
// уникальности имени для setNextRequest). Признак — на ИМЕНИ, а не на тексте
// скрипта, иначе он ловил бы и комментарий, объясняющий эту же защиту.
const WRAPPED_CREATE = /-cr\d+$/;

function retriesOn(step, body) {
  const exec = scriptOf(step, 'test');
  if (!exec) return null;
  const env = baseEnv();
  return runScript(exec, { response: { code: 400, body }, env, name: step.name }).retried;
}

function checkStepD(step) {
  if (!WRAPPED_CREATE.test(step.name || '')) return null;
  if (retriesOn(step, PEER_MISS_BODY) !== true) {
    return `${step.name}: на переходной полосе (ErrorInfo.reason=PEER_RESOURCE_MISSING) шаг НЕ повторяется `
      + '— окно видимости чужой ссылки не перекрыто, и шаг сгорает с первой попытки';
  }
  if (retriesOn(step, TERMINAL_BODY) !== false) {
    return `${step.name}: терминальный отказ (code 3, та же проза, без token'а) ПОВТОРЯЕТСЯ `
      + '— ретрай маскирует негатив, ради которого генерическая проза и заводилась';
  }
  return null;
}

// --- предикат E: ведомость ожидания отличает ИСЧЕРПАНИЕ от НЕНАДОБНОСТИ -------
//
// ПРЕДМЕТ (задача #1251). У обёрток ожидания концовка вела оба исхода — «повторять
// больше не нужно» и «повторять уже нельзя» — в один и тот же сброс счётчиков.
// След у них получался одинаковый, то есть никакой: «прогреть не удалось» и
// «прогрев не понадобился» становились неразличимы. У шага без собственного
// утверждения исчерпание проходило вовсе бесследно, а отказ доезжал до следующего
// шага — атрибуция сохранялась, наблюдаемость нет. Это то самое окно, из которого
// вырос исходный разбор: отказ в правах получило создание, а обвинён был шаг,
// проверявший запрет удаления.
//
// ЧТО ПРОВЕРЯЕТСЯ — ИСПОЛНЕНИЕМ, В ОБЕ СТОРОНЫ. Шаг гоняется дважды:
//   переходный ответ, держащийся всегда → бюджет обязан ИСЧЕРПАТЬСЯ и оставить след
//                                          (счётчик плюс имя шага в перечне);
//   ответ вне полосы повтора             → следа быть НЕ ДОЛЖНО, иначе счётчик
//                                          растёт там, где ждать было нечего, и
//                                          величина перестаёт что-либо значить.
// Одного первого мало: счётчик, инкрементируемый безусловно, прошёл бы его и лгал
// бы на каждом здоровом прогоне.
//
// ГРАНИЦА НАЗВАНА ЧИСЛОМ, А НЕ УМОЛЧАНИЕМ. Предикат смотрит обёртку окна
// материализации прав (суффикс `-rya`) — прямой предмет задачи и большинство
// ожиданий дерева. У прочих видов переходность задаётся не кодом ответа, а
// содержимым тела (наличие своего id в списке, сходимость поля, машинный признак
// промаха соседа), поэтому один и тот же подставной ответ для них не строится.
// Их число печатается отдельной строкой переписи: «не осмотрено» обязано быть
// отличимо от «осмотрено и чисто».
const WRAPPED_WARM = /-rya\d+$/;
const RETRY_BAND_RE = /\[([\d,\s]+)\]\.includes\(pm\.response\.code\)/;

function warmLedgerAfter(step, code) {
  const exec = scriptOf(step, 'test');
  if (!exec) return null;
  const env = baseEnv();
  const body = { code: 7, message: 'permission denied', details: [] };
  // Петля доводится до конца: пока обёртка повторяет, шаг молчит by construction.
  for (let i = 0; i < 400; i += 1) {
    const r = runScript(exec, { response: { code, body }, env, name: step.name });
    if (!r.retried) break;
  }
  return {
    count: parseInt(env.get('warmBudgetExhausted'), 10) || 0,
    steps: env.get('warmBudgetExhaustedSteps') || '',
  };
}

function checkStepE(step) {
  const exec = scriptOf(step, 'test');
  if (!exec) return null;
  const band = RETRY_BAND_RE.exec(exec.join('\n'));
  if (!band) {
    return `${step.name}: полоса повтора не читается из порождённого скрипта — `
      + 'обёртка сменила форму, и предикат E судил бы вслепую';
  }
  const codes = band[1].split(',').map((x) => parseInt(x.trim(), 10)).filter((x) => x);
  const transient = codes[0];
  const quiet = [200, 201, 204].find((c) => !codes.includes(c));

  const spent = warmLedgerAfter(step, transient);
  if (!spent || spent.count < 1) {
    return `${step.name}: переходный ответ ${transient} держится всегда, бюджет исчерпан — `
      + 'а следа нет: «прогреть не удалось» неотличимо от «прогрев не понадобился»';
  }
  if (!spent.steps.includes(step.name)) {
    return `${step.name}: исчерпание сосчитано, но шаг НЕ НАЗВАН — по величине не видно, `
      + 'где именно бюджета не хватило, и разбор снова начинается с гипотезы';
  }
  const quietRun = warmLedgerAfter(step, quiet);
  if (quietRun && quietRun.count > 0) {
    return `${step.name}: ответ ${quiet} вне полосы повтора — ждать было нечего, `
      + 'а ведомость всё равно записала исчерпание: счётчик, растущий всегда, не величина';
  }
  return null;
}

// --- предикат F: та же ведомость у ПРОЧИХ видов ожидания ----------------------
//
// ПРЕДМЕТ (задачи #1420, #1468). Предикат E полон относительно СВОЕГО предмета —
// ожидания окна материализации прав. Но ту же ведомость `warmBudgetExhausted` ведут
// ещё четыре вида ожидания, и свойство «исчерпание отличимо от ненадобности» у них
// не проверялось ничем: гейт честно печатал «НЕ осмотрено», а «ноль находок» по ним
// означало «не искали».
//
// ПОЧЕМУ НЕ РАСШИРЕНИЕ E — ЗАМЕРЕНО, А НЕ ПРЕДПОЛОЖЕНО. E выводит переходный ответ
// из полосы КОДОВ, объявленной в скрипте. У прочих видов переходность задаётся не
// кодом: у видимости чужой ссылки — машинным признаком промаха соседа в теле, у
// сходимости состояния переходным считается и успешный ответ с несошедшимся телом,
// у появления в списке — двухсотка без своего идентификатора. Прогон E с отбором,
// расширенным на них, дал 123 находки из 123 осмотренных — все ложные: требование
// одного вида, применённое к другому, краснеет на исправном.
//
// ПОЭТОМУ ПОЛОСА ОБЪЯВЛЯЕТСЯ ПО ВИДУ — И ПРОВЕРЯЕТСЯ ИСПОЛНЕНИЕМ. У каждого вида
// свой подставной переходный ответ и свой ответ вне полосы повтора. Объявление не
// принимается на веру: шаг обязан ПОВТОРИТЬСЯ на первом и НЕ повториться на втором,
// иначе полоса сменилась — и это ОТКАЗ, а не тихое «ноль находок». Требование же,
// собственно проверяемое, у всех видов одно и то же, и оно то же, что у E: бюджет,
// исчерпанный на держащемся переходном ответе, обязан оставить в ведомости счёт И
// имя шага; ответ вне полосы повтора обязан не оставить ничего.
//
// Вид, которому не нашлось описания, — тоже ОТКАЗ: иначе следующая форма ожидания
// завела бы новую слепую зону ровно тем же способом, каким её завели эти четыре.
const LEDGER_WRITE = "pm.environment.set('warmBudgetExhausted'";
const OP_SELFTEST_ID = OP_OK.id;
const DENY_BODY = { code: 7, message: 'permission denied', details: [] };
const ABORTED_BODY = { code: 10, message: 'Aborted', details: [] };

const WAIT_KINDS = [
  {
    key: 'cr',
    label: 'видимость чужой ссылки',
    name: /-cr\d+$/,
    transient: { code: 400, body: PEER_MISS_BODY },
    quiet: { code: 400, body: TERMINAL_BODY },
  },
  {
    key: 'op',
    label: 'переезд операции',
    name: /^poll-op-/,
    transient: { code: 200, body: { id: OP_SELFTEST_ID, done: true, error: { code: 9, message: 'subnet not found' } } },
    quiet: { code: 200, body: OP_OK },
    // Переезд снимает opId и уходит на шаг СОЗДАНИЯ; в настоящем прогоне тот ставит
    // его заново. Без пересева петля обрывалась бы на первом круге, и «бюджет не
    // исчерпан» читалось бы как дефект ведомости — обвинение исправного.
    reseed: (env) => env.set('opId', OP_SELFTEST_ID),
  },
  {
    key: 'st',
    label: 'сходимость состояния',
    name: /-st\d+$/,
    transient: { code: 403, body: DENY_BODY },
    quiet: { code: 409, body: ABORTED_BODY },
  },
  {
    key: 'lst',
    label: 'появление в списке',
    name: /-lst\d+$/,
    transient: { code: 200, body: {} },
    quiet: { code: 403, body: DENY_BODY },
  },
];

const waitKindOf = (step) => WAIT_KINDS.find((k) => k.name.test(step.name || ''));

// ledgerAfter — доводит петлю ожидания до конца на ОДНОМ подставленном ответе и
// возвращает след: повторился ли шаг с первого раза и что осталось в ведомости.
function ledgerAfter(step, probe, kind) {
  const exec = scriptOf(step, 'test');
  const env = baseEnv();
  let retriedFirst = null;
  for (let i = 0; i < 400; i += 1) {
    if (kind.reseed) kind.reseed(env);
    const r = runScript(exec, { response: { code: probe.code, body: probe.body }, env, name: step.name });
    if (retriedFirst === null) retriedFirst = r.retried;
    if (!r.retried) break;
  }
  return {
    retriedFirst,
    count: parseInt(env.get('warmBudgetExhausted'), 10) || 0,
    steps: env.get('warmBudgetExhaustedSteps') || '',
  };
}

// checkStepF — три исхода, и различать их обязательно: `ok` (вид судим, свойство
// держится), `finding` (вид судим, свойство нарушено), `unrecognised` (полоса не
// та, что объявлена, — судить нечем). Слить последний с первым значило бы вернуть
// слепую зону; слить со вторым — обвинить исправный шаг за смену формы обёртки.
function checkStepF(step, kind) {
  const exec = scriptOf(step, 'test');
  if (!exec) {
    return { verdict: 'unrecognised', message: `${step.name}: у шага нет test-скрипта, а ведомость он ведёт` };
  }
  const held = ledgerAfter(step, kind.transient, kind);
  if (held.retriedFirst !== true) {
    return { verdict: 'unrecognised', message:
      `${step.name}: объявленный переходный ответ ${kind.transient.code} шаг НЕ повторяет — `
      + `полоса повтора вида «${kind.label}» сменилась, и предикат F судил бы вслепую` };
  }
  if (held.count < 1) {
    return { verdict: 'finding', message:
      `${step.name}: переходный ответ ${kind.transient.code} (${kind.label}) держится всегда, бюджет исчерпан — `
      + 'а следа нет: «прогреть не удалось» неотличимо от «прогрев не понадобился»' };
  }
  if (!held.steps.includes(step.name)) {
    return { verdict: 'finding', message:
      `${step.name}: исчерпание сосчитано, но шаг НЕ НАЗВАН — по величине не видно, `
      + 'где именно бюджета не хватило, и разбор снова начинается с гипотезы' };
  }
  const calm = ledgerAfter(step, kind.quiet, kind);
  if (calm.retriedFirst !== false) {
    return { verdict: 'unrecognised', message:
      `${step.name}: объявленный ответ вне полосы повтора ${kind.quiet.code} шаг ПОВТОРЯЕТ — `
      + `полоса вида «${kind.label}» шире объявленной, и отрицательная половина F вакуумна` };
  }
  if (calm.count > 0) {
    return { verdict: 'finding', message:
      `${step.name}: ответ ${kind.quiet.code} вне полосы повтора — ждать было нечего, `
      + 'а ведомость всё равно записала исчерпание: счётчик, растущий всегда, не величина' };
  }
  return { verdict: 'ok' };
}

const proveProblems = prove();
if (proveProblems.length > 0) {
  console.error('SELFTEST FAIL: гейт не прошёл собственную проверку предпосылки:');
  for (const p of proveProblems) console.error('  - ' + p);
  process.exit(1);
}
console.log('проверка предпосылки: по каждому предикату дефект пойман, законные конструкции той же формы пропущены');

// Накопители послаблений СБРАСЫВАЮТСЯ после самопроверки. Её фикстуры — синтетика,
// а перепись обязана называть объём ДЕРЕВА: иначе «снято N» смешивает найденное в
// коллекциях с тем, что гейт предъявил сам себе, и число перестаёт что-либо мерить.
declaredTeardown.length = 0;
linkedOutcome.length = 0;

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
const problemsD = [];
const problemsE = [];
const problemsF = [];
let nCases = 0;
let nSteps = 0;
let nAuthedSteps = 0;   // перепись предмета предиката C: «ноль находок» != «ноль осмотренного»
let nWrappedCreates = 0; // перепись предмета предиката D (то же требование)
let nWarmSteps = 0;      // перепись предмета предиката E (осмотренные ожидания окна прав)
let nOtherWaits = 0;     // ожидания ПРОЧИХ видов — предмет предиката F
// Перепись предиката F по видам: ДВЕ величины на вид. Одно число скрывает ровно
// тот случай, ради которого F заведён: вид, чьи ожидания осмотрены, но не судятся.
const waitTally = new Map(WAIT_KINDS.map((k) => [k.key, { label: k.label, seen: 0, judged: 0, found: 0 }]));
let nWaitsOfUnknownKind = 0;

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
    for (const st of steps) {
      if (!WRAPPED_CREATE.test(st.name || '')) continue;
      nWrappedCreates += 1;
      const d = checkStepD(st);
      if (d) problemsD.push(`${trail.join(' :: ')}: ${d}`);
    }
    for (const st of steps) {
      // Отбор — ПО ПРИЗНАКУ ОЖИДАНИЯ (суффикс обёртки), а не по наличию ведомости.
      // Обратный порядок («смотрим тех, кто ведомость несёт») спрашивал бы лишь
      // «правильно ли ведут те, кто ведёт», и снятие ведомости с ОДНОГО шага прошло
      // бы мимо: он просто выпал бы из осмотренных. Проверено возвратом дефекта.
      if (!WRAPPED_WARM.test(st.name || '')) {
        // Признак — ЗАПИСЬ в ведомость, а не упоминание её имени: имя стоит и в
        // объясняющем комментарии рядом, и отбор по подстроке считал бы прозу.
        const ex = scriptOf(st, 'test');
        if (!ex || !ex.join('\n').includes(LEDGER_WRITE)) continue;
        nOtherWaits += 1;
        const kind = waitKindOf(st);
        if (!kind) {
          nWaitsOfUnknownKind += 1;
          problemsF.push(`${trail.join(' :: ')}: ${st.name}: вид ожидания не опознан — `
            + 'ведомость ведётся, а описания полосы повтора для этого вида нет: '
            + 'новая форма ожидания завела бы слепую зону тем же способом, каким её завели прежние');
          continue;
        }
        const tally = waitTally.get(kind.key);
        tally.seen += 1;
        const f = checkStepF(st, kind);
        if (f.verdict !== 'unrecognised') tally.judged += 1;
        if (f.verdict !== 'ok') {
          tally.found += 1;
          problemsF.push(`${trail.join(' :: ')}: ${f.message}`);
        }
        continue;
      }
      nWarmSteps += 1;
      const e = checkStepE(st);
      if (e) problemsE.push(`${trail.join(' :: ')}: ${e}`);
    }
    for (const c of checkCaseC(steps)) problemsC.push(`${trail.join(' :: ')}: ${c}`);
  }
}

console.log(`selftest-assertions: коллекций ${collections.length}, кейсов ${nCases}, шагов ${nSteps}`);
console.log(`  предикат A (принимает взаимоисключающие исходы): ${problemsA.length}`);
console.log(`  предикат B (умолчание подменяет субъекта):        ${problemsB.length}`);
console.log(`  заявленная терпимость уборки (исключено):        ${declaredTeardown.length}`);
console.log(`  исход связан со следующим шагом (снято разнородным миром): ${linkedOutcome.length}`);
console.log(`  предикат C (опрос ведёт не тот субъект):          ${problemsC.length}`
  + `   [шагов с явным субъектом осмотрено: ${nAuthedSteps}]`);
console.log(`  предикат D (ретрай peer-полосы: ловит переходное / не глотает терминальное): ${problemsD.length}`
  + `   [обёрнутых create-шагов осмотрено: ${nWrappedCreates}]`);
console.log(`  предикат E (ведомость ожидания окна прав: исчерпание отличимо от ненадобности): ${problemsE.length}`
  + `   [ожиданий ${nWarmSteps} · судятся ${nWarmSteps}]`);
console.log(`  предикат F (та же ведомость у прочих видов; полоса объявлена по виду и проверена исполнением): ${problemsF.length}`
  + `   [ожиданий ${nOtherWaits}]`);
for (const t of waitTally.values()) {
  console.log(`      ${(t.label + ':').padEnd(26)}ожиданий ${String(t.seen).padStart(3)}`
    + ` · судятся ${String(t.judged).padStart(3)} · находок ${t.found}`);
}
console.log(`      ${'вид не опознан:'.padEnd(26)}ожиданий ${String(nWaitsOfUnknownKind).padStart(3)}`
  + ` · судятся   0 · находок ${nWaitsOfUnknownKind}`);

// --- шапка сверяется с ВЕРДИКТОМ ---------------------------------------------
//
// ПРЕДМЕТ (задача #1429). Шапка — единственное место, где предикаты названы
// человеческими словами, и она уже пережила свой предмет: предикат E исполнялся,
// давал вердикт и печатался отдельной строкой переписи, а в перечне шапки его не
// было вовсе; счёт «Четыре» был не переписью, а константой, за которой никто не
// следил. Разбиравший красное по E не находил о нём ни строки, а объявленное число
// вводило в заблуждение прямо. Тот же класс, что гейт ловит у кейсов: утверждение,
// пережившее свой предмет.
//
// Поэтому число в шапке НЕ выписывается, а перечень сверяется с кодом. Сторона
// кода — ключи `VERDICT`: из них же собирается `fatal`, поэтому «объявлено» и
// «участвует в вердикте» разойтись не могут by construction. Сторона шапки — записи
// перечня. Читается СОБСТВЕННЫЙ исходник, и это не чтение чужого текста вместо
// исполняемого: предметом проверки здесь и ЯВЛЯЕТСЯ текст шапки.
const HEADER_ENTRY_RE = /^\/\/ {3}([A-Z]) \(/gm;

function headerAgainstVerdict(letters) {
  const problems = [];
  const listed = [];
  for (const m of fs.readFileSync(__filename, 'utf8').matchAll(HEADER_ENTRY_RE)) listed.push(m[1]);
  if (listed.length === 0) {
    problems.push('перечень предикатов из шапки не читается вовсе — форма записи сменилась, '
      + 'и сверка шапки с вердиктом судила бы вслепую');
    return problems;
  }
  const missing = letters.filter((x) => !listed.includes(x));
  const extra = listed.filter((x) => !letters.includes(x));
  if (missing.length > 0) {
    problems.push(`шапка НЕ называет предикат(ы) ${missing.join(', ')}, хотя они дают вердикт — `
      + 'разбирающий красное по ним не найдёт в шапке ни строки');
  }
  if (extra.length > 0) {
    problems.push(`шапка называет предикат(ы) ${extra.join(', ')}, которых в вердикте нет — `
      + 'перечень пережил свой предмет');
  }
  return problems;
}

// Накопители, дающие вердикт. ЕДИНСТВЕННЫЙ источник и для `fatal`, и для сверки
// шапки: перечень, выписанный вторым местом, разошёлся бы с первым молча.
const VERDICT = { A: problemsA, B: problemsB, C: problemsC, D: problemsD, E: problemsE, F: problemsF };
const headerProblems = headerAgainstVerdict(Object.keys(VERDICT));
console.log(`  шапка называет предикаты вердикта (${Object.keys(VERDICT).join('')}): ${headerProblems.length}`);

const fatal = Object.keys(VERDICT)
  .reduce((acc, k) => acc.concat(VERDICT[k]), [])
  .concat(headerProblems);
if (fatal.length > 0) {
  console.error(`SELFTEST FAIL: ${fatal.length} находка(ок):`);
  for (const p of fatal) console.error('  - ' + p);
  process.exit(1);
}
if (nCases === 0) {
  console.error('SELFTEST FAIL: ни одного кейса не осмотрено — гейт ничего не проверил');
  process.exit(1);
}
if (nWrappedCreates === 0) {
  // Предпосылка предиката D: обёрнутые create-шаги в дереве ЕСТЬ. Их исчезновение —
  // либо снятая защита от окна видимости, либо сменившийся суффикс обёртки; и то и
  // другое обязано быть отказом, а не тихим «ноль находок».
  console.error('SELFTEST FAIL: ни одного обёрнутого create-шага — предикату D нечего проверять');
  process.exit(1);
}
if (nWarmSteps === 0) {
  // Предпосылка предиката E: ожидания окна материализации прав в дереве ЕСТЬ, и они
  // ведут ведомость. Их исчезновение — либо снятая ведомость (тогда исчерпание снова
  // бесследно), либо сменившийся суффикс обёртки; и то и другое обязано быть отказом,
  // а не тихим «ноль находок».
  console.error('SELFTEST FAIL: ни одного ожидания с ведомостью — предикату E нечего проверять');
  process.exit(1);
}
if (nOtherWaits === 0) {
  // Предпосылка предиката F: ожидания ПРОЧИХ видов в дереве ЕСТЬ и ведут ведомость.
  // Их исчезновение — либо снятая ведомость (тогда исчерпание снова бесследно), либо
  // сменившийся признак записи; и то и другое обязано быть отказом, а не тишиной.
  console.error('SELFTEST FAIL: ни одного ожидания прочих видов — предикату F нечего проверять');
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
