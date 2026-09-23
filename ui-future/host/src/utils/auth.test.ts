import { loginUrl } from "./auth";

describe("auth utils", () => {
  it("ведёт на экран входа консоли с текущим адресом возврата", () => {
    window.history.pushState(null, "", "/projects/project-1/dashboard?tab=overview#top");

    expect(loginUrl()).toBe("/login?returnTo=%2Fprojects%2Fproject-1%2Fdashboard%3Ftab%3Doverview%23top");
  });

  it("не называет чужого поставщика ни одной формой адреса", () => {
    window.history.pushState(null, "", "/dashboard");

    // Отрицание в паре с положительным выше: адрес собран, и он — наш.
    expect(loginUrl()).not.toMatch(/\/\.ory\/|\/oauth2|\/self-service\//);
    expect(loginUrl()).toMatch(/^\/login\?returnTo=/);
  });
});
