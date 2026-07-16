// DependencyTreePanel — боковая панель в модалке удаления: ДЕРЕВО связанных
// ресурсов с сохранённой иерархией (Network → Подсети → {Адреса, NIC} и т.д.).
// На каждом уровне дети сгруппированы по типу: ветка-группа = бейдж (иконка+тип)
// + счётчик, внутри — только имена (ссылки в новой вкладке, чтобы не терять
// модалку). Блокирующие удаление помечены ⚠. Источник — lib/dependency-graph.

import { useMemo } from "react";
import { Link } from "react-router-dom";
import { Alert, Empty, Spin, Typography, Tag, Tree, theme } from "antd";
import { WarningFilled } from "@ant-design/icons";
import type { DataNode } from "antd/es/tree";
import { REGISTRY } from "@shared/lib/resource-registry";
import { ResourceIcon } from "@shared/components/organisms/form/ResourceIcon";
import { blockingNodes, type DepNode } from "@shared/lib/dependency-graph";

type Token = ReturnType<typeof theme.useToken>["token"];

// Label группы в дереве. Для addresses — точнее: под подсетью это ВНУТРЕННИЕ
// (приватные) адреса (резолвер фильтрует по internal_*.subnet_id), а registry-
// label «Публичные IP-адреса» здесь ввёл бы в заблуждение.
function groupLabel(resourceId: string): string {
  if (resourceId === "addresses") return "Внутренние адреса";
  return REGISTRY[resourceId]?.plural ?? REGISTRY[resourceId]?.singular ?? resourceId;
}

function CountPill({ n, token }: { n: number; token: Token }) {
  return (
    <span
      style={{
        fontSize: 11,
        minWidth: 18,
        height: 17,
        padding: "0 6px",
        borderRadius: 9,
        display: "inline-flex",
        alignItems: "center",
        justifyContent: "center",
        background: token.colorFillSecondary,
        color: token.colorTextSecondary,
        fontWeight: 600,
        flexShrink: 0,
      }}
    >
      {n}
    </span>
  );
}

function groupTitle(resourceId: string, count: number, anyBlocks: boolean, token: Token) {
  return (
    <span style={{ display: "inline-flex", alignItems: "center", gap: 8, whiteSpace: "nowrap" }}>
      <Tag
        color={anyBlocks ? "warning" : "default"}
        style={{ margin: 0, fontSize: 12, display: "inline-flex", alignItems: "center", gap: 5, paddingInline: 8 }}
      >
        <span style={{ display: "inline-flex", fontSize: 13, lineHeight: 0 }}>
          <ResourceIcon specId={resourceId} />
        </span>
        {groupLabel(resourceId)}
      </Tag>
      <CountPill n={count} token={token} />
    </span>
  );
}

function nameTitle(n: DepNode, token: Token) {
  const href = n.projectId ? `/projects/${n.projectId}/${n.routeSegment}/${n.id}` : null;
  return (
    <span style={{ display: "inline-flex", alignItems: "center", gap: 6, fontSize: 12, whiteSpace: "nowrap" }}>
      {n.blocks && (
        <WarningFilled
          style={{ color: token.colorWarning, fontSize: 10, flexShrink: 0 }}
          title="Блокирует удаление — удалите первым"
        />
      )}
      {href ? (
        <Link
          to={href}
          target="_blank"
          rel="noopener noreferrer"
          style={{ color: token.colorText }}
          onClick={(e) => e.stopPropagation()}
          title="Открыть в новой вкладке"
        >
          {n.name || n.id}
        </Link>
      ) : (
        <span style={{ color: token.colorText }}>{n.name || n.id}</span>
      )}
    </span>
  );
}

// Сгруппировать массив узлов по resourceId в ветки-группы; внутри — узлы-имена,
// которые рекурсивно получают свои сгруппированные ветки (иерархия сохраняется).
function buildGrouped(nodes: DepNode[], token: Token): DataNode[] {
  const order: string[] = [];
  const map = new Map<string, DepNode[]>();
  for (const n of nodes) {
    if (!map.has(n.resourceId)) {
      map.set(n.resourceId, []);
      order.push(n.resourceId);
    }
    map.get(n.resourceId)!.push(n);
  }
  return order.map((rid) => {
    const items = map.get(rid)!;
    const anyBlocks = items.some((i) => i.blocks);
    return {
      key: `grp-${rid}-${items[0].key}`,
      selectable: false,
      title: groupTitle(rid, items.length, anyBlocks, token),
      children: items.map<DataNode>((n) => ({
        key: n.key,
        selectable: false,
        title: nameTitle(n, token),
        children: n.children?.length ? buildGrouped(n.children, token) : undefined,
      })),
    };
  });
}

interface Props {
  nodes: DepNode[];
  loading: boolean;
  error?: string | null;
}

export function DependencyTreePanel({ nodes, loading, error }: Props) {
  const { token } = theme.useToken();
  const treeData = useMemo(() => buildGrouped(nodes, token), [nodes, token]);
  const blockers = useMemo(() => blockingNodes(nodes), [nodes]);

  return (
    <div
      style={{
        borderLeft: `1px solid ${token.colorBorderSecondary}`,
        paddingLeft: 16,
        minWidth: 340,
        maxHeight: 440,
        // overflow на внешнем убран — header и blocker-alert закреплены сверху,
        // скроллится только дерево (см. ниже). KAC-246.
        display: "flex",
        flexDirection: "column",
        gap: 10,
      }}
    >
      <Typography.Text strong style={{ fontSize: 12, flexShrink: 0 }}>
        Связанные ресурсы
      </Typography.Text>

      {loading ? (
        <div style={{ padding: "16px 0", textAlign: "center" }}>
          <Spin size="small" />
        </div>
      ) : error ? (
        <Alert type="error" showIcon message="Не удалось загрузить связи" description={error} />
      ) : treeData.length === 0 ? (
        <Empty
          image={Empty.PRESENTED_IMAGE_SIMPLE}
          description={
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              Зависимых ресурсов нет — можно удалять.
            </Typography.Text>
          }
        />
      ) : (
        <>
          {/* Blocker-alert закреплён сверху (вне скролла дерева). */}
          {blockers.length > 0 && (
            <Alert
              type="warning"
              showIcon
              style={{ fontSize: 12, flexShrink: 0 }}
              message={
                <span style={{ fontSize: 12 }}>
                  Сначала удалите помеченные ⚠ ресурсы — иначе удаление будет отклонено.
                </span>
              }
            />
          )}
          {/* Скроллится только дерево. */}
          <div style={{ overflowY: "auto", minHeight: 0, flex: 1 }}>
            <Tree
              treeData={treeData}
              defaultExpandAll
              selectable={false}
              showLine={{ showLeafIcon: false }}
              style={{ background: "transparent", fontSize: 12 }}
            />
          </div>
        </>
      )}
    </div>
  );
}
