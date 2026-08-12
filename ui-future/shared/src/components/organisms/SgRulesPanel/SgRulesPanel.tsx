// SgRulesPanel — управление правилами Security Group (KAC-239).
//
// Один таб «Правила» (INGRESS+EGRESS вместе; направление — первый столбец).
// Режимы: list (таблица: чекбоксы + per-row ⋮ Редактировать/Удалить + bulk-delete)
// ↔ edit (редактор правил через SgRulesEditor; direction выбирается в самом
// правиле). Каждая операция — UpdateRules по стабильному id:
//   • add    → { addition_rule_specs: [...] }
//   • edit   → { deletion_rule_ids: [id], addition_rule_specs: [spec] }
//   • delete → { deletion_rule_ids: [...] }

import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Button, Checkbox, Dropdown, Modal, Tag, Typography } from "antd";
import { MoreOutlined, EditOutlined, DeleteOutlined, PlusOutlined, ExclamationCircleFilled } from "@ant-design/icons";
import { ApiError, api } from "@shared/api/client";
import { extractOperationId } from "@shared/components/molecules/OperationDialog";
import { FormShell } from "@shared/components/organisms/form/FormShell";
import { FormFooter } from "@shared/components/organisms/form/FormFooter";
import { ResourceTable } from "@shared/components/organisms/ResourceTable";
import { HeaderSlotPortal } from "@shared/components/organisms/DetailShell";
import { RuleBody, emptyRule, type RuleExt } from "@shared/components/organisms/form/SgRulesEditor";
import { hasProtocolNumber, REGISTRY, sanitizeSgRule } from "@shared/lib/resource-registry";
import { operationStore } from "@shared/lib/use-operation-store";
import { toast } from "@shared/lib/toast";

export interface SgRule {
  id?: string;
  direction?: string;
  description?: string;
  protocol_name?: string;
  // int64 on the wire → a JSON string when it comes from the server.
  protocol_number?: number | string;
  ports?: { from_port?: number | string; to_port?: number | string };
  cidr_blocks?: { v4_cidr_blocks?: string[]; v6_cidr_blocks?: string[] };
  security_group_id?: string;
  predefined_target?: string;
  [k: string]: unknown;
}

interface Props {
  sgId: string;
  projectId: string | null;
  /** Все правила SG (из detail) — оба направления в одной таблице. */
  rules: SgRule[];
  /** KAC-243 (scenario 18): network_id редактируемой SG. SG-target picker в
   *  редакторе правил показывает только SG из этой же сети. */
  networkId?: string;
}

function dirOf(r: SgRule): "INGRESS" | "EGRESS" {
  return (r.direction ?? "INGRESS").toUpperCase() === "EGRESS" ? "EGRESS" : "INGRESS";
}
function protoLabel(r: SgRule): string {
  if (r.protocol_name) return r.protocol_name;
  if (hasProtocolNumber(r.protocol_number)) return `proto ${r.protocol_number}`;
  return "Any";
}
function portsLabel(r: SgRule): string {
  if (!r.ports) return "—";
  const f = r.ports.from_port;
  const t = r.ports.to_port;
  if (f == null && t == null) return "—";
  if (f === t || t == null) return String(f);
  return `${f}–${t}`;
}
function targetParts(r: SgRule): { kind: string; value: string } {
  if (r.cidr_blocks) {
    const v4 = r.cidr_blocks.v4_cidr_blocks ?? [];
    const v6 = r.cidr_blocks.v6_cidr_blocks ?? [];
    return { kind: "CIDR", value: [...v4, ...v6].join(", ") || "—" };
  }
  if (r.security_group_id) return { kind: "SG", value: r.security_group_id };
  if (r.predefined_target) return { kind: "Predefined", value: r.predefined_target };
  return { kind: "—", value: "—" };
}

export function SgRulesPanel({ sgId, projectId, rules, networkId }: Props) {
  const sgSpec = REGISTRY["security-groups"];
  const qc = useQueryClient();

  const [editObj, setEditObj] = useState<RuleExt | null>(null);
  const [editingId, setEditingId] = useState<string | null>(null); // null = добавление
  const [selected, setSelected] = useState<Set<string>>(new Set());

  const mutation = useMutation({
    mutationFn: (payload: unknown) => api.update(`${sgSpec.apiPath}/${sgId}/rules`, payload),
  });
  const refresh = () => qc.invalidateQueries({ queryKey: [sgSpec.id] });

  const runOp = async (payload: { deletion_rule_ids?: string[]; addition_rule_specs?: unknown[] }, opTitle: string) => {
    try {
      const resp = await mutation.mutateAsync(payload);
      const opId = extractOperationId(resp);
      if (opId) operationStore.start({ id: opId, title: opTitle, resourceId: sgSpec.id, projectId });
      void refresh();
    } catch (err) {
      const m = err instanceof ApiError ? `${err.code}: ${err.message}` : (err as Error).message;
      toast.error(`Правило группы безопасности: ${m}`);
    }
  };

  // Выбор — только правила с id (после backfill id есть у всех).
  const selectableIds = rules.map((r) => r.id).filter(Boolean) as string[];
  const allSelected = selectableIds.length > 0 && selectableIds.every((id) => selected.has(id));
  const someSelected = selectableIds.some((id) => selected.has(id));
  const selCount = selectableIds.filter((id) => selected.has(id)).length;

  const toggleOne = (id: string, on: boolean) =>
    setSelected((prev) => {
      const next = new Set(prev);
      if (on) next.add(id);
      else next.delete(id);
      return next;
    });
  const toggleAll = (on: boolean) => setSelected(on ? new Set(selectableIds) : new Set());

  const confirmDeleteSelected = () => {
    const ids = selectableIds.filter((id) => selected.has(id));
    if (ids.length === 0) return;
    Modal.confirm({
      title: `Удалить выбранные правила (${ids.length})`,
      icon: <ExclamationCircleFilled />,
      content: "Действие необратимо.",
      okText: "Удалить",
      okButtonProps: { danger: true },
      cancelText: "Отмена",
      onOk: async () => {
        await runOp({ deletion_rule_ids: ids }, `Удаление правил группы безопасности (${ids.length})`);
        setSelected(new Set());
      },
    });
  };

  const confirmDelete = (r: SgRule) => {
    if (!r.id) return;
    Modal.confirm({
      title: "Удалить правило",
      icon: <ExclamationCircleFilled />,
      content: `${dirOf(r)} · ${protoLabel(r)} · ${targetParts(r).value}`,
      okText: "Удалить правило",
      okButtonProps: { danger: true },
      cancelText: "Отмена",
      onOk: () => runOp({ deletion_rule_ids: [r.id as string] }, "Удаление правила группы безопасности"),
    });
  };

  const startAdd = () => {
    setEditingId(null);
    setEditObj(emptyRule());
  };
  const startEdit = (r: SgRule) => {
    setEditingId(r.id ?? null);
    setEditObj({ ...(r as RuleExt) });
  };
  const cancelEdit = () => {
    setEditObj(null);
    setEditingId(null);
  };

  const saveEdit = async () => {
    if (!editObj) {
      cancelEdit();
      return;
    }
    // Одно правило: direction/протокол/порты/источник — из самой формы (RuleBody).
    const clean = sanitizeSgRule({ ...(editObj as Record<string, unknown>) });
    delete clean.id;
    const payload: { deletion_rule_ids?: string[]; addition_rule_specs?: unknown[] } = {
      addition_rule_specs: [clean],
    };
    if (editingId) payload.deletion_rule_ids = [editingId]; // edit = delete+add
    cancelEdit();
    await runOp(
      payload,
      editingId ? "Изменение правила группы безопасности" : "Добавление правила группы безопасности",
    );
  };

  // ── режим редактора ОДНОГО правила — плоская форма (RuleBody), без Collapse ──
  if (editObj) {
    // Та же оболочка формы, что у всех форм консоли: единая ширина, шапка
    // «действие + тип» с иконкой ресурса, подвал с кнопками. Прежде здесь была
    // своя разметка — отсюда и другая ширина, и другие отступы, и кнопки не на
    // общем месте.
    return (
      <FormShell
        specId="security-groups"
        mode={editingId ? "edit" : "create"}
        singular="правило"
        title={editingId ? "Правило" : "Правило"}
      >
        <RuleBody rule={editObj} onChange={setEditObj} editingNetworkId={networkId || undefined} />
        <FormFooter
          submitLabel={editingId ? "Сохранить" : "Добавить правило"}
          submitting={mutation.isPending}
          onSubmit={saveEdit}
          onCancel={cancelEdit}
        />
      </FormShell>
    );
  }

  // ── режим списка ──
  return (
    <div>
      {/* Шапку «Правила» показывает зона-3 (название таба) — свой SectionHeader
          здесь убран (дубль). Действия Добавить/Удалить поднимаем в слот шапки
          таба (как фильтры у related-таблиц). */}
      <HeaderSlotPortal>
        {/* «Выбрать все» — рядом с действиями в шапке: заголовок колонки общей
            таблицы принимает только текст, и чекбокс в нём не поставить. Место
            в шапке ему подходит больше: там же живут действия над выбранным. */}
        <Checkbox
          checked={allSelected}
          indeterminate={someSelected && !allSelected}
          onChange={(e) => toggleAll(e.target.checked)}
          disabled={selectableIds.length === 0}
        >
          Выбрать все
        </Checkbox>
        <Button type="primary" icon={<PlusOutlined />} onClick={startAdd}>
          Добавить правило
        </Button>
        <Button danger icon={<DeleteOutlined />} disabled={!someSelected} onClick={confirmDeleteSelected}>
          Удалить{selCount > 0 ? ` (${selCount})` : ""}
        </Button>
      </HeaderSlotPortal>
      {rules.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border p-8 text-center text-sm text-muted-foreground">
          Правил нет — трафик блокируется (default-deny).
        </div>
      ) : (
        // Таблица правил — та же `ResourceTable`, что у всех списков консоли.
        // Прежде здесь стояла СВОЯ разметка с собственными классами: она
        // отличалась от остальных таблиц шапкой, отступами и поведением
        // сортировки, то есть один и тот же предмет — список строк ресурса —
        // выглядел на этой вкладке иначе, чем везде.
        <ResourceTable
          rows={rules as unknown as Record<string, unknown>[]}
          rowKey={(r) => (r.id as string) ?? String(rules.indexOf(r as unknown as SgRule))}
          columns={[
            {
              header: "",
              cell: (row) => {
                const r = row as unknown as SgRule;
                return (
                  <Checkbox
                    checked={!!r.id && selected.has(r.id)}
                    disabled={!r.id}
                    onChange={(e) => r.id && toggleOne(r.id, e.target.checked)}
                  />
                );
              },
            },
            {
              header: "Направление",
              cell: (row) => {
                const dir = dirOf(row as unknown as SgRule);
                return (
                  <Tag color={dir === "INGRESS" ? "green" : "blue"}>
                    {dir === "INGRESS" ? "Входящий" : "Исходящий"}
                  </Tag>
                );
              },
            },
            { header: "Протокол", cell: (row) => protoLabel(row as unknown as SgRule) },
            { header: "Диапазон портов", cell: (row) => portsLabel(row as unknown as SgRule) },
            {
              header: "Тип источника",
              cell: (row) => (
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                  {targetParts(row as unknown as SgRule).kind}
                </Typography.Text>
              ),
            },
            {
              header: "Источник",
              className: "font-mono text-xs",
              cell: (row) => targetParts(row as unknown as SgRule).value,
            },
            {
              header: "Описание",
              cell: (row) => (row as unknown as SgRule).description || "—",
            },
            {
              header: "",
              className: "text-right whitespace-nowrap",
              cell: (row) => {
                const r = row as unknown as SgRule;
                return (
                  <Dropdown
                    trigger={["click"]}
                    placement="bottomRight"
                    menu={{
                      items: [
                        { key: "edit", icon: <EditOutlined />, label: "Редактировать", onClick: () => startEdit(r) },
                        { type: "divider" as const },
                        {
                          key: "delete",
                          icon: <DeleteOutlined />,
                          label: "Удалить",
                          danger: true,
                          disabled: !r.id,
                          onClick: () => confirmDelete(r),
                        },
                      ],
                    }}
                  >
                    <Button type="text" size="small" icon={<MoreOutlined />} aria-label="Действия" />
                  </Dropdown>
                );
              },
            },
          ]}
        />
      )}
    </div>
  );
}
