// What a list page shows is a decision with five outcomes, and the wrong one is
// a lie: an authorization failure rendered as "nothing here yet, create one" is
// the worst of them. Keeping the decision in one pure function makes it possible
// to state the rules rather than infer them from JSX.

import { ApiError } from "@shared/api/client";
import { listViewState, loadedCountLabel } from "./list-view-state";

describe("listViewState", () => {
  it("shows the loader only while the first page is still on the wire", () => {
    expect(listViewState({ isLoading: true, error: null, rowCount: 0, filtered: false, canCreate: true })).toBe(
      "loading",
    );
    // A background poll over rows we already have must not blank the table.
    expect(listViewState({ isLoading: true, error: null, rowCount: 3, filtered: false, canCreate: true })).toBe("rows");
  });

  it("shows the failure over everything else — an error is not an empty list", () => {
    const denied = new ApiError(403, "PERMISSION_DENIED", null, "no path");
    expect(listViewState({ isLoading: false, error: denied, rowCount: 0, filtered: false, canCreate: true })).toBe(
      "error",
    );
    // Even mid-poll, and even when a previous page is still in the cache.
    expect(listViewState({ isLoading: true, error: denied, rowCount: 5, filtered: false, canCreate: true })).toBe(
      "error",
    );
  });

  it("invites creation only for a genuinely empty, unfiltered, creatable list", () => {
    expect(listViewState({ isLoading: false, error: null, rowCount: 0, filtered: false, canCreate: true })).toBe(
      "welcome",
    );
  });

  it("does not invite creation on a read-only catalog", () => {
    expect(listViewState({ isLoading: false, error: null, rowCount: 0, filtered: false, canCreate: false })).toBe(
      "empty",
    );
  });

  it("does not claim the list is empty when a filter is what emptied it", () => {
    expect(listViewState({ isLoading: false, error: null, rowCount: 0, filtered: true, canCreate: true })).toBe(
      "no-matches",
    );
  });

  it("shows rows once there are any", () => {
    expect(listViewState({ isLoading: false, error: null, rowCount: 2, filtered: true, canCreate: true })).toBe("rows");
  });
});

describe("loadedCountLabel", () => {
  it("states a plain count when the whole list is loaded", () => {
    expect(loadedCountLabel(12, false)).toBe("12");
  });

  it("marks the count as partial while pages remain — there is no total to show", () => {
    // Cursor pagination gives no total; printing a bare "50" next to a list that
    // continues past the cursor would read as the size of the list.
    expect(loadedCountLabel(50, true)).toBe("50+");
  });
});
