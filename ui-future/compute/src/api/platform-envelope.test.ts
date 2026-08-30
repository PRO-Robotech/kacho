// Типы ПРОВОДА платформы объявлены один раз — и объявляет их не модуль.
//
// ПРЕДМЕТ. Конверт операции (`Operation`/`OperationList`) и дескриптор
// зависимости (`Referrer`) — форма платформы, а не домена: их несёт КАЖДЫЙ
// сервис, и меняются они вместе с контрактом платформы. Пока модуль объявляет
// их СВОЕЙ копией, поле, добавленное в общий конверт, до этого домена не
// доезжает вовсе. Класс тихий by construction: копия компилируется, структурно
// совпадает и потому молчит — расхождение начинается в тот день, когда общий
// конверт что-нибудь приобретает, и первым его увидит не разработчик, а
// пользователь.
//
// ПОЧЕМУ ЭТА ПРОБА ВООБЩЕ ПОНАДОБИЛАСЬ. Гейт дерева
// `shared-organisms-single-source` узнаёт объявления по формам ЗНАЧЕНИЯ
// (`export function|const|let|var|class`) и типовых форм не знает — копия из
// одних `interface` ему невидима by construction. Ведомость форков это и
// показывала: у записи `compute/src/api/types.ts` перечень символов ПУСТ, хотя
// конверт был объявлен в ней целиком. Распознаватель типовых объявлений заведён
// рядом с ним (`declaredTypeSymbols`), а перевод самого гейта дерева на него —
// отдельный предмет: он меняет вердикт по шести модулям сразу.
//
// ОБЛАСТЬ. Судится ровно поверхность `shared/src/api/types.ts`. Доменные типы
// модуля (`Instance`, `MachineType`, `BootSource`, `EffectiveResources`) общим
// не объявлены, под правило не подпадают, и списка исключений у него нет вовсе:
// послаблению нечего было бы истекать.

import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { declaredSymbols, declaredTypeSymbols, sourceFiles } from "@shared/test/shared-symbol-sweep";

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");
const PLATFORM_TYPES = path.join(repoRoot, "shared/src/api/types.ts");
const MODULE_SRC = path.join(repoRoot, "compute/src");

/** Все объявленные символы исходника — и значения, и типы. */
function allDeclared(src: string): Set<string> {
  return new Set([...declaredSymbols(src), ...declaredTypeSymbols(src)]);
}

const platformSymbols = allDeclared(readFileSync(PLATFORM_TYPES, "utf8"));
const moduleFiles = sourceFiles(MODULE_SRC);
const redeclared = moduleFiles
  .map((file) => ({
    file: path.relative(repoRoot, file),
    symbols: [...allDeclared(readFileSync(file, "utf8"))].filter((s) => platformSymbols.has(s)).sort(),
  }))
  .filter((hit) => hit.symbols.length > 0);

describe("типы провода платформы объявлены один раз", () => {
  it(`перепись: файлов модуля ${moduleFiles.length}, символов платформы ${platformSymbols.size}`, () => {
    // «Ноль находок» обязано быть отличимо от «ноль прочитанного»: сдвинутый
    // корень или переименованная раскладка иначе делают утверждение ниже
    // вакуумно истинным, не покраснев ни на чём.
    expect(moduleFiles.length).toBeGreaterThan(0);
    expect(platformSymbols.size).toBeGreaterThan(0);
  });

  it("своя предпосылка: распознаватель узнаёт конверт в ОБЩЕМ исходнике", () => {
    // Предикат обоснован фактом о дереве — что конверт объявлен ИМЕННО такой
    // формой. Переедет он на другую (класс, тип-алиас, генерация) — всплывёт
    // здесь, а не превратит утверждение ниже в тихий no-op.
    expect([...platformSymbols]).toEqual(expect.arrayContaining(["Operation", "OperationList", "Referrer"]));
  });

  it("модуль не объявляет заново ни один символ платформы", () => {
    expect(redeclared.map((h) => `${h.file}: ${h.symbols.join(", ")}`)).toEqual([]);
  });

  // Законный близнец. Без него отрицание выше зеленело бы и на правке, которая
  // просто перестала читать исходники модуля: «ноль находок» получилось бы из
  // пустого обхода, а не из отсутствия копий.
  it("доменные типы модуля правилом НЕ задеты", () => {
    const own = allDeclared(readFileSync(path.join(MODULE_SRC, "api/types.ts"), "utf8"));
    expect(own.size).toBeGreaterThan(2);
    for (const domainType of ["Instance", "MachineType", "BootSource", "EffectiveResources"]) {
      expect(own.has(domainType)).toBe(true);
      expect(platformSymbols.has(domainType)).toBe(false);
    }
  });
});
