// InstanceDetailPage — generic ResourceDetailPage для compute Instance плюс
// secondary-actions "Подключить том" / "Отключить том" (verbs :attachDisk /
// :detachDisk) и встроенные Start/Stop/Restart (ops в registry).
//
// Оба verb'а адресуются id ТОМА storage (`vol…`): AttachedDiskSpec.disk и
// DetachInstanceDiskRequest.disk — oneof'ы с армом `volume_id`, поля `disk_id`
// у них нет. Поэтому и picker показывает тома storage, а не ретайренный дубль
// compute-дисков.
//
// Старт/Стоп/Перезапуск рендерятся самим ResourceDetailPage (spec.ops.start/stop/restart
// → POST <apiPath>/{id}:start|:stop|:restart). Здесь добавляем attach/detach над
// tab content через secondaryActions.
//
// network_interfaces рендерятся generic-ResourceDetailPage из payload Instance
// как есть; отдельного linked-NIC-блока со ссылкой на vpc NetworkInterface нет.

import { useCallback, useMemo, useState } from "react";
import { useParams } from "react-router";
import { useMutation } from "@tanstack/react-query";
import { Button, Modal, Space, Typography, Tag } from "antd";
import { PlusOutlined, MinusOutlined } from "@ant-design/icons";
import { ResourceDetailPage } from "@shared/components/organisms/ResourceDetailPage";
import { OperationDialog, extractOperationId } from "@shared/components/molecules/OperationDialog";
import { RefSelect } from "@shared/components/organisms/form/RefSelect";
import { api, ApiError } from "@shared/api/client";
import { REGISTRY, getByPath } from "@shared/lib/resource-registry";
import { useProjectStore } from "@shared/lib/context-store";
import { useInvalidateResourceList } from "@shared/lib/use-operation";
import { toast } from "@shared/lib/toast";

const SPEC = REGISTRY["compute-instances"];

export function InstanceDetailPage() {
  const { uid: instanceId } = useParams();
  const project = useProjectStore((s) => s.project);
  const invalidate = useInvalidateResourceList();

  const [attachOpen, setAttachOpen] = useState(false);
  const [detachOpen, setDetachOpen] = useState(false);
  const [attachVolumeId, setAttachVolumeId] = useState<string | undefined>();
  const [autoDelete, setAutoDelete] = useState(false);
  const [detachVolumeId, setDetachVolumeId] = useState<string | undefined>();
  const [opId, setOpId] = useState<string | null>(null);
  const [opTitle, setOpTitle] = useState("Операция");

  const onOpDone = useCallback(() => {
    setOpId(null);
    invalidate("compute-instances", project?.id);
    invalidate("volumes", project?.id);
  }, [invalidate, project?.id]);

  const attachMut = useMutation({
    mutationFn: () =>
      api.action(`${SPEC.apiPath}/${instanceId}:attachDisk`, {
        // AttachedDiskSpec.disk — oneof {disk_spec | volume_id}, exactly_one. There
        // is no `disk_id`: the edge would drop that name, leaving NO arm set.
        attached_disk_spec: { volume_id: attachVolumeId, auto_delete: autoDelete },
      }),
    onSuccess: (resp) => {
      setAttachOpen(false);
      const id = extractOperationId(resp);
      if (id) {
        setOpTitle("Подключение тома");
        setOpId(id);
      } else {
        invalidate("compute-instances", project?.id);
        invalidate("volumes", project?.id);
      }
    },
    onError: (e) => toast.error(`Подключить том: ${e instanceof ApiError ? `${e.code}: ${e.message}` : e.message}`),
  });

  const detachMut = useMutation({
    // DetachInstanceDiskRequest.disk — oneof {volume_id | device_name}, exactly_one.
    mutationFn: () => api.action(`${SPEC.apiPath}/${instanceId}:detachDisk`, { volume_id: detachVolumeId }),
    onSuccess: (resp) => {
      setDetachOpen(false);
      const id = extractOperationId(resp);
      if (id) {
        setOpTitle("Отключение тома");
        setOpId(id);
      } else {
        invalidate("compute-instances", project?.id);
        invalidate("volumes", project?.id);
      }
    },
    onError: (e) => toast.error(`Отключить том: ${e instanceof ApiError ? `${e.code}: ${e.message}` : e.message}`),
  });

  const secondaryActions = useMemo(
    () => function InstanceSecondaryActions(data: Record<string, unknown>) {
      // AttachedDisk carries `volume_id` — there is no `disk_id` on it, so reading
      // that name yielded an empty id for every attachment and the detach hint
      // below listed nothing.
      const bootDiskId = (getByPath<Record<string, unknown>>(data, "boot_disk")?.volume_id as string | undefined) ?? "";
      const secondary = getByPath<Array<Record<string, unknown>>>(data, "secondary_disks") ?? [];
      const secondaryIds = secondary.map((d) => d.volume_id as string).filter(Boolean);
      return (
        <Space size={8} wrap>
          <Button
            icon={<PlusOutlined />}
            onClick={() => {
              setAttachVolumeId(undefined);
              setAutoDelete(false);
              setAttachOpen(true);
            }}
          >
            Подключить том
          </Button>
          <Button
            icon={<MinusOutlined />}
            disabled={secondaryIds.length === 0}
            onClick={() => {
              setDetachVolumeId(secondaryIds[0]);
              setDetachOpen(true);
            }}
          >
            Отключить том
          </Button>
          {bootDiskId && (
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              Загрузочный диск: <Tag>{bootDiskId}</Tag>
            </Typography.Text>
          )}
        </Space>
      );
    },
    [],
  );

  return (
    <>
      <ResourceDetailPage spec={SPEC} secondaryActions={secondaryActions} />

      <Modal
        title="Подключить том к ВМ"
        open={attachOpen}
        onCancel={() => setAttachOpen(false)}
        onOk={() => attachMut.mutate()}
        okButtonProps={{ disabled: !attachVolumeId, loading: attachMut.isPending }}
        okText="Подключить"
        cancelText="Отмена"
      >
        <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
          <div>
            <Typography.Text>Том</Typography.Text>
            <RefSelect
              refResource="volumes"
              refProjectScoped
              value={attachVolumeId}
              onChange={(v) => setAttachVolumeId(v || undefined)}
            />
          </div>
          <label style={{ display: "flex", alignItems: "center", gap: 8, fontSize: 13 }}>
            <input type="checkbox" checked={autoDelete} onChange={(e) => setAutoDelete(e.target.checked)} />
            Удалять том вместе с ВМ (auto_delete)
          </label>
        </div>
      </Modal>

      <Modal
        title="Отключить том от ВМ"
        open={detachOpen}
        onCancel={() => setDetachOpen(false)}
        onOk={() => detachMut.mutate()}
        okButtonProps={{ disabled: !detachVolumeId, loading: detachMut.isPending, danger: true }}
        okText="Отключить"
        cancelText="Отмена"
      >
        <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            Загрузочный том отключить нельзя. Введите ID одного из дополнительных томов.
          </Typography.Text>
          <div>
            <Typography.Text>ID тома</Typography.Text>
            <input
              value={detachVolumeId ?? ""}
              onChange={(e) => setDetachVolumeId(e.target.value || undefined)}
              placeholder="vol…"
              style={{
                width: "100%",
                padding: "6px 8px",
                fontFamily: "ui-monospace, monospace",
                fontSize: 13,
                background: "transparent",
                border: "1px solid var(--ant-color-border, #383941)",
                borderRadius: 6,
                color: "inherit",
              }}
            />
          </div>
        </div>
      </Modal>

      <OperationDialog opId={opId} title={opTitle} onSuccess={onOpDone} onClose={onOpDone} />
    </>
  );
}
