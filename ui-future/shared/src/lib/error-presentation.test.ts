// The UI must not re-interpret what the backend told it.
//
// The gateway hides existence: a resource that exists but is not visible to the
// caller comes back as a NOT_FOUND byte-identical to a real miss. So a 404 is
// genuinely ambiguous, and the UI is not allowed to resolve that ambiguity in
// either direction — neither "this does not exist" nor "you have no access".
// A 403, by contrast, is unambiguous and must stay a 403.

import { ApiError } from "@shared/api/client";
import { NOT_FOUND_IS_AMBIGUOUS, presentError } from "./error-presentation";

describe("presentError", () => {
  it("keeps the backend message verbatim (it is the contract tone)", () => {
    const p = presentError(new ApiError(404, 5, null, "Region ru-central1 not found"));
    expect(p.subTitle).toBe("Region ru-central1 not found");
    // Код протокола адресован не читателю экрана — он уходит в подробности.
    expect(p.devDetail).toBe("NOT_FOUND (5) · HTTP 404");
  });

  it("marks a 404 as ambiguous and never claims the resource is absent", () => {
    const p = presentError(new ApiError(404, 5, null, "Zone ru-central1-a not found"));
    expect(p.status).toBe("404");
    expect(p.ambiguousNotFound).toBe(true);
    expect(p.note).toBe(NOT_FOUND_IS_AMBIGUOUS);
    // The caveat states both possibilities and settles neither.
    expect(NOT_FOUND_IS_AMBIGUOUS).toContain("не существует");
    expect(NOT_FOUND_IS_AMBIGUOUS).toContain("недоступен");
  });

  it("does not soften a 403 into a 404, nor add the 404 caveat to it", () => {
    const p = presentError(new ApiError(403, 7, null, "no path"));
    expect(p.status).toBe("403");
    expect(p.ambiguousNotFound).toBe(false);
    expect(p.note).toBeNull();
  });

  it("does not promise access for a 401 either", () => {
    const p = presentError(new ApiError(401, 16, null, "missing token"));
    expect(p.status).toBe("warning");
    expect(p.ambiguousNotFound).toBe(false);
  });

  it("maps server failures and transport failures apart", () => {
    expect(presentError(new ApiError(503, 14, null, "peer down")).status).toBe("500");

    const netErr = new TypeError("Failed to fetch");
    const net = presentError(netErr);
    expect(net.status).toBe("500");
    expect(net.title).toBe("Сеть недоступна");
    expect(net.ambiguousNotFound).toBe(false);
  });

  it("falls back without inventing a status for a non-ApiError", () => {
    const p = presentError(new Error("boom"));
    expect(p.status).toBe("error");
    expect(p.subTitle).toBe("boom");
    expect(p.ambiguousNotFound).toBe(false);
  });

  it("has nothing to say when there is no error", () => {
    const p = presentError(undefined);
    expect(p.subTitle).toBeNull();
    expect(p.ambiguousNotFound).toBe(false);
  });
});

describe("отказ в правах не цитирует внутреннюю проверку", () => {
  it("имя права заменяется объяснением, а сам текст уходит в подсказку", () => {
    const p = presentError(new ApiError(403, 7, null, "permission denied: iam.limits.list"));

    expect(p.title).toBe("Недостаточно прав");
    // На экране — объяснение, а не имя проверки: оно называет внутреннее
    // устройство и не отвечает ни на один вопрос смотрящего.
    expect(p.subTitle).toContain("администратор");
    expect(p.subTitle).not.toContain("iam.limits.list");
    // Точный текст сервера НЕ ПОТЕРЯН — он в подсказке, откуда его достанет
    // поддержка. Без этого замена была бы сокрытием, а не переводом.
    expect(p.devDetail).toContain("iam.limits.list");
  });

  it("осмысленный отказ края показывается ДОСЛОВНО — контроль к утверждению выше", () => {
    // Без этой пары «имя проверки скрыто» зеленело бы и на подмене ЛЮБОГО
    // отказа общей фразой, то есть на потере настоящей причины.
    const p = presentError(new ApiError(403, 7, null, "Аккаунт заблокирован администратором"));

    expect(p.subTitle).toBe("Аккаунт заблокирован администратором");
  });

  it("отказ по другой причине заголовок не меняет", () => {
    expect(presentError(new ApiError(404, 5, null, "Network net-1 not found")).title).toBe("Не найдено");
  });
});
