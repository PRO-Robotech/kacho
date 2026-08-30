// Проекция балансировщика в консоли — против ВХОДНЫХ полей его контракта.
//
// ПРЕДМЕТ. Поле, которое запрос принимает, а форма выразить не может, — это
// возможность, объявленная контрактом и неисполнимая из консоли: она
// задокументирована, покрыта типами, о ней спрашивают, и она не работает ни при
// каком вводе (`api-conventions.md` §«Неисполнимая возможность»). Класс тихий:
// обе стороны по отдельности исправны, расходятся они в третьем месте — в том,
// что видит арендатор.
//
// ИСТОЧНИК ИСТИНЫ — дерево контракта, а не перечень в этом файле: поле,
// добавленное в `Create*Request`, роняет пробу само, не дожидаясь, пока
// кто-нибудь вспомнит про форму.
//
// ВЕДОМОСТИ — ДВЕ, и обе истекают сами:
//   • ОТВЕРГАЕМЫЕ НА ЗАПИСЬ (`type`, `placement_type`) — сервис отвечает на них
//     явным InvalidArgument (NLB CONTRACT: `placement` — единственный
//     авторитетный ввод режима). От них требуется ОБРАТНОЕ: форма их слать не
//     вправе. Запись, чьего поля в контракте больше нет, — находка;
//   • НЕ ВЫРАЖАЕМЫЕ ОСОЗНАННО (`target_group_id`) — с причиной. Запись, чьё поле
//     форма начала выражать, — тоже находка: исключению нечего исключать.
//
// Перепись печатается в имени пробы, и пустой обход — отказ: «ноль находок»
// обязано быть отличимо от «ноль прочитанного».

import { readFileSync } from "node:fs";
import { join } from "node:path";

import { REGISTRY, applyFieldDefaults } from "./resource-registry";
import { computeUpdateMask } from "./update-mask";
import { PROTO_ROOT } from "@shared/test/oneof-branch-coverage";

const asObj = (v: unknown) => v as Record<string, unknown>;

/**
 * Имена полей верхнего уровня объявленного сообщения запроса — по тексту
 * контракта. Разбор узкий: именно это сообщение, именно его тело; вложенные
 * `oneof`/`message` в счёт не идут — предмет здесь верхний уровень тела запроса.
 *
 * Отсутствие сообщения — ОТКАЗ, а не пустой перечень: пустой означал бы «полей
 * нет» и зеленил бы форму любой бедности.
 */
function requestFields(relPath: string, message: string): string[] {
  const text = readFileSync(join(PROTO_ROOT, relPath), "utf8");
  const at = text.search(new RegExp(`^\\s*message\\s+${message}\\s*\\{`, "m"));
  if (at < 0) throw new Error(`в ${relPath} нет сообщения ${message} — контракт разошёлся с этой пробой`);
  const open = text.indexOf("{", at);
  let depth = 0;
  let close = open;
  for (let i = open; i < text.length; i += 1) {
    if (text[i] === "{") depth += 1;
    else if (text[i] === "}") {
      depth -= 1;
      if (depth === 0) {
        close = i;
        break;
      }
    }
  }
  const names: string[] = [];
  let nested = 0;
  for (const raw of text.slice(open + 1, close).split("\n")) {
    const line = raw.replace(/\/\/.*$/, "").trim();
    if (!line) continue;
    if (/^(oneof|message|enum)\b/.test(line) || (nested > 0 && line.includes("{"))) {
      nested += 1;
      continue;
    }
    if (nested > 0) {
      if (line.startsWith("}")) nested -= 1;
      continue;
    }
    // `map<string, string> labels = 4;` · `repeated string zones = 18;` ·
    // `NetworkLoadBalancer.Type type = 6;` — тип, имя, номер, опц. `[...]`.
    const m = /^(?:repeated\s+)?(?:map<[^>]+>|[A-Za-z_][\w.]*)\s+([a-z_]\w*)\s*=\s*\d+\s*(?:\[[^\]]*\])?\s*;/.exec(line);
    if (m) names.push(m[1]);
  }
  if (names.length === 0) throw new Error(`тело ${message} прочитано пустым — разбор разошёлся с контрактом`);
  return names;
}

/**
 * Ключи верхнего уровня, которые тело создания может нести.
 *
 * Считается ПРОГОНОМ той же цепочки, что исполняет страница создания
 * (`applyFieldDefaults` → `sanitize`), а не чтением объявления: составное поле
 * (`vip_source`) собирает в тело ветви под ДРУГИМИ именами (`v4_source`,
 * `v6_source`), и перечень имён полей о них не говорит ничего. Прогоны состояний
 * (`states`) — то, чем составное поле доказывает, что ветвь достижима: без них
 * «поле есть» означало бы «имя объявлено», а не «арендатор может это прислать».
 */
function createKeys(specId: string, states: readonly Record<string, unknown>[] = []): Set<string> {
  const spec = REGISTRY[specId];
  const seeded = applyFieldDefaults(spec.fields, asObj(spec.template({ projectId: "prj-1", accountId: "acc-1" })));
  const keys = new Set<string>();
  for (const edits of [{}, ...states]) {
    const obj = { ...seeded, ...edits };
    const body = spec.sanitize ? spec.sanitize(obj) : obj;
    for (const k of Object.keys(body)) keys.add(k);
  }
  for (const f of spec.fields ?? []) keys.add(f.name.split(".")[0]);
  return keys;
}

/** Каждый путь, который форма правки может положить в `update_mask`. */
function maskable(specId: string): Set<string> {
  const spec = REGISTRY[specId];
  const fields = spec.fields ?? [];
  const before: Record<string, unknown> = {};
  const after: Record<string, unknown> = {};
  for (const f of fields) {
    before[f.name.split(".")[0]] = "before";
    after[f.name.split(".")[0]] = "after";
  }
  return new Set(computeUpdateMask(before, after, fields).map((p) => p.split(".")[0]));
}

/** Колонки списка — по объявленному пути. */
function columnPaths(specId: string): Set<string> {
  return new Set((REGISTRY[specId].columns ?? []).map((c) => c.path).filter((p): p is string => !!p));
}

interface Projection {
  /** Идентификатор записи реестра. */
  specId: string;
  /** Файл контракта относительно `proto/kacho/cloud`. */
  proto: string;
  createMessage: string;
  updateMessage: string;
  /** Адресация и служебное: телом формы не являются. */
  routing: readonly string[];
  /** Сервис отвечает на них явным отказом — форма слать их НЕ вправе. */
  writeRejected: readonly { field: string; why: string }[];
  /** Не выражается осознанно — с причиной. */
  notExpressed: readonly { field: string; why: string }[];
  /**
   * Состояния формы, которыми достигаются ветви составных полей. Каждое —
   * законный ввод оператора, а не подгонка: состояние, которого он собрать не
   * может, доказывало бы выразимость, которой нет.
   */
  states?: readonly Record<string, unknown>[];
}

const PROJECTIONS: readonly Projection[] = [
  {
    specId: "load-balancers",
    proto: "loadbalancer/v1/network_load_balancer_service.proto",
    createMessage: "CreateNetworkLoadBalancerRequest",
    updateMessage: "UpdateNetworkLoadBalancerRequest",
    routing: ["network_load_balancer_id", "update_mask"],
    writeRejected: [
      {
        field: "type",
        why: "производная проекция `placement`; запрос принимает её лишь затем, чтобы выставивший получил явный InvalidArgument",
      },
      {
        field: "placement_type",
        why: "то же — производная проекция `placement`, write-reject",
      },
    ],
    notExpressed: [],
    states: [
      // Двойной стек: оператор включает ОБА семейства — так достигаются обе
      // ветви `v4_source` / `v6_source`, которые собирает одно поле формы.
      {
        placement: "INTERNAL_REGIONAL",
        vip_source: {
          _v4_mode: "subnet",
          v4: { subnet_id: "sub-abcdefghijklmnopq" },
          _v6_mode: "address",
          v6: { address_id: "adr-abcdefghijklmnopq" },
        },
      },
    ],
  },
  {
    specId: "listeners",
    proto: "loadbalancer/v1/listener_service.proto",
    createMessage: "CreateListenerRequest",
    updateMessage: "UpdateListenerRequest",
    routing: ["listener_id", "update_mask"],
    writeRejected: [],
    notExpressed: [
      {
        field: "target_group_id",
        why:
          "та же ссылка, что `default_target_group_id`: на фазе EXPAND поля сосуществуют и указывают на одну группу целей, " +
          "старшинство у `target_group_id`. Двумя полями формы один предмет назывался бы дважды, и арендатор выбирал бы " +
          "между ними без признака различия. Снимается вместе с фазой CONTRACT, когда одно из полей уйдёт из контракта.",
      },
    ],
  },
] as const;

describe("проекция балансировщика выражает входные поля контракта", () => {
  for (const p of PROJECTIONS) {
    const create = requestFields(p.proto, p.createMessage);
    const update = requestFields(p.proto, p.updateMessage);
    const rejected = new Set(p.writeRejected.map((e) => e.field));
    const skipped = new Set(p.notExpressed.map((e) => e.field));
    const routing = new Set(p.routing);

    const inputs = [...new Set([...create, ...update])].filter((f) => !routing.has(f));
    const mustExpress = inputs.filter((f) => !rejected.has(f) && !skipped.has(f));

    it(`${p.specId}: полей запроса прочитано ${inputs.length}, обязано выражаться ${mustExpress.length}`, () => {
      expect(inputs.length).toBeGreaterThan(0);
      const expressible = new Set([...createKeys(p.specId, p.states), ...maskable(p.specId)]);
      const missing = mustExpress.filter((f) => !expressible.has(f));
      expect(missing).toEqual([]);
    });

    it(`${p.specId}: правка выражает то, что несёт Update (полей ${update.filter((f) => !routing.has(f) && !rejected.has(f) && !skipped.has(f)).length})`, () => {
      const mutable = update.filter((f) => !routing.has(f) && !rejected.has(f) && !skipped.has(f));
      expect(mutable.length).toBeGreaterThan(0);
      const mask = maskable(p.specId);
      expect(mutable.filter((f) => !mask.has(f))).toEqual([]);
    });

    it(`${p.specId}: ведомость отвергаемых на запись (${p.writeRejected.length}) истекает сама`, () => {
      for (const e of p.writeRejected) {
        // Поле контракта, которого больше нет, — находка: запись пережила предмет.
        expect(new Set([...create, ...update]).has(e.field)).toBe(true);
        // И форма по-прежнему обязана его НЕ слать.
        expect(createKeys(p.specId, p.states).has(e.field)).toBe(false);
        expect(maskable(p.specId).has(e.field)).toBe(false);
        expect(e.why.length).toBeGreaterThan(20);
      }
    });

    it(`${p.specId}: ведомость невыражаемых (${p.notExpressed.length}) истекает сама`, () => {
      for (const e of p.notExpressed) {
        expect(new Set([...create, ...update]).has(e.field)).toBe(true);
        // Запись, чьё поле форма начала выражать, — исключение без предмета.
        expect(createKeys(p.specId, p.states).has(e.field)).toBe(false);
        expect(e.why.length).toBeGreaterThan(20);
      }
    });
  }

  it("разбор контракта различает — контроль в обе стороны", () => {
    expect(requestFields("loadbalancer/v1/listener_service.proto", "CreateListenerRequest")).toContain("port");
    expect(() =>
      requestFields("loadbalancer/v1/listener_service.proto", "НетТакогоСообщения"),
    ).toThrow(/нет сообщения/);
  });
});

describe("проекция балансировщика показывает то, что сервис возвращает", () => {
  // Колонка — единственное место, где значение доезжает до списка: карточка
  // читает его же, но список — то, с чего начинается всякая работа.
  it("список балансировщиков называет схему и резолвнутый VIP", () => {
    const paths = columnPaths("load-balancers");
    // `type` — производная проекция размещения, но именно она отвечает на вопрос
    // «внешний он или внутренний», с которого начинают.
    expect(paths).toContain("type");
    // VIP резолвится в связанный vpc Address; колонка ведёт на его карточку.
    expect(paths).toContain("v4_address_id");
  });

  it("список обработчиков называет порт, на который трафик уходит", () => {
    // `resolved_backend_port` — эхо порта группы целей: без него список
    // показывает, КУДА приходит трафик, и молчит о том, куда он уходит.
    expect(columnPaths("listeners")).toContain("resolved_backend_port");
  });

  it("карточка балансировщика ведёт на его обработчики", () => {
    // Связанный дочерний ресурс (within-service FK `load_balancer_id`):
    // без записи `related` вкладки нет, и путь «создал балансировщик → завёл
    // обработчик» из консоли не проходится.
    const related = REGISTRY["load-balancers"].related ?? [];
    expect(related.map((r) => r.childId)).toContain("listeners");
    expect(related.find((r) => r.childId === "listeners")?.filterField).toBe("load_balancer_id");
  });
});
