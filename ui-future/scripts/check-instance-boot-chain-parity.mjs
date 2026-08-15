#!/usr/bin/env node
// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

/**
 * Гейт цепочки загрузки: КАЖДАЯ копия формы машины даёт выбрать образ и ключи
 * входа списком.
 *
 * Предмет (#377). Из пустого проекта нельзя было дойти до машины: том создавался
 * пустым либо из снимка, снимок — из тома, образ — из тома или снимка, и круг
 * замыкался сам на себя. Форма машины при этом принимала образ СВОБОДНОЙ СТРОКОЙ,
 * а ключей входа в консоли не было вовсе, хотя ресурс есть на сервере и в
 * провайдере инфраструктуры.
 *
 * Почему гейт, а не две пробы. Форма машины объявлена в ДВУХ реестрах с разным
 * составом полей. Пробы уровня модуля закрепляют утверждение внутри своей копии,
 * но ни одна из них не знает, сколько копий вообще есть: третья, заведённая
 * завтра, не покраснит ни одну — и появится третье расхождение, которое эта
 * задача и запрещает оставлять. Перепись копий обязана выводиться ИЗ ДЕРЕВА.
 *
 * Что проверяется в каждой найденной копии:
 *   - поле `boot_source.id`   — тип `ref`, цель `images`;
 *   - поле `guest_access_key_ids` — тип `array`, вложенный элемент `ref` на
 *     `guest-access-keys`;
 *   - цели ссылок объявлены в ТОМ ЖЕ реестре: без них список не из чего собрать.
 *
 * Разбор — AST, не текст. Регулярное выражение нашло бы эти имена в комментарии,
 * который эту же проверку и объясняет (в том числе в шапке этого файла), и
 * осталось бы зелёным при снятом поле.
 *
 * ОПРОВЕРГНУТАЯ ГИПОТЕЗА (записана, чтобы её не повторили). Первая редакция
 * считала копией формы всякий реестр, объявляющий спеку «compute-instances», и
 * назвала таких ТРИ. Третья — `nlb/src/lib/resource-registry.tsx` — формы не
 * несёт вовсе: это цель ссылки для подборщика целей балансировщика
 * (`ops.create: false`, поля отсутствуют). Требовать от неё полей значило бы
 * ловить форму объявления, а не предмет, и первый же ложный срабат отключил бы
 * гейт. Предикат копии — `ops.create === true`: путь пользователя, на котором
 * машину ЗАКАЗЫВАЮТ. Реестр-цель ссылки остаётся законным близнецом, на котором
 * гейт обязан молчать.
 *
 * Проверка СВОЕЙ предпосылки. Гейт обоснован тем, что копии формы существуют.
 * Ноль найденных копий — ПАДЕНИЕ, а не тихое «ноль находок»: иначе он переживёт
 * свой предмет (переименовали id спеки, переехал каталог) и станет зелёным
 * навсегда. Объём осмотренного печатается, чтобы «ноль находок» было отличимо от
 * «ноль прочитанного».
 *
 * Единица счёта — отслеживаемый git-элемент (`git ls-files`), а не то, что лежит
 * на диске: иначе объявление и поведение разъедутся молча через .gitignore.
 *
 * Запуск из ui-future/:  node scripts/check-instance-boot-chain-parity.mjs
 * Выход ненулевой — есть находки.
 */

import { execFileSync } from "node:child_process";
import fs from "node:fs";

import ts from "typescript";

/** Идентификатор спеки, чью форму мы стережём. */
const SPEC_ID = "compute-instances";

/**
 * Требования к полям формы. Ключ — имя поля, значение — что оно обязано
 * утверждать. `inner` описывает элемент массива.
 */
const REQUIRED_FIELDS = {
  "boot_source.id": { type: "ref", refResource: "images" },
  guest_access_key_ids: { type: "array", inner: { type: "ref", refResource: "guest-access-keys" } },
};

/** Цели ссылок, без которых список не из чего собрать. */
const REQUIRED_REF_TARGETS = ["images", "guest-access-keys"];

function trackedRegistryFiles() {
  const out = execFileSync("git", ["ls-files", "*/src/lib/resource-registry.tsx"], { encoding: "utf8" });
  return out.split("\n").filter(Boolean);
}

function parse(file) {
  return ts.createSourceFile(file, fs.readFileSync(file, "utf8"), ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
}

/** Имя свойства объектного литерала как строка (`foo` / `"foo"` / `'foo'`). */
function propName(prop) {
  const n = prop.name;
  if (!n) return undefined;
  if (ts.isIdentifier(n)) return n.text;
  if (ts.isStringLiteralLike(n)) return n.text;
  return undefined;
}

function stringProp(objLit, name) {
  for (const p of objLit.properties) {
    if (!ts.isPropertyAssignment(p)) continue;
    if (propName(p) !== name) continue;
    return ts.isStringLiteralLike(p.initializer) ? p.initializer.text : undefined;
  }
  return undefined;
}

/** `ops: { create: true, … }` — спека предлагает заказать ресурс, то есть несёт форму. */
function offersCreate(specLit) {
  const ops = propInitializer(specLit, "ops");
  if (!ops || !ts.isObjectLiteralExpression(ops)) return false;
  for (const p of ops.properties) {
    if (!ts.isPropertyAssignment(p)) continue;
    if (propName(p) !== "create") continue;
    return p.initializer.kind === ts.SyntaxKind.TrueKeyword;
  }
  return false;
}

function propInitializer(objLit, name) {
  for (const p of objLit.properties) {
    if (!ts.isPropertyAssignment(p)) continue;
    if (propName(p) === name) return p.initializer;
  }
  return undefined;
}

/** Все объектные литералы верхнего уровня REGISTRY, ключённые по id спеки. */
function specLiterals(sf) {
  const specs = new Map();
  const visit = (node) => {
    if (ts.isVariableDeclaration(node) && ts.isIdentifier(node.name) && node.name.text === "REGISTRY") {
      const init = node.initializer;
      const obj = init && ts.isAsExpression(init) ? init.expression : init;
      if (obj && ts.isObjectLiteralExpression(obj)) {
        for (const p of obj.properties) {
          if (!ts.isPropertyAssignment(p)) continue;
          const key = propName(p);
          if (key && ts.isObjectLiteralExpression(p.initializer)) specs.set(key, p.initializer);
        }
      }
    }
    ts.forEachChild(node, visit);
  };
  visit(sf);
  return specs;
}

/** Поля формы спеки: имя → объектный литерал поля. */
function formFields(specLit) {
  const fields = propInitializer(specLit, "fields");
  const byName = new Map();
  if (!fields || !ts.isArrayLiteralExpression(fields)) return byName;
  for (const el of fields.elements) {
    if (!ts.isObjectLiteralExpression(el)) continue;
    const name = stringProp(el, "name");
    if (name) byName.set(name, el);
  }
  return byName;
}

function checkField(findings, file, fieldName, want, lit) {
  if (!lit) {
    findings.push(`${file}: спека «${SPEC_ID}» не объявляет поля «${fieldName}»`);
    return;
  }
  const type = stringProp(lit, "type");
  if (type !== want.type) {
    findings.push(`${file}: поле «${fieldName}» объявлено типом «${type ?? "—"}», ожидается «${want.type}»`);
    return;
  }
  if (want.refResource) {
    const ref = stringProp(lit, "refResource");
    if (ref !== want.refResource) {
      findings.push(`${file}: поле «${fieldName}» ссылается на «${ref ?? "—"}», ожидается «${want.refResource}»`);
    }
  }
  if (want.inner) {
    const items = propInitializer(lit, "itemFields");
    const first =
      items && ts.isArrayLiteralExpression(items) && items.elements.length > 0 && ts.isObjectLiteralExpression(items.elements[0])
        ? items.elements[0]
        : undefined;
    checkField(findings, file, `${fieldName}[]`, want.inner, first);
  }
}

function main() {
  const files = trackedRegistryFiles();
  const findings = [];
  const carriers = [];
  const refOnly = [];

  for (const file of files) {
    const specs = specLiterals(parse(file));
    const instanceSpec = specs.get(SPEC_ID);
    if (!instanceSpec) continue; // реестр без спеки машины — не предмет этого гейта
    if (!offersCreate(instanceSpec)) {
      // Цель ссылки, а не путь заказа: формы нет by construction.
      refOnly.push(file);
      continue;
    }
    carriers.push(file);

    const fields = formFields(instanceSpec);
    for (const [fieldName, want] of Object.entries(REQUIRED_FIELDS)) {
      checkField(findings, file, fieldName, want, fields.get(fieldName));
    }
    for (const target of REQUIRED_REF_TARGETS) {
      if (!specs.has(target)) {
        findings.push(`${file}: цель ссылки «${target}» не объявлена в этом реестре — список не из чего собрать`);
      }
    }
  }

  console.log(
    `осмотрено реестров: ${files.length}; несут форму заказа машины «${SPEC_ID}»: ${carriers.length}` +
      (carriers.length ? ` (${carriers.join(", ")})` : "") +
      `; объявляют её только целью ссылки: ${refOnly.length}` +
      (refOnly.length ? ` (${refOnly.join(", ")})` : ""),
  );

  // Предпосылка гейта: копии формы существуют. Ноль — падение, а не «чисто».
  if (carriers.length === 0) {
    console.error(
      `ПРЕДПОСЫЛКА НЕ ВЫПОЛНЕНА: ни один отслеживаемый реестр не предлагает создать «${SPEC_ID}». ` +
        "Гейт не осмотрел ничего и не вправе отчитаться чистым.",
    );
    process.exit(1);
  }

  if (findings.length > 0) {
    console.error(`\nнаходок: ${findings.length}`);
    for (const f of findings) console.error(`  - ${f}`);
    console.error(
      "\nФорма машины объявлена не в одном месте. Возможность, доехавшая до одной копии, " +
        "оставляет второй путь пользователя без неё — и разницу он читает как другое место продукта.",
    );
    process.exit(1);
  }

  console.log("находок нет: каждая копия формы машины даёт выбрать образ и ключи входа списком");
}

main();
