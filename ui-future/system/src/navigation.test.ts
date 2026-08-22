import { DASHBOARD_NAVIGATION } from "./navigation";

describe("System navigation", () => {
  it("exposes the System / Administration section", () => {
    const system = DASHBOARD_NAVIGATION.find((s) => s.key === "system");

    expect(system).toBeDefined();
    expect(system?.segment).toBe("system");
    expect(system?.landingPath).toBe("/system/regions");
    // Перечень утверждается ЦЕЛИКОМ и по порядку — не «содержит», а «равен»:
    // порядок пунктов виден пользователю в колонке раздела, и молча
    // переставленный пункт не был бы находкой при слабом утверждении.
    //
    // «Пределы» добавлены вместе со своим предметом: страница существовала и
    // работала, но пункта на неё не было НИ ОДНОГО — попасть можно было только
    // набрав адрес руками. Это зона рута: величины всех трёх уровней и их
    // правка.
    expect(system?.items.map((item) => item.path)).toEqual([
      "/system/regions",
      "/system/zones",
      "/system/address-pools",
      "/system/cluster/admins",
      "/system/limits",
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
