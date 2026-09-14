// Чтение САМОГО контракта — общий механизм для проб, сверяющих консоль с `.proto`.
//
// Заведён переиспользованием: разбор `message` жил внутри
// `RoutesPanel.contract.test.ts` и был написан ради одного сообщения. Второй
// пробе, сверяющей состав черновиков по всему дереву, нужен тот же разбор —
// вторая копия разошлась бы с первой ровно так же молча, как черновик разошёлся
// с контрактом.
//
// Читается ОРИГИНАЛ, а не его отражение в коде консоли: отражение — и есть то,
// что проверяется.
//
// КОРЕНЬ ДЕРЕВА КОНТРАКТОВ — НЕ ОБЯЗАТЕЛЬНО КАТАЛОГ ЭТОГО РЕПОЗИТОРИЯ. Платформа
// называет себя `kacho` и несёт свои контракты в `proto/`; служба доступа
// (`kaname`) свои унесла в собственный репозиторий (kacho#2616, исход C,
// 2026-09-13) и публикует их модулем Go. Обе разновидности разрешает один
// contractPath() — по наличию корня в дереве, а не по имени, — поэтому вызывающий
// называет путь ВНУТРИ дерева контрактов и о переезде не знает.

import { execFileSync } from "node:child_process";
import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import { dirname, join, relative, resolve, sep } from "node:path";

/** Поле сообщения контракта. */
export interface ProtoField {
  name: string;
  /**
   * Из чего синтезируется значение поля: `string` и `map` — то, что проба умеет
   * подставить дословно; `other` — всё прочее (вложенное сообщение, enum,
   * число), для круга «загрузили → сохранили» такие поля не синтезируются.
   */
  kind: "string" | "map" | "other";
  /** Имя `oneof`, если поле — его ветвь; иначе null. */
  oneof: string | null;
  /** Объявлено ли поле `repeated`. */
  repeated: boolean;
  /** Тип поля как он записан в контракте (`string`, `StaticRoute`, `map`, …). */
  type: string;
}

const SCALARS = new Set([
  "double",
  "float",
  "int32",
  "int64",
  "uint32",
  "uint64",
  "sint32",
  "sint64",
  "fixed32",
  "fixed64",
  "sfixed32",
  "sfixed64",
  "bool",
  "string",
  "bytes",
]);

/** Каталог контрактов относительно корня монорепо. */
const PROTO_DIR = join("proto", "kacho", "cloud");

/**
 * Корень монорепо ищется ПОДЪЁМОМ, а не считается от известной глубины: пробы
 * shared исполняются из `ui-future/<модуль>`, и модулей таких несколько.
 * Промах по глубине дал бы «файл не найден» там, где файл есть.
 */
export function monorepoRoot(): string {
  let dir = resolve(process.cwd());
  for (;;) {
    if (existsSync(join(dir, PROTO_DIR))) return dir;
    const parent = dirname(dir);
    if (parent === dir) break;
    dir = parent;
  }
  throw new Error(
    `не найден каталог контрактов ${PROTO_DIR} подъёмом от ${process.cwd()} — проба не прочитала ничего и не вправе молчать`,
  );
}

/**
 * Где объявлено, какой корень дерева контрактов каким модулем публикуется.
 *
 * ПЕРЕЧЕНЬ ЧИТАЕТСЯ У ПРОИЗВОДИТЕЛЯ, А НЕ КОПИРУЕТСЯ СЮДА. Копия была бы очередным
 * объявлением одного предмета: первое — `ExternalRootModules` пакета
 * `internal/contractsource`, второе — массив `KACHO_PROTO_ROOT_MODULES` оболочки
 * (`gateway/scripts/lib/stage-proto-tree.sh`), и расхождение ТЕХ ДВУХ держит гейт
 * (`internal/repohygiene` TestContractRootModulesAgreeBetweenGoAndShell). Гейта на
 * копию в консоли нет, поэтому она расходилась бы МОЛЧА — тем же способом, каким
 * устарели пять прежних объявлений формы имени (#715, см. `corelib-source.ts`).
 */
const EXTERNAL_ROOTS_DECL = join("internal", "contractsource", "contractsource.go");

/** {корень: путь модуля} — читается объявление Go-стороны; ноль записей — отказ. */
function externalRootModules(root: string): Map<string, string> {
  const declAt = join(root, EXTERNAL_ROOTS_DECL);
  let src: string;
  try {
    src = readFileSync(declAt, "utf8");
  } catch (err) {
    throw new Error(
      `объявление внешних корней контрактов не прочитано (${EXTERNAL_ROOTS_DECL}): ${String(err)} — ` +
        `узнать, каким модулем приезжает корень, стало нечем`,
    );
  }
  const block = /var ExternalRootModules = map\[string\]string\{([\s\S]*?)\n\}/.exec(src);
  if (!block) {
    throw new Error(
      `в ${EXTERNAL_ROOTS_DECL} нет объявления \`var ExternalRootModules = map[string]string{…}\` — ` +
        `перечень внешних корней переименован либо переписан`,
    );
  }
  const out = new Map<string, string>();
  for (const m of block[1].matchAll(/"([^"]+)"\s*:\s*"([^"]+)"/g)) out.set(m[1], m[2]);
  if (out.size === 0) {
    throw new Error(
      `объявление ExternalRootModules в ${EXTERNAL_ROOTS_DECL} разобрано в НОЛЬ записей — ` +
        `разбор сломан, а не перечень пуст`,
    );
  }
  return out;
}

/** Каталог распакованного модуля — один запрос на модуль за прогон. */
const moduleDirCache = new Map<string, string>();

/**
 * Каталог модуля спрашивается у `go`, а не собирается из переменных окружения и
 * не выводится из формы пути кэша: кодировка этого пути — внутреннее дело `go`, и
 * собранный вручную разошёлся бы с ним молча (тот же довод, что у
 * `internal/contractsource.moduleDir`).
 *
 * Кэш модулей мог быть не прогрет — одна попытка добора, и только одна. Она здесь
 * НЕСУЩАЯ: пробы консоли гоняются девятью наборами jest, и требовать от каждого
 * предварительного `go mod download` значило бы завести условие вне дерева,
 * которое невыполнение делает красным вместо дефекта.
 */
function goModuleDir(root: string, module: string): string {
  const cached = moduleDirCache.get(module);
  if (cached) return cached;
  const ask = (): string => {
    try {
      return execFileSync("go", ["list", "-m", "-f", "{{.Dir}}", module], {
        cwd: root,
        encoding: "utf8",
        stdio: ["ignore", "pipe", "ignore"],
      }).trim();
    } catch {
      return "";
    }
  };
  let dir = ask();
  if (!dir) {
    try {
      execFileSync("go", ["mod", "download", module], { cwd: root, stdio: "ignore" });
    } catch {
      // Добор мог не удаться (нет сети, нет `go`) — причину назовёт отказ ниже.
    }
    dir = ask();
  }
  if (!dir || !existsSync(dir)) {
    throw new Error(
      `модуль ${module} не резолвится из ${root} — дерево его контрактов взять неоткуда. ` +
        `Нужен установленный \`go\` и прогретый кэш модулей: \`go mod download ${module}\`. ` +
        `Это «не выполнилось», а не «контракт изменился», и молчание здесь означало бы «совпало», ` +
        `чего никто не проверял.`,
    );
  }
  moduleDirCache.set(module, dir);
  return dir;
}

/**
 * Полный путь к контракту по его пути внутри дерева контрактов
 * (`kacho/cloud/vpc/v1/route_table.proto`, `kaname/cloud/iam/v1/role.proto`).
 *
 * КОРНЕЙ ДВА ВИДА, И ВЫБОР ИДЁТ ПО НАЛИЧИЮ В ДЕРЕВЕ. Корень, физически лежащий в
 * `proto/` монорепо, берётся ОТТУДА, и модуль для него не резолвится вовсе. Корня
 * службы доступа (`kaname`) здесь больше нет: решением владельца (kacho#2616,
 * исход C, 2026-09-13) её контракты уехали в её репозиторий и приезжают
 * опубликованным модулем `github.com/PRO-Robotech/kaname`, каталогом
 * `proto/kaname` внутри него. Тот же порядок и тот же довод, что у Go-стороны
 * (`internal/contractsource.Dir`).
 *
 * МОДУЛЬ НЕ ЛЕЖИТ В `node_modules`, и подменить его пакетом npm нельзя: версию
 * закрепляет корневой `go.mod` — то самое объявление, которым собирается продукт.
 * Поэтому каталог спрашивается у `go`, как это уже делает `corelib-source.ts` для
 * общего фундамента.
 *
 * ОТКАЗ ГРОМКИЙ И НАЗЫВАЕТ ПРИЧИНУ. Здесь читают контракт на уровне МОДУЛЯ пробы —
 * до первого `it`, — поэтому тихий возврат пустого пути уронил бы весь файл пробы
 * сообщением про `undefined`, а не про недостижимый источник.
 */
export function contractPath(protoRelPath: string): string {
  const root = monorepoRoot();
  const segments = protoRelPath.split("/");
  const inTree = join(root, "proto", ...segments);
  if (existsSync(inTree)) return inTree;

  const contractRoot = segments[0];
  const module = externalRootModules(root).get(contractRoot);
  if (!module) {
    throw new Error(
      `контракт не найден: ${protoRelPath} (искали ${inTree}); корень "${contractRoot}" ` +
        `внешним модулем не объявлен (${EXTERNAL_ROOTS_DECL}) — либо путь неверен, либо корень ` +
        `надо объявить там же, где его объявляет платформа`,
    );
  }
  const full = join(goModuleDir(root, module), "proto", ...segments);
  if (!existsSync(full)) {
    throw new Error(
      `контракт не найден: ${protoRelPath} — модуль ${module} резолвится, но этого файла в нём нет ` +
        `(искали ${full}). Версия модуля не несёт контракт, который проба называет.`,
    );
  }
  return full;
}

/** Все файлы контрактов дерева — путями относительно `proto/`. */
export function listContracts(): string[] {
  const root = join(monorepoRoot(), "proto");
  const out: string[] = [];
  const walk = (dir: string) => {
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      const full = join(dir, entry.name);
      if (entry.isDirectory()) walk(full);
      else if (entry.name.endsWith(".proto")) out.push(relative(root, full).split(sep).join("/"));
    }
  };
  walk(join(root, "kacho", "cloud"));
  return out.sort();
}

/** Снимает комментарии, не трогая содержимое строковых литералов. */
function stripComments(src: string): string {
  let out = "";
  let i = 0;
  let inString: string | null = null;
  while (i < src.length) {
    const c = src[i];
    if (inString) {
      out += c;
      if (c === "\\") {
        out += src[i + 1] ?? "";
        i += 2;
        continue;
      }
      if (c === inString) inString = null;
      i += 1;
      continue;
    }
    if (c === '"' || c === "'") {
      inString = c;
      out += c;
      i += 1;
      continue;
    }
    if (c === "/" && src[i + 1] === "/") {
      while (i < src.length && src[i] !== "\n") i += 1;
      continue;
    }
    if (c === "/" && src[i + 1] === "*") {
      i += 2;
      while (i < src.length && !(src[i] === "*" && src[i + 1] === "/")) i += 1;
      i += 2;
      continue;
    }
    out += c;
    i += 1;
  }
  return out;
}

interface Block {
  kind: "message" | "oneof" | "enum" | "other";
  name: string;
  /** Полное имя сообщения с учётом вложенности (`Target.InCloudIP`). */
  qualified: string;
}

function fieldOf(statement: string, oneof: string | null): ProtoField | null {
  const s = statement.trim();
  if (!s || /^(option|reserved|import|package|syntax|extend|returns|rpc)\b/.test(s)) return null;
  const asMap = /^map\s*<\s*([\w.]+)\s*,\s*([\w.]+)\s*>\s+(\w+)\s*=\s*\d+/.exec(s);
  if (asMap) return { name: asMap[3], kind: "map", oneof, repeated: false, type: "map" };
  const asField = /^(repeated\s+|optional\s+)?([\w.]+)\s+(\w+)\s*=\s*\d+/.exec(s);
  if (!asField) return null;
  const repeated = (asField[1] ?? "").trim() === "repeated";
  const type = asField[2];
  const kind: ProtoField["kind"] = type === "string" && !repeated ? "string" : "other";
  return { name: asField[3], kind, oneof, repeated, type };
}

/** Все сообщения одного контракта: полное имя (с вложенностью) → его поля. */
export function parseContract(source: string): Map<string, ProtoField[]> {
  const src = stripComments(source);
  const messages = new Map<string, ProtoField[]>();
  const stack: Block[] = [];
  let buf = "";

  const openBlock = () => {
    const head = buf.trim();
    buf = "";
    const msg = /^message\s+(\w+)$/.exec(head);
    if (msg) {
      const parentMessages = stack.filter((b) => b.kind === "message").map((b) => b.name);
      const qualified = [...parentMessages, msg[1]].join(".");
      stack.push({ kind: "message", name: msg[1], qualified });
      messages.set(qualified, []);
      return;
    }
    const one = /^oneof\s+(\w+)$/.exec(head);
    if (one) {
      stack.push({ kind: "oneof", name: one[1], qualified: "" });
      return;
    }
    const en = /^enum\s+(\w+)$/.exec(head);
    stack.push({ kind: en ? "enum" : "other", name: en ? en[1] : head, qualified: "" });
  };

  for (const ch of src) {
    if (ch === "{") {
      openBlock();
      continue;
    }
    if (ch === "}") {
      buf = "";
      stack.pop();
      continue;
    }
    if (ch === ";") {
      const statement = buf;
      buf = "";
      const enclosingMessage = [...stack].reverse().find((b) => b.kind === "message");
      const inEnum = stack.length > 0 && stack[stack.length - 1].kind === "enum";
      if (!enclosingMessage || inEnum) continue;
      const oneofBlock = [...stack].reverse().find((b) => b.kind === "oneof" || b.kind === "message");
      const oneof = oneofBlock && oneofBlock.kind === "oneof" ? oneofBlock.name : null;
      const f = fieldOf(statement, oneof);
      if (f) messages.get(enclosingMessage.qualified)?.push(f);
      continue;
    }
    buf += ch;
  }
  return messages;
}

/** Поля названного сообщения одного контракта. Сообщения нет — отказ, а не пустой список. */
export function parseMessageFields(source: string, message: string): ProtoField[] {
  const all = parseContract(source);
  const exact = all.get(message);
  if (exact) return exact;
  const suffix = [...all.keys()].filter((k) => k.endsWith(`.${message}`));
  if (suffix.length === 1) return all.get(suffix[0]) as ProtoField[];
  if (suffix.length > 1)
    throw new Error(`имя ${message} неоднозначно в контракте: ${suffix.join(", ")} — назови вложенность`);
  throw new Error(`в контракте нет message ${message} — предпосылка пробы не выполнена`);
}

/** Поля названного сообщения названного контракта. */
export function readMessageFields(protoRelPath: string, message: string): ProtoField[] {
  return parseMessageFields(readFileSync(contractPath(protoRelPath), "utf8"), message);
}

/**
 * Имена `repeated`-полей СООБЩЕНЧЕСКОГО типа по всему дереву контрактов.
 *
 * Это и есть словарь, по которому распознаётся запись НАБОРА: скалярный
 * `repeated string` терять нечего — у его элемента нет полей.
 */
export function repeatedMessageFieldNames(): { names: Set<string>; contractsRead: number; messagesRead: number } {
  const names = new Set<string>();
  let messagesRead = 0;
  const contracts = listContracts();
  for (const rel of contracts) {
    const all = parseContract(readFileSync(contractPath(rel), "utf8"));
    for (const fields of all.values()) {
      messagesRead += 1;
      for (const f of fields) if (f.repeated && !SCALARS.has(f.type)) names.add(f.name);
    }
  }
  return { names, contractsRead: contracts.length, messagesRead };
}

/** Каталог консоли (`ui-future`) — от расположения этого файла, а не от cwd. */
export function consoleRoot(dirOfThisFile: string): string {
  return resolve(dirOfThisFile, "../../..");
}

/** Не-тестовые исходники консоли: каждое приложение — каталог верхнего уровня со своим `src/`. */
export function listConsoleSources(root: string): string[] {
  const NOT_APPS = new Set(["node_modules", "deploy", "docs", "scripts", ".git", "e2e", "dist"]);
  const out: string[] = [];
  const walk = (dir: string) => {
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      if (entry.name === "node_modules" || entry.name === "dist") continue;
      const full = join(dir, entry.name);
      if (entry.isDirectory()) {
        walk(full);
        continue;
      }
      if (!/\.tsx?$/.test(entry.name)) continue;
      if (/\.(test|spec)\.tsx?$/.test(entry.name)) continue;
      out.push(full);
    }
  };
  for (const name of readdirSync(root)) {
    if (NOT_APPS.has(name)) continue;
    const dir = join(root, name);
    if (!statSync(dir).isDirectory()) continue;
    const src = join(dir, "src");
    if (existsSync(src) && statSync(src).isDirectory()) walk(src);
  }
  return out.sort();
}

/**
 * Корни ДЕРЕВА КОНТРАКТОВ — каталоги, каждый из которых играет роль `proto/`.
 *
 * ПЕРВЫЙ — этого дерева. Остальные — по одному на объявленный внешний корень,
 * чьё дерево приезжает опубликованным модулем (kacho#2616, исход C, 2026-09-13:
 * контракты службы доступа уехали в `PRO-Robotech/kaname`).
 *
 * ЗАЧЕМ ОТДЕЛЬНО ОТ `contractPath`. Та отвечает про ОДИН названный файл; пробам
 * поверхности нужен ВЕСЬ состав — они обходят `proto/` целиком и выводят из него
 * множество путей REST. Обход одного корня после переезда сузился на 41 контракт
 * молча: проба не падала на отсутствии файла, она получала популяцию без домена
 * `iam` и объявляла каждый его маршрут неслужимым. Замер: 82 проверки из 309 в
 * трёх наборах, и все 82 — о маршрутах службы доступа.
 *
 * Внешний корень, УЖЕ лежащий в дереве, вторым корнем не добавляется: иначе один
 * файл прочитался бы дважды, и счётчики уникальности разошлись бы сами с собой.
 */
export function contractProtoRoots(): string[] {
  const root = monorepoRoot();
  const roots = [join(root, "proto")];
  for (const [contractRoot, module] of externalRootModules(root)) {
    if (existsSync(join(root, "proto", contractRoot))) continue;
    roots.push(join(goModuleDir(root, module), "proto"));
  }
  return roots;
}
