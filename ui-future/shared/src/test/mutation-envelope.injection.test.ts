// Способность правила «конверт мутации объявлен один раз» УПАСТЬ — и промолчать
// там, где падать не на чем.
//
// Над настоящим деревом правило сегодня зелёное (все восемь копий сведены к
// фабрике — это его ЦЕЛЬ), поэтому его работоспособность из зелени не следует
// НИКАК. Здесь она доказывается инъекцией на синтетическом дереве.
//
// Законных близнецов ЧЕТЫРЕ, и каждый закрывает свою ошибку распознавателя:
// объект, взявший пятёрку РАСПАКОВКОЙ и добавивший свои глаголы (это и есть
// требуемая форма); объект с четырьмя глаголами из пяти (домен вправе не иметь
// правки); чужой объект, у которого совпало ОДНО имя; и проба, объявившая
// пятёрку в фикстуре. Без них отрицание ловило бы форму, а не существо.

import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";

import { mutationEnvelopeCensus, scanEnvelopes } from "./mutation-envelope";

let root: string;

function put(rel: string, body: string): void {
  const full = path.join(root, rel);
  mkdirSync(path.dirname(full), { recursive: true });
  writeFileSync(full, body, "utf8");
}

beforeEach(() => {
  root = mkdtempSync(path.join(tmpdir(), "kacho-envelope-lit-"));
});

afterEach(() => {
  rmSync(root, { recursive: true, force: true });
});

const OWN_ENVELOPE = [
  "export const thingsApi = {",
  "  list: (q) => api.list(THINGS, q),",
  "  get: (id) => api.get(`${THINGS}/${id}`),",
  "  create: (body) => api.create(THINGS, body),",
  "  update: (id, body) => api.update(`${THINGS}/${id}`, body),",
  "  delete: (id) => api.delete(`${THINGS}/${id}`),",
  "};",
  "",
].join("\n");

describe("инъекция: правило падает на своём конверте и молчит на фабрике", () => {
  it("настоящий дефект — модуль выписал пятёрку сам — находка с файлом И строкой", () => {
    put("demo/src/api/resources.ts", OWN_ENVELOPE);
    expect(mutationEnvelopeCensus(root).hits).toEqual(["demo/src/api/resources.ts:1"]);
  });

  it("законный близнец: пятёрка РАСПАКОВКОЙ плюс свои глаголы — молчание", () => {
    put(
      "demo/src/api/resources.ts",
      [
        "export const thingsApi = {",
        "  ...resourceApi(THINGS),",
        "  start: (id) => api.action(`${THINGS}/${id}:start`),",
        "};",
        "",
      ].join("\n"),
    );
    const census = mutationEnvelopeCensus(root);
    expect(census.hits).toEqual([]);
    // Литерал ОСМОТРЕН, а не пропущен: молчание от отсутствия предмета, а не от
    // пустого обхода.
    expect(census.literalsSeen).toBeGreaterThan(0);
  });

  it("законный близнец: четыре глагола из пяти — не конверт", () => {
    put(
      "demo/src/api/resources.ts",
      [
        "export const catalogThings = {",
        "  list: (q) => api.list(THINGS, q),",
        "  get: (id) => api.get(`${THINGS}/${id}`),",
        "  create: (body) => api.create(THINGS, body),",
        "  update: (id, body) => api.update(`${THINGS}/${id}`, body),",
        "};",
        "",
      ].join("\n"),
    );
    expect(mutationEnvelopeCensus(root).hits).toEqual([]);
  });

  it("законный близнец: чужой объект с ОДНИМ совпавшим именем — не конверт", () => {
    put("demo/src/lib/opts.ts", ["export const options = { get: 1, other: 2 };", ""].join("\n"));
    expect(mutationEnvelopeCensus(root).hits).toEqual([]);
  });

  it("законный близнец: фикстура ПРОБЫ правилом не судится", () => {
    // Проба вправе объявить конверт, чтобы подать его в проверяемый код. Считай
    // её копией — и правило краснело бы на собственных пробах дерева.
    put("demo/src/api/resources.test.ts", OWN_ENVELOPE);
    expect(mutationEnvelopeCensus(root).hits).toEqual([]);
  });

  it("распознаватель судит РАЗБОР, а не текст: те же имена в комментарии молчат", () => {
    // Абзац, объясняющий правило, называет все пять глаголов. Текстовый предикат
    // нашёл бы их и остался бы красным на собственном объяснении.
    const prose = [
      "// Конверт — это list, get, create, update, delete над одним путём.",
      'const s = "list get create update delete";',
      "export const x = { onlyOne: s };",
      "",
    ].join("\n");
    const scan = scanEnvelopes(prose, "demo.ts");
    expect(scan.lines).toEqual([]);
    expect(scan.literals).toBe(1);
  });

  it("перечень приложений ВЫВОДИТСЯ обходом, а не выписан", () => {
    put("alpha/src/api/resources.ts", OWN_ENVELOPE);
    put("beta/src/api/resources.ts", OWN_ENVELOPE);
    const census = mutationEnvelopeCensus(root);
    expect(census.apps).toEqual(["alpha", "beta"]);
    expect(census.hits).toEqual(["alpha/src/api/resources.ts:1", "beta/src/api/resources.ts:1"]);
  });
});
