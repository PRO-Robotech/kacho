import { DASHBOARD_NAVIGATION } from "./navigation";

describe("System navigation", () => {
  it("exposes the System / Administration section", () => {
    const system = DASHBOARD_NAVIGATION.find((s) => s.key === "system");

    expect(system).toBeDefined();
    expect(system?.segment).toBe("system");
    expect(system?.landingPath).toBe("/system/regions");
    expect(system?.items.map((item) => item.path)).toEqual([
      "/system/regions",
      "/system/zones",
      "/system/address-pools",
      "/system/cluster/admins",
    ]);
  });

  it("exposes the Tokens & keys section", () => {
    const tokens = DASHBOARD_NAVIGATION.find((s) => s.key === "tokens");

    expect(tokens).toBeDefined();
    expect(tokens?.segment).toBe("tokens");
    expect(tokens?.landingPath).toBe("/system/tokens/service-account-keys");
    expect(tokens?.items.map((item) => item.path)).toEqual([
      "/system/tokens/service-account-keys",
      "/system/tokens/user-tokens",
    ]);
  });
});
