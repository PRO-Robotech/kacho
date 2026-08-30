// Выбранный адрес, когда край перестал его отдавать.
//
// ПРЕДМЕТ. Список адресов сужается на стороне владельца — по вводу оператора и
// по странице ответа. Уже сделанный выбор в этот ответ попадать не обязан, а
// значение формы от сужения СПИСКА не зависит. Значит каскадер обязан вернуть
// выбранный адрес в варианты сам: иначе он показывает сырой `adr-…` вместо
// имени (канон консоли, правило 2 `ui.md`), а ветка «сеть → адрес» схлопывается.
//
// ПОЧЕМУ ЧИСТЫМИ ФУНКЦИЯМИ, А НЕ РЕНДЕРОМ. В рендере «выбора нет в вариантах» и
// «запрос ещё не вернулся» дают одну и ту же картинку, поэтому проба на рендере
// зеленела бы на незагруженных данных. Здесь спрашивается именно решение.
//
// Разбор пришёл сюда вместе с компонентом (#1471, #408): пока он жил в модуле
// `nlb`, ни этой пробы, ни линта `shared` над ним не было.

import { optionsWithKept, keptOf, type CascaderGroup, type KeptAddress } from "./AddressVpcCascader";

const netName = new Map<string, string>([["net-1", "прод"]]);
const kept: KeptAddress = { id: "adr-1", groupKey: "net-1", label: "vip-прод · 10.0.0.7" };

const groupOf = (value: string, children: { value: string; label: string }[]): CascaderGroup => ({
  value,
  label: `Сеть · ${netName.get(value) ?? value}`,
  children,
});

describe("keptOf — что именно возвращать в список", () => {
  it("возвращает выбор, когда край его потерял", () => {
    expect(keptOf("adr-1", undefined, kept)).toEqual(kept);
  });

  it("НЕ возвращает, пока адрес есть в ответе — подпись берётся из свежего ответа", () => {
    // Положительный близнец отрицания ниже: если возвращать всегда,
    // переименованный адрес показывался бы старым именем до перезагрузки.
    expect(keptOf("adr-1", { groupKey: "net-1", label: "новое имя" }, kept)).toBeNull();
  });

  it("НЕ возвращает чужой выбор — память относится к ОДНОМУ адресу", () => {
    expect(keptOf("adr-2", undefined, kept)).toBeNull();
  });

  it("без выбора возвращать нечего", () => {
    expect(keptOf(undefined, undefined, kept)).toBeNull();
    expect(keptOf("adr-1", undefined, null)).toBeNull();
  });
});

describe("optionsWithKept — куда возвращённый выбор встаёт", () => {
  it("в СВОЮ группу, если она в ответе осталась, и первым в ней", () => {
    const out = optionsWithKept([groupOf("net-1", [{ value: "adr-9", label: "другой" }])], kept, netName);
    expect(out).toHaveLength(1);
    expect(out[0].children.map((c) => c.value)).toEqual(["adr-1", "adr-9"]);
    // Подпись — та, что была на момент выбора, а не идентификатор.
    expect(out[0].children[0].label).toBe(kept.label);
  });

  it("своей группой, если ответ унёс и её — иначе ветка схлопывается", () => {
    const out = optionsWithKept([groupOf("net-2", [{ value: "adr-9", label: "другой" }])], kept, netName);
    expect(out.map((g) => g.value)).toEqual(["net-1", "net-2"]);
    expect(out[0].label).toBe("Сеть · прод");
    expect(out[0].children).toEqual([{ value: kept.id, label: kept.label }]);
  });

  it("ничего не возвращает, когда возвращать нечего — отрицание в паре с положительным", () => {
    const options = [groupOf("net-1", [{ value: "adr-9", label: "другой" }])];
    expect(optionsWithKept(options, null, netName)).toBe(options);
  });

  it("чужие группы не переписываются — правка касается ровно одной", () => {
    const other = groupOf("net-2", [{ value: "adr-8", label: "чужой" }]);
    const out = optionsWithKept([groupOf("net-1", []), other], kept, netName);
    expect(out.find((g) => g.value === "net-2")).toBe(other);
  });
});
