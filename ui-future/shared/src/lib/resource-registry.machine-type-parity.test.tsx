// Размер машины, объявленный контрактом, показан каталогом — весь.
//
// ПРЕДМЕТ. `MachineType.effective_resources` — то единственное, ради чего
// арендатор читает каталог типов: он выбирает размер. Поле, которое контракт
// объявляет, а каталог не показывает, — это возможность, объявленная и
// невидимая: она задокументирована, покрыта типами, приезжает в каждом ответе,
// и человек о ней не узнаёт никогда. Тихо это потому, что обе стороны по
// отдельности исправны — расходятся они в третьем месте, в том, что видит
// арендатор.
//
// НАБЛЮДАЛОСЬ: модель ускорителя (`gpu_type`) показывал ТОЛЬКО форк реестра у
// модуля compute; общий реестр — а его и рисует раздел `/vpc/*` — этой колонки
// не имел вовсе. То есть одна и та же запись каталога выглядела по-разному в
// двух местах продукта, и правка одного места до другого не доезжала. Колонка
// перенесена в общий реестр вместе со сведением форка (#406).
//
// ИСТОЧНИК ИСТИНЫ — дерево контракта, а не перечень в этом файле: поле,
// добавленное в `EffectiveResources`, роняет пробу само, не дожидаясь, пока
// кто-нибудь вспомнит про колонку. Исходов у такого падения три и все законные
// (`api-conventions.md` §«Принято-и-проигнорировано»): показать · снять с
// контракта · завести здесь запись с причиной, почему показывать не надо.
// Списка исключений сегодня НЕТ, потому что исключать нечего: показаны все
// четыре поля. Заводить пустой список ради формы нельзя — послаблению нечего
// было бы истекать.
//
// Проверка ДВУСТОРОННЯЯ. Обратное направление («колонка называет поле, которого
// в контракте нет») ловит колонку, пережившую своё поле: она показывает пустоту
// на каждой строке и выглядит как «сервис не заполняет», а не как ошибка
// консоли.

import { readFileSync } from "node:fs";
import { join } from "node:path";

import { REGISTRY } from "./resource-registry";
import { PROTO_ROOT } from "@shared/test/oneof-branch-coverage";

const CONTRACT = "compute/v1/machine_type.proto";
const MESSAGE = "EffectiveResources";
const PREFIX = "effective_resources.";

/**
 * Имена полей верхнего уровня объявленного сообщения — по тексту контракта.
 *
 * Отсутствие сообщения — ОТКАЗ, а не пустой перечень: пустой означал бы «полей
 * нет» и зеленил бы каталог любой бедности.
 */
function messageFields(relPath: string, message: string): string[] {
  const text = readFileSync(join(PROTO_ROOT, relPath), "utf8");
  const at = text.search(new RegExp(`^\\s*message\\s+${message}\\s*\\{`, "m"));
  if (at < 0) throw new Error(`в ${relPath} нет сообщения ${message} — контракт разошёлся с этой пробой`);
  const open = text.indexOf("{", at);
  let depth = 0;
  let close = open;
  for (let i = open; i < text.length; i += 1) {
    if (text[i] === "{") depth += 1;
    else if (text[i] === "}") {
      depth -= 1;
      if (depth === 0) {
        close = i;
        break;
      }
    }
  }
  const names: string[] = [];
  for (const raw of text.slice(open + 1, close).split("\n")) {
    const line = raw.replace(/\/\/.*$/, "").trim();
    if (!line) continue;
    const m = /^(?:repeated\s+)?(?:map<[^>]+>|[A-Za-z_][\w.]*)\s+([a-z_]\w*)\s*=\s*\d+\s*(?:\[[^\]]*\])?\s*;/.exec(line);
    if (m) names.push(m[1]);
  }
  if (names.length === 0) throw new Error(`тело ${message} прочитано пустым — разбор разошёлся с контрактом`);
  return names;
}

const contractFields = messageFields(CONTRACT, MESSAGE);
const columns = REGISTRY["machine-types"].columns;
const shownFields = columns
  .map((c) => c.path ?? "")
  .filter((p) => p.startsWith(PREFIX))
  .map((p) => p.slice(PREFIX.length))
  .sort();

describe("каталог типов машин показывает размер, объявленный контрактом", () => {
  it(`перепись: полей контракта ${contractFields.length}, колонок каталога ${columns.length}, из них про размер ${shownFields.length}`, () => {
    // «Ноль находок» обязано быть отличимо от «ноль прочитанного»: пустой разбор
    // контракта либо пустой перечень колонок сделал бы оба утверждения ниже
    // вакуумно истинными.
    expect(contractFields.length).toBeGreaterThan(0);
    expect(columns.length).toBeGreaterThan(0);
    expect(shownFields.length).toBeGreaterThan(0);
  });

  it("своя предпосылка: разбор узнаёт поля именно этого сообщения", () => {
    // Переедет `EffectiveResources` на другую форму записи — всплывёт здесь, а
    // не превратит утверждения ниже в тихий no-op на пустом множестве.
    expect(contractFields).toEqual(expect.arrayContaining(["v_cpu", "memory_mib"]));
  });

  it("каждое поле размера показано колонкой", () => {
    const invisible = contractFields.filter((f) => !shownFields.includes(f));
    expect(invisible).toEqual([]);
  });

  it("ни одна колонка не называет поля, которого в контракте нет", () => {
    // Колонка, пережившая своё поле, показывает пустоту на каждой строке — со
    // стороны это читается как «сервис не заполняет», а не как ошибка консоли.
    const orphan = shownFields.filter((f) => !contractFields.includes(f));
    expect(orphan).toEqual([]);
  });
});
