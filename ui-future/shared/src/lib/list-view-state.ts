// Which of the five things a list page can show.
//
// Split out of the JSX because the ordering carries meaning: a request that
// failed must never fall through to the empty-list invitation. "Нет ресурсов,
// создайте первый" on top of a 403 tells the operator the project is empty when
// really they were refused, and on top of a 404 it does the same on top of an
// answer that could equally mean the resource is merely hidden from them.

export type ListViewState = "loading" | "error" | "welcome" | "empty" | "no-matches" | "rows";

export function listViewState(input: {
  isLoading: boolean;
  error: unknown;
  rowCount: number;
  /** A user-supplied filter is narrowing the list right now. */
  filtered: boolean;
  canCreate: boolean;
}): ListViewState {
  const { isLoading, error, rowCount, filtered, canCreate } = input;
  // A failure outranks everything, including rows left in the cache from an
  // earlier poll: showing stale rows under a live error implies they are current.
  if (error) return "error";
  if (rowCount > 0) return "rows";
  // Only the very first load blanks the page; a background poll keeps what we have.
  if (isLoading) return "loading";
  if (filtered) return "no-matches";
  return canCreate ? "welcome" : "empty";
}

/**
 * The count next to a list title. Cursor pagination gives no total, so once a
 * cursor remains the number is what has been loaded, not what exists — say so
 * rather than print a figure that reads as the size of the list.
 */
export function loadedCountLabel(loaded: number, hasMore: boolean): string {
  return hasMore ? `${loaded}+` : String(loaded);
}
