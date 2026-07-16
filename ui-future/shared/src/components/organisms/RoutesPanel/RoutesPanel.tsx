// RoutesPanel — static routes of a RouteTable rendered as ONE shared table in both modes.
//
// Read mode  : text cells, header action «Редактировать». 0 routes => dashed placeholder.
// Edit mode  : SAME table/columns — each value cell becomes a seamless borderless <Input>,
//              the (always-present) right column shows a per-row trash button, and a
//              full-width dashed «Добавить маршрут» footer row appears below the rows.
//              Header action becomes «Сохранить» + «Отменить».
//
// No-jump contract: table-layout:fixed + <colgroup> pin column widths identically in both
// modes; the trash column is always rendered (empty in read) so the column count never
// changes; every <tr> has a fixed height with vertical-align:middle so text-cells and
// input-cells occupy the exact same row height — nothing shifts when toggling edit.
//
// The SectionHeader title stays «Статические маршруты (N)» in BOTH modes.
// save() does a full-replace update (static_routes + update_mask) and starts an async
// Operation, exactly as before — no behaviour change to the save payload.

import { useState } from "react";
import { Button, Input, Space, Typography } from "antd";
import { DeleteOutlined, EditOutlined, PlusOutlined } from "@ant-design/icons";
import { useMutation, useQueryClient } from "@tanstack/react-query";

import { ApiError, api } from "@shared/api/client";
import { extractOperationId } from "@shared/components/molecules/OperationDialog";
import { SectionHeader } from "@shared/components/molecules/SectionHeader";
import { REGISTRY } from "@shared/lib/resource-registry";
import { operationStore } from "@shared/lib/use-operation-store";
import { toast } from "@shared/lib/toast";

export interface StaticRoute {
  destination_prefix?: string;
  next_hop_address?: string;
  gateway_id?: string;
}

interface RoutesPanelProps {
  routeTableId: string;
  projectId: string | null;
  routes: StaticRoute[];
}

interface DraftRoute {
  destination_prefix: string;
  next_hop_address: string;
}

const MONO_FONT = "ui-monospace, monospace";
const ROW_H = 41; // фиксированная высота строки в обоих режимах — нет вертикального прыжка

const rtSpec = REGISTRY["route-tables"];

// Inline-инпут, визуально неотличимый от текстовой ячейки read-режима
// (без рамки, тот же моноширинный шрифт/размер, нулевые отступы, центрирован
// по высоте строки) — переключение в edit не сдвигает контент.
const cellInputStyle: React.CSSProperties = {
  width: "100%",
  fontFamily: MONO_FONT,
  fontSize: 12,
  padding: 0,
  height: ROW_H - 2,
  lineHeight: `${ROW_H - 2}px`,
};

export function RoutesPanel({ routeTableId, projectId, routes }: RoutesPanelProps) {
  const qc = useQueryClient();
  const [drafts, setDrafts] = useState<DraftRoute[] | null>(null);

  const editing = drafts !== null;

  const mutation = useMutation({
    mutationFn: async () => {
      const next = (drafts ?? [])
        .map((r) => ({
          destination_prefix: r.destination_prefix.trim(),
          next_hop_address: r.next_hop_address.trim(),
        }))
        .filter((r) => r.destination_prefix !== "" && r.next_hop_address !== "");

      const res = await api.update(`${rtSpec.apiPath}/${routeTableId}`, {
        static_routes: next,
        // FieldMask JSON-пути — camelCase (googleapis FieldMask mapping);
        // protojson на бэкенде отвергает snake_case "static_routes".
        update_mask: "staticRoutes",
      });

      const operationId = extractOperationId(res);
      if (operationId) {
        operationStore.start({
          id: operationId,
          title: `Сохранение маршрутов (${next.length})`,
          resourceId: rtSpec.id,
          projectId,
        });
      }

      qc.invalidateQueries({ queryKey: [rtSpec.id] });
    },
  });

  function startEdit() {
    if (routes.length === 0) {
      setDrafts([{ destination_prefix: "", next_hop_address: "" }]);
      return;
    }
    setDrafts(
      routes.map((r) => ({
        destination_prefix: r.destination_prefix ?? "",
        next_hop_address: r.next_hop_address ?? r.gateway_id ?? "",
      })),
    );
  }

  function cancel() {
    setDrafts(null);
  }

  function addRow() {
    setDrafts((prev) => [...(prev ?? []), { destination_prefix: "", next_hop_address: "" }]);
  }

  function removeRow(index: number) {
    setDrafts((prev) => (prev ?? []).filter((_, i) => i !== index));
  }

  function setRow(index: number, patch: Partial<DraftRoute>) {
    setDrafts((prev) => (prev ?? []).map((r, i) => (i === index ? { ...r, ...patch } : r)));
  }

  async function save() {
    try {
      await mutation.mutateAsync();
      cancel();
    } catch (err) {
      const m = err instanceof ApiError ? `${err.code}: ${err.message}` : (err as Error).message;
      toast.error(`Статические маршруты: ${m}`);
    }
  }

  const count = editing ? (drafts?.length ?? 0) : routes.length;

  const headerRight = editing ? (
    <Space>
      <Button type="primary" loading={mutation.isPending} onClick={save}>
        Сохранить
      </Button>
      <Button disabled={mutation.isPending} onClick={cancel}>
        Отменить
      </Button>
    </Space>
  ) : (
    <Button icon={<EditOutlined />} onClick={startEdit}>
      Редактировать
    </Button>
  );

  const showTable = editing || routes.length > 0;

  return (
    <div style={{ marginTop: 24, maxWidth: 760 }}>
      <SectionHeader
        eyebrow="Список"
        title={
          <span>
            Статические маршруты <Typography.Text type="secondary">({count})</Typography.Text>
          </span>
        }
        right={headerRight}
      />

      {showTable ? (
        <div
          style={{
            border: "1px solid var(--kc-border)",
            borderRadius: 8,
            overflow: "hidden",
            background: "var(--kc-page)",
          }}
        >
          <table className="w-full text-sm kc-grid-table" style={{ tableLayout: "fixed" }}>
            {/* Фиксированные ширины колонок — идентичны в read и edit, без горизонтального прыжка. */}
            <colgroup>
              <col style={{ width: "calc((100% - 48px) / 2)" }} />
              <col style={{ width: "calc((100% - 48px) / 2)" }} />
              <col style={{ width: 48 }} />
            </colgroup>
            <thead>
              <tr style={{ background: "var(--kc-container)" }}>
                <th
                  className="text-left"
                  style={{
                    padding: "7px 12px",
                    fontSize: 11,
                    fontWeight: 600,
                    letterSpacing: "0.02em",
                    color: "var(--kc-text-tertiary)",
                  }}
                >
                  Префикс назначения
                </th>
                <th
                  className="text-left"
                  style={{
                    padding: "7px 12px",
                    fontSize: 11,
                    fontWeight: 600,
                    letterSpacing: "0.02em",
                    color: "var(--kc-text-tertiary)",
                  }}
                >
                  Следующий узел
                </th>
                {/* колонка действий присутствует всегда (пустая в read) → число колонок не меняется */}
                <th style={{ padding: "7px 4px" }} />
              </tr>
            </thead>
            <tbody>
              {editing
                ? (drafts ?? []).map((row, i) => (
                    <tr
                      key={i}
                      className="kc-kv-row"
                      style={{ height: ROW_H, borderTop: "1px solid var(--kc-border-secondary)" }}
                    >
                      <td className="px-3 font-mono text-xs" style={{ verticalAlign: "middle" }}>
                        <Input
                          variant="borderless"
                          placeholder="10.0.0.0/24"
                          value={row.destination_prefix}
                          onChange={(e) => setRow(i, { destination_prefix: e.target.value })}
                          style={cellInputStyle}
                        />
                      </td>
                      <td className="px-3 font-mono text-xs" style={{ verticalAlign: "middle" }}>
                        <Input
                          variant="borderless"
                          placeholder="10.0.0.1"
                          value={row.next_hop_address}
                          onChange={(e) => setRow(i, { next_hop_address: e.target.value })}
                          style={cellInputStyle}
                        />
                      </td>
                      <td className="px-1 text-center" style={{ verticalAlign: "middle" }}>
                        <Button
                          type="text"
                          danger
                          size="small"
                          icon={<DeleteOutlined />}
                          aria-label="Удалить маршрут"
                          onClick={() => removeRow(i)}
                        />
                      </td>
                    </tr>
                  ))
                : routes.map((r, i) => (
                    <tr
                      key={i}
                      className="kc-kv-row"
                      style={{ height: ROW_H, borderTop: "1px solid var(--kc-border-secondary)" }}
                    >
                      <td className="px-3 font-mono text-xs" style={{ verticalAlign: "middle" }}>
                        {r.destination_prefix}
                      </td>
                      <td className="px-3 font-mono text-xs" style={{ verticalAlign: "middle" }}>
                        {r.next_hop_address || r.gateway_id}
                      </td>
                      {/* пустая ячейка резервирует колонку действий */}
                      <td className="px-1" />
                    </tr>
                  ))}
            </tbody>
            {editing && (
              <tfoot>
                <tr style={{ borderTop: "1px solid var(--kc-border-secondary)" }}>
                  <td style={{ padding: "8px 12px" }} colSpan={3}>
                    <Button type="dashed" block icon={<PlusOutlined />} onClick={addRow}>
                      Добавить маршрут
                    </Button>
                  </td>
                </tr>
              </tfoot>
            )}
          </table>
        </div>
      ) : (
        <div
          style={{
            border: "1px dashed var(--kc-border)",
            borderRadius: 8,
            padding: "24px 12px",
            textAlign: "center",
            fontSize: 13,
            color: "var(--kc-text-tertiary)",
          }}
        >
          Статических маршрутов нет — нажмите «Редактировать», чтобы добавить.
        </div>
      )}
    </div>
  );
}
