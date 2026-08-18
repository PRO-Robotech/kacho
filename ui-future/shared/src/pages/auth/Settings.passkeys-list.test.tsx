// Перечень зарегистрированных passkey — то, ЧТО ВИДИТ ОПЕРАТОР на странице
// безопасности.
//
// ПРЕДМЕТ (#625). Строки приходят списку ПРОПАМИ (`dataSource` + `renderItem`),
// детьми он не получает НИЧЕГО. Пока общий заменитель подменял `List` пустым
// `<div>{children}</div>`, перечень был наблюдаем как пустота — при любом числе
// ключей, включая ни одного, — а вместе со строками пропадала и кнопка снятия
// на строке. То есть непроверяемым становился весь путь «увидеть свои ключи и
// снять чужой».
//
// Почему это не косметика. Ключ доступа — то, чем входят в облако; страница,
// которая молча не показывает чужой ключ, оставляет доступ у того, кого владелец
// считает отключённым.
//
// Утверждается наблюдаемое: строки с именами ключей и действие НА СТРОКЕ, а не
// где-то на странице. Привязка к строке несущая: одна кнопка «Удалить» на всю
// страницу удаляла бы не тот ключ, и утверждение «кнопка есть» этого не ловит.

import { render, screen, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import { SettingsPage } from "./Settings";

const realFetch = globalThis.fetch;

interface Passkey {
  id: string;
  label: string;
}

/**
 * Настоящий Kratos, отвечающий формой своего ответа: перечень ключей приходит
 * УЗЛАМИ формы (`webauthn_remove`), а не отдельным полем. Модуль `kratos` здесь
 * НЕ подменён — подменён только край: заменитель самого модуля пришлось бы
 * писать своей копией разбора узлов, и проба измеряла бы копию, а не продукт.
 */
function stubFlow(passkeys: Passkey[]) {
  const flow = {
    id: "flw-1",
    refresh: false,
    ui: {
      action: "/self-service/settings",
      method: "POST",
      messages: [],
      nodes: [
        { group: "default", attributes: { name: "csrf_token", value: "csrf" } },
        ...passkeys.map((p) => ({
          group: "webauthn",
          attributes: { name: "webauthn_remove", value: p.id },
          meta: { label: { text: p.label } },
        })),
      ],
    },
  };
  globalThis.fetch = () =>
    Promise.resolve({
      ok: true,
      status: 200,
      statusText: "OK",
      headers: { get: () => "application/json" },
      json: () => Promise.resolve(flow),
      text: () => Promise.resolve(JSON.stringify(flow)),
    } as unknown as Response);
}

function renderSettings() {
  return render(
    <MemoryRouter initialEntries={["/auth/settings?flow=flw-1"]}>
      <Routes>
        <Route path="/auth/settings" element={<SettingsPage />} />
      </Routes>
    </MemoryRouter>,
  );
}

afterEach(() => {
  globalThis.fetch = realFetch;
});

/**
 * Строки ключей — те элементы списка, что несут ПОДПИСЬ ключа.
 *
 * Просто «все элементы списка» здесь неверно, и это свойство настоящего antd, а
 * не заменителя: действия строки он тоже кладёт в список (`<ul><li>кнопка</li></ul>`),
 * поэтому на двух ключах элементов списка четыре. Считать их все значило бы
 * утверждать о разметке списка действий, а не о числе ключей.
 */
const строкиКлючей = () => screen.getAllByRole("listitem").filter((li) => within(li).queryByRole("heading"));

describe("Settings — перечень passkey", () => {
  it("каждый зарегистрированный ключ показан СВОЕЙ строкой", async () => {
    stubFlow([
      { id: "cred-1", label: "Ноутбук" },
      { id: "cred-2", label: "Телефон" },
    ]);
    renderSettings();

    expect(await screen.findByText("Ноутбук")).toBeInTheDocument();
    expect(screen.getByText("Телефон")).toBeInTheDocument();
    // Строк ровно столько, сколько ключей: перечень, показывающий один ключ из
    // двух, выглядит как исправный.
    expect(строкиКлючей()).toHaveLength(2);
  });

  it("снятие ключа предложено НА СТРОКЕ этого ключа", async () => {
    stubFlow([
      { id: "cred-1", label: "Ноутбук" },
      { id: "cred-2", label: "Телефон" },
    ]);
    renderSettings();

    await screen.findByText("Ноутбук");
    const строка = строкиКлючей().find((li) => within(li).queryByText("Телефон"))!;
    // Кнопка несёт идентификатор СВОЕГО ключа: одна кнопка на страницу сняла бы
    // не тот ключ, и утверждение «кнопка есть» этого не различает.
    expect(within(строка).getByTestId("settings-remove-passkey-cred-2")).toBeInTheDocument();
    expect(within(строка).queryByTestId("settings-remove-passkey-cred-1")).not.toBeInTheDocument();
  });

  it("без ключей перечня НЕТ, и сказано это прямо", async () => {
    // Парный контроль: без него утверждения выше зеленели бы на странице,
    // которая рисует строки всегда. И отдельно — пустота не молчит: «ключей
    // нет» и «страница не загрузилась» для оператора разные вещи.
    stubFlow([]);
    renderSettings();

    expect(await screen.findByText("Passkey не зарегистрированы.")).toBeInTheDocument();
    expect(screen.queryAllByRole("listitem")).toHaveLength(0);
  });
});
