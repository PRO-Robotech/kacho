// InstanceActions — доменные lifecycle-действия инстанса в шапке «Обзора»:
// Запустить / Остановить / Перезапустить (async :start / :stop / :restart →
// Operation-poll). Доступность действий зависит от текущего статуса инстанса.

import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { Button, Space } from "antd";
import { CaretRightOutlined, PoweroffOutlined, ReloadOutlined } from "@ant-design/icons";
import { ApiError } from "@/api/client";
import { instancesApi } from "@/api/resources";
import { extractOperationId } from "@/components/molecules/OperationDialog";
import { OperationToastWatcher } from "@/components/molecules/OperationToastWatcher";
import { useInvalidateResourceList } from "@/lib/use-operation";
import { toast } from "@/lib/toast";

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
    onError: (e) =>
      toast.error(`Инстанс: ${e instanceof ApiError ? `${e.code}: ${e.message}` : (e as Error).message}`),
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
        onClick={() => run("start", "Запуск инстанса")}
        disabled={busy || !isStopped}
      >
        Запустить
      </Button>
      <Button icon={<PoweroffOutlined />} onClick={() => run("stop", "Остановка инстанса")} disabled={busy || !isRunning}>
        Остановить
      </Button>
      <Button
        icon={<ReloadOutlined />}
        onClick={() => run("restart", "Перезапуск инстанса")}
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
