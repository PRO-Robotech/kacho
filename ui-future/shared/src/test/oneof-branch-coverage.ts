// Ветвь контракта, достижимая из создания, обязана быть выразима формой (#375).
//
// ПРЕДМЕТ. Возможность, объявленная контрактом и покрытая типами, но не имеющая
// выражения в форме, не работает НИ ПРИ КАКОМ вводе. Это хуже отсутствия: она
// задокументирована, её ищут и о ней спрашивают. Класс тихий — код собирается,
// каждая сторона по отдельности выглядит исправно, а расходятся они в третьем
// месте.
//
// ПОЧЕМУ ОБЩИЙ ХЕЛПЕР, А НЕ КОПИЯ ПРОБЫ В КАЖДОМ МОДУЛЕ. Реестр ресурсов у
// консоли не один: `shared` обслуживает vpc/iam/system, а `compute`, `nlb`,
// `registry`, `storage` несут СВОИ. Форма, которую видит пользователь, берётся
// из реестра ТОГО модуля, чей маршрут открыт, поэтому проба обязана спрашивать
// каждый реестр отдельно — иначе правка, сделанная в одном, зеленит все.
// Так и вышло с ветвями проверки живости: они были заведены в `shared`, а
// `/nlb/*` обслуживает модуль `nlb` со своим реестром, где их не было.
//
// ИСТОЧНИК ИСТИНЫ — дерево контракта. Ветви читаются из `.proto`, а не
// выписываются здесь: добавленная в контракт ветвь роняет пробу сама, не
// дожидаясь, пока кто-нибудь вспомнит про форму.

import { readFileSync } from "node:fs";
import { join, resolve } from "node:path";

import type { FormField } from "@shared/lib/form-schema";

/** Корень репозитория: cwd пробы — каталог модуля (`ui-future/<модуль>`). */
export const PROTO_ROOT = join(resolve(process.cwd(), "../.."), "proto", "kacho", "cloud");

/**
 * Ветви `oneof <name>` внутри `message <msg>` — по тексту контракта.
 *
 * Разбор нарочно узкий: ищется именно объявленное сообщение и именно поимённая
 * группа внутри него. Широкий разбор («любой oneof в файле») собрал бы ветви
 * соседних сообщений и молча раздул бы ожидание.
 *
 * Отсутствие сообщения или группы — ОТКАЗ, а не пустой список: пустой список
 * означал бы «ветвей нет» и зеленил бы всё сразу.
 */
export function oneofBranches(relPath: string, message: string, oneofName: string): string[] {
  const text = readFileSync(join(PROTO_ROOT, relPath), "utf8");
  const msgStart = text.search(new RegExp(`^\\s*message\\s+${message}\\s*\\{`, "m"));
  if (msgStart < 0) throw new Error(`в ${relPath} нет сообщения ${message} — контракт разошёлся с этой пробой`);
  // Ищется ОБЪЯВЛЕНИЕ группы, а не упоминание её имени: у этого же сообщения
  // комментарий о снятой ветви называет `oneof target` на 35 строк раньше самой
  // группы, и поиск подстрокой уводил разбор в СОСЕДНЮЮ группу — проба честно
  // сверяла цели правила с ветвями протокола и краснела не о том.
  const decl = new RegExp(`^[ \\t]*oneof[ \\t]+${oneofName}[ \\t]*\\{`, "m");
  const rest = text.slice(msgStart);
  const found = decl.exec(rest);
  if (!found) throw new Error(`в ${message} нет группы ${oneofName} — контракт разошёлся с этой пробой`);
  const oneofStart = msgStart + found.index;
  const open = text.indexOf("{", oneofStart);
  const close = text.indexOf("}", open);
  const body = text
    .slice(open + 1, close)
    .split("\n")
    .map((l) => l.replace(/\/\/.*$/, "").trim())
    .filter(Boolean);
  const branches: string[] = [];
  for (const line of body) {
    // `TcpOptions tcp = 6;` — тип, имя, номер. `option (...)` веткой не является.
    //
    // Хвост `[...]` обязателен к разбору: ветвь бывает объявлена с опцией
    // (`string subnet_id = 2 [(length) = "<=50"];`), и разбор без него молча
    // возвращал бы для такой группы ПУСТОЙ список — то есть «ветвей нет», и
    // сверка зеленела бы на любой форме.
    const m = /^[A-Za-z_][\w.]*\s+([a-z_][\w]*)\s*=\s*\d+\s*(\[[^\]]*\])?\s*;/.exec(line);
    if (m) branches.push(m[1]);
  }
  return branches;
}

/** Форма спека, которая нужна этой пробе. Не тянем весь `ResourceSpec`: у
 *  каждого модуля он свой тип, а предмет здесь — только имена полей. */
export interface FieldsCarrier {
  fields?: FormField[];
}

/**
 * Все имена полей формы — включая скрытые (они тоже уезжают в тело) и подполя
 * списков (`targets` + `instance_id` → `targets.instance_id`): ветвь внутри
 * повторяющегося поля выражается именно подполем, и без развёртки она читалась
 * бы как невыразимая.
 */
export function fieldNames(spec: FieldsCarrier | undefined): string[] {
  const out: string[] = [];
  for (const f of spec?.fields ?? []) {
    out.push(f.name);
    if (f.type === "array") {
      for (const sub of f.itemFields) out.push(`${f.name}.${sub.name}`);
    }
  }
  return out;
}

/**
 * Ветви, у которых нет ни одного поля формы под своим префиксом.
 *
 * «Выразима» означает: у формы есть поле, чьё имя ведёт в эту ветвь
 * (`health_check.http.path` → ветвь `http`). Ветвь без единого поля не выразима:
 * выбрать её пользователю нечем.
 */
export function unexpressibleBranches(spec: FieldsCarrier | undefined, prefix: string, branches: string[]): string[] {
  const names = fieldNames(spec);
  return branches.filter((b) => !names.some((n) => n === `${prefix}.${b}` || n.startsWith(`${prefix}.${b}.`)));
}
