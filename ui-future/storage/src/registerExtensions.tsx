// registerExtensions — регистрация доменных расширений detail-страницы storage.
// Импортируется как side-effect входной точкой storage-remote (StoragePage),
// поэтому расширения подключаются на старте бандла, до рендера страниц.
//
// Реестр расширений живёт в ЕДИНСТВЕННОМ экземпляре — `@shared/components/
// organisms/ResourceDetailExtensions`, — и доменная специфика инжектится сюда
// уже существующей ручкой `registerDetailExtension`, а не своей копией реестра.
// Здесь лежал форк: свои `DescItem` / `DetailExtCtx` / `DetailExtension` /
// `DETAIL_EXTENSIONS` / `detailExtension` при одноимённых общих. Он отставал
// молча — общий тип успел завести `childCreate` (кастомная embedded-форма для
// child-create-роута вне реестра), а копия про эту ручку не знала вовсе.
//
// Расширения раскрывают том (зона / тип диска / размер / занято / статус с
// причиной / источник / подключения), снимок (исходный том / зона / размер /
// статус), образ (регион / источник / размер / min-disk / формат / статус) и
// тип диска (ярус / состояние обращения / границы размера / способности).
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
//     причины читается как «причина есть, но мы её не знаем»;
//   · 9 — двух реализаций одного вида не бывает: реестр расширений общий.

import { type ReactNode } from "react";
import { Typography } from "antd";

import { registerDetailExtension, type DescItem } from "@shared/components/organisms/ResourceDetailExtensions";
import { DETAIL_CONTENT_WIDTH, DetailSurface, PropertyRows } from "@/components/organisms/DetailShell";
import { BoolFact } from "@/components/atoms/BoolFact";
import { StatusBadge } from "@/components/atoms/StatusBadge";
import { ConsumersFact } from "@shared/components/molecules/ConsumersFact";
import { PlacementAnchor } from "@shared/components/molecules/PlacementAnchor";
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
  imageFormatText,
  sizeLimitFacts,
  statusReasonText,
  tierLabel,
  usedBytesText,
  volumeTransientHint,
} from "@/lib/storage-enums";

// ─────────────────────────── helpers ───────────────────────────

const dash = <Typography.Text type="secondary">—</Typography.Text>;

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

/**
 * Строка причины состояния — либо пустой массив, чтобы строки не было вовсе.
 *
 * Возвращается массив (0 либо 1 элемент), чтобы вызывающий разворачивал его
 * спредом: ветка `? [ … ] : []` на месте вызова повторялась бы у каждого из трёх
 * ресурсов, и разошлась бы на первом же уточнении текста.
 */
function statusReasonRow(data: Record<string, unknown>): DescItem[] {
  const text = statusReasonText(getByPath<string>(data, "status_reason"));
  return text === null ? [] : [{ label: "Причина состояния", value: <>{text}</> }];
}

// ─────────────────────────── расширения ───────────────────────────

// Том: инфраструктурно-нейтральные, tenant-facing строки Обзора.
registerDetailExtension("volumes", {
  overviewExtra: ({ data }) => {
    const status = getByPath<string>(data, "status");
    const transient = volumeTransientHint(status);
    const usedText = usedBytesText(getByPath<unknown>(data, "used_bytes"));
    return [
      { label: "Зона доступности", value: zoneRef(getByPath<string>(data, "zone_id")) },
      {
        label: "Тип диска",
        value: getByPath<string>(data, "disk_type_id") ? (
          <RefNameLink specId="disk-types" refId={getByPath<string>(data, "disk_type_id")} maxChars={32} copy={false} />
        ) : (
          dash
        ),
        copy: getByPath<string>(data, "disk_type_id") ?? undefined,
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
      // used_by° — кто использует том. Потребитель — ЧУЖОЙ ресурс, значит
      // ссылка (правило 2 канона консоли), а не строка имён через запятую:
      // из имени машины на её карточку не перейти, а именно за этим сюда и
      // смотрят. Вид тот же, что у колонки «Используется» в списке томов —
      // один предмет, один вид (правило 9); прежде список рисовал ссылки, а
      // карточка того же тома — плоский текст.
      {
        label: "Используется",
        value: (
          <ConsumersFact
            usedBy={getByPath(data, "used_by")}
            projectId={getByPath<string>(data, "project_id") ?? null}
          />
        ),
      },
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
});

// Снимок: исходный том / собственный якорь зоны / размер / статус.
registerDetailExtension("snapshots", {
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
});

// Образ (STOR-1): регион / источник (snapshot XOR volume) / размер / min-disk /
// формат / статус. REGIONAL/anycast — placement по region_id, не zone_id.
registerDetailExtension("images", {
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
    const format = imageFormatText(getByPath<string>(data, "format"));
    return [
      // ОДНА строка размещения вместо двух. Прежде их было две: «Регион» со
      // ссылкой и «Размещение» с сырым токеном `REGIONAL` — то есть машинное
      // слово рядом с тем же самым фактом, уже названным ссылкой выше.
      // Ветку ZONAL/REGIONAL рисует единственный `PlacementAnchor`: вид
      // размещения он не называет отдельным словом — вид и есть тип ресурса,
      // на который ведёт ссылка.
      {
        label: "Размещение",
        value: <PlacementAnchor row={data} maxChars={32} />,
        // Копируется идентификатор якоря: имя меняется, идентификатор нет.
        copy: getByPath<string>(data, "region_id") || getByPath<string>(data, "zone_id") || undefined,
      },
      { label: "Источник", value: sourceValue },
      { label: "Размер", value: bytes(getByPath<unknown>(data, "size_bytes")) },
      { label: "Мин. размер тома", value: bytes(getByPath<unknown>(data, "min_disk_bytes")) },
      // Формат — закрытый словарь контракта, и наружу он идёт СЛОВОМ, а не
      // токеном: `STANDARD` отвечает, как значение называется внутри, но не
      // говорит читателю ничего. Незаявленного формата строки нет вовсе
      // (правило 9), а незнакомое значение показывается собой — словарь мог
      // пополниться раньше консоли.
      ...(format === null ? [] : [{ label: "Формат", value: <>{format}</> }]),
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
});

// Тип диска: политика класса — ярус, состояние обращения, границы размера,
// способности. Инфраструктуры здесь нет и не будет: координата бэкенда, имя
// пула, шаблон пространства имён и номер ревизии привязки живут на
// cluster-internal поверхности (:9091), потому что меняются вместе с бэкендом,
// а класс обязан переживать его смену.
registerDetailExtension("disk-types", {
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
    // Секции карточки — та же поверхность и те же строки «ключ · значение»,
    // что и «Обзор» над ними (правила 4 и 9 канона консоли). Здесь стояла
    // своя `<table>` с колонкой подписи в 240 точек, без рамки и без шапки:
    // на одной странице жили два вида одного предмета — строки свойств
    // сверху и другие строки свойств снизу. Заголовок секции — ПЕРВАЯ СТРОКА
    // таблицы (`DetailSurface`), а не блок над ней; оговорка о смысле
    // перечня стоит в правой части той же шапки, а не отдельным абзацем.
    return (
      <>
        {limits.length > 0 && (
          <div style={{ maxWidth: DETAIL_CONTENT_WIDTH, marginTop: 20 }}>
            <DetailSurface title="Границы размера тома" note="Проверяются при создании и изменении тома">
              <PropertyRows items={limits.map((f) => ({ label: f.label, value: f.text }))} />
            </DetailSurface>
          </div>
        )}
        {hasCaps && (
          <div style={{ maxWidth: DETAIL_CONTENT_WIDTH, marginTop: 20 }}>
            <DetailSurface title="Способности" note="Пересечение по всем зонам класса">
              <PropertyRows
                items={CAPABILITIES.map((c) => ({
                  label: c.label,
                  // ТОН ОБЪЯВЛЯЕТСЯ КАЖДОЙ СТОРОНЕ (правило 5 канона). Без
                  // него обе стороны уходили в нейтральный, и «Снимки
                  // недоступны» выглядело таким же штатным положением, как
                  // «Снимки поддерживаются», — при том что смысл у сторон
                  // разный: способность есть — возможность открыта (`good`),
                  // способности нет — возможность ЗАКРЫТА, и это та сторона,
                  // о которой стоит знать до создания тома (`attention`).
                  value: <BoolFact value={caps[c.path]} yes={c.yes} no={c.no} yesTone="good" noTone="attention" />,
                }))}
              />
            </DetailSurface>
          </div>
        )}
      </>
    );
  },
});
