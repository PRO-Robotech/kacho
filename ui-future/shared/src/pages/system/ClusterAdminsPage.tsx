// ClusterAdminsPage — KAC-196 Task 5.
//
// /system/cluster/admins — UI для управления cluster-admin grants. AntD `<Table>`
// с колонками Email / Display name / Granted by / Granted at / Actions.
// Header: кнопка «Добавить admin» → GrantAdminModal.
// Per-row action «Отозвать» → Popconfirm → clusterApi.revokeAdmin → poll Op.
//
// Backend guards (proto-доку см. internal_cluster_service.proto):
//   D-5 — нельзя revoke самого себя (server returns FailedPrecondition).
//   D-6 — нельзя revoke последнего admin'а (FailedPrecondition).
// UI дублирует guards на клиенте через `disabled` + tooltip, чтобы не отправлять
// заведомо обречённый запрос.
//
// 403 от api-gateway → форбидден-блок с `data-testid="cluster-admins-forbidden"`
// (для системы прав см. workspace CLAUDE.md §«Инфра-чувствительные данные»).

import { useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Alert, Button, Popconfirm, Space, Spin, Table, Tooltip, Typography } from "antd";
import { DeleteOutlined, ExclamationCircleOutlined, ReloadOutlined, UserAddOutlined } from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ApiError } from "@shared/api/client";
import { clusterApi, type ClusterAdminEntry } from "@shared/api/cluster";
import { ErrorResult } from "@shared/components/molecules/ErrorResult";
import { CopyableMonoId, fmtTs } from "@shared/components/organisms/iam/IamCommon";
import { GrantAdminModal } from "@shared/components/organisms/system/GrantAdminModal";
import { useAuth } from "@shared/contexts/AuthContext";
import { useOperation } from "@shared/lib/use-operation";
import { toast } from "@shared/lib/toast";

export default function ClusterAdminsPage() {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const { user } = useAuth();
  const [grantOpen, setGrantOpen] = useState(false);
  const [revokeOpId, setRevokeOpId] = useState<string | null>(null);
  const [revokingId, setRevokingId] = useState<string | null>(null);

  const adminsQ = useQuery({
    queryKey: ["cluster-admins"],
    queryFn: clusterApi.listAdmins,
    retry: (failureCount, error) => {
      // 401/403 — не ретраим (форбидден показываем сразу).
      if (error instanceof ApiError && (error.status === 401 || error.status === 403)) {
        return false;
      }
      return failureCount < 1;
    },
    refetchInterval: 10_000,
    staleTime: 5_000,
  });

  // Cluster singleton — мелкая sync-проверка, что мы вообще можем читать;
  // если падает 403 — показываем forbidden-страницу. Не блокирует list.
  const clusterQ = useQuery({
    queryKey: ["cluster"],
    queryFn: clusterApi.get,
    retry: false,
    staleTime: 60_000,
  });

  const isForbidden = useMemo(() => {
    const err = adminsQ.error ?? clusterQ.error;
    return err instanceof ApiError && (err.status === 401 || err.status === 403);
  }, [adminsQ.error, clusterQ.error]);

  // Poll revoke-operation до done; success → invalidate; error → toast.
  const { data: revokeOp } = useOperation(revokeOpId);
  useEffect(() => {
    if (!revokeOp?.done || !revokeOpId) return;
    if (revokeOp.error) {
      toast.error(revokeOp.error.message || "Не удалось отозвать admin");
    } else {
      toast.success("Admin отозван");
      qc.invalidateQueries({ queryKey: ["cluster-admins"] });
    }
    setRevokeOpId(null);
    setRevokingId(null);
  }, [revokeOp?.done, revokeOp?.error, revokeOpId, qc]);

  const handleRevoke = async (row: ClusterAdminEntry) => {
    setRevokingId(row.subject_id);
    try {
      const resp = await clusterApi.revokeAdmin(row.subject_id);
      const id = resp.operation?.id;
      if (id) {
        setRevokeOpId(id);
      } else {
        toast.success("Admin отозван");
        qc.invalidateQueries({ queryKey: ["cluster-admins"] });
        setRevokingId(null);
      }
    } catch (e) {
      const msg = e instanceof ApiError ? e.message : e instanceof Error ? e.message : "Ошибка";
      toast.error(msg);
      setRevokingId(null);
    }
  };

  const admins = adminsQ.data ?? [];
  const adminsCount = admins.length;
  const currentUserId = user?.id ?? "";

  const columns: ColumnsType<ClusterAdminEntry> = [
    {
      title: "Эл. почта",
      dataIndex: "subject_email",
      key: "subject_email",
      render: (v: string, row) =>
        v ? (
          <Typography.Text strong>{v}</Typography.Text>
        ) : (
          <Typography.Text type="secondary">{row.subject_id}</Typography.Text>
        ),
    },
    {
      title: "Отображаемое имя",
      dataIndex: "subject_display_name",
      key: "subject_display_name",
      render: (v: string) => v || <Typography.Text type="secondary">—</Typography.Text>,
    },
    {
      title: "Subject ID",
      dataIndex: "subject_id",
      key: "subject_id",
      width: 220,
      render: (v: string) => <CopyableMonoId id={v} />,
    },
    {
      title: "Кем выдано",
      dataIndex: "granted_by_email",
      key: "granted_by_email",
      render: (v: string, row) => {
        if (row.granted_by_user_id === "bootstrap") {
          return <Typography.Text type="secondary">bootstrap</Typography.Text>;
        }
        return v || <CopyableMonoId id={row.granted_by_user_id} />;
      },
    },
    {
      title: "Дата выдачи",
      dataIndex: "granted_at",
      key: "granted_at",
      width: 180,
      render: (v?: string) => fmtTs(v),
    },
    {
      title: "",
      key: "actions",
      width: 60,
      render: (_v, row) => {
        const isSelf = row.subject_id === currentUserId;
        const isLast = adminsCount === 1;
        const disabled = isSelf || isLast;
        const tooltip = isSelf ? "Нельзя отозвать самого себя" : isLast ? "Нельзя отозвать последнего admin'а" : "";
        const button = (
          <Button
            size="small"
            type="text"
            danger
            icon={<DeleteOutlined />}
            disabled={disabled}
            loading={revokingId === row.subject_id}
            data-testid={`cluster-admins-revoke-${row.subject_id}`}
          />
        );
        if (disabled) {
          return <Tooltip title={tooltip}>{button}</Tooltip>;
        }
        return (
          <Popconfirm
            title="Отозвать admin?"
            description={`Удалить cluster admin у «${row.subject_email || row.subject_id}»?`}
            okText="Отозвать"
            okButtonProps={{ danger: true }}
            cancelText="Отмена"
            onConfirm={() => void handleRevoke(row)}
            icon={<ExclamationCircleOutlined style={{ color: "#ff4d4f" }} />}
          >
            {button}
          </Popconfirm>
        );
      },
    },
  ];

  if (isForbidden) {
    return (
      <div data-testid="cluster-admins-forbidden">
        <ErrorResult
          status="403"
          title="403"
          subTitle="Недостаточно прав для просмотра cluster admin'ов. Требуется FGA-relation admin@cluster:cluster_kacho_root."
        />
      </div>
    );
  }

  if (adminsQ.isLoading && admins.length === 0) {
    return (
      <div style={{ padding: 48, textAlign: "center" }}>
        <Spin size="large" />
      </div>
    );
  }

  return (
    <Space direction="vertical" size={16} style={{ width: "100%" }} data-testid="cluster-admins-page">
      <div>
        <Typography.Title level={3} style={{ margin: 0 }} data-testid="cluster-admins-page-title">
          Cluster admins
        </Typography.Title>
        <Typography.Text type="secondary" style={{ fontSize: 13 }}>
          Управление permanent cluster-admin grants на singleton{" "}
          <Typography.Text code style={{ fontSize: 12 }}>
            cluster:cluster_kacho_root
          </Typography.Text>
          . Все мутации идут через async Operation envelope.
        </Typography.Text>
      </div>

      <Space size={8} wrap>
        <Button
          type="primary"
          icon={<UserAddOutlined />}
          onClick={() => setGrantOpen(true)}
          data-testid="cluster-admins-grant-button"
        >
          Добавить admin (legacy)
        </Button>
        <Button
          icon={<UserAddOutlined />}
          onClick={() =>
            navigate(
              "/iam/access-bindings?modal=cluster-admin&resource_type=cluster&resource_id=cluster_kacho_root&role_id=roles/admin",
            )
          }
          data-testid="cluster-admins-grant-via-binding"
        >
          Выдать через AccessBinding
        </Button>
        <Button
          icon={<ReloadOutlined />}
          onClick={() => qc.invalidateQueries({ queryKey: ["cluster-admins"] })}
          data-testid="cluster-admins-refresh"
        >
          Обновить
        </Button>
      </Space>

      {/* KAC item #5: cluster-admin grants теперь видимы и через AccessBindings
          page (resource_type=cluster). Legacy форма "Добавить admin" продолжает
          работать (POST /iam/v1/internal/cluster/admins), но новый unified
          flow — это POST /iam/v1/accessBindings с resource_type=cluster. */}
      <Alert
        type="info"
        showIcon
        message="Unified flow: cluster admin = AccessBinding"
        description={
          <>
            Cluster admin grants теперь видны и через страницу{" "}
            <a onClick={() => navigate("/iam/access-bindings")}>Access Bindings</a> (фильтр
            <code> resource_type=cluster, resource_id=cluster_kacho_root</code>). Создавать новый grant можно как через
            &quot;Добавить admin (legacy)&quot; (POST <code>/iam/v1/internal/cluster/admins</code>), так и через
            &quot;Выдать через AccessBinding&quot; — оба flow идемпотентны.
          </>
        }
        data-testid="cluster-admins-unified-flow-note"
      />

      {adminsQ.error && !isForbidden && (
        <Alert
          type="error"
          showIcon
          message="Не удалось загрузить список cluster admin'ов"
          description={adminsQ.error instanceof Error ? adminsQ.error.message : String(adminsQ.error)}
        />
      )}

      {adminsCount === 1 && (
        <Alert
          type="warning"
          showIcon
          message="В кластере единственный admin"
          description="Кнопка «Отозвать» отключена для последнего admin'а — иначе кластер останется без управления (D-6)."
        />
      )}

      <Table<ClusterAdminEntry>
        rowKey="cluster_admin_grant_id"
        size="small"
        loading={adminsQ.isFetching && admins.length === 0}
        dataSource={admins}
        columns={columns}
        pagination={false}
        locale={{ emptyText: "Cluster admin'ов нет." }}
      />

      <GrantAdminModal open={grantOpen} onClose={() => setGrantOpen(false)} />
    </Space>
  );
}
