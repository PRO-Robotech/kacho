import { buildAttachPayload, buildDetachPayload, attachedTargetGroupIds } from "./resources";

// Парные verb-RPC :attachTargetGroup / :detachTargetGroup имеют РАЗНУЮ форму
// request-body (attach — вложенный, detach — плоский); строить по proto-форме.
describe("NLB attach/detach payload builders", () => {
  it("buildAttachPayload — вложенный attached_target_group", () => {
    expect(buildAttachPayload("tgr-1")).toEqual({ attached_target_group: { target_group_id: "tgr-1" } });
  });

  it("buildAttachPayload — null при пустом id", () => {
    expect(buildAttachPayload(undefined)).toBeNull();
    expect(buildAttachPayload("")).toBeNull();
  });

  it("buildDetachPayload — плоский target_group_id", () => {
    expect(buildDetachPayload("tgr-1")).toEqual({ target_group_id: "tgr-1" });
  });

  it("buildDetachPayload — null при пустом id", () => {
    expect(buildDetachPayload(undefined)).toBeNull();
    expect(buildDetachPayload("")).toBeNull();
  });

  it("attachedTargetGroupIds — снимок id из attached_target_groups", () => {
    expect(
      attachedTargetGroupIds({
        attached_target_groups: [{ target_group_id: "tgr-1" }, { target_group_id: "tgr-2" }],
      }),
    ).toEqual(["tgr-1", "tgr-2"]);
    expect(attachedTargetGroupIds({})).toEqual([]);
    expect(attachedTargetGroupIds(undefined)).toEqual([]);
  });
});
