// The UI must not re-interpret what the backend told it.
//
// The gateway hides existence: a resource that exists but is not visible to the
// caller comes back as a NOT_FOUND byte-identical to a real miss. So a 404 is
// genuinely ambiguous, and the UI is not allowed to resolve that ambiguity in
// either direction — neither "this does not exist" nor "you have no access".
// A 403, by contrast, is unambiguous and must stay a 403.

import { ApiError } from "@shared/api/client";
import { NOT_FOUND_IS_AMBIGUOUS, presentError, QUOTA_SHOWCASE_HINT, errorText  } from "./error-presentation";

// ─────────────────────────────────────────────────────────────────────────────
// ОТКАЗ ПО ПРЕДЕЛУ (#1605)
//
// Отказ по исчерпанию предела попадал в общую ветку «прочий 4xx»: заголовок
// «Внимание» и английская строка производителя дословно — `project prj-1 has
// reached its limit of 5 vpc.network`. Вид назван машинным именем, кто задаёт
// величины — не сказано, куда идти — не сказано. Следующего шага у клиента не
// оставалось.
//
// ПОЧЕМУ КЛЮЧ — ТОКЕН ПРИЗНАКА, А НЕ HTTP-СТАТУС. Полос две, и они приходят
// РАЗНЫМИ статусами: «место кончилось» — `RESOURCE_EXHAUSTED` (429), «потолок не
// назван вовсе» — `FAILED_PRECONDITION` (400). Ключ по 429 потерял бы вторую
// полосу целиком, а она и есть та, где действие администратора другое: не
// поднять предел, а завести его.
import { QUOTA_VALUES_SET_BY } from "./quota-view";

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

/** Тело отказа в том виде, в каком его собирает край из `google.rpc.Status`. */
function quotaDetails(reason: string, metadata?: Record<string, string>) {
  return [
    {
      "@type": "type.googleapis.com/google.rpc.ErrorInfo",
      reason,
      domain: "vpc.kacho.cloud",
      ...(metadata ? { metadata } : {}),
    },
  ];
}

describe("отказ по пределу восстанавливает следующий шаг (#1605)", () => {
  it("«место кончилось» — по-русски, с именем вида, пределом и адресом витрины", () => {
    const p = presentError(
      new ApiError(429, 8, quotaDetails("QUOTA_EXCEEDED", {
        kind: "vpc.network",
        limit: "5",
        used: "5",
        carrier_type: "project",
        carrier_id: "prj-1",
      }), "project prj-1 has reached its limit of 5 vpc.network"),
    );

    expect(p.title).toBe("Предел исчерпан");
    // Вид назван ЧЕЛОВЕЧЕСКИМ именем из единственного словаря, а не `vpc.network`.
    expect(p.subTitle).toContain("Облачные сети");
    expect(p.subTitle).not.toContain("vpc.network");
    expect(p.subTitle).toContain("занято 5 из 5");
    // Кто задаёт величины — теми же словами, что на витрине.
    expect(p.subTitle).toContain(QUOTA_VALUES_SET_BY);
    // Действие администратора у этой полосы — ПОДНЯТЬ предел.
    expect(p.subTitle).toContain("поднять");
    // Куда идти.
    expect(p.note).toBe(QUOTA_SHOWCASE_HINT);
    expect(p.quota?.href).toBe("/projects/prj-1/quotas");
    // Текст сервера НЕ ПОТЕРЯН — он в подсказке, по образцу 403.
    expect(p.devDetail).toContain("project prj-1 has reached its limit of 5 vpc.network");
    expect(p.devDetail).toContain("RESOURCE_EXHAUSTED (8)");
  });

  it("«потолок не назван» — ДРУГАЯ полоса и другое действие, хотя статус 400", () => {
    const p = presentError(
      new ApiError(400, 9, quotaDetails("QUOTA_NOT_PROVISIONED", { kind: "vpc.subnet" }),
        "no limit provisioned for vpc.subnet"),
    );

    expect(p.quota?.lane).toBe("not_provisioned");
    expect(p.title).toBe("Предел не задан");
    expect(p.subTitle).toContain("Подсети");
    // Завести, а не поднять: свести полосы значило бы послать читателя искать,
    // что понизить, там, где ничего не назначено.
    expect(p.subTitle).toContain("завести");
    expect(p.subTitle).not.toContain("поднять");
    expect(p.note).toBe(QUOTA_SHOWCASE_HINT);
  });

  it("величин нет — всё равно по-русски и всё равно с адресом; вид НЕ выдумывается", () => {
    // Сегодня производители признак присылают, а величины — нет. Отказ, умеющий
    // показать только полный набор, на неполном показал бы хуже прежнего.
    const p = presentError(
      new ApiError(429, 8, quotaDetails("QUOTA_EXCEEDED"), "project prj-1 has reached its limit of 5 vpc.network"),
    );

    expect(p.title).toBe("Предел исчерпан");
    expect(p.quota?.kind).toBeNull();
    expect(p.quota?.limit).toBeNull();
    expect(p.subTitle).toContain(QUOTA_VALUES_SET_BY);
    expect(p.subTitle).not.toContain("has reached its limit");
    expect(p.note).toBe(QUOTA_SHOWCASE_HINT);
    // Носителя не назвали — адреса нет, и он не подделывается.
    expect(p.quota?.href).toBeNull();
  });

  it("занято НОЛЬ отличимо от «занято не назвали»", () => {
    // Ноль — законная величина. Приравняв её к отсутствию, столбец промолчал бы
    // там, где сервер сказал.
    const zero = presentError(
      new ApiError(400, 9, quotaDetails("QUOTA_NOT_PROVISIONED", { kind: "vpc.network", used: "0" }), "x"),
    );
    expect(zero.quota?.used).toBe(0);

    const absent = presentError(
      new ApiError(400, 9, quotaDetails("QUOTA_NOT_PROVISIONED", { kind: "vpc.network" }), "x"),
    );
    expect(absent.quota?.used).toBeNull();
  });

  it("незнакомый вид показывается СВОИМ именем, а не прячется", () => {
    const p = presentError(
      new ApiError(429, 8, quotaDetails("QUOTA_EXCEEDED", { kind: "future.widget" }), "x"),
    );
    expect(p.subTitle).toContain("future.widget");
  });

  it("текст для тоста — тоже русский, а не строка производителя", () => {
    const t = errorText(
      new ApiError(429, 8, quotaDetails("QUOTA_EXCEEDED", { kind: "vpc.network" }),
        "project prj-1 has reached its limit of 5 vpc.network"),
    );
    expect(t).toContain("Облачные сети");
    expect(t).not.toContain("has reached its limit");
    // Тост — самая частая поверхность этого отказа (мутации сообщают о себе
    // именно им), и «куда идти» обязано быть в НЁМ, а не только на экране
    // отказа: иначе половина фикса не доезжает до большинства случаев.
    expect(t).toContain(QUOTA_SHOWCASE_HINT);
  });

  it("тост обычного отказа оговорок НЕ обрастает — контроль к утверждению выше", () => {
    // Без пары «тост уводит на витрину» зеленело бы и на реализации, которая
    // приклеивает оговорку к КАЖДОМУ отказу.
    expect(errorText(new ApiError(404, 5, null, "Network net-1 not found"))).toBe("Network net-1 not found");
  });

  // ─── ПОЛОЖИТЕЛЬНЫЕ КОНТРОЛИ ────────────────────────────────────────────────
  // Без них «отказ по пределу переодет» зеленело бы и на распознавателе,
  // который переодевает ЛЮБОЙ 429 и любой 400.

  it("отсечка запросов — тоже 429, но НЕ отказ по пределу", () => {
    const p = presentError(new ApiError(429, 8, null, "too many authorization checks; retry later"));
    expect(p.quota).toBeNull();
    expect(p.title).toBe("Внимание");
    expect(p.subTitle).toBe("too many authorization checks; retry later");
  });

  it("обычный 400 остаётся собой", () => {
    const p = presentError(new ApiError(400, 3, null, "Illegal argument cidr"));
    expect(p.quota).toBeNull();
    expect(p.subTitle).toBe("Illegal argument cidr");
    expect(p.note).toBeNull();
  });

  it("чужой признак в тех же деталях отказом по пределу не считается", () => {
    const p = presentError(new ApiError(400, 9, quotaDetails("PEER_RESOURCE_STATE"), "subnet is not ready"));
    expect(p.quota).toBeNull();
    expect(p.subTitle).toBe("subnet is not ready");
  });
});

// ─────────────────────────────────────────────────────────────────────────────
// ПОЛОСА РАЗЛИЧАЕТСЯ ПРИЗНАКОМ, А НЕ ПРОЗОЙ (#1736)
//
// Разбор отказа по пределу выше ключуется на `reason` — и объясняет, почему:
// «вывод вида из английской фразы молча вернул бы пустоту при первой же смене
// тона». Полоса отказа в правах в том же файле делала ровно это, чего соседний
// разбор не велит: решала по английской подстроке `permission denied`.
//
// Тон сообщений — ЧАСТЬ КОНТРАКТА и меняется осознанно. Пока полоса ключуется
// прозой, любая такая правка молча возвращает арендатору внутреннее имя
// проверки вместо объяснения — то есть ровно то состояние, ради устранения
// которого объяснение и написано, и ни одно утверждение об экране не краснеет.
//
// Признак `AUTHZ_DENIED` для этого уже производится краем и службой личности —
// он просто не читался.

/** Тело отказа с признаком; домен назван, потому что его называет производитель. */
function refusalDetails(reason: string, domain: string, metadata?: Record<string, string>) {
  return [
    {
      "@type": "type.googleapis.com/google.rpc.ErrorInfo",
      reason,
      domain,
      ...(metadata ? { metadata } : {}),
    },
  ];
}

describe("полоса отказа читается по признаку, а не по английской прозе (#1736)", () => {
  it("AUTHZ_DENIED объясняется, даже когда в тексте НЕТ фразы, на которую опирался разбор", () => {
    // Ключевое: сообщение не содержит ни `permission denied`, ни точечного имени
    // права — то есть прозаический разбор его НЕ поймает. Признак поймает.
    // Это и есть смена тона, которая сегодня проходит молча.
    const p = presentError(
      new ApiError(403, 7, refusalDetails("AUTHZ_DENIED", "kaname.cloud.iam.v1", {
        action: "iam.limits.list",
        resource: "project:prj-1",
      }), "доступ к iam.limits.list закрыт"),
    );

    expect(p.title).toBe("Недостаточно прав");
    expect(p.subTitle).toContain("администратор");
    // Внутреннее имя проверки на экран не попадает ни в каком написании.
    expect(p.subTitle).not.toContain("iam.limits.list");
    // Текст сервера НЕ ПОТЕРЯН — он в подсказке, по образцу остальных полос.
    expect(p.devDetail).toContain("доступ к iam.limits.list закрыт");
  });

  it("403 БЕЗ признака и с осмысленным текстом показывается дословно — контроль", () => {
    // Парный положительный: без него утверждение выше зеленело бы и на подмене
    // ЛЮБОГО отказа общей фразой, то есть на потере настоящей причины.
    const p = presentError(new ApiError(403, 7, null, "Аккаунт заблокирован администратором"));

    expect(p.subTitle).toBe("Аккаунт заблокирован администратором");
  });

  it("AUTHN_REQUIRED говорит, что делать, вместо английской строки края", () => {
    // Край отвечает `unauthenticated: credentials required`. Арендатору это не
    // сообщает ни что произошло, ни что делать: сессия истекла — надо войти.
    const p = presentError(
      new ApiError(401, 16, refusalDetails("AUTHN_REQUIRED", "kaname.cloud.iam.v1"),
        "unauthenticated: credentials required"),
    );

    expect(p.subTitle).not.toBe("unauthenticated: credentials required");
    expect(p.subTitle).toContain("Войдите заново");
    expect(p.devDetail).toContain("unauthenticated: credentials required");
  });

  it("QUOTA_RATE_EXCEEDED — ТРЕТЬЯ полоса: ждёт САМ вызывающий, а не администратор", () => {
    // Производитель завёл её отдельным признаком осознанно и записал почему:
    // «повтор запроса по объёму не пройдёт никогда, повтор по темпу пройдёт в
    // следующем окне». Свести её с `QUOTA_EXCEEDED` значило бы послать клиента
    // поднимать предел там, где надо просто подождать.
    const p = presentError(
      new ApiError(429, 8, refusalDetails("QUOTA_RATE_EXCEEDED", "iam.kacho.cloud", { kind: "iam.user" }),
        "rate limit exceeded for iam.user"),
    );

    expect(p.quota?.lane).toBe("rate_exceeded");
    expect(p.subTitle).toContain("Повторите через несколько секунд");
    // Действие принадлежит ВЫЗЫВАЮЩЕМУ: про поднятие предела здесь не говорится.
    expect(p.subTitle).not.toContain("поднять");
    expect(p.subTitle).not.toContain("завести");
    expect(p.devDetail).toContain("rate limit exceeded for iam.user");
  });

  it("признак, по которому решено показывать текст сервера, его и показывает", () => {
    // `passthrough` — ЗАКОННЫЙ вердикт, а не пропуск: текст производителя уже
    // называет следующий шаг («другой префикс, а не починка кодировщика»).
    // Утверждается именно это, иначе «вердикт есть» ничем не отличалось бы от
    // «вердикта нет».
    const p = presentError(
      new ApiError(400, 3, refusalDetails("SUBNET_CIDR_RESERVED", "vpc.kacho.cloud"),
        "cidrBlock 10.0.0.0/8 overlaps an address range reserved by the platform"),
    );

    expect(p.subTitle).toBe("cidrBlock 10.0.0.0/8 overlaps an address range reserved by the platform");
    expect(p.quota).toBeNull();
  });
});
