import { apiErrorFromBody } from "@shared/api/client";
import { GrpcCode, grpcCodeOf, grpcCodeLabel } from "@shared/lib/grpc-status";

/**
 * Код ответа края — ЧИСЛО, и разбор обязан читать его таким, каким он приходит.
 *
 * # Класс
 *
 * Край собирает REST-ответ протоджейсоном из `google.rpc.Status`, у которого
 * `code` объявлен `int32`. В JSON это число: `{"code":5,"message":"Not Found"}`.
 * Дерево это уже знает в одном месте и не знает в другом — `Operation.error.code`
 * объявлен `number`, а `ApiError.code` объявлялся `string`. TypeScript расхождения
 * не ловит: объявление типа он принимает на веру, а тело ответа приходит из
 * `JSON.parse`, то есть как `unknown`.
 *
 * Следствие было двойным: строгое сравнение со строковым литералом (`code === "7"`)
 * ложно ВСЕГДА, а пользователю числовой код попадал в текст сообщения.
 *
 * # Тела ответов здесь — НЕ выдуманные
 *
 * Каждое взято из дерева продукта дословно; координата названа у каждого. Проба,
 * кормящая разбор объектом собственного сочинения, закрепляет ответ разбора на
 * входе, которого не бывает, — ровно так строковое сравнение и прожило до сих пор
 * (прежние пробы клиента подавали `{"code":"ALREADY_EXISTS"}` и `{"code":"NOT_FOUND"}`).
 */

/** `services/iam/tests/newman/cases/iam-internal-only-check.py` — промах маршрута края. */
const EDGE_ROUTING_MISS = '{"code":5,"message":"Not Found"}';

/** `services/nlb/docs/content/api/operations.mdx` — ошибка внутри Operation. */
const OPERATION_FAILED_PRECONDITION = { code: 9, message: "Subnet <id> not found", details: [] };

describe("grpcCodeOf читает код в той форме, в какой он приходит", () => {
  it("число из тела края", () => {
    const err = apiErrorFromBody(404, "Not Found", EDGE_ROUTING_MISS);
    expect(grpcCodeOf(err)).toBe(GrpcCode.NotFound);
  });

  it("ошибка внутри Operation (HTTP 200, решает только код)", () => {
    expect(grpcCodeOf(OPERATION_FAILED_PRECONDITION)).toBe(GrpcCode.FailedPrecondition);
  });

  it("имя кода — тоже законная форма записи", () => {
    expect(grpcCodeOf({ code: "PERMISSION_DENIED" })).toBe(GrpcCode.PermissionDenied);
  });

  it("числовая строка — тоже", () => {
    expect(grpcCodeOf({ code: "7" })).toBe(GrpcCode.PermissionDenied);
  });

  // Отрицание в паре с положительным: без этих трёх «код распознан» означало бы
  // лишь, что распознаватель отвечает хоть что-то на любой вход.
  it("не выдумывает код там, где его нет", () => {
    expect(grpcCodeOf(null)).toBeNull();
    expect(grpcCodeOf({})).toBeNull();
    expect(grpcCodeOf({ code: "не код" })).toBeNull();
  });

  it("не принимает значение вне словаря кодов", () => {
    expect(grpcCodeOf({ code: 99 })).toBeNull();
    expect(grpcCodeOf({ code: -1 })).toBeNull();
  });

  it("подпись для разработчика называет и имя, и число", () => {
    const err = apiErrorFromBody(404, "Not Found", EDGE_ROUTING_MISS);
    expect(grpcCodeLabel(err)).toBe("NOT_FOUND (5)");
    expect(grpcCodeLabel({ code: 999 })).toBeNull();
  });
});
