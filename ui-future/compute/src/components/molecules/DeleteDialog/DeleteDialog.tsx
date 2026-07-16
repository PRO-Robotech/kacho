// DeleteDialog — confirm-modal с реальным DELETE и polling Operation
// прямо из диалога (Удалить-кнопка остаётся в loading-состоянии до op.done).
// Для ресурсов с RESTRICT-детьми (Network/Subnet) сбоку — дерево связанных
// ресурсов (DependencyTreePanel): видно, что подвязано и что блокирует удаление.

import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Button, Modal, Typography, Input, theme } from "antd";
import { DeleteOutlined, ReloadOutlined } from "@ant-design/icons";
import { ApiError, api } from "@/api/client";
import { extractOperationId } from "@/components/molecules/OperationDialog";
import { DopplerButton } from "@/components/molecules/DopplerButton";
import { useInvalidateResourceList, useOperation } from "@/lib/use-operation";
import { toast } from "@/lib/toast";
import { DependencyTreePanel } from "@/components/organisms/DependencyTreePanel";
import { hasDependencyResolver, loadDependents } from "@/lib/dependency-graph";

/**
 * High-risk ресурсы — удаление требует ввода имени для подтверждения
 * (необратимо + каскадные RESTRICT-зависимости / влияние на трафик).
 * Используется action-меню (DetailOverviewActions / RowActionsMenu) для
 * вычисления requireNameConfirm по spec.id.
 */
export const HIGH_RISK_DELETE = new Set(["networks", "route-tables", "security-groups"]);

export function requiresNameConfirm(specId: string): boolean {
  return HIGH_RISK_DELETE.has(specId);
}

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Полный API path: /vpc/v1/networks/<id>. */
  apiPath: string;
  /** ID ресурса в registry — для invalidate. */
  resourceId: string;
  /** Verbose имя для UI. */
  resourceLabel: string;
  name: string;
  /** Project ID для invalidate соответствующих list-query (и для дерева связей). */
  projectId?: string | null;
  /** Callback после успешного запуска (navigate на list etc.). */
  onSuccess?: () => void;
  /** Если true — требуется ввести имя ресурса для подтверждения. */
  requireNameConfirm?: boolean;
}

export function DeleteDialog({
  open,
  onOpenChange,
  apiPath,
  resourceId,
  resourceLabel,
  name,
  projectId,
  onSuccess,
  requireNameConfirm,
}: Props) {
  const { token } = theme.useToken();
  const [confirmText, setConfirmText] = useState("");
  const invalidate = useInvalidateResourceList();
  const [pendingOpId, setPendingOpId] = useState<string | null>(null);
  const { data: op } = useOperation(pendingOpId);

  const resourceUid = useMemo(() => apiPath.split("/").filter(Boolean).pop() ?? "", [apiPath]);
  const showDeps = hasDependencyResolver(resourceId);
  const depsQuery = useQuery({
    queryKey: ["delete-deps", resourceId, resourceUid, projectId ?? ""],
    queryFn: () => loadDependents(resourceId, { id: resourceUid, project_id: projectId ?? null }),
    enabled: open && showDeps && !!resourceUid,
    staleTime: 0,
    gcTime: 0,
  });

  const mutation = useMutation({
    mutationFn: () => api.delete(apiPath),
    onSuccess: (resp) => {
      const opId = extractOperationId(resp);
      if (opId) {
        setPendingOpId(opId);
      } else {
        invalidate(resourceId, projectId ?? null);
        onOpenChange(false);
        setConfirmText("");
        onSuccess?.();
      }
    },
    onError: (e) => {
      const m = e instanceof ApiError ? `${e.code}: ${e.message}` : (e as Error).message;
      toast.error(`Удалить ${resourceLabel} ${name}: ${m}`);
    },
  });

  useEffect(() => {
    if (!pendingOpId || !op?.done) return;
    if (op.error) {
      toast.error(`Удалить ${resourceLabel} ${name}: ${op.error.message ?? "ошибка"}`);
    } else {
      invalidate(resourceId, projectId ?? null);
      toast.success(`${resourceLabel} ${name} удалён`);
      onSuccess?.();
    }
    setPendingOpId(null);
    onOpenChange(false);
    setConfirmText("");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [op?.done, op?.error?.code]);

  const pending = mutation.isPending || pendingOpId !== null;
  const canConfirm = !requireNameConfirm || confirmText.trim() === name;

  const displayName = name || "(без имени)";

  const close = () => {
    if (pending) return;
    onOpenChange(false);
    setConfirmText("");
  };

  const left = (
    <div style={{ display: "flex", flexDirection: "column", gap: 18, flex: 1, minWidth: 280 }}>
      {/* Header: danger-икон + caps-метка типа + имя (ellipsis) + подзаголовок. */}
      <div style={{ display: "flex", gap: 14, alignItems: "flex-start" }}>
        <div
          style={{
            width: 46,
            height: 46,
            borderRadius: 13,
            flexShrink: 0,
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            fontSize: 21,
            color: token.colorError,
            background: "linear-gradient(135deg, rgba(229,72,77,0.20), rgba(229,72,77,0.06))",
            border: "1px solid rgba(229,72,77,0.26)",
            boxShadow: "0 1px 0 rgba(255,255,255,0.04) inset",
          }}
        >
          <DeleteOutlined />
        </div>
        <div style={{ minWidth: 0, flex: 1, paddingTop: 1 }}>
          <div
            style={{
              fontSize: 11,
              fontWeight: 600,
              letterSpacing: "0.06em",
              textTransform: "uppercase",
              color: token.colorError,
            }}
          >
            Удаление · {resourceLabel}
          </div>
          <Typography.Title
            level={5}
            ellipsis={{ tooltip: displayName }}
            style={{ margin: "3px 0 6px", fontWeight: 600, color: "var(--kc-text)" }}
          >
            {displayName}
          </Typography.Title>
          <Typography.Text type="secondary" style={{ fontSize: 13, lineHeight: 1.55, display: "block" }}>
            Ресурс будет удалён безвозвратно. Действие необратимо.
          </Typography.Text>
        </div>
      </div>

      {requireNameConfirm && (
        <div
          style={{
            display: "flex",
            flexDirection: "column",
            gap: 7,
            padding: 12,
            borderRadius: 10,
            background: token.colorFillQuaternary,
            border: `1px solid ${token.colorBorderSecondary}`,
          }}
        >
          <Typography.Text style={{ fontSize: 12.5, color: "var(--kc-text-secondary)", lineHeight: 1.5 }}>
            Подтвердите удаление — введите имя ресурса
          </Typography.Text>
          <Input
            value={confirmText}
            onChange={(e) => setConfirmText(e.target.value)}
            placeholder={name}
            status={confirmText && !canConfirm ? "error" : undefined}
            allowClear
            autoFocus
          />
        </div>
      )}
    </div>
  );

  return (
    <Modal
      open={open}
      // Стабильная ширина: 820 с панелью зависимостей (две колонки), иначе 560.
      // Зависит только от наличия resolver'а ресурса — не «прыгает» в рамках
      // одного ресурса.
      width={showDeps ? 820 : 560}
      onCancel={close}
      title={null}
      // KAC-246: крестик не нужен — есть кнопка «Отмена».
      closable={false}
      destroyOnClose
      footer={[
        showDeps ? (
          <Button
            key="refresh"
            icon={<ReloadOutlined />}
            onClick={() => depsQuery.refetch()}
            disabled={pending || depsQuery.isFetching}
            style={{ marginInlineEnd: "auto" }}
          >
            Обновить связи
          </Button>
        ) : null,
        <Button key="cancel" onClick={close} disabled={pending}>
          Отмена
        </Button>,
        <DopplerButton
          key="ok"
          danger
          type="primary"
          onClick={() => mutation.mutate()}
          pulsing={pending}
          disabled={!canConfirm}
        >
          Удалить
        </DopplerButton>,
      ]}
    >
      {showDeps ? (
        <div style={{ display: "flex", gap: 16, alignItems: "flex-start" }}>
          {left}
          <DependencyTreePanel
            nodes={depsQuery.data ?? []}
            loading={depsQuery.isLoading || depsQuery.isFetching}
            error={depsQuery.error ? (depsQuery.error as Error).message : null}
          />
        </div>
      ) : (
        left
      )}
    </Modal>
  );
}
