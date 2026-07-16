// AccessBindingsPage — управление AccessBinding'ами (единая плоская таблица в
// выбранном Account) + full-page scope-first форма создания привязки.
//
// Create — отдельная full-page форма (AccessBindingCreatePage), рендерит
// scope-first <AccessBindingCreateForm> (тот же body переиспользуется embedded в
// зону-3 detail-страницы субъекта — ResourceShell child-create с залоченным
// субъектом). CTA «Создать привязку доступа» делает navigate на неё.

import { useMemo, useState } from "react";
import { Navigate, useNavigate, useSearchParams } from "react-router-dom";
import { Button, Empty, Select, Space, Typography } from "antd";
import { FilterOutlined, PlusOutlined } from "@ant-design/icons";
import { useQuery } from "@tanstack/react-query";
import { ResourceTable, type Column } from "@shared/components/organisms/ResourceTable";
import { RowActionsMenu } from "@shared/components/molecules/RowActionsMenu";
import { REGISTRY, getByPath } from "@shared/lib/resource-registry";
import { buildSpecColumns } from "@shared/lib/spec-columns";
import { iamApi, type AccessBindingList } from "@shared/api/iam";
import { FormShell } from "@shared/components/organisms/form/FormShell";
import { useBreadcrumb, useHeaderRight } from "@shared/components/molecules/PageHeaderSlot";
import { useContext } from "@shared/lib/context-store";
import { IamListShell } from "@/components/organisms/iam/IamListShell";
import {
  AccessBindingCreateForm,
  type SubjectType,
  type ResourceType,
} from "@/components/organisms/iam/AccessBindingCreateForm";

// Высокоуровневые скоупы, принимаемые backend validResourceTypes. "cluster" — для
// unified cluster-admin grant (resource_id = "cluster_kacho_root").
export const RESOURCE_TYPES: ResourceType[] = ["account", "project", "cluster"];

/** Cluster singleton id для resource_type="cluster". */
export const CLUSTER_RESOURCE_ID = "cluster_kacho_root";

/** Cluster admin role id (для preset'ов / quick-grant link'ов). */
export const CLUSTER_ADMIN_ROLE_ID = "roles/admin";

// AccessBindingsPage — единая плоская таблица привязок доступа в выбранном
// Account. Account берётся из context-store (пилюля в шапке), как и у прочих
// account-scoped IAM-страниц. Показывается ОДНА таблица «кто какую роль имеет на
// каком ресурсе» — GET /iam/v1/accounts/{id}/accessBindings (ListByAccount).
//
// Таблица — ТИПОВАЯ (как у generic-списков): ResourceTable + buildSpecColumns +
// kebab-колонка RowActionsMenu (Просмотр + Отозвать = Delete). Create — отдельная
// full-page форма (AccessBindingCreatePage); CTA «Создать привязку доступа» делает
// navigate на неё. Detail — клик по строке.
export function AccessBindingsPage() {
  const account = useContext((s) => s.account);
  const navigate = useNavigate();
  const abSpec = REGISTRY["access-bindings"];

  // Legacy deep-link ?modal=cluster-admin / ?modal=access-bindings-create →
  // редирект на full-page create, сохраняя preset-параметры.
  const [searchParams] = useSearchParams();
  const legacyModal = searchParams.get("modal");
  const legacyRedirect = useMemo(() => {
    if (legacyModal !== "cluster-admin" && legacyModal !== "access-bindings-create") return null;
    const next = new URLSearchParams(searchParams);
    next.delete("modal");
    if (legacyModal === "cluster-admin") {
      if (!next.get("resource_type")) next.set("resource_type", "cluster");
      if (!next.get("resource_id")) next.set("resource_id", CLUSTER_RESOURCE_ID);
      if (!next.get("role_id")) next.set("role_id", CLUSTER_ADMIN_ROLE_ID);
    }
    const qs = next.toString();
    return `/iam/access-bindings/create${qs ? `?${qs}` : ""}`;
  }, [legacyModal, searchParams]);

  // Лёгкий фильтр-ряд: только активные / включая отозванные + опц. subject_type.
  const [includeRevoked, setIncludeRevoked] = useState(false);
  const [subjectTypeFilter, setSubjectTypeFilter] = useState<string>("");

  const accountId = account?.id ?? "";

  // Единая таблица — все bindings, видимые в выбранном Account (account-scoped +
  // project-scoped). Не запрашиваем, пока Account не выбран.
  const bindingsQ = useQuery({
    queryKey: ["iam", "access-bindings", "by-account", accountId, subjectTypeFilter, includeRevoked],
    queryFn: () =>
      iamApi.listAccessBindingsByAccount(accountId, {
        page_size: 200,
        subject_type_filter: subjectTypeFilter || undefined,
        include_revoked: includeRevoked,
      }),
    enabled: !!accountId,
    refetchInterval: 5_000,
    staleTime: 0,
  });
  const bindings = (bindingsQ.data as AccessBindingList | undefined)?.access_bindings ?? [];

  // Колонки — из REGISTRY + kebab-колонка. revoke = Delete (RowActionsMenu,
  // spec.ops.delete); by-account query имеет refetchInterval 5s, строка уходит ≤5s.
  const columns: Column<Record<string, unknown>>[] = useMemo(() => {
    const cols = buildSpecColumns(abSpec);
    cols.push({
      header: "",
      className: "text-right whitespace-nowrap",
      cell: (row) => <RowActionsMenu spec={abSpec} row={row} basePath="/iam/access-bindings" projectId={null} />,
    });
    return cols;
  }, [abSpec]);

  // breadcrumb / CTA через header-слоты. Мемоизируем node'ы (useEffect на [node]).
  const breadcrumbNode = useMemo(
    () => (
      <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
        <Typography.Text type="secondary">{abSpec.serviceTitle}</Typography.Text>
        <Typography.Text type="secondary">/</Typography.Text>
        <Typography.Text strong>{abSpec.plural}</Typography.Text>
      </span>
    ),
    [abSpec.serviceTitle, abSpec.plural],
  );
  const ctaNode = useMemo(
    () => (
      <Button
        type="primary"
        icon={<PlusOutlined />}
        onClick={() => navigate("/iam/access-bindings/create")}
        data-testid="access-bindings-create-btn"
      >
        Создать привязку доступа
      </Button>
    ),
    [navigate],
  );
  useBreadcrumb(breadcrumbNode);
  useHeaderRight(ctaNode);

  if (legacyRedirect) return <Navigate to={legacyRedirect} replace />;

  if (!account) {
    return (
      <IamListShell specId="access-bindings" title={abSpec.plural}>
        <Empty
          description="Выберите Account вверху секции, чтобы увидеть привязки доступа."
          image={Empty.PRESENTED_IMAGE_SIMPLE}
          style={{ padding: "48px 0" }}
        />
      </IamListShell>
    );
  }

  return (
    <IamListShell
      specId="access-bindings"
      title={abSpec.plural}
      count={bindings.length}
      right={
        <Space size={8} align="center" wrap>
          <FilterOutlined style={{ opacity: 0.4, fontSize: 14 }} />
          <Select
            value={includeRevoked ? "true" : "false"}
            onChange={(v) => setIncludeRevoked(v === "true")}
            options={[
              { value: "false", label: "Только активные" },
              { value: "true", label: "Включая отозванные" },
            ]}
            style={{ width: 190 }}
            data-testid="access-bindings-revoked-filter"
          />
          <Select
            placeholder="Тип субъекта: все"
            value={subjectTypeFilter || undefined}
            onChange={(v) => setSubjectTypeFilter(v ?? "")}
            allowClear
            options={[
              { value: "user", label: "Пользователь" },
              { value: "service_account", label: "Сервисный аккаунт" },
              { value: "group", label: "Группа" },
            ]}
            style={{ width: 190 }}
            data-testid="access-bindings-subject-filter"
          />
        </Space>
      }
    >
      <div style={{ flex: 1, minHeight: 0, minWidth: 0 }} data-testid="access-bindings-table">
        <ResourceTable
          rows={bindings as unknown as Record<string, unknown>[]}
          columns={columns}
          rowKey={(r) => getByPath<string>(r, "id") ?? Math.random().toString()}
          loading={bindingsQ.isLoading}
          onRowClick={(row) => {
            const id = getByPath<string>(row, "id");
            if (id) navigate(`/iam/access-bindings/${id}`);
          }}
        />
      </div>
    </IamListShell>
  );
}

/**
 * Preset для create-формы — pre-fill полей deep-link'а (cluster-admin grant из
 * ClusterAdminsPage; grant с залоченным субъектом из вкладки «Привилегии»).
 */
export interface AccessBindingPreset {
  subject_type?: SubjectType;
  subject_id?: string;
  role_id?: string;
  resource_type?: ResourceType;
  resource_id?: string;
}

// AccessBindingCreatePage — full-page обёртка scope-first формы создания привязки.
// Тело формы — переиспользуемый <AccessBindingCreateForm> (тот же body рендерится
// embedded в зону-3 detail-страницы субъекта). Preset/deep-link через
// query-параметры (subject_type/subject_id/role_id/resource_type/resource_id);
// lock_subject=1 → субъект залочен (реконсайл-режим).
export function AccessBindingCreatePage() {
  const navigate = useNavigate();
  const abSpec = REGISTRY["access-bindings"];

  const [searchParams] = useSearchParams();
  const presetSubjectType = (searchParams.get("subject_type") as SubjectType | null) ?? undefined;
  const presetSubjectId = searchParams.get("subject_id") ?? undefined;
  const lockSubject = searchParams.get("lock_subject") === "1";

  const preset: AccessBindingPreset = useMemo(
    () => ({
      subject_type: presetSubjectType,
      subject_id: presetSubjectId,
      role_id: searchParams.get("role_id") ?? undefined,
      resource_type: (searchParams.get("resource_type") as ResourceType | null) ?? undefined,
      resource_id: searchParams.get("resource_id") ?? undefined,
    }),
    [searchParams, presetSubjectType, presetSubjectId],
  );

  const lockedSubject =
    lockSubject && presetSubjectType && presetSubjectId
      ? { type: presetSubjectType, id: presetSubjectId }
      : undefined;

  const breadcrumb = useMemo(
    () => (
      <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
        <Typography.Text type="secondary">{abSpec.serviceTitle}</Typography.Text>
        <Typography.Text type="secondary">/</Typography.Text>
        <Typography.Text type="secondary">{abSpec.plural}</Typography.Text>
        <Typography.Text type="secondary">/</Typography.Text>
        <Typography.Text strong>Создать</Typography.Text>
      </span>
    ),
    [abSpec.serviceTitle, abSpec.plural],
  );
  useBreadcrumb(breadcrumb);
  useHeaderRight(useMemo(() => null, []));

  // Success-навигация: lock_subject → на вкладку «Привилегии» субъекта; иначе на
  // список привязок. (Инвалидация query-кэша — внутри формы.)
  const onSuccess = () => {
    if (lockedSubject) {
      const route =
        lockedSubject.type === "service_account"
          ? "service-accounts"
          : lockedSubject.type === "group"
            ? "groups"
            : "users";
      navigate(`/iam/${route}/${lockedSubject.id}/privileges`);
      return;
    }
    navigate("/iam/access-bindings");
  };

  return (
    <FormShell specId="access-bindings" mode="create" singular={abSpec.singular}>
      <AccessBindingCreateForm
        lockedSubject={lockedSubject}
        preset={preset}
        onSuccess={onSuccess}
        onCancel={() => navigate("/iam/access-bindings")}
      />
    </FormShell>
  );
}
