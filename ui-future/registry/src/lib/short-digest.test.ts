import { shortDigest } from "./short-digest";

// Digest в панели тегов сокращается до 9 символов hex-части (после `sha256:`).
describe("shortDigest", () => {
  it("берёт 9 символов hex-части после `sha256:`", () => {
    expect(shortDigest("sha256:793a57cec5ee88d1c38575cefc16cc65")).toBe("793a57cec");
  });
  it("без префикса — первые 9 символов", () => {
    expect(shortDigest("abcdef0123456789")).toBe("abcdef012");
  });
  it("короче 9 — как есть", () => {
    expect(shortDigest("sha256:abc")).toBe("abc");
  });
  it("пусто / не строка → пустая строка (рендерится «—»)", () => {
    expect(shortDigest("")).toBe("");
    expect(shortDigest(undefined)).toBe("");
    expect(shortDigest(42)).toBe("");
  });
});
