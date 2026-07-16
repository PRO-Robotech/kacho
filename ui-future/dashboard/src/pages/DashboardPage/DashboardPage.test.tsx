import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { jest } from "@jest/globals";
import { DashboardPage } from ".";
import type { HostContext } from "../../utils";

const emptyContext: HostContext = {
  account: null,
  project: null,
};

const projectContext: HostContext = {
  account: { id: "account-1", name: "Account 1" },
  project: { id: "project-1", name: "Project 1", accountId: "account-1" },
};

function jsonResponse(body: unknown) {
  return Promise.resolve({
    ok: true,
    text: () => Promise.resolve(JSON.stringify(body)),
    statusText: "OK",
  } as Response);
}

describe("DashboardPage", () => {
  beforeEach(() => {
    jest.useFakeTimers();
    global.fetch = jest.fn<typeof fetch>();
    jest.spyOn(global, "fetch").mockImplementation((input) => {
      const path = input instanceof Request ? input.url : input.toString();
      if (path.startsWith("/vpc/v1/networks")) return jsonResponse({ networks: [{ id: "net-1" }] });
      if (path.startsWith("/vpc/v1/subnets")) return jsonResponse({ subnets: [] });
      if (path.startsWith("/vpc/v1/securityGroups")) return jsonResponse({ security_groups: [] });
      if (path.startsWith("/compute/v1/instances")) return jsonResponse({ instances: [{ id: "vm-1" }] });
      if (path.startsWith("/compute/v1/disks")) return jsonResponse({ disks: [] });
      if (path.startsWith("/compute/v1/images")) return jsonResponse({ images: [] });
      if (path.startsWith("/nlb/v1/networkLoadBalancers")) return jsonResponse({ network_load_balancers: [] });
      if (path.startsWith("/nlb/v1/listeners")) return jsonResponse({ listeners: [] });
      if (path.startsWith("/nlb/v1/targetGroups")) return jsonResponse({ target_groups: [] });
      if (path.startsWith("/iam/v1/accounts")) return jsonResponse({ accounts: [{ id: "account-1" }] });
      if (path.startsWith("/iam/v1/projects")) return jsonResponse({ projects: [{ id: "project-1" }] });
      if (path.startsWith("/iam/v1/roles")) return jsonResponse({ roles: [] });
      return jsonResponse({});
    });
  });

  afterEach(() => {
    jest.useRealTimers();
    jest.restoreAllMocks();
  });

  it("renders the same service tiles and disables project scoped modules without project context", () => {
    render(<DashboardPage context={emptyContext} />);

    expect(screen.getByRole("heading", { name: "Сервисы облака" })).toBeInTheDocument();
    expect(screen.getByTestId("dashboard-tile-vpc")).toHaveAttribute("data-disabled", "true");
    expect(screen.getByTestId("dashboard-tile-compute")).toHaveAttribute("data-disabled", "true");
    expect(screen.getByTestId("dashboard-tile-nlb")).toHaveAttribute("data-disabled", "true");
    expect(screen.getByTestId("dashboard-tile-iam")).toHaveAttribute("data-disabled", "false");
  });

  it("opens service landing routes and loads scoped counters when project is selected", async () => {
    const navigate = jest.fn((_: string) => undefined);
    render(<DashboardPage context={projectContext} navigate={navigate} />);

    fireEvent.click(screen.getByTestId("dashboard-tile-vpc"));

    expect(navigate).toHaveBeenCalledWith("/projects/project-1/vpc/networks");
    await waitFor(() => {
      const calls = (global.fetch as jest.MockedFunction<typeof fetch>).mock.calls;
      expect(calls.some(([url]) => url === "/vpc/v1/networks?pageSize=1000&project_id=project-1")).toBe(true);
    });
  });
});
