// update_mask helpers for resource Edit (PATCH) flows.
//
// Consumed by the inline edit forms (InlineResourceEditForm, InlineSubnetEditForm,
// InlineSecurityGroupEditForm, InlineNetworkInterfaceEditForm, InlineAddressPoolEditForm)
// and ResourceEditPage to compute the changed-field set and render it as a
// camelCase `update_mask` for the REST Update contract.

import { getByPath } from "@shared/lib/path";
import type { FormField } from "@shared/lib/form-schema";

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
    // бэкенде), напр. networks.create_default_security_group. В edit-форме оно
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
