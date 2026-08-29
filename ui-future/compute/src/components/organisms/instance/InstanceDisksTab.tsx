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
import { ROW_ACTION_TRIGGER } from "@/components/molecules/RowActionsMenu";
import { RefSelect } from "@/components/organisms/form/RefSelect";
import { RefNameLink } from "@/components/molecules/RefNameLink";
import { OperationToastWatcher } from "@/components/molecules/OperationToastWatcher";
import { instancesApi } from "@/api/resources";
import { getByPath } from "@/lib/resource-registry";
import { useInvalidateResourceList } from "@/lib/use-operation";
import { toast } from "@/lib/toast";
import { BoolFact } from "@/components/atoms/BoolFact";
import { errorText } from "@shared/lib/error-presentation";
import { resolveMutationResponse } from "@shared/lib/operation-outcome";
import { REGISTRY } from "@/lib/resource-registry";

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
  const attachedIds = useMemo(() => new Set(rows.map((r) => r.volume_id).filter((x): x is string => !!x)), [rows]);

  const mut = useMutation({
    mutationFn: (params: { verb: "attach" | "detach"; volumeId: string }) =>
      params.verb === "attach"
        ? instancesApi.attachDisk(instanceId, params.volumeId, deviceName || undefined, autoDelete)
        : instancesApi.detachDisk(instanceId, params.volumeId),
    onSuccess: (resp) => {
      // Разбор ОБЩИЙ: ответ без операции у ресурса, который её обещал, — не
      // синхронный успех, а нарушение контракта. Прежний свой ключ отвечал
      // `string | null`, и такой ответ молча обновлял список — том выглядел
      // подключённым, хотя подтвердить это было нечем.
      const resolved = resolveMutationResponse(resp, REGISTRY["compute-instances"]?.mutationsReturnOperation !== false);
      if (resolved.kind === "operation") setOpId(resolved.opId);
      else if (resolved.kind === "violation") {
        toast.error(`Диск: ${resolved.message}`);
        setPendingId(null);
      } else {
        setPendingId(null);
        invalidate("compute-instances", projectId);
      }
    },
    onError: (e) => {
      toast.error(`Диск: ${errorText(e)}`);
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
      // Том — ресурс со своей карточкой: ссылка «иконка + имя».
      cell: (r) =>
        r.volume_id ? (
          <RefNameLink specId="volumes" refId={r.volume_id} maxChars={28} />
        ) : (
          <Typography.Text type="secondary">—</Typography.Text>
        ),
    },
    { header: "Устройство", cell: (r) => r.device_name || "—" },
    {
      // Роль диска — булево свойство, названное СЛЕДСТВИЕМ. Прежде здесь стояли
      // служебные слова `boot` и `data`: латиница посреди русского интерфейса,
      // из которой не следует ни что машина грузится с этого тома, ни что она
      // грузится не с него. Выделена та сторона, о которой стоит знать: с
      // загрузочного тома машину не отключить, и строка ниже это подтверждает —
      // столбца действий у неё нет.
      header: "Роль",
      cell: (r) => <BoolFact value={r.is_boot} yes="Загрузочный" no="Дополнительный" yesTone="active" />,
    },
    { header: "Режим", cell: (r) => r.mode || "—" },
    {
      header: "При удалении машины",
      cell: (r) => (
        <BoolFact value={r.auto_delete} yes="Том удаляется" no="Том остаётся" yesTone="attention" yesGlyph="warn" />
      ),
    },
    {
      header: "",
      className: "text-right whitespace-nowrap",
      cell: (r) => {
        const vid = r.volume_id ?? "";
        if (!vid || r.is_boot) return null;
        return pendingId === vid ? (
          // Ожидание занимает РОВНО место ручки: иначе строка на время операции
          // становится другой высоты, и таблица дёргается на каждом отключении.
          <span
            style={{ display: "inline-flex", width: 30, height: 30, alignItems: "center", justifyContent: "center" }}
          >
            <Spin indicator={<LoadingOutlined style={{ fontSize: 12 }} spin />} />
          </span>
        ) : (
          // Геометрия ручки строки — ОДНА на продукт (`ROW_ACTION_TRIGGER`): 30×30,
          // радиус 6. Прежде здесь стоял `size="small"`, то есть высота, которая
          // ездит вместе с общей высотой элементов управления, — и столбец без
          // данных то поднимал строку, то нет. Цвет снимается: он принадлежит
          // `danger` (действие снимает связь), а не общему тону значка.
          <Button
            type="text"
            danger
            style={{ ...ROW_ACTION_TRIGGER, color: undefined }}
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
          placeholder="имя устройства (необязательно)"
          value={deviceName}
          onChange={(e) => setDeviceName(e.target.value)}
          style={{ width: 180 }}
          disabled={busy}
        />
        <Checkbox checked={autoDelete} onChange={(e) => setAutoDelete(e.target.checked)} disabled={busy}>
          Автоудаление
        </Checkbox>
        <Button type="primary" icon={<PlusOutlined />} onClick={onAttach} disabled={!draftVolume || busy}>
          Подключить
        </Button>
      </div>
      {/* Пустое состояние — та же таблица со своей строкой «пусто», а не своя
          рамка пунктиром. Своя рамка была вторым видом одного предмета: в
          соседних таблицах продукта пустота выглядит иначе, и переход между
          вкладками читался как переход в другое место продукта. Заодно шапка
          колонок остаётся на месте — видно, ЧТО именно не подключено. */}
      <ResourceTable
        rows={rows}
        columns={columns}
        rowKey={(r) => r.volume_id ?? r.device_name ?? Math.random().toString()}
        // Диски приезжают полем самой машины, а не отдельным списком: курсора
        // здесь нет, набор полон by construction.
        complete
        empty="Тома ещё не подключены"
      />
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
