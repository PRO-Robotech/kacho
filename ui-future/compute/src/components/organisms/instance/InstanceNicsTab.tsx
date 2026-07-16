// InstanceNicsTab — вкладка «Сетевые интерфейсы» detail-страницы инстанса:
// список подключённых NIC (network_interfaces — output-only зеркало) с
// привязкой/отвязкой kacho-vpc NetworkInterface. attach —
// :attachNetworkInterface (вложенный attached_nic_spec c nic_id), detach —
// :detachNetworkInterface (oneof network_interface → nic_id). Async → Operation.

import { useMemo, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { Button, Space, Spin, Typography } from "antd";
import { DeleteOutlined, LoadingOutlined, PlusOutlined } from "@ant-design/icons";
import { ResourceTable, type Column } from "@/components/organisms/ResourceTable";
import { RefSelect } from "@/components/organisms/form/RefSelect";
import { CopyableId } from "@/components/atoms/CopyableId";
import { OperationToastWatcher } from "@/components/molecules/OperationToastWatcher";
import { extractOperationId } from "@/components/molecules/OperationDialog";
import { ApiError } from "@/api/client";
import { instancesApi } from "@/api/resources";
import { getByPath } from "@/lib/resource-registry";
import { useInvalidateResourceList } from "@/lib/use-operation";
import { toast } from "@/lib/toast";

interface NicRow {
  index?: string;
  nic_id?: string;
  subnet_id?: string;
  primary_v4_address?: { address?: string };
}

export function InstanceNicsTab({
  instanceId,
  projectId,
  data,
}: {
  instanceId: string;
  projectId: string | null;
  data: Record<string, unknown>;
}) {
  const invalidate = useInvalidateResourceList();
  const [draftNic, setDraftNic] = useState<string | undefined>();
  const [opId, setOpId] = useState<string | null>(null);
  const [opTitle, setOpTitle] = useState("");
  const [pendingId, setPendingId] = useState<string | null>(null);

  const rows = useMemo<NicRow[]>(() => (getByPath<NicRow[]>(data, "network_interfaces") ?? []) as NicRow[], [data]);
  const attachedIds = useMemo(
    () => new Set(rows.map((r) => r.nic_id).filter((x): x is string => !!x)),
    [rows],
  );

  const mut = useMutation({
    mutationFn: (params: { verb: "attach" | "detach"; nicId: string }) =>
      params.verb === "attach"
        ? instancesApi.attachNetworkInterface(instanceId, params.nicId)
        : instancesApi.detachNetworkInterface(instanceId, params.nicId),
    onSuccess: (resp) => {
      const id = extractOperationId(resp);
      if (id) setOpId(id);
      else {
        setPendingId(null);
        invalidate("compute-instances", projectId);
      }
    },
    onError: (e) => {
      toast.error(`Интерфейс: ${e instanceof ApiError ? `${e.code}: ${e.message}` : (e as Error).message}`);
      setPendingId(null);
    },
  });
  const busy = mut.isPending || opId !== null;

  const onAttach = () => {
    if (!draftNic || attachedIds.has(draftNic)) return;
    setOpTitle("Подключение интерфейса");
    setPendingId(draftNic);
    mut.mutate({ verb: "attach", nicId: draftNic });
    setDraftNic(undefined);
  };
  const onDetach = (nicId: string) => {
    setOpTitle("Отключение интерфейса");
    setPendingId(nicId);
    mut.mutate({ verb: "detach", nicId });
  };

  const columns: Column<NicRow>[] = [
    { header: "Слот", cell: (r) => (r.index != null && r.index !== "" ? String(r.index) : "—") },
    {
      header: "NIC",
      cell: (r) => (r.nic_id ? <CopyableId id={r.nic_id} /> : <Typography.Text type="secondary">—</Typography.Text>),
    },
    { header: "Подсеть", cell: (r) => r.subnet_id || "—" },
    { header: "IPv4", cell: (r) => r.primary_v4_address?.address || "—" },
    {
      header: "",
      className: "text-right whitespace-nowrap",
      cell: (r) => {
        const nid = r.nic_id ?? "";
        if (!nid) return null;
        return pendingId === nid ? (
          <Spin indicator={<LoadingOutlined style={{ fontSize: 12 }} spin />} />
        ) : (
          <Button
            type="text"
            danger
            size="small"
            icon={<DeleteOutlined />}
            aria-label="Отключить"
            onClick={() => onDetach(nid)}
            disabled={busy}
          />
        );
      },
    },
  ];

  return (
    <Space direction="vertical" size={12} style={{ width: "100%" }}>
      <div style={{ display: "flex", gap: 8, alignItems: "center", flexWrap: "wrap" }}>
        <div style={{ minWidth: 300 }}>
          <RefSelect
            refResource="network-interfaces"
            refProjectScoped
            value={draftNic}
            onChange={(v) => setDraftNic(v || undefined)}
            refFilter={(row) => !attachedIds.has((row.id as string) ?? "")}
            placeholder="Выбрать сетевой интерфейс…"
            disabled={busy}
          />
        </div>
        <Button type="primary" icon={<PlusOutlined />} onClick={onAttach} disabled={!draftNic || busy}>
          Подключить
        </Button>
      </div>
      {rows.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border p-10 text-center text-sm text-muted-foreground">
          Сетевые интерфейсы ещё не подключены.
        </div>
      ) : (
        <ResourceTable rows={rows} columns={columns} rowKey={(r) => r.nic_id ?? r.index ?? Math.random().toString()} />
      )}
      <OperationToastWatcher
        opId={opId}
        title={opTitle}
        onDone={() => {
          setOpId(null);
          setPendingId(null);
          invalidate("compute-instances", projectId);
        }}
      />
    </Space>
  );
}
