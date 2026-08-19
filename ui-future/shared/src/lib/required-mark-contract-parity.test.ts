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
// ОБЛАСТЬ — ВСЕ поля, которые гейт способен рассудить (расширено #609; прежде
// судилось одно поле `name`). Отображение «подпись формы → поле контракта»
// выводится из реестра целиком: `REGISTRY[specId].fields` объявляет и `label`, и
// `name`, то есть обе стороны сопоставления уже лежат в дереве — выписывать их
// рядом с гейтом значило бы завести второе место об одном предмете. Пока область
// была сужена до `name`, поле `ipv4_cidr_primary` не держалось ничем: его
// звёздочка утверждала обязанность, которой контракт не объявляет, и гейт про
// это молчал по построению.
//
// ЧТО ГЕЙТ РАССУДИТЬ НЕ БЕРЁТСЯ — названо и посчитано, а не умолчано:
//
//   • поле формы (`_placement` и подобные) — у него нет соответствия в контракте
//     вовсе, спрашивать не о чем;
//   • вложенный путь (`spec.rules[0].x`) — обязательность родителя не есть
//     обязательность части;
//   • поле ЗА ДИСКРИМИНАТОРОМ (`visibleWhen`) — его обязанность УСЛОВНА, а
//     `(required)` безусловен, то есть инструмент не той меры. Зона доступности
//     и регион подсети именно таковы: показывается ровно один из двух, и
//     показанный обязателен — при том что контракт не объявляет обязательным ни
//     один. Вынести по ним вердикт значило бы потребовать снять звёздочку с
//     поля, которое край без значения не примет;
//   • подпись, которую не называет ни одно поле реестра, — форма назвала ряд
//     по-своему либо ряд вообще не поле ресурса (заголовок группы). Это не
//     «законно», это «вне инструмента», и счёт печатается отдельно.
//
// Исключение по дискриминатору САМОИСТЕКАЕТ: снимут `visibleWhen` — поле снова
// попадёт под суд, и звёздочка на нём будет сверена с контрактом.
//
// ЧЕМ ОГРАНИЧЕН ВЕРДИКТ (названо, чтобы не читалось шире). Обязательность
// берётся объединением по ВСЕМ путям записи ресурса (`post`/`put`/`patch` его
// REST-адреса): если контракт требует поле на создании, гейт молчит и о форме
// правки. Это делает его консервативным — он пропускает, но не выдумывает.
//
// СПОСОБНОСТЬ УПАСТЬ доказана инъекцией в обе стороны (describe ниже): дефект
// краснеет с координатой, а законный близнец ТОЙ ЖЕ ФОРМЫ — подпись со
// звёздочкой у поля, чей контракт её требует, — молчит. Дискриминатор держится
// одним именем с обеих сторон, и близнец берётся у ТОГО ЖЕ ресурса там, где это
// возможно: пара из разных ресурсов молчала бы и по причине, к предмету
// отношения не имеющей.

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

/** Поля ресурса, объявленные реестром: подпись ↔ имя поля контракта. */
function fieldsOf(specId: string): Array<{ name: string; label: string; branched: boolean }> {
  return (REGISTRY[specId]?.fields ?? []).map((f) => ({
    name: f.name,
    label: f.label,
    branched: f.visibleWhen !== undefined,
  }));
}

/** Подпись формы называет это поле ресурса? */
function labelNamesField(labelExpr: string, fieldLabel: string): boolean {
  const norm = labelExpr.replace(/>\s+/g, ">").replace(/\s+</g, "<").trim();
  return (
    norm === fieldLabel ||
    norm.includes(`"${fieldLabel}"`) ||
    norm.includes(`'${fieldLabel}'`) ||
    norm.includes(`>${fieldLabel}<`)
  );
}

function unjudgeable(field: { name: string; branched: boolean }): string | null {
  if (field.name.startsWith("_")) return "поле формы — соответствия в контракте нет";
  if (/[.[]/.test(field.name)) return "вложенный путь";
  if (field.branched) return "поле за дискриминатором: обязанность условна, а «(required)» безусловен";
  return null;
}

interface Verdict {
  findings: string[];
  judged: number;
  unresolved: number;
  unmapped: number;
  why: Map<string, number>;
  /** Что именно рассужено — иначе «рассужено N» неотличимо от «рассужено не то». */
  judgedLines: string[];
}

function adjudicate(forms: FormsRead): Verdict {
  const findings: string[] = [];
  const why = new Map<string, number>();
  const judgedLines: string[] = [];
  let judged = 0;
  let unresolved = 0;
  let unmapped = 0;

  const note = (reason: string) => why.set(reason, (why.get(reason) ?? 0) + 1);

  for (const claim of forms.claims) {
    let judgedHere = false;
    let reason: string | null = null;

    for (const specId of claim.specIds) {
      const apiPath = REGISTRY[specId]?.apiPath;
      for (const field of fieldsOf(specId)) {
        if (!labelNamesField(claim.label, field.label)) continue;
        const bad = unjudgeable(field);
        if (bad !== null) {
          reason ??= bad;
          continue;
        }
        const requires = apiPath ? contractRequires(apiPath, field.name) : null;
        if (requires === null) {
          reason ??= "путь записи ресурса в контракте не найден";
          continue;
        }
        judgedHere = true;
        judgedLines.push(`${claim.rel}:${claim.line} ${specId}.${field.name} — контракт требует: ${requires}`);
        if (requires === false) {
          findings.push(
            `${claim.rel}:${claim.line} — подпись «${field.label}» объявлена обязательной, но контракт ` +
              `владельца (${apiPath}) поле «${field.name}» обязательным не делает`,
          );
        }
      }
    }

    if (judgedHere) judged++;
    else if (reason !== null) {
      unresolved++;
      note(reason);
    } else unmapped++;
  }

  return { findings, judged, unresolved, unmapped, why, judgedLines };
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
      `[#609] форм прочитано ${treeForms.filesRead} · Form.Item разобрано ${treeForms.itemsParsed} · ` +
        `объявили обязательность ${treeForms.claims.length} = рассужено ${treeVerdict.judged} + ` +
        `не рассужено ${treeVerdict.unresolved} + подпись вне реестра ${treeVerdict.unmapped} · ` +
        `находок ${treeVerdict.findings.length}\n` +
        [...treeVerdict.why].map(([reason, n]) => `    не рассужено, ${reason}: ${n}`).join("\n") +
        `\n    подпись вне реестра — форма назвала ряд по-своему либо ряд вообще не поле ресурса\n` +
        treeVerdict.judgedLines.map((line) => `    рассужено: ${line}`).join("\n"),
    );
    expect(treeVerdict.judged).toBeGreaterThan(0);

    // Перепись обязана СХОДИТЬСЯ: три корзины в сумме дают все объявления.
    // Не сойдясь, она перестаёт быть переписью и становится тремя числами,
    // из которых каждое по отдельности ни о чём не говорит.
    expect(treeVerdict.judged + treeVerdict.unresolved + treeVerdict.unmapped).toBe(treeForms.claims.length);
  });
});

describe("звёздочка обязательности сходится с контрактом владельца", () => {
  it("ни одно поле не объявлено обязательным вопреки контракту владельца", () => {
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

  it("подпись, которой реестр не знает, — ВНЕ инструмента, и это не выдаётся за законность", () => {
    // Форма назвала ряд «Сеть», реестр называет то же поле «Облачная сеть».
    // Сопоставить нечем — значит вердикта нет: ни находки, ни оправдания.
    // Молчание такого рода обязано быть отличимо от разобранного молчания,
    // поэтому оно считается в свою корзину.
    const verdict = защёлка(
      `<FormShell specId="subnets" mode="create">\n  <Form.Item label="Сеть" required>\n    <Select />\n  </Form.Item>\n</FormShell>\n`,
    );
    expect(verdict.findings).toEqual([]);
    expect(verdict.judged).toBe(0);
    expect(verdict.unmapped).toBe(1);
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

  // ── область шире поля `name` (#609) ───────────────────────────────────────

  it("контракт различает два поля ОДНОГО ресурса — иначе дискриминатор мнимый", () => {
    // Пара внутри одного ресурса сильнее пары из двух: она исключает объяснение
    // «молчит, потому что про этот ресурс ничего не прочитал».
    expect(contractRequires("/vpc/v1/subnets", "network_id")).toBe(true);
    expect(contractRequires("/vpc/v1/subnets", "ipv4_cidr_primary")).toBe(false);
  });

  it("ДЕФЕКТ #609: «Основной IPv4 CIDR» со звёздочкой — находка с координатой", () => {
    // Ровно то, что стояло в дереве до #609. Пока область гейта была сужена до
    // поля `name`, это объявление не рассматривалось вовсе.
    const verdict = защёлка(
      `<FormShell specId="subnets" mode="create">\n` +
        `  <Form.Item label="Основной IPv4 CIDR" required>\n    <Input />\n  </Form.Item>\n</FormShell>\n`,
    );
    expect(verdict.findings).toHaveLength(1);
    expect(verdict.findings[0]).toContain("synthetic.tsx:2");
    expect(verdict.findings[0]).toContain("ipv4_cidr_primary");
  });

  it("ДЕФЕКТ #609 в ТОЙ ФОРМЕ ЗАПИСИ, в какой он и жил — подпись выражением на нескольких строках", () => {
    // Подпись в дереве стояла многострочным JSX, и `>Основной IPv4 CIDR<`
    // буквально в тексте не встречалось: между `>` и словами стоял перенос
    // строки с отступом. Сравнение без приведения пробелов молчало бы на
    // настоящем дефекте, находя лишь его выпрямленную копию, — то есть гейт был
    // бы доказан на входе, которого дерево не производит.
    const verdict = защёлка(
      `<FormShell specId="subnets" mode="create">\n` +
        `  <Form.Item\n    label={\n      <Space size={4}>\n        Основной IPv4 CIDR\n` +
        `        <Tooltip title="Неизменяемый основной IPv4 CIDR подсети.">\n` +
        `          <QuestionCircleOutlined />\n        </Tooltip>\n      </Space>\n    }\n    required\n  >\n` +
        `    <Input />\n  </Form.Item>\n</FormShell>\n`,
    );
    expect(verdict.findings).toHaveLength(1);
    expect(verdict.findings[0]).toContain("ipv4_cidr_primary");
  });

  it("БЛИЗНЕЦ #609: та же подпись без звёздочки — молчание", () => {
    const verdict = защёлка(
      `<FormShell specId="subnets" mode="create">\n` +
        `  <Form.Item label="Основной IPv4 CIDR">\n    <Input />\n  </Form.Item>\n</FormShell>\n`,
    );
    expect(verdict.findings).toEqual([]);
  });

  it("БЛИЗНЕЦ #609: звёздочка у поля ТОГО ЖЕ ресурса, которого контракт требует — молчание, и оно РАЗОБРАНО", () => {
    // Ключевой близнец: та же форма, тот же ресурс, та же запись — и молчание
    // не оттого, что гейт не разобрал, а оттого, что `network_id` объявлен
    // `(required) = true`. Без проверки `judged` молчание означало бы «не читал».
    const verdict = защёлка(
      `<FormShell specId="subnets" mode="create">\n` +
        `  <Form.Item label="Облачная сеть" required>\n    <Select />\n  </Form.Item>\n</FormShell>\n`,
    );
    expect(verdict.findings).toEqual([]);
    expect(verdict.judged).toBe(1);
  });

  it("поле ЗА ДИСКРИМИНАТОРОМ не судится — и гейт называет это причиной, а не молчит", () => {
    // Зона показывается только в ветви ZONAL и в ней обязательна, при том что
    // контракт не объявляет её обязательной безусловно: инструмент не той меры.
    // Требование снять с неё звёздочку было бы требованием солгать в другую
    // сторону — край подсеть без зоны не примет.
    const verdict = защёлка(
      `<FormShell specId="subnets" mode="create">\n` +
        `  <Form.Item label="Зона доступности" required>\n    <Select />\n  </Form.Item>\n</FormShell>\n`,
    );
    expect(verdict.findings).toEqual([]);
    expect(verdict.judged).toBe(0);
    expect(verdict.unresolved).toBe(1);
    expect([...verdict.why.keys()].join(" ")).toContain("дискриминатор");
  });

  it("поле ФОРМЫ не судится: у него нет соответствия в контракте", () => {
    const verdict = защёлка(
      `<FormShell specId="subnets" mode="create">\n` +
        `  <Form.Item label="Размещение" required>\n    <Select />\n  </Form.Item>\n</FormShell>\n`,
    );
    expect(verdict.findings).toEqual([]);
    expect(verdict.unresolved).toBe(1);
  });
});
