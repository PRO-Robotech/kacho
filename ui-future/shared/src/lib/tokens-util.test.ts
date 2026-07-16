import { MAX_TTL_DAYS, MAX_TTL_SECONDS, TTL_PRESETS, expiryState, ttlDaysToSeconds } from "./tokens-util";

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
