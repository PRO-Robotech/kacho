// List responses are cursor-paginated: the server returns one page plus an
// opaque next_page_token, and there is no total. Reading only the first page
// silently hides everything past it, which is what the list pages did.

import { hasMorePages, mergeCursorPages, nextCursor } from "./cursor-pages";

const page = (rows: unknown[], token?: string) => ({ regions: rows, next_page_token: token });

describe("mergeCursorPages", () => {
  it("concatenates pages in the order they were fetched", () => {
    const rows = mergeCursorPages<{ id: string }>(
      [page([{ id: "a" }, { id: "b" }], "t1"), page([{ id: "c" }])],
      "regions",
    );
    expect(rows.map((r) => r.id)).toEqual(["a", "b", "c"]);
  });

  it("de-duplicates by id — pages are re-polled and rows shift between them", () => {
    // Page 1 is polled live while page 2 is already loaded; a row can appear in
    // both. Rendering it twice would also break the row key.
    const rows = mergeCursorPages<{ id: string }>(
      [page([{ id: "a" }, { id: "b" }], "t1"), page([{ id: "b" }, { id: "c" }])],
      "regions",
    );
    expect(rows.map((r) => r.id)).toEqual(["a", "b", "c"]);
  });

  it("keeps the freshest copy of a duplicated row at its first position", () => {
    const rows = mergeCursorPages<{ id: string; name: string }>(
      [page([{ id: "a", name: "old" }], "t1"), page([{ id: "a", name: "new" }])],
      "regions",
    );
    expect(rows).toEqual([{ id: "a", name: "new" }]);
  });

  it("keeps rows that have no id instead of collapsing them into one", () => {
    const rows = mergeCursorPages<Record<string, unknown>>([page([{ name: "x" }, { name: "y" }])], "regions");
    expect(rows).toHaveLength(2);
  });

  it("survives an absent payload key and an absent page set", () => {
    expect(mergeCursorPages([{ next_page_token: "t" }], "regions")).toEqual([]);
    expect(mergeCursorPages(undefined, "regions")).toEqual([]);
  });
});

describe("nextCursor / hasMorePages", () => {
  it("takes the cursor from the last page only", () => {
    expect(nextCursor([page([], "t1"), page([], "t2")])).toBe("t2");
  });

  it("treats an empty token as the end of the list", () => {
    expect(nextCursor([page([], "t1"), page([], "")])).toBeUndefined();
    expect(hasMorePages([page([], "t1"), page([], "")])).toBe(false);
    expect(hasMorePages([page([], "t1")])).toBe(true);
    expect(hasMorePages(undefined)).toBe(false);
  });
});
