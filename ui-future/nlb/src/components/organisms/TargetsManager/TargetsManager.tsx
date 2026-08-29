// TargetsManager — управление backend-таргетами целевой группы (Target.oneof
// identity: Compute Instance / VPC NIC / in-cloud IP / external IP) прямо в блоке
// «Обзор». Add и remove — РАЗНЫЕ verb-RPC (:addTargets / :removeTargets), каждое
// действие применяется сразу своим RPC (Operation envelope). Прогресс —
// неблокирующий toast (OperationToastWatcher), onDone → invalidate target-groups.
//
// Backend (kacho-nlb) матчит :removeTargets по identity-форме (стабильного
// target id нет), поэтому remove отправляет только oneof-identity без weight.
//
// ВИД СЕКЦИИ — ОБЩИЙ, а не свой. Секция «шапка + таблица» рисуется
// `DetailSurface`, а геометрия строк берётся из `editor-surface` — те же числа,
// что у меток, статических маршрутов, правил группы и блоков CIDR. Здесь стояли
// свои: высота строки 41 против общей 42, радиус 8 против 11, своя заливка, свой
// перечень моноширинных гарнитур и свой стиль шапки колонок. Пять копий одной
// высоты расходятся молча, и расхождение видно только рядом на одном экране —
// то есть почти никогда (канон §4, §9).
//
// Снят и надзаголовок «Список» со счётчиком «Цели (N)»: надзаголовков в консоли
// нет, а счётчик снят решением владельца ВЕЗДЕ (канон §1).

import type { ReactNode } from "react";
import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { Button, Input, InputNumber, Select, Space, Spin, Typography } from "antd";
import { DeleteOutlined, LoadingOutlined, PlusOutlined } from "@ant-design/icons";
import { api } from "@/api/client";
import { OperationToastWatcher } from "@/components/molecules/OperationToastWatcher";
import { extractOperationId } from "@/components/molecules/OperationDialog";
import { RefSelect } from "@/components/organisms/form/RefSelect";
import { RefNameLink } from "@/components/molecules/RefNameLink";
import { StatusBadge } from "@/components/atoms/StatusBadge";
import { DetailSurface, DETAIL_CONTENT_WIDTH } from "@/components/organisms/DetailShell";
import {
  EDITOR_ACTIONS_WIDTH,
  MONO_FONT,
  editorBodyStyle,
  editorEmptyStyle,
  editorFirstRowStyle,
  editorHeadCellStyle,
  editorIconButtonStyle,
  editorRowStyle,
  editorValueCellStyle,
} from "@shared/components/organisms/form/editor-surface";
import { useInvalidateResourceList } from "@/lib/use-operation";
import { toast } from "@/lib/toast";
import { errorText } from "@shared/lib/error-presentation";
import type { SetReplacementDraft } from "@shared/lib/set-replacement-draft";

/**
 * Место полной замены набора: элемент уезжает на край целиком, поэтому поле
 * контракта, которого не назвал тип `Target`, до края не доедет и не вернётся.
 * Состав сверяется с контрактом гейтом
 * `shared/test/set-replacement-draft-composition`; тип `Target` объявлен ещё и
 * в `shared` — гейт разрешает имя по всему дереву, поэтому обе копии сверяются
 * вместе, а не расходятся молча.
 */
export const TARGETS_MANAGER_REPLACEMENT: SetReplacementDraft = {
  field: "targets",
  contract: "kacho/cloud/loadbalancer/v1/target_group.proto",
  message: "Target",
  drafts: ["Target"],
};

const TARGET_GROUPS_API = "/nlb/v1/targetGroups";

export type TargetKind = "instance" | "nic" | "ip_ref" | "external_ip";

// Target — wire-shape (snake_case). Ровно одна identity-форма из oneof.
export interface Target {
  instance_id?: string;
  nic_id?: string;
  ip_ref?: { subnet_id?: string; address?: string };
  external_ip?: { address?: string; zone_id?: string };
  weight?: number;
  /**
   * Состояние цели внутри группы. Назначается сервером (снятие двухфазное:
   * DRAINING, затем удаление по истечении задержки) — форма его не отправляет,
   * но НАЗЫВАЕТ: не названное поле контракта невидимо консоли целиком, и
   * «удаление ничего не сделало» отличить от «цель сливается» нечем.
   */
  status?: string;
  /** Момент перевода цели в слив. Назначается сервером; см. `status`. */
  drain_started_at?: string;
}

export interface TargetFormState {
  instanceId?: string;
  nicId?: string;
  subnetId?: string;
  ipAddr?: string;
  extAddr?: string;
  zoneId?: string;
  weight?: number;
}

// buildTargetPayload — собирает wire-Target из формы по дискриминатору kind.
export function buildTargetPayload(kind: TargetKind, f: TargetFormState): Target | null {
  const weight = typeof f.weight === "number" ? f.weight : 1;
  switch (kind) {
    case "instance":
      return f.instanceId ? { instance_id: f.instanceId, weight } : null;
    case "nic":
      return f.nicId ? { nic_id: f.nicId, weight } : null;
    case "ip_ref":
      return f.subnetId && f.ipAddr ? { ip_ref: { subnet_id: f.subnetId, address: f.ipAddr }, weight } : null;
    case "external_ip":
      return f.extAddr ? { external_ip: { address: f.extAddr, zone_id: f.zoneId ?? "" }, weight } : null;
    default:
      return null;
  }
}

// targetIdentity — вид цели словом. Ветвь `oneof` — закрытое множество, поэтому
// запасного значения здесь нет: неназванная цель отвечает прочерком.
export function targetIdentity(t: Target): { label: string } {
  if (t.instance_id) return { label: "Виртуальная машина" };
  if (t.nic_id) return { label: "Сетевой интерфейс" };
  if (t.ip_ref) return { label: "Адрес в облаке" };
  if (t.external_ip) return { label: "Внешний адрес" };
  return { label: "—" };
}

/**
 * Эндпоинт цели — ССЫЛКА на тот ресурс, которым цель и является.
 *
 * Здесь стоял машинный идентификатор плоским моноширинным текстом: ни имени, ни
 * перехода, при том что соседние поля той же карточки ссылкой уже были. Машина,
 * интерфейс и подсеть — ресурсы со своими карточками, поэтому рисуются
 * единственным видом ссылки консоли (канон §9); адрес и зона внешней цели
 * ресурсом не являются — адрес остаётся значением, зона ведёт в каталог.
 */
function TargetEndpoint({ t, projectId }: { t: Target; projectId: string | null }): ReactNode {
  const project = projectId ?? undefined;
  if (t.instance_id) {
    return <RefNameLink specId="compute-instances" refId={t.instance_id} projectId={project} maxChars={40} />;
  }
  if (t.nic_id) {
    return <RefNameLink specId="network-interfaces" refId={t.nic_id} projectId={project} maxChars={40} />;
  }
  if (t.ip_ref) {
    return (
      <span style={{ display: "inline-flex", alignItems: "center", gap: 8, minWidth: 0 }}>
        <span>{t.ip_ref.address || "—"}</span>
        <RefNameLink specId="subnets" refId={t.ip_ref.subnet_id} projectId={project} maxChars={28} />
      </span>
    );
  }
  if (t.external_ip) {
    return (
      <span style={{ display: "inline-flex", alignItems: "center", gap: 8, minWidth: 0 }}>
        <span>{t.external_ip.address || "—"}</span>
        {t.external_ip.zone_id ? <RefNameLink specId="zones" refId={t.external_ip.zone_id} maxChars={28} /> : null}
      </span>
    );
  }
  return <span>—</span>;
}

// targetIdentityOnly — для :removeTargets backend матчит по identity-форме.
export function targetIdentityOnly(t: Target): Target {
  if (t.instance_id) return { instance_id: t.instance_id };
  if (t.nic_id) return { nic_id: t.nic_id };
  if (t.ip_ref) return { ip_ref: { subnet_id: t.ip_ref.subnet_id, address: t.ip_ref.address } };
  if (t.external_ip) return { external_ip: { address: t.external_ip.address, zone_id: t.external_ip.zone_id } };
  return {};
}

interface Props {
  targetGroupId: string;
  projectId: string | null;
  targets: Target[];
}

export function TargetsManager({ targetGroupId, projectId, targets }: Props) {
  const invalidate = useInvalidateResourceList();
  const [kind, setKind] = useState<TargetKind>("instance");
  const [form, setForm] = useState<TargetFormState>({ weight: 1 });
  const [opId, setOpId] = useState<string | null>(null);
  const [opTitle, setOpTitle] = useState("");
  const [pendingKey, setPendingKey] = useState<string | null>(null);

  const set = (patch: Partial<TargetFormState>) => setForm((s) => ({ ...s, ...patch }));
  const resetForm = () => {
    setKind("instance");
    setForm({ weight: 1 });
  };

  const mutate = useMutation({
    mutationFn: (params: { verb: "add" | "remove"; target: Target }) =>
      // Глагол называется ЛИТЕРАЛОМ, а не собирается из дискриминатора
      // (`:${verb}Targets`). Собранный адрес не виден надзору за глаголами
      // (`shared/src/test/console-verb-routes-exist.test.ts`): он читает
      // синтаксическое дерево и сверяет каждый адресуемый консолью глагол с
      // контрактом. Пока сегмент склеивался, ОБА глагола группы целей были вне
      // наблюдения — надзор их не видел и о них ничего не утверждал, а
      // единственные видимые ему вхождения жили на странице, которую не
      // рендерил ни один маршрут.
      api.action(
        params.verb === "add"
          ? `${TARGET_GROUPS_API}/${targetGroupId}:addTargets`
          : `${TARGET_GROUPS_API}/${targetGroupId}:removeTargets`,
        {
          targets: [params.verb === "add" ? params.target : targetIdentityOnly(params.target)],
        },
      ),
    onSuccess: (resp, vars) => {
      const id = extractOperationId(resp);
      if (id) {
        setOpTitle(vars.verb === "add" ? "Добавление target" : "Удаление target");
        setOpId(id);
        if (vars.verb === "add") resetForm();
      } else {
        if (vars.verb === "add") resetForm();
        setPendingKey(null);
        invalidate("target-groups", projectId);
      }
    },
    onError: (err, vars) => {
      const m = errorText(err);
      toast.error(`${vars.verb === "add" ? "Добавить" : "Удалить"} target: ${m}`);
      setPendingKey(null);
    },
  });

  const payload = buildTargetPayload(kind, form);
  const inputsDisabled = mutate.isPending || opId !== null;

  const onAdd = () => {
    if (!payload) return;
    setPendingKey(JSON.stringify(targetIdentityOnly(payload)));
    mutate.mutate({ verb: "add", target: payload });
  };

  const onRemove = (t: Target) => {
    setPendingKey(JSON.stringify(targetIdentityOnly(t)));
    mutate.mutate({ verb: "remove", target: t });
  };

  return (
    <div style={{ marginTop: 24, maxWidth: DETAIL_CONTENT_WIDTH }}>
      {/* Секция — ОДИН блок: шапка внутри той же поверхности, что и таблица.
          Заголовок называет предмет, а не способ показа; счётчика при нём нет. */}
      <DetailSurface title="Цели">
        <div style={editorBodyStyle}>
          <table className="w-full kc-grid-table" style={{ tableLayout: "fixed", borderCollapse: "collapse" }}>
            <colgroup>
              <col style={{ width: 170 }} />
              <col />
              <col style={{ width: 150 }} />
              <col style={{ width: 80 }} />
              <col style={{ width: EDITOR_ACTIONS_WIDTH }} />
            </colgroup>
            <thead>
              <tr>
                <th className="text-left" style={editorHeadCellStyle}>
                  Тип
                </th>
                <th className="text-left" style={editorHeadCellStyle}>
                  Эндпоинт
                </th>
                {/* Состояние цели назначает сервер: снятие двухфазное (сперва
                    слив, потом удаление). Без этой колонки «удаление ничего не
                    сделало» и «цель сливается» выглядят одинаково — поле
                    приезжало и не читалось никем. */}
                <th className="text-left" style={editorHeadCellStyle}>
                  Состояние
                </th>
                <th className="text-left" style={editorHeadCellStyle}>
                  Вес
                </th>
                {/* Колонка действий есть всегда — число колонок не меняется. */}
                <th style={{ ...editorHeadCellStyle, padding: 0 }} />
              </tr>
            </thead>
            <tbody>
              {targets.length === 0 && (
                <tr style={editorFirstRowStyle}>
                  <td colSpan={5} style={editorEmptyStyle}>
                    Цели ещё не добавлены
                  </td>
                </tr>
              )}
              {targets.map((t, i) => {
                const ident = targetIdentity(t);
                const key = JSON.stringify(targetIdentityOnly(t));
                const busy = pendingKey === key && (mutate.isPending || opId !== null);
                return (
                  <tr key={i} className="kc-kv-row" style={i === 0 ? editorFirstRowStyle : editorRowStyle}>
                    {/* Вид цели — слово, а не машинное значение: у ячейки свой
                        набор, поэтому моноширинность здесь снята. */}
                    <td style={{ ...editorValueCellStyle, fontFamily: "inherit" }}>{ident.label}</td>
                    <td style={{ ...editorValueCellStyle, overflow: "hidden" }}>
                      <TargetEndpoint t={t} projectId={projectId} />
                    </td>
                    <td style={{ ...editorValueCellStyle, fontFamily: "inherit" }}>
                      <StatusBadge state={t.status} />
                    </td>
                    <td style={editorValueCellStyle}>{t.weight ?? 1}</td>
                    <td style={{ ...editorValueCellStyle, padding: 0, textAlign: "center" }}>
                      {busy ? (
                        <Spin indicator={<LoadingOutlined style={{ fontSize: 12 }} spin />} />
                      ) : (
                        <Button
                          type="text"
                          danger
                          size="small"
                          icon={<DeleteOutlined />}
                          aria-label="Удалить target"
                          onClick={() => onRemove(t)}
                          disabled={inputsDisabled}
                          style={editorIconButtonStyle}
                        />
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
            <tfoot>
              <tr style={{ borderTop: "1px solid var(--kc-border)" }}>
                <td colSpan={5} style={{ padding: 10 }}>
                  <Space direction="vertical" size={8} style={{ width: "100%" }}>
                    <Space wrap align="start" style={{ width: "100%" }}>
                      <Select
                        value={kind}
                        onChange={(v) => setKind(v as TargetKind)}
                        disabled={inputsDisabled}
                        style={{ width: 220 }}
                        options={[
                          { value: "instance", label: "Виртуальная машина" },
                          { value: "nic", label: "Сетевой интерфейс" },
                          { value: "ip_ref", label: "Адрес в облаке (подсеть + адрес)" },
                          { value: "external_ip", label: "Внешний адрес (вне облака)" },
                        ]}
                      />
                      {kind === "instance" && (
                        <div style={{ minWidth: 260 }}>
                          <RefSelect
                            refResource="compute-instances"
                            refProjectScoped
                            value={form.instanceId}
                            onChange={(v) => set({ instanceId: v || undefined })}
                          />
                        </div>
                      )}
                      {kind === "nic" && (
                        <div style={{ minWidth: 260 }}>
                          <RefSelect
                            refResource="network-interfaces"
                            refProjectScoped
                            value={form.nicId}
                            onChange={(v) => set({ nicId: v || undefined })}
                          />
                        </div>
                      )}
                      {kind === "ip_ref" && (
                        <>
                          <div style={{ minWidth: 220 }}>
                            <RefSelect
                              refResource="subnets"
                              refProjectScoped
                              value={form.subnetId}
                              onChange={(v) => set({ subnetId: v || undefined })}
                            />
                          </div>
                          <Input
                            value={form.ipAddr ?? ""}
                            onChange={(e) => set({ ipAddr: e.target.value.trim() })}
                            placeholder="10.0.0.5"
                            disabled={inputsDisabled}
                            style={{ width: 160, fontFamily: MONO_FONT }}
                          />
                        </>
                      )}
                      {kind === "external_ip" && (
                        <>
                          <Input
                            value={form.extAddr ?? ""}
                            onChange={(e) => set({ extAddr: e.target.value.trim() })}
                            placeholder="203.0.113.10"
                            disabled={inputsDisabled}
                            style={{ width: 180, fontFamily: MONO_FONT }}
                          />
                          <div style={{ minWidth: 200 }}>
                            <RefSelect
                              refResource="zones"
                              value={form.zoneId}
                              onChange={(v) => set({ zoneId: v || undefined })}
                              placeholder="Зона (опц.)"
                            />
                          </div>
                        </>
                      )}
                      <InputNumber
                        min={0}
                        max={1000}
                        value={form.weight ?? 1}
                        disabled={inputsDisabled}
                        onChange={(v) => set({ weight: typeof v === "number" ? v : 1 })}
                        style={{ width: 90 }}
                      />
                      <Button
                        type="dashed"
                        icon={<PlusOutlined />}
                        onClick={onAdd}
                        disabled={!payload || inputsDisabled}
                      >
                        Добавить
                      </Button>
                    </Space>
                    <Typography.Text type="secondary" style={{ fontSize: 11 }}>
                      Вес 0–1000; 0 — слить трафик, не удаляя target.
                    </Typography.Text>
                  </Space>
                </td>
              </tr>
            </tfoot>
          </table>
        </div>
      </DetailSurface>

      <OperationToastWatcher
        opId={opId}
        title={opTitle}
        onDone={() => {
          setOpId(null);
          setPendingKey(null);
          invalidate("target-groups", projectId);
        }}
      />
    </div>
  );
}
