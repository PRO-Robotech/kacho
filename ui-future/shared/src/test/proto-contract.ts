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

/** Полный путь к контракту по его пути внутри `proto/` (`kacho/cloud/vpc/v1/route_table.proto`). */
export function contractPath(protoRelPath: string): string {
  const full = join(monorepoRoot(), "proto", ...protoRelPath.split("/"));
  if (!existsSync(full)) throw new Error(`контракт не найден: ${protoRelPath} (искали ${full})`);
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
