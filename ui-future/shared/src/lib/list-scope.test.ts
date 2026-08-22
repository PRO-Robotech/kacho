// Область списка: три значения, и каждое обязано звучать по-разному (#373).

import { clientScope, rowsAreComplete, searchPlaceholder, narrowingTitle, noMatchesText } from "./list-scope";
import type { NarrowingScope } from "./list-scope";

const ALL: NarrowingScope[] = ["server", "whole", "loaded"];

describe("область клиентской ручки выводится из дочитанности курсора", () => {
  it("за курсором есть страницы — сужается прочитанная часть", () => {
    expect(clientScope(true)).toBe("loaded");
  });

  it("курсор дочитан — сужается весь набор", () => {
    expect(clientScope(false)).toBe("whole");
  });

  it("курсора нет вовсе (undefined) — это тоже весь набор, а не «неизвестно»", () => {
    // Список, читаемый одним запросом, курсора не имеет. Прочтя `undefined` как
    // «неизвестно», страница вечно объявляла бы себя недочитанной и никогда не
    // предлагала бы порядок — то есть осторожность стала бы отказом в работе.
    expect(clientScope(undefined)).toBe("whole");
  });
});

describe("порядок предлагается ТОЛЬКО на полном наборе", () => {
  it("сужал сервер — набор на экране всё равно страница… но она полна", () => {
    // `server` означает, что отбор сделал владелец. Полнота набора при этом
    // решается курсором, и вызывающий обязан объявить `loaded`, если курсор не
    // дочитан: сам по себе серверный отбор порядок не оправдывает.
    expect(rowsAreComplete("server")).toBe(true);
  });

  it("прочитанная часть — порядок не предлагается", () => {
    expect(rowsAreComplete("loaded")).toBe(false);
  });

  it("весь набор — порядок предлагается", () => {
    expect(rowsAreComplete("whole")).toBe(true);
  });
});

describe("каждая область называется своими словами", () => {
  it.each(ALL)("плейсхолдер области %s непуст и называет область", (scope) => {
    expect(searchPlaceholder(scope).length).toBeGreaterThan(0);
    expect(searchPlaceholder(scope)).toMatch(/по всему списку|среди загруженных/);
  });

  it("три области дают три РАЗНЫХ плейсхолдера", () => {
    // Отрицание в паре с положительным: если бы подписи совпали, проба выше
    // осталась бы зелёной на списке, который врёт одинаково во всех трёх
    // состояниях.
    expect(new Set(ALL.map(searchPlaceholder)).size).toBe(3);
  });

  it("три области дают три РАЗНЫЕ подсказки", () => {
    expect(new Set(ALL.map(narrowingTitle)).size).toBe(3);
  });

  it("только над прочитанной частью промах поиска говорит про курсор", () => {
    expect(noMatchesText("loaded")).toMatch(/за курсором/);
    expect(noMatchesText("whole")).not.toMatch(/за курсором/);
    expect(noMatchesText("server")).not.toMatch(/за курсором/);
  });
});
