// Deciding whether a mutation actually succeeded.
//
// Mutations answer with an Operation, not with the resource. `done` alone means
// the operation finished — not that it worked: an Operation can be done and
// carry an error, and its metadata carries a resource id that was allocated
// before the failure. Reading the id without reading the error is how a phantom
// resource ends up on screen.
//
// Two more ways this goes wrong, both handled here:
//
//   * the poll of /operations/{id} fails. The operation is neither done nor
//     failed — we simply do not know. Dropping that query error leaves a
//     spinner running forever; it has to surface as a failure to read.
//   * the mutation answers without an operation id. For a resource whose
//     mutations are declared to return an Operation that is a contract
//     violation, not a synchronous success. It matters here because the
//     internal REST listener omits default values, so an Operation with
//     done=false arrives with no `done` key at all.

import type { Operation } from "@shared/api/types";

export type OperationOutcome =
  { kind: "idle" } | { kind: "pending" } | { kind: "succeeded" } | { kind: "failed"; message: string };

const UNKNOWN_OPERATION_ERROR = "операция завершилась с ошибкой";

export function operationOutcome(input: {
  opId: string | null;
  op: Operation | undefined;
  fetchError: unknown;
}): OperationOutcome {
  const { opId, op, fetchError } = input;
  if (!opId) return { kind: "idle" };

  // The operation's own verdict wins over a transport hiccup while polling it.
  if (op?.done && op.error) {
    return { kind: "failed", message: op.error.message || UNKNOWN_OPERATION_ERROR };
  }
  if (fetchError) {
    const detail = fetchError instanceof Error ? fetchError.message : String(fetchError);
    return { kind: "failed", message: `не удалось прочитать статус операции: ${detail}` };
  }
  if (!op || !op.done) return { kind: "pending" };
  return { kind: "succeeded" };
}

export type MutationResponseResolution =
  { kind: "operation"; opId: string } | { kind: "sync" } | { kind: "violation"; message: string };

/**
 * Ответ на мутацию как он есть — форму мы как раз и определяем. Сузить тип до
 * Operation значило бы предположить то, что предстоит выяснить: приходит и
 * ресурс целиком (синхронный admin-RPC), и ответ, в котором операции нет вовсе.
 */
type MutationResponse = unknown;

/**
 * Work out what a mutation response was: an Operation to poll, a synchronous
 * resource answer, or neither.
 *
 * `expectOperation` comes from the resource spec. When it is set, "neither" is
 * reported as a violation instead of being waved through as success — a missing
 * operation id then means the response was not what the contract promised, and
 * we have no evidence the mutation ran.
 */
export function resolveMutationResponse(resp: MutationResponse, expectOperation: boolean): MutationResponseResolution {
  const opId = operationIdOf(resp);
  if (opId) return { kind: "operation", opId };
  if (expectOperation) {
    return {
      kind: "violation",
      message: "сервер не вернул операцию — подтвердить выполнение невозможно",
    };
  }
  return { kind: "sync" };
}

/**
 * The Operation id inside a mutation response, or null.
 *
 * grpc-gateway returns the Operation as top-level JSON; the wrapped shape is
 * kept for the older call sites that still type it that way.
 */
export function operationIdOf(resp: MutationResponse): string | null {
  if (!resp || typeof resp !== "object") return null;
  const rec = resp as Record<string, unknown>;
  if (typeof rec.id === "string" && rec.id !== "" && typeof rec.done === "boolean") {
    return rec.id;
  }
  const wrapped = rec.operation as Operation | undefined;
  if (wrapped && typeof wrapped.id === "string" && wrapped.id !== "") return wrapped.id;
  return null;
}

/**
 * The loud-no-op channel geo puts on Create metadata: a region or zone created
 * with status DOWN is created CLOSED to placement. The operation succeeds, so
 * without surfacing this the operator walks away believing the catalog entry is
 * usable.
 */
export function operationWarnings(op: Operation | undefined): string[] {
  const raw = op?.metadata?.warnings;
  if (!Array.isArray(raw)) return [];
  return raw.filter((w): w is string => typeof w === "string" && w !== "");
}
