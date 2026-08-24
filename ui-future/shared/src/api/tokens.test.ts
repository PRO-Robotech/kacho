import { issuedCredentialFromOperation } from "./tokens";
import type { Operation } from "./types";

// Одноразовое удостоучение приходит В ДВУХ ФОРМАХ, и читатель обязан различать
// их ВИДОМ, а не наличием поля (#1235).
//
// Контракт (`IssueSAKeyResponse` / `IssueUserTokenResponse`) заполняет РОВНО
// ОДНО из двух: `private_key_pem` у ключевой пары либо `secret` у секрета.
// Читатель, знающий только про ключевую пару, на секрете отдаёт `null` — и
// консоль говорит «секрет не получен» при исправной выдаче.
const op = (response: unknown): Operation =>
  ({ id: "opr-1", done: true, response }) as unknown as Operation;

describe("issuedCredentialFromOperation — вид решает, что показывать", () => {
  it("секрет читается и назван секретом", () => {
    const cred = issuedCredentialFromOperation(op({ key_id: "soc-7", client_id: "soc-7", secret: "kc.s.XXXX" }));

    expect(cred).not.toBeNull();
    expect(cred?.kind).toBe("secret");
    expect(cred?.kind === "secret" ? cred.secret : "").toBe("kc.s.XXXX");
  });

  // Положительный контроль к утверждению выше: прежняя форма не перестала
  // читаться. Без этой пары «секрет читается» зеленело бы на читателе, который
  // всё подряд объявляет секретом.
  it("ключевая пара читается по-прежнему и названа ключевой парой", () => {
    const cred = issuedCredentialFromOperation(
      op({ key_id: "soc-7", client_id: "soc-7", algorithm: "ES256", private_key_pem: "-----BEGIN…" }),
    );

    expect(cred?.kind).toBe("keypair");
    expect(cred?.kind === "keypair" ? cred.private_key_pem : "").toBe("-----BEGIN…");
  });

  it("тело без обеих форм — не удостоверение, а НИЧЕГО: фантома не выдаём", () => {
    expect(issuedCredentialFromOperation(op({ key_id: "soc-7", client_id: "soc-7" }))).toBeNull();
    expect(issuedCredentialFromOperation(undefined)).toBeNull();
  });

  // Строка операции секрета НЕ НЕСЁТ НИ В КАКОЙ МОМЕНТ: он подменяется в теле
  // ответа уже после записи. Значит пустое поле в опрошенной операции — это
  // штатное состояние, а не отказ, и путать его с «выдача не удалась» нельзя.
  it("пустая строка секрета читается как отсутствие, а не как секрет", () => {
    expect(issuedCredentialFromOperation(op({ key_id: "soc-7", secret: "" }))).toBeNull();
  });
});
