// Комментарий у `immutable?` в form-schema.ts ссылается на прод-код vpc: он
// утверждает, ЧЕМ именно backend отказал бы, если бы UI пропустил immutable-поле
// в update_mask. Ссылка была неверна — названа функция, которая сообщений не
// порождает вовсе (immutable-поля она молча не применяет). Такое утверждение хуже
// отсутствующего: следующий читатель идёт по названной координате, не находит там
// отказа и «чинит» вывод — либо снимает признак `immutable` как беспредметный.
//
// Поэтому ссылка утверждается против САМОГО цитируемого артефакта: тело названной
// Go-функции читается из дерева ствола, а не переписывается сюда. Переписанная
// цитата устаревает молча — это и была первопричина.
//
// Контроль в обе стороны обязателен, иначе предикат ловит форму, а не существо:
//   положительный — тело названного производителя содержит цитируемый тон и поле;
//   отрицательный — тело applySubnetMask тем же предикатом признаётся НЕ
//                   производителем (именно на нём прежняя ссылка и была неверна).
// Плюс проверка собственной предпосылки: если дерево сервисов недостижимо или
// тело не разобралось — это ПАДЕНИЕ, а не тихий пропуск. «Ноль находок» обязано
// быть отличимо от «ноль прочитанного».

import { existsSync, readFileSync, readdirSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

/** Что комментарий обязан называть — и что за этим стоит в дереве ствола. */
const CITED_PRODUCER = "UpdateSubnetUseCase.Execute";
const CITED_TONE = "is immutable after Subnet.Create";
const CITED_FIELD = "ipv4_cidr_primary";
/** Названный в комментарии НЕ-производитель: применяет маску, но не отвечает. */
const CITED_NON_PRODUCER = "applySubnetMask";

const FORM_SCHEMA = fileURLToPath(new URL("./form-schema.ts", import.meta.url));

/** Корень монорепо — ищется по наличию services/vpc, а не отсчитывается сегментами. */
function repoRoot(): string {
  let dir = dirname(FORM_SCHEMA);
  for (let i = 0; i < 10; i++) {
    if (existsSync(join(dir, "services", "vpc"))) return dir;
    dir = dirname(dir);
  }
  throw new Error(
    "не найден корень монорепо (services/vpc): цитата ссылается на прод-код ствола, " +
      "и проверить её без него нельзя — это падение, а не пропуск",
  );
}

function goFiles(dir: string, acc: string[] = []): string[] {
  for (const e of readdirSync(dir, { withFileTypes: true })) {
    const p = join(dir, e.name);
    if (e.isDirectory()) goFiles(p, acc);
    else if (e.name.endsWith(".go") && !e.name.endsWith("_test.go")) acc.push(p);
  }
  return acc;
}

/**
 * Тело функции/метода по имени `Name` либо `Type.Method`. Верхнеуровневая функция
 * в gofmt всегда закрывается `}` в нулевой колонке — по нему и режется.
 */
function goBody(files: string[], symbol: string): { file: string; body: string } | null {
  const [a, b] = symbol.split(".");
  const head = b
    ? new RegExp(String.raw`^func \([A-Za-z_]\w* \*?${a}\) ${b}\(`, "m")
    : new RegExp(String.raw`^func ${a}\(`, "m");
  for (const f of files) {
    const src = readFileSync(f, "utf8");
    const m = head.exec(src);
    if (!m) continue;
    const end = src.indexOf("\n}\n", m.index);
    return { file: f, body: src.slice(m.index, end === -1 ? src.length : end + 2) };
  }
  return null;
}

describe("ссылка комментария на отказ backend'а совпадает с прод-кодом vpc", () => {
  const root = repoRoot();
  const files = goFiles(join(root, "services", "vpc"));
  const schema = readFileSync(FORM_SCHEMA, "utf8");

  it("прочитан непустой объём прод-кода vpc, и цитируемый тон в нём существует", () => {
    expect(files.length).toBeGreaterThan(50);
    const carriers = files.filter((f) => readFileSync(f, "utf8").includes(CITED_TONE));
    // Тон обязан существовать хоть где-то: цитата выдуманного текста не должна
    // проходить только потому, что мы удачно выбрали функцию.
    expect(carriers.length).toBeGreaterThan(0);
    // eslint-disable-next-line no-console
    console.log(`[attribution] .go прочитано: ${files.length}; несут тон «${CITED_TONE}»: ${carriers.length}`);
  });

  it("комментарий у immutable? называет производителя, тон и пример поля", () => {
    const block = schema.slice(schema.indexOf("// Immutable after Create"), schema.indexOf("immutable?:"));
    expect(block.length).toBeGreaterThan(0);
    expect(block).toContain(CITED_PRODUCER);
    expect(block).toContain(CITED_TONE);
    expect(block).toContain(CITED_FIELD);
  });

  it("названный производитель действительно порождает этот отказ", () => {
    const found = goBody(files, CITED_PRODUCER);
    expect(found).not.toBeNull();
    const { body } = found!;
    // Предпосылка: тело разобралось, а не схлопнулось в сигнатуру.
    expect(body.length).toBeGreaterThan(200);
    expect(body).toContain(CITED_TONE);
    expect(body).toContain(CITED_FIELD);
  });

  it("названный НЕ-производитель тем же предикатом производителем не признаётся", () => {
    // Законный близнец: функция существует, тело читается — и отказа в нём нет.
    // Без этой пары утверждение выше зеленело бы на любом предикате, который
    // «находит» что угодно.
    const found = goBody(files, CITED_NON_PRODUCER);
    expect(found).not.toBeNull();
    const { body } = found!;
    expect(body.length).toBeGreaterThan(200);
    expect(body).not.toContain(CITED_TONE);
  });

  it("комментарий не приписывает отказ этому НЕ-производителю", () => {
    const block = schema.slice(schema.indexOf("// Immutable after Create"), schema.indexOf("immutable?:"));
    // Упоминать его можно — приписывать отказ нельзя: между именем и тоном в
    // одной строке и была неверная атрибуция.
    const lines = block.split("\n").filter((l) => l.includes(CITED_NON_PRODUCER));
    for (const l of lines) expect(l).not.toContain(CITED_TONE);
  });
});
