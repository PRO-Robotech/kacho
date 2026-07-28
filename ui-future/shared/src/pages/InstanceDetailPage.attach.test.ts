// Contract lock for the instance disk attach/detach bodies.
//
// Ground truth: proto/kacho/cloud/compute/v1/instance_service.proto.
//   AttachInstanceDiskRequest.attached_disk_spec : AttachedDiskSpec, and that
//     message's `disk` oneof has exactly two arms — `disk_spec` and `volume_id`
//     ("ID of the storage Volume (prefix \"vol\")"). There is no `disk_id`.
//   DetachInstanceDiskRequest.disk oneof: `volume_id` XOR `device_name`.
// Both oneofs carry `option (exactly_one) = true`, so naming a field the message
// does not have leaves NO arm set — and the edge, parsing with DiscardUnknown,
// drops the name without a word rather than complaining about it.
//
// The picker must offer storage Volumes for the same reason: a well-formed id of
// the retired compute-disk duplicate is not a `vol-` id, so the shape would be
// right and the value wrong.

import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { REGISTRY } from "@shared/lib/resource-registry";

const source = readFileSync(
  path.join(path.dirname(fileURLToPath(import.meta.url)), "InstanceDetailPage.tsx"),
  "utf8",
);

describe("instance disk attach/detach", () => {
  it("names the oneof arm the request message declares", () => {
    // `disk_id` is not a field of AttachedDiskSpec, DetachInstanceDiskRequest or
    // AttachedDisk — the whole family speaks `volume_id`. The only occurrence left
    // in the file is the comment saying so.
    expect(source).toContain("attached_disk_spec: { volume_id:");
    expect(source).toContain("{ volume_id: detachVolumeId }");
    const codeLines = source.split("\n").filter((l) => !l.trim().startsWith("//"));
    expect(codeLines.join("\n")).not.toMatch(/\bdisk_id\b/);
  });

  it("offers storage Volumes, whose ids the field is specified in terms of", () => {
    expect(source).toContain('refResource="volumes"');
    expect(source).not.toContain('refResource="compute-disks"');
  });

  it("resolves that ref target to the storage Volume list", () => {
    expect(REGISTRY["volumes"]?.apiPath).toBe("/storage/v1/volumes");
    expect(REGISTRY["volumes"]?.scope).toBe("project");
  });
});
