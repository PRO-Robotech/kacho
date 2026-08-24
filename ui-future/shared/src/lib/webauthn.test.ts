import { bufferToBase64Url } from "./webauthn";

const bufferOf = (...bytes: number[]) => new Uint8Array(bytes).buffer;

describe("bufferToBase64Url", () => {
  it("кодирует байты в base64url", () => {
    // 0x00 0x01 0x02 → "AAEC" в обычном base64, выравнивания не требует.
    expect(bufferToBase64Url(bufferOf(0x00, 0x01, 0x02))).toBe("AAEC");
  });

  it("снимает выравнивающие знаки", () => {
    // Один байт дал бы "AA==" — в base64url хвоста быть не должно.
    expect(bufferToBase64Url(bufferOf(0x00))).toBe("AA");
    expect(bufferToBase64Url(bufferOf(0x00, 0x00))).toBe("AAA");
  });

  it("заменяет знаки, недопустимые в адресе", () => {
    // 0xFB 0xEF 0xFE → "++/+" в обычном base64: покрывает обе замены сразу.
    const encoded = bufferToBase64Url(bufferOf(0xfb, 0xef, 0xfe));
    expect(encoded).toBe("--_-");
    expect(encoded).not.toMatch(/[+/=]/);
  });

  it("пустой буфер даёт пустую строку", () => {
    expect(bufferToBase64Url(new ArrayBuffer(0))).toBe("");
  });
});
