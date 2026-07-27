// Mutations answer with an Operation, not with the resource. Nothing may be
// reported as successful until the operation is done AND carries no error.
//
// Three ways this has actually gone wrong here, all locked below:
//   1. `done: true` with an `error` set — the row is not there, the id in
//      `metadata` was pre-allocated before the async failure.
//   2. the poll of /operations/{id} itself failing — the caller sat on a
//      spinner forever because the query error was dropped on the floor.
//   3. a response with no operation id being taken for a synchronous success —
//      for a resource whose mutations are declared to return an Operation that
//      is a contract violation, not a completed mutation.

import type { Operation } from "@shared/api/types";
import { operationOutcome, operationWarnings, resolveMutationResponse } from "./operation-outcome";

const op = (o: Partial<Operation>): Operation => ({ id: "op-1", done: false, ...o });

describe("operationOutcome", () => {
  it("is idle with no operation in flight", () => {
    expect(operationOutcome({ opId: null, op: undefined, fetchError: null })).toEqual({ kind: "idle" });
  });

  it("stays pending while the operation has not been read yet or is not done", () => {
    expect(operationOutcome({ opId: "op-1", op: undefined, fetchError: null })).toEqual({ kind: "pending" });
    expect(operationOutcome({ opId: "op-1", op: op({ done: false }), fetchError: null })).toEqual({ kind: "pending" });
  });

  it("succeeds only on done without an error", () => {
    expect(operationOutcome({ opId: "op-1", op: op({ done: true }), fetchError: null })).toEqual({ kind: "succeeded" });
  });

  it("fails on done+error and surfaces the operation's own message", () => {
    const out = operationOutcome({
      opId: "op-1",
      op: op({ done: true, error: { code: 9, message: "region ru-central1 has zones" } }),
      fetchError: null,
    });
    expect(out.kind).toBe("failed");
    expect(out.kind === "failed" && out.message).toContain("region ru-central1 has zones");
  });

  it("does not report success for an errored operation that carries no message", () => {
    const out = operationOutcome({
      opId: "op-1",
      op: op({ done: true, error: { code: 13, message: "" } }),
      fetchError: null,
    });
    expect(out.kind).toBe("failed");
    expect(out.kind === "failed" && out.message.length).toBeGreaterThan(0);
  });

  it("fails — never hangs — when the operation poll itself cannot be read", () => {
    const out = operationOutcome({ opId: "op-1", op: undefined, fetchError: new Error("Failed to fetch") });
    expect(out.kind).toBe("failed");
    expect(out.kind === "failed" && out.message).toContain("Failed to fetch");
  });

  it("prefers the operation's own error over a later poll failure", () => {
    const out = operationOutcome({
      opId: "op-1",
      op: op({ done: true, error: { code: 3, message: "invalid country code" } }),
      fetchError: new Error("Failed to fetch"),
    });
    expect(out.kind === "failed" && out.message).toContain("invalid country code");
  });
});

describe("resolveMutationResponse", () => {
  it("picks up a top-level Operation id", () => {
    expect(resolveMutationResponse({ id: "op-7", done: true }, true)).toEqual({ kind: "operation", opId: "op-7" });
  });

  it("picks up a wrapped Operation id", () => {
    expect(resolveMutationResponse({ operation: { id: "op-8", done: true } }, true)).toEqual({
      kind: "operation",
      opId: "op-8",
    });
  });

  it("refuses to call a missing operation id a success when one was promised", () => {
    // The internal mux omits default values, so an Operation with done=false
    // arrives without a `done` key. Guessing "synchronous success" there reports
    // a mutation as complete that has not even started.
    const out = resolveMutationResponse({ id: "reg-1", name: "ru-central1" }, true);
    expect(out.kind).toBe("violation");
    expect(out.kind === "violation" && out.message.length).toBeGreaterThan(0);
  });

  it("allows a synchronous resource answer only where the resource declares it", () => {
    expect(resolveMutationResponse({ id: "apl-1", name: "pool" }, false)).toEqual({ kind: "sync" });
  });

  it("treats an empty answer as synchronous only for a resource that declares it", () => {
    expect(resolveMutationResponse(null, false)).toEqual({ kind: "sync" });
    expect(resolveMutationResponse(null, true).kind).toBe("violation");
  });
});

describe("operationWarnings", () => {
  it("surfaces the geo loud-no-op channel from operation metadata", () => {
    // A region/zone created with status DOWN is created CLOSED to placement.
    // Swallowing that warning would leave the operator believing the catalog
    // entry is usable.
    const warnings = operationWarnings(
      op({
        done: true,
        metadata: {
          "@type": "type.googleapis.com/kacho.cloud.geo.v1.CreateRegionMetadata",
          region_id: "ru-central1",
          warnings: ["region ru-central1 is created CLOSED to placement (status DOWN)"],
        },
      }),
    );
    expect(warnings).toEqual(["region ru-central1 is created CLOSED to placement (status DOWN)"]);
  });

  it("is empty when there is no warning, and ignores a malformed channel", () => {
    expect(operationWarnings(op({ done: true }))).toEqual([]);
    expect(operationWarnings(op({ done: true, metadata: { "@type": "x", warnings: "oops" } }))).toEqual([]);
    expect(operationWarnings(undefined)).toEqual([]);
  });
});
