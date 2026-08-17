// Security-group rule shaping, against vpc.v1.SecurityGroupRuleSpec.
//
// The protocol arm is discriminated by which of protocol_name / protocol_number is
// set. `protocol_number` is an **int64**, and protojson renders a 64-bit integer as
// a JSON STRING — so a rule read back from the server carries `"47"`, not `47`. A
// `typeof === "number"` test is therefore false for every rule the server ever
// sent, the arm resolves to "any", and sanitize deletes BOTH protocol keys: editing
// an unrelated part of a GRE rule silently widened it to every protocol, and
// answered 200.

import { sanitizeSgRule } from "./resource-registry";

describe("sanitizeSgRule protocol arm", () => {
  it("keeps a protocol number that arrived as the JSON string an int64 becomes", () => {
    const fromServer = { direction: "INGRESS", protocol_number: "47", cidr_blocks: { v4_cidr_blocks: ["10.0.0.0/8"] } };
    expect(sanitizeSgRule({ ...fromServer })).toMatchObject({ protocol_number: "47" });
  });

  it("keeps a protocol number typed into the form as a number", () => {
    expect(sanitizeSgRule({ direction: "INGRESS", protocol_number: 47 })).toMatchObject({ protocol_number: 47 });
  });

  it("still means ANY when neither arm is set", () => {
    const out = sanitizeSgRule({ direction: "INGRESS" });
    expect(out).not.toHaveProperty("protocol_name");
    expect(out).not.toHaveProperty("protocol_number");
  });

  it("keeps the named arm and drops the numeric one", () => {
    const out = sanitizeSgRule({ direction: "INGRESS", protocol_name: "TCP", protocol_number: "6" });
    expect(out.protocol_name).toBe("TCP");
    expect(out).not.toHaveProperty("protocol_number");
  });

  it("honours the explicit widening the operator asked for", () => {
    // `_protocol_mode: "any"` is the operator saying «any protocol» — that must
    // still clear both arms. The bug was widening WITHOUT being asked.
    const out = sanitizeSgRule({ direction: "INGRESS", _protocol_mode: "any", protocol_number: "47" });
    expect(out).not.toHaveProperty("protocol_number");
    expect(out).not.toHaveProperty("_protocol_mode");
  });

  // Выбранная ветвь номера без номера — НАЗВАННАЯ нехватка, а не молчаливое
  // расширение (#375). Ключ с `undefined` не переживает сериализацию тела:
  // ветвь исчезает, правило означает «любой протокол», и вызывающий получает
  // 200 на правило, которого не задавал. Ноль сервер отвергает явно, называя
  // поле («номер 0 неотличим от незаданного протокола»), и форма показывает
  // этот отказ подписью «Номер IANA».
  it("выбранная ветвь номера без значения доезжает нулём — отказ края назван, а не проглочен", () => {
    const out = sanitizeSgRule({ direction: "INGRESS", _protocol_mode: "number" });
    expect(out.protocol_number).toBe(0);
    expect(out).not.toHaveProperty("protocol_name");
  });

  it("законный номер по-прежнему доезжает как есть — положительный контроль", () => {
    // Без него «пустая ветвь даёт 0» могло бы означать «ветвь всегда даёт 0».
    expect(sanitizeSgRule({ direction: "INGRESS", _protocol_mode: "number", protocol_number: 47 })).toMatchObject({
      protocol_number: 47,
    });
    expect(sanitizeSgRule({ direction: "INGRESS", _protocol_mode: "number", protocol_number: "47" })).toMatchObject({
      protocol_number: "47",
    });
  });
});
