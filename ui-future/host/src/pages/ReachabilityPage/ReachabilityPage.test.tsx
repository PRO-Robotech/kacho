import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { jest } from "@jest/globals";
import { stubNetwork } from "@shared/test/network-stub";
import { ReachabilityPage } from ".";

const jsonResponse = (body: unknown, status = 200) => {
  return Promise.resolve({
    ok: status >= 200 && status < 300,
    status,
    text: () => Promise.resolve(JSON.stringify(body)),
    statusText: "OK",
  } as Response);
};

// Снятый поставщик личности — обе его службы, выдача токена и сессия. Назван
// здесь ОДНИМ признаком — приставкой его публичного края, общей для обеих:
// утверждение о подписи строки пережило бы переименование подписи, а адрес — то,
// куда страница реально ходит. Приставка шире одной службы намеренно: служба,
// снятая вчера, и служба, снятая сегодня, дают на странице один и тот же дефект.
const RETIRED_IDENTITY_PROVIDER_EDGE = "/.ory/";

describe("ReachabilityPage", () => {
  afterEach(() => {
    jest.restoreAllMocks();
  });

  it("runs all probes", async () => {
    const user = userEvent.setup();
    const fetchMock = stubNetwork(() => jsonResponse({ message: "ready" }));
    render(<ReachabilityPage />);

    await user.click(screen.getByRole("button", { name: "Проверить все" }));

    // Число проверок не выписано: страница объявляет их сама, а проба
    // утверждает, что нажатие поднимает КАЖДУЮ строку и ни одной сверх того.
    const rows = await screen.findAllByRole("button", { name: "Проверить" });
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledTimes(rows.length);
    });
    expect(await screen.findAllByText("ok 200")).toHaveLength(rows.length);
  });

  // Снятие полос к чужому поставщику личности (#2733). Утверждается
  // НАБЛЮДАЕМОЕ: пользователь не видит на странице доступности строки его
  // служб, и «Проверить все» к ним не ходит. Поставщик не поднимается ни на
  // одном стенде, и полосы к нему нет ни в раздаче, ни в сборщике: строка
  // проверяла бы службу, которой нет, и её исход ничего не говорил бы о стенде.
  it("не предлагает проверку служб снятого поставщика личности", async () => {
    const user = userEvent.setup();
    const fetchMock = stubNetwork(() => jsonResponse({ message: "ready" }));
    render(<ReachabilityPage />);

    const shown = screen.getAllByText(/^\//).map((n) => n.textContent ?? "");
    expect(shown.length).toBeGreaterThan(0);
    expect(shown.filter((p) => p.startsWith(RETIRED_IDENTITY_PROVIDER_EDGE))).toEqual([]);

    await user.click(screen.getByRole("button", { name: "Проверить все" }));
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalled();
    });
    // Адрес берётся первым доводом вызова, и его вид утверждается: приведение
    // чужого объекта к строке молча дало бы `[object Object]`, и проба стала бы
    // зелёной на любом запросе.
    const asked = fetchMock.mock.calls.map(([target]) => {
      expect(typeof target).toBe("string");
      return target as string;
    });
    expect(asked.filter((p) => p.startsWith(RETIRED_IDENTITY_PROVIDER_EDGE))).toEqual([]);
  });
});
