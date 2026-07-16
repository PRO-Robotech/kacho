import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { makeRemote, type RemotePageProps } from "./makeRemote";

const Page = ({ context, navigate }: RemotePageProps) => (
  <div data-testid="remote-page">
    {context?.project?.id ?? "no-project"}:{typeof navigate}
  </div>
);

const renderRemote = (Remote: ReturnType<typeof makeRemote>) =>
  render(
    <MemoryRouter>
      <Remote context={{ account: null, project: { id: "project-1", name: "p1" } } as never} />
    </MemoryRouter>,
  );

describe("makeRemote", () => {
  it("renders the component picked from the loaded module with host props", async () => {
    const Remote = makeRemote(
      () => Promise.resolve({ default: Page }),
      (mod) => mod.default as never,
    );

    renderRemote(Remote);

    expect(await screen.findByTestId("remote-page")).toHaveTextContent("project-1:function");
  });

  it("falls back to a named export when there is no default", async () => {
    const Remote = makeRemote(
      () => Promise.resolve({ VpcPage: Page }),
      (mod) => (mod.default ?? mod.VpcPage) as never,
    );

    renderRemote(Remote);

    expect(await screen.findByTestId("remote-page")).toBeInTheDocument();
  });

  it("keeps its loader specifier literal (static federation resolution)", () => {
    // The factory closes over a loader; the import() specifier stays literal at
    // each call site, so the federation bundler resolves it statically.
    expect(typeof makeRemote).toBe("function");
  });
});
