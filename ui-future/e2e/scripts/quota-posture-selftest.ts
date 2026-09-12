// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

/**
 * САМОПРОВЕРКА РЕШЕНИЯ О ПОСАДКЕ ДОМЕНА ВЕЛИЧИН.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧТО ЗДЕСЬ ДОКАЗЫВАЕТСЯ И ПОЧЕМУ ЭТОГО НЕЛЬЗЯ ПРОЧИТАТЬ ГЛАЗАМИ
 *
 * Пробы витрины квот перестали утверждать успех безусловно: посадок стало две,
 * и каждая законна (#2515, объявлены #2596). Ветвление по посадке — самый
 * дешёвый способ получить пробу, которая зеленеет ВСЕГДА: достаточно, чтобы
 * решение относило к «домена величин нет» любой неуспех подряд. Такая проба
 * читается ровно так же убедительно, как работающая, и молчит на мёртвом крае.
 *
 * Поэтому решению подаются входы — по одному на каждый способ ошибиться — и
 * сверяется, что исходы ВЫШЛИ РАЗНЫЕ.
 *
 * Прогон гоняется голым `node`, без зависимостей и без стенда: секунды, и потому
 * ДО подъёма — как соседние самопроверки отображения имени и разбора отказа
 * потока. Проверять решение подъёмом кластера значило бы не проверять его
 * никогда: стенд поднимается в ОДНОЙ посадке, а решение обязано различать обе.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧЕГО ЭТА САМОПРОВЕРКА НЕ ДОКАЗЫВАЕТ — СКАЗАНО ВСЛУХ
 *
 * Она судит РЕШЕНИЕ, а не чтение края и не отрисовку. Что страница показывает
 * на каждой посадке, утверждают сами пробы браузером; что край производит
 * признак — пробы владельцев на их стороне. Границу видно по импорту: здесь
 * только чистый модуль решения, ни браузера, ни сети.
 */

import {
  QUOTA_AUTHORITY_ABSENT,
  QUOTA_OWNER_PATHS,
  type OwnerAnswer,
  postureOf,
  reasonFromBody,
} from "../specs/quota-posture.ts";

let failed = 0;
function check(ok: boolean, what: string, got?: string): void {
  if (ok) {
    console.log(`  ОК  ${what}`);
    return;
  }
  failed += 1;
  console.error(`  ПРОВАЛ  ${what}${got === undefined ? "" : ` (получили: ${got})`}`);
}

/** Тело отказа края в том виде, в каком его собирает `google.rpc.Status`. */
function refusalBody(reason: string): string {
  return JSON.stringify({
    code: 9,
    message: "resource count limits are not stated in this installation",
    details: [{ "@type": "type.googleapis.com/google.rpc.ErrorInfo", reason, domain: "vpc.kacho.cloud" }],
  });
}

/** Пятеро владельцев, ответивших успехом. */
function allAnswered(): OwnerAnswer[] {
  return QUOTA_OWNER_PATHS.map((path) => ({
    path,
    answered: true,
    status: 200,
    reason: "",
    kinds: ["vpc.network"],
  }));
}

/** Пятеро владельцев на посадке без домена величин. */
function allAbsent(): OwnerAnswer[] {
  return QUOTA_OWNER_PATHS.map((path) => ({
    path,
    answered: true,
    status: 400,
    reason: QUOTA_AUTHORITY_ABSENT,
    kinds: [],
  }));
}

// ── КОНТРОЛЬ: обе посадки узнаются, и они РАЗНЫЕ ────────────────────────────
{
  check(postureOf(allAnswered()).kind === "deployed", "пятеро ответили успехом — посадка «домен развёрнут»");
  check(postureOf(allAbsent()).kind === "absent", "пятеро назвали признак отсутствия — посадка «домена нет»");
  check(
    postureOf(allAnswered()).kind !== postureOf(allAbsent()).kind,
    "две посадки дают РАЗНЫЙ исход — иначе ветвление вакуумно",
  );
}

// ── ИНЪЕКЦИЯ НОВОГО СВОЙСТВА: пятеро обязаны СОГЛАСОВАТЬСЯ ──────────────────
//
// Посадка одна на установку. Разнобой — находка, а не посадка, и он обязан
// краснеть ОТДЕЛЬНЫМ исходом, а не приводиться к одной из двух.
{
  const otherReason = allAbsent();
  otherReason[2] = { ...otherReason[2], reason: "QUOTA_NOT_PROVISIONED" };
  const p = postureOf(otherReason);
  check(p.kind === "split", "владелец отказал ДРУГИМ признаком — разнобой, а не посадка", p.kind);
  check(
    p.kind === "split" && p.detail.includes(QUOTA_OWNER_PATHS[2]),
    "разбор называет КООРДИНАТУ — путь разошедшегося владельца",
    p.kind === "split" ? p.detail : p.kind,
  );

  const silent = allAbsent();
  silent[4] = { ...silent[4], answered: false, status: 0, reason: "" };
  check(postureOf(silent).kind === "split", "владелец не спрошен вовсе — посадка НЕ устанавливается");

  const mixed = allAnswered();
  mixed[0] = { ...mixed[0], status: 400, reason: QUOTA_AUTHORITY_ABSENT, kinds: [] };
  check(postureOf(mixed).kind === "split", "часть отдала пределы, часть объявила отсутствие — разнобой");

  check(postureOf([]).kind === "split", "владельцев не спрошено ни одного — посадка НЕ устанавливается");
}

// ── ЗАКОННЫЙ БЛИЗНЕЦ: тот же путь, отличается РОВНО ОДНИМ фактом ────────────
//
// Без него краснота выше доказывала бы лишь то, что решение вообще умеет
// краснеть, — но не то, что краснеет оно на РАЗНОБОЕ. Здесь тот же пятый
// владелец, тот же статус, отличается только признак: он тот самый.
{
  const twin = allAbsent();
  twin[2] = { ...twin[2], reason: QUOTA_AUTHORITY_ABSENT };
  check(postureOf(twin).kind === "absent", "тот же владелец с ТЕМ ЖЕ признаком — решение молчит");
}

// ── ИНЪЕКЦИЯ САМОГО ОПАСНОГО ВХОДА: МЁРТВЫЙ КРАЙ ────────────────────────────
//
// Это главная проба файла. Отказ БЕЗ машинного признака — сбой, а не посадка:
// прочитав его как «потолков нет», проба зеленела бы на полностью сломанной
// витрине, и ровно этого ждут от ветвления по посадке.
{
  const noReason = allAbsent().map((a) => ({ ...a, reason: "" }));
  check(postureOf(noReason).kind === "split", "отказ БЕЗ признака — сбой, а не посадка «домена нет»");

  const serverError = allAbsent().map((a) => ({ ...a, status: 500, reason: "" }));
  check(postureOf(serverError).kind === "split", "пятеро отдали 500 — сбой, а не посадка");

  const unauth = allAbsent().map((a) => ({ ...a, status: 403, reason: "AUTHZ_DENIED" }));
  check(postureOf(unauth).kind === "split", "отказ в доступе посадкой не считается");
}

// ── РАЗБОР ТЕЛА: признак берётся из тела, и пусто значит ПУСТО ──────────────
{
  check(reasonFromBody(refusalBody(QUOTA_AUTHORITY_ABSENT)) === QUOTA_AUTHORITY_ABSENT, "признак прочитан из тела");
  check(reasonFromBody("") === "", "пустое тело — признака нет");
  check(reasonFromBody("<html>502 Bad Gateway</html>") === "", "не-JSON — признака нет, а не догадка");
  check(reasonFromBody(JSON.stringify({ code: 9, message: "no details" })) === "", "тело без разбора — признака нет");
  check(
    reasonFromBody(JSON.stringify({ details: [{}, { reason: QUOTA_AUTHORITY_ABSENT }] })) === QUOTA_AUTHORITY_ABSENT,
    "признак найден не в первой записи разбора",
  );
  // Проза НЕ является признаком: тон отказа контрактен и меняется осознанно.
  check(
    reasonFromBody(JSON.stringify({ message: "no limit authority is deployed" })) === "",
    "английская проза признаком НЕ считается — иначе правка тона вернула бы пустоту",
  );
}

// ── ПЕРЕПИСЬ: «ноль провалов» отличимо от «ноль прочитанного» ───────────────
const RUNS = 5;
if (QUOTA_OWNER_PATHS.length !== 5) {
  console.error(`владельцев пределов ${QUOTA_OWNER_PATHS.length}, а не 5 — входы самопроверки стали не теми`);
  process.exit(1);
}
if (failed > 0) {
  console.error(`\nсамопроверка решения о посадке: провалов ${failed}`);
  process.exit(1);
}
console.log(
  `\nсамопроверка решения о посадке: все утверждения прошли ` +
    `(сцен ${RUNS}, владельцев в каждой ${QUOTA_OWNER_PATHS.length})`,
);
