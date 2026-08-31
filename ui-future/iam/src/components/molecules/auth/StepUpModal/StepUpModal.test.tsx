import { jest } from "@jest/globals";
import { act, render, screen } from "@testing-library/react";
import type { AuthContextValue } from "@shared/contexts/AuthContext";
import type { StepUpModal as StepUpModalExport } from "./StepUpModal";

type StepUpHandler = ((acr?: string) => Promise<void>) | null;

let registered: StepUpHandler = null;
const setStepUpHandler = jest.fn((h: StepUpHandler) => {
  registered = h;
});

const auth = {
  setStepUpHandler,
  markMfaFresh: jest.fn(),
  refresh: jest.fn(),
} as unknown as AuthContextValue;

jest.unstable_mockModule("@shared/contexts/AuthContext", () => ({
  useAuth: () => auth,
}));

// Мок отдаёт ВСЁ, что берёт цепочка импорта, а не только то, что зовёт сам
// компонент: под ESM недостающий экспорт кладёт всю суиту сразу — «does not
// provide an export named».
//
// Цепочка с тех пор укоротилась (#1225): за кодировщиком двоичных полей окно
// тянуло целую страницу входа, а она читала из библиотеки протокола ещё и
// сообщения уровня потока. Страница снята, кодировщик переехал в
// `@shared/lib/webauthn`.
//
// Здесь стояло, что подмена «стала ровно тем, что зовёт компонент», — и это
// было неправдой: лишний экспорт пережил снятую страницу и остался в подмене.
// Текст описывал состояние, которого нет, и следующий читатель чинил бы код под
// это описание. Экспорт снят вместе со своим объявлением в `@shared/lib/kratos`
// (#1782); теперь состав подмены и состав импорта компонента совпадают.
jest.unstable_mockModule("@shared/lib/kratos", () => ({
  kratos: {
    loginUrl: () => "#idp",
    settingsUrl: () => "#settings",
    initFlow: jest.fn(),
    getFlow: jest.fn(),
    submitFlow: jest.fn(),
  },
  findNode: jest.fn(),
  csrfToken: jest.fn(),
}));

jest.unstable_mockModule("@shared/lib/webauthn", () => ({
  bufferToBase64Url: () => "",
}));

let StepUpModal: typeof StepUpModalExport;

const modal = () => screen.getByTestId("stepup-modal");
/** `open` булев: React выкидывает атрибут при false, поэтому его наличие и есть состояние. */
/**
 * Закрытое окно НЕ показывает содержимого — это и есть наблюдаемое. Прежде
 * состояние читалось атрибутом `open` подменённого узла, то есть утверждалась
 * форма дублёра: настоящее окно antd при `open=false` содержимого не рисует.
 */
const isOpen = () => screen.queryByText(/дополнительной проверки безопасности/) !== null;

describe("StepUpModal", () => {
  beforeAll(async () => {
    ({ StepUpModal } = await import("./StepUpModal"));
  });

  beforeEach(() => {
    registered = null;
    jest.clearAllMocks();
  });

  it("подписывается на запрос подтверждения при монтировании", () => {
    render(<StepUpModal />);

    expect(setStepUpHandler).toHaveBeenCalledTimes(1);
    expect(typeof registered).toBe("function");
  });

  it("снимает подписку при размонтировании — иначе мёртвое окно перехватывало бы запросы", () => {
    const { unmount } = render(<StepUpModal />);

    unmount();

    expect(setStepUpHandler).toHaveBeenLastCalledWith(null);
  });

  it("до запроса окно закрыто и ничего не перехватывает", () => {
    render(<StepUpModal />);

    expect(isOpen()).toBe(false);
  });

  it("по запросу открывается и называет затребованный уровень", () => {
    render(<StepUpModal />);

    act(() => {
      void registered?.("3");
    });

    expect(isOpen()).toBe(true);
    expect(modal()).toHaveTextContent("(ACR=3)");
  });

  it("без указанного уровня подставляет второй, а не пустоту", () => {
    render(<StepUpModal />);

    act(() => {
      void registered?.();
    });

    expect(modal()).toHaveTextContent("(ACR=2)");
    expect(modal()).not.toHaveTextContent("(ACR=)");
  });

  it("обещание запроса не разрешается само — пока пользователь не ответил, запрос ждёт", async () => {
    render(<StepUpModal />);

    let settled = false;
    act(() => {
      void registered?.("2")
        .then(() => {
          settled = true;
        })
        .catch(() => {
          settled = true;
        });
    });

    await act(async () => {
      await Promise.resolve();
    });

    // Разрешись оно само — вызывающий повторил бы запрос без подтверждения.
    expect(settled).toBe(false);
    expect(isOpen()).toBe(true);
  });
});
