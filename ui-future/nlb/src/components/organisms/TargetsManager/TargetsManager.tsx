// TargetsManager — управление backend-таргетами целевой группы (Target.oneof
// identity: Compute Instance / VPC NIC / in-cloud IP / external IP) прямо в блоке
// «Обзор». Add и remove — РАЗНЫЕ verb-RPC (:addTargets / :removeTargets), каждое
// действие применяется сразу своим RPC (Operation envelope). Прогресс —
// неблокирующий toast (OperationToastWatcher), onDone → invalidate target-groups.
//
// Backend (kacho-nlb) матчит :removeTargets по identity-форме (стабильного
// target id нет), поэтому remove отправляет только oneof-identity без weight.

import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { Button, Input, InputNumber, Select, Space, Spin, Typography } from "antd";
import { DeleteOutlined, LoadingOutlined, PlusOutlined } from "@ant-design/icons";
import { ApiError, api } from "@/api/client";
import { OperationToastWatcher } from "@/components/molecules/OperationToastWatcher";
import { extractOperationId } from "@/components/molecules/OperationDialog";
import { RefSelect } from "@/components/organisms/form/RefSelect";
import { useInvalidateResourceList } from "@/lib/use-operation";
import { toast } from "@/lib/toast";

const TARGET_GROUPS_API = "/nlb/v1/targetGroups";
const MONO_FONT = "ui-monospace, monospace";
const ROW_H = 41;

export type TargetKind = "instance" | "nic" | "ip_ref" | "external_ip";

// Target — wire-shape (snake_case). Ровно одна identity-форма из oneof.
export interface Target {
  instance_id?: string;
  nic_id?: string;
  ip_ref?: { subnet_id?: string; address?: string };
  external_ip?: { address?: string; zone_id?: string };
  weight?: number;
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

// targetIdentity — человекочитаемое представление identity для таблицы.
export function targetIdentity(t: Target): { label: string; value: string } {
  if (t.instance_id) return { label: "Instance", value: t.instance_id };
  if (t.nic_id) return { label: "NIC", value: t.nic_id };
  if (t.ip_ref) return { label: "In-cloud IP", value: `${t.ip_ref.address ?? ""} (${t.ip_ref.subnet_id ?? ""})` };
  if (t.external_ip)
    return {
      label: "External IP",
      value: `${t.external_ip.address ?? ""}${t.external_ip.zone_id ? ` @${t.external_ip.zone_id}` : ""}`,
    };
  return { label: "—", value: "" };
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
      api.action(`${TARGET_GROUPS_API}/${targetGroupId}:${params.verb}Targets`, {
        targets: [params.verb === "add" ? params.target : targetIdentityOnly(params.target)],
      }),
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
      const m = err instanceof ApiError ? `${err.code}: ${err.message}` : (err as Error).message;
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
    <div style={{ marginTop: 24, maxWidth: 760 }}>
      {/* Заголовок секции — единый стиль caps-eyebrow + title (как PanelHeader). */}
      <div style={{ marginBottom: 10 }}>
        <div
          style={{
            fontSize: 11,
            fontWeight: 600,
            letterSpacing: "0.06em",
            textTransform: "uppercase",
            color: "var(--kc-primary)",
          }}
        >
          Список
        </div>
        <div style={{ fontSize: 15, fontWeight: 600, color: "var(--kc-text)" }}>
          Targets <Typography.Text type="secondary">({targets.length})</Typography.Text>
        </div>
      </div>

      <div
        style={{
          border: "1px solid var(--kc-border)",
          borderRadius: 8,
          overflow: "hidden",
          background: "var(--kc-page)",
        }}
      >
        <table className="w-full text-sm kc-grid-table" style={{ tableLayout: "fixed" }}>
          <colgroup>
            <col style={{ width: 130 }} />
            <col />
            <col style={{ width: 80 }} />
            <col style={{ width: 48 }} />
          </colgroup>
          <thead>
            <tr style={{ background: "var(--kc-container)" }}>
              {["Тип", "Эндпоинт", "Вес", ""].map((h, i) => (
                <th
                  key={i}
                  className="text-left"
                  style={{
                    padding: "7px 12px",
                    fontSize: 11,
                    fontWeight: 600,
                    letterSpacing: "0.02em",
                    color: "var(--kc-text-tertiary)",
                  }}
                >
                  {h}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {targets.length === 0 && (
              <tr style={{ height: ROW_H, borderTop: "1px solid var(--kc-border-secondary)" }}>
                <td
                  colSpan={4}
                  style={{
                    textAlign: "center",
                    verticalAlign: "middle",
                    fontSize: 12,
                    color: "var(--kc-text-tertiary)",
                  }}
                >
                  Targets ещё не добавлены
                </td>
              </tr>
            )}
            {targets.map((t, i) => {
              const ident = targetIdentity(t);
              const key = JSON.stringify(targetIdentityOnly(t));
              const busy = pendingKey === key && (mutate.isPending || opId !== null);
              return (
                <tr
                  key={i}
                  className="kc-kv-row"
                  style={{ height: ROW_H, borderTop: "1px solid var(--kc-border-secondary)" }}
                >
                  <td className="px-3" style={{ verticalAlign: "middle" }}>
                    {ident.label}
                  </td>
                  <td
                    className="px-3 font-mono text-xs"
                    style={{
                      verticalAlign: "middle",
                      overflow: "hidden",
                      textOverflow: "ellipsis",
                      whiteSpace: "nowrap",
                    }}
                  >
                    {ident.value}
                  </td>
                  <td className="px-3" style={{ verticalAlign: "middle" }}>
                    {t.weight ?? 1}
                  </td>
                  <td className="px-1 text-center" style={{ verticalAlign: "middle" }}>
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
                      />
                    )}
                  </td>
                </tr>
              );
            })}
          </tbody>
          <tfoot>
            <tr style={{ borderTop: "1px solid var(--kc-border-secondary)" }}>
              <td colSpan={4} style={{ padding: "10px 12px" }}>
                <Space direction="vertical" size={8} style={{ width: "100%" }}>
                  <Space wrap align="start" style={{ width: "100%" }}>
                    <Select
                      value={kind}
                      onChange={(v) => setKind(v as TargetKind)}
                      disabled={inputsDisabled}
                      style={{ width: 220 }}
                      options={[
                        { value: "instance", label: "Compute Instance" },
                        { value: "nic", label: "VPC NetworkInterface" },
                        { value: "ip_ref", label: "In-cloud IP (subnet + адрес)" },
                        { value: "external_ip", label: "External IP (вне облака)" },
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
                          style={{ width: 160, fontFamily: MONO_FONT, fontSize: 12.5 }}
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
                          style={{ width: 180, fontFamily: MONO_FONT, fontSize: 12.5 }}
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
                    <Button type="dashed" icon={<PlusOutlined />} onClick={onAdd} disabled={!payload || inputsDisabled}>
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
