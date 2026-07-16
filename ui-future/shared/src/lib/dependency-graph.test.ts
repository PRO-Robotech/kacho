import { jest } from "@jest/globals";

// Mock the REST client so we can feed backend-shaped (untyped JSON) records and
// assert the dependency tree is built by explicitly narrowing every field.
const list = jest.fn<(path: string, query?: Record<string, string>) => Promise<unknown>>();
const get = jest.fn<(path: string) => Promise<unknown>>();
jest.unstable_mockModule("@shared/api/client", () => ({
  api: { list, get },
}));
// resource-registry pulls the full (antd/monaco-heavy) registry; the graph only
// needs REGISTRY[id]?.route for URL segments, so stub it lightly.
jest.unstable_mockModule("@shared/lib/resource-registry", () => ({
  REGISTRY: {},
}));

const { loadDependents, blockingNodes, hasDependencyResolver } = await import("./dependency-graph");

type Payload = Record<string, unknown[]>;

beforeEach(() => {
  list.mockReset();
  get.mockReset();
});

describe("dependency-graph", () => {
  it("advertises resolvers only for the four supported resource ids", () => {
    expect(hasDependencyResolver("networks")).toBe(true);
    expect(hasDependencyResolver("subnets")).toBe(true);
    expect(hasDependencyResolver("addresses")).toBe(true);
    expect(hasDependencyResolver("network-interfaces")).toBe(true);
    expect(hasDependencyResolver("route-tables")).toBe(false);
  });

  it("builds a network subtree, narrowing string/array/nested backend fields", async () => {
    list.mockImplementation((path: string): Promise<Payload> => {
      if (path.endsWith("/subnets") && path.includes("/networks/"))
        return Promise.resolve({ subnets: [{ id: "sn-1", name: "sub", project_id: "p1" }] });
      if (path.endsWith("/route_tables")) return Promise.resolve({ route_tables: [] });
      if (path.endsWith("/security_groups"))
        return Promise.resolve({
          security_groups: [
            { id: "sg-default", name: "def", default_for_network: true },
            { id: "sg-user", name: "user", default_for_network: false },
          ],
        });
      if (path === "/vpc/v1/addresses")
        return Promise.resolve({
          addresses: [{ id: "addr-1", name: "a1", internal_ipv4_address: { subnet_id: "sn-1" } }],
        });
      if (path === "/vpc/v1/networkInterfaces")
        return Promise.resolve({ network_interfaces: [{ id: "ni-1", name: "n1", subnet_id: "sn-1" }] });
      return Promise.resolve({});
    });

    const tree = await loadDependents("networks", { id: "net-1", project_id: "p1" });

    const subnet = tree.find((n) => n.resourceId === "subnets");
    expect(subnet).toBeDefined();
    expect(subnet?.id).toBe("sn-1");
    // subnet children: the internal address + the NIC on that subnet.
    expect(subnet?.children.map((c) => c.resourceId).sort()).toEqual(["addresses", "network-interfaces"]);

    // Default SG does not block; user SG does.
    const defaultSg = tree.find((n) => n.id === "sg-default");
    const userSg = tree.find((n) => n.id === "sg-user");
    expect(defaultSg?.blocks).toBe(false);
    expect(userSg?.blocks).toBe(true);

    // blockingNodes walks recursively and collects only blocking nodes.
    const blocking = blockingNodes(tree).map((n) => n.id).sort();
    expect(blocking).toEqual(["addr-1", "ni-1", "sg-user", "sn-1"]);
  });

  it("reports the instance a NIC is attached to via used_by.referrer", async () => {
    get.mockResolvedValue({
      id: "ni-1",
      project_id: "p1",
      used_by: { referrer: { id: "inst-9" } },
    } as never);

    const tree = await loadDependents("network-interfaces", { id: "ni-1", project_id: "p1" });

    expect(tree).toHaveLength(1);
    expect(tree[0].resourceId).toBe("compute-instances");
    expect(tree[0].id).toBe("inst-9");
    expect(tree[0].blocks).toBe(true);
  });

  it("returns no dependents for an unattached NIC", async () => {
    get.mockResolvedValue({ id: "ni-2", project_id: "p1" } as never);
    const tree = await loadDependents("network-interfaces", { id: "ni-2", project_id: "p1" });
    expect(tree).toEqual([]);
  });
});
