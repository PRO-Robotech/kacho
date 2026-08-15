import { apiErrorFromBody } from "@shared/api/client";
import { errorText, presentError } from "@shared/lib/error-presentation";
import { isAlreadyExistsError, isPermissionDeniedError } from "@shared/lib/permissions";

/**
 * Пользователю показывается СООБЩЕНИЕ, а не номер кода.
 *
 * # Класс
 *
 * `${err.code}: ${err.message}` давало на экране «5: Region not found» и
 * «9: network is not empty». Числовой код gRPC — величина протокола: он ничего
 * не сообщает тому, кто читает экран, и при этом занимает начало строки, то есть
 * место, куда смотрят первым. Его адресат — разработчик, и место ему в подробностях.
 *
 * # Тела ответов здесь — НЕ выдуманные
 *
 * Взяты из дерева продукта дословно, координаты названы. Это и есть предмет:
 * прежние пробы кормили разбор строковым кодом (`{"code":"NOT_FOUND"}`), которого
 * край не производит, и оттого не могли упасть ни на числе в тексте, ни на мёртвом
 * сравнении со строкой.
 */

/** `services/iam/tests/newman/cases/iam-invite-grant-fga.py` — промах маршрута края. */
const EDGE_ROUTING_MISS = '{"code":5,"message":"Not Found"}';

/** `services/iam/docs/content/api/operations.mdx` — отказ по состоянию ресурса. */
const NOT_EMPTY = '{"code":9,"message":"account has non-empty projects","details":[]}';

/** `services/nlb/docs/content/api/operations.mdx` — ошибка внутри Operation (HTTP 200). */
const OPERATION_ERROR_DENIED = { code: 7, message: "no path", details: [] };

/** Отказ в правах на пути чтения — `google.rpc.Status` с `code` = 7. */
const DENIED = '{"code":7,"message":"no path","details":[]}';

describe("текст для пользователя не несёт числового кода", () => {
  it("подпись 404 — сообщение сервера дословно, без номера", () => {
    const p = presentError(apiErrorFromBody(404, "Not Found", EDGE_ROUTING_MISS));
    expect(p.subTitle).toBe("Not Found");
    expect(p.subTitle).not.toMatch(/^\d/);
  });

  it("подпись 400 — сообщение сервера дословно, без номера", () => {
    const p = presentError(apiErrorFromBody(400, "Bad Request", NOT_EMPTY));
    expect(p.subTitle).toBe("account has non-empty projects");
  });

  it("errorText для тоста — то же сообщение и ничего сверх", () => {
    expect(errorText(apiErrorFromBody(404, "Not Found", EDGE_ROUTING_MISS))).toBe("Not Found");
    expect(errorText(new Error("сеть отвалилась"))).toBe("сеть отвалилась");
  });

  it("код остаётся, но в подробностях для разработчика", () => {
    const p = presentError(apiErrorFromBody(404, "Not Found", EDGE_ROUTING_MISS));
    expect(p.devDetail).toBe("NOT_FOUND (5) · HTTP 404");
  });

  // Положительный контроль к предыдущему: без него «кода нет в подписи» было бы
  // выполнено и тем, что подписи нет вовсе.
  it("подпись не пуста там, где сервер прислал текст", () => {
    const p = presentError(apiErrorFromBody(400, "Bad Request", NOT_EMPTY));
    expect(p.subTitle).not.toBeNull();
    expect((p.subTitle ?? "").length).toBeGreaterThan(0);
  });

  it("ответ без разбираемого кода не выдумывает подробность", () => {
    const p = presentError(apiErrorFromBody(502, "Bad Gateway", "upstream connect error"));
    expect(p.devDetail).toBe("HTTP 502");
  });
});

describe("ветка отказа в правах читает код края", () => {
  it("отказ в правах на пути чтения", () => {
    expect(isPermissionDeniedError(apiErrorFromBody(403, "Forbidden", DENIED))).toBe(true);
  });

  // Здесь HTTP-статус не решает ничего: ответ — 200, отказ лежит внутри Operation.
  // Именно на этом входе прежнее сравнение `err.code === "7"` молчало.
  it("отказ в правах внутри Operation (HTTP 200)", () => {
    expect(isPermissionDeniedError(OPERATION_ERROR_DENIED)).toBe(true);
  });

  it("занятое имя внутри Operation (HTTP 200)", () => {
    expect(isAlreadyExistsError({ code: 6, message: "network exists" })).toBe(true);
  });

  // Отрицание в паре: иначе «true на всё» прошло бы обе проверки выше.
  it("чужой отказ отказом в правах не считается", () => {
    expect(isPermissionDeniedError(apiErrorFromBody(404, "Not Found", EDGE_ROUTING_MISS))).toBe(false);
    expect(isAlreadyExistsError({ code: 9, message: "network is not empty" })).toBe(false);
    expect(isPermissionDeniedError(null)).toBe(false);
    expect(isAlreadyExistsError("строка")).toBe(false);
  });
});
