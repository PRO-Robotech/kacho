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
// оригиналом ровно так же молча, как черновик разошёлся с контрактом. Разбор
// контракта живёт в `test/proto-contract` — общий с гейтом состава черновиков
// по всему дереву (`test/set-replacement-draft-composition`). Вторая копия
// разбора разошлась бы с первой так же тихо, как черновик с контрактом,
// поэтому здесь она не заводится.
//
// ОТЛИЧИЕ ОТ ТОГО ГЕЙТА, ради которого этот остаётся: тот сверяет ОБЪЯВЛЕННЫЙ
// СОСТАВ типов по всему дереву, а этот прогоняет ФУНКЦИИ круга — то есть ловит
// поле, которое тип называет, а перенос теряет.
//
// Инъекция (доказано в обе стороны): убрать перенос `labels` из
// `draftsFromRoutes` — гейт краснеет и называет поле; законный код — молчит.

import { parseMessageFields, readMessageFields, type ProtoField } from "@shared/test/proto-contract";

import { draftsFromRoutes, routesFromDrafts } from "./RoutesPanel";

const CONTRACT = "kacho/cloud/vpc/v1/route_table.proto";

/**
 * Поля, значение которых проба умеет синтезировать дословно. Круг «загрузили →
 * сохранили» подставляет строку и карту; поле другого рода в `StaticRoute` не
 * встречается, и появись оно — предпосылка ниже это назовёт.
 */
function synthesizable(f: ProtoField): boolean {
  return f.kind === "string" || f.kind === "map";
}

function sampleValue(f: ProtoField): unknown {
  return f.kind === "map" ? { [`k-${f.name}`]: `v-${f.name}` } : `v-${f.name}`;
}

const fields = readMessageFields(CONTRACT, "StaticRoute");
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
    // Круг подставляет значения только тем полям, которые умеет синтезировать.
    // Появление поля другого рода обязано быть ВИДНО здесь, а не молча сузить
    // проверяемый состав до подмножества.
    expect(fields.filter((f) => !synthesizable(f)).map((f) => f.name)).toEqual([]);
  });

  it("собственная предпосылка: разбор контракта читает то, что в контракте написано", () => {
    // Контроль в обе стороны на синтетике: сообщение без поля — поле не
    // появляется; с полем — появляется вместе со своей ветвью `oneof`.
    const WITHOUT = `message Sample {\n  oneof pick {\n    string a = 1;\n  }\n}\n`;
    const WITH = `message Sample {\n  oneof pick {\n    string a = 1;\n  }\n  map<string, string> labels = 2;\n}\n`;
    expect(parseMessageFields(WITHOUT, "Sample").map((f) => `${f.name}@${f.oneof}`)).toEqual(["a@pick"]);
    expect(parseMessageFields(WITH, "Sample").map((f) => `${f.name}@${f.oneof}`)).toEqual(["a@pick", "labels@null"]);
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
