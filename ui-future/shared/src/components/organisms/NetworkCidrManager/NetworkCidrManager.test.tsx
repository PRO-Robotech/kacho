import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const expectedExports = ["NetworkCidrManager"] as const;

const source = readFileSync(path.join(path.dirname(fileURLToPath(import.meta.url)), "NetworkCidrManager.tsx"), "utf8");

describe("NetworkCidrManager", () => {
  it("declares its public component exports", () => {
    for (const exportName of expectedExports) {
      expect(source).toContain(exportName);
    }
  });

  it("mutates the supernet via verb RPCs with the VPC-1 field names", () => {
    // Supernet is immutable through Update — only :add/:remove-cidr-blocks, and
    // the family is keyed by ipv4/ipv6_cidr_blocks (v4_/v6_ retired).
    expect(source).toContain(":add-cidr-blocks");
    expect(source).toContain(":remove-cidr-blocks");
    expect(source).toContain("ipv4_cidr_blocks");
    expect(source).toContain("ipv6_cidr_blocks");
  });
});
