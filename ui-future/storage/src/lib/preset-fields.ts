// Bounding a URL-derived preset to the form's own schema.
//
// A create form can be seeded from the URL (`?modal=<spec>-create&…`, or a nested
// route's :networkId/:subnetId). Those keys go into the form object and from there
// into the request body, exactly like a typed value — but their key set is chosen
// by whoever built the link, not by the resource. A stray query param would ride
// out as a request field the message does not declare, and the edge, which parses
// with protojson DiscardUnknown, would drop it without a word.
//
// So a preset may only name a field the form actually has. Anything else is a UI
// concern that happened to be in the query string.

import type { FormField } from "@/lib/form-schema";

export function presetFieldsForSpec(
  fields: FormField[] | undefined,
  presets: Record<string, unknown>,
): Record<string, unknown> {
  const known = new Set((fields ?? []).map((f) => f.name));
  const out: Record<string, unknown> = {};
  for (const [path, value] of Object.entries(presets)) {
    if (known.has(path)) out[path] = value;
  }
  return out;
}
