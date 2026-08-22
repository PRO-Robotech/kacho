// Ветви контракта, достижимые из создания, — против ТЕЛА, которое даёт форма.
//
// Соседний `oneof-form-coverage.test.ts` сверяет ветви с ИМЕНАМИ полей формы.
// Этого хватает там, где ветвь выражена полем (`health_check.http.path`), и не
// хватает там, где её называет дискриминатор, а на провод её кладёт `sanitize`
// (адрес, шлюз, правило группы). Поэтому здесь сверяется то же самое, но по
// ИСХОДУ: перечень ветвей контракта против перечня ветвей, которые форма
// действительно кладёт в тело.
//
// Форма ввода на ветвь — минимально законная: проба обязана СОЗДАТЬ условие
// ветви, а не проверить, что поле объявлено. Ветвь, которую нельзя выбрать
// никаким вводом, не работает ни при каком вводе — это и есть предмет #375.

import { REGISTRY } from "./resource-registry";
import { oneofBranches } from "@shared/test/oneof-branch-coverage";
import { instanceBody, localDiskRefusal, containerKindRefusal } from "@shared/test/instance-branch-coverage";

/** Ветви, которые тело несёт на верхнем уровне (или по указанному пути). */
function branchesInBody(body: Record<string, unknown>, at: string[] = []): string[] {
  let node: unknown = body;
  for (const seg of at) node = (node as Record<string, unknown> | undefined)?.[seg];
  if (!node || typeof node !== "object") return [];
  return Object.keys(node);
}

describe("адрес: каждая ветвь спецификации выразима формой", () => {
  const spec = REGISTRY["addresses"];
  const form: Record<string, string> = {
    external_ipv4_address_spec: "external",
    external_ipv6_address_spec: "external_v6",
    internal_ipv4_address_spec: "internal",
    internal_ipv6_address_spec: "internal_v6",
  };

  it("перечень контракта совпадает с перечнем, который даёт форма", () => {
    const contract = oneofBranches("vpc/v1/address_service.proto", "CreateAddressRequest", "address_spec");
    const expressible = contract.filter((branch) => {
      const kind = form[branch];
      if (!kind) return false;
      const body = spec.sanitize!({
        project_id: "prj-1",
        _address_kind: kind,
        [branch]: branch.startsWith("internal") ? { subnet_id: "sub-1" } : { zone_id: "ru-a" },
      });
      return branchesInBody(body).includes(branch);
    });
    expect(expressible).toEqual(contract);
  });

  it("выбранная ветвь остаётся в теле ОДНА — отрицание в паре с положительным", () => {
    const body = spec.sanitize!({
      project_id: "prj-1",
      _address_kind: "internal_v6",
      external_ipv4_address_spec: { zone_id: "ru-a" },
      internal_ipv6_address_spec: { subnet_id: "sub-1" },
    });
    expect(body.internal_ipv6_address_spec).toEqual({ subnet_id: "sub-1" });
    expect(body).not.toHaveProperty("external_ipv4_address_spec");
  });

  it("область внутренней ветви — её единственная ветвь — тоже доезжает", () => {
    for (const [message, kind, field] of [
      ["InternalIpv4AddressSpec", "internal", "internal_ipv4_address_spec"],
      ["InternalIpv6AddressSpec", "internal_v6", "internal_ipv6_address_spec"],
    ] as const) {
      const contract = oneofBranches("vpc/v1/address_service.proto", message, "scope");
      const body = spec.sanitize!({
        project_id: "prj-1",
        _address_kind: kind,
        [field]: { subnet_id: "sub-1" },
      });
      expect(branchesInBody(body, [field])).toEqual(contract);
    }
  });
});

describe("шлюз: каждая ветвь вида выразима формой", () => {
  const spec = REGISTRY["gateways"];

  it("перечень контракта совпадает с перечнем, который даёт форма", () => {
    const contract = oneofBranches("vpc/v1/gateway_service.proto", "CreateGatewayRequest", "gateway");
    const form: Record<string, string> = {
      nat_gateway_spec: "nat",
      egress_only_gateway_spec: "egress_only",
    };
    const expressible = contract.filter((branch) => {
      const body = spec.sanitize!({ project_id: "prj-1", subnet_id: "sub-1", _kind: form[branch] });
      return branchesInBody(body).includes(branch);
    });
    expect(expressible).toEqual(contract);
  });

  it("вторая ветвь в теле не появляется — отрицание в паре с положительным", () => {
    const body = spec.sanitize!({ project_id: "prj-1", subnet_id: "sub-1", _kind: "egress_only" });
    expect(body).toHaveProperty("egress_only_gateway_spec");
    expect(body).not.toHaveProperty("nat_gateway_spec");
  });
});

describe("вид машины: каждая ветвь спецификации выразима формой", () => {
  const spec = REGISTRY["compute-instances"];

  it("перечень контракта совпадает с перечнем, который даёт форма", () => {
    const contract = oneofBranches("compute/v1/instance_service.proto", "CreateInstanceRequest", "spec");
    const form: Record<string, Record<string, unknown>> = {
      vm_spec: instanceBody(spec, "VM"),
      container_spec: instanceBody(spec, "CONTAINER"),
    };
    const expressible = contract.filter((branch) => (form[branch] ?? {})[branch] !== undefined);
    expect(expressible).toEqual(contract);
  });

  it("вторая ветвь в теле не появляется — отрицание в паре с положительным", () => {
    expect(instanceBody(spec, "VM")).not.toHaveProperty("container_spec");
    expect(instanceBody(spec, "CONTAINER")).not.toHaveProperty("vm_spec");
  });

  it("ветвь местного диска формы не имеет — и у этого есть предикат снятия", () => {
    // Родитель ветви (`local_disk_specs`) отвергается сервером синхронно, с
    // именем поля: форма для отвергаемого поля была бы мёртвым интерфейсом.
    // Отказ читается ИЗ ДЕРЕВА — исчезнет он, исчезнет и основание.
    expect(localDiskRefusal()).toContain("not supported");
    expect(oneofBranches("compute/v1/instance_service.proto", "AttachedLocalDiskSpec", "type")).toEqual([
      "physical_local_disk",
    ]);
  });

  it("вид «контейнер» форма отвергает ДО отправки — и у этого есть предикат снятия", () => {
    // Вид объявлен контрактом несоздаваемым, и отказ читается ИЗ ДЕРЕВА:
    // станет вид создаваемым — покраснеет эта проба, а не арендатор.
    expect(containerKindRefusal()).toContain("not creatable");
    expect(spec.validate!({ instance_kind: "CONTAINER", boot_source: { type: "storage.image", id: "img-1" } })).toMatch(
      /контейнер/i,
    );
    // (+) отрицание в паре с положительным: вид VM тем же путём проходит.
    expect(
      spec.validate!({ instance_kind: "VM", boot_source: { type: "storage.image", id: "img-1" } }),
    ).toBeNull();
  });
});

describe("балансировщик: каждая ветвь источника VIP выразима формой", () => {
  const spec = REGISTRY["load-balancers"];

  /** Тело создания при выбранном источнике семейства IPv4. */
  function makeBody(mode: string, placement: string, fam: Record<string, unknown> = {}): Record<string, unknown> {
    return spec.sanitize!({
      project_id: "prj-1",
      region_id: "reg-1",
      placement,
      _v4_source: mode,
      v4_source: fam,
      _v6_source: "off",
      v6_source: {},
    });
  }

  it("перечень контракта совпадает с перечнем, который даёт форма", () => {
    const contract = oneofBranches("loadbalancer/v1/network_load_balancer.proto", "VipSource", "source");
    const form: Record<string, Record<string, unknown>> = {
      public: makeBody("public", "EXTERNAL_REGIONAL"),
      subnet_id: makeBody("subnet", "INTERNAL_REGIONAL", { subnet_id: "sub-1" }),
      address_id: makeBody("address", "EXTERNAL_REGIONAL", { address_id: "adr-1" }),
    };
    const expressible = contract.filter((branch) => branchesInBody(form[branch] ?? {}, ["v4_source"]).includes(branch));
    expect(expressible).toEqual(contract);
  });

  it("семейство без источника в тело не уезжает — отрицание в паре с положительным", () => {
    const body = makeBody("public", "EXTERNAL_REGIONAL");
    expect(body).toHaveProperty("v4_source");
    expect(body).not.toHaveProperty("v6_source");
  });
});

describe("правило группы безопасности: каждая ветвь цели и протокола выразима формой", () => {
  const spec = REGISTRY["security-groups"];

  /** Тело правила, как его отдаёт форма группы при выбранном виде цели. */
  function makeRule(rule: Record<string, unknown>): Record<string, unknown> {
    const body = spec.sanitize!({
      project_id: "prj-1",
      network_id: "net-1",
      rule_specs: [{ direction: "INGRESS", ...rule }],
    }) as { rule_specs: Array<Record<string, unknown>> };
    return body.rule_specs[0];
  }

  it("перечень ветвей цели совпадает с перечнем, который даёт форма", () => {
    const contract = oneofBranches("vpc/v1/security_group_service.proto", "SecurityGroupRuleSpec", "target");
    const form: Record<string, [string, unknown]> = {
      cidr_blocks: ["cidr", { v4_cidr_blocks: ["10.0.0.0/8"] }],
      security_group_id: ["sg", "sg-1"],
      cidr_group_id: ["cidr-group", "cg-1"],
    };
    const expressible = contract.filter((branch) => {
      const [kind, value] = form[branch] ?? [];
      if (!kind) return false;
      const out = makeRule({ _target_kind: kind, [branch]: value });
      return out[branch] !== undefined;
    });
    expect(expressible).toEqual(contract);
  });

  it("перечень ветвей протокола совпадает с перечнем, который даёт форма", () => {
    const contract = oneofBranches("vpc/v1/security_group_service.proto", "SecurityGroupRuleSpec", "protocol");
    const form: Record<string, [string, unknown]> = {
      protocol_name: ["name", "tcp"],
      protocol_number: ["number", 47],
    };
    const expressible = contract.filter((branch) => {
      const [mode, value] = form[branch] ?? [];
      if (!mode) return false;
      const out = makeRule({
        _protocol_mode: mode,
        [branch]: value,
        _target_kind: "cidr",
        cidr_blocks: { v4_cidr_blocks: ["10.0.0.0/8"] },
      });
      return out[branch] !== undefined;
    });
    expect(expressible).toEqual(contract);
  });

  it("две ветви цели в одном правиле не уезжают — отрицание в паре с положительным", () => {
    const out = makeRule({ _target_kind: "sg", security_group_id: "sg-1", cidr_group_id: "cg-1" });
    expect(out.security_group_id).toBe("sg-1");
    expect(out).not.toHaveProperty("cidr_group_id");
  });
});
