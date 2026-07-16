import { render, screen, waitFor } from "@testing-library/react";
import type { Dispatch, SetStateAction } from "react";
import { jest } from "@jest/globals";
import { HostBreadcrumb } from ".";
import type { HostContext } from "../../../utils";

const jsonResponse = (body: unknown) => {
  return Promise.resolve({
    ok: true,
    text: () => Promise.resolve(JSON.stringify(body)),
    statusText: "OK",
  } as Response);
};

describe("HostBreadcrumb", () => {
  beforeEach(() => {
    jest.spyOn(global, "fetch").mockImplementation(() => jsonResponse({ accounts: [] }));
  });

  afterEach(() => {
    jest.restoreAllMocks();
  });

  it("starts with original unselected placeholders", async () => {
    const context: HostContext = { account: null, project: null };
    const onChange: Dispatch<SetStateAction<HostContext>> = jest.fn();
    render(<HostBreadcrumb context={context} onChange={onChange} />);

    expect(screen.getByRole("button", { name: /выберите аккаунт/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /проект/i })).toBeInTheDocument();

    await waitFor(() => {
      expect(global.fetch).toHaveBeenCalled();
    });
    const fetchMock = jest.mocked(global.fetch);
    const [url, init] = fetchMock.mock.calls[0] as [RequestInfo | URL, RequestInit];
    const headers = init.headers as Record<string, string>;

    expect(url).toBe("/iam/v1/accounts?pageSize=1000");
    expect(headers["X-Request-ID"]).toEqual(expect.any(String));
  });

  it("uses selected account and project names or ids", () => {
    const context: HostContext = {
      account: { id: "acc-1", name: "" },
      project: { id: "project-1", name: "", accountId: "acc-1" },
    };
    const onChange: Dispatch<SetStateAction<HostContext>> = jest.fn();
    render(<HostBreadcrumb context={context} onChange={onChange} />);

    expect(screen.getByRole("button", { name: "acc-1" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "project-1" })).toBeInTheDocument();
  });
});
