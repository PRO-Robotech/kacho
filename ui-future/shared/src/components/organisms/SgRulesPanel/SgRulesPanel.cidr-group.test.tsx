// Цель правила, ссылающаяся на ЧУЖОЙ ресурс, обязана быть ссылкой — иконка
// типа, имя, переход (канон консоли, правило 2). Столбец «Источник» показывал
// моноширинный идентификатор: из него нельзя ни узнать, на что правило
// ссылается, ни открыть это.
//
// Третья ветвь цели (`cidr_group_id`) появляется здесь впервые, и вместе с ней
// правится ОДИН предмет целиком: обе ссылочные ветви (группа безопасности и
// набор префиксов) показываются одинаково. Оставить одну ссылкой, а другую
// строкой значило бы показать один предмет двумя видами.

import { jest } from "@jest/globals";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@shared/api/client";
import { antdStub } from "@shared/test/antd-stub";

const list = jest.fn<(path: string, params?: unknown) => Promise<unknown>>();

jest.unstable_mockModule("antd", () => antdStub());
jest.unstable_mockModule("@shared/api/client", () => ({
  api: { update: jest.fn(), get: jest.fn(), list, create: jest.fn(), delete: jest.fn(), action: jest.fn() },
  ApiError,
}));

const { SgRulesPanel } = await import("./SgRulesPanel");
const { PageHeaderSlotProvider, HeaderRightSlot } = await import("@shared/components/molecules/PageHeaderSlot");
const { MemoryRouter } = await import("react-router");

type Rule = Parameters<typeof SgRulesPanel>[0]["rules"][number];

function show(rules: Rule[]) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={["/projects/prj-1/vpc/security-groups/sg-1"]}>
        <PageHeaderSlotProvider>
          <HeaderRightSlot />
          <SgRulesPanel sgId="sg-1" projectId="prj-1" rules={rules} networkId="net-1" />
        </PageHeaderSlotProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  jest.clearAllMocks();
  list.mockImplementation((path: string) =>
    Promise.resolve(
      path === "/vpc/v1/cidrGroups"
        ? { cidr_groups: [{ id: "cdg-1", name: "office" }] }
        : { security_groups: [{ id: "sg-9", name: "backend" }] },
    ),
  );
});

describe("источник правила — ссылка на то, на что правило ссылается", () => {
  it("набор префиксов назван своим типом и ведёт на свою карточку", async () => {
    show([{ id: "sgr-1", direction: "INGRESS", cidr_group_id: "cdg-1" } as Rule]);

    expect(screen.getByText("Набор префиксов")).toBeInTheDocument();
    const link = await screen.findByRole("link", { name: "office" });
    expect(link).toHaveAttribute("href", "/projects/prj-1/vpc/cidr-groups/cdg-1");
  });

  it("группа безопасности показана ТЕМ ЖЕ видом — один предмет, один вид", async () => {
    show([{ id: "sgr-2", direction: "EGRESS", security_group_id: "sg-9" } as Rule]);

    const link = await screen.findByRole("link", { name: "backend" });
    expect(link).toHaveAttribute("href", "/projects/prj-1/vpc/security-groups/sg-9");
  });

  it("набор блоков остаётся текстом — ссылаться там не на что", () => {
    // Контроль в обратную сторону: без него «ссылка есть» зеленело бы на
    // реализации, оборачивающей в ссылку что угодно, включая CIDR.
    show([{ id: "sgr-3", direction: "INGRESS", cidr_blocks: { v4_cidr_blocks: ["10.0.0.0/8"] } } as Rule]);

    expect(screen.getByText("10.0.0.0/8")).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "10.0.0.0/8" })).not.toBeInTheDocument();
  });
});
