// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import {
  AccessKeyEncodingError,
  assertionBodyOf,
  assertionRequestOf,
  base64urlOf,
  bytesOf,
} from "./access-key";

// Кодек церемонии ключа доступа (приёмка F8-S4, Р4; условия переписи К3, К5, К6).
//
// Кодек — чистый: без сети, без состояния, без журнала. Что он утверждает:
//   • base64url без дополнения — единственная форма в обе стороны (служба
//     принимает только её, `loginlanehttp/access_key_login.go`);
//   • параметры церемонии браузера берутся из ответа службы ДОСЛОВНО и только
//     названные — ничего не подставляется (К6);
//   • тело предъявления — закрытая проекция ответа браузера: поля сериализации
//     браузера (`clientExtensionResults`, `authenticatorAttachment`) в него не
//     попадают (Р4, К5);
//   • отказ кодека не несёт входа: ни испытания, ни удостоверения (К3).

const bytes = (...xs: number[]) => new Uint8Array(xs).buffer;
const view = (b: ArrayBuffer) => [...new Uint8Array(b)];

/** Маркер, по которому видно эхо входа в тексте отказа. */
const MARKER = "https://marker.invalid/echo";

function refusalText(run: () => unknown): string {
  try {
    run();
  } catch (e) {
    expect(e).toBeInstanceOf(AccessKeyEncodingError);
    return (e as Error).message;
  }
  throw new Error("кодек принял негодный вход");
}

describe("кодек церемонии ключа доступа", () => {
  it("base64url без дополнения — в обе стороны, на всех 256 значениях байта", () => {
    const all = new Uint8Array(256).map((_, i) => i);
    const text = base64urlOf(all.buffer);
    expect(text).toBe(Buffer.from(all).toString("base64url"));
    expect(text).not.toMatch(/[=+/]/);
    expect(view(bytesOf(text, "challenge"))).toEqual([...all]);
    // Вид над буфером кодируется ровно своими байтами, а не всем буфером.
    expect(base64urlOf(new Uint8Array([9, 1, 2, 3]).subarray(1, 3))).toBe(Buffer.from([1, 2]).toString("base64url"));
  });

  it("второе написание того же значения не принимается: дополнение, стандартный алфавит, обрезок", () => {
    for (const bad of ["AA==", "+/8", "A", "AA AA", ""]) {
      expect([bad, refusalText(() => bytesOf(bad, "challenge"))]).toEqual([
        bad,
        "access key: challenge is malformed",
      ]);
    }
    expect(refusalText(() => bytesOf(42, "challenge"))).toBe("access key: challenge is malformed");
  });

  it("параметры церемонии — из ответа службы дословно, и только названные", () => {
    const challenge = bytes(1, 2, 3, 250);
    const options = assertionRequestOf({
      publicKey: {
        challenge: base64urlOf(challenge),
        rpId: "console.kacho.local",
        timeout: 300000,
        userVerification: "preferred",
        allowCredentials: [],
        extensions: { appid: "https://evil.example" },
      },
    });
    expect({ ...options, challenge: view(options.challenge as ArrayBuffer) }).toEqual({
      challenge: [1, 2, 3, 250],
      rpId: "console.kacho.local",
      timeout: 300000,
      userVerification: "preferred",
      allowCredentials: [],
    });
  });

  it("перечень допустимых удостоверений переносится как прислан — тем же правилом кодирования", () => {
    const options = assertionRequestOf({
      publicKey: {
        challenge: "AQID",
        rpId: "console.kacho.local",
        allowCredentials: [{ type: "public-key", id: "Y3JlZA", transports: ["internal"] }],
      },
    });
    const [only] = options.allowCredentials ?? [];
    expect({ ...only, id: view(only.id as ArrayBuffer) }).toEqual({
      type: "public-key",
      id: [...Buffer.from("cred")],
      transports: ["internal"],
    });
    // Срока и требования проверки служба не назвала — консоль их не выдумывает.
    expect("timeout" in options || "userVerification" in options).toBe(false);
  });

  it("К3 · ответ испытания не той формы — отказ кодека, и вход в его текст не попадает", () => {
    const cases: Array<[string, unknown]> = [
      ["нет publicKey", { challenge: "AQID" }],
      ["не объект", MARKER],
      ["испытание не base64url", { publicKey: { challenge: MARKER, rpId: "console.kacho.local" } }],
      ["нет имени доверяющей стороны", { publicKey: { challenge: "AQID" } }],
      ["имя доверяющей стороны не строка", { publicKey: { challenge: "AQID", rpId: 7 } }],
      ["срок не число", { publicKey: { challenge: "AQID", rpId: "r", timeout: MARKER } }],
      ["перечень не массив", { publicKey: { challenge: "AQID", rpId: "r", allowCredentials: MARKER } }],
      ["удостоверение перечня не base64url", { publicKey: { challenge: "AQID", rpId: "r", allowCredentials: [{ type: "public-key", id: MARKER }] } }],
    ];
    for (const [what, answer] of cases) {
      const text = refusalText(() => assertionRequestOf(answer));
      expect([what, text.includes(MARKER), text.startsWith("access key: ")]).toEqual([what, false, true]);
    }
  });

  it("тело предъявления — закрытая проекция: ровно объявленные поля, значения перенесены без изменения", () => {
    const credential = {
      id: "Y3JlZA",
      rawId: bytes(...Buffer.from("cred")),
      type: "public-key",
      authenticatorAttachment: "platform",
      getClientExtensionResults: () => ({ credProps: { rk: true } }),
      toJSON: () => ({ clientExtensionResults: {}, authenticatorAttachment: "platform" }),
      response: {
        clientDataJSON: bytes(123, 125),
        authenticatorData: bytes(1, 2, 3),
        signature: bytes(4, 5),
        userHandle: bytes(6),
      },
    };
    expect(assertionBodyOf(credential)).toEqual({
      id: "Y3JlZA",
      rawId: "Y3JlZA",
      type: "public-key",
      response: {
        clientDataJSON: base64urlOf(bytes(123, 125)),
        authenticatorData: base64urlOf(bytes(1, 2, 3)),
        signature: base64urlOf(bytes(4, 5)),
        userHandle: base64urlOf(bytes(6)),
      },
    });
  });

  it("рукоятки человека браузер не назвал — поле пустое, выдуманного значения нет", () => {
    const body = assertionBodyOf({
      id: "AQ",
      rawId: bytes(1),
      type: "public-key",
      response: { clientDataJSON: bytes(1), authenticatorData: bytes(2), signature: bytes(3), userHandle: null },
    });
    expect(body.response.userHandle).toBe("");
  });

  it("К3 · ответ браузера не той формы — отказ кодека без эха", () => {
    const cases: Array<[string, unknown]> = [
      ["не объект", MARKER],
      ["нет ответа аутентификатора", { id: MARKER, rawId: bytes(1), type: "public-key" }],
      ["подпись не байты", { id: "AQ", rawId: bytes(1), type: "public-key", response: { clientDataJSON: bytes(1), authenticatorData: bytes(1), signature: MARKER, userHandle: null } }],
      ["идентификатор не строка", { id: 1, rawId: bytes(1), type: "public-key", response: {} }],
    ];
    for (const [what, credential] of cases) {
      const text = refusalText(() => assertionBodyOf(credential));
      expect([what, text.includes(MARKER)]).toEqual([what, false]);
    }
  });
});
