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
//
// save() does a full-replace update (static_routes + update_mask) and starts an async
// Operation. ЗАМЕНА ВСЕГО СПИСКА — несущее свойство, и из него следуют два
// требования, каждое со своей пробой:
//
//   * черновик обязан нести ВСЕ поля `StaticRoute` контракта, в том числе те,
//     которых редактор не показывает (`labels`): отсутствующее поле стирается у
//     всех строк разом — `RoutesPanel.contract.test.ts`;
//   * неполная строка НЕ отбрасывается перед отправкой — она называется, а
//     «Сохранить» выключается: отброшенная строка при полной замене означает
//     удалённый маршрут, о котором оператору отвечают успехом — `routeGaps`.

import { useState } from "react";
import { Button, Input, Select, Space, Typography } from "antd";
import { DeleteOutlined, EditOutlined, PlusOutlined } from "@ant-design/icons";
import { useMutation, useQueryClient } from "@tanstack/react-query";

import { api } from "@shared/api/client";
import { extractOperationId } from "@shared/components/molecules/OperationDialog";
import { SectionHeader } from "@shared/components/molecules/SectionHeader";
import { REGISTRY } from "@shared/lib/resource-registry";
import { RefSelect } from "@shared/components/organisms/form/RefSelect";
import type { SetReplacementDraft } from "@shared/lib/set-replacement-draft";
import { operationStore } from "@shared/lib/use-operation-store";
import { toast } from "@shared/lib/toast";
import { errorText } from "@shared/lib/error-presentation";

/**
 * Место полной замены набора. Состав обоих типов, через которые проходит
 * маршрут, сверяется с `StaticRoute` контракта гейтом
 * `test/set-replacement-draft-composition`.
 */
export const STATIC_ROUTES_REPLACEMENT: SetReplacementDraft = {
  field: "static_routes",
  contract: "kacho/cloud/vpc/v1/route_table.proto",
  message: "StaticRoute",
  drafts: ["StaticRoute", "DraftRoute"],
};

export interface StaticRoute {
  destination_prefix?: string;
  next_hop_address?: string;
  gateway_id?: string;
  /**
   * Метки маршрута. Редактор их не показывает и не правит, но ОБЯЗАН пронести:
   * сохранение заменяет весь список, поэтому поле, которого нет в черновике,
   * стирается у всех строк разом — включая нетронутые. Состав черновика против
   * состава контракта держит `RoutesPanel.contract.test.ts`.
   */
  labels?: Record<string, string>;
}

interface RoutesPanelProps {
  routeTableId: string;
  projectId: string | null;
  routes: StaticRoute[];
}

/** Ветвь `StaticRoute.next_hop`, выбранная строкой. Поле ФОРМЫ, не контракта. */
export type RouteNextHopKind = "address" | "gateway";

export interface DraftRoute {
  destination_prefix: string;
  next_hop_address: string;
  /**
   * Выбранная ветвь `StaticRoute.next_hop` (`next_hop_address` XOR `gateway_id`).
   *
   * Прежде ветви у черновика не было: редактор шлюзы не авторил, а строка со
   * шлюзом лишь ПЕРЕЖИВАЛА сохранение (оно заменяет весь список). Сменить ветвь
   * можно было только набрав адрес поверх, и обратного пути не существовало —
   * снятый шлюз не возвращался ничем, кроме пересоздания таблицы (#375). Ветвь
   * выбирается явно, и выбор — то, что делает пользователь, а не побочный
   * эффект набора.
   */
  _kind?: RouteNextHopKind;
  /** Ветвь шлюза. Пустая строка означает «шлюз выбран, но не назван». */
  gateway_id?: string;
  /** Проносится нетронутым по той же причине, что и ветвь шлюза. */
  labels?: Record<string, string>;
}

/** Ветвь строки: выбор, если он назван, иначе — та, что заполнена. */
function kindOf(r: DraftRoute): RouteNextHopKind {
  if (r._kind) return r._kind;
  return r.gateway_id ? "gateway" : "address";
}

/** Чего не хватает в строке, которую оператор ещё не дописал. */
export interface RouteGap {
  /** Номер строки, как её видит оператор, — с единицы. */
  row: number;
  /** Названия недостающего — словами подписей столбцов таблицы. */
  missing: string[];
}

const MISSING_DESTINATION = "префикс назначения";
const MISSING_NEXT_HOP = "следующий узел";

// Экспортированы для тестов.
export function draftsFromRoutes(routes: StaticRoute[]): DraftRoute[] {
  return routes.map((r) => ({
    destination_prefix: r.destination_prefix ?? "",
    next_hop_address: r.next_hop_address ?? "",
    _kind: r.gateway_id ? ("gateway" as const) : ("address" as const),
    ...(r.gateway_id ? { gateway_id: r.gateway_id } : {}),
    ...(r.labels ? { labels: r.labels } : {}),
  }));
}

export function routesFromDrafts(drafts: DraftRoute[]): StaticRoute[] {
  return drafts.map((r) => {
    const destination_prefix = r.destination_prefix.trim();
    const labels = r.labels ? { labels: r.labels } : {};
    // Ровно одна ветвь на строку, и её называет ВЫБОР строки: группа
    // взаимоисключающая, а две заполненные ветви — отказ края.
    if (kindOf(r) === "gateway") {
      const gateway = (r.gateway_id ?? "").trim();
      if (gateway) return { destination_prefix, gateway_id: gateway, ...labels };
      return { destination_prefix, ...labels };
    }
    const address = r.next_hop_address.trim();
    if (address) return { destination_prefix, next_hop_address: address, ...labels };
    return { destination_prefix, ...labels };
  });
}

/**
 * Строки, которые край не примет, — названные, а НЕ отброшенные.
 *
 * Прежде такие строки отсеивались перед отправкой. Сохранение заменяет весь
 * список, поэтому «не отправлена» и «удалена» — одно и то же: оператор, стерев
 * адрес существующего маршрута, чтобы набрать его заново, терял маршрут целиком
 * и получал сообщение об успехе. Отбор при этом не оберегал вызов от отказа —
 * он ПОДМЕНЯЛ точный отказ края (`static_routes[i]: next_hop_address or
 * gateway_id is required`) потерей данных.
 */
export function routeGaps(drafts: DraftRoute[]): RouteGap[] {
  const gaps: RouteGap[] = [];
  drafts.forEach((r, i) => {
    const missing: string[] = [];
    if (r.destination_prefix.trim() === "") missing.push(MISSING_DESTINATION);
    // Нехватка считается ПО ВЫБРАННОЙ ВЕТВИ: у строки со шлюзом пустое поле
    // адреса претензией не является, а вот невыбранный шлюз — является. Счёт по
    // одному полю уже однажды дал молчаливую потерю маршрута.
    const заполнена =
      kindOf(r) === "gateway" ? (r.gateway_id ?? "").trim() !== "" : r.next_hop_address.trim() !== "";
    if (!заполнена) missing.push(MISSING_NEXT_HOP);
    if (missing.length > 0) gaps.push({ row: i + 1, missing });
  });
  return gaps;
}

export function routeGapText(gap: RouteGap): string {
  return `Строка ${gap.row}: не указан ${gap.missing.join(" и ")}`;
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
      const next = routesFromDrafts(drafts ?? []);

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

      void qc.invalidateQueries({ queryKey: [rtSpec.id] });
    },
  });

  function startEdit() {
    if (routes.length === 0) {
      setDrafts([{ destination_prefix: "", next_hop_address: "" }]);
      return;
    }
    setDrafts(draftsFromRoutes(routes));
  }

  function cancel() {
    setDrafts(null);
  }

  function addRow() {
    setDrafts((prev) => [...(prev ?? []), { destination_prefix: "", next_hop_address: "", _kind: "address" }]);
  }

  function removeRow(index: number) {
    setDrafts((prev) => (prev ?? []).filter((_, i) => i !== index));
  }

  function setRow(index: number, patch: Partial<DraftRoute>) {
    setDrafts((prev) => (prev ?? []).map((r, i) => (i === index ? { ...r, ...patch } : r)));
  }

  const gaps = editing ? routeGaps(drafts ?? []) : [];

  async function save() {
    // Кнопка уже выключена; проверка повторена здесь, потому что защита от
    // отправки неполного набора обязана стоять там, где отправка происходит, —
    // иначе она держится тем, что второго вызывающего не появится.
    if (gaps.length > 0) return;
    try {
      await mutation.mutateAsync();
      cancel();
    } catch (err) {
      const m = errorText(err);
      toast.error(`Статические маршруты: ${m}`);
    }
  }

  const count = editing ? (drafts?.length ?? 0) : routes.length;

  const headerRight = editing ? (
    <Space>
      <Button type="primary" loading={mutation.isPending} disabled={gaps.length > 0} onClick={save}>
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
    // Маркер нужен ПРОБЕ, и предмет у него точный: на карточке таблицы
    // маршрутов действие «Редактировать» есть у ДВУХ разных хозяев — у самого
    // ресурса (шапка карточки) и у этой панели. Их доступные имена совпадают
    // дословно («edit Редактировать»), поэтому выбрать панель по имени кнопки
    // нельзя ничем: `.first()` берёт чужую и открывает не тот редактор.
    <div data-testid="routes-panel" style={{ marginTop: 24, maxWidth: 760 }}>
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
                        {/* Ветвь `next_hop` выбирается ЯВНО и правится здесь же.
                            Прежде выбора не было вовсе: шлюз лишь переживал
                            сохранение, а сменить ветвь можно было только набрав
                            адрес поверх — и обратно шлюз не возвращался ничем,
                            кроме пересоздания таблицы (#375). Обе ячейки стоят в
                            одном столбце: число столбцов таблицы — часть её
                            договора об отсутствии прыжка при входе в правку. */}
                        <div style={{ display: "flex", alignItems: "center", gap: 8, minWidth: 0 }}>
                          <Select
                            aria-label="Вид следующего узла"
                            variant="borderless"
                            value={kindOf(row)}
                            onChange={(v) =>
                              setRow(
                                i,
                                v === "gateway"
                                  ? { _kind: "gateway", next_hop_address: "" }
                                  : { _kind: "address", gateway_id: "" },
                              )
                            }
                            style={{ width: 108, flexShrink: 0 }}
                            options={[
                              { value: "address", label: "Адрес" },
                              { value: "gateway", label: "Шлюз" },
                            ]}
                          />
                          <div style={{ flex: 1, minWidth: 0 }}>
                            {kindOf(row) === "gateway" ? (
                              <RefSelect
                                refResource="gateways"
                                refProjectScoped
                                value={row.gateway_id ?? ""}
                                onChange={(id) => setRow(i, { gateway_id: id })}
                                placeholder="Выберите шлюз"
                              />
                            ) : (
                              <Input
                                variant="borderless"
                                placeholder="10.0.0.1"
                                value={row.next_hop_address}
                                onChange={(e) => setRow(i, { next_hop_address: e.target.value })}
                                style={cellInputStyle}
                              />
                            )}
                          </div>
                        </div>
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

      {gaps.length > 0 && (
        // Каждая неполная строка названа отдельной строкой: перечень через
        // запятую скрывал бы, сколько их, за первой же.
        <div role="alert" style={{ marginTop: 8, fontSize: 12, color: "var(--kc-error)" }}>
          {gaps.map((g) => (
            <div key={g.row}>{routeGapText(g)}</div>
          ))}
        </div>
      )}
    </div>
  );
}
