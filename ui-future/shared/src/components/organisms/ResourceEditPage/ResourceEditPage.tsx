// ResourceEditPage — full-page Edit (экран "Изменение ...").
// Поллит ресурс по id, заполняет initial state, отправляет PATCH с update_mask.

import { useEffect, useMemo, useRef, useState } from "react";
import { Link, useLocation, useNavigate, useParams } from "react-router-dom";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Alert, Button, Space, Spin, Typography } from "antd";
import { ArrowLeftOutlined } from "@ant-design/icons";
import { ErrorResult } from "@shared/components/molecules/ErrorResult";
import { ResourceFormBody } from "@shared/components/organisms/form/ResourceFormBody";
import { FORM_WIDTH } from "@shared/components/organisms/form/FormShell";
import { extractOperationId } from "@shared/components/molecules/OperationDialog";
import { computeUpdateMask, snakeToCamelPath } from "@shared/lib/update-mask";
import { useBreadcrumb, useHeaderRight } from "@shared/components/molecules/PageHeaderSlot";
import { ApiError, api } from "@shared/api/client";
import { applyFieldDefaults, type ResourceSpec } from "@shared/lib/resource-registry";
import { useInvalidateResourceList, useOperation } from "@shared/lib/use-operation";
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

  const { data, isLoading, isError, error } = useQuery({
    queryKey: [spec.id, "detail", uid],
    queryFn: () => api.get<Record<string, unknown>>(`${spec.apiPath}/${uid}`),
    enabled: !!uid,
    staleTime: 0,
  });

  const fields = spec.fields;
  const originalRef = useRef<Record<string, unknown> | null>(null);
  const [obj, setObj] = useState<Record<string, unknown>>({});
  const [hydrated, setHydrated] = useState(false);

  useEffect(() => {
    if (!data || hydrated) return;
    const baseObj: Record<string, unknown> = { ...data };
    const merged = applyFieldDefaults(fields, baseObj);
    originalRef.current = baseObj;
    setObj(merged);
    setHydrated(true);
  }, [data, fields, hydrated]);

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
  const { data: op } = useOperation(pendingOpId);

  const mutation = useMutation({
    mutationFn: (item: unknown) => api.update(`${spec.apiPath}/${uid}`, item),
    onSuccess: (resp) => {
      const opId = extractOperationId(resp);
      if (opId) {
        setPendingOpId(opId);
      } else {
        invalidate(spec.id, project?.id ?? null);
        navigate(backHref);
      }
    },
    onError: (err) => {
      const m = err instanceof ApiError ? `${err.code}: ${err.message}` : (err as Error).message;
      toast.error(`Сохранить ${spec.singular}: ${m}`);
    },
  });

  useEffect(() => {
    if (!pendingOpId || !op?.done) return;
    if (op.error) {
      toast.error(`Сохранить ${spec.singular}: ${op.error.message ?? "ошибка"}`);
      setPendingOpId(null);
    } else {
      invalidate(spec.id, project?.id ?? null);
      toast.success(`${spec.singular} сохранён`);
      setPendingOpId(null);
      navigate(backHref);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [op?.done, op?.error?.code]);

  const submit = () => {
    if (!fields || !originalRef.current) return;
    let parsed: Record<string, unknown> = obj;
    if (spec.sanitize) parsed = spec.sanitize(parsed);
    const mask = computeUpdateMask(originalRef.current, parsed, fields);
    if (mask.length === 0) {
      navigate(backHref);
      return;
    }
    const payload = {
      ...parsed,
      update_mask: mask.map(snakeToCamelPath).join(","),
    };
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
