import { fetchAllPages } from "./use-resource-list";

// GWT-9 (load-all) — образы пагинируются на handler-слое; fetchAllPages обязан
// следовать next_page_token до пустого и аккумулировать ВСЕ страницы, иначе facet
// над одной страницей неполон (helm-образ со страницы 2 пропал бы).
describe("fetchAllPages (load-all pagination)", () => {
  it("аккумулирует несколько страниц, следуя next_page_token до пустого", async () => {
    const pages: Record<string, Record<string, unknown>> = {
      "": { repositories: [{ name: "a" }, { name: "b" }], next_page_token: "tok2" },
      tok2: { repositories: [{ name: "helm-on-page-2" }], next_page_token: "" },
    };
    const calls: Record<string, string>[] = [];
    const listFn = async (_path: string, q: Record<string, string>) => {
      calls.push(q);
      return pages[q.pageToken ?? ""];
    };

    const rows = await fetchAllPages<{ name: string }>("/registry/v1/registries/reg-x/repositories", "repositories", listFn);

    expect(rows.map((r) => r.name)).toEqual(["a", "b", "helm-on-page-2"]);
    // Первая страница — без pageToken; вторая — с pageToken=tok2.
    expect(calls[0].pageToken).toBeUndefined();
    expect(calls[1].pageToken).toBe("tok2");
    // pageSize запрашивается большим, чтобы минимизировать число round-trip'ов.
    expect(calls[0].pageSize).toBe("1000");
  });

  it("одна страница (next_page_token пуст) → один вызов", async () => {
    let n = 0;
    const listFn = async () => {
      n++;
      return { repositories: [{ name: "solo" }], next_page_token: "" };
    };
    const rows = await fetchAllPages("/p", "repositories", listFn);
    expect(rows).toHaveLength(1);
    expect(n).toBe(1);
  });

  it("отсутствующий payloadKey → пустой набор (грациозно)", async () => {
    const listFn = async () => ({ next_page_token: "" });
    const rows = await fetchAllPages("/p", "repositories", listFn);
    expect(rows).toEqual([]);
  });

  // Регрессия: apiPath с неразрешённым path-плейсхолдером (родитель ещё не известен)
  // НЕ должен уходить на backend литералом `{registryId}` — иначе InvalidArgument
  // «invalid registry id '{registryId}'». Guard возвращает пустой набор без fetch.
  it("неразрешённый {registryId} → пустой набор, БЕЗ вызова listFn (guard)", async () => {
    let n = 0;
    const listFn = async () => {
      n++;
      return { repositories: [{ name: "x" }], next_page_token: "" };
    };
    const rows = await fetchAllPages(
      "/registry/v1/registries/{registryId}/repositories",
      "repositories",
      listFn,
    );
    expect(rows).toEqual([]);
    expect(n).toBe(0);
  });
});
