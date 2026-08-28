// ChangeDiskTypeDialog — перевод тома на другой тип диска (`:changeDiskType`).
//
// # Почему это действие, а не поле формы правки
//
// Это ПЕРЕМЕЩЕНИЕ ДАННЫХ. Оно длится (том всё это время в MIGRATING), может
// отказать на половине — и тогда данные остаются на исходном типе. Маска правки
// такого выразить не может: она описывает набор изменений, применяемых вместе, и
// «полуприменённого» изменения в её семантике не бывает. Поэтому `disk_type_id`
// объявлен неизменяемым в форме, а смена типа живёт здесь, отдельной кнопкой,
// которая называет предмет вслух.
//
// # Предусловия названы ДО отправки, а не выясняются из отказа
//
// Край отвергает смену, если том не в AVAILABLE/IN_USE, если целевой тип не
// принимает новые тома, если он не предлагается в зоне тома, либо если размер
// тома не укладывается в границы целевого типа. Первое из них известно консоли
// в момент отрисовки — кнопка недоступна и говорит почему. Остальные решает
// владелец, и подменять его ответ мы не пытаемся: подборщик ПОМЕЧАЕТ такие
// типы, а не прячет их (тот же выбор, что у зон, закрытых для размещения, —
// сузить чужие права интерфейс не вправе).

import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { Alert, Button, Modal, Space, Typography } from "antd";
import { SwapOutlined } from "@ant-design/icons";

import { volumesApi } from "@/api/resources";
import { extractOperationId } from "@shared/components/molecules/OperationDialog";
import { OperationToastWatcher } from "@shared/components/molecules/OperationToastWatcher";
import { RefSelect } from "@shared/components/organisms/form/RefSelect";
import { useInvalidateResourceList } from "@shared/lib/use-operation";
import { toast } from "@shared/lib/toast";
import { errorText } from "@shared/lib/error-presentation";

/** Состояния тома, из которых край принимает смену типа диска. Перечень —
 *  предусловие RPC, а не догадка: из прочих состояний он отвечает
 *  FAILED_PRECONDITION. */
const CHANGEABLE_FROM = new Set(["AVAILABLE", "IN_USE"]);

export function changeDiskTypeAllowed(status: unknown): boolean {
  return typeof status === "string" && CHANGEABLE_FROM.has(status);
}

export function ChangeDiskTypeDialog({
  volumeId,
  projectId,
  status,
  currentDiskTypeId,
}: {
  volumeId: string;
  projectId: string | null;
  status: string | undefined;
  currentDiskTypeId: string | undefined;
}) {
  const invalidate = useInvalidateResourceList();
  const [open, setOpen] = useState(false);
  const [target, setTarget] = useState("");
  const [opId, setOpId] = useState<string | null>(null);

  const mut = useMutation({
    mutationFn: () => volumesApi.changeDiskType(volumeId, target),
    onSuccess: (resp) => {
      setOpen(false);
      setTarget("");
      const id = extractOperationId(resp);
      if (id) setOpId(id);
      else invalidate("volumes", projectId);
    },
    onError: (e) => toast.error(`Смена типа диска: ${errorText(e)}`),
  });

  const allowed = changeDiskTypeAllowed(status);
  // Тот же тип — не смена. Отправлять его значило бы просить перенос в то же
  // место: край такое отвергнет, а пользователь не поймёт, что выбрал.
  const canSubmit = allowed && !!target && target !== currentDiskTypeId && !mut.isPending;

  return (
    <>
      <Button
        icon={<SwapOutlined />}
        onClick={() => setOpen(true)}
        disabled={!allowed}
        title={
          allowed
            ? undefined
            : "Сменить тип диска можно, только пока том доступен или подключён к машине. Дождитесь окончания текущей операции."
        }
      >
        Сменить тип диска
      </Button>
      <Modal
        open={open}
        title="Смена типа диска"
        onCancel={() => {
          setOpen(false);
          setTarget("");
        }}
        okText="Перенести"
        cancelText="Отмена"
        okButtonProps={{ disabled: !canSubmit, loading: mut.isPending }}
        onOk={() => mut.mutate()}
        destroyOnHidden
      >
        <Space direction="vertical" size="middle" style={{ width: "100%" }}>
          <Alert
            type="info"
            showIcon
            message="Это перенос данных, а не правка поля"
            description="Пока перенос идёт, том находится в состоянии «Migrating». Если перенос отказал, данные остаются на исходном типе диска. Зона тома не меняется — перенос между зонами делается копией снимка."
          />
          <div>
            <Typography.Text strong>Новый тип диска</Typography.Text>
            <RefSelect refResource="disk-types" value={target} onChange={setTarget} />
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              Целевой тип должен принимать новые тома и предлагаться в зоне тома, а размер тома — укладываться в его
              границы. Типы, выведенные из обращения, помечены в списке.
            </Typography.Text>
          </div>
        </Space>
      </Modal>
      <OperationToastWatcher
        opId={opId}
        title="Смена типа диска"
        onDone={() => {
          setOpId(null);
          invalidate("volumes", projectId);
        }}
      />
    </>
  );
}
