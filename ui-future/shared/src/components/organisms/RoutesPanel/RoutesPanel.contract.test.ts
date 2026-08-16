// Гейт: состав черновика маршрута обязан совпадать с составом `StaticRoute`
// контракта.
//
// Панель сохраняет маршруты ЗАМЕНОЙ всего списка (`static_routes` +
// `update_mask: "staticRoutes"`), поэтому любое поле контракта, которого нет в
// черновике, стирается у ВСЕХ строк — включая те, которых оператор не касался.
// Правка одной строки удаляет метки у остальных, и ни один ответ края об этом
// не сообщает: запрос успешен, операция завершается, данных нет.
//
// Поэтому проверяется не «есть ли поле X», а свойство: КАЖДОЕ поле, которое
// контракт объявляет у `StaticRoute`, обязано пережить круг
// `draftsFromRoutes → routesFromDrafts`. Следующее добавленное в контракт поле
// уронит этот гейт, не дожидаясь, пока его потерю заметит арендатор.
//
// Гейт читает САМ контракт, а не его копию в коде консоли: копия разошлась бы с
// оригиналом ровно так же молча, как черновик разошёлся с контрактом.
//
// Инъекция (доказано в обе стороны): убрать перенос `labels` из
// `draftsFromRoutes` — гейт краснеет и называет поле; законный код — молчит.

import { existsSync, readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";

import { draftsFromRoutes, routesFromDrafts } from "./RoutesPanel";

const CONTRACT = join("proto", "kacho", "cloud", "vpc", "v1", "route_table.proto");

/**
 * Контракт лежит в корне монорепо, а прогон идёт из `ui-future/<модуль>` —
 * и модулей, исполняющих пробы shared, три. Поэтому корень ищется подъёмом, а
 * не считается от известной глубины: промах по глубине дал бы «файл не найден»
 * там, где файл есть.
 */
function contractPath(): string {
  let dir = resolve(process.cwd());
  for (;;) {
    const candidate = join(dir, CONTRACT);
    if (existsSync(candidate)) return candidate;
    const parent = dirname(dir);
    if (parent === dir) break;
    dir = parent;
  }
  throw new Error(
    `не найден контракт ${CONTRACT} подъёмом от ${process.cwd()} — гейт не прочитал ничего и не вправе молчать`,
  );
}

interface ProtoField {
  name: string;
  /** `string` | `map` — из чего синтезируется значение для круга. */
  kind: "string" | "map";
  /** Имя `oneof`, если поле — его ветвь; иначе null. */
  oneof: string | null;
}

/** Поля message `StaticRoute` с разбором ветвей `oneof`. */
function parseStaticRouteFields(source: string): ProtoField[] {
  const start = source.indexOf("message StaticRoute {");
  if (start < 0) throw new Error("в контракте нет message StaticRoute — предпосылка гейта не выполнена");

  const fields: ProtoField[] = [];
  let depth = 0;
  let oneof: string | null = null;

  for (const raw of source.slice(start).split("\n")) {
    const line = raw.trim();
    if (line.startsWith("//")) continue;

    const oneofOpen = /^oneof\s+(\w+)\s*\{/.exec(line);
    if (oneofOpen) {
      oneof = oneofOpen[1];
      depth += 1;
      continue;
    }
    if (line.startsWith("message ") || line.endsWith("{")) {
      depth += 1;
      continue;
    }
    if (line.startsWith("}")) {
      depth -= 1;
      if (depth <= 1) oneof = null;
      if (depth === 0) break;
      continue;
    }

    const asMap = /^map<\s*[\w.]+\s*,\s*[\w.]+\s*>\s+(\w+)\s*=\s*\d+\s*;/.exec(line);
    if (asMap) {
      fields.push({ name: asMap[1], kind: "map", oneof });
      continue;
    }
    const asScalar = /^(?:repeated\s+)?([\w.]+)\s+(\w+)\s*=\s*\d+\s*;/.exec(line);
    if (asScalar && asScalar[1] === "string") {
      fields.push({ name: asScalar[2], kind: "string", oneof });
    }
  }
  return fields;
}

function sampleValue(f: ProtoField): unknown {
  return f.kind === "map" ? { [`k-${f.name}`]: `v-${f.name}` } : `v-${f.name}`;
}

const fields = parseStaticRouteFields(readFileSync(contractPath(), "utf8"));
const plain = fields.filter((f) => f.oneof === null);
const oneofNames = [...new Set(fields.filter((f) => f.oneof !== null).map((f) => f.oneof as string))];

/**
 * Ровно одна ветвь на каждый `oneof` — иначе набор непредставим в контракте.
 * Декартово произведение ветвей даёт все законные формы строки.
 */
function armCombinations(): ProtoField[][] {
  return oneofNames.reduce<ProtoField[][]>(
    (acc, name) => {
      const arms = fields.filter((f) => f.oneof === name);
      return acc.flatMap((combo) => arms.map((arm) => [...combo, arm]));
    },
    [[]],
  );
}

describe("состав черновика маршрута против контракта StaticRoute", () => {
  it("гейт прочитал контракт — перепись, а не «ноль находок»", () => {
    // Предпосылка самого гейта: разбор нашёл поля и ветви. Парсер, вернувший
    // пусто, молчал бы на любом дефекте.
    expect(fields.length).toBeGreaterThanOrEqual(4);
    expect(fields.map((f) => f.name)).toEqual(
      expect.arrayContaining(["destination_prefix", "next_hop_address", "gateway_id", "labels"]),
    );
    expect(oneofNames).toEqual(expect.arrayContaining(["destination", "next_hop"]));
    expect(plain.length).toBeGreaterThanOrEqual(1);
  });

  it.each(armCombinations().map((arms) => [arms.map((a) => a.name).join(" + "), arms] as const))(
    "круг «загрузили → сохранили» не теряет ни одного поля контракта (%s)",
    (_label, arms) => {
      const route: Record<string, unknown> = {};
      for (const f of [...plain, ...arms]) route[f.name] = sampleValue(f);

      const [saved] = routesFromDrafts(draftsFromRoutes([route]));

      const lost = Object.keys(route).filter((k) => !(k in (saved ?? {})));
      expect(lost).toEqual([]);
      expect(saved).toEqual(route);
    },
  );
});
