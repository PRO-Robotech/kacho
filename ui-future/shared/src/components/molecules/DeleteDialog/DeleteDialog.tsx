// DeleteDialog — confirm-modal с реальным DELETE и polling Operation
// прямо из диалога (Удалить-кнопка остаётся в loading-состоянии до op.done).
//
// Диалог отвечает на ДВА разных вопроса, и путать их нельзя:
//   · «что блокирует удаление» — дерево связанных ресурсов (DependencyTreePanel)
//     у ресурсов с RESTRICT-детьми: край откажет, пока дети есть;
//   · «что исчезнет, если удаление пройдёт» — `IRREVERSIBLE_DELETE` ниже.
// Первое означает, что терять нечего; второе — что терять есть что. Прежде эти
// два вопроса были склеены в один критерий, и ритуал вышел перевёрнутым (#1606).

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Button, Modal, Typography, Input, theme } from "antd";
import { DeleteOutlined, ReloadOutlined } from "@ant-design/icons";
import { api } from "@shared/api/client";
import { DopplerButton } from "@shared/components/molecules/DopplerButton";
import { useInvalidateResourceList } from "@shared/lib/use-operation";
import { DependencyTreePanel } from "@shared/components/organisms/DependencyTreePanel";
import { hasDependencyResolver, loadDependents } from "@shared/lib/dependency-graph";
import { genderOfLabel } from "@shared/lib/mutation-signal";
import { useSignalledMutation } from "@shared/lib/use-signalled-mutation";

/**
 * НЕОБРАТИМОЕ УДАЛЕНИЕ: ключ — ресурс, значение — ЧТО ИМЕННО исчезнет (#1606).
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * КРИТЕРИЙ — «ИСЧЕЗАЮТ ЛИ ДАННЫЕ», А НЕ «ЕСТЬ ЛИ ЗАВИСИМОСТИ»
 *
 * Здесь стоял перевёрнутый критерий: имя руками спрашивали у сети, таблицы
 * маршрутов и группы безопасности, а комментарий объявлял его как «необратимо +
 * каскадные RESTRICT-зависимости». RESTRICT-зависимость — ровно то, что делает
 * удаление НЕ разрушительным: пока есть дети, край отказывает, а без детей
 * ресурс воссоздают той же формой за минуту. Терять там нечего.
 *
 * Ровно наоборот было у тома, снимка, образа, машины и репозитория реестра:
 * содержимое исчезает безвозвратно, а хватало одного клика. Самая дорогая
 * ошибка в консоли стоила меньше усилий, чем самая безобидная.
 *
 * ПОЧЕМУ НЕ «СПРАШИВАТЬ ИМЯ ВЕЗДЕ». Ритуал — сигнал, и стоит он ровно столько,
 * сколько стоит его редкость. Спрашивая имя у каждого удаления, продукт
 * перестал бы отличать опасное от рядового, и ввод имени выродился бы в
 * механическое движение — тот же изъян, что у порога тревоги, срабатывающего на
 * штатном состоянии.
 *
 * ПОЧЕМУ ЗДЕСЬ ПРИЧИНА, А НЕ ПРИЗНАК. Признак выводится из наличия причины,
 * поэтому «ресурс опасен» и «вот что он теряет» — ОДНА запись. Двумя записями
 * они разошлись бы при первой правке, и опасным остался бы ресурс, которому
 * терять нечего, — то есть вернулось бы ровно то состояние, из которого правило
 * выведено.
 *
 * Держит критерий гейт `shared/src/test/console-delete-ritual-tracks-risk`:
 * множества «требует имя» и «защищён RESTRICT» обязаны НЕ пересекаться, каждая
 * запись обязана называть живой удаляемый ресурс и называть предмет потери.
 */
export const IRREVERSIBLE_DELETE: Record<string, string> = {
  volumes: "Данные тома будут стёрты. Восстановить их можно только из снимка, если он есть.",
  snapshots: "Снимок исчезнет вместе с сохранённым в нём состоянием диска. Тома, созданные из него, останутся.",
  images: "Образ и его содержимое исчезнут. Машины, уже развёрнутые из него, останутся, но развернуть новую будет неоткуда.",
  "compute-instances": "Машина будет выключена и удалена. Данные её локальных дисков исчезнут; подключённые тома останутся.",
  registries: "Реестр исчезнет вместе со всеми репозиториями, тегами и слоями. Копии край не держит.",
};

// РЕПОЗИТОРИЯ ЗДЕСЬ НЕТ, и это не пропуск: консоль его не удаляет вовсе
// (`ops.delete: false` у спеки). Запись про ресурс, которого не удаляют,
// инертна — выглядит защитой и не срабатывает ни разу; её и отверг гейт, когда
// она сюда попала. Появится удаление — запись заводится вместе с ним, и текст
// у неё уже есть: страница документации реестра называет это опасностью
// («теги и слои уходят вместе с ним, и отменить это нечем»).

/**
 * Требует ли удаление ввода имени руками.
 *
 * Отдельного перечня нет намеренно: признак — это наличие причины, см. выше.
 */
export function requiresNameConfirm(specId: string): boolean {
  return Object.prototype.hasOwnProperty.call(IRREVERSIBLE_DELETE, specId);
}

/** Что исчезнет вместе с ресурсом, либо `undefined` у обратимого удаления. */
export function deleteConsequence(specId: string): string | undefined {
  return IRREVERSIBLE_DELETE[specId];
}

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Полный API path: /vpc/v1/networks/<id>. */
  apiPath: string;
  /** ID ресурса в registry — для invalidate. */
  resourceId: string;
  /** Verbose имя для UI. */
  resourceLabel: string;
  name: string;
  /** Project ID для invalidate соответствующих list-query (и для дерева связей). */
  projectId?: string | null;
  /** Callback после успешного запуска (navigate на list etc.). */
  onSuccess?: () => void;
  /** Если true — требуется ввести имя ресурса для подтверждения. */
  requireNameConfirm?: boolean;
  /** Ресурс объявил, что мутации отвечают Operation (`spec.mutationsReturnOperation`).
   *  Тогда ответ без операции — не «удалено синхронно», а нечем подтверждать. */
  expectOperation?: boolean;
}

export function DeleteDialog({
  open,
  onOpenChange,
  apiPath,
  resourceId,
  resourceLabel,
  name,
  projectId,
  onSuccess,
  requireNameConfirm,
  expectOperation,
}: Props) {
  const { token } = theme.useToken();
  const [confirmText, setConfirmText] = useState("");
  const invalidate = useInvalidateResourceList();

  const resourceUid = useMemo(() => apiPath.split("/").filter(Boolean).pop() ?? "", [apiPath]);
  const showDeps = hasDependencyResolver(resourceId);
  // Что исчезнет вместе с ресурсом — берётся у ТОГО ЖЕ объявления, что решает
  // про ввод имени. Двух источников тут нет by construction.
  const consequence = deleteConsequence(resourceId);
  const depsQuery = useQuery({
    queryKey: ["delete-deps", resourceId, resourceUid, projectId ?? ""],
    queryFn: () => loadDependents(resourceId, { id: resourceUid, project_id: projectId ?? null }),
    enabled: open && showDeps && !!resourceUid,
    staleTime: 0,
    gcTime: 0,
  });

  // Исход — через единый механизм: разбор ответа, опрос операции и сообщение
  // одной формой на все три исхода (`use-signalled-mutation`).
  const mutation = useSignalledMutation({
    verb: "delete",
    subject: { label: resourceLabel, gender: genderOfLabel(resourceLabel) ?? "m", name },
    expectOperation: expectOperation === true,
    mutationFn: () => api.delete(apiPath),
    onSucceeded: () => {
      invalidate(resourceId, projectId ?? null);
      onOpenChange(false);
      setConfirmText("");
      onSuccess?.();
    },
    // На отказе окно НЕ закрывается. Закрытие — жест успеха: за ним обновляется
    // список и пользователь уходит уверенным, что ресурса больше нет. Отказ
    // обязан оставить его там, где он нажал, рядом с причиной.
  });

  const pending = mutation.pending;
  const canConfirm = !requireNameConfirm || confirmText.trim() === name;

  const displayName = name || "(без имени)";

  const close = () => {
    if (pending) return;
    onOpenChange(false);
    setConfirmText("");
  };

  const left = (
    <div style={{ display: "flex", flexDirection: "column", gap: 18, flex: 1, minWidth: 280 }}>
      {/* Header: danger-икон + caps-метка типа + имя (ellipsis) + подзаголовок. */}
      <div style={{ display: "flex", gap: 14, alignItems: "flex-start" }}>
        <div
          style={{
            width: 46,
            height: 46,
            borderRadius: 8,
            flexShrink: 0,
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            fontSize: 21,
            // Тон опасности — из набора состояний продукта, а не свой красный:
            // прежний цвет не менялся ни в одной теме, а на светлой ещё и не
            // совпадал с цветом кнопки «Удалить» в том же окне.
            color: "var(--status-error-fg)",
            background: "var(--status-error-bg)",
            border: "1px solid var(--status-error-border)",
          }}
        >
          <DeleteOutlined />
        </div>
        <div style={{ minWidth: 0, flex: 1, paddingTop: 1 }}>
          <div
            style={{
              fontSize: 11,
              fontWeight: 600,
              letterSpacing: "0.06em",
              textTransform: "uppercase",
              color: token.colorError,
            }}
          >
            Удаление · {resourceLabel}
          </div>
          <Typography.Title
            level={5}
            ellipsis={{ tooltip: displayName }}
            style={{ margin: "3px 0 6px", fontWeight: 600, color: "var(--kc-text)" }}
          >
            {displayName}
          </Typography.Title>
          {/* ПРЕДУПРЕЖДЕНИЕ НАЗЫВАЕТ ПРЕДМЕТ ПОТЕРИ, а не повторяет слово
              «безвозвратно». Общая фраза одинакова у тома и у пустой сети,
              поэтому не сообщает ничего: читатель не может по ней решить, чем
              он рискует. Там, где терять нечего, она и остаётся общей — это
              честно и отличимо от предупреждения по существу (#1606). */}
          <Typography.Text
            type="secondary"
            style={{ fontSize: 13, lineHeight: 1.55, display: "block" }}
            data-testid="delete-consequence"
          >
            {consequence ?? "Ресурс будет удалён безвозвратно. Действие необратимо."}
          </Typography.Text>
        </div>
      </div>

      {requireNameConfirm && (
        <div
          style={{
            display: "flex",
            flexDirection: "column",
            gap: 7,
            padding: 12,
            borderRadius: 10,
            background: token.colorFillQuaternary,
            border: `1px solid ${token.colorBorderSecondary}`,
          }}
        >
          <Typography.Text style={{ fontSize: 12.5, color: "var(--kc-text-secondary)", lineHeight: 1.5 }}>
            Подтвердите удаление — введите имя ресурса
          </Typography.Text>
          <Input
            value={confirmText}
            onChange={(e) => setConfirmText(e.target.value)}
            placeholder={name}
            status={confirmText && !canConfirm ? "error" : undefined}
            allowClear
            autoFocus
          />
        </div>
      )}
    </div>
  );

  return (
    <Modal
      open={open}
      // Стабильная ширина: 820 с панелью зависимостей (две колонки), иначе 560.
      // Зависит только от наличия resolver'а ресурса — не «прыгает» в рамках
      // одного ресурса.
      width={showDeps ? 820 : 560}
      onCancel={close}
      title={null}
      // KAC-246: крестик не нужен — есть кнопка «Отмена».
      closable={false}
      destroyOnClose
      footer={[
        showDeps ? (
          <Button
            key="refresh"
            icon={<ReloadOutlined />}
            onClick={() => depsQuery.refetch()}
            disabled={pending || depsQuery.isFetching}
            style={{ marginInlineEnd: "auto" }}
          >
            Обновить связи
          </Button>
        ) : null,
        <Button key="cancel" onClick={close} disabled={pending}>
          Отмена
        </Button>,
        <DopplerButton
          key="ok"
          danger
          type="primary"
          onClick={() => mutation.run()}
          pulsing={pending}
          disabled={!canConfirm}
        >
          Удалить
        </DopplerButton>,
      ]}
    >
      {showDeps ? (
        <div style={{ display: "flex", gap: 16, alignItems: "flex-start" }}>
          {left}
          <DependencyTreePanel
            nodes={depsQuery.data ?? []}
            loading={depsQuery.isLoading || depsQuery.isFetching}
            error={depsQuery.error ? depsQuery.error.message : null}
          />
        </div>
      ) : (
        left
      )}
    </Modal>
  );
}
