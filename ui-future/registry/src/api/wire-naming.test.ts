// Шапка `src/api/types.ts` против того, что реально происходит на проводе.
//
// Предмет пробы — не стиль текста, а расхождение двух мест об одном предмете.
// Типы в этом файле объявлены в snake_case, и шапка обязана объяснить, почему:
// край отдаёт camelCase, а обратно в snake_case их переводит `api/client.ts`
// (`lib/case.ts`). Формулировка «grpc-gateway сериализует proto snake_case →
// JSON snake_case (прямой маппинг)» утверждает ОБРАТНОЕ — что переводить нечего.
// Следующий читатель, поверивший ей, снимет перевод как лишний, и весь домен
// начнёт читать поля, которых в ответе нет.
//
// Гейт заякорен на ствол, а не на память: посадку края читает
// `gateway/internal/restmux/strict_enum.go`, наличие перевода — `api/client.ts`.
// Сменят посадку края на `UseProtoNames: true` — предпосылка перестанет
// выполняться, и проба скажет об этом отдельным утверждением, а не молча
// позеленеет.

import { readFileSync } from "node:fs";
import { join } from "node:path";

const REPO_ROOT = join(process.cwd(), "..", "..");
const STRICT_ENUM = join(REPO_ROOT, "gateway", "internal", "restmux", "strict_enum.go");
const TYPES_FILE = join(process.cwd(), "src", "api", "types.ts");
const CLIENT_FILE = join(process.cwd(), "src", "api", "client.ts");

/** Объявления имён полей в маршалере края (обе посадки mux'а). */
function edgeNameDeclarations(): string[] {
  return readFileSync(STRICT_ENUM, "utf8").match(/UseProtoNames:\s*(true|false)/g) ?? [];
}

/** Первый блок `//`-строк файла — его шапка. */
function headerOf(file: string): string {
  const lines = readFileSync(file, "utf8").split("\n");
  const header: string[] = [];
  for (const line of lines) {
    if (!line.startsWith("//")) break;
    header.push(line);
  }
  return header.join("\n");
}

/**
 * Утверждает ли шапка, что перевода имён нет (край отдаёт то же, что объявлено)?
 * Обе формулировки — настоящие входы: именно они стояли в дереве.
 */
export function claimsUntranslatedWire(header: string): boolean {
  const flat = header.replace(/\s+/g, " ");
  return /JSON\s+snake_case/i.test(flat) || /прямой\s+маппинг/i.test(flat);
}

/** Называет ли шапка настоящий механизм — перевод в api/client.ts? */
export function namesTranslator(header: string): boolean {
  return /camelCase/.test(header) && /client\.ts/.test(header);
}

describe("шапка типов домена против посадки края", () => {
  it("предпосылка: край отдаёт camelCase (UseProtoNames: false на обеих посадках)", () => {
    const decls = edgeNameDeclarations();
    expect(decls.length).toBeGreaterThan(0);
    expect(decls.filter((d) => /true/.test(d))).toEqual([]);
  });

  it("предпосылка: клиент домена действительно переводит имена в обе стороны", () => {
    const client = readFileSync(CLIENT_FILE, "utf8");
    expect(client).toMatch(/snakeToCamel\(/);
    expect(client).toMatch(/camelToSnake\(/);
  });

  it("шапка не утверждает, что перевода нет", () => {
    expect(claimsUntranslatedWire(headerOf(TYPES_FILE))).toBe(false);
  });

  it("шапка называет переводчик, а не оставляет snake_case без объяснения", () => {
    expect(namesTranslator(headerOf(TYPES_FILE))).toBe(true);
  });

  it("инъекция: прежняя формулировка предикатом ловится", () => {
    const injected = "// grpc-gateway сериализует proto snake_case → JSON snake_case (прямой маппинг).";
    expect(claimsUntranslatedWire(injected)).toBe(true);
  });

  it("законный близнец: верная формулировка предикатом не ловится", () => {
    const legal = [
      "// На проводе край отдаёт camelCase (UseProtoNames: false);",
      "// объявления ниже — в snake_case, перевод делает api/client.ts.",
    ].join("\n");
    expect(claimsUntranslatedWire(legal)).toBe(false);
    expect(namesTranslator(legal)).toBe(true);
  });
});
