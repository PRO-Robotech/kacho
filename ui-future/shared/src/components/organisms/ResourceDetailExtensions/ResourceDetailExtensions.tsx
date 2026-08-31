// resource-detail-extensions — реестр доменных расширений detail-страницы.
//
// ResourceShell остаётся generic (Обзор/связанные/Операции/JSON + формы-панели).
// Доменно-специфичный контент конкретного ресурса (доп. строки Обзора, доменные
// табы — SG-правила, RouteTable-маршруты, Instance NIC/power, TG targets, IPAM,
// IAM access-bindings — кнопки-действия в шапке) подключается ЗДЕСЬ, по spec.id,
// переиспользуя уже существующие доменные компоненты/логику кастом-страниц.
//
// Так раскатка эталона на все ресурсы не теряет доменную функциональность и не
// раздувает ResourceShell. Карта миграции:
// docs/superpowers/specs/2026-05-30-kacho-ui-rollout-migration-map.json

import { type ReactNode } from "react";
import { DETAIL_CONTENT_WIDTH } from "@shared/components/organisms/DetailShell";
import { Link } from "react-router";
import { useQuery } from "@tanstack/react-query";
import { Tag, Typography } from "antd";

import { toast } from "@shared/lib/toast";
import type { DetailTab } from "@shared/components/organisms/DetailShell";

import { PlacementAnchor } from "@shared/components/molecules/PlacementAnchor";
import { BoolFact } from "@shared/components/atoms/BoolFact";
import { RefNameLink } from "@shared/components/molecules/RefNameLink";
import { SgRulesPanel, type SgRule } from "@shared/components/organisms/SgRulesPanel";
import { RoutesPanel } from "@shared/components/organisms/RoutesPanel";
import { SubnetCidrPanel } from "@shared/components/organisms/SubnetCidrPanel";
import { NetworkCidrManager } from "@shared/components/organisms/NetworkCidrManager";
import { CidrGroupBlocksManager } from "@shared/components/organisms/CidrGroupBlocksManager";
import { ResourceIcon } from "@shared/components/organisms/form/ResourceIcon";
import { ConsumersFact } from "@shared/components/molecules/ConsumersFact";
import { SECURITY_GROUP_USED_BY_LIMIT } from "@shared/lib/used-by-limits";
import { api } from "@shared/api/client";
import { getByPath } from "@shared/lib/resource-registry";
import { displayText } from "@shared/lib/display-text";
import { copyText } from "@shared/lib/clipboard";

/** Одна запись `used_by` — output-only kacho.cloud.reference.Reference. */
export interface UsedByEntry {
  referrer?: { type?: string; id?: string; name?: string };
}

export interface DescItem {
  /** ReactNode, а не string: строке обзора бывает нужна подсказка ⓘ рядом с
   *  именем — поле, которое заполняет система, иначе читается как пустое,
   *  которое пользователь забыл ввести (продукт #478). Рисует её `FieldLabel`. */
  label: ReactNode;
  value: ReactNode;
  /** Что кладёт в буфер значок рядом со значением. Не задано — кнопки нет.
   *
   *  Объявляется там, где значение ПЕРЕНОСЯТ в чужое поле (адрес, MAC), и не
   *  объявляется там, где значение — ссылка, набор меток или факт словами:
   *  ссылку копируют её собственной кнопкой, а слово «Свободен» вставлять
   *  некуда. Совпадает по имени и смыслу с `PropertyItem.copy` — строку рисует
   *  один компонент (`PropertyRows`), и второй формы у этого поля нет. */
  copy?: string;
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
  /** Контент под Обзор-таблицей (отдельные секции-таблицы с подписью, напр.
   *  статические маршруты RouteTable). */
  overviewBelow?: (ctx: DetailExtCtx) => ReactNode;
  headerActions?: (ctx: DetailExtCtx) => ReactNode;
  extraTabs?: (ctx: DetailExtCtx) => DetailTab[];
  /** Кастомная embedded create-форма для child-create-роута, которого НЕТ в
   *  REGISTRY (напр. "privileges" → AccessBindingCreateForm с залоченным
   *  субъектом). ResourceShell зовёт это в child-create branch, когда REGISTRY-spec
   *  для childRoute не найден. Форма сама навигирует через onSuccess/onCancel. */
  childCreate?: (childRoute: string, ctx: DetailExtCtx) => ReactNode;
  // Здесь была ручка `hideOperations`. Её не выставляло ни одно расширение, а
  // вкладка операций тем временем показывалась у ресурсов, у которых подмаршрута
  // `<apiPath>/{id}/operations` в стволе нет вовсе. Решает не ручка, а контракт:
  // `lib/operations-subroute` — единственное место, где консоль утверждает, у
  // кого этот подмаршрут есть, и оно сверяется с деревом proto в обе стороны.
  title?: (data: Record<string, unknown>) => string | undefined;
}

// ─────────────────────────── helpers ───────────────────────────

const dash = <Typography.Text type="secondary">—</Typography.Text>;

function txt(v: unknown): ReactNode {
  const s = displayText(v);
  return s ? s : dash;
}

function mono(v: unknown): ReactNode {
  const s = displayText(v);
  return s ? <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, fontWeight: 520 }}>{s}</span> : dash;
}


// CIDR-блоки — нейтральные (цвет текста) теги, друг под другом, клик = копировать.
function cidrTags(items: string[] | undefined): ReactNode {
  if (!items || items.length === 0) return dash;
  return (
    <span style={{ display: "inline-flex", flexDirection: "column", gap: 4, alignItems: "flex-start" }}>
      {items.map((c) => (
        <Tag
          key={c}
          title="Нажмите, чтобы скопировать"
          onClick={(e) => {
            e.stopPropagation();
            // См. `@shared/lib/clipboard`: вне защищённого контекста прямого
            // доступа к буферу нет вовсе, и `?.` тихо не делает ничего.
            void copyText(c);
            toast.success(`Скопировано: ${c}`);
          }}
          style={{
            margin: 0,
            cursor: "pointer",
            fontFamily: "var(--font-mono)",
            fontSize: 11,
            fontWeight: 520,
          }}
        >
          {c}
        </Tag>
      ))}
    </span>
  );
}

// Ссылки на ресурсы (иконка + имя), друг под другом — единый вид как везде.
function refLinks(ids: string[] | undefined, specId: string): ReactNode {
  if (!ids || ids.length === 0) return dash;
  return (
    <span style={{ display: "inline-flex", flexDirection: "column", gap: 4, alignItems: "flex-start" }}>
      {ids.map((id) => (
        <RefNameLink key={id} specId={specId} refId={id} maxChars={28} />
      ))}
    </span>
  );
}

// ── RouteTable static_routes ──
// Копия того же имени, что у типа панели маршрутов, — поэтому гейт
// `test/set-replacement-draft-composition` сверяет её с контрактом вместе с
// оригиналом: форк не остаётся незамеченным ровно потому, что он форк.
interface StaticRoute {
  destination_prefix?: string;
  next_hop_address?: string;
  gateway_id?: string;
  labels?: Record<string, string>;
}
// Статические маршруты — PROP таблицы маршрутов (не смежный ресурс).
// Показываем ОТДЕЛЬНОЙ таблицей с подписью под Обзором (overviewBelow);
// добавление/правка — через «Редактировать» (generic array-field static_routes).

// ── Address: вычисление IP/семейства/вида ──
function addressInfo(data: Record<string, unknown>): { ip: string; family: string; kind: string } {
  const ext4 = getByPath<{ address?: string }>(data, "external_ipv4_address");
  const int4 = getByPath<{ address?: string }>(data, "internal_ipv4_address");
  const ext6 = getByPath<{ address?: string }>(data, "external_ipv6_address");
  const int6 = getByPath<{ address?: string }>(data, "internal_ipv6_address");
  if (ext4?.address) return { ip: ext4.address, family: "IPv4", kind: "Внешний" };
  if (int4?.address) return { ip: int4.address, family: "IPv4", kind: "Внутренний" };
  if (ext6?.address) return { ip: ext6.address, family: "IPv6", kind: "Внешний" };
  if (int6?.address) return { ip: int6.address, family: "IPv6", kind: "Внутренний" };
  return { ip: "", family: "—", kind: "—" };
}

// AddressRefTag — тег адреса: имя ресурса + доп-алиас (сам IP), кликабельно на
// detail адреса. Резолвит адрес по id (TanStack-дедуп).
function AddressRefTag({ id, projectId }: { id: string; projectId: string | null }) {
  const { data } = useQuery({
    queryKey: ["ref-address", id],
    queryFn: () => api.get<Record<string, unknown>>(`/vpc/v1/addresses/${id}`),
    enabled: !!id,
    staleTime: 30_000,
  });
  const name = (data ? getByPath<string>(data, "name") : "") || id.slice(0, 12);
  const ip = data ? addressInfo(data).ip : "";
  // Единый вид ссылки на ресурс: иконка + имя (+ доп-алиас IP), не тег.
  const content = (
    <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
      <ResourceIcon specId="addresses" />
      {name}
      {ip && <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, opacity: 0.85 }}> · {ip}</span>}
    </span>
  );
  return projectId ? (
    <Link
      to={`/projects/${projectId}/vpc/addresses/${id}`}
      onClick={(e) => e.stopPropagation()}
      className="text-primary hover:underline"
    >
      {content}
    </Link>
  ) : (
    <span className="text-foreground">{content}</span>
  );
}

function AddressRefTags({ ids, projectId }: { ids: string[] | undefined; projectId: string | null }): ReactNode {
  if (!ids || ids.length === 0) return dash;
  return (
    <span style={{ display: "inline-flex", flexDirection: "column", gap: 4, alignItems: "flex-start" }}>
      {ids.map((id) => (
        <AddressRefTag key={id} id={id} projectId={projectId} />
      ))}
    </span>
  );
}

// ─────────────────────────── реестр ───────────────────────────

export const DETAIL_EXTENSIONS: Record<string, DetailExtension> = {
  networks: {
    // VPC-1: system-provisioned default-SG + default-RT (echoed on create).
    overviewExtra: ({ data }) => [
      {
        label: "Группа безопасности по умолчанию",
        value: (
          <RefNameLink
            specId="security-groups"
            refId={getByPath<string>(data, "default_security_group_id")}
            maxChars={42}
          copy={false}
          />
        ),
        copy: getByPath<string>(data, "default_security_group_id") ?? undefined,
      },
      {
        label: "Таблица маршрутов по умолчанию",
        value: getByPath<string>(data, "default_route_table_id") ? (
          <RefNameLink specId="route-tables" refId={getByPath<string>(data, "default_route_table_id")} maxChars={42} copy={false} />
        ) : (
          dash
        ),
        copy: getByPath<string>(data, "default_route_table_id") ?? undefined,
      },
    ],
    // VPC-1: declared supernet — managed via :add/:remove-cidr-blocks (immutable
    // through Update). Панель под Обзором, как CIDR у подсети.
    overviewBelow: ({ data }) => {
      const networkId = getByPath<string>(data, "id") ?? "";
      const v4 = getByPath<string[]>(data, "ipv4_cidr_blocks") ?? [];
      const v6 = getByPath<string[]>(data, "ipv6_cidr_blocks") ?? [];
      return (
        <div style={{ marginTop: 24, maxWidth: DETAIL_CONTENT_WIDTH }}>
          <NetworkCidrManager networkId={networkId} v4Blocks={v4} v6Blocks={v6} />
        </div>
      );
    },
  },

  subnets: {
    // VPC-1: derived placement (ZONAL zone / REGIONAL region) + primary anchor.
    overviewExtra: ({ data }) => {
      return [
        {
          // Якорь размещения — ресурс geo, поэтому ссылка (как «Сеть» ниже), а
          // не моноширинный идентификатор. Ветку рисует единственный
          // `PlacementAnchor` — здесь стояла третья её копия.
          label: "Размещение",
          value: <PlacementAnchor row={data} maxChars={42} />,
        // Копируется идентификатор якоря — зоны либо региона.
        copy: getByPath<string>(data, "zone_id") || getByPath<string>(data, "region_id") || undefined,
        },
        {
          label: "Сеть",
          value: <RefNameLink specId="networks" refId={getByPath<string>(data, "network_id")}  maxChars={42} copy={false} />, copy: getByPath<string>(data, "network_id") ?? undefined,
        },
        {
          label: "Таблица маршрутов",
          value: getByPath<string>(data, "route_table_id") ? (
            <RefNameLink specId="route-tables" refId={getByPath<string>(data, "route_table_id")} maxChars={42} copy={false} />
          ) : (
            dash
          ),
          copy: getByPath<string>(data, "route_table_id") ?? undefined,
        },
        // CIDR (primary + доп.) — НЕ в таблице Обзора: доп. диапазоны управляются
        // отдельными RPC (:add/:remove-cidr-blocks), показаны панелью ниже.
      ];
    },
    // CIDR — отдельная панель управления под Обзором: основной (immutable) +
    // доп. диапазоны (:add/:remove-cidr-blocks, не PATCH).
    overviewBelow: ({ data, projectId }) => {
      const subnetId = getByPath<string>(data, "id") ?? "";
      const v4Primary = getByPath<string>(data, "ipv4_cidr_primary") ?? "";
      const v6Primary = getByPath<string>(data, "ipv6_cidr_primary") ?? "";
      const v4 = getByPath<string[]>(data, "ipv4_cidr_blocks") ?? [];
      const v6 = getByPath<string[]>(data, "ipv6_cidr_blocks") ?? [];
      return (
        <SubnetCidrPanel
          subnetId={subnetId}
          v4Primary={v4Primary}
          v6Primary={v6Primary}
          v4Blocks={v4}
          v6Blocks={v6}
          projectId={projectId}
        />
      );
    },
  },

  // Балансировщик нагрузки — площадка размещения (#1473).
  //
  // Обзор карточки состоит из пяти обязательных строк плюс доменных, и до этой
  // записи балансировщик не показывал НИ ОДНОЙ координаты размещения: ни зоны,
  // ни региона. «Размещение зональное» читалось из списка, а на какой площадке
  // стоит балансировщик — не отвечал никто, при том что машина и балансировщик
  // обязаны совпасть площадкой (data-integrity.md §Placement-coherence).
  "load-balancers": {
    overviewExtra: ({ data }) => [
      {
        // Ветку ZONAL/REGIONAL рисует тот же единственный `PlacementAnchor`,
        // что и у подсети: якорь — ресурс geo, поэтому ссылка, а не
        // моноширинный идентификатор.
        label: "Размещение",
        value: <PlacementAnchor row={data} maxChars={42} />,
        // Копируется идентификатор якоря — зоны либо региона.
        copy: getByPath<string>(data, "zone_id") || getByPath<string>(data, "region_id") || undefined,
      },
    ],
  },

  "route-tables": {
    overviewExtra: ({ data }) => [
      {
        label: "Сеть",
        value: <RefNameLink specId="networks" refId={getByPath<string>(data, "network_id")}  maxChars={42} copy={false} />, copy: getByPath<string>(data, "network_id") ?? undefined,
      },
    ],
    // Статические маршруты — отдельная таблица с подписью под Обзором.
    overviewBelow: ({ data, projectId }) => {
      // KAC-239: маршруты управляются отдельно от ресурса — RoutesPanel
      // (Добавить / чекбоксы + bulk-delete), не правкой всего RT.
      const routes = getByPath<StaticRoute[]>(data, "static_routes") ?? [];
      const rtId = getByPath<string>(data, "id") ?? "";
      return <RoutesPanel routeTableId={rtId} projectId={projectId} routes={routes} />;
    },
  },

  "security-groups": {
    overviewExtra: ({ data, projectId }) => {
      return [
        {
          label: "Сеть",
          value: getByPath<string>(data, "network_id") ? (
            <RefNameLink specId="networks" refId={getByPath<string>(data, "network_id")} maxChars={42} copy={false} />
          ) : (
            dash
          ),
          copy: getByPath<string>(data, "network_id") ?? undefined,
        },
        {
          label: "Назначение",
          value: (
            <BoolFact
              value={getByPath<boolean>(data, "default_for_network")}
              yes="Группа по умолчанию для сети"
              no="Назначается только явно"
              accent
            />
          ),
        },
        {
          // Поле вернулось ВМЕСТЕ С ИСТОЧНИКОМ, как и было записано при его
          // снятии: сервер выводит потребителей чтением по отношениям, которые
          // база уже держит (набор групп на интерфейсе и группа по умолчанию у
          // сети). Пока источника не было, поле показывало прочерк при живых
          // потребителях — то есть утверждало о ресурсе неправду.
          //
          // Потолок передаётся ЯВНО: у группы правил число потребителей ничем не
          // ограничено, сервер отдаёт предел плюс одну строку, и по этой лишней
          // строке компонент отличает полный список от усечённого.
          label: "Потребители",
          value: (
            <ConsumersFact
              usedBy={getByPath<UsedByEntry[]>(data, "used_by")}
              projectId={projectId}
              limit={SECURITY_GROUP_USED_BY_LIMIT}
            />
          ),
        },
      ];
    },
    // req: правила — ОТДЕЛЬНЫМ табом «Правила» (таблица + «Добавить» + чекбоксы +
    // bulk-delete через SgRulesPanel). Бэкенд — UpdateRules по стабильным id.
    extraTabs: ({ data, projectId }) => {
      const all = getByPath<SgRule[]>(data, "rules") ?? [];
      const sgId = getByPath<string>(data, "id") ?? "";
      // KAC-243 (scenario 18): network_id SG → SG-target picker в редакторе
      // правил фильтрует кандидатов по той же сети.
      const networkId = getByPath<string>(data, "network_id") ?? "";
      return [
        {
          id: "rules",
          label: "Правила",
          count: all.length,
          render: () => <SgRulesPanel sgId={sgId} projectId={projectId} rules={all} networkId={networkId} />,
        },
      ];
    },
  },

  addresses: {
    overviewExtra: ({ data, projectId }) => {
      const info = addressInfo(data);
      const usedBy = getByPath<{ referrer?: { type?: string; id?: string } }[]>(data, "used_by") ?? [];
      const used = getByPath<boolean>(data, "used") ?? usedBy.length > 0;
      return [
        { label: "IP-адрес", value: cidrTags(info.ip ? [info.ip] : undefined) },
        { label: "Версия", value: txt(info.family) },
        { label: "Вид", value: txt(info.kind) },
        {
          label: "Занятость",
          // Тон `active` — «связь установлена, ресурс задействован» (канон §5).
          // Не `good`, которым помечена защита ниже: одинаковый тон делал охрану
          // и занятость одним событием на вид. Без тона вовсе «занят» получал
          // приглушённый цвет и читался выключенным — с этого начался #446.
          value: <BoolFact value={used} yes="Используется ресурсом" no="Свободен" yesTone="active" yesGlyph="link" />,
        },
        {
          // Тот же вид, что у группы правил, — но БЕЗ потолка: у адреса число
          // потребителей ограничено по построению (адрес держит один интерфейс),
          // и придумывать ему предел значило бы обещать усечение, которого не
          // бывает.
          label: "Потребители",
          value: <ConsumersFact usedBy={usedBy} projectId={projectId} />,
        },
        {
          label: "Защита от удаления",
          value: (
            <BoolFact
              value={getByPath<boolean>(data, "deletion_protection")}
              yes="Удаление запрещено"
              no="Удаление разрешено"
              yesTone="good"
              yesGlyph="lock"
              noTone="attention"
              noGlyph="unlock"
              />
          ),
        },
      ];
    },
  },

  gateways: {
    overviewExtra: ({ data }) => [
      { label: "Тип", value: txt(getByPath<string>(data, "type") || "SHARED_EGRESS_GATEWAY") },
    ],
  },

  "cidr-groups": {
    overviewExtra: ({ data, projectId }) => [
      { label: "Членов", value: txt(getByPath<number>(data, "cidr_block_count")) },
      {
        // Тот же вид, что у адреса и у группы правил, — и БЕЗ потолка: сервер
        // отдаёт потребителей набора целиком (усечения на этом поле у него нет),
        // поэтому подпись «показаны первые N» была бы утверждением, которого
        // никто не делал.
        label: "Кем используется",
        value: (
          <ConsumersFact
            usedBy={getByPath<UsedByEntry[]>(data, "used_by") ?? []}
            projectId={projectId}
          />
        ),
      },
    ],
    // Состав набора — глаголы `:add-cidr-blocks` / `:remove-cidr-blocks`, как у
    // подсети и сети; правкой он не меняется (Update таких полей не несёт).
    overviewBelow: ({ data }) => {
      const id = getByPath<string>(data, "id") ?? "";
      const v4 = getByPath<string[]>(data, "v4_cidr_blocks") ?? [];
      const v6 = getByPath<string[]>(data, "v6_cidr_blocks") ?? [];
      return (
        <div style={{ marginTop: 24, maxWidth: DETAIL_CONTENT_WIDTH }}>
          <CidrGroupBlocksManager cidrGroupId={id} v4Blocks={v4} v6Blocks={v6} />
        </div>
      );
    },
  },

  "network-interfaces": {
    overviewExtra: ({ data, projectId }) => [
      {
        label: "Подсеть",
        value: <RefNameLink specId="subnets" refId={getByPath<string>(data, "subnet_id")}  maxChars={42} copy={false} />, copy: getByPath<string>(data, "subnet_id") ?? undefined,
      },
      {
        label: "MAC-адрес",
        value: mono(getByPath<string>(data, "mac_address")),
        // MAC переносят в конфигурацию гостя и в правила соседних систем —
        // ровно тот случай, ради которого кнопка и заведена. Пустого значения
        // копировать нечего: там стоит прочерк, и кнопка обещала бы значение.
        copy: getByPath<string>(data, "mac_address") || undefined,
      },
      {
        label: "IPv4-адреса",
        value: <AddressRefTags ids={getByPath<string[]>(data, "v4_address_ids")} projectId={projectId} />,
      },
      {
        label: "IPv6-адреса",
        value: <AddressRefTags ids={getByPath<string[]>(data, "v6_address_ids")} projectId={projectId} />,
      },
      {
        label: "Группы безопасности",
        value: refLinks(getByPath<string[]>(data, "security_group_ids"), "security-groups"),
      },
    ],
  },
};

// Расширения, зарегистрированные app'ом на старте (напр. IAM-remote регистрирует
// доменные детейл-расширения своих ресурсов). Так shared остаётся app-agnostic:
// доменная специфика инжектится потребителем, а не хардкодится здесь. Регистрация
// перекрывает базовую DETAIL_EXTENSIONS для того же specId.
const registeredExtensions: Record<string, DetailExtension> = {};

// registerDetailExtension — подключает доменное расширение detail-страницы для
// ресурса specId (вызывается app'ом на старте, до рендера detail-страниц).
export function registerDetailExtension(specId: string, ext: DetailExtension): void {
  registeredExtensions[specId] = ext;
}

export function detailExtension(specId: string): DetailExtension | undefined {
  return registeredExtensions[specId] ?? DETAIL_EXTENSIONS[specId];
}
