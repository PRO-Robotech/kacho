import { DASHBOARD_NAVIGATION } from "./navigation";

describe("IAM navigation", () => {
  it("exports the public IAM sections", () => {
    const section = DASHBOARD_NAVIGATION[0];

    expect(section.segment).toBe("iam");
    expect(section.landingPath).toBe("/iam/accounts");
    expect(section.items.map((item) => item.path)).toEqual([
      "/iam/accounts",
      "/iam/projects",
      "/iam/users",
      "/iam/service-accounts",
      "/iam/groups",
      "/iam/roles",
      "/iam/access-bindings",
      "/iam/operations",
    ]);
  });
});
