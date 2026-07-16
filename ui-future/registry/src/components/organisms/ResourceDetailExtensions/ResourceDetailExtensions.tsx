// resource-detail-extensions — реестр доменных расширений detail-страницы
// registry-remote.
//
// ResourceShell остаётся generic (Обзор / связанные / Операции / JSON + формы-
// панели). Доменно-специфичные строки Обзора и header-действия конкретного
// ресурса подключаются здесь по spec.id. Для Registry: реестр — endpoint /
// число репозиториев / статус + header-действие «Управление доступом»
// (навигация в IAM-remote к созданию AccessBinding на проекте реестра).

import { type ReactNode } from "react";
import { Button, Typography } from "antd";
import { SafetyCertificateOutlined } from "@ant-design/icons";

import type { DetailTab } from "@/components/organisms/DetailShell";
import { StatusBadge } from "@/components/atoms/StatusBadge";
import { getByPath } from "@/lib/resource-registry";

export interface DescItem {
  label: string;
  value: ReactNode;
}

export interface DetailExtCtx {
  data: Record<string, unknown>;
  projectId: string | null;
  /** Базовый URL detail-страницы ресурса (без хвостов /edit, /json, /<tab>). */
  detailBase: string;
  navigate: (to: string) => void;
}

export interface DetailExtension {
  overviewExtra?: (ctx: DetailExtCtx) => DescItem[];
  /** Контент под Обзор-таблицей (отдельные секции-таблицы с подписью). */
  overviewBelow?: (ctx: DetailExtCtx) => ReactNode;
  headerActions?: (ctx: DetailExtCtx) => ReactNode;
  extraTabs?: (ctx: DetailExtCtx) => DetailTab[];
  hideOperations?: boolean;
  title?: (data: Record<string, unknown>) => string | undefined;
}

// ─────────────────────────── helpers ───────────────────────────

const dash = <Typography.Text type="secondary">—</Typography.Text>;

function code(v: unknown): ReactNode {
  const s = v == null ? "" : String(v);
  return s ? (
    <Typography.Text code copyable style={{ fontSize: 12 }}>
      {s}
    </Typography.Text>
  ) : (
    dash
  );
}

function txt(v: unknown): ReactNode {
  const s = v == null ? "" : String(v);
  return s ? s : dash;
}

// ─────────────────────────── реестр ───────────────────────────

export const DETAIL_EXTENSIONS: Record<string, DetailExtension> = {
  registries: {
    // Доменные строки Обзора реестра: endpoint для docker login/push,
    // число репозиториев (растёт с push) и статус.
    overviewExtra: ({ data }) => [
      { label: "Endpoint", value: code(getByPath<string>(data, "endpoint")) },
      { label: "Репозиториев", value: txt(getByPath<number>(data, "repository_count") ?? 0) },
      { label: "Статус", value: <StatusBadge state={getByPath<string>(data, "status")} /> },
    ],
    // «Управление доступом» — доступ к реестру = registry-scoped Role, привязанная
    // на ПРОЕКТЕ реестра (уровни scope — только CLUSTER/ACCOUNT/PROJECT, отдельного
    // per-registry-object scope нет). Кнопка ведёт в IAM-remote к созданию
    // AccessBinding на проекте; форму IAM cross-remote НЕ импортируем.
    headerActions: ({ projectId, navigate }) =>
      projectId ? (
        <Button
          icon={<SafetyCertificateOutlined />}
          onClick={() => navigate(`/projects/${projectId}/iam/access-bindings/create`)}
        >
          Управление доступом
        </Button>
      ) : null,
  },
};

export function detailExtension(specId: string): DetailExtension | undefined {
  return DETAIL_EXTENSIONS[specId];
}
