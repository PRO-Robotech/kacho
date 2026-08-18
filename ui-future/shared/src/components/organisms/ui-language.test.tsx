// Язык подписей на ДВУХ поверхностях, с которых заведена задача #478: форма
// сетевого интерфейса и редактор правил группы безопасности.
//
// Почему проба нужна ОТДЕЛЬНО от гейта `scripts/check-ui-language.mjs`. Гейт
// читает исходники и судит о литералах; он не знает, доехал ли литерал до
// экрана. Здесь предмет обратный — ВИДИМЫЙ текст: компонент рендерится, и
// утверждение делается о том, что показано человеку. Первое ловит подпись,
// написанную по-английски; второе — что показана именно исправленная.
//
// Утверждения парные, и это не избыточность: «есть русское» без «нет
// английского» проходит на форме, где стоят ОБА (гибрид «Группа безопасности
// (Security Group)» — ровно тот вид, с которого заведена задача).

import { jest } from "@jest/globals";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@shared/api/client";
import { antdStub } from "@shared/test/antd-stub";

const list = jest.fn<(path: string, q?: unknown) => Promise<Record<string, unknown>>>();

jest.unstable_mockModule("@shared/api/client", () => ({
  api: { list, create: jest.fn(), get: jest.fn(), update: jest.fn(), delete: jest.fn(), action: jest.fn() },
  ApiError,
}));

jest.unstable_mockModule("@shared/lib/toast", () => ({
  toast: { error: jest.fn(), success: jest.fn(), info: jest.fn(), loading: jest.fn(), dismiss: jest.fn() },
}));

jest.unstable_mockModule("@shared/lib/use-operation", () => ({
  useInvalidateResourceList: () => jest.fn(),
  useOperation: () => ({ data: undefined }),
}));

// Подписи панелей гармошки приходят пропом `items` — их рисует общий
// стенд-заменитель. Своей копии здесь больше нет (#570).
jest.unstable_mockModule("antd", () => antdStub());

const { InlineNetworkInterfaceCreateForm } = await import(
  "@shared/components/organisms/InlineNetworkInterfaceCreateForm"
);
const { SgRulesEditor } = await import("@shared/components/organisms/form/SgRulesEditor");

beforeEach(() => {
  jest.clearAllMocks();
  list.mockImplementation((path: string) => {
    if (path.includes("/subnets")) return Promise.resolve({ subnets: [{ id: "sub-1", name: "внутренняя" }] });
    if (path.includes("/security-groups")) return Promise.resolve({ security_groups: [{ id: "sg-1", name: "веб" }] });
    return Promise.resolve({});
  });
});

/**
 * Всё, что человек может прочитать: текст экрана И подсказки.
 *
 * Подсказки обязаны входить: заменитель `Tooltip` кладёт строковую подпись в
 * атрибут `title`, а не в текст, — проба по одному `textContent` осталась бы
 * зелёной на подсказке «Security Groups, прилинкованные к NIC», то есть ровно
 * на том, что владелец увидел на экране.
 */
const shown = () =>
  (document.body.textContent ?? "") +
  " " +
  [...document.querySelectorAll("[title]")].map((e) => e.getAttribute("title")).join(" ");

describe("форма сетевого интерфейса называет сущности по-русски", () => {
  it("группы безопасности подписаны по-русски, и английского имени рядом нет", async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={client}>
        <InlineNetworkInterfaceCreateForm projectId="prj-1" onCancel={jest.fn()} />
      </QueryClientProvider>,
    );

    await waitFor(() => expect(shown()).toContain("Группы безопасности"));
    // Положительный контроль выше уже сработал: форма отрисована и текст читается.
    // Значит отрицание ниже утверждает об отрисованной форме, а не о пустом теле.
    expect(shown()).not.toContain("Security Group");
    expect(shown()).not.toContain("NetworkInterface");
  });
});

describe("редактор правил называет источник по-русски", () => {
  it("вариант «Группа безопасности» подписан по-русски, английского имени нет", () => {
    const rules = [
      {
        direction: "INGRESS",
        _protocol_mode: "name",
        protocol_name: "TCP",
        ports: { from_port: 443, to_port: 443 },
        _target_kind: "sg",
        security_group_id: "sg-1",
      },
    ];
    render(<SgRulesEditor pathPrefix="" value={{ rules }} onChange={jest.fn()} path="rules" />);

    // Панель правила приходит СВЁРНУТОЙ — как у пользователя: её содержимое
    // видно после раскрытия, и до него подписи вариантов на экране нет.
    fireEvent.click(screen.getByRole("button", { expanded: false }));

    const options = [...document.querySelectorAll("option")].map((o) => o.textContent);
    // Положительный контроль: варианты вообще отрисованы — иначе отрицание ниже
    // было бы истинным на пустом наборе.
    expect(options.length).toBeGreaterThan(0);
    expect(options).toContain("Группа безопасности");
    expect(options).not.toContain("Security Group");
    expect(shown()).not.toContain("Security Group");
  });
});
