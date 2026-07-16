// ResourceCreatePage — full-page форма Create (не modal).

import { useEffect, useMemo, useRef, useState } from "react";
import { Link, useLocation, useNavigate, useParams, useSearchParams } from "react-router-dom";
import { useMutation } from "@tanstack/react-query";
import { Alert, Typography } from "antd";
import { ResourceFormBody } from "@/components/organisms/form/ResourceFormBody";
import { FORM_WIDTH } from "@/components/organisms/form/FormShell";
import { extractOperationId } from "@/components/molecules/OperationDialog";
import { useBreadcrumb, useHeaderRight } from "@/components/molecules/PageHeaderSlot";
import { ApiError, api } from "@/api/client";
import { applyFieldDefaults, type ResourceSpec } from "@/lib/resource-registry";
import { setByPath } from "@/lib/path";
import { useInvalidateResourceList, useOperation } from "@/lib/use-operation";
import { toast } from "@/lib/toast";

interface Props {
  spec: ResourceSpec;
  parentField?: string;
  parentParam?: string;
}

export function ResourceCreatePage({ spec, parentField, parentParam }: Props) {
  const params = useParams();
  const navigate = useNavigate();
  const location = useLocation();
  const [searchParams] = useSearchParams();
  const filterValue = parentParam ? (params[parentParam] ?? null) : null;
  const invalidate = useInvalidateResourceList();

  const ctx = useMemo(
    () => ({
      projectId: parentField === "project_id" ? (filterValue ?? undefined) : undefined,
    }),
    [parentField, filterValue],
  );

  // Контекст приходит либо из nested-URL params (`/networks/<n>/.../create`),
  // либо из query (`?network_id=...&subnet_id=...&kind=...`) для обратной
  // совместимости и для случая create-из-list-page с pre-selected parent.
  // `presetFields` — заблокированные (immutable) поля из контекста.
  // `softPresetFields` — предзаполненные, но editable (начальное значение,
  // не lock). Пример: `_address_kind` для адреса из контекста подсети — дефолт
  // "internal", но пользователь может переключить на "internal_v6".
  const { presetFields, softPresetFields, fieldOptionsFilter } = useMemo(() => {
    const out: Record<string, unknown> = {};
    const soft: Record<string, unknown> = {};
    const optFilter: Record<string, string[]> = {};
    const subnetId = (params.subnetId as string | undefined) ?? searchParams.get("subnet_id");
    const networkId = (params.networkId as string | undefined) ?? searchParams.get("network_id");
    const kind = searchParams.get("kind");
    if (spec.id === "addresses" && subnetId) {
      // Адрес в контексте подсети — только ВНУТРЕННИЙ (internal); привязан к
      // этой подсети, sanitize выкинет неактивную ветку.
      out["internal_ipv4_address_spec.subnet_id"] = subnetId;
      out["internal_ipv6_address_spec.subnet_id"] = subnetId;
      soft["_address_kind"] = kind === "internal_v6" ? "internal_v6" : "internal";
      optFilter["_address_kind"] = ["internal", "internal_v6"];
    } else {
      if (kind) out["_address_kind"] = kind;
      if (subnetId) out["subnet_id"] = subnetId;
      // Глобальное создание адреса — только ВНЕШНИЙ (external); internal задаётся
      // из контекста подсети.
      if (spec.id === "addresses") optFilter["_address_kind"] = ["external", "external_v6"];
    }
    if (networkId) out["network_id"] = networkId;
    return { presetFields: out, softPresetFields: soft, fieldOptionsFilter: optFilter };
  }, [params.subnetId, params.networkId, searchParams, spec.id]);

  const initialObj = useMemo(() => {
    const tpl = spec.template(ctx);
    const baseObj = typeof tpl === "object" && tpl !== null ? { ...(tpl as Record<string, unknown>) } : {};
    let merged: Record<string, unknown> = applyFieldDefaults(spec.fields, baseObj);
    for (const [path, val] of Object.entries(softPresetFields)) {
      merged = setByPath(merged, path, val);
    }
    for (const [path, val] of Object.entries(presetFields)) {
      merged = setByPath(merged, path, val);
    }
    // Auto-name: пустое name + UNIQUE на (project_id, name) → ALREADY_EXISTS
    // на повторе. Генерируем <route>-NNNNNN.
    if (spec.fields?.some((f) => f.name === "name") && (!merged.name || merged.name === "")) {
      const stem = spec.route.replace(/-/g, "");
      merged.name = `${stem}-${Math.floor(100000 + Math.random() * 900000)}`;
    }
    return merged;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const [obj, setObj] = useState<Record<string, unknown>>(initialObj);

  const lockedPathsRef = useRef(new Set(Object.keys(presetFields)));

  // Back = текущий path без /create суффикса. Для nested URL вида
  //   /projects/X/vpc/networks/Y/route-tables/create
  // полученный URL `/projects/X/vpc/networks/Y/route-tables` не существует —
  // вместо этого возвращаемся к parent detail (network detail с табом).
  const rawBack = location.pathname.replace(/\/create$/, "") || "/";
  const projectId = params.projectId as string | undefined;
  const networkId = params.networkId as string | undefined;
  const subnetId = params.subnetId as string | undefined;
  const isNestedUnderSubnet = !!(projectId && subnetId);
  const isNestedUnderNetwork = !!(projectId && networkId);
  const backHref = isNestedUnderSubnet
    ? networkId
      ? `/projects/${projectId}/vpc/networks/${networkId}/subnets/${subnetId}?tab=addresses`
      : `/projects/${projectId}/vpc/subnets/${subnetId}?tab=addresses`
    : isNestedUnderNetwork
      ? `/projects/${projectId}/vpc/networks/${networkId}?tab=${spec.route}`
      : rawBack;

  const breadcrumb = useMemo(
    () => (
      <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
        {spec.serviceTitle && (
          <>
            <Typography.Text type="secondary">{spec.serviceTitle}</Typography.Text>
            <Typography.Text type="secondary">/</Typography.Text>
          </>
        )}
        <Link to={backHref}>
          <Typography.Text type="secondary">{spec.plural}</Typography.Text>
        </Link>
        <Typography.Text type="secondary">/</Typography.Text>
        <Typography.Text strong>Создать</Typography.Text>
      </span>
    ),
    [backHref, spec.plural, spec.serviceTitle],
  );
  useBreadcrumb(breadcrumb);
  const noHeaderRight = useMemo(() => null, []);
  useHeaderRight(noHeaderRight);

  // Doppler-flow: после POST дожидаемся op.done через polling. Кнопка
  // пульсирует пока pending. По завершении — toast + navigate на список.
  const [pendingOpId, setPendingOpId] = useState<string | null>(null);
  const { data: op } = useOperation(pendingOpId);

  const mutation = useMutation({
    mutationFn: (item: unknown) => api.create(spec.apiPath, item),
    onSuccess: (resp) => {
      const id = extractOperationId(resp);
      if (id) {
        setPendingOpId(id);
      } else {
        // Sync (Region/Zone/AddressPool — admin RPC без Operation envelope).
        invalidate(spec.id, filterValue ?? null);
        navigate(backHref);
      }
    },
    onError: (err) => {
      const m = err instanceof ApiError ? `${err.code}: ${err.message}` : (err as Error).message;
      toast.error(`Создать ${spec.singular}: ${m}`);
    },
  });

  useEffect(() => {
    if (!pendingOpId || !op?.done) return;
    if (op.error) {
      toast.error(`Создать ${spec.singular}: ${op.error.message ?? "ошибка"}`);
      setPendingOpId(null);
    } else {
      invalidate(spec.id, filterValue ?? null);
      toast.success(`${spec.singular} создан`);
      setPendingOpId(null);
      navigate(backHref);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [op?.done, op?.error?.code]);

  const submit = () => {
    let parsed: Record<string, unknown> = obj;
    // Клиент-валидация ДО sanitize (инварианты читают UI-дискриминаторы формы).
    if (spec.validate) {
      const err = spec.validate(parsed);
      if (err) {
        toast.error(err);
        return;
      }
    }
    if (spec.sanitize) parsed = spec.sanitize(parsed);
    mutation.mutate(parsed);
  };

  const fields = spec.fields;
  if (!fields) {
    return <Alert type="warning" message={`У ресурса ${spec.singular} нет form-schema; используйте API напрямую.`} />;
  }

  return (
    <div style={{ maxWidth: FORM_WIDTH }}>
      {/* Беклинк убран (req) — путь назад есть в breadcrumb хедера. */}
      <ResourceFormBody
        spec={spec}
        mode="create"
        obj={obj}
        onChange={setObj}
        lockedPaths={lockedPathsRef.current}
        fieldOptionsFilter={fieldOptionsFilter}
        submitLabel={`Создать ${spec.singular.toLowerCase()}`}
        submitting={mutation.isPending || pendingOpId !== null}
        onSubmit={submit}
        onCancel={() => navigate(backHref)}
      />
    </div>
  );
}
