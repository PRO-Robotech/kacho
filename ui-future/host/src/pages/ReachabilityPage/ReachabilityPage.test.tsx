import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { jest } from "@jest/globals";
import { ReachabilityPage } from ".";

const jsonResponse = (body: unknown, status = 200) => {
  return Promise.resolve({
    ok: status >= 200 && status < 300,
    status,
    text: () => Promise.resolve(JSON.stringify(body)),
    statusText: "OK",
  } as Response);
};

// Снятая служба выдачи токена. Названа здесь ОДНИМ признаком — приставкой её
// публичного края: утверждение о подписи («Hydra») пережило бы переименование
// подписи, а адрес — то, куда страница реально ходит.
const RETIRED_TOKEN_ISSUER_PREFIX = "/.ory/hydra/";

describe("ReachabilityPage", () => {
  afterEach(() => {
    jest.restoreAllMocks();
  });

  it("runs all probes", async () => {
    const user = userEvent.setup();
    const fetchMock = jest.spyOn(global, "fetch").mockImplementation(() => jsonResponse({ message: "ready" }));
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

  // Снятие полосы к чужой службе выдачи токена (#2733). Утверждается
  // НАБЛЮДАЕМОЕ: пользователь не видит на странице доступности строки этой
  // службы, и «Проверить все» к ней не ходит. Утверждение о подписи строки
  // пережило бы её переименование, поэтому судится адрес.
  it("не предлагает проверку снятой службы выдачи токена", async () => {
    const user = userEvent.setup();
    const fetchMock = jest.spyOn(global, "fetch").mockImplementation(() => jsonResponse({ message: "ready" }));
    render(<ReachabilityPage />);

    const shown = screen.getAllByText(/^\//).map((n) => n.textContent ?? "");
    expect(shown.length).toBeGreaterThan(0);
    expect(shown.filter((p) => p.startsWith(RETIRED_TOKEN_ISSUER_PREFIX))).toEqual([]);

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
    expect(asked.filter((p) => p.startsWith(RETIRED_TOKEN_ISSUER_PREFIX))).toEqual([]);
  });
});
