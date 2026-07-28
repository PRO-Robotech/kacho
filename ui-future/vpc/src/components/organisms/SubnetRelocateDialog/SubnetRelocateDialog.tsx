// SubnetRelocateDialog — Move Subnet в другую зону через POST /vpc/v1/subnets/{id}:relocate.

import { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Alert, Modal, Select, Typography, Form } from "antd";
import { extractOperationId } from "@shared/components/molecules/OperationDialog";
import { OperationToastWatcher } from "@shared/components/molecules/OperationToastWatcher";
import { ApiError, api } from "@shared/api/client";
import { useInvalidateResourceList } from "@shared/lib/use-operation";
import { toast } from "@shared/lib/toast";

interface ZoneRow {
  id: string;
  name?: string;
  region_id?: string;
}

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  subnetId: string;
  subnetName: string;
  currentZone: string;
  projectId?: string | null;
}

export function SubnetRelocateDialog({ open, onOpenChange, subnetId, subnetName, currentZone, projectId }: Props) {
  const [targetZone, setTargetZone] = useState<string | undefined>();
  const [opId, setOpId] = useState<string | null>(null);
  const invalidate = useInvalidateResourceList();

  const { data, isLoading } = useQuery({
    queryKey: ["zones-relocate"],
    queryFn: () => api.list<{ zones: ZoneRow[] }>("/geo/v1/zones"),
    enabled: open,
    staleTime: 30_000,
  });

  const candidates = (data?.zones ?? []).filter((z) => z.id !== currentZone);

  const mutation = useMutation({
    mutationFn: () =>
      api.action(`/vpc/v1/subnets/${subnetId}:relocate`, {
        destination_zone_id: targetZone,
      }),
    onSuccess: (resp) => {
      const id = extractOperationId(resp);
      if (id) setOpId(id);
      else {
        invalidate("subnets", projectId ?? null);
        onOpenChange(false);
      }
    },
    onError: (err) => {
      const m = err instanceof ApiError ? `${err.code}: ${err.message}` : (err as Error).message;
      toast.error(`Перенос подсети ${subnetName}: ${m}`);
    },
  });

  return (
    <>
      <Modal
        open={open}
        onCancel={() => {
          setTargetZone(undefined);
          onOpenChange(false);
        }}
        onOk={() => mutation.mutate()}
        okText="Перенести"
        okButtonProps={{
          disabled: !targetZone || mutation.isPending || opId !== null,
          loading: mutation.isPending || opId !== null,
        }}
        cancelText="Отменить"
        title="Перенести подсеть в другую зону"
      >
        <div style={{ marginBottom: 12 }}>
          <Typography.Text type="secondary">Подсеть: </Typography.Text>
          <Typography.Text strong>{subnetName}</Typography.Text>
          <br />
          <Typography.Text type="secondary">Текущая зона: </Typography.Text>
          <Typography.Text code>{currentZone}</Typography.Text>
        </div>
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 12 }}
          message={
            <span>
              Перенос невозможен, если в подсети уже есть IP-адреса — backend вернёт <code>FailedPrecondition</code>.
            </span>
          }
        />
        <Form layout="vertical">
          <Form.Item label="Целевая зона" required>
            <Select
              value={targetZone}
              onChange={(v) => setTargetZone(v)}
              loading={isLoading}
              placeholder="Выберите зону"
              options={candidates.map((z) => ({
                value: z.id,
                label: z.name ? `${z.id} — ${z.name}` : z.id,
              }))}
              notFoundContent={isLoading ? "Загрузка зон…" : "Нет доступных целевых зон"}
            />
          </Form.Item>
        </Form>
      </Modal>

      <OperationToastWatcher
        opId={opId}
        title={`Перенос подсети ${subnetName}`}
        onDone={(success) => {
          setOpId(null);
          invalidate("subnets", projectId ?? null);
          if (success) onOpenChange(false);
        }}
      />
    </>
  );
}
