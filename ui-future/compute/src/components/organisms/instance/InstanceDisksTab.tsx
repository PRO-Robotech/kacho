// InstanceDisksTab — вкладка «Диски» detail-страницы инстанса: список
// подключённых томов (boot_disk + secondary_disks — output-only зеркала) с
// привязкой/отвязкой storage-тома. attach — :attachDisk (вложенный
// attached_disk_spec c volume_id), detach — :detachDisk (oneof disk → volume_id).
// Оба async → Operation-poll.

import { useMemo, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { Button, Checkbox, Input, Space, Spin, Typography } from "antd";
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

interface DiskRow {
  volume_id?: string;
  device_name?: string;
  mode?: string;
  auto_delete?: boolean;
  is_boot?: boolean;
}

export function InstanceDisksTab({
  instanceId,
  projectId,
  data,
}: {
  instanceId: string;
  projectId: string | null;
  data: Record<string, unknown>;
}) {
  const invalidate = useInvalidateResourceList();
  const [draftVolume, setDraftVolume] = useState<string | undefined>();
  const [deviceName, setDeviceName] = useState("");
  const [autoDelete, setAutoDelete] = useState(false);
  const [opId, setOpId] = useState<string | null>(null);
  const [opTitle, setOpTitle] = useState("");
  const [pendingId, setPendingId] = useState<string | null>(null);

  const rows = useMemo<DiskRow[]>(() => {
    const boot = getByPath<DiskRow>(data, "boot_disk");
    const secondary = (getByPath<DiskRow[]>(data, "secondary_disks") ?? []) as DiskRow[];
    const list: DiskRow[] = [];
    if (boot && (boot.volume_id || boot.device_name)) list.push({ ...boot, is_boot: true });
    list.push(...secondary);
    return list;
  }, [data]);
  const attachedIds = useMemo(
    () => new Set(rows.map((r) => r.volume_id).filter((x): x is string => !!x)),
    [rows],
  );

  const mut = useMutation({
    mutationFn: (params: { verb: "attach" | "detach"; volumeId: string }) =>
      params.verb === "attach"
        ? instancesApi.attachDisk(instanceId, params.volumeId, deviceName || undefined, autoDelete)
        : instancesApi.detachDisk(instanceId, params.volumeId),
    onSuccess: (resp) => {
      const id = extractOperationId(resp);
      if (id) setOpId(id);
      else {
        setPendingId(null);
        invalidate("compute-instances", projectId);
      }
    },
    onError: (e) => {
      toast.error(`Диск: ${e instanceof ApiError ? `${e.code}: ${e.message}` : (e as Error).message}`);
      setPendingId(null);
    },
  });
  const busy = mut.isPending || opId !== null;

  const onAttach = () => {
    if (!draftVolume || attachedIds.has(draftVolume)) return;
    setOpTitle("Подключение тома");
    setPendingId(draftVolume);
    mut.mutate({ verb: "attach", volumeId: draftVolume });
    setDraftVolume(undefined);
    setDeviceName("");
    setAutoDelete(false);
  };
  const onDetach = (volumeId: string) => {
    setOpTitle("Отключение тома");
    setPendingId(volumeId);
    mut.mutate({ verb: "detach", volumeId });
  };

  const columns: Column<DiskRow>[] = [
    {
      header: "Том",
      cell: (r) => (r.volume_id ? <CopyableId id={r.volume_id} /> : <Typography.Text type="secondary">—</Typography.Text>),
    },
    { header: "Устройство", cell: (r) => r.device_name || "—" },
    { header: "Роль", cell: (r) => (r.is_boot ? "boot" : "data") },
    { header: "Режим", cell: (r) => r.mode || "—" },
    { header: "Auto-delete", cell: (r) => (r.auto_delete ? "да" : "нет") },
    {
      header: "",
      className: "text-right whitespace-nowrap",
      cell: (r) => {
        const vid = r.volume_id ?? "";
        if (!vid || r.is_boot) return null;
        return pendingId === vid ? (
          <Spin indicator={<LoadingOutlined style={{ fontSize: 12 }} spin />} />
        ) : (
          <Button
            type="text"
            danger
            size="small"
            icon={<DeleteOutlined />}
            aria-label="Отключить"
            onClick={() => onDetach(vid)}
            disabled={busy}
          />
        );
      },
    },
  ];

  return (
    <Space direction="vertical" size={12} style={{ width: "100%" }}>
      <div style={{ display: "flex", gap: 8, alignItems: "center", flexWrap: "wrap" }}>
        <div style={{ minWidth: 260 }}>
          <RefSelect
            refResource="volumes"
            refProjectScoped
            value={draftVolume}
            onChange={(v) => setDraftVolume(v || undefined)}
            refFilter={(row) => !attachedIds.has((row.id as string) ?? "")}
            placeholder="Выбрать том…"
            disabled={busy}
          />
        </div>
        <Input
          placeholder="device_name (опц.)"
          value={deviceName}
          onChange={(e) => setDeviceName(e.target.value)}
          style={{ width: 180 }}
          disabled={busy}
        />
        <Checkbox checked={autoDelete} onChange={(e) => setAutoDelete(e.target.checked)} disabled={busy}>
          Auto-delete
        </Checkbox>
        <Button type="primary" icon={<PlusOutlined />} onClick={onAttach} disabled={!draftVolume || busy}>
          Подключить
        </Button>
      </div>
      {rows.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border p-10 text-center text-sm text-muted-foreground">
          Тома ещё не подключены.
        </div>
      ) : (
        <ResourceTable rows={rows} columns={columns} rowKey={(r) => r.volume_id ?? r.device_name ?? Math.random().toString()} />
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
