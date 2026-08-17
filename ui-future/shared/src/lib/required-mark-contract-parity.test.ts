// Звёздочка обязательности обязана сходиться с КОНТРАКТОМ владельца (#608).
//
// ПРЕДМЕТ. Значок «*» у подписи сообщает арендатору: без этого поля запрос не
// примут. Утверждение проверяемое — обязательность поля объявляет контракт
// владельца (`(required) = true` у поля запроса). Форма, ставящая значок там,
// где контракт поле обязательным не делает, утверждает неправду; хуже того, она
// обесценивает значок вообще: если он стоит безотносительно обязательности, по
// нему нельзя понять, что заполнить обязан, — а это единственное, ради чего он
// нарисован (решение владельца #562).
//
// ПОЧЕМУ ЭТО ГЕЙТ, А НЕ ПРОБА ОДНОЙ ФОРМЫ. Класс найден переписью, а не диффом:
// пять форм арендатора объявляли «Имя» обязательным, тогда как контракт vpc не
// требует `name` НИ У ОДНОГО ресурса, а `submit()` этих форм имени не проверяет
// вовсе — то есть значок не подпирался даже самой формой. Проба браузером
// (`e2e/specs/findings.spec.ts`, `verifies #562`) видит одну форму из пяти:
// остальные четыре остались бы без держащего артефакта.
//
// ПОЧЕМУ ЧТЕНИЕМ ДЕРЕВА. Общий стенд проб подменяет `antd` (`test/antd-stub.ts`):
// `Form.Item` рисуется без разметки библиотеки, звёздочки в jsdom нет как узла —
// утверждать о ней рендером здесь нельзя ни при каком написании пробы. Поэтому
// сверяются ОБЪЯВЛЕНИЯ: что объявила форма и что объявил контракт. Это перепись
// состава дерева, а не «проба, подтверждающая себя своим же текстом»: она обходит
// дерево и читает файлы, которых сама не называет (гейт продукта
// `TestUITestsDoNotReadTheirOwnSourceAsText` исключает обход именно по этой
// причине). Разбор идёт по исполняемой части — комментарии сняты общим
// разборщиком `stripComments`, иначе гейт нашёл бы `required` в абзаце, который
// сам же и объясняет запрет.
//
// ОБЛАСТЬ — поле `name`, и это сказано прямо. Общее отображение «подпись формы →
// поле контракта» в дереве не объявлено, поэтому гейт судит ровно то поле, чья
// подпись выводится из реестра (`REGISTRY[specId].fields`, элемент `name`), и
// печатает, сколько объявлений он рассудить не смог. Расширять область — работа
// со своим предметом, а не молчаливое умолчание этой.
//
// ЧЕМ ОГРАНИЧЕН ВЕРДИКТ (названо, чтобы не читалось шире). Обязательность
// берётся объединением по ВСЕМ путям записи ресурса (`post`/`put`/`patch` его
// REST-адреса): если контракт требует `name` на создании, гейт молчит и о форме
// правки. Это делает его консервативным — он пропускает, но не выдумывает.
//
// СПОСОБНОСТЬ УПАСТЬ доказана инъекцией в обе стороны (describe ниже): дефект
// краснеет с координатой, а законный близнец ТОЙ ЖЕ ФОРМЫ — подпись «Имя» со
// звёздочкой у ресурса, чей контракт её требует (группа IAM), — молчит.
// Дискриминатор держится одним именем с обеих сторон.

import { execFileSync } from "node:child_process";
import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import { join, resolve } from "node:path";

import { stripComments } from "@shared/test/strip-comments";
import { REGISTRY } from "./resource-registry";

const APP_DIR = process.cwd();
const REPO_ROOT = resolve(APP_DIR, "../..");
const UI_ROOT = resolve(APP_DIR, "..");
const PROTO_ROOT = join(REPO_ROOT, "proto", "kacho", "cloud");

// ── разбор исходного текста: строки, теги, атрибуты ──────────────────────────

/** Индекс ПОСЛЕ закрывающей кавычки литерала, начинающегося в `i`. */
function skipString(src: string, i: number): number {
  const quote = src[i];
  i++;
  while (i < src.length) {
    if (src[i] === "\\") {
      i += 2;
      continue;
    }
    if (src[i] === quote) return i + 1;
    i++;
  }
  return i;
}

/**
 * Текст ОТКРЫВАЮЩЕГО тега — от `<` до его собственного `>`.
 *
 * Скобки считаются: `label={<Space size={4}>…</Space>}` несёт `>` внутри
 * выражения, и наивный поиск первого `>` обрезал бы тег на нём, потеряв всё, что
 * стоит правее, — в том числе `required`.
 */
function readOpeningTag(src: string, start: number): string | null {
  let i = start + 1;
  let depth = 0;
  while (i < src.length) {
    const c = src[i];
    if (c === '"' || c === "'" || c === "`") {
      i = skipString(src, i);
      continue;
    }
    if (c === "{") depth++;
    else if (c === "}") depth--;
    else if (c === ">" && depth === 0) return src.slice(start, i + 1);
    i++;
  }
  return null;
}

/** Значение атрибута тега: `"…"` → его содержимое, `{…}` → выражение целиком. */
function attrValue(tag: string, attr: string): string | null {
  const m = new RegExp(`(?:^|\\s)${attr}\\s*=\\s*`).exec(tag);
  if (!m) return null;
  const i = m.index + m[0].length;
  const c = tag[i];
  if (c === '"' || c === "'" || c === "`") return tag.slice(i + 1, skipString(tag, i) - 1);
  if (c !== "{") return null;
  let depth = 0;
  let j = i;
  while (j < tag.length) {
    const ch = tag[j];
    if (ch === '"' || ch === "'" || ch === "`") {
      j = skipString(tag, j);
      continue;
    }
    if (ch === "{") depth++;
    else if (ch === "}" && --depth === 0) return tag.slice(i + 1, j);
    j++;
  }
  return null;
}

/**
 * Тег объявляет поле обязательным.
 *
 * Обе формы засчитываются, потому что библиотека рисует звёздочку по обеим:
 * свойство `required` и правило `rules={[{ required: true }]}`. `required={false}`
 * под предикат не подпадает — за `required` там стоит `=`, а не конец имени.
 */
const CLAIMS_REQUIRED = /(?:^|[\s{,])required(?:\s*[:=]\s*\{?\s*true\s*\}?)?(?=[\s>/,}\]])/;

function lineOf(src: string, index: number): number {
  return src.slice(0, index).split("\n").length;
}

function walk(dir: string, match: RegExp, out: string[] = []): string[] {
  for (const entry of readdirSync(dir)) {
    if (entry === "node_modules" || entry === "dist") continue;
    const p = join(dir, entry);
    if (statSync(p).isDirectory()) walk(p, match, out);
    else if (match.test(p)) out.push(p);
  }
  return out;
}

// ── сторона контракта: что владелец объявил обязательным ────────────────────

/** Тело `{…}`, начинающееся в `open`, без самих скобок. */
function readBraced(src: string, open: number): string {
  let depth = 0;
  let i = open;
  while (i < src.length) {
    const c = src[i];
    if (c === '"' || c === "'") {
      i = skipString(src, i);
      continue;
    }
    if (c === "{") depth++;
    else if (c === "}" && --depth === 0) return src.slice(open + 1, i);
    i++;
  }
  return src.slice(open + 1);
}

/** Опции поля `[…]`, начинающиеся в `open`, вместе со скобками. */
function readBracketed(src: string, open: number): string {
  let depth = 0;
  let i = open;
  while (i < src.length) {
    const c = src[i];
    if (c === '"' || c === "'") {
      i = skipString(src, i);
      continue;
    }
    if (c === "[") depth++;
    else if (c === "]" && --depth === 0) return src.slice(open, i + 1);
    i++;
  }
  return src.slice(open);
}

/** Тело сообщения без вложенных `message`/`oneof`/`enum` блоков. */
function withoutNested(body: string): string {
  let out = "";
  let i = 0;
  while (i < body.length) {
    const m = /\b(message|oneof|enum)\s+\w+\s*\{/.exec(body.slice(i));
    if (!m) return out + body.slice(i);
    const open = i + m.index + m[0].length - 1;
    out += body.slice(i, i + m.index);
    i = open + readBraced(body, open).length + 2;
  }
  return out;
}

interface Contract {
  /** REST-путь записи → имена сообщений запроса. */
  writePaths: Map<string, Set<string>>;
  /** Сообщение → набор полей, объявленных `(required) = true`. */
  requiredFields: Map<string, Set<string>>;
  filesRead: number;
}

function readContract(): Contract {
  const writePaths = new Map<string, Set<string>>();
  const requiredFields = new Map<string, Set<string>>();
  const files = existsSync(PROTO_ROOT) ? walk(PROTO_ROOT, /\.proto$/) : [];

  for (const file of files) {
    const src = stripComments(readFileSync(file, "utf8"));

    // rpc → REST-адреса записи. Тело rpc берётся до следующего `rpc `: другого
    // rpc внутри него быть не может.
    const rpcs = [...src.matchAll(/\brpc\s+\w+\s*\(\s*([\w.]+)\s*\)/g)];
    rpcs.forEach((m, k) => {
      const chunk = src.slice(m.index, k + 1 < rpcs.length ? rpcs[k + 1].index : src.length);
      const input = m[1].split(".").pop()!;
      for (const h of chunk.matchAll(/\b(?:post|put|patch)\s*:\s*"([^"]+)"/g)) {
        const set = writePaths.get(h[1]) ?? new Set<string>();
        set.add(input);
        writePaths.set(h[1], set);
      }
    });

    // message → поля с `(required) = true`.
    for (const m of src.matchAll(/\bmessage\s+(\w+)\s*\{/g)) {
      const body = withoutNested(readBraced(src, m.index + m[0].length - 1));
      const required = new Set<string>();
      for (const f of body.matchAll(/\b[\w.<>, ]+?\s(\w+)\s*=\s*\d+\s*(?=[[;])/g)) {
        const after = f.index + f[0].length;
        if (body[after] !== "[") continue;
        if (/\(required\)\s*=\s*true/.test(readBracketed(body, after))) required.add(f[1]);
      }
      requiredFields.set(m[1], required);
    }
  }

  return { writePaths, requiredFields, filesRead: files.length };
}

const contract = readContract();

/**
 * Контракт требует поле `field` у ресурса с REST-адресом `apiPath`?
 *
 * `null` — сопоставить не удалось: ни один путь записи не отвечает адресу. Это
 * не «не требует»: молчание и незнание обязаны быть различимы.
 */
function contractRequires(apiPath: string, field: string): boolean | null {
  const messages = new Set<string>();
  for (const [path, msgs] of contract.writePaths) {
    if (path === apiPath || path.startsWith(`${apiPath}/`)) for (const m of msgs) messages.add(m);
  }
  if (messages.size === 0) return null;
  for (const m of messages) if (contract.requiredFields.get(m)?.has(field)) return true;
  return false;
}

// ── сторона консоли: что объявила форма ─────────────────────────────────────

interface Claim {
  rel: string;
  line: number;
  specIds: string[];
  label: string;
}

interface FormsRead {
  filesRead: number;
  itemsParsed: number;
  claims: Claim[];
}

/** Все `Form.Item`, объявившие обязательность, вместе с их подписью и ресурсом. */
function readForms(sources: Array<{ rel: string; raw: string }>): FormsRead {
  let itemsParsed = 0;
  const claims: Claim[] = [];

  for (const { rel, raw } of sources) {
    const src = stripComments(raw, { keepLines: true });
    const specIds = [
      ...new Set(
        [...src.matchAll(/<FormShell\b/g)]
          .map((m) => readOpeningTag(src, m.index))
          .map((tag) => (tag === null ? null : attrValue(tag, "specId")))
          .filter((v): v is string => v !== null),
      ),
    ];
    for (const m of src.matchAll(/<Form\.Item\b/g)) {
      const tag = readOpeningTag(src, m.index);
      if (tag === null) continue;
      itemsParsed++;
      if (!CLAIMS_REQUIRED.test(tag)) continue;
      const label = attrValue(tag, "label");
      if (label === null) continue;
      claims.push({ rel, line: lineOf(src, m.index), specIds, label });
    }
  }

  return { filesRead: sources.length, itemsParsed, claims };
}

/** Подпись поля `name` у ресурса — берётся из реестра, а не выписывается. */
function nameLabelOf(specId: string): string | null {
  const spec = REGISTRY[specId];
  const field = spec?.fields?.find((f) => f.name === "name");
  return field?.label ?? null;
}

/** Подпись формы называет поле `name` этого ресурса? */
function labelNamesField(labelExpr: string, fieldLabel: string): boolean {
  return (
    labelExpr.trim() === fieldLabel ||
    labelExpr.includes(`"${fieldLabel}"`) ||
    labelExpr.includes(`'${fieldLabel}'`) ||
    labelExpr.includes(`>${fieldLabel}<`)
  );
}

interface Verdict {
  findings: string[];
  /** Объявлений про поле `name`, сопоставленных с контрактом. */
  judged: number;
  /** Объявлений, которые гейт рассудить не смог (ресурс или контракт не найден). */
  unresolved: number;
}

function adjudicate(forms: FormsRead): Verdict {
  const findings: string[] = [];
  let judged = 0;
  let unresolved = 0;

  for (const claim of forms.claims) {
    for (const specId of claim.specIds) {
      const fieldLabel = nameLabelOf(specId);
      if (fieldLabel === null || !labelNamesField(claim.label, fieldLabel)) continue;
      const apiPath = REGISTRY[specId]?.apiPath;
      const requires = apiPath ? contractRequires(apiPath, "name") : null;
      if (requires === null) {
        unresolved++;
        continue;
      }
      judged++;
      if (requires === false) {
        findings.push(
          `${claim.rel}:${claim.line} — подпись «${fieldLabel}» объявлена обязательной, но контракт ` +
            `владельца (${apiPath}) поле «name» обязательным не делает. Значок утверждает обязанность, ` +
            `которой нет: арендатор перестаёт различать по нему, что заполнить обязан`,
        );
      }
    }
  }

  return { findings, judged, unresolved };
}

// ── перепись дерева ──────────────────────────────────────────────────────────

const treeSources = execFileSync("git", ["ls-files", "*/src/**/*.tsx"], { cwd: UI_ROOT, encoding: "utf8" })
  .split("\n")
  .filter((rel) => rel.length > 0 && !rel.includes(".test."))
  .map((rel) => ({ rel, raw: readFileSync(join(UI_ROOT, rel), "utf8") }));

const treeForms = readForms(treeSources);
const treeVerdict = adjudicate(treeForms);

describe("объём осмотренного — «ноль находок» отличимо от «ноль прочитанного»", () => {
  it("контракт прочитан, и обязательность в нём вообще встречается", () => {
    // Пустой разбор контракта дал бы «ничего не обязательно» и объявил находкой
    // КАЖДУЮ звёздочку — то есть гейт лгал бы уверенно.
    expect(contract.filesRead).toBeGreaterThan(20);
    expect(contract.writePaths.size).toBeGreaterThan(20);
    expect([...contract.requiredFields.values()].filter((s) => s.size > 0).length).toBeGreaterThan(20);
  });

  it("формы консоли прочитаны, и объявления обязательности в них найдены", () => {
    expect(treeForms.filesRead).toBeGreaterThan(100);
    expect(treeForms.itemsParsed).toBeGreaterThan(50);
    expect(treeForms.claims.length).toBeGreaterThan(5);
  });

  it("перепись напечатана: рассужено, не рассужено, найдено", () => {
    // Число здесь не утверждается — оно печатается. Утверждение о нём было бы
    // ориентиром, который устаревает молча.
    // eslint-disable-next-line no-console
    console.log(
      `[#608] форм прочитано ${treeForms.filesRead} · Form.Item разобрано ${treeForms.itemsParsed} · ` +
        `объявили обязательность ${treeForms.claims.length} · из них про поле «name» рассужено ` +
        `${treeVerdict.judged}, не рассужено ${treeVerdict.unresolved} · находок ${treeVerdict.findings.length}`,
    );
    expect(treeVerdict.judged).toBeGreaterThan(0);
  });
});

describe("звёздочка обязательности сходится с контрактом владельца", () => {
  it("ни одна форма не объявляет «Имя» обязательным вопреки контракту", () => {
    expect(treeVerdict.findings).toEqual([]);
  });
});

// ── способность упасть: инъекция в обе стороны ──────────────────────────────

describe("инъекция: гейт краснеет на дефекте и молчит на законном близнеце", () => {
  const защёлка = (raw: string) => adjudicate(readForms([{ rel: "synthetic.tsx", raw }]));

  it("контракт различает два ресурса — иначе дискриминатору нечего различать", () => {
    // Положительный контроль самого разбора контракта: если бы обе стороны
    // читались одинаково, инъекция ниже проходила бы по причине, не имеющей
    // отношения к предмету.
    expect(contractRequires("/vpc/v1/subnets", "name")).toBe(false);
    expect(contractRequires("/iam/v1/groups", "name")).toBe(true);
  });

  it("ДЕФЕКТ: «Имя» со звёздочкой у ресурса, чей контракт её не требует — находка с координатой", () => {
    const verdict = защёлка(
      `<FormShell specId="subnets" mode="create">\n` +
        `  <Form.Item label="Имя" required>\n    <Input />\n  </Form.Item>\n` +
        `</FormShell>\n`,
    );
    expect(verdict.findings).toHaveLength(1);
    expect(verdict.findings[0]).toContain("synthetic.tsx:2");
  });

  it("ДЕФЕКТ той же формы через выражение подписи — тоже находка", () => {
    // Первая перепись по `label="Имя"` нашла три места; ещё два несли подпись
    // выражением (`labelWithInfo("Имя", …)`) и в неё не попали. Радиус берётся
    // по механизму, а не по форме записи.
    const verdict = защёлка(
      `<FormShell specId="network-interfaces" mode="create">\n` +
        `  <Form.Item label={labelWithInfo("Имя", "Имя интерфейса в пределах фолдера.")} required>\n` +
        `    <Input />\n  </Form.Item>\n</FormShell>\n`,
    );
    expect(verdict.findings).toHaveLength(1);
  });

  it("БЛИЗНЕЦ: та же подпись без звёздочки — молчание, и объявление рассужено", () => {
    const verdict = защёлка(
      `<FormShell specId="subnets" mode="create">\n  <Form.Item label="Имя">\n    <Input />\n  </Form.Item>\n</FormShell>\n`,
    );
    expect(verdict.findings).toEqual([]);
  });

  it("БЛИЗНЕЦ: «Имя» со звёздочкой там, где контракт её ТРЕБУЕТ — молчание, но объявление рассужено", () => {
    // Ключевой близнец: форма выглядит ровно как дефект, и молчит гейт не
    // потому, что не разобрал её, а потому, что контракт группы IAM объявляет
    // `name` обязательным. Без проверки `judged` молчание означало бы «не
    // прочитал».
    const verdict = защёлка(
      `<FormShell specId="groups" mode="create">\n` +
        `  <Form.Item label="Имя" name="name" required rules={[{ required: true }]}>\n` +
        `    <Input />\n  </Form.Item>\n</FormShell>\n`,
    );
    expect(verdict.findings).toEqual([]);
    expect(verdict.judged).toBe(1);
  });

  it("БЛИЗНЕЦ: звёздочка у ДРУГОГО поля — вне области гейта, и он это не скрывает", () => {
    // Гейт судит поле `name`. Обязательность «Сети» он не рассматривает вовсе —
    // и не объявляет её законной: она просто не попадает в счёт рассуженного.
    const verdict = защёлка(
      `<FormShell specId="subnets" mode="create">\n  <Form.Item label="Сеть" required>\n    <Select />\n  </Form.Item>\n</FormShell>\n`,
    );
    expect(verdict.findings).toEqual([]);
    expect(verdict.judged).toBe(0);
  });

  it("разбор тега переживает `>` внутри выражения подписи", () => {
    // Наивный поиск первого `>` обрезал бы тег на `<Space size={4}>` и потерял
    // `required` — гейт молчал бы на дефекте, который обязан находить.
    const verdict = защёлка(
      `<FormShell specId="subnets" mode="create">\n` +
        `  <Form.Item label={<Space size={4}>Имя<Tooltip title="x" /></Space>} required>\n` +
        `    <Input />\n  </Form.Item>\n</FormShell>\n`,
    );
    expect(verdict.findings).toHaveLength(1);
  });
});
