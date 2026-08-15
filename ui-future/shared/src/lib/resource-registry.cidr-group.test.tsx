// Набор префиксов (`CidrGroup`) заведён в стволе как полноценный ресурс vpc —
// со списком, чтением, созданием, правкой, удалением, двумя глаголами состава и
// подмаршрутом операций. В консоли его не было вовсе: ни ключа в реестре, ни
// страницы, — при том что перечень подмаршрутов операций его адрес уже называл.
// То есть половина связи существовала, а войти в ресурс было неоткуда.
//
// Проба утверждает ровно предмет задачи: набор объявлен ресурсом консоли и его
// форма выражает то, что требует и принимает контракт. Отрицания стоят в паре с
// положительным контролем — иначе «поле не в маске» зеленело бы и на реестре,
// где спеки нет вовсе.
//
// Контракт (ground truth), сверено по proto/kacho/cloud/vpc/v1/cidr_group*.proto:
//   CreateCidrGroupRequest {project_id(required), name, description, labels,
//                           v4_cidr_blocks, v6_cidr_blocks}
//   UpdateCidrGroupRequest {cidr_group_id, update_mask, name, description, labels}
//     — состав через Update НЕ меняется, только :add-cidr-blocks/:remove-cidr-blocks
//   CidrGroup {…, v4_cidr_blocks, v6_cidr_blocks, cidr_block_count°, used_by°}

import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";

import { DETAIL_EXTENSIONS } from "@shared/components/organisms/ResourceDetailExtensions";

import { REGISTRY, applyFieldDefaults } from "./resource-registry";
import { formatCellByFormat } from "./spec-columns";
import { computeUpdateMask } from "./update-mask";
import { hasOperationsSubroute } from "./operations-subroute";

const asObj = (v: unknown) => v as Record<string, unknown>;

const realFetch = globalThis.fetch;
function stubList(payload: unknown) {
  globalThis.fetch = () =>
    Promise.resolve({
      ok: true,
      status: 200,
      statusText: "OK",
      text: () => Promise.resolve(JSON.stringify(payload)),
    } as Response);
}
afterEach(() => {
  globalThis.fetch = realFetch;
});

describe("набор префиксов объявлен ресурсом консоли", () => {
  it("спека есть и адресует поверхность ствола", () => {
    const spec = REGISTRY["cidr-groups"];
    expect(spec).toBeDefined();
    expect(spec.apiPath).toBe("/vpc/v1/cidrGroups");
    expect(spec.payloadKey).toBe("cidr_groups");
    expect(spec.scope).toBe("project");
    expect(spec.ops).toEqual({ create: true, update: true, delete: true });
    // Мутации отвечают Operation (ban #9) — ответ без operation-id есть
    // нарушение контракта, а не синхронный успех.
    expect(spec.mutationsReturnOperation).toBe(true);
  });

  it("несёт вкладку операций — и она берётся из перечня, а не собирается на месте", () => {
    expect(hasOperationsSubroute(REGISTRY["cidr-groups"].apiPath)).toBe(true);
    // Контроль в обратную сторону: у каталожного ресурса подмаршрута нет, и
    // проверка обязана это различать.
    expect(hasOperationsSubroute(REGISTRY.regions.apiPath)).toBe(false);
  });

  it("несёт пустое состояние — иначе пустой список молчит о том, что это за ресурс", () => {
    expect(REGISTRY["cidr-groups"].emptyState?.title).toBeTruthy();
    expect(REGISTRY["cidr-groups"].emptyState?.body).toBeTruthy();
  });

  it("форма создания выражает состав набора и отдаёт его строками", () => {
    const spec = REGISTRY["cidr-groups"];
    const tpl = applyFieldDefaults(spec.fields, asObj(spec.template({ projectId: "prj-1" })));
    const body = spec.sanitize ? spec.sanitize({ ...tpl, v4_cidr_blocks: [{ value: "10.0.0.0/8" }] }) : tpl;
    expect(body.project_id).toBe("prj-1");
    // На проводе состав — массив СТРОК; в форме он живёт объектами {value}.
    expect(body.v4_cidr_blocks).toEqual(["10.0.0.0/8"]);
    // Пустая строка не уезжает — иначе край получил бы член, которого оператор
    // не вводил.
    expect(spec.sanitize!({ ...tpl, v6_cidr_blocks: [{ value: "" }] }).v6_cidr_blocks).toEqual([]);
  });

  it("правка НИКОГДА не отправляет состав — его меняют глаголы", () => {
    const spec = REGISTRY["cidr-groups"];
    const fields = spec.fields ?? [];
    const before: Record<string, unknown> = {};
    const after: Record<string, unknown> = {};
    for (const f of fields) {
      before[f.name.split(".")[0]] = "before";
      after[f.name.split(".")[0]] = "after";
    }
    const mask = computeUpdateMask(before, after, fields).map((p) => p.split(".")[0]);
    expect(mask).not.toContain("v4_cidr_blocks");
    expect(mask).not.toContain("v6_cidr_blocks");
    // Положительный контроль: маска вообще что-то содержит — иначе отрицание
    // выше выполнялось бы на пустом списке.
    expect(mask).toContain("name");
  });

  it("потребители набора показаны СПИСКОМ ссылок, а не строкой идентификаторов", async () => {
    const spec = REGISTRY["cidr-groups"];
    const col = spec.columns.find((c) => c.path === "used_by");
    expect(col?.format).toBe("references");

    stubList({ security_groups: [{ id: "sg-1", name: "office" }] });
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const row = { used_by: [{ referrer: { type: "vpc.securityGroup", id: "sg-1", name: "office" } }] };
    render(
      <QueryClientProvider client={qc}>
        <MemoryRouter initialEntries={["/projects/prj-1/vpc/cidr-groups/cdg-1"]}>
          {formatCellByFormat(col!, row, { projectId: "prj-1" })}
        </MemoryRouter>
      </QueryClientProvider>,
    );

    const link = await screen.findByRole("link", { name: "office" });
    expect(link).toHaveAttribute("href", "/projects/prj-1/vpc/security-groups/sg-1");
  });

  it("карточка называет потребителей тем же видом, что и остальные ресурсы", () => {
    // Строка «Кем используется» на карточке — общий `ConsumersFact`, а не своя
    // разметка: то же поле контракта у адреса и у группы правил рисуется им же.
    expect(DETAIL_EXTENSIONS["cidr-groups"]).toBeDefined();
    const items = DETAIL_EXTENSIONS["cidr-groups"].overviewExtra!({
      data: { id: "cdg-1", used_by: [] },
      projectId: "prj-1",
      detailBase: "/projects/prj-1/vpc/cidr-groups/cdg-1",
      navigate: () => {},
    });
    expect(items.map((i) => i.label)).toContain("Кем используется");
  });
});
