// Исход мутации читается ОБЩИМ разбором, а не своим ключом — область `shared/`.
//
// ПРЕДМЕТ. Мутации Kachō отвечают `Operation`, а не ресурсом. Ответ, в котором
// операции нет, — не «выполнено синхронно», а нарушение контракта: подтвердить
// выполнение нечем. Свой ключ (`extractOperationId` → `if (id) … else …`) этой
// разницы не знает by construction — он отвечает `string | null`, и ветка
// `else` читается как успех: форма закрывается, список обновляется, оператор
// уходит уверенным, что ресурс создан.
//
// Общий разбор (`resolveMutationResponse`) отвечает ТРЕМЯ исходами — операция ·
// синхронный ответ ресурсом · нарушение контракта, — и третий отличает от
// второго по объявлению самого ресурса (`spec.mutationsReturnOperation`).
//
// ПОЧЕМУ ЭТО НЕ ВКУС, А РАСХОЖДЕНИЕ ПОЛОС. К одной и той же мутации ресурса в
// консоли ведут ДВА пути: страница (`ResourceCreatePage`/`ResourceEditPage`/
// `DeleteDialog`, они уже читают общим разбором) и модальная форма-прослойка
// (`Inline*Form`). До этой правки один и тот же ответ без операции на странице
// объявлялся нарушением контракта, а в модалке — успехом. Разницу никто не
// решал, и увидеть её можно было только сравнением полос.
//
// ПОЧЕМУ ПО УЗЛАМ СИНТАКСИСА, А НЕ ПО ТЕКСТУ. Имя `extractOperationId`
// встречается в этом же дереве в комментариях — как раз объясняя запрет;
// текстовый предикат зачёл бы объяснение за нарушение, и запрет, краснеющий на
// собственном объяснении, снимут первым же.
//
// ОБЛАСТЬ — `shared/`. У `nlb` и `storage` та же прослойка ещё живёт, и их
// де-форк идёт своими задачами (#408, #407); краснить чужую полосу отсюда
// значило бы отдать их вердикт в чужие руки. Свой модуль так же закрыл
// `compute` — `compute/src/test/mutation-outcome-read-in-common.test.ts`.
//
// САМОИСТЕЧЕНИЕ. Прослойка `extractOperationId` держится в дереве ТОЛЬКО
// оставшимися вызывающими. Когда их не останется, её место не подтверждается
// ничем — и об этом говорит последняя проба: послабление, пережившее свой
// предмет, тот же класс, что мы ловим в коде.

import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import ts from "typescript";

import { discoverApps, sourceFiles } from "./shared-symbol-sweep";

const SHARED_SRC = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const REPO_ROOT = path.resolve(SHARED_SRC, "../..");

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
    // `propertyName` — исходное имя при переименовании (`{ a as b }`): судить
    // надо то, что импортируется, а не то, как его назвали у себя.
    for (const el of bindings.elements) out.push((el.propertyName ?? el.name).text);
  }
  return out;
}

const files = sourceFiles(SHARED_SRC);
const readers = files.map((f) => ({ file: path.relative(SHARED_SRC, f), names: importedNames(f) }));
const ownKey = readers.filter((r) => r.names.includes("extractOperationId")).map((r) => r.file);
const commonRead = readers.filter((r) => r.names.includes("resolveMutationResponse")).map((r) => r.file);

/** Вызывающие прослойки ВНЕ этой области — по всем приложениям дерева. */
const outsideCallers = discoverApps(REPO_ROOT)
  .filter((app) => app !== "shared")
  .flatMap((app) =>
    sourceFiles(path.join(REPO_ROOT, app, "src"))
      .filter((f) => importedNames(f).includes("extractOperationId"))
      .map((f) => path.relative(REPO_ROOT, f)),
  );

describe("общий модуль читает исход мутации общим разбором", () => {
  it(`перепись: исходников shared ${files.length}, читают общим разбором ${commonRead.length}, зовут прослойку ${ownKey.length}; вне области вызывающих ${outsideCallers.length} [${outsideCallers.join(", ")}]`, () => {
    // «Ноль находок» обязано быть отличимо от «ноль прочитанного»: пустой обход
    // сделал бы запрет ниже вакуумно истинным. И положительный контроль — если
    // общим разбором не читает НИКТО, запрет тоже ничего не значит.
    expect(files.length).toBeGreaterThan(0);
    expect(commonRead.length).toBeGreaterThan(0);
  });

  it("своя предпосылка: разбор узнаёт именованный импорт", () => {
    // Иначе перечень имён мог бы оказаться пустым при любом входе, и обе
    // стороны утверждения зеленели бы разом.
    //
    // Имя разбираемого — МЕТКА, а не координата: исходник подаётся строкой,
    // файла за этим именем нет и на диск разбор не ходит. Поэтому оно не
    // собирается из пути: литерал внутри сборки пути читается (и гейтом
    // `uisourcereadtest`, и человеком) как обращение к модулю консоли.
    const probe = "<синтетический разбор>";
    expect(importedNames(probe, 'import { extractOperationId } from "x";')).toEqual(["extractOperationId"]);
    // Переименование при импорте — та же законная форма записи предмета.
    expect(importedNames(probe, 'import { extractOperationId as readId } from "x";')).toEqual([
      "extractOperationId",
    ]);
    // Законный близнец: то же имя в КОММЕНТАРИИ нарушением не является.
    expect(importedNames(probe, "// extractOperationId здесь звать нельзя\n")).toEqual([]);
  });

  it("ни один исходник shared не читает исход своим ключом", () => {
    expect(ownKey).toEqual([]);
  });

  it("прослойка держится вызывающими: без них её место не подтверждается ничем", () => {
    // Это НЕ повтор предыдущего: там предмет — область `shared/`, здесь —
    // право самой прослойки существовать. Ноль вызывающих во всём дереве
    // означает, что реэкспорт пережил тех, ради кого заведён, и его надо снять
    // вместе с этой пробой.
    expect(outsideCallers.length).toBeGreaterThan(0);
  });
});
