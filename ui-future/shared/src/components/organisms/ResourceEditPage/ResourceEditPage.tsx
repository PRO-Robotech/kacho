// ResourceEditPage — full-page Edit (экран "Изменение ...").
// Поллит ресурс по id, заполняет initial state, отправляет PATCH с update_mask.

import { useEffect, useMemo, useRef, useState } from "react";
import { Link, useLocation, useNavigate, useParams } from "react-router";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Alert, Button, Space, Spin, Typography } from "antd";
import { ArrowLeftOutlined } from "@ant-design/icons";
import { ErrorResult } from "@shared/components/molecules/ErrorResult";
import { ResourceFormBody } from "@shared/components/organisms/form/ResourceFormBody";
import { FORM_WIDTH } from "@shared/components/organisms/form/FormShell";
import { buildUpdateBody, computeUpdateMask } from "@shared/lib/update-mask";
import { useBreadcrumb, useHeaderRight } from "@shared/components/molecules/PageHeaderSlot";
import { ApiError, api } from "@shared/api/client";
import { applyFieldDefaults, editReadPath, mutationBasePath, type ResourceSpec } from "@shared/lib/resource-registry";
import { useInvalidateResourceList, useOperation } from "@shared/lib/use-operation";
import { operationOutcome, resolveMutationResponse } from "@shared/lib/operation-outcome";
import { toast } from "@shared/lib/toast";
import { useProjectStore } from "@shared/lib/context-store";
import { useNestedBreadcrumb } from "@shared/lib/use-nested-breadcrumb";

interface Props {
  spec: ResourceSpec;
  paramKey?: string;
}

export function ResourceEditPage({ spec, paramKey = "uid" }: Props) {
  const params = useParams();
  const navigate = useNavigate();
  const location = useLocation();
  const project = useProjectStore((s) => s.project);
  const invalidate = useInvalidateResourceList();

  const uid = params[paramKey];

  // backHref = current path без /edit (вернуться на detail).
  const backHref = location.pathname.replace(/\/edit$/, "") || "/";

  // Начальное состояние формы читается с той проекции, где живут мутируемые
  // поля. У двухпроекционного ресурса (geo Region/Zone) это Internal: `status` и
  // infra° на публичной проекции отсутствуют, и форма, заполненная публичным
  // чтением, показала бы пустое поле там, где значение есть.
  const readPath = uid ? editReadPath(spec, uid) : "";
  const { data, isLoading, isError, error } = useQuery({
    queryKey: [spec.id, "detail", uid, readPath],
    queryFn: () => api.get<Record<string, unknown>>(readPath),
    enabled: !!uid,
    staleTime: 0,
  });

  const fields = spec.fields;
  const originalRef = useRef<Record<string, unknown> | null>(null);
  const [obj, setObj] = useState<Record<string, unknown>>({});
  const [hydrated, setHydrated] = useState(false);

  useEffect(() => {
    if (!data || hydrated) return;
    // hydrate — обратная sanitize: wire-форма поля (repeated, вложенный oneof) в
    // то, что редактирует форма. Сравнение для update_mask идёт против
    // ГИДРАТИРОВАННОГО снимка, иначе поле числилось бы изменённым, пока его не
    // трогали.
    const baseObj: Record<string, unknown> = spec.hydrate ? { ...spec.hydrate({ ...data }) } : { ...data };
    const merged = applyFieldDefaults(fields, baseObj);
    // Снимок для диффа хранится в ТОЙ ЖЕ форме, что даёт sanitize: маска считается
    // против санитайзнутого объекта, и спека, чей sanitize меняет представление
    // (число → Duration "300s"), иначе выглядела бы изменённой на каждом сохранении —
    // поле, которого оператор не трогал, уезжало бы в update_mask.
    originalRef.current = spec.sanitize ? spec.sanitize(baseObj) : baseObj;
    setObj(merged);
    setHydrated(true);
  }, [data, fields, hydrated, spec]);

  const name = (data?.name as string | undefined) ?? uid ?? "";

  // Auto-detect nested-context из URL params (projectId/networkId/subnetId).
  // Возвращает дополнительные breadcrumb-сегменты для тех ресурсов, чей
  // detail-URL nested под Network/Subnet.
  const nested = useNestedBreadcrumb({
    projectId: params.projectId,
    networkId: params.networkId,
    subnetId: params.subnetId,
    currentResourcePlural: spec.plural,
  });

  const breadcrumb = useMemo(() => {
    const tailSegments = nested.segments ?? [{ label: spec.plural, href: backHref.replace(/\/[^/]+$/, "") }];
    return (
      <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
        {spec.serviceTitle && (
          <>
            <Typography.Text type="secondary">{spec.serviceTitle}</Typography.Text>
            <Typography.Text type="secondary">/</Typography.Text>
          </>
        )}
        {tailSegments.map((seg, i) => (
          <span key={i} style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
            {seg.href ? (
              <Link to={seg.href}>
                <Typography.Text type="secondary">{seg.label}</Typography.Text>
              </Link>
            ) : (
              <Typography.Text type="secondary">{seg.label}</Typography.Text>
            )}
            <Typography.Text type="secondary">/</Typography.Text>
          </span>
        ))}
        <Link to={backHref}>
          <Typography.Text type="secondary">{name}</Typography.Text>
        </Link>
        <Typography.Text type="secondary">/</Typography.Text>
        <Typography.Text strong>Редактировать</Typography.Text>
      </span>
    );
  }, [backHref, spec.plural, spec.serviceTitle, name, nested.segments]);
  useBreadcrumb(breadcrumb);
  const noHeaderRight = useMemo(() => null, []);
  useHeaderRight(noHeaderRight);

  // Doppler-flow: ждём op.done через polling, кнопка пульсирует.
  const [pendingOpId, setPendingOpId] = useState<string | null>(null);
  const { data: op, error: opFetchError } = useOperation(pendingOpId);
  const outcome = operationOutcome({ opId: pendingOpId, op, fetchError: opFetchError });

  const mutation = useMutation({
    mutationFn: (item: unknown) => api.update(`${mutationBasePath(spec)}/${uid}`, item),
    onSuccess: (resp) => {
      const resolved = resolveMutationResponse(resp, spec.mutationsReturnOperation === true);
      if (resolved.kind === "operation") {
        setPendingOpId(resolved.opId);
        return;
      }
      if (resolved.kind === "violation") {
        toast.error(`Сохранить ${spec.singular}: ${resolved.message}`);
        return;
      }
      invalidate(spec.id, project?.id ?? null);
      void navigate(backHref);
    },
    onError: (err) => {
      const m = err instanceof ApiError ? `${err.code}: ${err.message}` : err.message;
      toast.error(`Сохранить ${spec.singular}: ${m}`);
    },
  });

  useEffect(() => {
    if (outcome.kind === "failed") {
      toast.error(`Сохранить ${spec.singular}: ${outcome.message}`);
      setPendingOpId(null);
      return;
    }
    if (outcome.kind !== "succeeded") return;
    invalidate(spec.id, project?.id ?? null);
    toast.success(`${spec.singular} сохранён`);
    setPendingOpId(null);
    void navigate(backHref);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [outcome.kind, outcome.kind === "failed" ? outcome.message : null]);

  const submit = () => {
    if (!fields || !originalRef.current) return;
    let parsed: Record<string, unknown> = obj;
    if (spec.sanitize) parsed = spec.sanitize(parsed);
    const mask = computeUpdateMask(originalRef.current, parsed, fields);
    // The body carries the masked fields and nothing else — the form was hydrated
    // from a GET projection whose id/created_at/status/output-only mirrors are not
    // fields of any Update* message.
    const payload = buildUpdateBody(parsed, mask);
    if (!payload) {
      void navigate(backHref);
      return;
    }
    mutation.mutate(payload);
  };

  if (!fields) {
    return <Alert type="warning" message={`У ресурса ${spec.singular} нет form-schema; используйте API напрямую.`} />;
  }

  if (isLoading && !data) {
    return (
      <div style={{ padding: 24 }}>
        <Spin tip="Загрузка…" />
      </div>
    );
  }

  if (isError || !data) {
    return (
      <ErrorResult
        error={error ?? undefined}
        status={!isError && !data ? "404" : undefined}
        subTitle={!isError && !data ? "Ресурс не найден." : undefined}
        extra={
          <Link to={backHref}>
            <Button icon={<ArrowLeftOutlined />}>Назад</Button>
          </Link>
        }
      />
    );
  }

  return (
    <div style={{ maxWidth: FORM_WIDTH }}>
      <Space direction="vertical" size={20} style={{ width: "100%" }}>
        <div>
          <Link to={backHref}>
            <Button type="text" size="small" icon={<ArrowLeftOutlined />} style={{ marginLeft: -8 }}>
              {name}
            </Button>
          </Link>
        </div>
        <ResourceFormBody
          spec={spec}
          mode="edit"
          obj={obj}
          onChange={setObj}
          submitLabel="Сохранить"
          submitting={mutation.isPending || pendingOpId !== null}
          onSubmit={submit}
          onCancel={() => navigate(backHref)}
        />
      </Space>
    </div>
  );
}
