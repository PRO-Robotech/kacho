import { jest } from "@jest/globals";
import { render, screen } from "@testing-library/react";
import { antdStub } from "@shared/test/antd-stub";
import type { IssuedCredential } from "@shared/api/tokens";

// Окно одноразового показа обязано различать ВИДЫ удостоверения (#1235).
//
// Оно писалось под единственную форму — приватный ключ — и говорит о ней в
// подписях, в файле для скачивания и в тексте подтверждения. Секрет через него
// показать нельзя: подпись обещает PEM, а поле пусто. Значение при этом
// невосстановимо, то есть цена ошибки здесь — потерянный доступ, а не неудобство.
jest.unstable_mockModule("antd", () => antdStub());
jest.unstable_mockModule("@shared/lib/clipboard", () => ({ copyText: jest.fn() }));

const { OneTimeSecretModal } = await import("./OneTimeSecretModal");

const secretCred: IssuedCredential = {
  kind: "secret",
  client_id: "soc-7",
  key_id: "soc-7",
  secret: "kc.s.QQQQ",
};

const keypairCred: IssuedCredential = {
  kind: "keypair",
  client_id: "soc-8",
  key_id: "soc-8",
  algorithm: "ES256",
  private_key_pem: "-----BEGIN PRIVATE KEY-----",
};

function show(credential: IssuedCredential) {
  return render(
    <OneTimeSecretModal open onClose={() => undefined} credential={credential} title="Токен выпущен" />,
  );
}

describe("OneTimeSecretModal — секрет", () => {
  it("показывает сам секрет, а не пустое поле приватного ключа", () => {
    const { container } = show(secretCred);

    expect(screen.getByDisplayValue("kc.s.QQQQ")).toBeInTheDocument();
    // Подпись обещает то, что показано. Обещать PEM на секрете значит утверждать
    // о выданном неправду.
    expect(container).not.toHaveTextContent("Приватный ключ");
  });

  it("называет радиус секрета прямо в окне выдачи", () => {
    const { container } = show(secretCred);

    expect(container).toHaveTextContent(/не только в реестре/);
    expect(container).toHaveTextContent(/учётная запись/);
  });

  it("подтверждение говорит о том, что сохранили, — о секрете, а не о ключе", () => {
    const { container } = show(secretCred);

    expect(container).toHaveTextContent(/секрет/i);
  });
});

// Положительный контроль: прежняя форма не сломана. Без этой пары утверждения
// выше зеленели бы на окне, которое разучилось показывать ключевую пару.
describe("OneTimeSecretModal — ключевая пара", () => {
  it("по-прежнему показывает приватный ключ и алгоритм", () => {
    const { container } = show(keypairCred);

    expect(screen.getByDisplayValue("-----BEGIN PRIVATE KEY-----")).toBeInTheDocument();
    expect(container).toHaveTextContent("Приватный ключ");
    // Алгоритм живёт в поле ввода, а не в тексте: спрашиваем его значением.
    expect(screen.getByDisplayValue("ES256")).toBeInTheDocument();
  });

  it("радиус предъявительского секрета к ключевой паре НЕ приписывается", () => {
    const { container } = show(keypairCred);

    expect(container).not.toHaveTextContent(/не только в реестре/);
  });
});
