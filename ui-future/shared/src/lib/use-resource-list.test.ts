// Where the parent id of a list goes: into the path or into the query.
//
// Some list paths are child paths — `/registry/v1/registries/{registryId}/repositories`
// — and the backend scopes them by the path segment, not by a query parameter.
// A hook that only ever appends the parent to the query leaves the `{registryId}`
// literal in the URL, so the request goes out as written and the backend answers
// InvalidArgument. The guard matters just as much: until the parent is known the
// request must not go out at all, or every poll spends a 400.
//
// This lives next to the hook as a pure function so the rule is testable without
// mounting react-query.

import { readAllPages, resolveListPath } from "./use-resource-list";
import type { CursorPage } from "./cursor-pages";

describe("resolveListPath", () => {
  it("puts the parent id in the query when the path has no placeholder for it", () => {
    expect(resolveListPath("/vpc/v1/subnets", "project_id", "prj-1")).toEqual({
      path: "/vpc/v1/subnets",
      query: { project_id: "prj-1" },
      resolved: true,
    });
  });

  it("substitutes the parent id into the path when the path names it", () => {
    // registry_id → {registryId}: snake_case field, camelCase placeholder.
    expect(resolveListPath("/registry/v1/registries/{registryId}/repositories", "registry_id", "reg-1")).toEqual({
      path: "/registry/v1/registries/reg-1/repositories",
      query: {},
      resolved: true,
    });
  });

  it("reports unresolved while any placeholder is still unfilled", () => {
    // The repositories list is reached with project_id as the parent, so
    // {registryId} stays unfilled — the request must not be issued.
    const out = resolveListPath("/registry/v1/registries/{registryId}/repositories", "project_id", "prj-1");
    expect(out.resolved).toBe(false);
    expect(out.path).toContain("{registryId}");
  });

  it("reports unresolved when there is no parent at all", () => {
    expect(resolveListPath("/registry/v1/registries/{registryId}/repositories", null, null).resolved).toBe(false);
  });

  it("leaves a plain path untouched when there is no parent", () => {
    expect(resolveListPath("/storage/v1/diskTypes", null, null)).toEqual({
      path: "/storage/v1/diskTypes",
      query: {},
      resolved: true,
    });
  });
});

// Пути с ДВУМЯ и более подстановками. Ребёнок второго уровня (теги репозитория)
// адресуется `/registry/v1/registries/{registryId}/repositories/{repository}/tags`:
// одного родительского идентификатора для него не хватает by construction, и
// «резолвить по одному фильтру» означает `resolved:false` навсегда — вкладка
// молчит, потому что запрос не уходит вовсе. Отличать надо не «запрос не удался»
// от «удался», а «подстановка заполнена» от «заполнить было нечем».
describe("resolveListPath: подстановок несколько", () => {
  const TAGS = "/registry/v1/registries/{registryId}/repositories/{repository}/tags";

  it("заполняет ВСЕ подстановки из именованных значений", () => {
    expect(resolveListPath(TAGS, null, null, { registryId: "reg-1", repository: "nginx" })).toEqual({
      path: "/registry/v1/registries/reg-1/repositories/nginx/tags",
      query: {},
      resolved: true,
    });
  });

  it("незаполненная подстановка оставляет путь неразрешённым — запрос не уходит", () => {
    // Заполнен ровно один из двух: это НЕ «почти готово», это негодный путь.
    const out = resolveListPath(TAGS, null, null, { registryId: "reg-1" });
    expect(out.resolved).toBe(false);
    expect(out.path).toContain("{repository}");
  });

  it("именованные значения принимаются и в snake_case, и в camelCase", () => {
    // Спека называет поля родителя в snake_case (`registry_id`), путь — в
    // camelCase. Требовать от вызывающего знать, какую форму ждёт резолвер,
    // значит завести второе правило об одном предмете.
    expect(resolveListPath(TAGS, null, null, { registry_id: "reg-1", repository: "nginx" }).path).toBe(
      "/registry/v1/registries/reg-1/repositories/nginx/tags",
    );
  });

  it("пустое значение подстановки НЕ считается заполнением", () => {
    // Пустая строка подставилась бы как `//`, и запрос ушёл бы по чужому адресу.
    const out = resolveListPath(TAGS, null, null, { registryId: "reg-1", repository: "" });
    expect(out.resolved).toBe(false);
    expect(out.path).toContain("{repository}");
  });

  it("именованные значения не отменяют прежний путь по фильтру", () => {
    // Положительный контроль на совместимость: одиночный фильтр продолжает
    // работать ровно как прежде, когда именованных значений не передали.
    expect(resolveListPath("/registry/v1/registries/{registryId}/repositories", "registry_id", "reg-1")).toEqual({
      path: "/registry/v1/registries/reg-1/repositories",
      query: {},
      resolved: true,
    });
  });

  it("лишнее именованное значение в query не уезжает", () => {
    // Оно адресует ПУТЬ. Утечка его в query добавила бы серверу параметр,
    // которого он не объявлял, и сузила бы список молча.
    expect(resolveListPath(TAGS, null, null, { registryId: "reg-1", repository: "nginx", zoneId: "ru-1a" }).query).toEqual(
      {},
    );
  });
});

// Дочитывание курсора до конца — `spec.loadAllPages`.
//
// Поле объявлено в ЕДИНОМ типе спеки, а читателей в `shared/` не было ни одного:
// умели дочитывать только копии в модулях. Сведение оболочки без этого читателя
// сняло бы возможность молча — фасетный фильтр судил бы по первой странице и
// отвечал «таких нет» про набор, которого не читал.
//
// Дочитывание живёт ВНУТРИ запроса, а не в эффекте. Эффект, зовущий продолжение
// на каждый ответ, вызывает себя же: в этой консоли такой цикл дважды убивал
// прогон по памяти, не оставив вердикта НИ ОДНОЙ пробе (`ui.md`, «Как проверить
// правку консоли»). Поэтому предел страниц — не украшение, а условие
// завершимости на сервере, который всегда отдаёт курсор.
describe("readAllPages: курсор дочитывается до конца", () => {
  const page = (rows: number[], token?: string): CursorPage => ({
    items: rows.map((n) => ({ id: `i-${n}` })),
    ...(token ? { next_page_token: token } : {}),
  });

  it("следует за курсором и склеивает все страницы", async () => {
    const seen: string[] = [];
    const out = await readAllPages(
      (token) => {
        seen.push(token);
        if (token === "") return Promise.resolve(page([1, 2], "t1"));
        if (token === "t1") return Promise.resolve(page([3, 4], "t2"));
        return Promise.resolve(page([5]));
      },
      "items",
    );
    expect(seen).toEqual(["", "t1", "t2"]);
    expect((out.items as { id: string }[]).map((r) => r.id)).toEqual(["i-1", "i-2", "i-3", "i-4", "i-5"]);
  });

  it("дочитанный набор не объявляет продолжения", async () => {
    // Иначе вызывающий покажет «Показать ещё» под полным списком.
    const out = await readAllPages(() => Promise.resolve(page([1])), "items");
    expect(out.next_page_token).toBeUndefined();
  });

  it("одна страница — один запрос (положительный контроль)", async () => {
    let calls = 0;
    await readAllPages(() => {
      calls++;
      return Promise.resolve(page([1]));
    }, "items");
    expect(calls).toBe(1);
  });

  it("строка, приехавшая дважды, не задваивается", async () => {
    // Первая страница продолжает поллиться, пока дочитываются следующие.
    const out = await readAllPages(
      (token) => Promise.resolve(token === "" ? page([1, 2], "t1") : page([2, 3])),
      "items",
    );
    expect((out.items as { id: string }[]).map((r) => r.id)).toEqual(["i-1", "i-2", "i-3"]);
  });

  it("сервер, всегда отдающий курсор, не вешает чтение", async () => {
    // Предел страниц обязан быть, иначе это бесконечный цикл в запросе.
    let calls = 0;
    const out = await readAllPages(
      () => {
        calls++;
        return Promise.resolve(page([calls], "всегда-ещё"));
      },
      "items",
      3,
    );
    expect(calls).toBe(3);
    // Чтение оборвано пределом — оно НЕ вправе выдавать себя за дочитанное:
    // курсор остаётся, вызывающий покажет продолжение.
    expect(out.next_page_token).toBe("всегда-ещё");
  });
});
