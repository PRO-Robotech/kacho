import { loginUrl } from "./auth";

describe("auth utils", () => {
  it("builds login URL with current return_to", () => {
    window.history.pushState(null, "", "/projects/project-1/dashboard?tab=overview#top");

    expect(loginUrl()).toBe(
      "/.ory/kratos/public/self-service/login/browser?return_to=%2Fprojects%2Fproject-1%2Fdashboard%3Ftab%3Doverview%23top",
    );
  });
});
