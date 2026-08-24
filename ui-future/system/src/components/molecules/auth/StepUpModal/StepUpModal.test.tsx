// StepUpModal — контракт перехвата: регистрация обработчика, снятие его при
// размонтировании и уровень проверки, который видит пользователь.
//
// Прежняя редакция открывала `StepUpModal.tsx` и утверждала, что в тексте файла
// встречается строка «StepUpModal». Это утверждение о существовании файла, а не о
// поведении: компонент не монтировался, обработчик не регистрировался, ни один
// исход не проверялся. Между тем предмет здесь — единственный путь, которым
// клиент отвечает на отказ «нужна повторная проверка»: не зарегистрируется
// обработчик — запрос не будет повторён никогда, не снимется при размонтировании
// — отвечать возьмётся мёртвый компонент.
//
// Ограничение среды названо честно: общий дублёр antd (`shared/src/test/setup.ts`)
// рисует `Modal` как обычный контейнер и НЕ рисует его `footer`, поэтому кнопки
// «Отменить» / «Подтвердить через Passkey» в этой суите недостижимы. Наблюдаемы
// регистрация/снятие обработчика и текст с уровнем проверки — то, что и различает
// состояния модалки.

import { jest } from "@jest/globals";
import { render, screen, act } from "@testing-library/react";

type StepUpHandler = ((acr?: string) => Promise<void>) | null;

let registered: StepUpHandler[] = [];
const setStepUpHandler = jest.fn((h: StepUpHandler) => {
  registered.push(h);
});
const markMfaFresh = jest.fn();
const refresh = jest.fn(async () => {});

jest.unstable_mockModule("@shared/contexts/AuthContext", () => ({
  useAuth: () => ({ setStepUpHandler, markMfaFresh, refresh }),
}));

const { StepUpModal } = await import("./StepUpModal");

beforeEach(() => {
  registered = [];
  setStepUpHandler.mockClear();
});

/** Последний непустой обработчик, который компонент отдал контексту. */
function currentHandler(): (acr?: string) => Promise<void> {
  const h = registered.filter(Boolean).at(-1);
  if (!h) throw new Error("компонент не зарегистрировал обработчик повторной проверки");
  return h;
}

describe("StepUpModal — жизненный цикл обработчика", () => {
  it("регистрирует обработчик при монтировании", () => {
    render(<StepUpModal />);

    expect(setStepUpHandler).toHaveBeenCalled();
    expect(typeof currentHandler()).toBe("function");
  });

  it("снимает обработчик при размонтировании", () => {
    // Не снятый обработчик оставляет отвечать компонент, которого на экране уже
    // нет: запрос уйдёт в промис, который никто не разрешит.
    const view = render(<StepUpModal />);
    expect(registered.at(-1)).not.toBeNull();

    view.unmount();

    expect(registered.at(-1)).toBeNull();
  });
});

describe("StepUpModal — что видит пользователь", () => {
  it("до запроса не показывает ничего — окно закрыто", () => {
    // Прежде здесь утверждалось, что уровень по умолчанию виден ДО запроса.
    // Это было свойство заменителя: он рисовал содержимое закрытого окна, чего
    // настоящее окно antd не делает. Положительный близнец «показал запрошенный
    // уровень» — случай про уровень по умолчанию ниже, уже после запроса.
    render(<StepUpModal />);

    expect(screen.queryByText(/ACR=/)).toBeNull();
  });

  it("запрос без уровня показывает уровень по умолчанию, а не пустоту", async () => {
    render(<StepUpModal />);

    await act(async () => {
      void currentHandler()();
    });

    expect(screen.getByText(/ACR=2/)).toBeInTheDocument();
  });

  it("показывает ИМЕННО тот уровень, который запросил вызывающий", async () => {
    render(<StepUpModal />);

    await act(async () => {
      void currentHandler()("3");
    });

    expect(screen.getByText(/ACR=3/)).toBeInTheDocument();
    expect(screen.queryByText(/ACR=2/)).toBeNull();
  });

  it("объясняет, чем подтверждать, а не только что нужно подтвердить", async () => {
    // ПРОБА ЗАМЕНЕНА ВМЕСТЕ СО СВОИМ МЕХАНИЗМОМ (#1213), а не ослаблена.
    //
    // Прежняя редакция утверждала, что окно называет ключ доступа СРАЗУ ПРИ
    // ОТКРЫТИИ. Это было верно ровно потому, что окно вело единственный способ
    // и объявляло его константой — а настройки объявляют ключ доступа ПЕРВЫМ
    // фактором, которого в потоке `aal=aal2` нет вовсе. То есть окно называло
    // способ, которым подтвердить было НЕЛЬЗЯ.
    //
    // Существо утверждения сохранено и стало строже: окно обязано назвать тот
    // способ, которым оно САМО и отправит подтверждение, а способ этот приходит
    // от службы личности. Поэтому проба сперва даёт поток.
    const fetchMock = jest.fn(async () => ({
      ok: true,
      status: 200,
      text: async () =>
        JSON.stringify({
          id: "flw-1",
          type: "browser",
          ui: {
            action: "/x",
            method: "POST",
            nodes: [
              { type: "input", group: "default", attributes: { name: "csrf_token", value: "c" } },
              { type: "input", group: "totp", attributes: { name: "totp_code" } },
            ],
          },
        }),
    }));
    (globalThis as unknown as { fetch: unknown }).fetch = fetchMock;

    render(<StepUpModal />);

    await act(async () => {
      void currentHandler()("2");
    });

    expect(await screen.findByText(/приложение-аутентификатор/i)).toBeInTheDocument();
    expect(screen.getByTestId("stepup-code")).toBeInTheDocument();
    expect(screen.getByTestId("stepup-confirm")).toBeInTheDocument();
  });
});

describe("StepUpModal — обещание вызывающему", () => {
  it("обработчик отдаёт промис, который до ответа пользователя не разрешён", async () => {
    render(<StepUpModal />);

    let settled = false;
    await act(async () => {
      void currentHandler()("2").then(
        () => {
          settled = true;
        },
        () => {
          settled = true;
        },
      );
    });

    // Именно неразрешённость и есть предмет: запрос обязан ЖДАТЬ человека, а не
    // проскочить дальше с прежним уровнем проверки.
    expect(settled).toBe(false);
    expect(screen.getByText(/ACR=2/)).toBeInTheDocument();
  });
});
