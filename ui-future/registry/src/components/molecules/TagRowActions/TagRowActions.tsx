// TagRowActions — действия строки тега на вкладке «Теги» карточки репозитория.
//
// Здесь живёт то, что прежде несла боковая панель тегов: копирование команды
// `docker pull` и удаление тега. Панель снята (#633) — открыть её было нечем,
// поэтому она не отрисовывалась ни разу, а после появления вкладки (#627) стала
// вторым видом того же списка (правило 3 `ui.md`). Возможности перенесены сюда:
// снять компонент, не перенеся их, было бы потерей функциональности.
//
// ПОЧЕМУ У ТЕГА СВОИ ДЕЙСТВИЯ, А НЕ ОБЩЕЕ МЕНЮ СТРОКИ
//
// Общее меню строит адрес как `spec.apiPath` + поле `id` строки. Тегу не годится
// ни то, ни другое: его адрес несёт ДВЕ подстановки (реестр и репозиторий), а
// поля `id` у него нет вовсе — натуральный ключ тега это сам тег. Общее меню
// поэтому рисовало кнопку, которая отправляла запрос по адресу с литералом
// `{registryId}` и пустым последним сегментом, то есть выглядела работающей и
// не работала.

import { type FC, useEffect, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Button, Popconfirm, Tooltip } from "antd";
import { CopyOutlined, DeleteOutlined } from "@ant-design/icons";
import { registriesApi } from "@/api/resources";
import { extractOperationId } from "@/components/molecules/OperationDialog";
import { useOperation } from "@/lib/use-operation";
import { toast } from "@/lib/toast";
import { errorText } from "@shared/lib/error-presentation";

async function copyText(text: string, okMsg: string) {
  try {
    await navigator.clipboard.writeText(text);
    toast.success(okMsg);
  } catch {
    toast.error("Не удалось скопировать");
  }
}

export const TagRowActions: FC<{
  registryId: string;
  repository: string;
  tag: string;
  /** Позвать после того, как тег удалён, — обновить список. */
  onDone: () => void;
}> = ({ registryId, repository, tag, onDone }) => {
  // Адрес реестра для команды pull. Ключ запроса общий на весь список, поэтому
  // строки его ДЕЛЯТ: react-query отдаёт один ответ всем, и «по запросу на
  // строку» здесь не возникает. Пока ответ не пришёл — конвенционный адрес.
  const { data: reg } = useQuery({
    queryKey: ["registry", "endpoint", registryId],
    queryFn: () => registriesApi.get(registryId),
    enabled: !!registryId,
    staleTime: 60_000,
  });
  const pullBase = (reg?.endpoint as string | undefined) ?? `registry.kacho.local/${registryId}`;
  const pullRef = `${pullBase}/${repository}:${tag}`;

  return (
    // Нажатие гасится НА ОБЁРТКЕ, а не на кнопках. Подтверждение удаления
    // навешивает свой обработчик на обёртку вокруг кнопки, поэтому гашение на
    // самой кнопке не дало бы ему сработать вовсе — окно подтверждения не
    // открывалось бы, и кнопка «удалить» молча ничего не делала. Поймано пробой
    // (первая редакция была именно такой).
    <span
      onClick={(e) => e.stopPropagation()}
      style={{ display: "inline-flex", alignItems: "center", gap: 2, whiteSpace: "nowrap" }}
    >
      <Tooltip title={`Копировать: docker pull ${pullRef}`} placement="topRight">
        <Button
          type="text"
          size="small"
          icon={<CopyOutlined />}
          onClick={() => void copyText(`docker pull ${pullRef}`, "docker pull скопирован")}
          aria-label="Копировать docker pull"
        />
      </Tooltip>
      <TagDeleteAction registryId={registryId} repository={repository} tag={tag} onDone={onDone} />
    </span>
  );
};

// TagDeleteAction — удаление тега (async Operation): иконка + подтверждение →
// deleteTag → extractOperationId → поллинг → обновление списка. Ошибка → toast.
// Удаление необратимо и оно единственная мутация тега (создаёт теги docker push).
function TagDeleteAction({
  registryId,
  repository,
  tag,
  onDone,
}: {
  registryId: string;
  repository: string;
  tag: string;
  onDone: () => void;
}) {
  const [pendingOpId, setPendingOpId] = useState<string | null>(null);
  const { data: op } = useOperation(pendingOpId);

  const mutation = useMutation({
    mutationFn: () => registriesApi.deleteTag(registryId, repository, tag),
    onSuccess: (resp) => {
      const opId = extractOperationId(resp);
      if (opId) {
        setPendingOpId(opId);
      } else {
        toast.success(`Тег ${tag} удалён`);
        onDone();
      }
    },
    onError: (e) => {
      toast.error(`Удалить тег ${tag}: ${errorText(e)}`);
    },
  });

  useEffect(() => {
    if (!pendingOpId || !op?.done) return;
    if (op.error) {
      toast.error(`Удалить тег ${tag}: ${op.error.message ?? "ошибка"}`);
    } else {
      toast.success(`Тег ${tag} удалён`);
      onDone();
    }
    setPendingOpId(null);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [op?.done, op?.error?.code]);

  const pending = mutation.isPending || pendingOpId !== null;

  return (
    <Popconfirm
      title="Удалить тег"
      description={
        <span>
          Тег <b>{tag}</b> будет удалён безвозвратно.
        </span>
      }
      okText="Удалить"
      okButtonProps={{ danger: true, loading: pending }}
      cancelText="Отмена"
      onConfirm={() => mutation.mutate()}
    >
      <Button
        type="text"
        size="small"
        danger
        icon={<DeleteOutlined />}
        loading={pending}
        aria-label="Удалить тег"
      />
    </Popconfirm>
  );
}
