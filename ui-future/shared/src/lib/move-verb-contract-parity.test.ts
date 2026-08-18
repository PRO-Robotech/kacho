// Пункт «Переместить» предлагается ТОЛЬКО там, где глагол объявлен (#583).
//
// ПРЕДМЕТ. Пункт меню строки открывает окно-заглушку, печатающее `POST <путь>:move`.
// Предлагать его ресурсу, у чьего API такого глагола нет, значит рекламировать
// операцию, которой не существует: арендатор видит действие, нажимает и получает
// печать несуществующего вызова. Правило записано комментарием у самого списка с
// самого начала — нарушало его УМОЛЧАНИЕ, а не забывчивость.
//
// ПОЧЕМУ УМОЛЧАНИЕ И ЕСТЬ ПРЕДМЕТ. Список был устроен как перечень ИСКЛЮЧЕНИЙ
// («перемещать нечем»), тогда как перемещаемых ресурсов во всём продукте ДВА.
// При таком умолчании каждый новый ресурс попадает в меню с заглушкой сам, и
// автор ресурса о списке не знает: #561 (пользователь) был не пропуском одной
// записи, а первым замеченным экземпляром. Признак перевёрнут: пункт предлагается
// там, где глагол ОБЪЯВЛЕН, а не там, где не запрещён.
//
// ПОЧЕМУ ГЕЙТ, А НЕ ВЫПИСАННЫЙ СПИСОК. Перечень перемещаемых ресурсов ВЫВОДИТСЯ
// из контрактов: гейт обходит `proto/kacho/cloud/**` и берёт REST-аннотации,
// оканчивающиеся на `:move`. Тогда новый глагол на контракте включает пункт сам
// (гейт краснеет, пока консоль не догонит), а снятый — выключает. Выписанный
// список этого свойства не имеет: он расходится с контрактом молча и ровно так,
// что расхождение не видно ни на одной стороне.
//
// ПОЧЕМУ ЧТЕНИЕМ ДЕРЕВА, А НЕ РЕНДЕРОМ. Форков меню в дереве пять, и судить надо
// КАЖДЫЙ: рендер одного из них ничего не говорит об остальных, а именно в них
// защита и отсутствовала. Разбор идёт по исполняемой части — комментарии сняты
// `stripComments`, иначе гейт нашёл бы `moveCapable` в абзаце, который сам же его
// и объясняет.
//
// ОБЛАСТЬ ВЕРДИКТА названа прямо. Гейт судит ресурс, у чьей записи реестра есть
// `apiPath`: сверять контракт можно только с тем, у кого объявлен адрес. Спеки без
// `apiPath` он считает НЕ рассуженными и печатает их число — молчание по ним не
// означает «законно».
//
// СПОСОБНОСТЬ УПАСТЬ доказана инъекцией в обе стороны (describe ниже): перечень
// исключений вместо перечня разрешений краснеет с координатой, а законный
// близнец — ресурс, чей контракт глагол ОБЪЯВЛЯЕТ, — молчит.

import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import { join, resolve } from "node:path";

import { stripComments } from "@shared/test/strip-comments";

const APP_DIR = process.cwd();
const REPO_ROOT = resolve(APP_DIR, "../..");
const UI_ROOT = resolve(APP_DIR, "..");
const PROTO_ROOT = join(REPO_ROOT, "proto", "kacho", "cloud");

/**
 * Модули, чьё меню строки ещё несёт СОБСТВЕННЫЙ перечень.
 *
 * Сведение пяти форков к одной реализации — предмет #560 (`release:deforking`),
 * и оно УЖЕ написано в своей линии: копии становятся ре-экспортом `shared`, после
 * чего перечень остаётся ровно один — тот, что судится ниже. Чинить чужой предмет
 * в этом изменении запрещено (две линии правили бы одни файлы), поэтому долг
 * НАЗВАН числом, а не замолчан.
 *
 * Послабление САМОИСТЕКАЕТ: запись про модуль, который больше своего перечня не
 * объявляет, — находка. Иначе оно пережило бы свой предмет и охраняло пустоту.
 */
const OWN_LIST_PENDING_DEFORKING = ["compute", "nlb", "registry", "storage"];

/** Модули, у которых в дереве есть своя копия меню строки. */
function menuModules(): string[] {
  return readdirSync(UI_ROOT)
    .filter((m) => existsSync(menuPath(m)) && existsSync(registryPath(m)))
    .sort();
}

const menuPath = (m: string) =>
  join(UI_ROOT, m, "src/components/molecules/RowActionsMenu/RowActionsMenu.tsx");
const registryPath = (m: string) => join(UI_ROOT, m, "src/lib/resource-registry.tsx");

// ── сторона контракта: у каких адресов объявлен глагол `:move` ───────────────

function protoFiles(dir: string, out: string[] = []): string[] {
  for (const entry of readdirSync(dir)) {
    const p = join(dir, entry);
    if (statSync(p).isDirectory()) protoFiles(p, out);
    else if (p.endsWith(".proto")) out.push(p);
  }
  return out;
}

/** REST-адреса, у которых контракт объявляет `:move`, без сегмента идентификатора. */
function movablePathsFromContract(): { paths: Set<string>; filesRead: number; verbs: number } {
  const files = protoFiles(PROTO_ROOT);
  const paths = new Set<string>();
  let verbs = 0;
  for (const f of files) {
    const src = stripComments(readFileSync(f, "utf8"));
    for (const m of src.matchAll(/(?:post|put|patch):\s*"([^"]*):move"/g)) {
      verbs++;
      paths.add(m[1].replace(/\/\{[^}]*\}$/, ""));
    }
  }
  return { paths, filesRead: files.length, verbs };
}

// ── сторона консоли: что реестр объявляет и что меню предлагает ─────────────

/** `id` спеки → её `apiPath` (первый `apiPath` после `id` — раскладка реестра плоская). */
function specsOf(module: string): Map<string, string | null> {
  const src = stripComments(readFileSync(registryPath(module), "utf8"));
  const specs = new Map<string, string | null>();
  let cur: string | null = null;
  for (const m of src.matchAll(/\n\s{2,6}(id|apiPath):\s*"([^"]+)"/g)) {
    if (m[1] === "id") {
      cur = m[2];
      if (!specs.has(cur)) specs.set(cur, null);
    } else if (cur && specs.get(cur) === null) {
      specs.set(cur, m[2]);
    }
  }
  return specs;
}

type MoveRule =
  | { kind: "allow" | "deny"; ids: Set<string>; ownList: true }
  /** Своего перечня нет: модуль берёт решение у общей реализации. */
  | { kind: "shared"; ids: Set<string>; ownList: false };

/**
 * Как модуль решает, предлагать ли перемещение.
 *
 * Обе формы засчитываются, потому что обе встречались в дереве: именованная
 * константа и массив, вписанный прямо в выражение (`nlb`). Гейт, знающий только
 * первую, объявил бы второй модуль чистым — а он предлагал заглушку на всех своих
 * ресурсах.
 */
function moveRuleOf(module: string): MoveRule {
  const src = stripComments(readFileSync(menuPath(module), "utf8"));
  const decl = /const\s+moveCapable\s*=\s*([\s\S]*?);/.exec(src);
  if (!decl) return { kind: "shared", ids: new Set(), ownList: false };
  const expr = decl[1].trim();
  const deny = expr.startsWith("!");

  let body: string | null = null;
  const inline = /\[([\s\S]*?)\]/.exec(expr);
  if (inline) body = inline[1];
  else {
    const named = /!?\s*([A-Za-z_][A-Za-z0-9_]*)\s*\.includes/.exec(expr);
    if (named) {
      const cm = new RegExp(`const\\s+${named[1]}\\s*=\\s*\\[([\\s\\S]*?)\\]`).exec(src);
      if (cm) body = cm[1];
    }
  }
  if (body === null) return { kind: "shared", ids: new Set(), ownList: false };
  const ids = new Set([...body.matchAll(/"([^"]+)"/g)].map((m) => m[1]));
  return { kind: deny ? "deny" : "allow", ids, ownList: true };
}

/** Ресурсы, которым модуль ПРЕДЛАГАЕТ перемещение. */
function offeredBy(module: string, rule: MoveRule): string[] {
  const specs = specsOf(module);
  const shared = module === "shared" ? null : moveRuleOf("shared");
  const effective = rule.ownList ? rule : (shared as MoveRule);
  return [...specs.keys()].filter((id) =>
    effective.kind === "deny" ? !effective.ids.has(id) : effective.ids.has(id),
  );
}

interface Finding {
  module: string;
  id: string;
}

function census() {
  const contract = movablePathsFromContract();
  const modules = menuModules();
  const findings: Finding[] = [];
  let offeredPairs = 0;
  let unjudged = 0;
  for (const m of modules) {
    const specs = specsOf(m);
    for (const id of offeredBy(m, moveRuleOf(m))) {
      offeredPairs++;
      const apiPath = specs.get(id);
      if (!apiPath) {
        unjudged++;
        continue;
      }
      if (!contract.paths.has(apiPath)) findings.push({ module: m, id });
    }
  }
  return { contract, modules, findings, offeredPairs, unjudged };
}

// ── перепись дерева ─────────────────────────────────────────────────────────

describe("объём осмотренного — «ноль находок» отличимо от «ноль прочитанного»", () => {
  it("контракт прочитан, и глагол перемещения в нём вообще встречается", () => {
    const { filesRead, verbs, paths } = movablePathsFromContract();
    expect(filesRead).toBeGreaterThan(50);
    expect(verbs).toBeGreaterThan(0);
    expect(paths.size).toBeGreaterThan(0);
  });

  it("копии меню найдены — судить есть что", () => {
    expect(menuModules().length).toBeGreaterThanOrEqual(2);
  });

  it("перепись напечатана", () => {
    const c = census();
    // eslint-disable-next-line no-console
    console.log(
      `[перемещение] контракт: файлов ${c.contract.filesRead}, глаголов ${c.contract.verbs} ` +
        `(${[...c.contract.paths].join(", ")}) · копий меню ${c.modules.length} ` +
        `(со своим перечнем ${c.modules.filter((m) => moveRuleOf(m).ownList).join(", ") || "нет"}) · ` +
        `предложено пар ${c.offeredPairs}, не рассужено ${c.unjudged}, находок ${c.findings.length}`,
    );
    expect(true).toBe(true);
  });
});

// ── сам запрет ──────────────────────────────────────────────────────────────

describe("перемещение предлагается только там, где глагол объявлен", () => {
  it("общая реализация предлагает РОВНО то, что объявил контракт", () => {
    const contract = movablePathsFromContract();
    const specs = specsOf("shared");
    const offered = offeredBy("shared", moveRuleOf("shared"));
    const withoutVerb = offered.filter((id) => {
      const p = specs.get(id);
      return !p || !contract.paths.has(p);
    });
    expect(withoutVerb).toEqual([]);

    // Обратная сторона: объявленный глагол обязан ДОЕХАТЬ до меню. Иначе гейт
    // зеленел бы на реализации, которая не предлагает перемещение никому.
    const declared = [...specs.entries()]
      .filter(([, p]) => p && contract.paths.has(p))
      .map(([id]) => id);
    expect(declared.length).toBeGreaterThan(0);
    expect([...offered].sort()).toEqual(declared.sort());
  });

  it("перечень строится РАЗРЕШЕНИЕМ, а не исключением", () => {
    // Умолчание и есть предмет задачи: при перечне исключений новый ресурс
    // получает заглушку сам, и автор ресурса об этом не узнаёт.
    expect(moveRuleOf("shared").kind).toBe("allow");
  });

  it("модули со своим перечнем — ровно те, что названы долгом (послабление самоистекает)", () => {
    const own = menuModules().filter((m) => m !== "shared" && moveRuleOf(m).ownList);
    expect(own.sort()).toEqual([...OWN_LIST_PENDING_DEFORKING].sort());
  });
});

// ── инъекция: способность упасть и промолчать ───────────────────────────────

describe("инъекция: гейт краснеет на дефекте и молчит на законном близнеце", () => {
  const contract = movablePathsFromContract();
  const specs = specsOf("shared");
  const judged = (ids: string[]) =>
    ids.filter((id) => {
      const p = specs.get(id);
      return !p || !contract.paths.has(p);
    });

  it("контракт различает два ресурса — иначе дискриминатору нечего различать", () => {
    const withVerb = [...specs.entries()].filter(([, p]) => p && contract.paths.has(p));
    const withoutVerb = [...specs.entries()].filter(([, p]) => p && !contract.paths.has(p));
    expect(withVerb.length).toBeGreaterThan(0);
    expect(withoutVerb.length).toBeGreaterThan(0);
  });

  it("ДЕФЕКТ: ресурс без глагола в перечне разрешений — находка с координатой", () => {
    const victim = [...specs.entries()].find(([, p]) => p && !contract.paths.has(p))![0];
    expect(judged([victim])).toEqual([victim]);
  });

  it("ДЕФЕКТ: перечень ИСКЛЮЧЕНИЙ на месте перечня разрешений — находка", () => {
    // Та самая форма, что стояла в дереве: «предлагаем всем, кроме перечисленных».
    const denyList = new Set(["accounts", "projects"]);
    const offered = [...specs.keys()].filter((id) => !denyList.has(id));
    expect(judged(offered).length).toBeGreaterThan(0);
  });

  it("БЛИЗНЕЦ: ресурс, чей контракт глагол ОБЪЯВЛЯЕТ — молчание", () => {
    const lawful = [...specs.entries()].find(([, p]) => p && contract.paths.has(p))![0];
    expect(judged([lawful])).toEqual([]);
  });

  it("БЛИЗНЕЦ: пустой перечень — молчание, но и доезда глагола тогда нет", () => {
    // Пустой перечень находок не даёт: он никому ничего не предлагает. Именно
    // поэтому запрет выше проверяется ПАРОЙ — «ничего лишнего» и «объявленное доехало».
    expect(judged([])).toEqual([]);
  });
});
