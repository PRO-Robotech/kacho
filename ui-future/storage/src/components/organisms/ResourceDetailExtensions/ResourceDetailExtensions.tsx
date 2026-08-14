// resource-detail-extensions — реестр доменных расширений detail-страницы
// storage-remote.
//
// ResourceShell остаётся generic (Обзор / связанные / Операции / JSON + формы-
// панели). Доменно-специфичные строки Обзора и header-действия конкретного
// ресурса подключаются здесь по spec.id. Для Storage расширения раскрывают
// том (зона / тип диска / размер / занято / статус с причиной / источник /
// подключения), снимок (исходный том / зона / размер / статус), образ (регион /
// источник / размер / min-disk / формат / статус) и тип диска (ярус / состояние
// обращения / границы размера / способности).
//
// Правила канона, которые здесь исполняются:
//   · 2 — ссылка на чужой ресурс всегда `RefNameLink`: зона, регион и тип диска
//     показываются именем со ссылкой, а не идентификатором. Зона и регион —
//     ГЛОБАЛЬНЫЙ каталог geo, и запрос за именем идёт без `project_id`;
//   · 6 — булево называется СЛЕДСТВИЕМ: способности типа диска — `BoolFact`
//     («Снимки поддерживаются» / «Снимки недоступны»), а не «Да»/«Нет»;
//   · 9 — поле без источника не показывается: причина состояния, занятые байты и
//     границы размера появляются, ТОЛЬКО когда сервер их прислал. Прочерк на
//     месте живого факта утверждает о ресурсе неправду, а прочерк на месте
//     причины читается как «причина есть, но мы её не знаем».

import { type ReactNode } from "react";
import { Typography } from "antd";

import type { DetailTab } from "@/components/organisms/DetailShell";
import { BoolFact } from "@/components/atoms/BoolFact";
import { StatusBadge } from "@/components/atoms/StatusBadge";
import { RefNameLink } from "@/components/molecules/RefNameLink";
import { ChangeDiskTypeDialog } from "@/components/organisms/storage/ChangeDiskTypeDialog";
import { CopyToPlacementDialog } from "@/components/organisms/storage/CopyToPlacementDialog";
import { imagesApi, snapshotsApi } from "@/api/resources";
import { getByPath } from "@/lib/resource-registry";
import { formatBytes } from "@/lib/bytes";
import {
  CAPABILITIES,
  LIFECYCLE_HINT,
  TIER_HINT,
  lifecycleLabel,
  sizeLimitFacts,
  statusReasonText,
  tierLabel,
  usedBytesText,
  volumeTransientHint,
} from "@/lib/storage-enums";

export interface DescItem {
  label: string;
  value: ReactNode;
}

export interface DetailExtCtx {
  data: Record<string, unknown>;
  projectId: string | null;
  /** Базовый URL detail-страницы ресурса (без хвостов /edit, /json, /<tab>). */
  detailBase: string;
  navigate: (to: string) => void;
}

export interface DetailExtension {
  overviewExtra?: (ctx: DetailExtCtx) => DescItem[];
  /** Контент под Обзор-таблицей (отдельные секции-таблицы с подписью). */
  overviewBelow?: (ctx: DetailExtCtx) => ReactNode;
  headerActions?: (ctx: DetailExtCtx) => ReactNode;
  extraTabs?: (ctx: DetailExtCtx) => DetailTab[];
  // Здесь была ручка `hideOperations`. Её не выставляло ни одно расширение, а
  // вкладка операций тем временем показывалась у ресурсов, у которых подмаршрута
  // `<apiPath>/{id}/operations` в стволе нет вовсе. Решает не ручка, а контракт:
  // `@shared/lib/operations-subroute` — единственное место, где консоль
  // утверждает, у кого этот подмаршрут есть, и оно сверяется с деревом proto.
  title?: (data: Record<string, unknown>) => string | undefined;
}

// ─────────────────────────── helpers ───────────────────────────

const dash = <Typography.Text type="secondary">—</Typography.Text>;

function txt(v: unknown): ReactNode {
  const s = v == null ? "" : String(v);
  return s ? s : dash;
}

function bytes(v: unknown): ReactNode {
  const s = formatBytes(v);
  return s === "—" ? dash : <>{s}</>;
}

function hinted(label: string | null, hint: string | undefined): ReactNode {
  if (!label) return dash;
  return hint ? (
    <span>
      {label} <Typography.Text type="secondary">— {hint}</Typography.Text>
    </span>
  ) : (
    <>{label}</>
  );
}

/** Ссылка на зону — глобальный каталог geo. `projectId` НЕ передаётся: измерения
 *  «проект» у каталога нет, и `RefNameLink` спрашивает его без этого параметра. */
function zoneRef(id: unknown): ReactNode {
  const v = typeof id === "string" ? id : "";
  return v ? <RefNameLink specId="zones" refId={v} maxChars={32} /> : dash;
}

function regionRef(id: unknown): ReactNode {
  const v = typeof id === "string" ? id : "";
  return v ? <RefNameLink specId="regions" refId={v} maxChars={32} /> : dash;
}

/**
 * Строка причины состояния — либо `null`, чтобы строки не было вовсе.
 *
 * Возвращается массив (0 либо 1 элемент), чтобы вызывающий разворачивал его
 * спредом: ветка `? [ … ] : []` на месте вызова повторялась бы у каждого из трёх
 * ресурсов, и разошлась бы на первом же уточнении текста.
 */
function statusReasonRow(data: Record<string, unknown>): DescItem[] {
  const text = statusReasonText(getByPath<string>(data, "status_reason"));
  return text === null ? [] : [{ label: "Причина состояния", value: <>{text}</> }];
}

// ─────────────────────────── реестр ───────────────────────────

export const DETAIL_EXTENSIONS: Record<string, DetailExtension> = {
  // Том: инфраструктурно-нейтральные, tenant-facing строки Обзора.
  volumes: {
    overviewExtra: ({ data }) => {
      const attachments = (getByPath<unknown[]>(data, "attachments") ?? []) as Array<Record<string, unknown>>;
      const attachLabel =
        attachments.length > 0
          ? attachments.map((a) => (a.instance_name as string) || (a.instance_id as string) || "?").join(", ")
          : "";
      const status = getByPath<string>(data, "status");
      const transient = volumeTransientHint(status);
      const usedText = usedBytesText(getByPath<unknown>(data, "used_bytes"));
      return [
        { label: "Зона доступности", value: zoneRef(getByPath<string>(data, "zone_id")) },
        {
          label: "Тип диска",
          value: getByPath<string>(data, "disk_type_id") ? (
            <RefNameLink specId="disk-types" refId={getByPath<string>(data, "disk_type_id")} maxChars={32} />
          ) : (
            dash
          ),
        },
        { label: "Размер", value: bytes(getByPath<unknown>(data, "size_bytes")) },
        // Занятые байты ОТСУТСТВУЮТ, когда бэкенд потребление не сообщил, — и это
        // не ноль: ноль означал бы «том пуст», а такое утверждение было бы
        // правдоподобной ложью, по которой строят биллинг и решают о ресайзе.
        // Поэтому строки нет вовсе, а не прочерка (правило 9); а сообщённый ноль,
        // наоборот, показывается «0 B» — общий `formatBytes` перевёл бы его в
        // «—», то есть сказал бы «не знаю» там, где сервер ответил точно.
        ...(usedText === null ? [] : [{ label: "Занято", value: <>{usedText}</> }]),
        { label: "Статус", value: <StatusBadge state={status} /> },
        // Том рождается в состоянии создания, пригодным его делает сверщик —
        // между успешным ответом на Create и работающим томом есть окно. Значок
        // называет состояние, но не отвечает, надо ли что-то делать. У конечного
        // состояния строки нет: «Available» самодостаточен.
        ...(transient === null ? [] : [{ label: "Что происходит", value: <>{transient}</> }]),
        ...statusReasonRow(data),
        {
          label: "Исходный снимок",
          value: getByPath<string>(data, "source_snapshot_id") ? (
            <RefNameLink
              specId="snapshots"
              refId={getByPath<string>(data, "source_snapshot_id")}
              projectId={getByPath<string>(data, "project_id")}
              maxChars={32}
            />
          ) : (
            dash
          ),
        },
        {
          label: "Исходный образ",
          value: getByPath<string>(data, "source_image_id") ? (
            <RefNameLink
              specId="images"
              refId={getByPath<string>(data, "source_image_id")}
              projectId={getByPath<string>(data, "project_id")}
              maxChars={32}
            />
          ) : (
            dash
          ),
        },
        // used_by° — производно от attachments (кто использует том). Показываем
        // имена инстансов-потребителей; пусто → «—».
        { label: "Используется", value: txt(attachLabel) },
      ];
    },
    headerActions: ({ data, projectId }) => (
      <ChangeDiskTypeDialog
        volumeId={getByPath<string>(data, "id") ?? ""}
        projectId={projectId}
        status={getByPath<string>(data, "status")}
        currentDiskTypeId={getByPath<string>(data, "disk_type_id")}
      />
    ),
  },

  // Снимок: исходный том / собственный якорь зоны / размер / статус.
  snapshots: {
    overviewExtra: ({ data }) => [
      // Происхождение ровно одно: том (снятие) либо снимок (копирование).
      // Строка называет ВИД происхождения, а не «источник вообще»: у копии
      // родитель — снимок, и подпись «Исходный том» на нём была бы неправдой.
      // До появления поля контракта карточка копии показывала здесь прочерк.
      ...(getByPath<string>(data, "source_snapshot_id")
        ? [
            {
              label: "Скопирован со снимка",
              value: (
                <RefNameLink
                  specId="snapshots"
                  refId={getByPath<string>(data, "source_snapshot_id")}
                  projectId={getByPath<string>(data, "project_id")}
                  maxChars={32}
                />
              ),
            },
          ]
        : [
            {
              label: "Исходный том",
              value: getByPath<string>(data, "source_volume_id") ? (
                <RefNameLink
                  specId="volumes"
                  refId={getByPath<string>(data, "source_volume_id")}
                  projectId={getByPath<string>(data, "project_id")}
                  maxChars={32}
                />
              ) : (
                dash
              ),
            },
          ]),
      // Собственный якорь снимка, а не «зона исходного тома»: ссылка на источник
      // обнуляется при его удалении (снимок обязан пережить свой том), и зона,
      // добираемая через источник, однажды стала бы пустой строкой.
      { label: "Зона доступности", value: zoneRef(getByPath<string>(data, "zone_id")) },
      { label: "Размер", value: bytes(getByPath<unknown>(data, "size_bytes")) },
      { label: "Статус", value: <StatusBadge state={getByPath<string>(data, "status")} /> },
      ...statusReasonRow(data),
    ],
    headerActions: ({ data, projectId }) => (
      <CopyToPlacementDialog
        specId="snapshots"
        projectId={projectId}
        targetField="target_zone_id"
        targetRefResource="zones"
        targetLabel="Зона копии"
        targetDescription="Зона называется явно: умолчание «та же зона» превратило бы перенос в дубликатор без предмета."
        title="Копия снимка в другую зону"
        buttonLabel="Скопировать в зону"
        submit={(body) => snapshotsApi.copy(getByPath<string>(data, "id") ?? "", body)}
      />
    ),
  },

  // Образ (STOR-1): регион / источник (snapshot XOR volume) / размер / min-disk /
  // формат / статус. REGIONAL/anycast — placement по region_id, не zone_id.
  images: {
    overviewExtra: ({ data }) => {
      const snap = getByPath<string>(data, "source_snapshot_id");
      const vol = getByPath<string>(data, "source_volume_id");
      const projectId = getByPath<string>(data, "project_id");
      // Происхождений ТРИ вида, и ровно одно из них: снимок либо том (снятие),
      // либо образ (копирование). Третий вид добавлен вместе с полем контракта:
      // до него карточка КОПИИ показывала прочерк при живом происхождении —
      // родитель был записан в базе, но наружу не выходил.
      const parent = getByPath<string>(data, "source_image_id");
      const sourceValue = snap ? (
        <RefNameLink specId="snapshots" refId={snap} projectId={projectId} maxChars={32} />
      ) : vol ? (
        <RefNameLink specId="volumes" refId={vol} projectId={projectId} maxChars={32} />
      ) : parent ? (
        <RefNameLink specId="images" refId={parent} projectId={projectId} maxChars={32} />
      ) : (
        dash
      );
      return [
        { label: "Регион", value: regionRef(getByPath<string>(data, "region_id")) },
        { label: "Размещение", value: txt(getByPath<string>(data, "placement_type")) },
        { label: "Источник", value: sourceValue },
        { label: "Размер", value: bytes(getByPath<unknown>(data, "size_bytes")) },
        { label: "Мин. размер тома", value: bytes(getByPath<unknown>(data, "min_disk_bytes")) },
        { label: "Формат", value: txt(getByPath<string>(data, "format")) },
        { label: "Статус", value: <StatusBadge state={getByPath<string>(data, "status")} /> },
        ...statusReasonRow(data),
      ];
    },
    headerActions: ({ data, projectId }) => (
      <CopyToPlacementDialog
        specId="images"
        projectId={projectId}
        targetField="target_region_id"
        targetRefResource="regions"
        targetLabel="Регион копии"
        targetDescription="Перенос — предмет этого действия, поэтому регион называется явно: угаданное умолчание неотличимо от опечатки."
        title="Копия образа в другой регион"
        buttonLabel="Скопировать в регион"
        submit={(body) => imagesApi.copy(getByPath<string>(data, "id") ?? "", body)}
      />
    ),
  },

  // Тип диска: политика класса — ярус, состояние обращения, границы размера,
  // способности. Инфраструктуры здесь нет и не будет: координата бэкенда, имя
  // пула, шаблон пространства имён и номер ревизии привязки живут на
  // cluster-internal поверхности (:9091), потому что меняются вместе с бэкендом,
  // а класс обязан переживать его смену.
  "disk-types": {
    overviewExtra: ({ data }) => {
      const tier = getByPath<string>(data, "tier");
      const lifecycle = getByPath<string>(data, "lifecycle");
      return [
        { label: "Ярус", value: hinted(tierLabel(tier), tier ? TIER_HINT[tier] : undefined) },
        {
          label: "Обращение",
          value: hinted(lifecycleLabel(lifecycle), lifecycle ? LIFECYCLE_HINT[lifecycle] : undefined),
        },
      ];
    },
    overviewBelow: ({ data }) => {
      const caps = (getByPath<Record<string, unknown>>(data, "capabilities") ?? {}) as Record<string, unknown>;
      const hasCaps = getByPath<unknown>(data, "capabilities") !== undefined;
      const limits = sizeLimitFacts(getByPath(data, "limits"));
      return (
        <>
          {limits.length > 0 && (
            <section style={{ marginTop: 20 }}>
              <Typography.Title level={5} style={{ marginBottom: 8 }}>
                Границы размера тома
              </Typography.Title>
              <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginBottom: 8 }}>
                Границы объявляет тип диска, и они проверяются при создании и изменении тома.
              </Typography.Paragraph>
              <FactTable rows={limits.map((f) => ({ label: f.label, value: <>{f.text}</> }))} />
            </section>
          )}
          {hasCaps && (
            <section style={{ marginTop: 20 }}>
              <Typography.Title level={5} style={{ marginBottom: 8 }}>
                Способности
              </Typography.Title>
              <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginBottom: 8 }}>
                Перечисленное действует во всех зонах, где предлагается этот тип диска: способности выводятся
                пересечением, то есть консервативно.
              </Typography.Paragraph>
              <FactTable
                rows={CAPABILITIES.map((c) => ({
                  label: c.label,
                  value: <BoolFact value={caps[c.path]} yes={c.yes} no={c.no} />,
                }))}
              />
            </section>
          )}
        </>
      );
    },
  },
};

/** Секция-таблица «подпись → факт» под Обзором. Вид один на обе секции: границы
 *  и способности — один предмет («что этот тип диска обещает»), и два разных
 *  вида читались бы как два разных предмета (правило 3). */
function FactTable({ rows }: { rows: DescItem[] }): ReactNode {
  return (
    <table style={{ width: "100%", borderCollapse: "collapse" }}>
      <tbody>
        {rows.map((r) => (
          <tr key={r.label}>
            <td
              style={{
                width: 240,
                padding: "6px 12px 6px 0",
                verticalAlign: "top",
                color: "var(--kc-text-secondary)",
              }}
            >
              {r.label}
            </td>
            <td style={{ padding: "6px 0", verticalAlign: "top" }}>{r.value}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

export function detailExtension(specId: string): DetailExtension | undefined {
  return DETAIL_EXTENSIONS[specId];
}
