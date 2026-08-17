// Подтверждение действия-глагола со строки списка (`POST …/{id}:verb`).
//
// Почему отдельным окном, а не `Popconfirm` у пункта меню: пункт живёт в
// портале выпадающего меню, и подтверждение внутри него закрывается вместе с
// ним — то есть необратимое действие подтверждалось бы всплывающей подсказкой,
// которая исчезает от любого движения мыши. Окно принадлежит строке, а не меню.
//
// Исход сообщает ЕДИНЫЙ механизм (`use-signalled-mutation`): он же разбирает
// ответ, опрашивает операцию и держит кнопку занятой до вердикта. Своя проводка
// здесь была бы вторым механизмом об одном предмете — и разошлась бы с первым
// молча, потому что на валидном ответе оба отвечают одинаково.
//
// Формулировка исхода — СВОЯ, и это не украшение: три причастия CRUD говорят про
// ресурс («пользователь обновлён»), а глагол — про предмет действия («участие
// запрещено»). Подмена сказала бы о ресурсе то, чего не было.
//
// Окно НЕ закрывается в момент отправки. Исход асинхронной мутации становится
// известен позже — когда опрос операции дойдёт до `done`; закрытие «по отправке»
// увело бы оператора с экрана раньше вердикта.

import { Modal, Typography } from "antd";
import { api } from "@shared/api/client";
import { useInvalidateResourceList } from "@shared/lib/use-operation";
import { useSignalledMutation } from "@shared/lib/use-signalled-mutation";
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

  const mutation = useSignalledMutation({
    signal: {
      succeeded: `${state.progressTitle}: готово`,
      failed: (reason) => `${state.progressTitle}: ${reason}`,
    },
    // Глаголы Kachō отвечают `Operation` (ban #9). Ответ без операции — не
    // «выполнено синхронно», а нарушение контракта: подтверждать нечем.
    expectOperation: true,
    mutationFn: () => api.action(state.path),
    onSucceeded: () => {
      invalidate(resourceId, projectId);
      onClose();
    },
    // На отказе окно НЕ закрывается: закрытие — жест успеха, и уводить
    // оператора от причины отказа значит прятать её.
  });

  return (
    <Modal
      open
      title={state.confirmTitle}
      okText={state.okText}
      cancelText="Отмена"
      okButtonProps={{ danger: state.danger, loading: mutation.pending }}
      onOk={() => {
        if (mutation.pending) return;
        mutation.run();
      }}
      onCancel={() => {
        if (mutation.pending) return;
        onClose();
      }}
    >
      <Typography.Paragraph>{state.confirmText}</Typography.Paragraph>
    </Modal>
  );
}
