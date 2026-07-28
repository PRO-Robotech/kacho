// NLB-1c contract lock for the target-groups spec.
//
// Ground truth: proto/kacho/cloud/loadbalancer/v1 — Create/UpdateTargetGroupRequest
// both RESERVE the names `deregistration_delay_seconds` and `slow_start_seconds`
// (tags 9/10 and 8/9 respectively) and replace them with
// `google.protobuf.Duration deregistration_delay` / `slow_start`. A body still
// keyed on the int-seconds names is dropped at the edge without a word, so the
// drain timeout the operator typed never reached the server and the call returned
// 200 — and on Update the retired name in the mask is a 400, since protojson
// rejects a FieldMask path containing "_" before the known-set is even consulted.
//
// protojson renders a Duration as a seconds string with a trailing "s" ("300s").

import { REGISTRY } from "./resource-registry";

const asObj = (v: unknown) => v as Record<string, unknown>;
const RETIRED = ["deregistration_delay_seconds", "slow_start_seconds"];

describe("target-groups registry contract (NLB-1c)", () => {
  const spec = REGISTRY["target-groups"];

  it("declares no form field addressing a retired request field", () => {
    const names = (spec.fields ?? []).map((f) => f.name.split(".")[0]);
    for (const retired of RETIRED) {
      expect(names).not.toContain(retired);
    }
    expect(names).toContain("deregistration_delay");
  });

  it("templates the Duration channel, not the int-seconds one", () => {
    const t = asObj(spec.template({ projectId: "prj-1" }));
    for (const retired of RETIRED) {
      expect(t).not.toHaveProperty(retired);
    }
    // protojson reads a Duration from a seconds string with a trailing "s".
    expect(t.deregistration_delay).toBe("300s");
  });

  it("sanitize renders an operator-entered number as a Duration", () => {
    const body = spec.sanitize!({ ...asObj(spec.template({ projectId: "prj-1" })), deregistration_delay: 45 });
    expect(body.deregistration_delay).toBe("45s");
    // 0 is a legal drain timeout and must survive as an explicit "0s".
    expect(spec.sanitize!({ deregistration_delay: 0 }).deregistration_delay).toBe("0s");
    // Left blank → omitted, so the server applies its own default.
    expect(spec.sanitize!({ deregistration_delay: "" })).not.toHaveProperty("deregistration_delay");
  });

  it("hydrate turns the Duration back into the number the form edits", () => {
    expect(asObj(spec.hydrate!({ deregistration_delay: "300s" })).deregistration_delay).toBe(300);
    expect(asObj(spec.hydrate!({})).deregistration_delay).toBeUndefined();
  });
});
