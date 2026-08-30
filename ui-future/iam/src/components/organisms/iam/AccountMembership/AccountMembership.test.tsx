// Членство человека В ТЕКУЩЕМ АККАУНТЕ на карточке сотрудника.
//
// ПРЕДМЕТ. До ресурса членства (#1085) ответа «к какому аккаунту относится этот
// человек» не было ни у одного чтения: поле снято с записи пользователя (#471),
// и карточка обязана была МОЛЧАТЬ. Источник появился — молчание кончилось, но
// вместе с ним появился второй предмет, противоположный первому: человек
// состоит в НЕСКОЛЬКИХ аккаунтах, и карточка не вправе называть ни один, кроме
// текущего. Оба утверждения живут здесь, потому что порознь каждое зеленеет на
// сломанном: «A назван» истинно у карточки, которая называет и A, и B; «B не
// назван» истинно у карточки, которая не отрисовалась вовсе. Поэтому у каждого
// отрицания ниже стоит положительный контроль.
//
// ПОЧЕМУ ПОДМЕНЯЕТСЯ `fetch`, А НЕ КЛИЕНТ. Сценарий приёмки требует утверждения
// о ПЕРЕЧНЕ ИСХОДЯЩИХ ОБРАЩЕНИЙ («консоль не задаёт ни одного запроса, который
// спрашивал бы про человека без аккаунта»). Слежка за методом клиента отвечает
// на другой вопрос — «позвали ли эту функцию», — и о запросе, ушедшем мимо неё,
// не говорит ничего. Перечень читается только на транспорте.
//
// ЧЕГО ЭТА ПРОБА НЕ РАЗЛИЧАЕТ — названо, а не умолчано. Она монтирует ОДИН
// компонент, а не карточку целиком: собери карточку без него — здесь останется
// зелено. Стык «секция доезжает до карточки» держит сквозная проба браузером
// (`ui-future/e2e/specs/users.spec.ts`), а провязку — проба расширений рядом
// (`iam/src/registerExtensions.membership.test.tsx`).

import { jest } from "@jest/globals";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { antdStub } from "@shared/test/antd-stub";
import { requestUrl } from "@shared/test/fetch-capture";

jest.unstable_mockModule("antd", () => antdStub());

const CURRENT = "acc-A";
const OTHER = "acc-B";
const PERSON = "usr-P";

/** Членство в проводной форме края — camelCase; клиент приводит его к snake_case. */
function wireMembership(accountId: string, state: "ACTIVE" | "PENDING", accountName: string) {
  return {
    id: `mbr-${accountId}`,
    accountId,
    accountName,
    userId: PERSON,
    state,
    invitedBy: "usr-boss",
    createdAt: "2026-08-01T10:00:00Z",
    updatedAt: "2026-08-02T10:00:00Z",
  };
}

let calls: string[] = [];

/**
 * Подменённый транспорт. Отвечает ПО АДРЕСУ, а не по порядку вызова: порядок —
 * свойство реализации, и проба, закрепившая его, краснела бы на перестановке,
 * ничего не меняющей по существу.
 */
function serve(memberships: Array<ReturnType<typeof wireMembership>>) {
  globalThis.fetch = ((input: RequestInfo | URL) => {
    const url = requestUrl(input);
    calls.push(url);
    const body = url.includes("/memberships")
      ? { memberships }
      : url.includes(`/iam/v1/accounts/${CURRENT}`)
        ? { id: CURRENT, name: "Ромашка" }
        : url.includes(`/iam/v1/accounts/${OTHER}`)
          ? { id: OTHER, name: "Одуванчик" }
          : {};
    return Promise.resolve({
      ok: true,
      status: 200,
      statusText: "OK",
      text: () => Promise.resolve(JSON.stringify(body)),
    } as unknown as Response);
  }) as unknown as typeof fetch;
}

async function mount(userId = PERSON) {
  const { AccountMembership } = await import("./AccountMembership");
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <AccountMembership userId={userId} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

/** Обращения к поверхности членства — то, что разбирает сценарий 20. */
const membershipCalls = () => calls.filter((u) => u.includes("memberships"));

describe("карточка сотрудника: членство в текущем аккаунте", () => {
  const originalFetch = globalThis.fetch;

  beforeEach(async () => {
    calls = [];
    const { contextApi } = await import("@shared/lib/context-store");
    contextApi.setAccount({ id: CURRENT, name: "Ромашка" });
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  // ── Сценарий 19: карточка НАЗЫВАЕТ членство в текущем аккаунте ────────────

  it("называет аккаунт ссылкой на его карточку, а не голым идентификатором", async () => {
    // verifies #1085
    serve([wireMembership(CURRENT, "ACTIVE", "Ромашка")]);
    const { container } = await mount();

    // Положительный контроль: секция отрисована и второй факт членства назван.
    // Без него утверждение про ссылку зеленело бы на пустом поддереве.
    await waitFor(() => expect(screen.queryByText(/Состоит в аккаунте/)).not.toBeNull());

    // Голый идентификатор адресует ресурс и никуда не ведёт (ui.md, правило 2).
    const link = () => container.querySelector(`a[href="/iam/accounts/${CURRENT}"]`);
    expect(link()).not.toBeNull();

    // Имя резолвится ОТДЕЛЬНЫМ точечным чтением аккаунта, поэтому его ждут, а не
    // читают сразу: иначе проба закрепила бы гонку, а не свойство. Человек
    // работает с именем, а не с идентификатором.
    await waitFor(() => expect(link()?.textContent ?? "").toContain("Ромашка"));
  });

  it("называет состояние следствием, а не словом «да»", async () => {
    // verifies #1085 — ui.md, правило 6: «Да» не говорит ни что человек состоит,
    // ни что он приглашён.
    serve([wireMembership(CURRENT, "ACTIVE", "Ромашка")]);
    await mount();

    await waitFor(() => expect(screen.queryByText(/Состоит в аккаунте/)).not.toBeNull());
    expect(screen.queryByText("Да", { exact: true })).toBeNull();
    expect(screen.queryByText("Нет", { exact: true })).toBeNull();
  });

  it("приглашённого называет приглашённым, а не состоящим", async () => {
    // verifies #1085 — вторая сторона той же оси. Без неё утверждение выше
    // зеленело бы у секции, которая печатает «состоит» при любом состоянии.
    serve([wireMembership(CURRENT, "PENDING", "Ромашка")]);
    await mount();

    await waitFor(() => expect(screen.queryByText(/Приглашён/)).not.toBeNull());
    expect(screen.queryByText(/Состоит в аккаунте/)).toBeNull();
  });

  it("спрашивает членство у текущего аккаунта: он в пути, человек — в фильтре", async () => {
    // verifies #1085 — чтение сужается аккаунтом В ЗАПРОСЕ, а не проверкой после.
    serve([wireMembership(CURRENT, "ACTIVE", "Ромашка")]);
    await mount();

    await waitFor(() => expect(membershipCalls().length).toBeGreaterThan(0));
    expect(membershipCalls()[0]).toContain(`/iam/v1/accounts/${CURRENT}/memberships`);
    expect(decodeURIComponent(membershipCalls()[0])).toContain(`filter=userId="${PERSON}"`);
  });

  it("сменилась область — сменился и вопрос", async () => {
    // verifies #1085 — источник аккаунта ХРАНИЛИЩЕ ОБЛАСТИ, а не контекст списка:
    // список пользователей охватывает несколько аккаунтов и определить аккаунт
    // членства не может by construction.
    const { contextApi } = await import("@shared/lib/context-store");
    contextApi.setAccount({ id: OTHER, name: "Одуванчик" });
    serve([wireMembership(OTHER, "ACTIVE", "Одуванчик")]);
    await mount();

    await waitFor(() => expect(membershipCalls().length).toBeGreaterThan(0));
    expect(membershipCalls()[0]).toContain(`/iam/v1/accounts/${OTHER}/memberships`);
  });

  // ── Сценарий 20: карточка МОЛЧИТ о других аккаунтах ───────────────────────

  it("не называет ни одного другого аккаунта, даже пришедшего в ответе", async () => {
    // verifies #1085
    //
    // Ответ несёт чужое членство ПЕРВЫМ. Край такого не отдаёт — чтение сужено
    // аккаунтом, — но карточка не вправе держаться на этом одним лишь порядком:
    // «взять первое» назвало бы чужой аккаунт при первом же расхождении, и вид
    // у ответа был бы такой же уверенный.
    serve([wireMembership(OTHER, "ACTIVE", "Одуванчик"), wireMembership(CURRENT, "ACTIVE", "Ромашка")]);
    const { container } = await mount();

    // Положительный контроль — членство в ТЕКУЩЕМ аккаунте названо. Без него
    // отрицания ниже истинны у карточки, которая молчит обо всём.
    await waitFor(() => expect(container.querySelector(`a[href="/iam/accounts/${CURRENT}"]`)).not.toBeNull());

    expect(container.textContent ?? "").not.toContain(OTHER);
    expect(container.textContent ?? "").not.toContain("Одуванчик");
    expect(container.querySelector(`a[href="/iam/accounts/${OTHER}"]`)).toBeNull();
  });

  it("не задаёт ни одного запроса о человеке без аккаунта", async () => {
    // verifies #1085 — вопрос без аккаунта отвечал бы про ВСЕ аккаунты человека
    // сразу, то есть был бы оракулом их состава.
    serve([wireMembership(CURRENT, "ACTIVE", "Ромашка")]);
    await mount();

    await waitFor(() => expect(membershipCalls().length).toBeGreaterThan(0));
    for (const url of membershipCalls()) {
      expect(url).toContain(`/iam/v1/accounts/${CURRENT}/memberships`);
    }
    expect(calls.filter((u) => u.includes("/me/memberships"))).toHaveLength(0);
  });

  it("не показывает ни числа аккаунтов, ни признака «есть ещё»", async () => {
    // verifies #1085 — состав раскрывает и намёк на него, не только перечень.
    serve([wireMembership(CURRENT, "ACTIVE", "Ромашка")]);
    const { container } = await mount();

    await waitFor(() => expect(container.querySelector(`a[href="/iam/accounts/${CURRENT}"]`)).not.toBeNull());
    const text = container.textContent ?? "";
    expect(text).not.toMatch(/ещё\s+\d|и ещё/i);
    expect(text).not.toMatch(/аккаунтов[:\s]+\d/i);
  });

  // ── Область не выбрана и членства нет ─────────────────────────────────────

  it("без выбранной области не спрашивает ничего и о членстве не утверждает", async () => {
    // verifies #1085 — карточка достижима без выбранной области (у маршрута
    // `users/:uid` нет стража), поэтому ветвь спроектирована, а не досталась
    // умолчанием.
    const { contextApi } = await import("@shared/lib/context-store");
    contextApi.setAccount(null);
    serve([wireMembership(CURRENT, "ACTIVE", "Ромашка")]);
    const { container } = await mount();

    await waitFor(() => expect(container).toBeTruthy());
    expect(membershipCalls()).toHaveLength(0);
    expect(container.querySelector("a[href^='/iam/accounts/']")).toBeNull();
    // Молчание ОБЪЯСНЯЕТСЯ: иначе «членства нет» и «область не выбрана»
    // читаются одинаково — пустым местом, то есть «не состоит нигде».
    expect(container.textContent ?? "").toMatch(/Аккаунт не выбран/);
  });

  it("если человек не состоит в текущем аккаунте — молчит, а не рисует прочерк", async () => {
    // verifies #1085 — прочерк читается как «значение не задано», тогда как не
    // задана не величина, а строка членства: её нет (ui.md, правило 9).
    serve([]);
    const { container } = await mount();

    await waitFor(() => expect(membershipCalls().length).toBeGreaterThan(0));
    expect(container.querySelector("a[href^='/iam/accounts/']")).toBeNull();
    expect(container.textContent ?? "").not.toContain("—");
  });
});
