// Строка ответа API — `Record<string, unknown>`, и это не формальность типа:
// поля ресурсов Kachō бывают вложенными объектами (`internal_ipv4_address`),
// картами (`labels`) и списками. Консоль показывает такие поля через общий
// путь «значение → текст», и пока этим путём был `String(v)`, всякое
// не-скалярное значение доезжало до пользователя как `[object Object]` —
// то есть ячейка, поиск и сортировка утверждали об объекте одно и то же
// нечитаемое слово, одинаковое для ЛЮБОГО объекта.
//
// Проба фиксирует границу: скаляры проходят как были, объект/список
// показывают состав, отсутствие — пустую строку.

import { displayText } from "./display-text";

describe("displayText", () => {
  it("никогда не отдаёт [object Object]", () => {
    expect(displayText({ address: "10.0.0.5", zone_id: "ru-a" })).not.toContain("[object Object]");
    expect(displayText([{ a: 1 }, { b: 2 }])).not.toContain("[object Object]");
  });

  it("показывает состав объекта, а не его класс", () => {
    expect(displayText({ address: "10.0.0.5" })).toBe('{"address":"10.0.0.5"}');
    expect(displayText(["a", "b"])).toBe('["a","b"]');
  });

  it("оставляет скаляры как есть", () => {
    expect(displayText("net-1")).toBe("net-1");
    expect(displayText(42)).toBe("42");
    expect(displayText(false)).toBe("false");
  });

  it("отсутствие значения — пустая строка, а не слово null", () => {
    expect(displayText(null)).toBe("");
    expect(displayText(undefined)).toBe("");
  });

  it("переживает значение, которое нельзя сериализовать", () => {
    const cyclic: Record<string, unknown> = {};
    cyclic.self = cyclic;
    expect(displayText(cyclic)).toBe("");
    expect(displayText(() => undefined)).toBe("");
  });
});
