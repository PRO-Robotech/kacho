import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { jest } from "@jest/globals";
import { ReachabilityPage } from ".";

const jsonResponse = (body: unknown, status = 200) => {
  return Promise.resolve({
    ok: status >= 200 && status < 300,
    status,
    text: () => Promise.resolve(JSON.stringify(body)),
    statusText: "OK",
  } as Response);
};

describe("ReachabilityPage", () => {
  afterEach(() => {
    jest.restoreAllMocks();
  });

  it("runs all probes", async () => {
    const user = userEvent.setup();
    jest.spyOn(global, "fetch").mockImplementation(() => jsonResponse({ message: "ready" }));
    render(<ReachabilityPage />);

    await user.click(screen.getByRole("button", { name: "Probe all" }));

    await waitFor(() => {
      expect(global.fetch).toHaveBeenCalledTimes(6);
    });
    expect(await screen.findAllByText("ok 200")).toHaveLength(6);
  });
});
