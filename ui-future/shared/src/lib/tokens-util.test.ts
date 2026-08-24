import {
  CREDENTIAL_KIND_KEYPAIR,
  CREDENTIAL_KIND_SECRET,
  MAX_TTL_DAYS,
  MAX_TTL_SECONDS,
  SECRET_RADIUS_NOTICE,
  SECRET_TTL_CEILING_DAYS,
  SECRET_TTL_CEILING_SECONDS,
  SECRET_TTL_DEFAULT_DAYS,
  SECRET_TTL_DEFAULT_SECONDS,
  TTL_PRESETS,
  credentialKindLabel,
  expiryState,
  maxTtlDaysFor,
  ttlDaysToSeconds,
  ttlPresetsFor,
} from "./tokens-util";

describe("tokens-util", () => {
  it("TTL-пресеты переводятся в ожидаемые ttl_seconds (0 = бессрочно)", () => {
    const by = Object.fromEntries(TTL_PRESETS.map((p) => [p.key, p.seconds]));
    expect(by["30d"]).toBe(2592000);
    expect(by["90d"]).toBe(7776000);
    expect(by["1y"]).toBe(31536000);
    expect(by["never"]).toBe(0);
  });

  it("ttlDaysToSeconds ограничивает диапазон proto и обнуляет непозитивные дни", () => {
    expect(ttlDaysToSeconds(30)).toBe(2592000);
    expect(ttlDaysToSeconds(MAX_TTL_DAYS)).toBe(MAX_TTL_SECONDS);
    expect(ttlDaysToSeconds(100000)).toBe(MAX_TTL_SECONDS);
    expect(ttlDaysToSeconds(0)).toBe(0);
    expect(ttlDaysToSeconds(-5)).toBe(0);
  });

  it("expiryState: без срока → бессрочный", () => {
    expect(expiryState(undefined).kind).toBe("none");
    expect(expiryState("").kind).toBe("none");
    expect(expiryState("not-a-date").kind).toBe("none");
  });

  it("expiryState: срок в прошлом → истек", () => {
    const past = new Date(Date.now() - 3600_000).toISOString();
    const st = expiryState(past);
    expect(st.kind).toBe("expired");
    expect(st.label).toBe("Истек");
  });

  it("expiryState: срок в будущем → «истекает через …»", () => {
    const now = Date.parse("2026-01-01T00:00:00Z");
    const future = new Date(now + 3 * 86400_000).toISOString();
    const st = expiryState(future, now);
    expect(st.kind).toBe("active");
    expect(st.label).toContain("истекает через");
  });
});

// ── Вид удостоверения: срок называется ЧИСЛОМ, «бессрочно» — только там, где
// это правда (#1235) ────────────────────────────────────────────────────────
//
// Контракт различает виды, и срок у них устроен ПО-РАЗНОМУ:
//   * KEYPAIR  — `ttl_seconds = 0` означает БЕССРОЧНО, потолок 2 года;
//   * SECRET   — бессрочного НЕ БЫВАЕТ ни в каком написании: ноль означает
//     «срок не назван» и разрешается умолчанием политики (30 суток), а срок
//     сверх потолка (90 суток) ОТВЕРГАЕТСЯ, а не урезается молча.
// Источник обеих величин — `pkg/tokenpolicy/policy.go`
// (`SecretCredentialTTLDefault` / `SecretCredentialTTLCeiling`).
describe("tokens-util — срок зависит от вида удостоверения", () => {
  it("величины срока секрета взяты у политики: умолчание 30 суток, потолок 90", () => {
    expect(SECRET_TTL_DEFAULT_DAYS).toBe(30);
    expect(SECRET_TTL_CEILING_DAYS).toBe(90);
    expect(SECRET_TTL_DEFAULT_SECONDS).toBe(30 * 86400);
    expect(SECRET_TTL_CEILING_SECONDS).toBe(90 * 86400);
  });

  it("у секрета НЕТ варианта «без срока», и потолок — его собственный", () => {
    const keys = ttlPresetsFor(CREDENTIAL_KIND_SECRET).map((p) => p.key);
    expect(keys).not.toContain("never");
    expect(ttlPresetsFor(CREDENTIAL_KIND_SECRET).every((p) => p.seconds > 0)).toBe(true);
    expect(maxTtlDaysFor(CREDENTIAL_KIND_SECRET)).toBe(SECRET_TTL_CEILING_DAYS);
  });

  // Положительный контроль к отрицанию выше: «без срока» не исчезло вообще, оно
  // осталось ровно там, где контракт его допускает. Без этой пары отрицание
  // зеленело бы на пустом перечне вариантов.
  it("у ключевой пары «без срока» ОСТАЁТСЯ — там оно правда, и потолок прежний", () => {
    const keys = ttlPresetsFor(CREDENTIAL_KIND_KEYPAIR).map((p) => p.key);
    expect(keys).toContain("never");
    expect(maxTtlDaysFor(CREDENTIAL_KIND_KEYPAIR)).toBe(MAX_TTL_DAYS);
  });

  it("пустой срок у секрета НЕ называется бессрочным — такой строки не бывает", () => {
    const st = expiryState(undefined, Date.now(), CREDENTIAL_KIND_SECRET);
    expect(st.kind).toBe("unknown");
    expect(st.label).not.toContain("ессрочн");
  });

  it("пустой срок у ключевой пары по-прежнему бессрочный", () => {
    expect(expiryState(undefined, Date.now(), CREDENTIAL_KIND_KEYPAIR).label).toBe("Бессрочный");
  });

  it("вид удостоверения называется словами клиента, а не именем с провода", () => {
    expect(credentialKindLabel(CREDENTIAL_KIND_SECRET)).toBe("Секрет");
    expect(credentialKindLabel(CREDENTIAL_KIND_KEYPAIR)).toBe("Ключевая пара");
    // Вид, которого край не назвал, не выдумывается: поле без источника не
    // показывается (канон консоли, правило 9).
    expect(credentialKindLabel(undefined)).toBe("");
  });

  it("радиус секрета назван клиенту прямо: не «доступ к реестру», а вся учётная запись", () => {
    expect(SECRET_RADIUS_NOTICE).toContain("реестр");
    expect(SECRET_RADIUS_NOTICE).toContain("учётн");
  });
});
