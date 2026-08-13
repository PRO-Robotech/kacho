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

  /** Записывает КАЖДЫЙ запрос: предмет части утверждений — сам запрос, а не дерево. */
  function recordNetworkSubtree(): { path: string; query?: Record<string, string> }[] {
    const calls: { path: string; query?: Record<string, string> }[] = [];
    list.mockImplementation((path: string, query?: Record<string, string>): Promise<Payload> => {
      calls.push({ path, query });
      if (path === "/vpc/v1/subnets")
        return Promise.resolve({ subnets: [{ id: "sn-1", name: "sub", project_id: "p1" }] });
      if (path === "/vpc/v1/routeTables") return Promise.resolve({ route_tables: [] });
      if (path === "/vpc/v1/securityGroups")
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
    return calls;
  }

  it("дети сети спрашиваются ПЛОСКИМИ списками, сужёнными по родителю на сервере", async () => {
    // Три под-перечисления сети (`/vpc/v1/networks/{id}/{subnets,route_tables,
    // security_groups}`) сняты с контракта как вторые пути к одному ответу.
    // Замена — тот же плоский список ресурса с `network_id` в выражении фильтра;
    // он стоит в белом списке каждого из трёх владельцев.
    const calls = recordNetworkSubtree();

    await loadDependents("networks", { id: "net-1", project_id: "p1" });

    const byNetwork = { pageSize: "1000", project_id: "p1", filter: 'network_id="net-1"' };
    expect(calls.filter((c) => c.path === "/vpc/v1/subnets").map((c) => c.query)).toEqual([byNetwork]);
    expect(calls.filter((c) => c.path === "/vpc/v1/routeTables").map((c) => c.query)).toEqual([byNetwork]);
    expect(calls.filter((c) => c.path === "/vpc/v1/securityGroups").map((c) => c.query)).toEqual([byNetwork]);
    // Снятого под-перечисления консоль не спрашивает ни в одной форме: такой
    // запрос доехал бы до края и вернул 404, а дерево связей просто оказалось бы
    // пустым — то есть диалог удаления сообщал бы «ничего не мешает».
    expect(calls.filter((c) => c.path.includes("/networks/"))).toEqual([]);
  });

  it("без проекта дети сети не спрашиваются вовсе — плоский список требует project_id", async () => {
    // `project_id` у плоского списка обязателен (у снятого под-перечисления его
    // не было: область бралась из сегмента пути). Без проекта спрашивать нечем,
    // и три отказа подряд не превратились бы в дерево; авторитетный запрет
    // остаётся за сервером (FK RESTRICT на самом удалении).
    const calls = recordNetworkSubtree();

    expect(await loadDependents("networks", { id: "net-1", project_id: null })).toEqual([]);
    expect(calls).toEqual([]);
  });

  it("builds a network subtree, narrowing string/array/nested backend fields", async () => {
    recordNetworkSubtree();

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
    const blocking = blockingNodes(tree)
      .map((n) => n.id)
      .sort();
    expect(blocking).toEqual(["addr-1", "ni-1", "sg-user", "sn-1"]);
  });

  it("reports the instance a NIC is attached to via used_by.referrer", async () => {
    get.mockResolvedValue({
      id: "ni-1",
      project_id: "p1",
      used_by: { referrer: { id: "inst-9" } },
    });

    const tree = await loadDependents("network-interfaces", { id: "ni-1", project_id: "p1" });

    expect(tree).toHaveLength(1);
    expect(tree[0].resourceId).toBe("compute-instances");
    expect(tree[0].id).toBe("inst-9");
    expect(tree[0].blocks).toBe(true);
  });

  it("returns no dependents for an unattached NIC", async () => {
    get.mockResolvedValue({ id: "ni-2", project_id: "p1" });
    const tree = await loadDependents("network-interfaces", { id: "ni-2", project_id: "p1" });
    expect(tree).toEqual([]);
  });
});
