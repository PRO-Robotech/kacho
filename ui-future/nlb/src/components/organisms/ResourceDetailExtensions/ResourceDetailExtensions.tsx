// resource-detail-extensions — реестр доменных расширений detail-страницы NLB.
//
// ResourceShell остаётся generic (Обзор / связанные / Операции / JSON + формы-
// панели). Доменно-специфичные строки Обзора конкретного ресурса подключаются
// здесь по spec.id. Для NLB: LoadBalancer — регион/схема/размещение/VIP/статус;
// Listener — балансировщик/протокол/порт; TargetGroup — регион/порт/окна
// вывода и разгона/проба.
// Богатый LoadBalancer-detail (attach/detach TG, per-tab actions) подключается
// отдельной кастом-обёрткой на следующем этапе.

import { type ReactNode } from "react";
import { Tag, Typography } from "antd";

import type { DetailTab } from "@/components/organisms/DetailShell";
import { RefNameLink } from "@/components/molecules/RefNameLink";
import { TargetsManager, type Target } from "@/components/organisms/TargetsManager";
import { StatusBadge } from "@/components/atoms/StatusBadge";
import { NlbVipCell } from "@/components/molecules/NlbVipCell";
import { getByPath } from "@/lib/resource-registry";

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

function code(v: unknown): ReactNode {
  const s = v == null ? "" : String(v);
  return s ? (
    <Typography.Text code style={{ fontSize: 12 }}>
      {s}
    </Typography.Text>
  ) : (
    dash
  );
}

function boolTag(v: unknown, yes = "Да", no = "Нет"): ReactNode {
  return v ? <Tag color="green">{yes}</Tag> : <Tag>{no}</Tag>;
}

/**
 * Административное состояние балансировщика — словом, а не именем константы.
 *
 * Это замена снятым глаголам `:start`/`:stop`: выключенный балансировщик
 * сохраняет конфигурацию, а его таргеты сообщаются как INACTIVE. Не показать
 * его значит оставить выключенный балансировщик неотличимым от рабочего —
 * ACTIVE в строке «Статус» стоит у обоих.
 */
function adminStateTag(v: unknown): ReactNode {
  switch (v) {
    case "ADMIN_STATE_ENABLED":
      return <Tag color="green">Включён</Tag>;
    case "ADMIN_STATE_DISABLED":
      return <Tag color="red">Выключен</Tag>;
    default:
      // UNSPECIFIED/пусто — сервер состояния не назвал; выдавать это за
      // «включён» нельзя.
      return dash;
  }
}

/**
 * Подстатус листенера — производное значение: резолвится ли его целевая группа.
 *
 * MISCONFIGURED значит «объявлен, обслуживать некому»; из строки «Статус» это не
 * видно — она остаётся ACTIVE. UNSPECIFIED/пусто выдавать за OK нельзя.
 */
function substatusTag(v: unknown): ReactNode {
  switch (v) {
    case "OK":
      return <Tag color="green">обслуживается</Tag>;
    case "MISCONFIGURED":
      return <Tag color="orange">целевая группа не резолвится</Tag>;
    default:
      return dash;
  }
}

/** Список идентификаторов чипами; пустой набор — прочерк, а не пустая строка. */
function idTags(v: unknown): ReactNode {
  const ids = Array.isArray(v) ? (v as unknown[]).map(String).filter(Boolean) : [];
  if (ids.length === 0) return dash;
  return (
    <span>
      {ids.map((id) => (
        <Tag key={id} style={{ marginInlineEnd: 4, fontFamily: "ui-monospace, SFMono-Regular, monospace" }}>
          {id}
        </Tag>
      ))}
    </span>
  );
}

/** Ветви пробы целевой группы — ровно одна из четырёх задана (oneof options). */
const HEALTH_CHECK_KINDS = ["tcp", "http", "https", "grpc"] as const;

/**
 * Краткое описание пробы: «<ветвь> :<порт>».
 *
 * Проба не именована — `name` снят с контракта, — поэтому отличать одну от
 * другой приходится тем, что проба собственно делает. Порт берётся из
 * производного `effective_port` (переопределение ветви, иначе порт группы):
 * расхождение порта пробы и порта трафика видно by construction. Ни одной
 * заданной ветви — пусто: молчание ответа не выдаём за настроенную проверку.
 */
function healthCheckSummary(data: Record<string, unknown>): string {
  const kind = HEALTH_CHECK_KINDS.find((k) => getByPath<unknown>(data, `health_check.${k}`) != null);
  if (!kind) return "";
  const port = getByPath<number>(data, "health_check.effective_port");
  return port ? `${kind} :${port}` : kind;
}

// ─────────────────────────── реестр ───────────────────────────

export const DETAIL_EXTENSIONS: Record<string, DetailExtension> = {
  "load-balancers": {
    // Единая таблица «Обзор»: immutable схема/размещение + mutable-скаляры +
    // резолвнутый VIP пофамильно + drain-зоны. Размещение — только для INTERNAL,
    // зоны без анонса — только для REGIONAL (зеркалит форму создания).
    //
    // Условные строки показываются ровно там, где поле применимо, — границы
    // взяты у владельца контракта, а не угаданы: cross_zone_enabled применим при
    // любом НЕ-зональном размещении (включая EXTERNAL, у которого placement_type
    // пуст), security_group_ids — только у INTERNAL (группы сетевые). Показывать
    // значение там, где сервер его отвергает, значит предлагать настройку,
    // которой у этой посадки нет.
    overviewExtra: ({ data }) => {
      const type = getByPath<string>(data, "type") ?? "";
      const placement = getByPath<string>(data, "placement_type") ?? "";
      const drainZones = (getByPath<string[]>(data, "disabled_announce_zones") ?? []) as string[];
      const items: DescItem[] = [
        { label: "Регион", value: code(getByPath<string>(data, "region_id")) },
        {
          label: "Схема",
          value: type ? <Tag color={type === "INTERNAL" ? "geekblue" : "blue"}>{type}</Tag> : dash,
        },
      ];
      if (type === "INTERNAL") {
        items.push({
          label: "Размещение",
          value: placement ? <Tag color={placement === "REGIONAL" ? "purple" : "cyan"}>{placement}</Tag> : dash,
        });
      }
      items.push(
        { label: "Административное состояние", value: adminStateTag(getByPath<string>(data, "admin_state")) },
        { label: "Session affinity", value: code(getByPath<string>(data, "session_affinity")) },
        { label: "IPv4-адрес", value: <NlbVipCell v4AddressId={getByPath<string>(data, "v4_address_id")} /> },
        { label: "IPv6-адрес", value: <NlbVipCell v6AddressId={getByPath<string>(data, "v6_address_id")} /> },
      );
      if (placement === "REGIONAL") {
        items.push({
          label: "Зоны без анонса",
          value:
            drainZones.length > 0 ? (
              <span>
                {drainZones.map((z) => (
                  <Tag key={z} style={{ marginInlineEnd: 4 }}>
                    {z}
                  </Tag>
                ))}
              </span>
            ) : (
              <Typography.Text type="secondary">анонс из всех зон</Typography.Text>
            ),
        });
      }
      // Балансировка между зонами неприменима только зональному размещению
      // (у него зона одна) — при пустом placement_type (EXTERNAL) применима.
      if (placement !== "ZONAL") {
        items.push({
          label: "Балансировка между зонами",
          value: boolTag(getByPath<boolean>(data, "cross_zone_enabled")),
        });
      }
      if (type === "INTERNAL") {
        items.push({
          label: "Группы безопасности VIP",
          value: idTags(getByPath<string[]>(data, "security_group_ids")),
        });
      }
      items.push(
        { label: "Статус", value: <StatusBadge state={getByPath<string>(data, "status")} /> },
        { label: "Защита от удаления", value: boolTag(getByPath<boolean>(data, "deletion_protection")) },
      );
      return items;
    },
  },

  listeners: {
    overviewExtra: ({ data }) => [
      {
        label: "Балансировщик",
        value: (
          <RefNameLink specId="load-balancers" refId={getByPath<string>(data, "load_balancer_id")} maxChars={42} />
        ),
      },
      { label: "Протокол", value: code(getByPath<string>(data, "protocol")) },
      { label: "Порт", value: code(getByPath<number>(data, "port")) },
      { label: "Порт на target", value: code(getByPath<number>(data, "target_port")) },
      // Целевая группа листенера: привязка перешла сюда со снятых глаголов
      // балансировщика (:attachTargetGroup / :detachTargetGroup). Строка одна, и
      // групп тоже одна: на текущем шаге контракта `target_group_id` и
      // `default_target_group_id` — два имени ОДНОЙ ссылки (владелец отдаёт в них
      // одно значение). Показывается идущее вперёд имя; вторую строку заводить
      // нельзя — она читалась бы как вторая группа.
      {
        label: "Целевая группа",
        value: <RefNameLink specId="target-groups" refId={getByPath<string>(data, "target_group_id")} maxChars={42} />,
      },
      // Порт бэкенда — эхо TargetGroup.port, а не frontend-порт листенера.
      // Ноль в контракте означает «ни одна группа не резолвится», а не номер
      // порта, поэтому показывается прочерком.
      { label: "Порт бэкенда", value: code(getByPath<number>(data, "resolved_backend_port") || "") },
      // Подстатус: листенер бывает объявлен и ACTIVE, а обслуживать его некому
      // (целевой группы нет или ссылка повисла). Из строки «Статус» это не видно.
      { label: "Состояние конфигурации", value: substatusTag(getByPath<string>(data, "substatus")) },
      { label: "Статус", value: <StatusBadge state={getByPath<string>(data, "status")} /> },
    ],
  },

  "target-groups": {
    overviewExtra: ({ data }) => [
      { label: "Регион", value: txt(getByPath<string>(data, "region_id")) },
      // Единственный backend-порт группы: именно его листенер переотражает в
      // `resolved_backend_port`, и от него же наследуется порт пробы.
      { label: "Порт бэкенда", value: code(getByPath<number>(data, "port")) },
      // Duration приходит строкой секунд с хвостовым «s» («300s») — своей
      // единицы подпись не называет, иначе она противоречила бы значению.
      { label: "Drain timeout", value: code(getByPath<string>(data, "deregistration_delay")) },
      { label: "Slow start", value: code(getByPath<string>(data, "slow_start")) },
      // У пробы нет имени (оно снято с контракта: HealthCheck — встроенный
      // объект-значение, а не адресуемый ресурс). Содержательны выбранная ветвь
      // (tcp|http|https|grpc) и разрешённый порт, а не идентичность.
      { label: "Health-check", value: code(healthCheckSummary(data)) },
      { label: "Статус", value: <StatusBadge state={getByPath<string>(data, "status")} /> },
    ],
    // Управление backend-таргетами (add/remove через :addTargets/:removeTargets)
    // прямо в блоке «Обзор».
    overviewBelow: ({ data, projectId }) => (
      <TargetsManager
        targetGroupId={getByPath<string>(data, "id") ?? ""}
        projectId={projectId}
        targets={getByPath<Target[]>(data, "targets") ?? []}
      />
    ),
  },
};

export function detailExtension(specId: string): DetailExtension | undefined {
  return DETAIL_EXTENSIONS[specId];
}
