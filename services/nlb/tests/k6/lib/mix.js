// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Workload mix per design §7.4.
//
//   60% reads          (NLB.Get / NLB.List / TG.Get)
//   20% Create+Delete  (short LB / Listener / TG lifecycle)
//   10% AddTargets / RemoveTargets
//   10% Listener wire / unwire TargetGroup
//
// Each VU iteration picks ONE op weighted by these probabilities. The
// caller (scenario) controls VU count and iteration rate; we only decide
// "which op runs this iteration".

import {
  getLB, listLB, getTG, listTG,
  createLB, deleteLB,
  createListener, deleteListener,
  createTG, deleteTG,
  addTargets, removeTargets,
  wireListenerTG, unwireListenerTG,
} from './dsl.js';
import { pollOperation } from './poll-op.js';
import { FIXTURES, pickOne, validateRequiredOnce } from './fixtures.js';
import { templates } from './payloads.js';
import { check } from 'k6';

// Lifecycle resources created within a scenario are tracked PER-VU so we
// can tear down in teardown(). Cross-VU sharing of mutable arrays is not
// supported by k6 — that's intentional, it keeps cleanup local.
const created = {
  lbs: [],
  listeners: [],
  tgs: [],
};

export function exportCreated() {
  return created;
}

// runMixedIteration executes ONE op picked by the design weights.
// Returns the high-level op label so the scenario can tag custom metrics.
export function runMixedIteration() {
  validateRequiredOnce();
  const r = Math.random();
  if (r < 0.60) return doRead(r);
  if (r < 0.80) return doShortLifecycle();
  if (r < 0.90) return doTargetsOp();
  return doWireOp();
}

// --- READ (60%) ----------------------------------------------------------

function doRead(rRaw) {
  // Sub-pick within reads to ensure all three read RPCs see traffic.
  const sub = (rRaw * 100) % 3; // 0,1,2
  if (sub < 1) {
    // NLB.List with a tight page_size — exercises pagination + filter path.
    const res = listLB(`page_size=20`);
    check(res, { 'read NLB.List 2xx': (r) => r.status >= 200 && r.status < 300 });
    return 'NLB.List';
  }
  if (sub < 2) {
    // NLB.Get on a warm-set id (avoids the predictable 404 hot path).
    const id = pickOne(FIXTURES.readLbIds) || pickOne(created.lbs);
    if (!id) {
      // Fall back to a List so the read budget isn't wasted.
      listLB(`page_size=10`);
      return 'NLB.List';
    }
    const res = getLB(id);
    check(res, { 'read NLB.Get 2xx-or-404': (r) => r.status < 500 });
    return 'NLB.Get';
  }
  const id = pickOne(FIXTURES.readTgIds) || pickOne(created.tgs);
  if (!id) {
    listTG(`page_size=10`);
    return 'TG.List';
  }
  const res = getTG(id);
  check(res, { 'read TG.Get 2xx-or-404': (r) => r.status < 500 });
  return 'TG.Get';
}

// --- SHORT LIFECYCLE: Create -> Delete (20%) -----------------------------

function doShortLifecycle() {
  // Round-robin Create across LB / Listener / TG to spread load fairly.
  const pick = Math.floor(Math.random() * 3);
  if (pick === 0) return shortLBCycle();
  if (pick === 1) return shortListenerCycle();
  return shortTGCycle();
}

function shortLBCycle() {
  const c = createLB({ projectId: FIXTURES.projectId, regionId: FIXTURES.regionId });
  check(c, { 'shortLB Create 2xx': (r) => r.status >= 200 && r.status < 300 });
  if (c.status >= 300 || !c.opId) return 'NLB.Create';
  const op = pollOperation(c.opId, { tag: 'mix-create-lb', maxAttempts: 30 });
  const lbId = op.ok && op.response ? op.response.id : '';
  if (lbId) {
    created.lbs.push(lbId);
    // Immediate teardown — short cycle.
    const d = deleteLB(lbId);
    check(d, { 'shortLB Delete 2xx': (r) => r.status >= 200 && r.status < 300 });
  }
  return 'NLB.Create+Delete';
}

function shortListenerCycle() {
  // Requires a parent LB; reuse a created one if any, else fall through to
  // an LB cycle (which still exercises Create+Delete).
  // Условия «есть адрес» здесь больше нет: собственного адреса листенер не несёт,
  // он наследует VIP балансировщика. Прежний гейт на `FIXTURES.addressId` уводил
  // полосу в цикл балансировщика всякий раз, когда адрес не задан, — то есть
  // ставил условием величину, которая на запрос не влияет вовсе.
  const lbId = pickOne(created.lbs);
  if (!lbId) return shortLBCycle();
  const c = createListener({ lbId });
  check(c, { 'shortListener Create 2xx-or-pre': (r) => r.status < 500 });
  if (c.status >= 300 || !c.opId) return 'Listener.Create';
  const op = pollOperation(c.opId, { tag: 'mix-create-listener', maxAttempts: 30 });
  const lid = op.ok && op.response ? op.response.id : '';
  if (lid) {
    // Id НЕ регистрируется в created.listeners: он удаляется тут же, а перечень
    // читают другие полосы и teardownAll. Прежде мёртвый id туда попадал, и
    // уборка повторно удаляла удалённое.
    const d = deleteListener(lid);
    check(d, { 'shortListener Delete 2xx': (r) => r.status >= 200 && r.status < 300 });
  }
  return 'Listener.Create+Delete';
}

function shortTGCycle() {
  const c = createTG({ projectId: FIXTURES.projectId, regionId: FIXTURES.regionId });
  check(c, { 'shortTG Create 2xx': (r) => r.status >= 200 && r.status < 300 });
  if (c.status >= 300 || !c.opId) return 'TG.Create';
  const op = pollOperation(c.opId, { tag: 'mix-create-tg', maxAttempts: 30 });
  const tgId = op.ok && op.response ? op.response.id : '';
  if (tgId) {
    created.tgs.push(tgId);
    const d = deleteTG(tgId);
    check(d, { 'shortTG Delete 2xx': (r) => r.status >= 200 && r.status < 300 });
  }
  return 'TG.Create+Delete';
}

// --- TARGETS (10%) -------------------------------------------------------

function doTargetsOp() {
  const tgId = pickOne(created.tgs) || pickOne(FIXTURES.readTgIds);
  if (!tgId) return shortTGCycle();
  const t1 = templates.externalTarget((Date.now() % 200) + 1);
  const t2 = templates.externalTarget((Date.now() % 200) + 50);
  const add = addTargets(tgId, [t1, t2]);
  check(add, { 'AddTargets 2xx-or-pre': (r) => r.status < 500 });
  // RemoveTargets in the same iteration — 2-phase drain (Phase A is fast).
  const rem = removeTargets(tgId, [t1]);
  check(rem, { 'RemoveTargets 2xx-or-pre': (r) => r.status < 500 });
  return 'TG.Add+RemoveTargets';
}

// --- WIRE (10%) ----------------------------------------------------------

// Привязка группы целей живёт на ЛИСТЕНЕРЕ. Прежде эта полоса звала
// `:attachTargetGroup` / `:detachTargetGroup`, которых в контракте нет: край
// отвечал `404`, проверка `status < 500` его принимала, и десять процентов смеси
// мерили несуществующий глагол, выглядя при этом здоровыми (задача продукта #1617).
//
// Порог поднят до `< 400` намеренно: у живого глагола отказ клиента — это отказ,
// а не «почти успех». Прежний порог не отличил бы починку от её отсутствия.
function doWireOp() {
  const lbId = pickOne(created.lbs);
  const tgId = pickOne(created.tgs) || pickOne(FIXTURES.readTgIds);
  if (!lbId || !tgId) return shortLBCycle();
  // Полоса заводит СВОЙ листенер и снимает его за собой: чужие перечни держат
  // либо ещё не созданное, либо уже удалённое, и полоса, читающая их, мерила бы
  // не привязку, а промах по несуществующему ресурсу.
  const c = createListener({ lbId });
  if (c.status >= 300 || !c.opId) return 'Listener.Create';
  const op = pollOperation(c.opId, { tag: 'mix-wire-listener', maxAttempts: 30 });
  const lstId = op.ok && op.response ? op.response.id : '';
  if (!lstId) return 'Listener.Create';

  const w = wireListenerTG(lstId, tgId);
  check(w, { 'Listener wire TG 2xx': (r) => r.status < 400 });
  const u = unwireListenerTG(lstId);
  check(u, { 'Listener unwire TG 2xx': (r) => r.status < 400 });
  deleteListener(lstId);
  return 'Listener.Wire+UnwireTG';
}

// teardownAll — best-effort cleanup of resources this VU created.
// Called from scenario teardown(). Errors are swallowed; we don't want a
// flaky cleanup to mask scenario results.
export function teardownAll() {
  for (const id of created.listeners.splice(0)) {
    deleteListener(id);
  }
  for (const id of created.lbs.splice(0)) {
    deleteLB(id);
  }
  for (const id of created.tgs.splice(0)) {
    deleteTG(id);
  }
}
