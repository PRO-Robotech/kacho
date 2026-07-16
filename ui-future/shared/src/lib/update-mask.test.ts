import { computeUpdateMask, snakeToCamelPath } from "./update-mask";
import type { FormField } from "./form-schema";

const str = (name: string, extra: Partial<FormField> = {}): FormField =>
  ({ name, label: name, type: "string", ...extra }) as FormField;

describe("computeUpdateMask", () => {
  it("includes only mutable fields whose value changed", () => {
    const fields = [str("name"), str("description")];
    const mask = computeUpdateMask({ name: "a", description: "old" }, { name: "a", description: "new" }, fields);
    expect(mask).toEqual(["description"]);
  });

  it("returns an empty mask when nothing changed", () => {
    const fields = [str("name"), str("description")];
    expect(computeUpdateMask({ name: "a" }, { name: "a" }, fields)).toEqual([]);
  });

  it("skips hidden / immutable / editHidden / createOnly and _-prefixed fields", () => {
    const fields = [
      str("hidden_f", { hidden: true }),
      str("immutable_f", { immutable: true }),
      str("edit_hidden_f", { editHidden: true }),
      str("create_only_f", { createOnly: true }),
      str("_discriminator"),
      str("mutable_f"),
    ];
    const original = {
      hidden_f: "a",
      immutable_f: "a",
      edit_hidden_f: "a",
      create_only_f: "a",
      _discriminator: "a",
      mutable_f: "a",
    };
    const current = {
      hidden_f: "b",
      immutable_f: "b",
      edit_hidden_f: "b",
      create_only_f: "b",
      _discriminator: "b",
      mutable_f: "b",
    };
    expect(computeUpdateMask(original, current, fields)).toEqual(["mutable_f"]);
  });

  it("resolves dotted paths via getByPath", () => {
    const fields = [str("labels.env")];
    const mask = computeUpdateMask({ labels: { env: "dev" } }, { labels: { env: "prod" } }, fields);
    expect(mask).toEqual(["labels.env"]);
  });

  it("treats deep structural changes as a diff (JSON compare)", () => {
    const fields = [str("routes")];
    const mask = computeUpdateMask({ routes: [{ dst: "0.0.0.0/0" }] }, { routes: [{ dst: "10.0.0.0/8" }] }, fields);
    expect(mask).toEqual(["routes"]);
  });
});

describe("snakeToCamelPath", () => {
  it("converts a snake_case path segment to camelCase", () => {
    expect(snakeToCamelPath("v4_cidr_blocks")).toBe("v4CidrBlocks");
  });

  it("leaves a path without underscores unchanged", () => {
    expect(snakeToCamelPath("name")).toBe("name");
  });

  it("converts each dotted segment independently", () => {
    expect(snakeToCamelPath("static_routes.next_hop")).toBe("staticRoutes.nextHop");
  });
});
