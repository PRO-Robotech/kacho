// Исход мутации читается ОБЩИМ разбором, а не своим ключом.
//
// ПРЕДМЕТ. Мутации Kachō отвечают `Operation`, а не ресурсом. Ответ, в котором
// операции нет, — не «выполнено синхронно», а нарушение контракта: подтвердить
// выполнение нечем. Свой ключ (`extractOperationId` → `if (id) … else …`) этой
// разницы не знает by construction — он отвечает `string | null`, и ветка
// `else` читается как успех. Оператор видит обновившийся список и уходит в
// уверенности, что машина запущена.
//
// Общий разбор (`resolveMutationResponse`) отвечает ТРЕМЯ исходами — операция ·
// синхронный ответ ресурсом · нарушение контракта, — и третий отличает от
// второго по объявлению самого ресурса. Именно поэтому здесь судится ИМПОРТ, а
// не текст: `extractOperationId` объявлен исторической прослойкой над тем же
// чтением, поэтому «зовёт ли кто-то её» и есть предмет — вернувшийся импорт
// возвращает и потерю третьего исхода.
//
// ПОЧЕМУ ПО УЗЛАМ СИНТАКСИСА. Имя `extractOperationId` встречается в этом же
// дереве в комментариях — как раз объясняя запрет; текстовый предикат зачёл бы
// объяснение за нарушение, и запрет, краснеющий на собственном объяснении,
// снимут первым же.
//
// ОБЛАСТЬ — этот модуль. У соседей та же прослойка ещё живёт, и их де-форк идёт
// своими задачами; краснить чужую полосу отсюда значило бы отдать вердикт этого
// модуля в чужие руки.

import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import ts from "typescript";

import { sourceFiles } from "@shared/test/shared-symbol-sweep";

const MODULE_SRC = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

/** Имена, ИМПОРТИРОВАННЫЕ файлом (именованные привязки). */
function importedNames(file: string, source?: string): string[] {
  const sf = ts.createSourceFile(
    file,
    source ?? readFileSync(file, "utf8"),
    ts.ScriptTarget.ESNext,
    true,
    ts.ScriptKind.TSX,
  );
  const out: string[] = [];
  for (const st of sf.statements) {
    if (!ts.isImportDeclaration(st)) continue;
    const bindings = st.importClause?.namedBindings;
    if (bindings === undefined || !ts.isNamedImports(bindings)) continue;
    for (const el of bindings.elements) out.push(el.name.text);
  }
  return out;
}

const files = sourceFiles(MODULE_SRC);
const readers = files.map((f) => ({ file: path.relative(MODULE_SRC, f), names: importedNames(f) }));
const ownKey = readers.filter((r) => r.names.includes("extractOperationId")).map((r) => r.file);
const commonRead = readers.filter((r) => r.names.includes("resolveMutationResponse")).map((r) => r.file);

describe("модуль читает исход мутации общим разбором", () => {
  it(`перепись: исходников ${files.length}, читают общим разбором ${commonRead.length}`, () => {
    // «Ноль находок» обязано быть отличимо от «ноль прочитанного»: пустой обход
    // сделал бы запрет ниже вакуумно истинным. И положительный контроль — если
    // общим разбором не читает НИКТО, запрет тоже ничего не значит.
    expect(files.length).toBeGreaterThan(0);
    expect(commonRead.length).toBeGreaterThan(0);
  });

  it("своя предпосылка: разбор узнаёт именованный импорт", () => {
    // Иначе перечень имён мог бы оказаться пустым при любом входе, и обе
    // стороны утверждения зеленели бы разом.
    const probe = importedNames(path.join(MODULE_SRC, "probe.ts"), 'import { extractOperationId } from "x";');
    expect(probe).toEqual(["extractOperationId"]);
    // Законный близнец: то же имя в КОММЕНТАРИИ нарушением не является.
    expect(importedNames(path.join(MODULE_SRC, "probe.ts"), "// extractOperationId здесь звать нельзя\n")).toEqual([]);
  });

  it("ни один исходник не читает исход своим ключом", () => {
    expect(ownKey).toEqual([]);
  });
});
