// Cursor pagination, the way the List contract defines it.
//
// A List answers with one page plus an opaque next_page_token ordered by
// (created_at, id); there is no total and no page number. Two consequences the
// helpers below encode:
//
//   * everything past the first page is invisible until the cursor is followed,
//     so a list that renders only the first response is not showing the list;
//   * the first page keeps being polled while later pages are already loaded, so
//     a row can arrive in two pages at once and has to be de-duplicated by id —
//     otherwise it renders twice and collides on the row key.

export interface CursorPage {
  next_page_token?: string;
  [key: string]: unknown;
}

/**
 * All rows across the loaded pages, in fetch order, one entry per id. A later
 * page wins on content (it is the fresher read) but keeps the position of the
 * first appearance, so rows do not jump around under polling.
 *
 * Rows without an id are never merged — collapsing them would silently drop
 * data for any resource whose list rows are not id-keyed.
 */
export function mergeCursorPages<T = Record<string, unknown>>(
  pages: CursorPage[] | undefined,
  payloadKey: string,
): T[] {
  if (!pages || pages.length === 0) return [];
  const out: T[] = [];
  const indexById = new Map<string, number>();
  for (const page of pages) {
    const rows = page?.[payloadKey];
    if (!Array.isArray(rows)) continue;
    for (const row of rows as T[]) {
      const id = idOf(row);
      if (id === null) {
        out.push(row);
        continue;
      }
      const at = indexById.get(id);
      if (at === undefined) {
        indexById.set(id, out.length);
        out.push(row);
      } else {
        out[at] = row;
      }
    }
  }
  return out;
}

function idOf(row: unknown): string | null {
  if (!row || typeof row !== "object") return null;
  const id = (row as Record<string, unknown>).id;
  return typeof id === "string" && id !== "" ? id : null;
}

/** The cursor to ask for next, taken from the last page only. Empty means end. */
export function nextCursor(pages: CursorPage[] | undefined): string | undefined {
  if (!pages || pages.length === 0) return undefined;
  const token = pages[pages.length - 1]?.next_page_token;
  return token ? token : undefined;
}

export function hasMorePages(pages: CursorPage[] | undefined): boolean {
  return nextCursor(pages) !== undefined;
}
