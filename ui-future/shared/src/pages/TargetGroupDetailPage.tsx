// TargetGroupDetailPage — generic ResourceDetailPage (общая инфа + health_check)
// плюс кастомная секция «Targets» через secondaryActions (KAC-230).
//
// Targets — бэкенды target-group'ы, ссылающиеся на Compute Instance / VPC NIC /
// in-cloud IP / external IP (Target.oneof identity, design §6). Управляются
// отдельными verb-RPC :addTargets / :removeTargets (Operation envelope), как
// InstanceDetailPage :attachDisk / :detachDisk. `TargetGroup.targets` приходит в
// GET-ответе — рендерим из него.

import { useCallback, useMemo, useState } from "react";
import { useParams } from "react-router-dom";
import { useMutation } from "@tanstack/react-query";
import { Button, Card, Form, Input, InputNumber, Modal, Popconfirm, Select, Space, Table, Tag, Typography } from "antd";
import { PlusOutlined, DeleteOutlined } from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import { ResourceDetailPage } from "@shared/components/organisms/ResourceDetailPage";
import { OperationDialog, extractOperationId } from "@shared/components/molecules/OperationDialog";
import { RefSelect } from "@shared/components/organisms/form/RefSelect";
import { api, ApiError } from "@shared/api/client";
import { REGISTRY, getByPath } from "@shared/lib/resource-registry";
import { useProjectStore } from "@shared/lib/context-store";
import { useInvalidateResourceList } from "@shared/lib/use-operation";
import { toast } from "@shared/lib/toast";

const SPEC = REGISTRY["target-groups"];

export type TargetKind = "instance" | "nic" | "ip_ref" | "external_ip";

// Target — wire-shape (snake_case, как отдаёт/принимает api.client после
// camel↔snake конверсии). Ровно одна identity-форма из oneof.
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

/**
 * buildTargetPayload — собирает wire-Target из формы по дискриминатору `kind`.
 * Возвращает null, если обязательные для выбранного типа поля не заполнены
 * (UI блокирует submit). Имена полей точно соответствуют proto oneof identity:
 * instance_id / nic_id / ip_ref{subnet_id,address} / external_ip{address,zone_id}.
 */
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

/** targetIdentity — человекочитаемое представление identity для таблицы. */
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

/**
 * targetIdentityOnly — для :removeTargets backend матчит по identity-форме
 * (стабильного target id нет), поэтому отправляем только oneof-identity без
 * weight/прочего.
 */
export function targetIdentityOnly(t: Target): Target {
  if (t.instance_id) return { instance_id: t.instance_id };
  if (t.nic_id) return { nic_id: t.nic_id };
  if (t.ip_ref) return { ip_ref: { subnet_id: t.ip_ref.subnet_id, address: t.ip_ref.address } };
  if (t.external_ip) return { external_ip: { address: t.external_ip.address, zone_id: t.external_ip.zone_id } };
  return {};
}

export function TargetGroupDetailPage() {
  const { uid: tgId } = useParams();
  const project = useProjectStore((s) => s.project);
  const invalidate = useInvalidateResourceList();

  const [addOpen, setAddOpen] = useState(false);
  const [kind, setKind] = useState<TargetKind>("instance");
  const [form, setForm] = useState<TargetFormState>({ weight: 1 });
  const [opId, setOpId] = useState<string | null>(null);
  const [opTitle, setOpTitle] = useState("Операция");

  const set = (patch: Partial<TargetFormState>) => setForm((s) => ({ ...s, ...patch }));
  const resetForm = () => {
    setKind("instance");
    setForm({ weight: 1 });
  };

  const onOpDone = useCallback(() => {
    setOpId(null);
    invalidate("target-groups", project?.id);
  }, [invalidate, project?.id]);

  const addMut = useMutation({
    mutationFn: (t: Target) => api.action(`${SPEC.apiPath}/${tgId}:addTargets`, { targets: [t] }),
    onSuccess: (resp) => {
      setAddOpen(false);
      resetForm();
      const id = extractOperationId(resp);
      if (id) {
        setOpTitle("Добавление target");
        setOpId(id);
      } else {
        invalidate("target-groups", project?.id);
      }
    },
    onError: (e) =>
      toast.error(`Добавить target: ${e instanceof ApiError ? `${e.code}: ${e.message}` : (e as Error).message}`),
  });

  const removeMut = useMutation({
    mutationFn: (t: Target) =>
      api.action(`${SPEC.apiPath}/${tgId}:removeTargets`, { targets: [targetIdentityOnly(t)] }),
    onSuccess: (resp) => {
      const id = extractOperationId(resp);
      if (id) {
        setOpTitle("Удаление target");
        setOpId(id);
      } else {
        invalidate("target-groups", project?.id);
      }
    },
    onError: (e) =>
      toast.error(`Удалить target: ${e instanceof ApiError ? `${e.code}: ${e.message}` : (e as Error).message}`),
  });

  const targetsSection = useMemo(
    () => (data: Record<string, unknown>) => {
      const targets = (getByPath<Target[]>(data, "targets") ?? []) as Target[];
      const columns: ColumnsType<Target> = [
        { title: "Тип", key: "kind", width: 120, render: (_v, t) => <Tag>{targetIdentity(t).label}</Tag> },
        {
          title: "Эндпоинт",
          key: "endpoint",
          render: (_v, t) => (
            <Typography.Text style={{ fontFamily: "monospace", fontSize: 12 }}>
              {targetIdentity(t).value}
            </Typography.Text>
          ),
        },
        { title: "Вес", dataIndex: "weight", key: "weight", width: 90, render: (v) => v ?? 1 },
        {
          title: "",
          key: "actions",
          width: 60,
          render: (_v, t) => (
            <Popconfirm
              title="Удалить target?"
              okText="Удалить"
              okButtonProps={{ danger: true }}
              cancelText="Отмена"
              onConfirm={() => removeMut.mutate(t)}
            >
              <Button size="small" type="text" danger icon={<DeleteOutlined />} />
            </Popconfirm>
          ),
        },
      ];
      return (
        <Card
          size="small"
          title={
            <Space>
              <span>Targets</span>
              <Tag color="blue">{targets.length}</Tag>
            </Space>
          }
          extra={
            <Button
              size="small"
              type="primary"
              icon={<PlusOutlined />}
              onClick={() => {
                resetForm();
                setAddOpen(true);
              }}
            >
              Добавить target
            </Button>
          }
        >
          {targets.length === 0 ? (
            <Typography.Text type="secondary">Targets ещё не добавлены — нажмите «Добавить target».</Typography.Text>
          ) : (
            <Table<Target>
              rowKey={(t) => JSON.stringify(targetIdentityOnly(t))}
              size="small"
              pagination={false}
              dataSource={targets}
              columns={columns}
            />
          )}
        </Card>
      );
    },
    [removeMut],
  );

  const payload = buildTargetPayload(kind, form);

  return (
    <>
      <ResourceDetailPage spec={SPEC} secondaryActions={targetsSection} />

      <Modal
        title="Добавить target"
        open={addOpen}
        onCancel={() => setAddOpen(false)}
        onOk={() => {
          if (payload) addMut.mutate(payload);
        }}
        okButtonProps={{ disabled: !payload, loading: addMut.isPending }}
        okText="Добавить"
        cancelText="Отмена"
        width={560}
        destroyOnHidden
      >
        <Form
          layout="horizontal"
          labelCol={{ flex: "170px" }}
          wrapperCol={{ flex: "auto" }}
          labelAlign="left"
          colon={false}
        >
          <Form.Item label="Тип target">
            <Select
              value={kind}
              onChange={(v) => setKind(v as TargetKind)}
              options={[
                { value: "instance", label: "Compute Instance" },
                { value: "nic", label: "VPC NetworkInterface" },
                { value: "ip_ref", label: "In-cloud IP (subnet + адрес)" },
                { value: "external_ip", label: "External IP (вне облака)" },
              ]}
            />
          </Form.Item>

          {kind === "instance" && (
            <Form.Item label="Instance">
              <RefSelect
                refResource="compute-instances"
                refProjectScoped
                value={form.instanceId}
                onChange={(v) => set({ instanceId: v || undefined })}
              />
            </Form.Item>
          )}
          {kind === "nic" && (
            <Form.Item label="NetworkInterface">
              <RefSelect
                refResource="network-interfaces"
                refProjectScoped
                value={form.nicId}
                onChange={(v) => set({ nicId: v || undefined })}
              />
            </Form.Item>
          )}
          {kind === "ip_ref" && (
            <>
              <Form.Item label="Subnet">
                <RefSelect
                  refResource="subnets"
                  refProjectScoped
                  value={form.subnetId}
                  onChange={(v) => set({ subnetId: v || undefined })}
                />
              </Form.Item>
              <Form.Item label="Адрес">
                <Input
                  value={form.ipAddr ?? ""}
                  onChange={(e) => set({ ipAddr: e.target.value.trim() })}
                  placeholder="10.0.0.5"
                  style={{ fontFamily: "monospace" }}
                />
              </Form.Item>
            </>
          )}
          {kind === "external_ip" && (
            <>
              <Form.Item label="Адрес">
                <Input
                  value={form.extAddr ?? ""}
                  onChange={(e) => set({ extAddr: e.target.value.trim() })}
                  placeholder="203.0.113.10"
                  style={{ fontFamily: "monospace" }}
                />
              </Form.Item>
              <Form.Item label="Zone (опц.)">
                <RefSelect refResource="zones" value={form.zoneId} onChange={(v) => set({ zoneId: v || undefined })} />
              </Form.Item>
            </>
          )}

          <Form.Item label="Weight">
            <InputNumber
              min={0}
              max={1000}
              value={form.weight ?? 1}
              onChange={(v) => set({ weight: typeof v === "number" ? v : 1 })}
            />
            <Typography.Text type="secondary" style={{ fontSize: 12, marginLeft: 8 }}>
              0–1000; 0 = слить трафик, не удаляя target.
            </Typography.Text>
          </Form.Item>
        </Form>
      </Modal>

      <OperationDialog opId={opId} title={opTitle} onSuccess={onOpDone} onClose={onOpDone} />
    </>
  );
}
