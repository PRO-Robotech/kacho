// Чтение файла общего фундамента (`github.com/PRO-Robotech/corelib`) — для
// проб, которые сверяют консоль с объявлением, переехавшим из дерева в
// опубликованный модуль.
//
// ПРЕДМЕТ. `pkg/` монорепо был домом горизонтального кода семи сервисов; часть
// его (в т.ч. `validate/nameform`) вынесена отдельным опубликованным модулем,
// версия которого закреплена корневым `go.mod`. Свойство, которое пробы этого
// каталога стерегли, не исчезло — оно объявляется всё так же в единственном
// месте, но это место теперь не отслеживаемый файл ДЕРЕВА, а пакет МОДУЛЯ.
//
// ПОЧЕМУ ЧЕРЕЗ КЭШ МОДУЛЕЙ, А НЕ ВТОРОЙ КОПИЕЙ В КОНСОЛИ. Выписанная константа —
// второе место об одном предмете: она верна в день записи и стареет молча,
// ровно как устарели пять прежних объявлений формы имени (#715). Единственный
// способ читать ОБЪЯВЛЕНИЕ, а не его слепок, — читать туда, куда оно переехало.
//
// ПРИЁМ ЗАИМСТВОВАН у Go-стороны дерева: `internal/repohygiene/corelibsource_test.go`
// решает тот же класс тем же способом (резолвит версию `corelib`, закреплённую
// `go.mod`, и читает файлы кэша модулей), и его же кодировка пути кэша
// (`EscapeModulePath`) воспроизведена здесь дословно — не изобретается вторая.
//
// ПОЧЕМУ КАТАЛОГ КЭША СПРАШИВАЕТСЯ У `go`, А НЕ СОБИРАЕТСЯ ИЗ ПЕРЕМЕННЫХ
// ОКРУЖЕНИЯ. `GOMODCACHE` бывает не задан, и тогда значение выводится из
// `GOPATH`, а он тоже бывает не задан — та же оговорка, что у
// `internal/repohygiene/dependencylicense_test.go` (`moduleCacheDir`).
//
// ПОЧЕМУ ОТКАЗ, А НЕ ПУСТОЙ ОТВЕТ. Модуль, не извлечённый в кэш
// (`go mod download` не выполнялся), и файл, переехавший внутри модуля, — это
// РАЗНЫЕ события, но оба дают одно и то же пустое совпадение, если тут
// промолчать. Тогда «объявления не стало» и «кэш не наполнен» становятся
// неотличимы, и вызывающая проба, не сумевшая прочитать источник, обязана
// упасть сама, а не молча решить, что сравнивать не с чем.
//
// СИНТЕТИЧЕСКАЯ КООРДИНАТА. Файлу модуля даётся адрес вида
// `corelib/<пакет>/<файл>` — той же формы, что у Go-стороны. Он не совпадает ни
// с одним путём индекса git (в дереве нет каталога `corelib/`) и поэтому
// однозначно отличим от репозиторного пути в тексте отказа.

import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import path from "node:path";

/** Путь опубликованного модуля общего фундамента. Один на все пробы этого класса. */
const CORELIB_MODULE_PATH = "github.com/PRO-Robotech/corelib";

/** Префикс синтетической координаты — см. шапку файла. */
export const CORELIB_PREFIX = "corelib/";

/** Координата указывает на файл общего фундамента, а не на путь дерева? */
export function isCorelibCoordinate(coordinate: string): boolean {
  return coordinate.startsWith(CORELIB_PREFIX);
}

/** Версия `corelib`, закреплённая `require`-записью корневого `go.mod`, либо отказ. */
function corelibVersion(repoRootDir: string): string {
  const goMod = readFileSync(path.join(repoRootDir, "go.mod"), "utf8");
  const re = new RegExp(`^\\s*${CORELIB_MODULE_PATH.replace(/[.]/g, "\\.")}\\s+(\\S+)`, "m");
  const m = re.exec(goMod);
  if (!m) {
    throw new Error(
      `go.mod не закрепляет ${CORELIB_MODULE_PATH} — общий фундамент не резолвится, ` +
        `сравнивать объявление не с чем.`,
    );
  }
  return m[1];
}

/**
 * Кодировка пути модуля для кэша: заглавная буква становится восклицательным
 * знаком со строчной (та же кодировка, что у `internal/repohygiene.EscapeModulePath` —
 * правило экосистемы Go, а не наше, и второй раз оно здесь не изобретается).
 */
function escapeModulePath(p: string): string {
  return p.replace(/[A-Z]/g, (c) => `!${c.toLowerCase()}`);
}

/** Каталог кэша модулей Go — спрашивается у `go env`, см. шапку файла. */
function moduleCacheDir(): string {
  const out = execFileSync("go", ["env", "GOMODCACHE"], { encoding: "utf8" }).trim();
  if (!out) {
    throw new Error("`go env GOMODCACHE` вернул пустую строку — каталог кэша модулей не установлен.");
  }
  return out;
}

/**
 * Файл общего фундамента по синтетической координате `corelib/<пакет>/<файл>`,
 * либо отказ с названной координатой.
 *
 * Модуль, не извлечённый в кэш, и файл, переехавший внутри модуля, дают ОДИН
 * И ТОТ ЖЕ отказ чтения — различать их дальше незачем, оба означают «источник
 * недостижим», и оба обязаны остановить пробу, а не пропустить сравнение.
 */
export function readCorelibFile(repoRootDir: string, coordinate: string): string {
  if (!isCorelibCoordinate(coordinate)) {
    throw new Error(`${coordinate}: не координата общего фундамента (нет префикса "${CORELIB_PREFIX}")`);
  }
  const relInModule = coordinate.slice(CORELIB_PREFIX.length);
  const version = corelibVersion(repoRootDir);
  const full = path.join(moduleCacheDir(), `${escapeModulePath(CORELIB_MODULE_PATH)}@${version}`, relInModule);
  try {
    return readFileSync(full, "utf8");
  } catch {
    throw new Error(
      `${coordinate}: файл не прочитан (${CORELIB_MODULE_PATH}@${version}, ${full}). Модуль не извлечён ` +
        `в кэш модулей (\`go mod download\`), либо файл переехал внутри модуля. Сравнивать не с чем, и ` +
        `молчание здесь означало бы «совпало», чего никто не проверял.`,
    );
  }
}
