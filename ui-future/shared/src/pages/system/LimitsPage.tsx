// Пределы: раздел АДМИНИСТРАТОРА облака (#364).
//
// Величины меняет только он, и только отсюда. Тенантская витрина («Мои квоты»)
// этой ручки не несёт: кнопка, которая у арендатора всегда заканчивается
// отказом, — обещание возможности, которой у него нет.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЗДЕСЬ ИЗМЕНЯЕМО, А ЧТО НЕТ
//
// Изменяемо ОДНО поле — величина. Тройка «область · её идентификатор · вид» есть
// ИДЕНТИЧНОСТЬ предела: сменить её значит завести другой предел, а не поправить
// этот. Форма поэтому предлагает править только число, а остальное показывает.
//
// ПОЧЕМУ ПОНИЖЕНИЕ НИЖЕ ПОТРЕБЛЕНИЯ РАЗРЕШЕНО. Это решение владельца, а не
// недосмотр: уже созданное не сносится, а новое перестаёт создаваться, пока
// потребление не опустится. Отказывать здесь значило бы лишить администратора
// единственного способа остановить рост.
//
// ШАГ ПОДТВЕРЖДЕНИЯ. Правка предела требует свежего подтверждения личности
// (`required_acr_min = "2"`). Без него край отвечает отказом, и отказ этот НЕ
// про права — про давность входа. Показать его как «нет прав» значило бы
// отправить администратора выяснять то, что и так верно.

import { useMemo, useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { Alert, Button, InputNumber, Modal, Typography } from "antd";
import { api, ApiError } from "@shared/api/client";
import { ErrorResult } from "@shared/components/molecules/ErrorResult";
import { ResourceTable, type Column } from "@shared/components/organisms/ResourceTable";
import { kindLabel } from "@shared/lib/quota-view";
import { clientScope, rowsAreComplete } from "@shared/lib/list-scope";

/** Внутренний слушатель: раздел администратора, не публичная поверхность. */
export const LIMITS_PATH = "/iam/v1/limits";

interface Limit {
  id: string;
  scope: string;
  scope_id: string;
  kind: string;
  value: number;
}

interface LimitsResponse {
  limits?: Limit[];
  next_page_token?: string;
}

/** Область предела словами: у платформенного умолчания идентификатора нет. */
function scopeLabel(l: Limit): string {
  switch (l.scope) {
    case "DEFAULT":
      return "Платформенное умолчание";
    case "ACCOUNT":
      return `Аккаунт ${l.scope_id}`;
    case "PROJECT":
      return `Проект ${l.scope_id}`;
    default:
      return l.scope_id ? `${l.scope} ${l.scope_id}` : l.scope;
  }
}

/**
 * Отказ по давности входа — не отказ по правам.
 *
 * Признак нестрогий (край не выделяет этот случай отдельным кодом), поэтому
 * решает пара: код отказа плюс упоминание подтверждения в тексте. Ошибиться в
 * пользу «нет прав» здесь дороже: администратор пойдёт проверять права, которые
 * у него есть.
 */
function looksLikeStepUp(e: unknown): boolean {
  if (!(e instanceof ApiError)) return false;
  const hay = `${e.message}`.toLowerCase();
  return ["acr", "step-up", "step up", "stepup", "mfa", "assurance", "aal2"].some((n) => hay.includes(n));
}

export default function LimitsPage() {
  const qc = useQueryClient();
  const [editing, setEditing] = useState<Limit | null>(null);
  const [draft, setDraft] = useState<number | null>(null);

  const { data, isLoading, isError, error } = useQuery({
    queryKey: ["admin-limits"],
    queryFn: () => api.list<LimitsResponse>(LIMITS_PATH, { pageSize: "1000" }),
    staleTime: 10_000,
  });

  const save = useMutation({
    mutationFn: (v: { id: string; value: number }) =>
      // Изменяемо одно поле, поэтому маска называет его — и только его. Пустая
      // маска означала бы «применить всё тело» и применила бы к пределу всё,
      // что форма о нём помнит.
      api.update(`${LIMITS_PATH}/${v.id}`, { value: v.value, update_mask: "value" }),
    onSuccess: async () => {
      setEditing(null);
      await qc.invalidateQueries({ queryKey: ["admin-limits"] });
    },
  });

  // Читается ОДНА страница, продолжения у этой таблицы нет: пока за курсором
  // есть ещё, порядок предлагать нельзя (#373). Поле `next_page_token` было
  // объявлено в типе ответа и не читалось ни разу.
  const scope = clientScope(!!data?.next_page_token);
  const rows = useMemo(() => {
    const limits = data?.limits ?? [];
    // Устойчивый порядок: ответ его не обещает, а список поллится.
    return [...limits].sort((a, b) => a.kind.localeCompare(b.kind) || a.scope.localeCompare(b.scope));
  }, [data]);

  const columns: Column<Limit>[] = [
    { header: "Ресурс", className: "font-medium", cell: (l) => kindLabel(l.kind) },
    { header: "Вид", className: "font-mono", cell: (l) => l.kind },
    { header: "Область", cell: (l) => <Typography.Text type="secondary">{scopeLabel(l)}</Typography.Text> },
    { header: "Величина", cell: (l) => <span style={{ fontVariantNumeric: "tabular-nums" }}>{l.value}</span> },
    {
      header: "",
      className: "text-right",
      cell: (l) => (
        <Button
          size="small"
          onClick={() => {
            setEditing(l);
            setDraft(l.value);
          }}
        >
          Изменить
        </Button>
      ),
    },
  ];

  if (isError) return <ErrorResult error={error} />;

  return (
    <>
      <ResourceTable
        rows={rows}
        loading={isLoading}
        rowKey={(l) => l.id}
        columns={columns}
        complete={rowsAreComplete(scope)}
        empty="Пределов нет"
      />

      <Modal
        open={!!editing}
        title={editing ? `Предел: ${kindLabel(editing.kind)}` : ""}
        okText="Сохранить"
        cancelText="Отмена"
        confirmLoading={save.isPending}
        onCancel={() => setEditing(null)}
        onOk={() => {
          if (editing && draft !== null) save.mutate({ id: editing.id, value: draft });
        }}
      >
        {editing && (
          <>
            <Typography.Paragraph type="secondary" style={{ marginBottom: 12 }}>
              {scopeLabel(editing)} · <span style={{ fontFamily: "ui-monospace, monospace" }}>{editing.kind}</span>
            </Typography.Paragraph>
            <InputNumber
              min={0}
              value={draft ?? undefined}
              onChange={(v) => setDraft(typeof v === "number" ? v : null)}
              style={{ width: "100%" }}
              aria-label="Величина предела"
            />
            <Typography.Paragraph type="secondary" style={{ marginTop: 12, marginBottom: 0, fontSize: 12 }}>
              Ноль — законная величина: она означает «нельзя создавать», а не «предел снят».
              Значение ниже текущего потребления допустимо: уже созданное остаётся, новое перестаёт создаваться.
            </Typography.Paragraph>
            {save.isError && (
              <Alert
                type={looksLikeStepUp(save.error) ? "warning" : "error"}
                showIcon
                style={{ marginTop: 12 }}
                message={
                  looksLikeStepUp(save.error)
                    ? "Нужно подтвердить личность заново"
                    : "Величина не изменена"
                }
                description={
                  looksLikeStepUp(save.error)
                    ? "Правка предела требует свежего подтверждения входа. Это не отказ в правах."
                    : <ErrorResult error={save.error} />
                }
              />
            )}
          </>
        )}
      </Modal>
    </>
  );
}
