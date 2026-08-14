// Form → wire helpers for resource Create (POST) and Edit (PATCH).
//
// Consumed by the SPEC-DRIVEN forms: ResourceCreatePage / ResourceEditPage /
// InlineResourceCreateForm / InlineResourceEditForm (and the vendored copies of the
// same in the compute/nlb/registry/storage remotes).
//
// NOT consumed by the four bespoke edit forms — InlineSubnetEditForm,
// InlineSecurityGroupEditForm, InlineNetworkInterfaceEditForm,
// InlineAddressPoolEditForm. Each keeps its own local state, diffs it by hand and
// sends its whole (small, contract-declared) field set alongside the mask. That is
// over-sending, not unknown-field sending: every key they emit is a real field of
// the Update message, and a masked PATCH ignores the rest.
//
// Why the request body is BUILT rather than spread: the edge parses request bodies
// with protojson `DiscardUnknown`, so a key that is not a field of the request
// message is dropped without a word and the caller still gets 200 for something the
// server never saw. Two sources of such keys live on this path:
//   - the GET projection an edit form is hydrated from (id / created_at / status /
//     output-only mirrors) — none of which any Update* message carries;
//   - form-only discriminators (`_placement`, `_address_kind`, `_boot_source`,
//     `_ext_mode`, …), which api/client.ts camel-cases into `Placement`,
//     `AddressKind`, `BootSource` on the way out.
// So an Update body carries exactly the masked fields, and every body is stripped
// of form-only keys first.

import { getByPath, setByPath } from "@shared/lib/path";
import type { FormField } from "@shared/lib/form-schema";

/** Keys whose CHILDREN are tenant data, not field names (parity with lib/case.ts). */
const OPAQUE_FIELDS = new Set(["labels", "annotations", "match_labels", "matchLabels"]);

export function computeUpdateMask(
  original: Record<string, unknown>,
  current: Record<string, unknown>,
  fields: FormField[],
): string[] {
  const out: string[] = [];
  for (const f of fields) {
    if (f.hidden) continue;
    if (f.immutable) continue;
    // editHidden — поле в edit-форме не рендерится, его не должны включать
    // в update_mask (например, sg-rules управляются через спец-RPC, и backend
    // отвергает `rules` в Update mask с reason="unknown field").
    if (f.editHidden) continue;
    // createOnly — поле задаётся только при Create (нет Update-семантики на
    // бэкенде), напр. instances.assign_external_address. В edit-форме оно
    // скрыто, но в update_mask попадать НЕ должно — иначе backend отвергает
    // `unknown field in update_mask` (KAC-239).
    if (f.createOnly) continue;
    if (f.name.startsWith("_")) continue;
    const o = getByPath(original, f.name);
    const c = getByPath(current, f.name);
    if (JSON.stringify(o) !== JSON.stringify(c)) out.push(f.name);
  }
  return out;
}

export function snakeToCamelPath(p: string): string {
  return p.replace(/_([a-z])/g, (_, c: string) => c.toUpperCase());
}

/**
 * Drop form-only keys (leading `_`) at every depth.
 *
 * These exist to drive a widget — which branch of a proto oneof the form is
 * showing, the cascader path, the "use an existing NIC" toggle — and are never
 * fields of a request message. Keys of `labels` / `annotations` are tenant data and
 * are left exactly as the tenant wrote them: the same carve-out lib/case.ts makes.
 */
export function stripFormOnlyKeys<T>(value: T): T {
  return strip(value, false) as T;
}

function strip(value: unknown, opaque: boolean): unknown {
  if (Array.isArray(value)) return value.map((it) => strip(it, opaque));
  if (value && typeof value === "object") {
    const out: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(value as Record<string, unknown>)) {
      if (!opaque && k.startsWith("_")) continue;
      out[k] = strip(v, opaque || OPAQUE_FIELDS.has(k));
    }
    return out;
  }
  return value;
}

/**
 * Body of an Update (PATCH): exactly the fields named by `mask`, plus the mask.
 *
 * An Update* request message carries `<resource>_id` (bound from the URL path),
 * `update_mask`, and the mutable fields — nothing else; and a masked PATCH applies
 * only what the mask names. Sending the rest of the hydrated GET projection adds
 * neither meaning nor safety: it is discarded at the edge in silence.
 *
 * The mask is rendered in the camelCase form protojson accepts — its FieldMask
 * decoder rejects any path containing `_` outright, so a snake_case path is a 400,
 * not a lenient spelling.
 *
 * Returns null for an empty mask: nothing changed, so there is no request to send.
 */
export function buildUpdateBody(current: Record<string, unknown>, mask: string[]): Record<string, unknown> | null {
  if (mask.length === 0) return null;
  let picked: Record<string, unknown> = {};
  for (const path of mask) {
    picked = setByPath(picked, path, getByPath(current, path));
  }
  // Strip over the ASSEMBLED body, not over each value as it is picked: the
  // labels/annotations carve-out is keyed by the field's own name, and a value
  // handed over detached from its key has lost it — a masked `labels` would then
  // have a tenant key dropped that the same map keeps when the resource is
  // created.
  const body = stripFormOnlyKeys(picked);
  body.update_mask = mask.map(snakeToCamelPath).join(",");
  return body;
}

/**
 * Body of a Create (POST): the sanitized form object with form-only keys removed.
 *
 * `spec.sanitize` shapes the domain payload (oneof branch selection, unit
 * conversion); this is the transport-level guarantee that nothing which existed
 * only for the widget reaches the wire — including keys nested inside array items,
 * which a per-resource sanitize does not necessarily walk.
 */
export function buildCreateBody(parsed: Record<string, unknown>): Record<string, unknown> {
  return stripFormOnlyKeys(parsed);
}
