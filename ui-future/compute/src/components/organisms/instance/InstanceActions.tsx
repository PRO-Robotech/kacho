// InstanceActions — доменные lifecycle-действия машины в шапке «Обзора»:
// Запустить / Остановить / Перезапустить (async :start / :stop / :restart →
// Operation-poll). Доступность действий зависит от текущего статуса машины.

import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { Button, Space } from "antd";
import { CaretRightOutlined, PoweroffOutlined, ReloadOutlined } from "@ant-design/icons";
import { instancesApi } from "@/api/resources";
import { extractOperationId } from "@/components/molecules/OperationDialog";
import { OperationToastWatcher } from "@/components/molecules/OperationToastWatcher";
import { useInvalidateResourceList } from "@/lib/use-operation";
import { toast } from "@/lib/toast";
// Имя ресурса в тексте отказа — из единственного источника: «Инстанс» было
// вторым именем машины в продукте (везде остальное — «Виртуальная машина»).
import { ENTITIES } from "@shared/lib/entity-names";
import { errorText } from "@shared/lib/error-presentation";
import { REGISTRY } from "@/lib/resource-registry";

// Подпись операции склоняется — «Запуск виртуальной машины», а не «Запуск
// виртуальная машина», — поэтому берётся родительный падеж, объявленный реестром
// (то самое поле, ради которого он там и заведён). Прежде здесь стояло «Запуск
// инстанса»: ВТОРОЕ имя того же предмета, при том что шапка этого же файла
// объявляет запрет на него и импортирует ради него единый источник имён.
const VM_GENITIVE = (REGISTRY["compute-instances"]?.genitive ?? ENTITIES.instances.singular).toLowerCase();

type Verb = "start" | "stop" | "restart";

export function InstanceActions({
  instanceId,
  status,
  projectId,
}: {
  instanceId: string;
  status: string | undefined;
  projectId: string | null;
}) {
  const invalidate = useInvalidateResourceList();
  const [opId, setOpId] = useState<string | null>(null);
  const [opTitle, setOpTitle] = useState("Операция");

  const mut = useMutation({
    mutationFn: (verb: Verb) => instancesApi[verb](instanceId),
    onSuccess: (resp) => {
      const id = extractOperationId(resp);
      if (id) setOpId(id);
      else invalidate("compute-instances", projectId);
    },
    onError: (e) => toast.error(`${ENTITIES.instances.singular}: ${errorText(e)}`),
  });
  const busy = mut.isPending || opId !== null;

  const run = (verb: Verb, title: string) => {
    setOpTitle(title);
    mut.mutate(verb);
  };

  // Статусная логика: STOPPED → можно запустить; RUNNING → остановить/перезапустить.
  const isStopped = status === "STOPPED";
  const isRunning = status === "RUNNING";

  return (
    <Space>
      <Button
        icon={<CaretRightOutlined />}
        onClick={() => run("start", `Запуск ${VM_GENITIVE}`)}
        disabled={busy || !isStopped}
      >
        Запустить
      </Button>
      <Button
        icon={<PoweroffOutlined />}
        onClick={() => run("stop", `Остановка ${VM_GENITIVE}`)}
        disabled={busy || !isRunning}
      >
        Остановить
      </Button>
      <Button
        icon={<ReloadOutlined />}
        onClick={() => run("restart", `Перезапуск ${VM_GENITIVE}`)}
        disabled={busy || !isRunning}
      >
        Перезапустить
      </Button>
      <OperationToastWatcher
        opId={opId}
        title={opTitle}
        onDone={() => {
          setOpId(null);
          invalidate("compute-instances", projectId);
        }}
      />
    </Space>
  );
}
