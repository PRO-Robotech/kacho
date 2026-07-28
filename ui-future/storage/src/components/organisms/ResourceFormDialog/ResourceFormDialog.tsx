// ResourceFormDialog — Create/Update ресурса через REST API.
// Create: POST /v1/<plural>  → Operation
// Update: PATCH /v1/<plural>/{id} → Operation
// После получения Operation — поллит до done=true через OperationToastWatcher.

import { useEffect, useRef, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { Alert, Modal, Button, Segmented } from "antd";
import { PlusOutlined, EditOutlined, FormOutlined, CodeOutlined } from "@ant-design/icons";
import { JsonEditor } from "@/components/molecules/JsonEditor";
import { FormFieldRenderer } from "@/components/organisms/form/FormField";
import { extractOperationId } from "@/components/molecules/OperationDialog";
import { OperationToastWatcher } from "@/components/molecules/OperationToastWatcher";
import { ApiError, api } from "@/api/client";
import { applyFieldDefaults } from "@/lib/resource-registry";
import { getByPath, setByPath } from "@/lib/path";
import { useInvalidateResourceList } from "@/lib/use-operation";
import { toast } from "@/lib/toast";
import type { FormField } from "@/lib/form-schema";

type Mode = "create" | "edit";

interface Props {
  mode: Mode;
  title: string;
  description?: string;
  apiPath: string;
  resourceId: string;
  template: unknown;
  fields?: FormField[];
  projectId?: string | null;
  trigger?: React.ReactNode;
  controlledOpen?: { open: boolean; setOpen: (b: boolean) => void };
  onSuccess?: () => void;
  sanitize?: (obj: Record<string, unknown>) => Record<string, unknown>;
}

export function ResourceFormDialog({
  mode,
  title,
  description,
  apiPath,
  resourceId,
  template,
  fields,
  projectId,
  trigger,
  controlledOpen,
  onSuccess,
  sanitize,
}: Props) {
  const [internalOpen, setInternalOpen] = useState(false);
  const open = controlledOpen ? controlledOpen.open : internalOpen;
  const setOpen = controlledOpen ? controlledOpen.setOpen : setInternalOpen;

  const [view, setView] = useState<"form" | "json">(fields ? "form" : "json");
  const [obj, setObj] = useState<Record<string, unknown>>(() => normalize(template, fields));
  const [text, setText] = useState(() => JSON.stringify(template, null, 2));
  const [opId, setOpId] = useState<string | null>(null);

  const invalidate = useInvalidateResourceList();

  const snapshotRef = useRef({ template, fields });
  snapshotRef.current = { template, fields };

  const originalRef = useRef<Record<string, unknown> | null>(null);

  useEffect(() => {
    if (open) {
      const snap = snapshotRef.current;
      setObj(normalize(snap.template, snap.fields));
      setText(JSON.stringify(snap.template, null, 2));
      setOpId(null);
      setView(snap.fields ? "form" : "json");
      // Снимок для диффа — в той же форме, что даёт sanitize (см. ResourceEditPage).
      const snapObj =
        mode === "edit" && typeof snap.template === "object" && snap.template !== null
          ? (snap.template as Record<string, unknown>)
          : null;
      originalRef.current = snapObj && sanitize ? sanitize(snapObj) : snapObj;
    }
  }, [open, mode]);

  const mutation = useMutation({
    mutationFn: async (item: unknown) => {
      if (mode === "create") return api.create(apiPath, item);
      return api.update(apiPath, item);
    },
    onSuccess: (resp) => {
      const id = extractOperationId(resp);
      if (id) setOpId(id);
      else {
        setOpen(false);
        invalidate(resourceId, projectId ?? null);
        onSuccess?.();
      }
    },
    onError: (err) => {
      const m = err instanceof ApiError ? `${err.code}: ${err.message}` : (err as Error).message;
      toast.error(`${title}: ${m}`);
    },
  });

  const submit = () => {
    let parsed: unknown;
    if (view === "form") {
      parsed = obj;
    } else {
      try {
        parsed = JSON.parse(text);
      } catch (e) {
        return;
      }
    }
    if (sanitize && typeof parsed === "object" && parsed !== null) {
      parsed = sanitize(parsed as Record<string, unknown>);
    }

    if (mode === "edit" && originalRef.current && fields && typeof parsed === "object" && parsed !== null) {
      const mask = computeUpdateMask(originalRef.current, parsed as Record<string, unknown>, fields);
      // The body carries the masked fields and nothing else — an edit form is
      // hydrated from a GET projection whose id/created_at/status/output-only
      // mirrors are not fields of any Update* message.
      const payload = buildUpdateBody(parsed as Record<string, unknown>, mask);
      if (!payload) {
        setOpen(false);
        return;
      }
      parsed = payload;
    } else if (typeof parsed === "object" && parsed !== null) {
      parsed = buildCreateBody(parsed as Record<string, unknown>);
    }

    mutation.mutate(parsed);
  };

  const switchView = (next: "form" | "json") => {
    if (next === view) return;
    if (next === "json") setText(JSON.stringify(obj, null, 2));
    else {
      try {
        setObj(JSON.parse(text));
      } catch {
        // оставим текущий obj
      }
    }
    setView(next);
  };

  const opTitle =
    mode === "create" ? `Creating ${title.replace("Create ", "")}` : `Updating ${title.replace("Edit ", "")}`;

  return (
    <>
      {!controlledOpen && (
        <span
          onClick={(e) => {
            e.stopPropagation();
            setOpen(true);
          }}
        >
          {trigger ?? (
            <Button
              type={mode === "create" ? "primary" : "default"}
              size="small"
              icon={mode === "create" ? <PlusOutlined /> : <EditOutlined />}
            >
              {mode === "create" ? "Создать" : "Редактировать"}
            </Button>
          )}
        </span>
      )}

      <Modal
        open={open}
        onCancel={() => setOpen(false)}
        title={
          <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 12 }}>
            <span>{title}</span>
            {fields && (
              <Segmented
                size="small"
                value={view}
                onChange={(v) => switchView(v as "form" | "json")}
                options={[
                  { label: "Form", value: "form", icon: <FormOutlined /> },
                  { label: "JSON", value: "json", icon: <CodeOutlined /> },
                ]}
              />
            )}
          </div>
        }
        width={760}
        footer={[
          <Button key="cancel" onClick={() => setOpen(false)} disabled={mutation.isPending}>
            Отменить
          </Button>,
          <Button key="ok" type="primary" onClick={submit} loading={mutation.isPending || opId !== null}>
            {mode === "create" ? "Создать" : "Сохранить"}
          </Button>,
        ]}
      >
        {description && <Alert type="info" showIcon message={description} style={{ marginBottom: 12 }} />}

        <div style={{ maxHeight: "60vh", overflowY: "auto", paddingRight: 4 }}>
          {view === "form" && fields ? (
            <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
              {fields.map((f) => (
                <FormFieldRenderer
                  key={f.name}
                  field={f}
                  pathPrefix=""
                  value={obj}
                  onChange={setObj}
                  editMode={mode === "edit"}
                />
              ))}
            </div>
          ) : (
            <JsonEditor value={text} onChange={setText} rows={18} />
          )}
        </div>
      </Modal>

      <OperationToastWatcher
        opId={opId}
        title={opTitle}
        onDone={(success) => {
          setOpId(null);
          invalidate(resourceId, projectId ?? null);
          if (success) {
            setOpen(false);
            onSuccess?.();
          }
        }}
      />
    </>
  );
}

function normalize(tpl: unknown, fields: FormField[] | undefined): Record<string, unknown> {
  const obj =
    typeof tpl === "object" && tpl !== null ? { ...(tpl as Record<string, unknown>) } : ({} as Record<string, unknown>);
  return applyFieldDefaults(fields, obj);
}

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

/** Keys whose CHILDREN are tenant data, not field names (parity with lib/case.ts). */
const OPAQUE_FIELDS = new Set(["labels", "annotations", "match_labels", "matchLabels"]);

/**
 * Drop form-only keys (leading `_`) at every depth.
 *
 * These exist to drive a widget — which branch of a proto oneof the form is
 * showing, the cascader path, a mode toggle — and are never fields of a request
 * message; api/client.ts would camel-case `_address_kind` into `AddressKind` on the
 * way out, which the edge then discards in silence. Keys of `labels` /
 * `annotations` are tenant data and are left exactly as the tenant wrote them.
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
 * only what the mask names. Sending the rest of a hydrated GET projection adds
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
 * `sanitize` shapes the domain payload (oneof branch selection, unit conversion);
 * this is the transport-level guarantee that nothing which existed only for the
 * widget reaches the wire — including keys nested inside array items, which a
 * per-resource sanitize does not necessarily walk.
 */
export function buildCreateBody(parsed: Record<string, unknown>): Record<string, unknown> {
  return stripFormOnlyKeys(parsed);
}
