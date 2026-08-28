// CopyToPlacementDialog — копия ресурса storage в ДРУГОЕ размещение.
//
// # Один предмет — один вид (правило 3 канона консоли)
//
// Снимок копируется в другую ЗОНУ, образ — в другой РЕГИОН. Это один предмет:
// «сделать копию там, где её ещё нет». Механика совпадает дословно — глагол
// `:copy`, тело с `project_id` источника, обязательная цель, необязательные
// имя / описание / метки, ответ `Operation`. Отличается ровно ось размещения,
// и она задана ПАРАМЕТРОМ, а не вторым компонентом: два вида одного предмета
// читаются пользователем как два разных предмета, а правка одного молча не
// доезжает до другого.
//
// # Путь к краю строит ВЛАДЕЛЕЦ ресурса
//
// Компонент принимает `submit`, а литерал пути остаётся в `api/resources.ts`
// рядом с константой базы. Причина не косметическая: гейт
// `shared/src/test/console-verb-routes-exist.test.ts` резолвит ГОЛОВУ литерала
// в константу файла и сверяет получившийся путь с `google.api.http` ствола.
// Спрятав путь за проп, объединение вывело бы оба ресурса из-под его надзора —
// и снятый или переименованный глагол остался бы живой кнопкой.
//
// # Что НЕ наследуется от источника и почему это видно в форме
//
// Имя и метки. Имя уникально в паре с проектом, а копия остаётся в проекте
// источника — унаследованное имя столкнулось бы с ним самим. Метки несут смысл,
// который арендатор вложил в ИСХОДНЫЙ ресурс, и перенос данных этот смысл не
// переносит. Поля показаны пустыми, а не заполненными значением источника,
// именно чтобы это было видно до отправки, а не выяснялось из отказа.

import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { Button, Input, Modal, Space, Typography } from "antd";
import { CopyOutlined } from "@ant-design/icons";

import type { Operation } from "@/api/types";
import { extractOperationId } from "@shared/components/molecules/OperationDialog";
import { OperationToastWatcher } from "@shared/components/molecules/OperationToastWatcher";
import { RefSelect } from "@shared/components/organisms/form/RefSelect";
import { LabelsEditor, labelsFromEntries, type LabelEntry } from "@shared/components/organisms/LabelsEditor";
import { useInvalidateResourceList } from "@shared/lib/use-operation";
import { toast } from "@shared/lib/toast";
import { errorText } from "@shared/lib/error-presentation";

export interface CopyToPlacementDialogProps {
  /** Ключ ресурса в реестре — что инвалидировать после успеха. */
  specId: string;
  /** Проект источника: он же проект копии и объект вопроса о правах. */
  projectId: string | null;
  /** Имя поля цели в теле запроса: `target_zone_id` | `target_region_id`. */
  targetField: string;
  /** Ресурс-цель подборщика: `zones` | `regions` (глобальный каталог geo). */
  targetRefResource: string;
  /** Подпись поля цели — словами предмета, а не именем поля. */
  targetLabel: string;
  /** Пояснение под полем цели. */
  targetDescription: string;
  /** Заголовок окна и прогресс-уведомления. */
  title: string;
  /** Подпись кнопки в шапке карточки. */
  buttonLabel: string;
  /** Отправка. Путь строит владелец ресурса — см. шапку файла. */
  submit: (body: Record<string, unknown>) => Promise<{ operation: Operation }>;
}

export function CopyToPlacementDialog({
  specId,
  projectId,
  targetField,
  targetRefResource,
  targetLabel,
  targetDescription,
  title,
  buttonLabel,
  submit,
}: CopyToPlacementDialogProps) {
  const invalidate = useInvalidateResourceList();
  const [open, setOpen] = useState(false);
  const [target, setTarget] = useState("");
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [labels, setLabels] = useState<LabelEntry[]>([]);
  const [opId, setOpId] = useState<string | null>(null);

  const reset = () => {
    setTarget("");
    setName("");
    setDescription("");
    setLabels([]);
  };

  const mut = useMutation({
    mutationFn: () => {
      const body: Record<string, unknown> = { project_id: projectId ?? "", [targetField]: target };
      // Пустые необязательные поля НЕ отправляются. Пустая строка — это ввод, а
      // не его отсутствие: она уехала бы в тело и была бы применена как «имя
      // стереть», хотя пользователь поля не трогал.
      if (name) body.name = name;
      if (description) body.description = description;
      const map = labelsFromEntries(labels);
      if (Object.keys(map).length > 0) body.labels = map;
      return submit(body);
    },
    onSuccess: (resp) => {
      setOpen(false);
      reset();
      const id = extractOperationId(resp);
      if (id) setOpId(id);
      else invalidate(specId, projectId);
    },
    onError: (e) => toast.error(`${title}: ${errorText(e)}`),
  });

  // Проект — объект вопроса о правах: «создать» спрашивают у него, поэтому без
  // проекта в контексте отправлять нечего. Кнопка недоступна, а не молча
  // отправляет пустое значение.
  const canSubmit = !!projectId && !!target && !mut.isPending;

  return (
    <>
      <Button icon={<CopyOutlined />} onClick={() => setOpen(true)} disabled={!projectId}>
        {buttonLabel}
      </Button>
      <Modal
        open={open}
        title={title}
        onCancel={() => {
          setOpen(false);
          reset();
        }}
        okText="Создать копию"
        cancelText="Отмена"
        okButtonProps={{ disabled: !canSubmit, loading: mut.isPending }}
        onOk={() => mut.mutate()}
        destroyOnHidden
      >
        <Space direction="vertical" size="middle" style={{ width: "100%" }}>
          <div>
            <Typography.Text strong>{targetLabel}</Typography.Text>
            <RefSelect refResource={targetRefResource} value={target} onChange={setTarget} />
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              {targetDescription}
            </Typography.Text>
          </div>
          <div>
            <Typography.Text strong>Имя копии</Typography.Text>
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="Можно оставить пустым" />
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              Имя источника не наследуется: оно уникально в пределах проекта, а копия остаётся в проекте источника.
            </Typography.Text>
          </div>
          <div>
            <Typography.Text strong>Описание</Typography.Text>
            <Input.TextArea
              rows={2}
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="Краткое описание копии (опционально)"
            />
          </div>
          <div>
            <Typography.Text strong>Метки</Typography.Text>
            <Typography.Text type="secondary" style={{ fontSize: 12, display: "block", marginBottom: 4 }}>
              Метки источника не наследуются — перенос данных не переносит смысл, который вы в них вложили.
            </Typography.Text>
            <LabelsEditor value={labels} onChange={setLabels} />
          </div>
        </Space>
      </Modal>
      <OperationToastWatcher
        opId={opId}
        title={title}
        onDone={() => {
          setOpId(null);
          invalidate(specId, projectId);
        }}
      />
    </>
  );
}
