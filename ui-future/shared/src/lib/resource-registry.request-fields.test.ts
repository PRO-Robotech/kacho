// Every spec-driven form field must name a field of the request message it
// reaches. The edge parses with protojson DiscardUnknown, so one that does not is
// dropped without a word — the operator sets it, the call returns 200, and the
// setting was never applied.
//
// Ground truth per case is cited inline from proto/kacho/cloud/**. Create-side
// misses are the silent ones; Update-side misses are loud (the server's mask
// known-set rejects them), but both are listed here because both are wrong and
// the form is the single place that decides.

import { REGISTRY, applyFieldDefaults } from "./resource-registry";
import { computeUpdateMask } from "./update-mask";

const asObj = (v: unknown) => v as Record<string, unknown>;

/** Top-level keys a Create body can carry: template + defaults + declared fields. */
function createKeys(specId: string): Set<string> {
  const spec = REGISTRY[specId];
  const tpl = applyFieldDefaults(spec.fields, asObj(spec.template({ projectId: "prj-1", accountId: "acc-1" })));
  const body = spec.sanitize ? spec.sanitize(tpl) : tpl;
  const keys = new Set(Object.keys(body));
  for (const f of spec.fields ?? []) {
    if (!f.hidden && !f.editHidden) keys.add(f.name.split(".")[0]);
  }
  return keys;
}

/** Every path the edit form can put into update_mask. */
function maskable(specId: string): string[] {
  const spec = REGISTRY[specId];
  const fields = spec.fields ?? [];
  const before: Record<string, unknown> = {};
  const after: Record<string, unknown> = {};
  for (const f of fields) {
    before[f.name.split(".")[0]] = "before";
    after[f.name.split(".")[0]] = "after";
  }
  return computeUpdateMask(before, after, fields).map((p) => p.split(".")[0]);
}

describe("spec fields name real request fields", () => {
  // iam.v1.CreateAccountRequest {name, description, labels, owner_user_id};
  // UpdateAccountRequest {account_id, update_mask, name, description, labels}.
  // Neither carries deletion_protection — nor does the Account resource.
  it("accounts does not offer a deletion-protection the message has no field for", () => {
    expect(createKeys("accounts")).not.toContain("deletion_protection");
    expect(maskable("accounts")).not.toContain("deletion_protection");
  });

  // vpc.v1.CreateSecurityGroupRequest: the rules field is `rule_specs` (tag 6).
  // There is no `rules` at any depth — an SG created with authored rules was
  // created default-deny, with a 200.
  it("security-groups authors rules under the name Create declares", () => {
    const keys = createKeys("security-groups");
    expect(keys).toContain("rule_specs");
    expect(keys).not.toContain("rules");
  });

  // UpdateSecurityGroupRequest {security_group_id, update_mask, name, description,
  // labels, rule_specs} — network_id is Create-only.
  it("security-groups never masks the network it was created in", () => {
    expect(maskable("security-groups")).not.toContain("network_id");
  });

  // vpc.v1.CreateNetworkInterfaceRequest / UpdateNetworkInterfaceRequest both
  // carry bandwidth_limit_mbps. The edge rejects a non-empty value on a stand
  // whose executor does not declare the capability — that is a stand property, so
  // the form must offer the field on BOTH paths or the setting is unreachable
  // where it does work.
  it("network-interfaces offers the bandwidth limit on create and on edit", () => {
    expect(createKeys("network-interfaces")).toContain("bandwidth_limit_mbps");
    expect(maskable("network-interfaces")).toContain("bandwidth_limit_mbps");
  });

  // loadbalancer.v1.CreateListenerRequest {load_balancer_id, name, description,
  // labels, protocol, port, target_port, default_target_group_id,
  // target_group_id}. A listener inherits its project from the parent load
  // balancer. proxy_protocol_v2 is retired from the contract (reserved): no
  // reviewed L4 dataplane can insert the framing, so the field was accepted and
  // never executed.
  it("listeners does not send a project the message has no field for", () => {
    expect(createKeys("listeners")).not.toContain("project_id");
  });

  // UpdateListenerRequest {listener_id, update_mask, name, description, labels,
  // default_target_group_id, target_group_id} — everything else is Create-only,
  // as each field's own description already says.
  it("listeners masks only what Update carries", () => {
    const mutable = ["name", "description", "labels", "default_target_group_id", "target_group_id"];
    expect(maskable("listeners").filter((p) => !mutable.includes(p))).toEqual([]);
  });

  // UpdateNetworkLoadBalancerRequest carries no region_id / type: both are
  // Create-only (tags 5/6 on the Create message).
  it("load-balancers masks neither region nor type", () => {
    const m = maskable("load-balancers");
    expect(m).not.toContain("region_id");
    expect(m).not.toContain("type");
  });

  // NLB CONTRACT: `placement` is the SOLE authoritative mode input on Create
  // (EXTERNAL_REGIONAL | INTERNAL_REGIONAL | INTERNAL_ZONAL). `type` and
  // `placement_type` are derived output-only projections, retained on the request
  // ONLY so that a client setting them gets an explicit InvalidArgument — so a
  // create form that sends them cannot create a load balancer at all, and one that
  // omits `placement` has no way to say what it wants.
  it("load-balancers sends the mode input, and none of the write-reject projections", () => {
    const keys = createKeys("load-balancers");
    expect(keys).toContain("placement");
    expect(keys).not.toContain("type");
    expect(keys).not.toContain("placement_type");
    const placement = (REGISTRY["load-balancers"].fields ?? []).find((f) => f.name === "placement");
    expect(placement?.required).toBe(true);
    // Create-only: UpdateNetworkLoadBalancerRequest carries no placement.
    expect(maskable("load-balancers")).not.toContain("placement");
  });

  // CreateTargetGroupRequest.port is required (1..65535): every target receives
  // forwarded traffic on it. Unreachable from the form meant no target group could
  // be created from the console at all.
  it("target-groups can express the backend port Create requires", () => {
    const keys = createKeys("target-groups");
    expect(keys).toContain("port");
    const port = (REGISTRY["target-groups"].fields ?? []).find((f) => f.name === "port");
    expect(port?.required).toBe(true);
  });

  // loadbalancer.v1.HealthCheck: `reserved 1; reserved "name";` and the options
  // oneof arms are tcp(6)/http(7)/https(8)/grpc(9) — `tcp_options` was replaced.
  it("target-groups health check uses the live option arm and no retired name", () => {
    const tpl = asObj(REGISTRY["target-groups"].template({ projectId: "prj-1" }));
    const hc = asObj(tpl.health_check);
    expect(hc).not.toHaveProperty("name");
    expect(hc).not.toHaveProperty("tcp_options");
    expect(asObj(hc.tcp).port).toBe(80);
    const names = (REGISTRY["target-groups"].fields ?? []).map((f) => f.name);
    expect(names).not.toContain("health_check.name");
    expect(names).not.toContain("health_check.tcp_options.port");
    expect(names).toContain("health_check.tcp.port");
  });

  // iam.v1.CreateAccessBindingRequest: the anchor pair is `scope_type` (dotted)
  // + `scope_id`, both `(required) = true`. `resource_type`/`resource_id` are not
  // fields of this message at any tag — the AccessBinding itself tombstones the
  // whole legacy scope projection (`reserved 15,16,17,18; reserved "scope",
  // "scope_ref", …`).
  //
  // The spec is not create-capable — the bespoke page builds the body — but the
  // template is still the registry's written statement of the request's shape,
  // and a statement in the wrong vocabulary is what the next contributor copies.
  it("access-bindings anchors the grant in the vocabulary Create declares", () => {
    const tpl = asObj(REGISTRY["access-bindings"].template({ accountId: "acc-1" }));
    expect(tpl).not.toHaveProperty("resource_type");
    expect(tpl).not.toHaveProperty("resource_id");
    expect(tpl).not.toHaveProperty("scope_ref");
    expect(tpl.scope_type).toBe("iam.account");
    expect(tpl.scope_id).toBe("acc-1");
  });

  // vpc.v1.CreateAddressPoolRequest: `cidr_blocks` (tag 5) is `reserved`, split into
  // `v4_cidr_blocks` (11) + `v6_cidr_blocks` (12). The Create body must name the
  // split pair and must not name the retired one at any depth.
  it("address-pools names the split cidr pair and not the retired one", () => {
    const keys = createKeys("address-pools");
    expect(keys).toContain("v4_cidr_blocks");
    expect(keys).toContain("v6_cidr_blocks");
    expect(keys).not.toContain("cidr_blocks");
  });

  // `AccessTarget target = 15` is `REQUIRED` on CreateAccessBindingRequest, and the
  // service rejects its absence in the first statement: `targetFromProto` answers a
  // nil target and an empty oneof alike with INVALID_ARGUMENT «target is required;
  // use target.allInScope{} to grant all objects under the anchor»
  // (services/iam/.../access_binding/delta_input.go). A skeleton without it states a
  // request shape the server refuses — and it is the skeleton, not the bespoke page,
  // that the next contributor copies. The arm is spelled the way the console's own
  // body builder spells it (`buildCreateAccessBindingBody`, shared/src/api/iam.ts):
  // per-object `resources` when the operator picks objects, whole-anchor otherwise.
  it("access-bindings states the target the grant cannot be created without", () => {
    const tpl = asObj(REGISTRY["access-bindings"].template({ accountId: "acc-1" }));
    expect(tpl).toHaveProperty("target");
    const target = asObj(tpl.target);
    // Ровно один арм oneof — «оба» на проводе означало бы, что выигрывает
    // последний, и выбор делает не форма, а порядок ключей.
    const arms = Object.keys(target).filter((k) => k === "all_in_scope" || k === "resources");
    expect(arms).toHaveLength(1);
    expect(target.all_in_scope).toEqual({});
  });
});
