// Подтверждение действия-глагола со строки списка (`POST …/{id}:verb`).
//
// Почему отдельным окном, а не `Popconfirm` у пункта меню: пункт живёт в
// портале выпадающего меню, и подтверждение внутри него закрывается вместе с
// ним — то есть необратимое действие подтверждалось бы всплывающей подсказкой,
// которая исчезает от любого движения мыши. Окно принадлежит строке, а не меню.
//
// Разбор ответа берётся у ЕДИНОГО механизма (`resolveMutationResponse`), а не
// пишется здесь заново: второй разборщик разошёлся бы с первым молча — и
// разошёлся бы именно там, где расхождение не видно, потому что на валидном
// ответе оба отвечают одинаково.
//
// Мутации Kachō отвечают `Operation` (ban #9). Ответ без операции — не
// «выполнено синхронно», а нарушение контракта: подтверждать нечем, и окно
// говорит об этом вместо того, чтобы отрапортовать «готово».
//
// Окно НЕ закрывается в момент отправки. Исход асинхронной мутации становится
// известен позже — когда опрос операции дойдёт до `done`; закрытие «по отправке»
// увело бы оператора с экрана раньше вердикта, а наблюдателя операции сняло бы
// вместе с окном, и вердикта не осталось бы вовсе.

import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { Modal, Typography } from "antd";
import { api } from "@shared/api/client";
import { OperationToastWatcher } from "@shared/components/molecules/OperationToastWatcher";
import { errorText } from "@shared/lib/error-presentation";
import { resolveMutationResponse } from "@shared/lib/operation-outcome";
import { toast } from "@shared/lib/toast";
import { useInvalidateResourceList } from "@shared/lib/use-operation";
import type { RowVerbState } from "@shared/lib/resource-spec";

interface Props {
  state: RowVerbState;
  /** id ресурса в реестре — для сброса кэша его списка. */
  resourceId: string;
  projectId: string | null;
  onClose: () => void;
}

export function RowVerbDialog({ state, resourceId, projectId, onClose }: Props) {
  const invalidate = useInvalidateResourceList();
  const [opId, setOpId] = useState<string | null>(null);

  const mutation = useMutation({
    mutationFn: () => api.action(state.path),
    onSuccess: (resp) => {
      const resolved = resolveMutationResponse(resp, true);
      if (resolved.kind === "operation") {
        setOpId(resolved.opId);
        return;
      }
      if (resolved.kind === "violation") {
        // На отказе окно остаётся: закрытие — жест успеха, и уводить оператора
        // от причины значит прятать её.
        toast.error(`${state.progressTitle}: ${resolved.message}`);
        return;
      }
      // Ветка недостижима при `expectOperation = true`, но обязана быть
      // ИСХОДОМ, а не падением: список сбрасывается, окно уходит.
      invalidate(resourceId, projectId);
      onClose();
    },
    onError: (e) => toast.error(`${state.progressTitle}: ${errorText(e)}`),
  });

  const busy = mutation.isPending || opId !== null;

  return (
    <>
      <Modal
        open
        title={state.confirmTitle}
        okText={state.okText}
        cancelText="Отмена"
        okButtonProps={{ danger: state.danger, loading: busy }}
        onOk={() => {
          if (busy) return;
          mutation.mutate();
        }}
        onCancel={() => {
          if (busy) return;
          onClose();
        }}
      >
        <Typography.Paragraph>{state.confirmText}</Typography.Paragraph>
      </Modal>
      <OperationToastWatcher
        opId={opId}
        title={state.progressTitle}
        onDone={() => {
          setOpId(null);
          invalidate(resourceId, projectId);
          onClose();
        }}
      />
    </>
  );
}
