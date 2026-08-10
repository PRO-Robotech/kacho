// Имена полей на проводе против имён, которыми говорит домен.
//
// Край отдаёт JSON в camelCase, а типы этого домена объявлены в snake_case:
// перевод в обе стороны делает `api/client.ts` (`lib/case.ts`). Пока перевод
// есть, расхождения не видно вовсе; сними его — и домен начнёт читать поля,
// которых в ответе нет, а тела запросов уедут с именами, которых край не
// принимает. Оба отказа МОЛЧАЛИВЫ: список просто пуст, правка просто без
// эффекта.
//
// Поэтому предмет пробы — сам перевод, наблюдаемый по кругу: что ушло на
// провод и что вернулось домену. Прежняя редакция вместо этого читала ШАПКУ
// файла типов и утверждала о её словах — то есть проверяла, что в комментарии
// написано то, что написано, и не могла упасть на снятом переводе.

import { jest } from "@jest/globals";

interface Captured {
  url: string;
  method: string;
  body: Record<string, unknown> | null;
}

const realFetch = globalThis.fetch;
let captured: Captured[] = [];

function stubFetch(reply: Record<string, unknown>) {
  captured = [];
  globalThis.fetch = ((input: RequestInfo | URL, init?: RequestInit) => {
    captured.push({
      url: String(input),
      method: init?.method ?? "GET",
      body: init?.body ? (JSON.parse(String(init.body)) as Record<string, unknown>) : null,
    });
    return Promise.resolve({
      ok: true,
      status: 200,
      statusText: "OK",
      text: () => Promise.resolve(JSON.stringify(reply)),
    } as Response);
  }) as typeof globalThis.fetch;
}

const { api } = await import("./client");

afterEach(() => {
  globalThis.fetch = realFetch;
  jest.clearAllMocks();
});

describe("перевод имён между доменом и проводом", () => {
  it("ответ края в camelCase домен получает в snake_case", async () => {
    stubFetch({ regionId: "ru-central1", createdAt: "2026-08-01T00:00:00Z", projectId: "prj-1" });

    const got = (await api.get<Record<string, unknown>>("/x/v1/things/thg-1")) as Record<string, unknown>;

    expect(got.region_id).toBe("ru-central1");
    expect(got.created_at).toBe("2026-08-01T00:00:00Z");
    // Имена провода домену не видны: иначе в дереве завелись бы обе формы
    // одного поля, и одна из них молча оставалась бы пустой.
    expect(got).not.toHaveProperty("regionId");
  });

  it("тело запроса из snake_case уходит на провод в camelCase", async () => {
    stubFetch({});

    await api.create("/x/v1/things", { region_id: "ru-central1", update_mask: "name", project_id: "prj-1" });

    const body = captured[0].body!;
    expect(body.regionId).toBe("ru-central1");
    expect(body.updateMask).toBe("name");
    expect(body).not.toHaveProperty("region_id");
  });

  it("перевод рекурсивен — вложенное и списки тоже", async () => {
    stubFetch({ healthCheck: { effectivePort: 8081, expectedCodes: "200-299" }, targetIds: ["tg-1"] });

    const got = (await api.get<Record<string, unknown>>("/x/v1/things/thg-1")) as Record<string, unknown>;

    expect((got.health_check as Record<string, unknown>).effective_port).toBe(8081);
    expect((got.health_check as Record<string, unknown>).expected_codes).toBe("200-299");
    expect(got.target_ids).toEqual(["tg-1"]);
  });

  it("имя без границы слов не трогается — контроль в обратную сторону", async () => {
    // Без него «перевод работает» удовлетворялось бы функцией, портящей ЛЮБОЕ
    // имя: односложные ключи обязаны доезжать как есть.
    stubFetch({ id: "thg-1", name: "web", status: "ACTIVE" });

    const got = (await api.get<Record<string, unknown>>("/x/v1/things/thg-1")) as Record<string, unknown>;

    expect(got.id).toBe("thg-1");
    expect(got.name).toBe("web");
    expect(got.status).toBe("ACTIVE");
  });
});
