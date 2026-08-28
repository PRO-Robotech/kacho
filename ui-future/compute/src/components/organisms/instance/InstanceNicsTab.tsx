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
import { ROW_ACTION_TRIGGER } from "@/components/molecules/RowActionsMenu";
import { RefSelect } from "@/components/organisms/form/RefSelect";
import { RefNameLink } from "@/components/molecules/RefNameLink";
import { OperationToastWatcher } from "@/components/molecules/OperationToastWatcher";
import { instancesApi } from "@/api/resources";
import { getByPath } from "@/lib/resource-registry";
import { applyMutationOutcome } from "./mutation-outcome";
import { useInvalidateResourceList } from "@/lib/use-operation";
import { toast } from "@/lib/toast";
import { errorText } from "@shared/lib/error-presentation";

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
  const attachedIds = useMemo(() => new Set(rows.map((r) => r.nic_id).filter((x): x is string => !!x)), [rows]);

  const mut = useMutation({
    mutationFn: (params: { verb: "attach" | "detach"; nicId: string }) =>
      params.verb === "attach"
        ? instancesApi.attachNetworkInterface(instanceId, params.nicId)
        : instancesApi.detachNetworkInterface(instanceId, params.nicId),
    // attach/detach интерфейса объявлены возвращающими Operation — ответ без
    // неё означает, что подтвердить выполнение нечем (см. mutation-outcome.ts).
    onSuccess: (resp) =>
      applyMutationOutcome(resp, true, {
        onOperation: (id) => setOpId(id),
        onSync: () => {
          setPendingId(null);
          invalidate("compute-instances", projectId);
        },
        onViolation: (message) => {
          setPendingId(null);
          toast.error(`Интерфейс: ${message}`);
        },
      }),
    onError: (e) => {
      toast.error(`Интерфейс: ${errorText(e)}`);
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
      // Интерфейс — ресурс со своей карточкой: ссылка «иконка + имя», а не
      // копируемый идентификатор (канон ссылок на чужой ресурс).
      cell: (r) =>
        r.nic_id ? (
          <RefNameLink specId="network-interfaces" refId={r.nic_id} maxChars={28} />
        ) : (
          <Typography.Text type="secondary">—</Typography.Text>
        ),
    },
    {
      // Подсеть — ресурс VPC со своей карточкой, а не строка. Идентификатор
      // `sub-…` человеку не адресован: Kachō адресует ресурсы неизменяемым `id`,
      // а работает человек с именем. Соседняя колонка «NIC» в этой же таблице
      // уже ссылка — плоский текст рядом с ней читается как недоделанный переход.
      header: "Подсеть",
      cell: (r) => <RefNameLink specId="subnets" refId={r.subnet_id} maxChars={28} />,
    },
    { header: "IPv4", cell: (r) => r.primary_v4_address?.address || "—" },
    {
      header: "",
      className: "text-right whitespace-nowrap",
      cell: (r) => {
        const nid = r.nic_id ?? "";
        if (!nid) return null;
        return pendingId === nid ? (
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
      {/* Пустое состояние — та же таблица со своей строкой «пусто», а не своя
          рамка пунктиром: см. тот же довод во вкладке дисков. */}
      <ResourceTable
        rows={rows}
        columns={columns}
        rowKey={(r) => r.nic_id ?? r.index ?? Math.random().toString()}
        // Интерфейсы приезжают полем машины, а не списком у края: курсора
        // здесь нет, набор полон by construction.
        complete
        empty="Сетевые интерфейсы ещё не подключены"
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
