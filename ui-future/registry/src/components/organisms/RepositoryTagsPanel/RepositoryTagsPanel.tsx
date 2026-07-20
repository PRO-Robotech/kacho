// RepositoryTagsPanel — встроенная боковая панель тегов репозитория. Живёт
// ВНУТРИ зоны контента (сиблинг таблицы репозиториев): раздвигает таблицу вбок,
// не оверлей.
//
// UX: вместо широкой таблицы — вертикальный список карточек тегов (компактно на
// узком экране, без горизонтального скролла). Каждая карточка: тег + короткий
// digest (9 симв., копируемый) + размер/push/pull + кнопка копирования
// `docker pull`. Read-only, кроме DeleteTag (async Operation).

import { type FC, useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Button, Empty, Popconfirm, Skeleton, Tag, Tooltip, Typography } from "antd";
import {
  ClockCircleOutlined,
  CloseOutlined,
  CopyOutlined,
  DeleteOutlined,
  DownloadOutlined,
  HddOutlined,
  UserOutlined,
} from "@ant-design/icons";
import { ApiError } from "@/api/client";
import { registriesApi } from "@/api/resources";
import { extractOperationId } from "@/components/molecules/OperationDialog";
import { ResourceIcon } from "@/components/organisms/form/ResourceIcon";
import { ErrorResult } from "@/components/molecules/ErrorResult";
import { getByPath } from "@/lib/resource-registry";
import { useOperation } from "@/lib/use-operation";
import { shortDigest } from "@/lib/short-digest";
import { formatDateTime } from "@/lib/datetime";
import { formatBytes } from "@/lib/bytes";
import { toast } from "@/lib/toast";

// fmtLastPull — время последнего pull тега. Zero-time («1970-01-01T00:00:00Z»,
// нулевой timestamp) / пусто → «не скачивался»; иначе — дата-время.
function fmtLastPull(v: unknown): string {
  if (typeof v !== "string" || !v) return "не скачивался";
  const d = new Date(v);
  if (Number.isNaN(d.getTime()) || d.getTime() <= 0 || d.getUTCFullYear() <= 1970) return "не скачивался";
  return formatDateTime(v);
}

// parsePositiveInt — int64-счётчик (приходит строкой) → число, если > 0.
function parsePositiveInt(v: unknown): number | null {
  const n = typeof v === "string" ? Number.parseInt(v, 10) : typeof v === "number" ? v : Number.NaN;
  return Number.isFinite(n) && n > 0 ? n : null;
}

async function copyText(text: string, okMsg: string) {
  try {
    await navigator.clipboard.writeText(text);
    toast.success(okMsg);
  } catch {
    toast.error("Не удалось скопировать");
  }
}

export const RepositoryTagsPanel: FC<{
  registryId: string;
  repository: string;
  onClose: () => void;
}> = ({ registryId, repository, onClose }) => {
  const qc = useQueryClient();
  const tagsKey = useMemo(() => ["tags", "list", registryId, repository], [registryId, repository]);
  const { data, isLoading, isError, error } = useQuery({
    queryKey: tagsKey,
    queryFn: () => registriesApi.listTags(registryId, repository),
    enabled: !!registryId && !!repository,
    refetchInterval: 5_000,
    staleTime: 0,
  });

  // endpoint реестра для команды docker pull (registry.kacho.local/<id>). Кэшируем
  // по реестру; fallback — конвенционный base, пока запрос не вернулся.
  const { data: reg } = useQuery({
    queryKey: ["registry", "endpoint", registryId],
    queryFn: () => registriesApi.get(registryId),
    enabled: !!registryId,
    staleTime: 60_000,
  });
  const pullBase = (reg?.endpoint as string | undefined) ?? `registry.kacho.local/${registryId}`;

  const invalidateTags = () => void qc.invalidateQueries({ queryKey: tagsKey, refetchType: "all" });
  const rows = (data?.tags as Record<string, unknown>[] | undefined) ?? [];

  return (
    <div
      className="kc-tags-panel"
      style={{
        height: "100%",
        minHeight: 0,
        minWidth: 0,
        display: "flex",
        flexDirection: "column",
        overflow: "hidden",
      }}
    >
      {/* Шапка панели */}
      <div
        style={{
          flexShrink: 0,
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          gap: 8,
          padding: "10px 10px 10px 16px",
          borderBottom: "1px solid var(--kc-border, rgba(128,128,128,0.18))",
        }}
      >
        <span style={{ display: "inline-flex", alignItems: "center", gap: 8, minWidth: 0 }}>
          <ResourceIcon specId="tags" />
          <span
            style={{
              fontWeight: 600,
              maxWidth: 220,
              overflow: "hidden",
              textOverflow: "ellipsis",
              whiteSpace: "nowrap",
            }}
          >
            {repository}
          </span>
          <Typography.Text type="secondary" style={{ fontWeight: 400 }}>
            · теги{rows.length ? ` · ${rows.length}` : ""}
          </Typography.Text>
        </span>
        <Button type="text" size="small" icon={<CloseOutlined />} onClick={onClose} aria-label="Закрыть теги" />
      </div>

      {/* Тело: вертикальный список карточек тегов (скролл внутри). */}
      <div
        style={{
          flex: 1,
          minHeight: 0,
          overflow: "auto",
          padding: 12,
          display: "flex",
          flexDirection: "column",
          gap: 10,
        }}
      >
        {isError ? (
          <ErrorResult error={error} />
        ) : isLoading ? (
          <Skeleton active paragraph={{ rows: 3 }} />
        ) : rows.length === 0 ? (
          <Empty description="Нет тегов — репозиторий ещё не публиковался (docker push)." />
        ) : (
          rows.map((r) => {
            const tag = getByPath<string>(r, "tag") ?? "";
            const digest = getByPath<string>(r, "digest") ?? "";
            const created = getByPath<string>(r, "created_at");
            const size = formatBytes(getByPath(r, "size_bytes"));
            const arch = getByPath<string>(r, "architecture");
            const lastPull = fmtLastPull(getByPath(r, "last_pulled_at"));
            const pushedBy = getByPath<string>(r, "pushed_by") || "";
            const downloadCount = parsePositiveInt(getByPath(r, "download_count"));
            const pullRef = `${pullBase}/${repository}:${tag}`;
            return (
              <div key={tag || digest} className="kc-tag-card">
                {/* Строка 1: тег-герой + sha256:digest В ОДНУ ЛИНИЮ + удаление. */}
                <div style={{ display: "flex", alignItems: "center", gap: 8, minWidth: 0 }}>
                  <Tag
                    color="blue"
                    style={{
                      fontFamily: "var(--font-mono, monospace)",
                      fontSize: 13,
                      fontWeight: 600,
                      margin: 0,
                      flexShrink: 0,
                      paddingInline: 8,
                    }}
                  >
                    {tag}
                  </Tag>
                  {shortDigest(digest) && (
                    <Typography.Text
                      type="secondary"
                      code
                      copyable={{ text: digest, tooltips: ["Копировать digest", "Скопировано"] }}
                      style={{
                        fontSize: 11,
                        minWidth: 0,
                        overflow: "hidden",
                        textOverflow: "ellipsis",
                        whiteSpace: "nowrap",
                      }}
                    >
                      sha256:{shortDigest(digest)}…
                    </Typography.Text>
                  )}
                  <span style={{ flex: 1 }} />
                  <TagDeleteAction registryId={registryId} repository={repository} tag={tag} onDone={invalidateTags} />
                </div>

                {/* Строка 2: метаданные (размер · запушен · последний pull · арх)
                    с иконками — быстрое сканирование. Значение времени: clock =
                    push (created_at), download = последний pull (last_pulled_at);
                    смысл иконок — в tooltip. */}
                <div
                  style={{
                    display: "flex",
                    flexWrap: "wrap",
                    alignItems: "center",
                    columnGap: 14,
                    rowGap: 6,
                    marginTop: 8,
                    color: "var(--kc-text-secondary)",
                    fontSize: 12,
                  }}
                >
                  <span style={{ display: "inline-flex", alignItems: "center", gap: 5 }}>
                    <HddOutlined style={{ fontSize: 12 }} /> {size}
                  </span>
                  <Tooltip title="Запушен (время docker push)">
                    <span style={{ display: "inline-flex", alignItems: "center", gap: 5, minWidth: 0 }}>
                      <ClockCircleOutlined style={{ fontSize: 12 }} /> {created ? formatDateTime(created) : "—"}
                    </span>
                  </Tooltip>
                  <Tooltip title="Последний pull (docker pull)">
                    <span style={{ display: "inline-flex", alignItems: "center", gap: 5, minWidth: 0 }}>
                      <DownloadOutlined style={{ fontSize: 12 }} /> {lastPull}
                    </span>
                  </Tooltip>
                  {arch && (
                    <Tag
                      bordered
                      style={{ margin: 0, fontFamily: "var(--font-mono, monospace)", fontSize: 11, lineHeight: "18px" }}
                    >
                      {arch}
                    </Tag>
                  )}
                </div>

                {/* Строка 2b (опц.): автор push + число pull'ов — приглушённо. */}
                {(pushedBy || downloadCount !== null) && (
                  <div
                    style={{
                      display: "flex",
                      flexWrap: "wrap",
                      alignItems: "center",
                      columnGap: 12,
                      rowGap: 4,
                      marginTop: 6,
                      color: "var(--kc-text-tertiary, #8b929e)",
                      fontSize: 11,
                    }}
                  >
                    {pushedBy && (
                      <Tooltip title="Кем запушен">
                        <span
                          style={{
                            display: "inline-flex",
                            alignItems: "center",
                            gap: 5,
                            minWidth: 0,
                            overflow: "hidden",
                            textOverflow: "ellipsis",
                            whiteSpace: "nowrap",
                            maxWidth: 180,
                          }}
                        >
                          <UserOutlined style={{ fontSize: 11 }} /> {pushedBy}
                        </span>
                      </Tooltip>
                    )}
                    {downloadCount !== null && (
                      <Tooltip title="Число pull'ов">
                        <span style={{ display: "inline-flex", alignItems: "center", gap: 5 }}>
                          <DownloadOutlined style={{ fontSize: 11 }} /> {downloadCount}
                        </span>
                      </Tooltip>
                    )}
                  </div>
                )}

                {/* Строка 3: docker pull — утопленное code-поле + копирование в конце. */}
                <div className="kc-pull-field">
                  <Typography.Text
                    style={{
                      flex: 1,
                      minWidth: 0,
                      fontFamily: "var(--font-mono, monospace)",
                      fontSize: 12,
                      color: "var(--kc-text-secondary)",
                      overflow: "hidden",
                      textOverflow: "ellipsis",
                      whiteSpace: "nowrap",
                    }}
                  >
                    docker pull …/{tag}
                  </Typography.Text>
                  <Tooltip title={`Копировать: docker pull ${pullRef}`} placement="topRight">
                    <Button
                      type="text"
                      size="small"
                      icon={<CopyOutlined />}
                      onClick={() => copyText(`docker pull ${pullRef}`, "docker pull скопирован")}
                      aria-label="Копировать docker pull"
                    />
                  </Tooltip>
                </div>
              </div>
            );
          })
        )}
      </div>
    </div>
  );
};

// TagDeleteAction — per-tag удаление (async Operation): иконка + Popconfirm →
// deleteTag → extractOperationId → poll → invalidate. Ошибка → toast.
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
      const m = e instanceof ApiError ? `${e.code}: ${e.message}` : (e as Error).message;
      toast.error(`Удалить тег ${tag}: ${m}`);
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
      <Button type="text" size="small" danger icon={<DeleteOutlined />} loading={pending} aria-label="Удалить тег" />
    </Popconfirm>
  );
}
